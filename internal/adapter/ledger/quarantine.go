package ledger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"

	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

// Quarantine is the file-backed usecase.QuarantineStore. It writes bytes and
// nothing else: the content is never opened, executed or interpreted.
type Quarantine struct {
	// Root is the policy read.attachments.out_dir. Save refuses an outDir that
	// does not resolve (symlinks included) to Root or below, and checks this
	// BEFORE creating any directory. An empty Root is invalid: use
	// NewQuarantine, which rejects it at construction; the zero value fails
	// closed in Save.
	Root string
}

// NewQuarantine returns a Quarantine confined to root (FR-R12).
func NewQuarantine(root string) (Quarantine, error) {
	if strings.TrimSpace(root) == "" {
		return Quarantine{}, domain.NewValidation("quarantine root is empty").
			WithHint("set read.attachments.out_dir in the policy")
	}
	return Quarantine{Root: root}, nil
}

var _ usecase.QuarantineStore = Quarantine{}

const (
	maxNameLen    = 120
	maxCollisions = 1000
)

// Save implements usecase.QuarantineStore.
func (q Quarantine) Save(ctx context.Context, outDir, name string, content io.Reader, maxBytes int64) (string, int64, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if maxBytes <= 0 {
		return "", 0, domain.NewValidation("attachment size cap must be positive")
	}
	if outDir == "" {
		return "", 0, domain.NewValidation("attachment output directory is empty")
	}
	dir, err := q.resolve(outDir)
	if err != nil {
		return "", 0, err
	}
	f, path, err := createUnique(dir, SanitizeName(name))
	if err != nil {
		return "", 0, err
	}
	n, copyErr := io.Copy(f, io.LimitReader(&ctxReader{ctx, content}, maxBytes+1))
	closeErr := f.Close()
	switch {
	case copyErr != nil:
		_ = os.Remove(path)
		return "", 0, copyErr
	case n > maxBytes:
		_ = os.Remove(path)
		return "", 0, domain.NewPolicyDenied("attachment is larger than the configured size cap").
			WithHint("the partial file was removed")
	case closeErr != nil:
		_ = os.Remove(path)
		return "", 0, domain.NewGeneral("cannot finish attachment file").WithCause(closeErr)
	}
	return path, n, nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// createUnique creates dir/name exclusively (0600, no symlink following). If
// the name is taken it tries "stem (n).ext" so an earlier file (or a sender
// who controls names) cannot make later downloads fail (FR-R12).
func createUnique(dir, name string) (*os.File, string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		stem, ext = name, ""
	}
	for i := 0; i < maxCollisions; i++ {
		cand := name
		if i > 0 {
			cand = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		path := filepath.Join(dir, cand)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if err == nil {
			return f, path, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", domain.NewGeneral("cannot create attachment file").WithCause(err)
		}
	}
	return nil, "", domain.NewConflict("too many attachments with this name in the quarantine directory")
}

// resolveProspective returns the real path p would have if created: the deepest
// existing ancestor is resolved with EvalSymlinks and the not-yet-existing
// remainder (cleaned, absolute, so no "..") is appended. Nothing is created.
func resolveProspective(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", domain.NewGeneral("cannot resolve path").WithCause(err)
	}
	var rest []string
	cur := abs
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(rest) - 1; i >= 0; i-- {
				real = filepath.Join(real, rest[i])
			}
			return real, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", domain.NewGeneral("cannot resolve quarantine directory").WithCause(err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", domain.NewGeneral("cannot resolve quarantine directory").WithCause(err)
		}
		rest = append(rest, filepath.Base(cur))
		cur = parent
	}
}

func within(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func outside() error {
	return domain.NewPolicyDenied("attachment output directory is outside the policy quarantine directory").
		WithHint("choose a directory inside read.attachments.out_dir")
}

// resolve checks containment of outDir in Root using the deepest existing
// ancestors (symlinks resolved), and only then creates the directories (0700).
// The result is re-resolved after creation to close a symlink race.
func (q Quarantine) resolve(outDir string) (string, error) {
	if strings.TrimSpace(q.Root) == "" {
		return "", domain.NewValidation("quarantine root is empty").
			WithHint("set read.attachments.out_dir in the policy")
	}
	root, err := resolveProspective(q.Root)
	if err != nil {
		return "", err
	}
	want, err := resolveProspective(outDir)
	if err != nil {
		return "", err
	}
	if !within(root, want) {
		return "", outside()
	}
	if err := os.MkdirAll(want, 0o700); err != nil {
		return "", domain.NewGeneral("cannot create quarantine directory").WithCause(err)
	}
	got, err := filepath.EvalSymlinks(want)
	if err != nil {
		return "", domain.NewGeneral("cannot resolve quarantine directory").WithCause(err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", domain.NewGeneral("cannot resolve quarantine root").WithCause(err)
	}
	if !within(realRoot, got) {
		return "", outside()
	}
	return got, nil
}

// SanitizeName reduces an untrusted attachment name to a safe single path
// element: no separators, no control characters, no leading dots, bounded
// length. An empty result becomes "attachment".
func SanitizeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == 0 || unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			continue
		case strings.ContainsRune(`<>:"|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimLeft(strings.TrimSpace(b.String()), ".")
	out = strings.TrimSpace(out)
	if len(out) > maxNameLen {
		out = truncateUTF8(out, maxNameLen)
	}
	if out == "" {
		return "attachment"
	}
	return out
}

func truncateUTF8(s string, n int) string {
	for n > 0 && n < len(s) && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
