package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

const (
	fileVersion = 1
	// DefaultRetention is how long sent and failed entries are kept.
	DefaultRetention = 90 * 24 * time.Hour
	// DefaultLockTimeout bounds how long a process waits for the lock.
	DefaultLockTimeout = 10 * time.Second
	lockPoll           = 5 * time.Millisecond
)

// Config configures a File ledger.
type Config struct {
	// Path of the ledger JSON file. Its directory is created 0700.
	Path string
	// Retention drops sent and failed entries older than this at write time
	// (measured against the time passed to the mutating call). Pending entries
	// are never dropped: they must keep failing closed. Zero means
	// DefaultRetention.
	Retention time.Duration
	// LockTimeout zero means DefaultLockTimeout.
	LockTimeout time.Duration
}

// File is the file-backed usecase.Ledger.
type File struct {
	path      string
	retention time.Duration
	lockWait  time.Duration
}

var _ usecase.Ledger = (*File)(nil)

// New returns a File ledger. The file is created on first write.
func New(cfg Config) (*File, error) {
	if cfg.Path == "" {
		return nil, domain.NewUsage("ledger path is empty")
	}
	f := &File{path: cfg.Path, retention: cfg.Retention, lockWait: cfg.LockTimeout}
	if f.retention <= 0 {
		f.retention = DefaultRetention
	}
	if f.lockWait <= 0 {
		f.lockWait = DefaultLockTimeout
	}
	return f, nil
}

type entryJSON struct {
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"`
	At          time.Time `json:"at"`
	RefID       string    `json:"ref_id,omitempty"`
	Count       int       `json:"count,omitempty"`
}

type fileJSON struct {
	Version int                  `json:"version"`
	Entries map[string]entryJSON `json:"entries"`
}

func failClosed(msg string, cause error) error {
	return domain.NewGeneral(msg).WithCause(cause).
		WithHint("the send ledger is unusable, so nothing was sent; a human must inspect or remove the ledger file")
}

// withLock runs fn holding the exclusive lock. fn receives the loaded state and
// returns whether to persist it.
func (l *File) withLock(ctx context.Context, fn func(*fileJSON) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return failClosed("cannot create ledger directory", err)
	}
	lock, err := os.OpenFile(l.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return failClosed("cannot open ledger lock", err)
	}
	defer func() { _ = lock.Close() }()
	if err := l.flock(ctx, lock); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()

	st, err := l.load()
	if err != nil {
		return err
	}
	persist, err := fn(st)
	if err != nil {
		return err
	}
	if !persist {
		return nil
	}
	return l.store(st)
}

func (l *File) flock(ctx context.Context, f *os.File) error {
	deadline := time.Now().Add(l.lockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return failClosed("cannot lock ledger", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return failClosed("timed out waiting for the ledger lock", nil)
		}
		time.Sleep(lockPoll)
	}
}

func (l *File) load() (*fileJSON, error) {
	b, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return &fileJSON{Version: fileVersion, Entries: map[string]entryJSON{}}, nil
	}
	if err != nil {
		return nil, failClosed("cannot read ledger", err)
	}
	var st fileJSON
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, failClosed("ledger file is corrupt", nil)
	}
	if st.Version != fileVersion {
		return nil, failClosed(fmt.Sprintf("ledger file has unsupported version %d", st.Version), nil)
	}
	if st.Entries == nil {
		st.Entries = map[string]entryJSON{}
	}
	for k, e := range st.Entries {
		if k == "" || !validStatus(e.Status) {
			return nil, failClosed("ledger file is corrupt", nil)
		}
	}
	return &st, nil
}

func validStatus(s string) bool {
	switch usecase.LedgerStatus(s) {
	case usecase.LedgerPending, usecase.LedgerSent, usecase.LedgerFailed:
		return true
	}
	return false
}

// store replaces the ledger atomically: temp file in the same directory,
// fsync, rename, fsync of the directory.
func (l *File) store(st *fileJSON) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return failClosed("cannot encode ledger", err)
	}
	dir := filepath.Dir(l.path)
	tmp, err := os.CreateTemp(dir, ".ledger-*.tmp")
	if err != nil {
		return failClosed("cannot write ledger", err)
	}
	name := tmp.Name()
	werr := func() error {
		if err := tmp.Chmod(0o600); err != nil {
			return err
		}
		if _, err := tmp.Write(append(b, '\n')); err != nil {
			return err
		}
		return tmp.Sync()
	}()
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(name)
		return failClosed("cannot write ledger", werr)
	}
	if err := os.Rename(name, l.path); err != nil {
		_ = os.Remove(name)
		return failClosed("cannot replace ledger", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func toEntry(key string, e entryJSON) usecase.LedgerEntry {
	return usecase.LedgerEntry{Key: key, Fingerprint: e.Fingerprint, Status: usecase.LedgerStatus(e.Status), At: e.At, RefID: e.RefID, Count: e.Count}
}

func (l *File) prune(st *fileJSON, now time.Time) {
	for k, e := range st.Entries {
		if e.Status != string(usecase.LedgerPending) && now.Sub(e.At) > l.retention {
			delete(st.Entries, k)
		}
	}
}

// Reserve implements usecase.Ledger.
func (l *File) Reserve(ctx context.Context, key, fingerprint string, at time.Time) (usecase.LedgerEntry, bool, error) {
	if key == "" {
		return usecase.LedgerEntry{}, false, domain.NewValidation("ledger key is empty")
	}
	var out usecase.LedgerEntry
	var created bool
	err := l.withLock(ctx, func(st *fileJSON) (bool, error) {
		e, existed := st.Entries[key]
		if existed && e.Status != string(usecase.LedgerFailed) {
			out = toEntry(key, e)
			return false, nil
		}
		st.Entries[key] = entryJSON{Fingerprint: fingerprint, Status: string(usecase.LedgerPending), At: at}
		l.prune(st, at)
		out = toEntry(key, st.Entries[key])
		if !existed {
			out.Status = usecase.LedgerNew
		}
		created = true
		return true, nil
	})
	if err != nil {
		return usecase.LedgerEntry{}, false, err
	}
	return out, created, nil
}

func (l *File) transition(ctx context.Context, key string, at time.Time, fn func(*entryJSON)) error {
	return l.withLock(ctx, func(st *fileJSON) (bool, error) {
		e, ok := st.Entries[key]
		if !ok {
			return false, domain.NewNotFound("ledger entry not found").
				WithHint("Reserve the key before completing or failing it")
		}
		fn(&e)
		e.At = at
		st.Entries[key] = e
		l.prune(st, at)
		return true, nil
	})
}

// Complete implements usecase.Ledger.
func (l *File) Complete(ctx context.Context, key, refID string, at time.Time) error {
	return l.transition(ctx, key, at, func(e *entryJSON) {
		e.Status = string(usecase.LedgerSent)
		e.RefID = refID
	})
}

// Fail implements usecase.Ledger.
func (l *File) Fail(ctx context.Context, key string, at time.Time) error {
	return l.transition(ctx, key, at, func(e *entryJSON) { e.Status = string(usecase.LedgerFailed) })
}

// SentSince implements usecase.Ledger.
func (l *File) SentSince(ctx context.Context, since time.Time) (int, error) {
	n := 0
	err := l.withLock(ctx, func(st *fileJSON) (bool, error) {
		for _, e := range st.Entries {
			if e.Status == string(usecase.LedgerSent) && !e.At.Before(since) {
				n++
			}
		}
		return false, nil
	})
	return n, err
}
