package policyfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/domain"
)

const (
	agentUID = 501
	rootUID  = 0
)

// fakeFS answers lstat/fstat from a table; unlisted paths are root-owned
// 0755 directories (so only the entries a test cares about need listing).
type fakeFS struct {
	info map[string]statInfo
	file statInfo // answer for fstat of the opened descriptor
}

func (f fakeFS) lstat(p string) (statInfo, error) {
	if si, ok := f.info[p]; ok {
		return si, nil
	}
	return statInfo{UID: rootUID, Perm: 0o755, Kind: kindDir}, nil
}

func (f fakeFS) fstat(*os.File) (statInfo, error) { return f.file, nil }

func (f fakeFS) opts(euid uint32, extra ...Option) []Option {
	return append([]Option{withTrustEnv(f.lstat, f.fstat, euid)}, extra...)
}

func rootFile() statInfo { return statInfo{UID: rootUID, Perm: 0o644, Kind: kindRegular} }

func realPolicy(t *testing.T) (path string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "outlook.policy.yaml")
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantDenied(t *testing.T, err error, substr string) {
	t.Helper()
	var de *domain.Error
	if !errors.As(err, &de) || de.Cat != output.CategoryPolicyDenied {
		t.Fatalf("want policy_denied, got %v", err)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not mention %q", err, substr)
	}
}

// FR-R2: root-owned 0644 file in root-owned 0755 dirs is accepted.
func TestFRR2RootOwnedAccepted(t *testing.T) {
	p := realPolicy(t)
	fs := fakeFS{file: rootFile()}
	got, err := Load(p, fs.opts(agentUID)...)
	if err != nil || got.Mailbox != "a@corp.example.com" {
		t.Fatalf("%v %+v", err, got)
	}
}

// FR-R2: ownership, not mode bits. A file owned by the agent is refused even
// when it is mode 0444 in an agent-owned 0555 directory (the evasion probe).
func TestFRR2AgentOwnedReadOnlyRefusedRegardlessOfMode(t *testing.T) {
	p := realPolicy(t)
	fs := fakeFS{
		file: statInfo{UID: agentUID, Perm: 0o444, Kind: kindRegular},
		info: map[string]statInfo{filepath.Dir(p): {UID: agentUID, Perm: 0o555, Kind: kindDir}},
	}
	_, err := Load(p, fs.opts(agentUID)...)
	wantDenied(t, err, "not owned by a trusted")
}

func TestFRR2AgentOwnedFileInRootDirRefused(t *testing.T) {
	p := realPolicy(t)
	fs := fakeFS{file: statInfo{UID: agentUID, Perm: 0o444, Kind: kindRegular}}
	_, err := Load(p, fs.opts(agentUID)...)
	wantDenied(t, err, "file")
}

// FR-R2: an ancestor writable by the agent uid (by ownership or by mode) is refused.
func TestFRR2AncestorOwnedByAgentRefused(t *testing.T) {
	p := realPolicy(t)
	anc := filepath.Dir(filepath.Dir(p))
	fs := fakeFS{file: rootFile(), info: map[string]statInfo{anc: {UID: agentUID, Perm: 0o755, Kind: kindDir}}}
	_, err := Load(p, fs.opts(agentUID)...)
	wantDenied(t, err, anc)
}

func TestFRR2GroupOrWorldWritableAncestorRefused(t *testing.T) {
	for _, perm := range []os.FileMode{0o775, 0o757, 0o777} {
		p := realPolicy(t)
		fs := fakeFS{file: rootFile(), info: map[string]statInfo{filepath.Dir(p): {UID: rootUID, Perm: perm, Kind: kindDir}}}
		_, err := Load(p, fs.opts(agentUID)...)
		wantDenied(t, err, "writable by group or others")
	}
}

func TestFRR2GroupWritableFileRefused(t *testing.T) {
	p := realPolicy(t)
	fs := fakeFS{file: statInfo{UID: rootUID, Perm: 0o664, Kind: kindRegular}}
	_, err := Load(p, fs.opts(agentUID)...)
	wantDenied(t, err, "writable by group or others")
}

func TestFRR2NonRegularFileRefused(t *testing.T) {
	p := realPolicy(t)
	fs := fakeFS{file: statInfo{UID: rootUID, Perm: 0o644, Kind: kindOther}}
	_, err := Load(p, fs.opts(agentUID)...)
	wantDenied(t, err, "regular file")
}

// FR-R2: a configured trusted uid other than the effective uid is accepted;
// the effective uid is never trusted even if configured.
func TestFRR2ConfiguredTrustedUID(t *testing.T) {
	p := realPolicy(t)
	fs := fakeFS{file: statInfo{UID: 900, Perm: 0o644, Kind: kindRegular}}
	if _, err := Load(p, fs.opts(agentUID, WithTrustedUIDs(900))...); err != nil {
		t.Fatalf("configured uid should be trusted: %v", err)
	}
	_, err := Load(p, fs.opts(900, WithTrustedUIDs(900))...)
	wantDenied(t, err, "not owned by a trusted")
}

func TestFRR2RootEffectiveUIDDistrustsRoot(t *testing.T) {
	p := realPolicy(t)
	fs := fakeFS{file: rootFile()}
	_, err := Load(p, fs.opts(rootUID)...)
	wantDenied(t, err, "not owned by a trusted")
}

// FR-R2: a symlink owned by the agent is refused; a root-owned one is followed
// and the target and its directories are checked.
func TestFRR2SymlinkOwnedByAgentRefused(t *testing.T) {
	target := realPolicy(t)
	link := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	fs := fakeFS{file: rootFile(), info: map[string]statInfo{link: {UID: agentUID, Perm: 0o777, Kind: kindSymlink}}}
	_, err := Load(link, fs.opts(agentUID)...)
	wantDenied(t, err, link)
}

func TestFRR2SymlinkTargetDirectoriesChecked(t *testing.T) {
	target := realPolicy(t)
	link := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	fs := fakeFS{file: rootFile(), info: map[string]statInfo{
		link:                 {UID: rootUID, Perm: 0o777, Kind: kindSymlink},
		filepath.Dir(target): {UID: agentUID, Perm: 0o755, Kind: kindDir},
	}}
	_, err := Load(link, fs.opts(agentUID)...)
	wantDenied(t, err, filepath.Dir(target))

	fs.info[filepath.Dir(target)] = statInfo{UID: rootUID, Perm: 0o755, Kind: kindDir}
	if _, err := Load(link, fs.opts(agentUID)...); err != nil {
		t.Fatalf("root-owned link to root-owned target must load: %v", err)
	}
}

func TestFRR2StatErrorsFailClosed(t *testing.T) {
	p := realPolicy(t)
	boom := errors.New("boom")
	_, err := Load(p, withTrustEnv(func(string) (statInfo, error) { return statInfo{}, boom }, nil, agentUID))
	wantDenied(t, err, "cannot check")
	_, err = Load(p, withTrustEnv(fakeFS{}.lstat, func(*os.File) (statInfo, error) { return statInfo{}, boom }, agentUID))
	wantDenied(t, err, "cannot check")
}

func TestFRR2MissingFileIsValidationNotDenied(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("missing file must fail")
	}
}

// FR-R2: the content is read from the descriptor that was verified, so a
// swap after the check cannot change what is parsed.
func TestFRR2ContentComesFromVerifiedDescriptor(t *testing.T) {
	p := realPolicy(t)
	evil := strings.Replace(valid, "a@corp.example.com", "evil@corp.example.com", 1)
	fs := fakeFS{file: rootFile()}
	fstat := func(f *os.File) (statInfo, error) {
		// Swap the path after the descriptor is open; the open fd still
		// refers to the original inode.
		_ = os.Remove(p)
		_ = os.WriteFile(p, []byte(evil), 0o644)
		return fs.fstat(f)
	}
	got, err := Load(p, withTrustEnv(fs.lstat, fstat, agentUID))
	if err != nil || got.Mailbox != "a@corp.example.com" {
		t.Fatalf("%v %q", err, got.Mailbox)
	}
}

// FR-R2: the real stat path. A policy in a temp dir is owned by the test user,
// so the production check refuses it (skipped when running as root).
func TestFRR2RealStatRefusesAgentOwned(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	p := realPolicy(t)
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	wantDenied(t, err, "not owned by a trusted")
}

func TestFRR2RealLstatFstat(t *testing.T) {
	p := realPolicy(t)
	si, err := realLstat(p)
	if err != nil || si.Kind != kindRegular || si.UID != uint32(os.Geteuid()) {
		t.Fatalf("%+v %v", si, err)
	}
	d, _ := realLstat(filepath.Dir(p))
	if d.Kind != kindDir {
		t.Fatalf("dir kind %v", d.Kind)
	}
	l := filepath.Join(t.TempDir(), "l")
	_ = os.Symlink(p, l)
	if s, _ := realLstat(l); s.Kind != kindSymlink {
		t.Fatalf("symlink kind %v", s.Kind)
	}
	if _, err := realLstat(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing must error")
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if s, err := realFstat(f); err != nil || s.Kind != kindRegular {
		t.Fatalf("%+v %v", s, err)
	}
	_ = f.Close()
	if _, err := realFstat(f); err == nil {
		t.Fatal("closed fd must error")
	}
}

func TestFRR2OpenNoFollowRefusesSymlink(t *testing.T) {
	p := realPolicy(t)
	l := filepath.Join(t.TempDir(), "l")
	if err := os.Symlink(p, l); err != nil {
		t.Fatal(err)
	}
	if f, err := openNoFollow(l); err == nil {
		_ = f.Close()
		t.Fatal("symlink must not be followed")
	}
	f, err := openNoFollow(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}
