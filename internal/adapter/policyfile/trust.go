package policyfile

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// FR-R2: the policy file is the agent's guardrail, so it must be provably
// authored by someone other than the agent. The test is ownership, not mode
// bits: the agent can chmod its own files read-only, but it cannot make a
// file root-owned. The file and every directory above it (and every symlink
// on the way) must be owned by root or by a configured uid other than the
// effective uid, and must not be writable by group or others. The content is
// read from the descriptor that was verified with fstat, so there is no
// check-then-read race.

type kind int

const (
	kindOther kind = iota
	kindRegular
	kindDir
	kindSymlink
)

// statInfo is the part of a stat result the trust check needs.
type statInfo struct {
	UID  uint32
	Perm fs.FileMode // permission bits only
	Kind kind
}

type trustEnv struct {
	lstat func(string) (statInfo, error)
	fstat func(*os.File) (statInfo, error)
	euid  uint32
}

const installHint = "install the policy as root, for example: " +
	"sudo install -d -o root -m 0755 /etc/agent-cli && " +
	"sudo install -o root -m 0644 outlook.policy.yaml /etc/agent-cli/outlook.policy.yaml"

func deny(msg string, cause error) error {
	e := domain.NewPolicyDenied(msg).WithHint(installHint)
	if cause != nil {
		e = e.WithCause(cause)
	}
	return e
}

func (c *loadConfig) trusted(uid uint32) bool {
	if uid == c.env.euid {
		return false // the agent's own uid never vouches for itself
	}
	return uid == 0 || c.trustedUIDs[uid]
}

// checkInfo applies the ownership and mode rules to one stat result.
func (c *loadConfig) checkInfo(what, path string, si statInfo) error {
	if !c.trusted(si.UID) {
		return deny("policy "+what+" "+path+" is not owned by a trusted account (root)", nil)
	}
	// A symlink's own mode is meaningless on most systems; its owner is what
	// decides who could repoint it.
	if si.Kind != kindSymlink && si.Perm&0o022 != 0 {
		return deny("policy "+what+" "+path+" is writable by group or others", nil)
	}
	return nil
}

// ancestors lists p and every parent directory up to the root, root first.
func ancestors(p string) []string {
	var out []string
	for {
		out = append([]string{p}, out...)
		parent := filepath.Dir(p)
		if parent == p {
			return out
		}
		p = parent
	}
}

// checkChain verifies every component of path (not following symlinks).
func (c *loadConfig) checkChain(path string, seen map[string]bool) error {
	for _, p := range ancestors(path) {
		if seen[p] {
			continue
		}
		seen[p] = true
		si, err := c.env.lstat(p)
		if err != nil {
			return deny("policy: cannot check ownership of "+p, err)
		}
		what := "directory"
		switch {
		case p == path && si.Kind != kindDir:
			what = "file"
		case si.Kind == kindSymlink:
			what = "symlink"
		}
		if err := c.checkInfo(what, p, si); err != nil {
			return err
		}
	}
	return nil
}

// openTrusted checks the lexical path and its symlink-resolved target, then
// opens the target with O_NOFOLLOW and verifies the open descriptor.
func (c *loadConfig) openTrusted(path string) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, deny("policy: cannot resolve "+path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, domain.NewValidation("policy: cannot read " + path).WithHint(hint).WithCause(err)
	}
	seen := map[string]bool{}
	if err := c.checkChain(abs, seen); err != nil {
		return nil, err
	}
	if err := c.checkChain(resolved, seen); err != nil {
		return nil, err
	}
	f, err := openNoFollow(resolved)
	if err != nil {
		return nil, domain.NewValidation("policy: cannot read " + path).WithHint(hint).WithCause(err)
	}
	si, err := c.env.fstat(f)
	if err != nil {
		_ = f.Close()
		return nil, deny("policy: cannot check ownership of "+resolved, err)
	}
	if si.Kind != kindRegular {
		_ = f.Close()
		return nil, deny("policy "+resolved+" is not a regular file", nil)
	}
	if err := c.checkInfo("file", resolved, si); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
