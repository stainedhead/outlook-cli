package cli

import (
	"flag"
	"io"
	"strings"
	"time"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// maxBodyInput bounds --body-file / stdin so a runaway pipe cannot exhaust memory.
const maxBodyInput = 4 << 20

func readAll(r io.Reader) ([]byte, error) { return io.ReadAll(io.LimitReader(r, maxBodyInput)) }

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
		if err != nil {
			return "", domain.NewUsage("cannot read body from stdin")
		}
		return string(b), nil
	}
	b, err := r.ReadFile(file)
	if err != nil {
		return "", domain.NewUsage("cannot read --body-file")
	}
	return string(b), nil
}
