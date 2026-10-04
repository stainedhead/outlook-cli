package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

func TestMisconfiguredDepsFailGeneral(t *testing.T) {
	base := newEnv(t).deps
	muts := map[string]func(*Deps){
		"Reader": func(d *Deps) { d.Reader = nil }, "Writer": func(d *Deps) { d.Writer = nil },
		"Ledger": func(d *Deps) { d.Ledger = nil }, "Policy": func(d *Deps) { d.Policy = nil },
		"Audit": func(d *Deps) { d.Audit = nil }, "Clock": func(d *Deps) { d.Clock = nil },
	}
	for name, m := range muts {
		d := base
		m(&d)
		_, err := New(d).Whoami(context.Background())
		mustCat(t, err, 1)
		if err == nil || !contains(err.Error(), name) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestMailboxCheckOncePerRun(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 3; i++ {
		if _, err := e.cmds.ListFolders(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if e.r.meCalls != 1 || e.pol.calls != 1 {
		t.Errorf("Me=%d Policy=%d, want 1 each", e.r.meCalls, e.pol.calls)
	}
}

func TestMailboxMismatchRefusesEveryCommand(t *testing.T) {
	e := newEnv(t)
	e.r.profile = domain.Profile{Mail: "other@corp.example.com", UserPrincipalName: "other@corp.example.com"}
	ctx := context.Background()
	_, err := e.cmds.Whoami(ctx)
	mustCat(t, err, 6)
	if ruleOf(err) != domain.RuleMailbox {
		t.Errorf("rule = %q", ruleOf(err))
	}
	if _, err = e.cmds.Send(ctx, SendRequest{To: []string{"a@corp.example.com"}, Subject: "s", Body: "b"}); exitOf(err) != 6 {
		t.Errorf("send exit %d", exitOf(err))
	}
	if len(e.w.sent) != 0 {
		t.Error("sent despite mismatch")
	}
	// Failure is not cached as success: a corrected mailbox works.
	e.r.profile = domain.Profile{Mail: mailbox}
	if _, err = e.cmds.Whoami(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyAndMeErrorsPropagate(t *testing.T) {
	e := newEnv(t)
	e.pol.err = domain.NewPolicyDenied("policy file is agent-writable")
	_, err := e.cmds.Whoami(context.Background())
	mustCat(t, err, 6)
	if got := e.lastAudit(t); got.Outcome != "denied" {
		t.Errorf("audit = %+v", got)
	}
	e = newEnv(t)
	e.r.meErr = domain.NewAuth("daemon unavailable")
	_, err = e.cmds.Whoami(context.Background())
	mustCat(t, err, 3)
	if got := e.lastAudit(t); got.Outcome != "error" || got.PolicyDecision != "allow" {
		t.Errorf("audit = %+v", got)
	}
}

func TestAuditOneEntryPerCommandAndBlockMode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.cmds.Whoami(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.cmds.ListFolders(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.a.entries) != 2 {
		t.Fatalf("entries = %d", len(e.a.entries))
	}
	if a := e.a.entries[0]; a.Verb != domain.VerbRead || a.Resource != "whoami" || a.Outcome != "ok" || a.PolicyDecision != "allow" {
		t.Errorf("entry = %+v", a)
	}
	e.a.err = errors.New("audit disk full")
	if _, err := e.cmds.Whoami(ctx); err == nil {
		t.Error("block-mode audit failure must fail the command")
	}
}

func TestAuditDurationUsesClock(t *testing.T) {
	e := newEnv(t)
	if _, err := e.cmds.Whoami(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.lastAudit(t).Duration != 0 {
		t.Error("fake clock does not advance")
	}
}

func TestWhoami(t *testing.T) {
	e := newEnv(t)
	got, err := e.cmds.Whoami(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Mailbox != mailbox || got.AgentID != "rev-01" || got.RunID != "run-7" || got.Profile != "agent" ||
		got.SendMode != domain.SendAllow || got.External != domain.ExternalDeny || got.MaxTotal != 5 ||
		got.Rate.PerHour != 3 || got.Limits.MaxResults != 10 || got.Limits.MaxWritesPerRun != 20 {
		t.Errorf("whoami = %+v", got)
	}
}

func TestWhoamiDefaultsAreDeny(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Mode = ""; p.Send.Recipients.External = "" })
	got, err := e.cmds.Whoami(context.Background())
	if err != nil || got.SendMode != domain.SendDeny || got.External != domain.ExternalDeny {
		t.Errorf("whoami = %+v, %v", got, err)
	}
}

func TestAuditCarriesUpstreamHTTPStatus(t *testing.T) {
	e := newEnv(t)
	e.r.listErr = domain.NewGeneral("boom").WithHTTPStatus(502)
	_, _ = e.cmds.ListFolders(context.Background())
	e.r.listErr = domain.WithUpstreamStatus(domain.NewForbidden("no"), 403)
	_, _ = e.cmds.ListFolders(context.Background())
	var got []int
	for _, a := range e.a.entries {
		got = append(got, a.HTTPStatus)
	}
	if len(got) < 2 || got[len(got)-2] != 502 || got[len(got)-1] != 403 {
		t.Fatalf("audit statuses = %v", got)
	}
}
