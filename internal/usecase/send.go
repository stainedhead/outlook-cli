package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// Send implements FR-007, FR-008 and FR-009.
func (s *service) Send(ctx context.Context, r SendRequest) (res domain.SendResult, err error) {
	err = s.exec(ctx, domain.VerbSend, "mail.send", func(c *call) error {
		var e error
		res, e = s.send(ctx, c, r)
		return e
	})
	return res, err
}

func (s *service) send(ctx context.Context, c *call, r SendRequest) (domain.SendResult, error) {
	p, err := s.begin(ctx)
	if err != nil {
		return domain.SendResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(r.IdempotencyKey); err != nil {
		return domain.SendResult{}, err
	}
	rc, err := domain.NormalizeRecipients(r.To, r.Cc, r.Bcc)
	if err != nil {
		return domain.SendResult{}, err
	}
	if err := domain.ValidateSubject(r.Subject); err != nil {
		return domain.SendResult{}, err
	}
	if err := domain.ValidateBody(r.Body); err != nil {
		return domain.SendResult{}, err
	}
	dec := p.EvalSend(domain.SendInput{
		Recipients:     rc,
		BccRequested:   len(r.Bcc) > 0,
		HasAttachments: r.HasAttachments,
		Body:           r.Body,
	})
	c.setDecision(dec)
	if dec.Mode == domain.DecisionDeny {
		return domain.SendResult{}, domain.ErrFor(dec)
	}
	if r.HasAttachments {
		return domain.SendResult{}, domain.NewValidation("sending attachments is not supported by this build")
	}
	if err := s.scan(p, r.Subject, r.Body); err != nil {
		return domain.SendResult{}, err
	}
	msg := s.render(p, rc, r.Subject, r.Body, r.IdempotencyKey)
	base := domain.SendResult{IdempotencyKey: r.IdempotencyKey, Rendered: msg, Decision: dec}
	return s.dispatch(ctx, c, p, dispatchArgs{
		kind: "send", dryRun: r.DryRun, key: r.IdempotencyKey, msg: msg, base: base,
		send: func() (string, error) { return "", s.d.Writer.SendMail(ctx, msg) },
		draft: func() (string, error) {
			d, e := s.d.Writer.CreateDraft(ctx, msg)
			return d.ID, e
		},
		probe: true,
	})
}

// render applies the subject prefix, footer and agent headers.
func (s *service) render(p domain.Policy, rc domain.Recipients, subject, body, key string) domain.OutgoingMessage {
	return domain.OutgoingMessage{
		To:              rc.To,
		Cc:              rc.Cc,
		Bcc:             rc.Bcc,
		Subject:         domain.ApplySubjectPrefix(p.Send.SubjectPrefix, strings.TrimSpace(subject)),
		Body:            domain.ComposeBody(body, p.Send.Footer, s.d.Run.AgentID),
		InternetHeaders: domain.AgentHeaders(s.d.Run.AgentID, s.d.Run.RunID, key),
	}
}

// scan runs the policy content filters over the outgoing text. A finding (or a
// filter named by policy that is not wired) refuses the send. The error never
// contains the matched text.
func (s *service) scan(p domain.Policy, texts ...string) error {
	for _, name := range p.Send.ContentFilters {
		f, ok := s.d.Filters[name]
		if !ok || f == nil {
			return denyRule(domain.RuleSendFilterPrefix+name, fmt.Sprintf("content filter %q is required by policy but not available", name))
		}
		for _, t := range texts {
			if fs := f.Scan(t); len(fs) > 0 {
				kinds := make([]string, 0, len(fs))
				for _, x := range fs {
					kinds = append(kinds, x.Kind)
				}
				return denyRule(domain.RuleSendFilterPrefix+name,
					fmt.Sprintf("content filter %q matched %d time(s): %s", name, len(fs), strings.Join(kinds, ", ")))
			}
		}
	}
	return nil
}

func denyRule(rule, reason string) error {
	return domain.ErrFor(domain.Decision{Mode: domain.DecisionDeny, RuleID: rule, Reason: reason})
}

// dispatchArgs describes one outgoing operation for dispatch.
type dispatchArgs struct {
	kind   string
	dryRun bool
	key    string
	msg    domain.OutgoingMessage
	base   domain.SendResult
	send   func() (refID string, err error)
	draft  func() (refID string, err error)
	// probe enables the optional Sent Items check for send operations.
	probe bool
}

// dispatch finishes an operation whose policy checks have passed: dry run,
// draft or real send, with the idempotency ledger around real sends.
func (s *service) dispatch(ctx context.Context, c *call, p domain.Policy, a dispatchArgs) (domain.SendResult, error) {
	res := a.base
	switch res.Decision.Mode {
	case domain.DecisionDryRunOnly:
		a.dryRun = true
	case domain.DecisionDraftOnly:
		return s.deliver(ctx, c, p, a, false)
	}
	if a.dryRun {
		// Validate the rate cap too, so a dry run predicts the real outcome.
		if err := s.checkRate(ctx, p); err != nil {
			return domain.SendResult{}, err
		}
		res.DryRun = true
		c.outcome = outcomeDryRun
		return res, nil
	}
	return s.deliver(ctx, c, p, a, true)
}

func (s *service) checkRate(ctx context.Context, p domain.Policy) error {
	r := p.Send.Rate
	if r.PerHour <= 0 && r.PerDay <= 0 {
		return nil
	}
	now := s.d.Clock.Now()
	hour, err := s.d.Ledger.SentSince(ctx, now.Add(-timeHour))
	if err != nil {
		return ledgerErr(err)
	}
	day, err := s.d.Ledger.SentSince(ctx, now.Add(-24*timeHour))
	if err != nil {
		return ledgerErr(err)
	}
	return domain.ErrFor(p.EvalRate(hour, day))
}

// deliver performs the real operation. isSend distinguishes a real send
// (counted against the rate cap, probed) from a draft creation.
func (s *service) deliver(ctx context.Context, c *call, p domain.Policy, a dispatchArgs, isSend bool) (domain.SendResult, error) {
	res := a.base
	act := a.send
	if !isSend {
		act = a.draft
	}
	finish := func(ref string) {
		if isSend {
			c.outcome = outcomeOK
		} else {
			res.DraftID = ref
			c.outcome = outcomeDraft
		}
	}

	// Drafts without a key carry no ledger entry: nothing is sent.
	if !isSend && a.key == "" {
		if err := s.countWrite(p); err != nil {
			return domain.SendResult{}, err
		}
		ref, err := act()
		if err != nil {
			return domain.SendResult{}, err
		}
		finish(ref)
		return res, nil
	}

	key := a.key
	if key == "" {
		// A send without a key is still recorded so rate caps see it.
		key = fmt.Sprintf("auto-%s-%d-%d", s.d.Run.RunID, s.d.Clock.Now().UnixNano(), s.nextSeq())
	}
	fp := domain.Fingerprint(a.kind, a.msg)
	now := s.d.Clock.Now()
	entry, created, err := s.d.Ledger.Reserve(ctx, key, fp, now)
	if err != nil {
		return domain.SendResult{}, ledgerErr(err)
	}
	if !created {
		return s.replay(c, a, entry, fp)
	}
	release := func() {
		// Best effort: the attempt provably did nothing, so free the key.
		_ = s.d.Ledger.Fail(ctx, key, s.d.Clock.Now())
	}
	if isSend {
		if err := s.checkRate(ctx, p); err != nil {
			release()
			return domain.SendResult{}, err
		}
	}
	if err := s.countWrite(p); err != nil {
		release()
		return domain.SendResult{}, err
	}
	if isSend && a.probe && a.key != "" && s.d.Probe != nil {
		found, perr := s.d.Probe.FindSentByKey(ctx, a.key)
		if perr != nil {
			release()
			return domain.SendResult{}, perr
		}
		if found {
			_ = s.d.Ledger.Complete(ctx, key, "", s.d.Clock.Now())
			res.AlreadySent = true
			c.outcome = outcomeAlreadySent
			return res, nil
		}
	}
	ref, err := act()
	if err != nil {
		if errors.Is(err, domain.ErrNotSent) {
			release()
		}
		// Otherwise the outcome is ambiguous: the entry stays pending and a
		// later attempt with this key fails closed with a conflict.
		return domain.SendResult{}, err
	}
	// The operation happened. A failed ledger update leaves the entry pending,
	// which makes any retry of this key fail closed; it must not turn a
	// completed send into an error exit.
	_ = s.d.Ledger.Complete(ctx, key, ref, s.d.Clock.Now())
	finish(ref)
	return res, nil
}

// replay handles an existing ledger entry for the key.
func (s *service) replay(c *call, a dispatchArgs, e LedgerEntry, fp string) (domain.SendResult, error) {
	if e.Fingerprint != fp {
		return domain.SendResult{}, domain.NewConflict("idempotency key was already used for a different request").
			WithHint("use a new --idempotency-key for a different message")
	}
	if e.Status == LedgerSent {
		res := a.base
		res.AlreadySent = true
		res.DraftID = e.RefID
		c.outcome = outcomeAlreadySent
		return res, nil
	}
	return domain.SendResult{}, domain.NewConflict("an earlier attempt with this idempotency key has an unknown outcome; nothing was sent now").
		WithHint("check Sent Items before retrying with a new key")
}

// Reply implements FR-010: reply to the sender only.
func (s *service) Reply(ctx context.Context, r ReplyRequest) (res domain.SendResult, err error) {
	err = s.exec(ctx, domain.VerbSend, "mail.reply", func(c *call) error {
		var e error
		res, e = s.reply(ctx, c, r)
		return e
	})
	return res, err
}

func (s *service) reply(ctx context.Context, c *call, r ReplyRequest) (domain.SendResult, error) {
	p, err := s.begin(ctx)
	if err != nil {
		return domain.SendResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(r.IdempotencyKey); err != nil {
		return domain.SendResult{}, err
	}
	if err := domain.ValidateBody(r.Body); err != nil {
		return domain.SendResult{}, err
	}
	if d := p.EvalReplyAll(r.All); !d.Allowed() {
		c.setDecision(d)
		return domain.SendResult{}, domain.ErrFor(d)
	}
	if r.All {
		return domain.SendResult{}, domain.NewValidation("reply-all is not supported by this build")
	}
	orig, err := s.readableMessage(ctx, p, r.MessageID, false)
	if err != nil {
		return domain.SendResult{}, err
	}
	sender, err := domain.ParseAddress(orig.From.Address)
	if err != nil {
		return domain.SendResult{}, domain.NewValidation("the message has no usable sender address to reply to")
	}
	rc := domain.Recipients{To: []domain.Address{sender}}
	dec := p.EvalSend(domain.SendInput{Recipients: rc, Body: r.Body})
	c.setDecision(dec)
	if dec.Mode == domain.DecisionDeny {
		return domain.SendResult{}, domain.ErrFor(dec)
	}
	if err := s.scan(p, r.Body); err != nil {
		return domain.SendResult{}, err
	}
	body := domain.ComposeBody(r.Body, p.Send.Footer, s.d.Run.AgentID)
	subject := "Re: " + oneLine(orig.Subject)
	msg := domain.OutgoingMessage{
		To:              rc.To,
		Subject:         domain.ApplySubjectPrefix(p.Send.SubjectPrefix, subject),
		Body:            body,
		InternetHeaders: domain.AgentHeaders(s.d.Run.AgentID, s.d.Run.RunID, r.IdempotencyKey),
	}
	base := domain.SendResult{IdempotencyKey: r.IdempotencyKey, Rendered: msg, Decision: dec}
	// ASSUMPTION(unverified against a real tenant): the reply endpoint builds
	// the subject, recipients and quoted thread itself, so the body (footer
	// included) and X-Agent-* headers (message.internetMessageHeaders) are
	// ours; the subject prefix cannot be set.
	return s.dispatch(ctx, c, p, dispatchArgs{
		kind: "reply:" + r.MessageID, dryRun: r.DryRun, key: r.IdempotencyKey,
		msg:  domain.OutgoingMessage{To: rc.To, Body: body},
		base: base,
		send: func() (string, error) {
			return "", s.d.Writer.ReplyToSender(ctx, domain.Reply{MessageID: r.MessageID, Body: body, InternetHeaders: msg.InternetHeaders})
		},
		draft: func() (string, error) {
			d, e := s.d.Writer.CreateDraft(ctx, msg)
			return d.ID, e
		},
	})
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(domain.CleanText(s)), " ")
}

// CreateDraft implements `mail draft create`. A draft goes through the same
// recipient, bcc, size and content-filter rules as a send; a draft is also a
// write, so send.mode must be allow.
func (s *service) CreateDraft(ctx context.Context, r DraftRequest) (draft domain.Draft, err error) {
	err = s.exec(ctx, domain.VerbSend, "mail.draft.create", func(c *call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		rc, e := domain.NormalizeRecipients(r.To, r.Cc, r.Bcc)
		if e != nil {
			return e
		}
		if e = domain.ValidateSubject(r.Subject); e != nil {
			return e
		}
		if e = domain.ValidateBody(r.Body); e != nil {
			return e
		}
		dec := p.EvalSend(domain.SendInput{Recipients: rc, BccRequested: len(r.Bcc) > 0, Body: r.Body})
		c.setDecision(dec)
		if dec.Mode != domain.DecisionAllow && dec.Mode != domain.DecisionDraftOnly {
			return domain.ErrFor(dec)
		}
		if e = s.scan(p, r.Subject, r.Body); e != nil {
			return e
		}
		msg := s.render(p, rc, r.Subject, r.Body, "")
		if e = s.countWrite(p); e != nil {
			return e
		}
		d, e := s.d.Writer.CreateDraft(ctx, msg)
		if e != nil {
			return e
		}
		d.MessageSummary = s.cleanSummary(p, d.MessageSummary)
		draft = d
		c.outcome = outcomeDraft
		return nil
	})
	return draft, err
}

// ListDrafts implements `mail draft list`.
func (s *service) ListDrafts(ctx context.Context, r DraftListRequest) (page domain.Page[domain.MessageSummary], err error) {
	err = s.exec(ctx, domain.VerbRead, "mail.draft.list", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		pg, e := s.d.Reader.ListDrafts(ctx, p.EffectiveMaxResults(r.Limit), r.PageToken)
		if e != nil {
			return e
		}
		page = s.cleanPage(p, pg)
		return nil
	})
	return page, err
}

// ownDraft fetches a message and requires it to be a draft authored by the
// agent mailbox.
func (s *service) ownDraft(ctx context.Context, p domain.Policy, id string) (domain.RawMessage, error) {
	if id == "" {
		return domain.RawMessage{}, domain.NewUsage("draft id is required")
	}
	raw, err := s.d.Reader.GetMessage(ctx, id, false)
	if err != nil {
		return domain.RawMessage{}, err
	}
	if !raw.IsDraft {
		return domain.RawMessage{}, domain.NewValidation("the message is not a draft")
	}
	if from := lower(raw.From.Address); from != "" && from != lower(p.Mailbox) {
		return domain.RawMessage{}, domain.ErrFor(domain.Decision{Mode: domain.DecisionDeny, RuleID: "write.draft.author",
			Reason: "the draft was not authored by the agent mailbox"})
	}
	return raw, nil
}

// DeleteDraft implements `mail draft delete`: only the agent's own drafts.
func (s *service) DeleteDraft(ctx context.Context, draftID string) error {
	return s.exec(ctx, domain.VerbWrite, "mail.draft.delete", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		if _, e = s.ownDraft(ctx, p, draftID); e != nil {
			return e
		}
		if e = s.countWrite(p); e != nil {
			return e
		}
		return s.d.Writer.DeleteDraft(ctx, draftID)
	})
}

// SendDraft implements `mail draft send`. The draft is re-read and its
// recipients, size and content are re-validated against policy.
//
// Bcc recipients on the draft (RawMessage.Bcc) are subject to the bcc rule.
func (s *service) SendDraft(ctx context.Context, r SendDraftRequest) (res domain.SendResult, err error) {
	err = s.exec(ctx, domain.VerbSend, "mail.draft.send", func(c *call) error {
		var e error
		res, e = s.sendDraft(ctx, c, r)
		return e
	})
	return res, err
}

func (s *service) sendDraft(ctx context.Context, c *call, r SendDraftRequest) (domain.SendResult, error) {
	p, err := s.begin(ctx)
	if err != nil {
		return domain.SendResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(r.IdempotencyKey); err != nil {
		return domain.SendResult{}, err
	}
	raw, err := s.ownDraft(ctx, p, r.DraftID)
	if err != nil {
		return domain.SendResult{}, err
	}
	var rc domain.Recipients
	seen := map[string]bool{}
	for _, part := range []struct {
		src []domain.Address
		dst *[]domain.Address
	}{{raw.To, &rc.To}, {raw.Cc, &rc.Cc}, {raw.Bcc, &rc.Bcc}} {
		for _, a := range part.src {
			na, e := domain.ParseAddress(a.Address)
			if e != nil {
				return domain.SendResult{}, e
			}
			if !seen[na.Address] {
				seen[na.Address] = true
				*part.dst = append(*part.dst, na)
			}
		}
	}
	if len(rc.To) == 0 {
		return domain.SendResult{}, domain.NewValidation("the draft has no recipient")
	}
	subject := oneLine(raw.Subject)
	body, _ := s.convertBody(p, raw.Body, maxRawBodyBytes)
	dec := p.EvalSend(domain.SendInput{Recipients: rc, BccRequested: len(rc.Bcc) > 0, Body: body.Text})
	c.setDecision(dec)
	// A draft that needs a human (external, draft_only) is never sent by the agent.
	if dec.Mode == domain.DecisionDeny || dec.Mode == domain.DecisionDraftOnly {
		return domain.SendResult{}, domain.ErrFor(dec)
	}
	if err := s.scan(p, subject, body.Text); err != nil {
		return domain.SendResult{}, err
	}
	msg := domain.OutgoingMessage{
		To: rc.To, Cc: rc.Cc, Bcc: rc.Bcc, Subject: subject, Body: body.Text,
		InternetHeaders: domain.AgentHeaders(s.d.Run.AgentID, s.d.Run.RunID, r.IdempotencyKey),
	}
	base := domain.SendResult{IdempotencyKey: r.IdempotencyKey, Rendered: msg, Decision: dec}
	return s.dispatch(ctx, c, p, dispatchArgs{
		kind: "senddraft:" + r.DraftID, dryRun: r.DryRun, key: r.IdempotencyKey, msg: msg, base: base,
		send:  func() (string, error) { return "", s.d.Writer.SendDraft(ctx, r.DraftID) },
		probe: true,
	})
}
