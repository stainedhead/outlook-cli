package ledger

import (
	"context"
	"errors"
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
	// Root, when set, is the policy read.attachments.out_dir. Save refuses an
	// outDir that does not resolve (symlinks included) to Root or below. When
	// empty the caller (the use case) has already validated outDir against
	// policy and only the outDir itself is resolved.
	Root string
}

var _ usecase.QuarantineStore = Quarantine{}

const maxNameLen = 120

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
	safe := SanitizeName(name)
	path := filepath.Join(dir, safe)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", 0, domain.NewConflict("an attachment with this name is already in the quarantine directory")
		}
		return "", 0, domain.NewGeneral("cannot create attachment file").WithCause(err)
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

// resolve creates outDir (0700) if needed, resolves symlinks and checks
// containment in Root.
func (q Quarantine) resolve(outDir string) (string, error) {
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", domain.NewGeneral("cannot create quarantine directory").WithCause(err)
	}
	dir, err := filepath.EvalSymlinks(outDir)
	if err != nil {
		return "", domain.NewGeneral("cannot resolve quarantine directory").WithCause(err)
	}
	if q.Root == "" {
		return dir, nil
	}
	if err := os.MkdirAll(q.Root, 0o700); err != nil {
		return "", domain.NewGeneral("cannot create quarantine root").WithCause(err)
	}
	root, err := filepath.EvalSymlinks(q.Root)
	if err != nil {
		return "", domain.NewGeneral("cannot resolve quarantine root").WithCause(err)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", domain.NewPolicyDenied("attachment output directory is outside the policy quarantine directory").
			WithHint("choose a directory inside read.attachments.out_dir")
	}
	return dir, nil
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
