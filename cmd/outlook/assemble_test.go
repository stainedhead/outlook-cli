package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
)

// FR-R14: assemble failure paths. Each fails closed with a stable exit code
// and nothing reaches Graph.

func TestFRR14AssembleBadPolicyExit9(t *testing.T) {
	e := newITEnv(t, func(p string) string { return strings.Replace(p, "profile: agent", "profile: [", 1) })
	if _, err := assemble(context.Background(), e.cfg); output.ExitOf(err) != 9 {
		t.Fatalf("got %v exit %d", err, output.ExitOf(err))
	}
}

func TestFRR14AssembleUnwritableAuditPath(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e := newITEnv(t, func(p string) string {
		return strings.ReplaceAll(p, e0audit(p), filepath.Join(blocker, "sub", "audit.jsonl"))
	})
	if _, err := assemble(context.Background(), e.cfg); err == nil {
		t.Fatal("unwritable audit path must fail assemble")
	}
	e.run("whoami").wantExit(t, 1)
}

// e0audit extracts the audit path value from the policy text.
func e0audit(p string) string {
	i := strings.Index(p, "audit: { path: ")
	rest := p[i+len("audit: { path: "):]
	return rest[:strings.Index(rest, " }")]
}

func TestFRR14AssembleUnknownFilterRejected(t *testing.T) {
	e := newITEnv(t, func(p string) string {
		return strings.Replace(p, "content_filters: [secret_patterns, classification_markers]", "content_filters: [nope]", 1)
	})
	_, err := assemble(context.Background(), e.cfg)
	if err == nil {
		t.Fatal("unknown content filter must fail")
	}
}

func TestFRR14AssembleBadGraphBaseURL(t *testing.T) {
	e := newITEnv(t, nil)
	e.cfg.GraphBaseURL = "://nope"
	_, err := assemble(context.Background(), e.cfg)
	if output.ExitOf(err) != 2 {
		t.Fatalf("got %v exit %d", err, output.ExitOf(err))
	}
}

func TestFRR14AssembleNilDaemonFails(t *testing.T) {
	e := newITEnv(t, nil)
	e.cfg.Daemon = nil
	if _, err := assemble(context.Background(), e.cfg); err == nil {
		t.Fatal("a nil credential client must fail")
	}
}

func TestFRR14AssembleDefaultsAndLedgerNextToAudit(t *testing.T) {
	e := newITEnv(t, nil)
	e.cfg.AgentID, e.cfg.RunID, e.cfg.Clock = "", "", nil
	a, err := assemble(context.Background(), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	r1, r2 := newRunID(), newRunID()
	if !strings.HasPrefix(r1, "run-") || r1 == r2 {
		t.Error("run ids must be unique run-* values")
	}
	if got := pageKeyPath(filepath.Join("/x", ledgerFileName)); got != filepath.Join("/x", pageKeyFileName) {
		t.Errorf("page key path %s", got)
	}
}

func TestFRR14SelftestForFailsOnBadPolicy(t *testing.T) {
	e := newITEnv(t, nil)
	e.cfg.PolicyPath += ".missing"
	if _, err := selftestFor(e.cfg)(context.Background()); output.ExitOf(err) != 9 {
		t.Fatalf("got %v", err)
	}
	e.run("selftest").wantExit(t, 9)
}

// FR-R2: OUTLOOK_POLICY cannot defeat the ownership check. The target is an
// agent-owned, read-only file in an agent-owned, read-only directory.
func TestFRR2EnvPolicyAgentOwnedReadOnlyRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	if devBuild {
		t.Skip("release behaviour only")
	}
	e := newITEnv(t, nil)
	dir := filepath.Dir(e.cfg.PolicyPath)
	if err := os.Chmod(e.cfg.PolicyPath, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	t.Setenv(envPolicy, e.cfg.PolicyPath)
	cfg := prodConfig()
	if cfg.PolicyPath != e.cfg.PolicyPath {
		t.Fatalf("OUTLOOK_POLICY not honoured as a path: %s", cfg.PolicyPath)
	}
	if len(cfg.PolicyOpts) != 0 {
		t.Fatal("a release build must not carry any policy override option")
	}
	cfg.Daemon = e.cfg.Daemon
	_, err := assemble(context.Background(), cfg)
	if output.ExitOf(err) != 6 {
		t.Fatalf("want policy_denied exit 6, got %v (exit %d)", err, output.ExitOf(err))
	}
}

// FR-R2: the insecure-override variable has no effect in a release build.
func TestFRR2ReleaseIgnoresInsecureEnv(t *testing.T) {
	if devBuild {
		t.Skip("release behaviour only")
	}
	t.Setenv("OUTLOOK_POLICY_INSECURE", "1")
	if opts := devPolicyOpts(os.Getenv); len(opts) != 0 {
		t.Fatal("release build must ignore OUTLOOK_POLICY_INSECURE")
	}
	if prodConfig().PolicyOpts != nil {
		t.Fatal("prodConfig must carry no override in a release build")
	}
}

// FR-R12: with download allowed the quarantine store is built through
// NewQuarantine; the assembled graph is usable.
func TestFRR12AssembleBuildsQuarantineWhenDownloadAllowed(t *testing.T) {
	out := t.TempDir()
	e := newITEnv(t, func(p string) string {
		return strings.Replace(p, "attachments: { download: deny }",
			"attachments: { download: allow, allow_types: [pdf], max_bytes: 1000, out_dir: "+out+" }", 1)
	})
	a, err := assemble(context.Background(), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	a.close()
}

// FR-R13: a ledger directory that is not private (0700) stops assemble.
func TestFRR13AssembleRefusesSharedLedgerDir(t *testing.T) {
	e := newITEnv(t, nil)
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	e.cfg.LedgerPath = filepath.Join(dir, ledgerFileName)
	if _, err := assemble(context.Background(), e.cfg); err == nil {
		t.Fatal("a 0755 ledger directory must be refused")
	}
}
