package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

func okSend() SendRequest {
	return SendRequest{To: []string{"jane@corp.example.com"}, Subject: "Build 4812", Body: "All green."}
}

func TestSendHappyPathRendersPrefixFooterHeaders(t *testing.T) {
	e := newEnv(t)
	res, err := e.cmds.Send(context.Background(), okSend())
	if err != nil {
		t.Fatal(err)
	}
	if len(e.w.sent) != 1 || res.DryRun || res.AlreadySent {
		t.Fatalf("sent=%d res=%+v", len(e.w.sent), res)
	}
	m := e.w.sent[0]
	if m.Subject != "[agent] Build 4812" {
		t.Errorf("subject = %q", m.Subject)
	}
	if !strings.HasSuffix(m.Body, "-- \nAutomated message from agent rev-01. A human owns decisions.\n") || !strings.HasPrefix(m.Body, "All green.") {
		t.Errorf("body = %q", m.Body)
	}
	hs := map[string]string{}
	for _, h := range m.InternetHeaders {
		hs[h.Name] = h.Value
	}
	if hs[domain.HeaderAgentID] != "rev-01" || hs[domain.HeaderAgentRun] != "run-7" || len(hs) != 2 {
		t.Errorf("headers = %v", hs)
	}
	if a := e.lastAudit(t); a.Verb != domain.VerbSend || a.Resource != "mail.send" || a.Outcome != "ok" || a.PolicyDecision != "allow" {
		t.Errorf("audit = %+v", a)
	}
	if res.Rendered.Subject != m.Subject {
		t.Error("result must carry the rendered message")
	}
}

func TestSendDryRunSendsNothing(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.DryRun = true
	req.IdempotencyKey = "k1"
	res, err := e.cmds.Send(context.Background(), req)
	if err != nil || !res.DryRun || res.Rendered.Subject != "[agent] Build 4812" || res.IdempotencyKey != "k1" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if e.w.total() != 0 || len(e.l.entries) != 0 {
		t.Error("dry run must not write or reserve")
	}
	var found bool
	for _, h := range res.Rendered.InternetHeaders {
		found = found || h.Name == domain.HeaderIdempotencyKey && h.Value == "k1"
	}
	if !found {
		t.Error("idempotency header missing from the rendering")
	}
	if a := e.lastAudit(t); a.Outcome != "dry_run" {
		t.Errorf("audit = %+v", a)
	}
}

func TestSendDryRunStillEnforcesPolicy(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.DryRun = true
	req.To = []string{"x@evil.com"}
	_, err := e.cmds.Send(context.Background(), req)
	mustCat(t, err, 6)
	if ruleOf(err) != domain.RuleSendExternal {
		t.Errorf("rule %q", ruleOf(err))
	}
}

func TestSendModeDryRunOnly(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Mode = domain.SendDryRunOnly })
	res, err := e.cmds.Send(context.Background(), okSend())
	if err != nil || !res.DryRun || e.w.total() != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if a := e.lastAudit(t); a.Outcome != "dry_run" || a.PolicyDecision != "dry_run_only:send.mode" {
		t.Errorf("audit = %+v", a)
	}
}

func TestSendDenials(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*domain.Policy)
		req  func(*SendRequest)
		exit int
		rule string
	}{
		{"mode deny", func(p *domain.Policy) { p.Send.Mode = domain.SendDeny }, nil, 6, domain.RuleSendMode},
		{"external", nil, func(r *SendRequest) { r.To = []string{"x@evil.com"} }, 6, domain.RuleSendExternal},
		{"case-variant external", nil, func(r *SendRequest) { r.To = []string{"X@EVIL.com"} }, 6, domain.RuleSendExternal},
		{"bcc", nil, func(r *SendRequest) { r.Bcc = []string{"b@corp.example.com"} }, 6, domain.RuleSendBcc},
		{"bcc duplicate of to still refused", nil, func(r *SendRequest) { r.Bcc = []string{"jane@corp.example.com"} }, 6, domain.RuleSendBcc},
		{"attachments", nil, func(r *SendRequest) { r.HasAttachments = true }, 6, domain.RuleSendAttachments},
		{"max total", func(p *domain.Policy) { p.Send.Recipients.MaxTotal = 1 }, func(r *SendRequest) { r.Cc = []string{"b@corp.example.com"} }, 6, domain.RuleSendMaxTotal},
		{"body size", nil, func(r *SendRequest) { r.Body = strings.Repeat("x", 501) }, 6, domain.RuleSendBodyMax},
		{"secret filter", nil, func(r *SendRequest) { r.Body = "key AKIAABCDEF" }, 6, "send.content_filter.secret_patterns"},
		{"secret in subject", nil, func(r *SendRequest) { r.Subject = "AKIA" }, 6, "send.content_filter.secret_patterns"},
		{"filter not wired", func(p *domain.Policy) { p.Send.ContentFilters = []string{"classification_markers"} }, nil, 6, "send.content_filter.classification_markers"},
		{"invalid recipient", nil, func(r *SendRequest) { r.To = []string{"nope"} }, 9, ""},
		{"crlf recipient", nil, func(r *SendRequest) { r.To = []string{"a@corp.example.com\r\nBcc: x@evil.com"} }, 9, ""},
		{"no recipient", nil, func(r *SendRequest) { r.To = nil }, 9, ""},
		{"crlf subject", nil, func(r *SendRequest) { r.Subject = "hi\r\nBcc: x@evil.com" }, 9, ""},
		{"empty subject", nil, func(r *SendRequest) { r.Subject = "" }, 9, ""},
		{"empty body", nil, func(r *SendRequest) { r.Body = "  " }, 9, ""},
		{"bad key", nil, func(r *SendRequest) { r.IdempotencyKey = "a b" }, 9, ""},
		{"attachments allowed but unsupported", func(p *domain.Policy) { p.Send.Attachments = true }, func(r *SendRequest) { r.HasAttachments = true }, 9, ""},
	}
	for _, c := range cases {
		e := newEnv(t, func(p *domain.Policy) {
			if c.mut != nil {
				c.mut(p)
			}
		})
		req := okSend()
		if c.req != nil {
			c.req(&req)
		}
		res, err := e.cmds.Send(context.Background(), req)
		if exitOf(err) != c.exit || (c.rule != "" && ruleOf(err) != c.rule) {
			t.Errorf("%s: exit=%d rule=%q err=%v", c.name, exitOf(err), ruleOf(err), err)
		}
		if e.w.total() != 0 || res.Rendered.Subject != "" {
			t.Errorf("%s: wrote despite denial", c.name)
		}
	}
}

func TestSendFilterFindingNeverEchoesMatchedText(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.Body = "token AKIAS3CR3TVALUE123"
	_, err := e.cmds.Send(context.Background(), req)
	if err == nil || strings.Contains(err.Error(), "AKIAS3CR3T") || !strings.Contains(err.Error(), "aws_key") {
		t.Errorf("err = %v", err)
	}
}

func TestSendDedupesBeforeMaxTotal(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Recipients.MaxTotal = 2 })
	req := okSend()
	req.To = []string{"a@corp.example.com", "A@corp.example.com"}
	req.Cc = []string{"a@corp.example.com", "b@corp.example.com"}
	res, err := e.cmds.Send(context.Background(), req)
	if err != nil || len(res.Rendered.To) != 1 || len(res.Rendered.Cc) != 1 {
		t.Errorf("res=%+v err=%v", res.Rendered, err)
	}
}

func TestSendExternalDraftOnlyCreatesDraft(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalDraftOnly })
	req := okSend()
	req.To = []string{"x@partner.com"}
	res, err := e.cmds.Send(context.Background(), req)
	if err != nil || res.DraftID != "draft-1" || len(e.w.sent) != 0 || len(e.w.drafts) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if a := e.lastAudit(t); a.Outcome != "draft" || a.PolicyDecision != "draft_only:send.recipients.external" {
		t.Errorf("audit = %+v", a)
	}
	if len(e.l.entries) != 0 {
		t.Error("a keyless draft needs no ledger entry")
	}
}

func TestSendDraftOnlyWithKeyIsIdempotent(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalDraftOnly })
	req := okSend()
	req.To = []string{"x@partner.com"}
	req.IdempotencyKey = "dk"
	for i := 0; i < 2; i++ {
		res, err := e.cmds.Send(context.Background(), req)
		if err != nil || res.DraftID != "draft-1" || res.AlreadySent || res.AlreadyDrafted != (i == 1) {
			t.Fatalf("call %d: res=%+v err=%v", i, res, err)
		}
	}
	if len(e.w.drafts) != 1 {
		t.Errorf("drafts = %d, want exactly 1", len(e.w.drafts))
	}
}

func TestSendDraftOnlyErrorPaths(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalDraftOnly })
	req := okSend()
	req.To = []string{"x@partner.com"}
	e.w.draftErr = errors.New("boom")
	if _, err := e.cmds.Send(context.Background(), req); err == nil {
		t.Error("draft error swallowed")
	}
	e2 := newEnv(t, func(p *domain.Policy) {
		p.Send.Recipients.External = domain.ExternalDraftOnly
		p.Limits.MaxWritesPerRun = 0
	})
	if _, err := e2.cmds.Send(context.Background(), req); exitOf(err) != 6 {
		t.Errorf("write cap exit %d", exitOf(err))
	}
}

// AC-3: the same idempotency key twice results in exactly one POST.
func TestIdempotencySameKeyOnePost(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.IdempotencyKey = "run-7/step-1"
	r1, err := e.cmds.Send(context.Background(), req)
	if err != nil || r1.AlreadySent {
		t.Fatalf("r1=%+v err=%v", r1, err)
	}
	// Second attempt in a new process (fresh facade), same ledger.
	e.rebuild()
	e.clk.now = e.clk.now.Add(time.Minute)
	r2, err := e.cmds.Send(context.Background(), req)
	if err != nil || !r2.AlreadySent || r2.IdempotencyKey != "run-7/step-1" {
		t.Fatalf("r2=%+v err=%v", r2, err)
	}
	if len(e.w.sent) != 1 {
		t.Fatalf("POSTs = %d, want 1", len(e.w.sent))
	}
	if a := e.lastAudit(t); a.Outcome != "already_sent" {
		t.Errorf("audit = %+v", a)
	}
	if e.l.status("run-7/step-1") != LedgerSent {
		t.Error("ledger status")
	}
}

func TestIdempotencyDifferentRequestSameKeyConflicts(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.IdempotencyKey = "k"
	if _, err := e.cmds.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.Body = "different"
	_, err := e.cmds.Send(context.Background(), req)
	mustCat(t, err, 7)
	if len(e.w.sent) != 1 {
		t.Error("second send went out")
	}
}

func TestIdempotencyAmbiguousFailureLeavesPendingAndFailsClosed(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.IdempotencyKey = "k"
	e.w.sendErr = errors.New("timeout after write")
	if _, err := e.cmds.Send(context.Background(), req); err == nil {
		t.Fatal("expected error")
	}
	if e.l.status("k") != LedgerPending {
		t.Fatalf("status = %q, want pending", e.l.status("k"))
	}
	e.w.sendErr = nil
	_, err := e.cmds.Send(context.Background(), req)
	mustCat(t, err, 7)
	if len(e.w.sent) != 0 {
		t.Error("retry after ambiguous failure must not send")
	}
}

func TestIdempotencyProvableNotSentFreesKey(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.IdempotencyKey = "k"
	e.w.sendErr = domain.NotSent(domain.NewForbidden("403"))
	_, err := e.cmds.Send(context.Background(), req)
	if !errors.Is(err, domain.ErrNotSent) || exitOf(err) != 4 {
		t.Fatalf("err = %v exit %d", err, exitOf(err))
	}
	if e.l.status("k") != LedgerFailed {
		t.Fatalf("status = %q", e.l.status("k"))
	}
	e.w.sendErr = nil
	res, err := e.cmds.Send(context.Background(), req)
	if err != nil || res.AlreadySent || len(e.w.sent) != 1 {
		t.Errorf("retry: res=%+v err=%v sent=%d", res, err, len(e.w.sent))
	}
}

func TestLedgerFailureFailsClosed(t *testing.T) {
	e := newEnv(t)
	e.l.reserveErr = errors.New("corrupt ledger")
	_, err := e.cmds.Send(context.Background(), okSend())
	mustCat(t, err, 1)
	if len(e.w.sent) != 0 {
		t.Error("sent with a broken ledger")
	}
	e.l.reserveErr = nil
	e.l.sentErr = errors.New("unreadable")
	req := okSend()
	req.DryRun = true
	if _, err = e.cmds.Send(context.Background(), req); exitOf(err) != 1 {
		t.Errorf("dry run rate read failure: %v", err)
	}
}

func TestLedgerSentSinceSecondCallFailure(t *testing.T) {
	e := newEnv(t)
	e.deps.Ledger = &flakyLedger{fakeLedger: e.l, okCalls: 1}
	e.rebuild()
	req := okSend()
	req.DryRun = true
	_, err := e.cmds.Send(context.Background(), req)
	mustCat(t, err, 1)
	if len(e.w.sent) != 0 {
		t.Error("sent")
	}
}

type flakyLedger struct {
	*fakeLedger
	okCalls int
}

func (f *flakyLedger) SentSince(ctx context.Context, since time.Time) (int, error) {
	if f.okCalls <= 0 {
		return 0, errors.New("flaky")
	}
	f.okCalls--
	return f.fakeLedger.SentSince(ctx, since)
}

func TestCompleteFailureDoesNotTurnSendIntoError(t *testing.T) {
	e := newEnv(t)
	e.l.completeErr = errors.New("disk full")
	req := okSend()
	req.IdempotencyKey = "k"
	res, err := e.cmds.Send(context.Background(), req)
	if err != nil || len(e.w.sent) != 1 || res.AlreadySent {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// ...but the key stays pending, so a retry cannot double-send.
	e.l.completeErr = nil
	_, err = e.cmds.Send(context.Background(), req)
	mustCat(t, err, 7)
	if len(e.w.sent) != 1 {
		t.Error("double send")
	}
}

func TestKeylessSendsAreRecordedForRateCaps(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Rate = domain.SendRate{PerHour: 2, PerDay: 10} })
	for i := 0; i < 2; i++ {
		req := okSend()
		req.Body = "msg " + string(rune('a'+i))
		if _, err := e.cmds.Send(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.l.entries) != 2 {
		t.Fatalf("ledger entries = %d", len(e.l.entries))
	}
	_, err := e.cmds.Send(context.Background(), okSend())
	mustCat(t, err, 8)
	if ruleOf(err) != domain.RuleSendRateHour {
		t.Errorf("rule %q", ruleOf(err))
	}
	if len(e.w.sent) != 2 {
		t.Errorf("sent = %d", len(e.w.sent))
	}
	if a := e.lastAudit(t); a.Outcome != "denied" || a.PolicyDecision != "deny:send.rate.per_hour" {
		t.Errorf("audit = %+v", a)
	}
}

func TestRateWindowsSlideWithFakeClock(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Rate = domain.SendRate{PerHour: 1, PerDay: 2} })
	send := func(body string) error {
		req := okSend()
		req.Body = body
		_, err := e.cmds.Send(context.Background(), req)
		return err
	}
	if err := send("1"); err != nil {
		t.Fatal(err)
	}
	if err := send("2"); exitOf(err) != 8 {
		t.Fatalf("second within hour: %v", err)
	}
	e.clk.now = e.clk.now.Add(61 * time.Minute)
	if err := send("2"); err != nil {
		t.Fatalf("after the hour: %v", err)
	}
	e.clk.now = e.clk.now.Add(61 * time.Minute)
	err := send("3")
	mustCat(t, err, 8)
	if ruleOf(err) != domain.RuleSendRateDay {
		t.Errorf("rule %q", ruleOf(err))
	}
	e.clk.now = e.clk.now.Add(25 * time.Hour)
	if err := send("3"); err != nil {
		t.Fatalf("after the day: %v", err)
	}
}

func TestRateDenialFreesReservedKey(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Rate = domain.SendRate{PerHour: 1} })
	if _, err := e.cmds.Send(context.Background(), okSend()); err != nil {
		t.Fatal(err)
	}
	req := okSend()
	req.Body = "other"
	req.IdempotencyKey = "k2"
	if _, err := e.cmds.Send(context.Background(), req); exitOf(err) != 8 {
		t.Fatal("expected rate limit")
	}
	// FR-R8: the over-cap check happens before any reservation, so nothing is
	// written for the key and it can be retried later.
	if st := e.l.status("k2"); st != "" {
		t.Errorf("status = %q, want no entry", st)
	}
}

func TestReplayIsNotRateLimited(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Rate = domain.SendRate{PerHour: 1} })
	req := okSend()
	req.IdempotencyKey = "k"
	if _, err := e.cmds.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	res, err := e.cmds.Send(context.Background(), req)
	if err != nil || !res.AlreadySent {
		t.Errorf("replay: res=%+v err=%v", res, err)
	}
}

func TestDryRunRateCap(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Rate = domain.SendRate{PerHour: 1} })
	if _, err := e.cmds.Send(context.Background(), okSend()); err != nil {
		t.Fatal(err)
	}
	req := okSend()
	req.Body = "other"
	req.DryRun = true
	if _, err := e.cmds.Send(context.Background(), req); exitOf(err) != 8 {
		t.Errorf("dry run must predict rate denial: %v", err)
	}
}

func TestWriteCapPerRun(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Limits.MaxWritesPerRun = 2; p.Send.Rate = domain.SendRate{} })
	for i := 0; i < 2; i++ {
		req := okSend()
		req.Body = string(rune('a' + i))
		if _, err := e.cmds.Send(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	req := okSend()
	req.Body = "third"
	req.IdempotencyKey = "third"
	_, err := e.cmds.Send(context.Background(), req)
	mustCat(t, err, 6)
	if ruleOf(err) != domain.RuleWritesPerRun || e.l.status("third") != LedgerFailed {
		t.Errorf("rule %q status %q", ruleOf(err), e.l.status("third"))
	}
	// A new process run starts a new count.
	e.rebuild()
	if _, err = e.cmds.Send(context.Background(), req); err != nil {
		t.Errorf("new run: %v", err)
	}
}

func TestProbeFindsEarlierSend(t *testing.T) {
	e := newEnv(t)
	e.probe.found = true
	req := okSend()
	req.IdempotencyKey = "k"
	res, err := e.cmds.Send(context.Background(), req)
	if err != nil || !res.AlreadySent || len(e.w.sent) != 0 || e.l.status("k") != LedgerSent {
		t.Errorf("res=%+v err=%v status=%q", res, err, e.l.status("k"))
	}
	if len(e.probe.keys) != 1 || e.probe.keys[0] != "k" {
		t.Errorf("probe keys %v", e.probe.keys)
	}
}

func TestProbeNotFoundAndKeylessSkipsProbe(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.IdempotencyKey = "k"
	if _, err := e.cmds.Send(context.Background(), req); err != nil || len(e.w.sent) != 1 {
		t.Fatalf("err=%v", err)
	}
	if _, err := e.cmds.Send(context.Background(), SendRequest{To: req.To, Subject: "s", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if len(e.probe.keys) != 1 {
		t.Errorf("probe consulted for keyless send: %v", e.probe.keys)
	}
}

func TestProbeErrorFailsClosedAndFreesKey(t *testing.T) {
	e := newEnv(t)
	e.probe.err = domain.NewGeneral("graph down")
	req := okSend()
	req.IdempotencyKey = "k"
	_, err := e.cmds.Send(context.Background(), req)
	mustCat(t, err, 1)
	if len(e.w.sent) != 0 || e.l.status("k") != LedgerFailed {
		t.Errorf("sent=%d status=%q", len(e.w.sent), e.l.status("k"))
	}
}

func TestNilProbeAllowed(t *testing.T) {
	e := newEnv(t)
	e.deps.Probe = nil
	e.rebuild()
	req := okSend()
	req.IdempotencyKey = "k"
	if _, err := e.cmds.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestSendBodyWithInjectionStaysInBody(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.Body = "hello\r\nBcc: evil@x.com\r\n\r\nmore"
	if _, err := e.cmds.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(e.w.sent[0].Bcc) != 0 {
		t.Error("body newlines must not create recipients")
	}
}

func TestSubjectPrefixNotDoubled(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.Subject = "[agent] already"
	res, _ := e.cmds.Send(context.Background(), req)
	if res.Rendered.Subject != "[agent] already" {
		t.Errorf("subject %q", res.Rendered.Subject)
	}
}

// ---- reply ----

func TestReplyToSender(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox", func(m *domain.RawMessage) { m.Subject = "Q3\r\nreport" })
	res, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "Thanks"})
	if err != nil || len(e.w.replies) != 1 || res.DryRun {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	rp := e.w.replies[0]
	if rp.MessageID != "m1" || !strings.HasPrefix(rp.Body, "Thanks") || !strings.Contains(rp.Body, "agent rev-01") {
		t.Errorf("reply = %+v", rp)
	}
	if res.Rendered.Subject != "[agent] Re: Q3 report" || res.Rendered.To[0].Address != "jane@corp.example.com" {
		t.Errorf("rendered = %+v", res.Rendered)
	}
	if a := e.lastAudit(t); a.Resource != "mail.reply" || a.Outcome != "ok" {
		t.Errorf("audit = %+v", a)
	}
}

func TestReplyAllDeniedByDefault(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox")
	_, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "x", All: true})
	mustCat(t, err, 6)
	if ruleOf(err) != domain.RuleSendReplyAll || e.w.total() != 0 {
		t.Errorf("rule %q", ruleOf(err))
	}
	if a := e.lastAudit(t); a.PolicyDecision != "deny:send.reply_all" {
		t.Errorf("audit = %+v", a)
	}
}

func TestReplyAllEnabledByPolicyIsStillUnsupported(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.ReplyAll = true })
	e.addMessage("m1", "f-inbox")
	_, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "x", All: true})
	mustCat(t, err, 9)
}

func TestReplyPolicyChecks(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.addMessage("ext", "f-inbox", func(m *domain.RawMessage) { m.From = domain.Address{Address: "x@evil.com"} })
	e.addMessage("arch", "f-arch")
	e.addMessage("nosender", "f-inbox", func(m *domain.RawMessage) { m.From = domain.Address{} })
	e.addMessage("ok", "f-inbox")
	cases := []struct {
		name string
		req  ReplyRequest
		exit int
	}{
		{"external sender", ReplyRequest{MessageID: "ext", Body: "x"}, 6},
		{"forbidden folder", ReplyRequest{MessageID: "arch", Body: "x"}, 6},
		{"no sender", ReplyRequest{MessageID: "nosender", Body: "x"}, 9},
		{"empty body", ReplyRequest{MessageID: "ok", Body: " "}, 9},
		{"bad key", ReplyRequest{MessageID: "ok", Body: "x", IdempotencyKey: "a b"}, 9},
		{"secret", ReplyRequest{MessageID: "ok", Body: "AKIA1234"}, 6},
		{"missing", ReplyRequest{MessageID: "zzz", Body: "x"}, 5},
		{"empty id", ReplyRequest{Body: "x"}, 2},
	}
	for _, c := range cases {
		if _, err := e.cmds.Reply(ctx, c.req); exitOf(err) != c.exit {
			t.Errorf("%s: exit %d err %v", c.name, exitOf(err), err)
		}
	}
	if e.w.total() != 0 {
		t.Error("something was written")
	}
}

func TestReplyDryRunAndMode(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox")
	res, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "x", DryRun: true})
	if err != nil || !res.DryRun || e.w.total() != 0 {
		t.Errorf("res=%+v err=%v", res, err)
	}
	e2 := newEnv(t, func(p *domain.Policy) { p.Send.Mode = domain.SendDeny })
	e2.addMessage("m1", "f-inbox")
	if _, err = e2.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "x"}); exitOf(err) != 6 {
		t.Errorf("mode deny: %v", err)
	}
}

func TestReplyIdempotent(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox")
	req := ReplyRequest{MessageID: "m1", Body: "ok", IdempotencyKey: "r1"}
	for i := 0; i < 2; i++ {
		res, err := e.cmds.Reply(context.Background(), req)
		if err != nil || res.AlreadySent != (i == 1) {
			t.Fatalf("i=%d res=%+v err=%v", i, res, err)
		}
	}
	if len(e.w.replies) != 1 {
		t.Errorf("replies = %d", len(e.w.replies))
	}
	// Same key, different target message: conflict.
	e.addMessage("m2", "f-inbox")
	req.MessageID = "m2"
	if _, err := e.cmds.Reply(context.Background(), req); exitOf(err) != 7 {
		t.Errorf("exit %d", exitOf(err))
	}
}

func TestReplyExternalDraftOnlyCreatesDraft(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalDraftOnly })
	e.addMessage("ext", "f-inbox", func(m *domain.RawMessage) { m.From = domain.Address{Address: "x@partner.com"}; m.Subject = "Hi" })
	res, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "ext", Body: "ok"})
	if err != nil || res.DraftID != "draft-1" || len(e.w.replies) != 0 || len(e.w.drafts) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if e.w.drafts[0].Subject != "[agent] Re: Hi" {
		t.Errorf("subject %q", e.w.drafts[0].Subject)
	}
}

// ---- drafts ----

func okDraft() DraftRequest {
	return DraftRequest{To: []string{"jane@corp.example.com"}, Subject: "Plan", Body: "Draft body"}
}

func TestCreateDraft(t *testing.T) {
	e := newEnv(t)
	d, err := e.cmds.CreateDraft(context.Background(), okDraft())
	if err != nil || d.ID != "draft-1" || len(e.w.drafts) != 1 || len(e.w.sent) != 0 {
		t.Fatalf("d=%+v err=%v", d, err)
	}
	if e.w.drafts[0].Subject != "[agent] Plan" || !strings.Contains(e.w.drafts[0].Body, "agent rev-01") {
		t.Errorf("draft = %+v", e.w.drafts[0])
	}
	if a := e.lastAudit(t); a.Outcome != "draft" || a.Resource != "mail.draft.create" {
		t.Errorf("audit = %+v", a)
	}
}

func TestCreateDraftPolicy(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		mut  func(*domain.Policy)
		req  func(*DraftRequest)
		exit int
	}{
		{"mode deny", func(p *domain.Policy) { p.Send.Mode = domain.SendDeny }, nil, 6},
		{"dry_run_only", func(p *domain.Policy) { p.Send.Mode = domain.SendDryRunOnly }, nil, 6},
		{"bcc", nil, func(r *DraftRequest) { r.Bcc = []string{"b@corp.example.com"} }, 6},
		{"external deny", nil, func(r *DraftRequest) { r.To = []string{"x@evil.com"} }, 6},
		{"secret", nil, func(r *DraftRequest) { r.Body = "BEGIN PRIVATE KEY" }, 6},
		{"bad recipient", nil, func(r *DraftRequest) { r.To = []string{"x"} }, 9},
		{"bad subject", nil, func(r *DraftRequest) { r.Subject = "a\nb" }, 9},
		{"empty body", nil, func(r *DraftRequest) { r.Body = "" }, 9},
		{"write cap", func(p *domain.Policy) { p.Limits.MaxWritesPerRun = 0 }, nil, 6},
	}
	for _, c := range cases {
		e := newEnv(t, func(p *domain.Policy) {
			if c.mut != nil {
				c.mut(p)
			}
		})
		req := okDraft()
		if c.req != nil {
			c.req(&req)
		}
		if _, err := e.cmds.CreateDraft(ctx, req); exitOf(err) != c.exit || e.w.total() != 0 {
			t.Errorf("%s: exit %d err %v", c.name, exitOf(err), err)
		}
	}
	e := newEnv(t, func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalDraftOnly })
	req := okDraft()
	req.To = []string{"x@partner.com"}
	if _, err := e.cmds.CreateDraft(ctx, req); err != nil {
		t.Errorf("draft_only external draft: %v", err)
	}
	e.w.draftErr = errors.New("boom")
	if _, err := e.cmds.CreateDraft(ctx, req); err == nil {
		t.Error("adapter error swallowed")
	}
}

func draftMsg(mut ...func(*domain.RawMessage)) func(*domain.RawMessage) {
	return func(m *domain.RawMessage) {
		m.IsDraft = true
		m.From = domain.Address{Address: mailbox}
		m.To = []domain.Address{{Address: "jane@corp.example.com"}}
		m.Subject = "[agent] Plan"
		m.Body = domain.RawBody{Format: domain.BodyText, Content: "draft text"}
		for _, f := range mut {
			f(m)
		}
	}
}

func TestSendDraft(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg())
	res, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", IdempotencyKey: "dk"})
	if err != nil || len(e.w.sentDraf) != 1 || e.w.sentDraf[0] != "d1" || res.DryRun {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// Idempotent.
	res, err = e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", IdempotencyKey: "dk"})
	if err != nil || !res.AlreadySent || len(e.w.sentDraf) != 1 {
		t.Errorf("replay res=%+v err=%v", res, err)
	}
	if a := e.lastAudit(t); a.Resource != "mail.draft.send" || a.Outcome != "already_sent" {
		t.Errorf("audit = %+v", a)
	}
}

func TestSendDraftBccAllowedWhenPolicyAllows(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Recipients.BccAllowed = true })
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.Bcc = []domain.Address{{Address: "x@corp.example.com"}} }))
	if _, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"}); err != nil || len(e.w.sentDraf) != 1 {
		t.Errorf("err=%v sent=%d", err, len(e.w.sentDraf))
	}
}

func TestReplyCarriesAgentHeaders(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox")
	if _, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "ok"}); err != nil || len(e.w.replies) != 1 {
		t.Fatalf("err=%v", err)
	}
	found := false
	for _, h := range e.w.replies[0].InternetHeaders {
		if h.Name == "X-Agent-Run" {
			found = true
		}
	}
	if !found {
		t.Errorf("reply headers = %+v", e.w.replies[0].InternetHeaders)
	}
}

func TestSendDraftRevalidatesAtSendTime(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		mut  func(*domain.RawMessage)
		pol  func(*domain.Policy)
		exit int
	}{
		{"recipient edited to external", draftMsg(func(m *domain.RawMessage) { m.To = []domain.Address{{Address: "x@evil.com"}} }), nil, 6},
		{"external with draft_only needs a human", draftMsg(func(m *domain.RawMessage) { m.To = []domain.Address{{Address: "x@partner.com"}} }),
			func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalDraftOnly }, 6},
		{"bcc added to a draft", draftMsg(func(m *domain.RawMessage) { m.Bcc = []domain.Address{{Address: "x@corp.example.com"}} }), nil, 6},
		{"cc edited to external", draftMsg(func(m *domain.RawMessage) { m.Cc = []domain.Address{{Address: "x@evil.com"}} }), nil, 6},
		{"secret added", draftMsg(func(m *domain.RawMessage) { m.Body.Content = "AKIA123" }), nil, 6},
		{"secret in html draft", draftMsg(func(m *domain.RawMessage) {
			m.Body = domain.RawBody{Format: domain.BodyHTML, Content: "<p>AKIA123</p>"}
		}), nil, 6},
		{"secret in subject", draftMsg(func(m *domain.RawMessage) { m.Subject = "AKIA" }), nil, 6},
		{"too big", draftMsg(func(m *domain.RawMessage) { m.Body.Content = strings.Repeat("x", 600) }), nil, 6},
		{"mode deny", draftMsg(), func(p *domain.Policy) { p.Send.Mode = domain.SendDeny }, 6},
		{"not a draft", draftMsg(func(m *domain.RawMessage) { m.IsDraft = false }), nil, 9},
		{"foreign author", draftMsg(func(m *domain.RawMessage) { m.From = domain.Address{Address: "jane@corp.example.com"} }), nil, 6},
		{"no recipient", draftMsg(func(m *domain.RawMessage) { m.To = nil }), nil, 9},
		{"malformed recipient", draftMsg(func(m *domain.RawMessage) { m.To = []domain.Address{{Address: "junk"}} }), nil, 9},
	}
	for _, c := range cases {
		e := newEnv(t, func(p *domain.Policy) {
			if c.pol != nil {
				c.pol(p)
			}
		})
		e.addMessage("d1", "f-drafts", c.mut)
		if _, err := e.cmds.SendDraft(ctx, SendDraftRequest{DraftID: "d1"}); exitOf(err) != c.exit || e.w.total() != 0 {
			t.Errorf("%s: exit %d err %v", c.name, exitOf(err), err)
		}
	}
	e := newEnv(t)
	if _, err := e.cmds.SendDraft(ctx, SendDraftRequest{DraftID: "zzz"}); exitOf(err) != 5 {
		t.Error("missing draft")
	}
	if _, err := e.cmds.SendDraft(ctx, SendDraftRequest{}); exitOf(err) != 2 {
		t.Error("empty id")
	}
	if _, err := e.cmds.SendDraft(ctx, SendDraftRequest{DraftID: "d", IdempotencyKey: "a b"}); exitOf(err) != 9 {
		t.Error("bad key")
	}
}

func TestSendDraftDedupeDryRunAndMode(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) {
		m.To = []domain.Address{{Address: "Jane@corp.example.com"}, {Address: "jane@corp.example.com"}}
		m.Cc = []domain.Address{{Address: "jane@corp.example.com"}}
		m.Subject = "multi\r\nline"
	}))
	res, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", DryRun: true})
	if err != nil || !res.DryRun || len(res.Rendered.To) != 1 || len(res.Rendered.Cc) != 0 || res.Rendered.Subject != "multi line" || e.w.total() != 0 {
		t.Errorf("res=%+v err=%v", res, err)
	}
	e2 := newEnv(t, func(p *domain.Policy) { p.Send.Mode = domain.SendDryRunOnly })
	e2.addMessage("d1", "f-drafts", draftMsg())
	res, err = e2.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"})
	if err != nil || !res.DryRun || e2.w.total() != 0 {
		t.Errorf("dry_run_only: res=%+v err=%v", res, err)
	}
}

func TestSendDraftRateAndProbe(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Rate = domain.SendRate{PerHour: 1} })
	e.addMessage("d1", "f-drafts", draftMsg())
	e.addMessage("d2", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.Subject = "two" }))
	if _, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d2"}); exitOf(err) != 8 {
		t.Errorf("rate: %v", err)
	}
	e2 := newEnv(t)
	e2.probe.found = true
	e2.addMessage("d1", "f-drafts", draftMsg())
	// FR-R6: the probe header cannot be applied to a draft send, so it is skipped.
	res, err := e2.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", IdempotencyKey: "k"})
	if err != nil || res.AlreadySent || len(e2.w.sentDraf) != 1 || len(e2.probe.keys) != 0 {
		t.Errorf("probe: res=%+v err=%v", res, err)
	}
}

func TestDeleteDraftOnlyOwnDrafts(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg())
	e.addMessage("noauthor", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.From = domain.Address{} }))
	e.addMessage("inbox", "f-inbox")
	e.addMessage("foreign", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.From = domain.Address{Address: "jane@corp.example.com"} }))
	if err := e.cmds.DeleteDraft(ctx, "d1"); err != nil || len(e.w.deleted) != 1 {
		t.Fatalf("delete own: %v", err)
	}
	if err := e.cmds.DeleteDraft(ctx, "noauthor"); exitOf(err) != 6 {
		t.Errorf("FR-R9: draft with no from must be refused: %v", err)
	}
	if err := e.cmds.DeleteDraft(ctx, "inbox"); exitOf(err) != 9 {
		t.Errorf("inbox message must not be deletable: %v", err)
	}
	if err := e.cmds.DeleteDraft(ctx, "foreign"); exitOf(err) != 6 {
		t.Errorf("foreign draft: %v", err)
	}
	if err := e.cmds.DeleteDraft(ctx, ""); exitOf(err) != 2 {
		t.Error("empty id")
	}
	if err := e.cmds.DeleteDraft(ctx, "zzz"); exitOf(err) != 5 {
		t.Error("missing")
	}
	e.w.deleteErr = errors.New("boom")
	if err := e.cmds.DeleteDraft(ctx, "d1"); err == nil {
		t.Error("adapter error swallowed")
	}
	if len(e.w.deleted) != 1 {
		t.Errorf("deleted = %v", e.w.deleted)
	}
	e2 := newEnv(t, func(p *domain.Policy) { p.Limits.MaxWritesPerRun = 0 })
	e2.addMessage("d1", "f-drafts", draftMsg())
	if err := e2.cmds.DeleteDraft(ctx, "d1"); exitOf(err) != 6 {
		t.Errorf("write cap: %v", err)
	}
}
