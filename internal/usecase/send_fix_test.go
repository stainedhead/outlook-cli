package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

func hasWarning(ws []string, w string) bool {
	for _, x := range ws {
		if x == w {
			return true
		}
	}
	return false
}

// ---- FR-R3

func TestFRR3_DraftSendWithAttachmentsFlagIsDeniedAndAudited(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.HasAttachments = true }))
	_, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"})
	mustCat(t, err, 6)
	if ruleOf(err) != domain.RuleSendAttachments || e.w.total() != 0 {
		t.Errorf("rule %q total %d", ruleOf(err), e.w.total())
	}
	if a := e.lastAudit(t); a.Outcome != "denied" || a.PolicyDecision != "deny:send.attachments" {
		t.Errorf("audit = %+v", a)
	}
}

func TestFRR3_DraftSendWithAttachmentListIsDenied(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.Attachments = []domain.Attachment{{ID: "a", Name: "x.pdf"}} }))
	_, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"})
	if ruleOf(err) != domain.RuleSendAttachments {
		t.Errorf("rule %q err %v", ruleOf(err), err)
	}
}

func TestFRR3_DraftSendWithAttachmentsAllowedByPolicy(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Attachments = true })
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.HasAttachments = true }))
	if _, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"}); err != nil || len(e.w.sentDraf) != 1 {
		t.Errorf("err %v", err)
	}
}

func TestFRR3_SecretOnlyInHTMLAttributeIsDetected(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) {
		m.Body = domain.RawBody{Format: domain.BodyHTML, Content: `<p>hi</p><a href="https://x.example/?k=AKIA123" title="t">link</a>`}
	}))
	_, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"})
	mustCat(t, err, 6)
	if ruleOf(err) != domain.RuleSendFilterPrefix+"secret_patterns" || e.w.total() != 0 {
		t.Errorf("rule %q", ruleOf(err))
	}
}

func TestFRR3_SecretOnlyInHTMLCommentIsDetected(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) {
		m.Body = domain.RawBody{Format: domain.BodyHTML, Content: `<p>hi</p><!-- BEGIN PRIVATE KEY -->`}
	}))
	_, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"})
	if ruleOf(err) != domain.RuleSendFilterPrefix+"secret_patterns" {
		t.Errorf("rule %q err %v", ruleOf(err), err)
	}
}

func TestFRR3_SecretBeyondTwoMiBIsDetected(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.BodyMaxBytes = 8 << 20 })
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) {
		m.Body = domain.RawBody{Format: domain.BodyText, Content: strings.Repeat("x", 3<<20) + " AKIA123"}
	}))
	_, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"})
	if ruleOf(err) != domain.RuleSendFilterPrefix+"secret_patterns" || e.w.total() != 0 {
		t.Errorf("rule %q err %v", ruleOf(err), err)
	}
}

func TestFRR3_BodySizeIsMeasuredOnRawContent(t *testing.T) {
	e := newEnv(t) // limit 500 bytes
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) {
		m.Body = domain.RawBody{Format: domain.BodyHTML, Content: "<p>ok</p>" + strings.Repeat("<b></b>", 200)}
	}))
	_, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"})
	if ruleOf(err) != domain.RuleSendBodyMax {
		t.Errorf("rule %q err %v", ruleOf(err), err)
	}
}

func TestFRR3_DryRunReportsSameDecisionAsRealSend(t *testing.T) {
	for _, dry := range []bool{true, false} {
		e := newEnv(t)
		e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.HasAttachments = true }))
		_, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", DryRun: dry})
		if ruleOf(err) != domain.RuleSendAttachments {
			t.Errorf("dry=%v rule %q", dry, ruleOf(err))
		}
	}
}

// ---- FR-R4

func replyTo(addrs ...string) func(*domain.RawMessage) {
	return func(m *domain.RawMessage) {
		for _, a := range addrs {
			m.ReplyTo = append(m.ReplyTo, domain.Address{Address: a})
		}
	}
}

func TestFRR4_ExternalReplyToIsDeniedAndNothingPosted(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox", replyTo("attacker@evil.com"))
	_, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "ok"})
	mustCat(t, err, 6)
	if ruleOf(err) != domain.RuleSendExternal || e.w.total() != 0 {
		t.Errorf("rule %q total %d", ruleOf(err), e.w.total())
	}
}

func TestFRR4_OneBadReplyToAmongSeveralIsDenied(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox", replyTo("ok@corp.example.com", "attacker@evil.com"))
	if _, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "ok"}); exitOf(err) != 6 || e.w.total() != 0 {
		t.Errorf("err %v", err)
	}
}

func TestFRR4_ExternalReplyToDraftOnlyPolicyCreatesDraft(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalDraftOnly })
	e.addMessage("m1", "f-inbox", replyTo("x@partner.com"))
	res, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "ok"})
	if err != nil || res.DraftID == "" || len(e.w.replies) != 0 {
		t.Fatalf("res %+v err %v", res, err)
	}
	if res.Rendered.To[0].Address != "x@partner.com" {
		t.Errorf("rendered to %+v", res.Rendered.To)
	}
}

func TestFRR4_InternalReplyToDiffersFromFromIsEvaluatedAndUsed(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox", replyTo("Boss@corp.example.com"))
	res, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "ok", DryRun: true})
	if err != nil || len(res.Rendered.To) != 1 || res.Rendered.To[0].Address != "boss@corp.example.com" {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestFRR4_MatchingReplyToBehavesAsBefore(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox", replyTo("jane@corp.example.com"))
	if _, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "ok"}); err != nil || len(e.w.replies) != 1 {
		t.Errorf("err %v", err)
	}
}

func TestFRR4_MalformedReplyToIsRefused(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox", replyTo("not an address"))
	if _, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "ok"}); exitOf(err) != 9 || e.w.total() != 0 {
		t.Errorf("err %v", err)
	}
}

// ---- FR-R6

func TestFRR6_Probe400IsInconclusiveAndSendProceeds(t *testing.T) {
	e := newEnv(t)
	e.probe.err = domain.NewValidation("graph rejected the request as invalid (HTTP 400)")
	req := okSend()
	req.IdempotencyKey = "k"
	res, err := e.cmds.Send(context.Background(), req)
	if err != nil || len(e.w.sent) != 1 || e.l.status("k") != LedgerSent {
		t.Fatalf("err %v sent %d", err, len(e.w.sent))
	}
	if !hasWarning(res.Warnings, "probe=inconclusive") || !hasWarning(e.lastAudit(t).Warnings, "probe=inconclusive") {
		t.Errorf("warnings %v / audit %v", res.Warnings, e.lastAudit(t).Warnings)
	}
}

func TestFRR6_ProbeNetworkAuthAnd5xxStayFailClosed(t *testing.T) {
	for _, err := range []error{domain.NewGeneral("graph 503"), domain.NewAuth("token"), errors.New("connection reset")} {
		e := newEnv(t)
		e.probe.err = err
		req := okSend()
		req.IdempotencyKey = "k"
		if _, got := e.cmds.Send(context.Background(), req); got == nil || len(e.w.sent) != 0 || e.l.status("k") != LedgerFailed {
			t.Errorf("%v: got %v sent %d status %q", err, got, len(e.w.sent), e.l.status("k"))
		}
	}
}

func TestFRR6_DraftSendSkipsProbeAndCarriesNoHeaders(t *testing.T) {
	e := newEnv(t)
	e.probe.found = true
	e.addMessage("d1", "f-drafts", draftMsg())
	res, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", IdempotencyKey: "k", DryRun: true})
	if err != nil || len(e.probe.keys) != 0 || len(res.Rendered.InternetHeaders) != 0 {
		t.Errorf("err %v probe %v headers %v", err, e.probe.keys, res.Rendered.InternetHeaders)
	}
	res, err = e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", IdempotencyKey: "k"})
	if err != nil || res.AlreadySent || len(e.w.sentDraf) != 1 || len(e.probe.keys) != 0 {
		t.Errorf("res %+v err %v", res, err)
	}
}

func TestFRR6_ReplySkipsProbe(t *testing.T) {
	e := newEnv(t)
	e.probe.found = true
	e.addMessage("m1", "f-inbox")
	res, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "x", IdempotencyKey: "k"})
	if err != nil || res.AlreadySent || len(e.w.replies) != 1 || len(e.probe.keys) != 0 {
		t.Errorf("res %+v err %v", res, err)
	}
}

// ---- FR-R8

// serial serializes the non-thread-safe fakes (not the ledger, whose
// ReserveWithin is the thing under test).
type serial struct{ mu sync.Mutex }

type serialWriter struct {
	MailWriter
	s *serial
}

func (w serialWriter) SendMail(ctx context.Context, m domain.OutgoingMessage) error {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	return w.MailWriter.SendMail(ctx, m)
}

type serialReader struct {
	MailReader
	s *serial
}

func (r serialReader) Me(ctx context.Context) (domain.Profile, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.MailReader.Me(ctx)
}

type serialPolicy struct {
	PolicyProvider
	s *serial
}

func (p serialPolicy) Policy(ctx context.Context) (domain.Policy, error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()
	return p.PolicyProvider.Policy(ctx)
}

type serialAudit struct {
	AuditSink
	s *serial
}

func (a serialAudit) Record(ctx context.Context, en AuditEntry, err error) error {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	return a.AuditSink.Record(ctx, en, err)
}

func TestFRR8_ConcurrentSendsAgainstCapOfOneSendExactlyOnce(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Rate = domain.SendRate{PerHour: 1} })
	sr := &serial{}
	e.deps.Writer = serialWriter{e.deps.Writer, sr}
	e.deps.Reader = serialReader{e.deps.Reader, sr}
	e.deps.Policy = serialPolicy{e.deps.Policy, sr}
	e.deps.Audit = serialAudit{e.deps.Audit, sr}
	var mu sync.Mutex
	var wg sync.WaitGroup
	ok := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := okSend()
			req.Body = "n" + string(rune('a'+i))
			if _, err := New(e.deps).Send(context.Background(), req); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if ok != 1 {
		t.Errorf("successful sends = %d, want exactly 1", ok)
	}
}

func TestFRR8_AmbiguousSendCountsTowardCap(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Rate = domain.SendRate{PerHour: 1} })
	e.w.sendErr = errors.New("timeout") // ambiguous: stays pending
	if _, err := e.cmds.Send(context.Background(), okSend()); err == nil {
		t.Fatal("want error")
	}
	e.w.sendErr = nil
	req := okSend()
	req.Body = "retry with a new key"
	_, err := e.cmds.Send(context.Background(), req)
	mustCat(t, err, 8)
	if ruleOf(err) != domain.RuleSendRateHour || len(e.w.sent) != 0 {
		t.Errorf("rule %q sent %d", ruleOf(err), len(e.w.sent))
	}
}

func TestFRR8_PendingOlderThanWindowDoesNotCount(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Rate = domain.SendRate{PerHour: 1} })
	e.w.sendErr = errors.New("timeout")
	_, _ = e.cmds.Send(context.Background(), okSend())
	e.w.sendErr = nil
	e.clk.now = e.clk.now.Add(2 * time.Hour)
	req := okSend()
	req.Body = "later"
	if _, err := e.cmds.Send(context.Background(), req); err != nil {
		t.Errorf("err %v", err)
	}
}

func TestFRR8_KeyedDraftIsDraftKindDoesNotConsumeBudgetAndReplaysAsDrafted(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) {
		p.Send.Recipients.External = domain.ExternalDraftOnly
		p.Send.Rate = domain.SendRate{PerHour: 1}
	})
	req := okSend()
	req.To = []string{"x@partner.com"}
	req.IdempotencyKey = "dk"
	for i := 0; i < 2; i++ {
		res, err := e.cmds.Send(context.Background(), req)
		if err != nil || res.DraftID != "draft-1" || res.AlreadySent || res.AlreadyDrafted != (i == 1) {
			t.Fatalf("call %d res %+v err %v", i, res, err)
		}
	}
	if e.l.entries["dk"].Kind != LedgerKindDraft {
		t.Errorf("kind %q", e.l.entries["dk"].Kind)
	}
	if n, _ := e.l.SentSince(context.Background(), t0.Add(-time.Hour)); n != 0 {
		t.Errorf("drafts counted as sends: %d", n)
	}
	if _, err := e.cmds.Send(context.Background(), okSend()); err != nil {
		t.Errorf("draft must not use the hourly budget: %v", err)
	}
	if a := e.lastAudit(t); a.Outcome != "ok" {
		t.Errorf("audit %+v", a)
	}
}

func TestFRR8_DraftReplayAuditOutcome(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalDraftOnly })
	req := okSend()
	req.To = []string{"x@partner.com"}
	req.IdempotencyKey = "dk"
	_, _ = e.cmds.Send(context.Background(), req)
	_, _ = e.cmds.Send(context.Background(), req)
	if a := e.lastAudit(t); a.Outcome != "already_drafted" {
		t.Errorf("audit %+v", a)
	}
}

func TestFRR8_CompleteFailureAfterSendIsWarnedInResultAndAudit(t *testing.T) {
	e := newEnv(t)
	e.l.completeErr = errors.New("disk full")
	req := okSend()
	req.IdempotencyKey = "k"
	res, err := e.cmds.Send(context.Background(), req)
	if err != nil || len(e.w.sent) != 1 {
		t.Fatalf("err %v", err)
	}
	if !hasWarning(res.Warnings, "ledger_update_failed") || !hasWarning(e.lastAudit(t).Warnings, "ledger_update_failed") {
		t.Errorf("warnings %v audit %v", res.Warnings, e.lastAudit(t).Warnings)
	}
}

// ---- FR-R9

func TestFRR9_DraftWithEmptyFromIsRefusedForSend(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.From = domain.Address{} }))
	_, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"})
	mustCat(t, err, 6)
	if e.w.total() != 0 {
		t.Error("sent")
	}
}

func TestFRR9_DraftWithEmptyFromIsRefusedForDelete(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.From = domain.Address{} }))
	mustCat(t, e.cmds.DeleteDraft(context.Background(), "d1"), 6)
	if len(e.w.deleted) != 0 {
		t.Error("deleted")
	}
}

func TestFRR9_DraftChangedBetweenPolicyReadAndSendAborts(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg())
	e.r.onGet = func(id string, n int) {
		if n == 1 { // after the policy read, an attacker edits the draft
			m := e.r.messages[id]
			m.To = []domain.Address{{Address: "attacker@evil.com"}}
			e.r.messages[id] = m
		}
	}
	_, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", IdempotencyKey: "k"})
	mustCat(t, err, 7)
	if len(e.w.sentDraf) != 0 {
		t.Error("changed draft was sent")
	}
	if e.l.status("k") != LedgerFailed {
		t.Errorf("key must be freed (nothing was sent), status %q", e.l.status("k"))
	}
}

func TestFRR9_UnchangedDraftIsSentAfterSecondRead(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg())
	if _, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"}); err != nil || e.r.getCalls != 2 {
		t.Errorf("err %v reads %d", err, e.r.getCalls)
	}
}

func TestFRR9_SecondReadFailureAbortsWithoutSending(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg())
	e.r.onGet = func(id string, n int) {
		if n == 1 {
			delete(e.r.messages, id)
		}
	}
	if _, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"}); err == nil || len(e.w.sentDraf) != 0 {
		t.Errorf("err %v", err)
	}
}

func TestFRR9_DraftWithoutPrefixFooterIsSentAsIsAndDryRunFlagsIt(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) { m.Subject = "plain"; m.Body.Content = "no footer" }))
	res, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", DryRun: true})
	if err != nil || res.PrefixApplied || res.Rendered.Subject != "plain" || res.Rendered.Body != "no footer" {
		t.Errorf("res %+v err %v", res, err)
	}
	if _, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"}); err != nil || len(e.w.sentDraf) != 1 {
		t.Errorf("err %v", err)
	}
}

func TestFRR9_DraftWithPrefixAndFooterReportsApplied(t *testing.T) {
	e := newEnv(t)
	e.addMessage("d1", "f-drafts", draftMsg(func(m *domain.RawMessage) {
		m.Body.Content = domain.ComposeBody("hello", testPolicy().Send.Footer, "rev-01")
	}))
	res, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1", DryRun: true})
	if err != nil || !res.PrefixApplied {
		t.Errorf("res %+v err %v", res, err)
	}
}

// ---- FR-R13

func TestFRR13_SendAuditCarriesRecipientCountHashAndNoContent(t *testing.T) {
	e := newEnv(t)
	req := okSend()
	req.Cc = []string{"bob@corp.example.com"}
	req.Subject = "SECRETSUBJECT"
	req.Body = "SECRETBODY"
	if _, err := e.cmds.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	a := e.lastAudit(t)
	if a.RecipientCount != 2 || len(a.RecipientHash) != 32 {
		t.Errorf("audit %+v", a)
	}
	dump := a.RecipientHash + a.MessageID + a.Resource + a.PolicyDecision + strings.Join(a.Warnings, ",")
	for _, bad := range []string{"SECRETSUBJECT", "SECRETBODY", "jane@", "bob@"} {
		if strings.Contains(dump, bad) {
			t.Errorf("audit leaks %q", bad)
		}
	}
	// Same audience, same hash; different audience, different hash.
	req2 := okSend()
	req2.Cc = []string{"BOB@corp.example.com"}
	req2.Body = "other"
	_, _ = e.cmds.Send(context.Background(), req2)
	if e.lastAudit(t).RecipientHash != a.RecipientHash {
		t.Error("hash must be order/case independent")
	}
}

func TestFRR13_ReplyDraftSendAndMoveAuditFields(t *testing.T) {
	e := newEnv(t)
	e.addMessage("m1", "f-inbox")
	if _, err := e.cmds.Reply(context.Background(), ReplyRequest{MessageID: "m1", Body: "ok"}); err != nil {
		t.Fatal(err)
	}
	if a := e.lastAudit(t); a.MessageID != "m1" || a.RecipientCount != 1 || a.RecipientHash == "" {
		t.Errorf("reply audit %+v", a)
	}
	e.addMessage("d1", "f-drafts", draftMsg())
	if _, err := e.cmds.SendDraft(context.Background(), SendDraftRequest{DraftID: "d1"}); err != nil {
		t.Fatal(err)
	}
	if a := e.lastAudit(t); a.MessageID != "d1" || a.RecipientCount != 1 || a.RecipientHash == "" {
		t.Errorf("draft send audit %+v", a)
	}
	if _, err := e.cmds.Move(context.Background(), MoveRequest{MessageID: "m1", Folder: "Processed"}); err != nil {
		t.Fatal(err)
	}
	if a := e.lastAudit(t); a.MessageID != "m1" {
		t.Errorf("move audit %+v", a)
	}
}

func TestFRR13_HTTPStatusPopulatedFromError(t *testing.T) {
	e := newEnv(t)
	e.w.sendErr = domain.NotSent(domain.NewForbidden("nope").WithHTTPStatus(403))
	if _, err := e.cmds.Send(context.Background(), okSend()); err == nil {
		t.Fatal("want error")
	}
	if a := e.lastAudit(t); a.HTTPStatus != 403 {
		t.Errorf("audit %+v", a)
	}
}
