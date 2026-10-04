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
	// DirInfo reports mode and owner uid of a directory (default: os.Stat).
	// Injected in tests (FR-R13).
	DirInfo func(path string) (mode os.FileMode, uid int, err error)
	// UID returns the current user id (default: os.Getuid).
	UID func() int
}

// File is the file-backed usecase.Ledger.
type File struct {
	path      string
	retention time.Duration
	lockWait  time.Duration
	dirInfo   func(string) (os.FileMode, int, error)
	uid       func() int
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
	f.dirInfo, f.uid = cfg.DirInfo, cfg.UID
	if f.dirInfo == nil {
		f.dirInfo = osDirInfo
	}
	if f.uid == nil {
		f.uid = os.Getuid
	}
	if err := f.checkDir(); err != nil {
		return nil, err
	}
	return f, nil
}

func osDirInfo(path string) (os.FileMode, int, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	uid := -1
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		uid = int(sys.Uid)
	}
	return st.Mode(), uid, nil
}

// checkDir refuses an existing ledger directory that is not mode 0700 or not
// owned by the current user (FR-R13). A missing directory is fine: it is
// created 0700 on first use. This is a guardrail against mistakes, not
// against a hostile local user (see the trust model in the docs).
func (l *File) checkDir() error {
	dir := filepath.Dir(l.path)
	mode, uid, err := l.dirInfo(dir)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil // created (or failing closed) at first use
	}
	if err != nil {
		return failClosed("cannot inspect ledger directory", err)
	}
	hint := "chmod 700 and chown the ledger directory to the agent user, or point the ledger at a private directory"
	if mode.Perm() != 0o700 {
		return domain.NewGeneral(fmt.Sprintf("ledger directory mode is %04o, want 0700", mode.Perm())).WithHint(hint)
	}
	if uid != l.uid() {
		return domain.NewGeneral("ledger directory is not owned by the current user").WithHint(hint)
	}
	return nil
}

type entryJSON struct {
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"`
	At          time.Time `json:"at"`
	RefID       string    `json:"ref_id,omitempty"`
	Count       int       `json:"count,omitempty"`
	// Kind is "send" or "draft"; empty (older builds) means send.
	Kind string `json:"kind,omitempty"`
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
	if err := l.checkDir(); err != nil {
		return err
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
		if k == "" || !validStatus(e.Status) || !validKind(e.Kind) {
			return nil, failClosed("ledger file is corrupt", nil)
		}
	}
	return &st, nil
}

func validKind(k string) bool {
	switch usecase.LedgerKind(k) {
	case "", usecase.LedgerKindSend, usecase.LedgerKindDraft:
		return true
	}
	return false
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
	return usecase.LedgerEntry{Key: key, Fingerprint: e.Fingerprint, Status: usecase.LedgerStatus(e.Status), Kind: kindOf(e), At: e.At, RefID: e.RefID, Count: e.Count}
}

func kindOf(e entryJSON) usecase.LedgerKind {
	if e.Kind == "" {
		return usecase.LedgerKindSend
	}
	return usecase.LedgerKind(e.Kind)
}

// countsTowardRate: send-kind entries that are sent or pending (pending counts
// conservatively: an ambiguous send may have gone out).
func countsTowardRate(e entryJSON) bool {
	return kindOf(e) == usecase.LedgerKindSend &&
		(e.Status == string(usecase.LedgerSent) || e.Status == string(usecase.LedgerPending))
}

func (l *File) prune(st *fileJSON, now time.Time) {
	for k, e := range st.Entries {
		if e.Status != string(usecase.LedgerPending) && now.Sub(e.At) > l.retention {
			delete(st.Entries, k)
		}
	}
}

// Reserve implements usecase.Ledger. It reserves a send-kind entry with no
// rate check.
func (l *File) Reserve(ctx context.Context, key, fingerprint string, at time.Time) (usecase.LedgerEntry, bool, error) {
	e, created, _, err := l.ReserveWithin(ctx, key, fingerprint, usecase.LedgerKindSend, at, nil)
	return e, created, err
}

// ReserveWithin implements usecase.Ledger: the rate-window count and the
// reservation happen under one lock (FR-R8).
func (l *File) ReserveWithin(ctx context.Context, key, fingerprint string, kind usecase.LedgerKind, at time.Time, windows []usecase.RateWindow) (usecase.LedgerEntry, bool, []int, error) {
	if key == "" {
		return usecase.LedgerEntry{}, false, nil, domain.NewValidation("ledger key is empty")
	}
	if kind != usecase.LedgerKindSend && kind != usecase.LedgerKindDraft {
		return usecase.LedgerEntry{}, false, nil, domain.NewValidation("ledger kind is invalid")
	}
	var out usecase.LedgerEntry
	var created bool
	var counts []int
	err := l.withLock(ctx, func(st *fileJSON) (bool, error) {
		e, existed := st.Entries[key]
		if existed && e.Status != string(usecase.LedgerFailed) {
			out = toEntry(key, e)
			return false, nil
		}
		if kind == usecase.LedgerKindSend && len(windows) > 0 {
			counts = make([]int, len(windows))
			over := false
			for k, o := range st.Entries {
				if k == key || !countsTowardRate(o) {
					continue
				}
				for i, w := range windows {
					if !o.At.Before(w.Since) {
						counts[i]++
					}
				}
			}
			for i, w := range windows {
				if w.Cap > 0 && counts[i] >= w.Cap {
					over = true
				}
			}
			if over {
				out = usecase.LedgerEntry{Key: key, Fingerprint: fingerprint, Status: usecase.LedgerOverCap, Kind: kind}
				return false, nil
			}
		}
		st.Entries[key] = entryJSON{Fingerprint: fingerprint, Status: string(usecase.LedgerPending), At: at, Kind: string(kind)}
		l.prune(st, at)
		out = toEntry(key, st.Entries[key])
		if !existed {
			out.Status = usecase.LedgerNew
		}
		created = true
		return true, nil
	})
	if err != nil {
		return usecase.LedgerEntry{}, false, nil, err
	}
	if out.Status == usecase.LedgerOverCap {
		return out, false, counts, nil
	}
	if !created {
		counts = nil
	}
	return out, created, counts, nil
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
			if countsTowardRate(e) && !e.At.Before(since) {
				n++
			}
		}
		return false, nil
	})
	return n, err
}
