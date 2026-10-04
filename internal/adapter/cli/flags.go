package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// maxBodyInput bounds --body-file / stdin so a runaway pipe cannot exhaust memory.
const maxBodyInput = 4 << 20

// errBodyTooLarge reports input over the cap.
var errBodyTooLarge = domain.NewUsage("body input exceeds the 4 MiB limit")

// readAll reads r up to maxBodyInput bytes and fails when there is more
// (FR-R7: no silent truncation).
func readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBodyInput+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBodyInput {
		return nil, errBodyTooLarge
	}
	return b, nil
}

// readBodyFile is the default Deps.ReadFile. It resolves symlinks, accepts a
// regular file only (no device, FIFO, socket or directory), re-checks the
// opened handle against swaps, and reads at most maxBodyInput bytes. It can
// read any file the user can; path roots are deferred (OQ-3).
func readBodyFile(path string) ([]byte, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(resolved); err != nil {
		return nil, err
	} else if !st.Mode().IsRegular() {
		return nil, errNotRegular
	}
	// O_NONBLOCK so a FIFO swapped in after the check cannot block open.
	f, err := os.OpenFile(resolved, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err != nil {
		return nil, err
	} else if !st.Mode().IsRegular() {
		return nil, errNotRegular
	}
	return readAll(f)
}

var errNotRegular = domain.NewUsage("--body-file must be a regular file (no device, FIFO or directory)")

// listFlag is a repeatable, comma-separated string flag (--to a@x,b@x --to c@x).
type listFlag struct{ v []string }

func (l *listFlag) String() string { return strings.Join(l.v, ",") }

func (l *listFlag) Set(s string) error {
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			l.v = append(l.v, p)
		}
	}
	return nil
}

func addList(fs *flag.FlagSet, name, usage string) *listFlag {
	l := &listFlag{}
	fs.Var(l, name, usage)
	return l
}

// parseSince accepts RFC 3339 or a plain date (UTC midnight).
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, domain.NewUsage("--since must be an RFC 3339 time or a YYYY-MM-DD date")
}

// need checks the number of positional arguments.
func need(pos []string, n int, usage string) error {
	if len(pos) != n {
		return domain.NewUsage("expected: " + usage)
	}
	return nil
}

// resolveBody returns the body from --body or --body-file ("-" is stdin).
func (r *runner) resolveBody(body, file string, bodySet bool) (string, error) {
	switch {
	case bodySet && file != "":
		return "", domain.NewUsage("use either --body or --body-file, not both")
	case bodySet:
		return body, nil
	case file == "":
		return "", domain.NewUsage("one of --body or --body-file is required")
	case file == "-":
		b, err := readAll(r.Stdin)
		if errors.Is(err, errBodyTooLarge) {
			return "", err
		}
		if err != nil {
			return "", domain.NewUsage("cannot read body from stdin")
		}
		return string(b), nil
	}
	b, err := r.ReadFile(file)
	if err != nil {
		var ue *domain.Error
		if errors.As(err, &ue) {
			return "", err
		}
		return "", domain.NewUsage("cannot read --body-file")
	}
	if len(b) > maxBodyInput {
		return "", errBodyTooLarge
	}
	return string(b), nil
}
