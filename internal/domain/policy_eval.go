package domain

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Defaults applied when the policy leaves a bound at zero.
const (
	DefaultMaxResults   = 25
	DefaultMaxBodyBytes = 16000
	DefaultSendBodyMax  = 20000
	// MaxLinks bounds the links listed for one message.
	MaxLinks = 50
)

// Stable rule ids written to audit and Error.RuleID.
const (
	RuleDefaultDeny        = "default-deny"
	RuleMailbox            = "mailbox.mismatch"
	RuleReadFolders        = "read.folders"
	RuleSendMode           = "send.mode"
	RuleSendBcc            = "send.recipients.bcc"
	RuleSendMaxTotal       = "send.recipients.max_total"
	RuleSendAllowlist      = "send.recipients.allow"
	RuleSendExternal       = "send.recipients.external"
	RuleSendAttachments    = "send.attachments"
	RuleSendReplyAll       = "send.reply_all"
	RuleSendBodyMax        = "send.body.max_bytes"
	RuleSendRateHour       = "send.rate.per_hour"
	RuleSendRateDay        = "send.rate.per_day"
	RuleSendFilterPrefix   = "send.content_filter."
	RuleWritesPerRun       = "limits.max_writes_per_run"
	RuleMoveFolder         = "write.move.folder"
	RuleMoveDeletedItems   = "write.move.deleted_items"
	RuleAttachmentDownload = "read.attachments.download"
	RuleAttachmentType     = "read.attachments.allow_types"
	RuleAttachmentSize     = "read.attachments.max_bytes"
	RuleAttachmentOutDir   = "read.attachments.out_dir"
)

func allow() Decision { return Decision{Mode: DecisionAllow} }

func deny(rule, format string, args ...any) Decision {
	return Decision{Mode: DecisionDeny, RuleID: rule, Reason: fmt.Sprintf(format, args...)}
}

// ErrFor turns a non-allow Decision into the error a command returns: a rate
// limit denial maps to exit 8 (with the RuleID kept), every other denial to
// policy_denied (exit 6). It returns nil for DecisionAllow.
func ErrFor(d Decision) error {
	if d.Allowed() {
		return nil
	}
	if d.RuleID == RuleSendRateHour || d.RuleID == RuleSendRateDay {
		e := NewRateLimited(d.Reason)
		e.RuleID = d.RuleID
		if d.RetryAfter > 0 {
			e.HintMsg = fmt.Sprintf("retry after at most %s", d.RetryAfter)
		}
		return e
	}
	return d.Err()
}

// EvalMailbox checks the FR-002 start-up rule: the policy mailbox must equal
// the Graph /me mail or userPrincipalName (case-insensitive).
func (p Policy) EvalMailbox(pr Profile) Decision {
	m := strings.TrimSpace(p.Mailbox)
	if m == "" {
		return deny(RuleMailbox, "policy has no mailbox configured")
	}
	if strings.EqualFold(m, strings.TrimSpace(pr.Mail)) || strings.EqualFold(m, strings.TrimSpace(pr.UserPrincipalName)) {
		return allow()
	}
	return deny(RuleMailbox, "signed-in mailbox does not match the policy mailbox")
}

// EffectiveMaxResults clamps a requested page size to the policy bound.
func (p Policy) EffectiveMaxResults(requested int) int {
	limit := p.Limits.MaxResults
	if limit <= 0 {
		limit = DefaultMaxResults
	}
	if requested <= 0 || requested > limit {
		return limit
	}
	return requested
}

// EffectiveBodyBytes returns the body bound for a read: the policy bound,
// lowered (never raised) by a positive request.
func (p Policy) EffectiveBodyBytes(requested int) int {
	limit := p.Read.MaxBodyBytes
	if limit <= 0 {
		limit = DefaultMaxBodyBytes
	}
	if requested > 0 && requested < limit {
		return requested
	}
	return limit
}

// FolderAllowed reports whether policy read.folders names the folder, by
// well-known alias or display name, case-insensitively.
func (p Policy) FolderAllowed(f Folder) bool {
	for _, n := range p.Read.Folders {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if strings.EqualFold(n, f.Name) || (f.WellKnown != WellKnownNone && strings.EqualFold(n, string(f.WellKnown))) {
			return true
		}
	}
	return false
}

// EvalReadFolder returns the decision for reading or listing folder f.
func (p Policy) EvalReadFolder(f Folder) Decision {
	if p.FolderAllowed(f) {
		return allow()
	}
	return deny(RuleReadFolders, "folder %q is not in the policy read.folders list", f.Name)
}

// EvalMove returns the decision for moving a message into target: never Deleted
// Items, and only into a folder named by read.folders.
func (p Policy) EvalMove(target Folder) Decision {
	if target.WellKnown == WellKnownDeletedItems || strings.EqualFold(strings.ReplaceAll(target.Name, " ", ""), "deleteditems") {
		return deny(RuleMoveDeletedItems, "moving to Deleted Items is not allowed")
	}
	if !p.FolderAllowed(target) {
		return deny(RuleMoveFolder, "folder %q is not in the policy read.folders list", target.Name)
	}
	return allow()
}

// EvalWrites enforces limits.max_writes_per_run. done is the number of writes
// already performed in this process run. Zero denies (the zero value denies).
func (p Policy) EvalWrites(done int) Decision {
	if done >= p.Limits.MaxWritesPerRun {
		return deny(RuleWritesPerRun, "write limit for this run reached (%d)", p.Limits.MaxWritesPerRun)
	}
	return allow()
}

// SendInput is what the send policy evaluates. Recipients are already
// normalized and de-duplicated; BccRequested is true when the caller supplied
// any bcc input at all (checked before de-duplication).
type SendInput struct {
	Recipients     Recipients
	BccRequested   bool
	HasAttachments bool
	// Body is the user-supplied body before footer.
	Body string
}

// EvalSend evaluates every static send rule (mode, attachments, bcc, body
// size, recipient allow-list, max total, external handling). It returns
// DecisionDeny with the deciding RuleID, DecisionDryRunOnly, DecisionDraftOnly
// or DecisionAllow. Rate caps and content filters need other inputs and are
// evaluated separately (EvalRate, the ContentFilter port).
func (p Policy) EvalSend(in SendInput) Decision {
	s := p.Send
	if s.Mode != SendAllow && s.Mode != SendDryRunOnly {
		return deny(RuleSendMode, "send mode is %q", modeName(s.Mode))
	}
	if in.HasAttachments && !s.Attachments {
		return deny(RuleSendAttachments, "attachments are not allowed")
	}
	if in.BccRequested && !s.Recipients.BccAllowed {
		return deny(RuleSendBcc, "bcc is not allowed")
	}
	if d := p.EvalBodySize(in.Body); !d.Allowed() {
		return d
	}
	d := p.EvalRecipients(in.Recipients)
	if d.Mode == DecisionDeny {
		return d
	}
	if s.Mode == SendDryRunOnly {
		return Decision{Mode: DecisionDryRunOnly, RuleID: RuleSendMode, Reason: "send mode is dry_run_only"}
	}
	return d
}

func modeName(m SendMode) string {
	if m == "" {
		return string(SendDeny)
	}
	return string(m)
}

// EvalBodySize enforces send.body.max_bytes (default DefaultSendBodyMax).
func (p Policy) EvalBodySize(body string) Decision {
	limit := p.Send.BodyMaxBytes
	if limit <= 0 {
		limit = DefaultSendBodyMax
	}
	if len(body) > limit {
		return deny(RuleSendBodyMax, "body is %d bytes, the limit is %d", len(body), limit)
	}
	return allow()
}

// EvalRecipients applies max_total, the allow-lists and the external mode.
// A recipient is explicitly allowed when its address is in allow_addresses or
// its domain is in allow_domains. Any other recipient on an internal domain is
// denied; any other recipient on a non-internal domain is handled by the
// external mode (deny, draft_only, allow). The zero policy denies.
func (p Policy) EvalRecipients(r Recipients) Decision {
	rp := p.Send.Recipients
	if rp.MaxTotal <= 0 || r.Total() > rp.MaxTotal {
		return deny(RuleSendMaxTotal, "%d recipients exceeds the limit of %d", r.Total(), rp.MaxTotal)
	}
	result := allow()
	for _, a := range r.All() {
		if containsFold(rp.AllowAddresses, a.Address) || containsFold(rp.AllowDomains, AddressDomain(a.Address)) {
			continue
		}
		if p.SenderTrustOf(a) == TrustInternal {
			return deny(RuleSendAllowlist, "recipient %s is not on the send allow-list", a.Address)
		}
		switch rp.External {
		case ExternalAllow:
		case ExternalDraftOnly:
			result = Decision{Mode: DecisionDraftOnly, RuleID: RuleSendExternal,
				Reason: fmt.Sprintf("external recipient %s: saved as a draft for a human", a.Address)}
		default:
			return deny(RuleSendExternal, "external recipient %s is not allowed", a.Address)
		}
	}
	return result
}

// EvalReplyAll refuses reply-all unless the policy enables it.
func (p Policy) EvalReplyAll(all bool) Decision {
	if all && !p.Send.ReplyAll {
		return deny(RuleSendReplyAll, "reply-all is not allowed")
	}
	return allow()
}

// EvalRate enforces send.rate. sentHour and sentDay are the sends recorded in
// the ledger over the trailing hour and day. A zero cap means no limit. The
// RetryAfter of a denial is the window length (an upper bound: the ledger does
// not expose when the oldest counted send expires).
func (p Policy) EvalRate(sentHour, sentDay int) Decision {
	r := p.Send.Rate
	if r.PerHour > 0 && sentHour >= r.PerHour {
		d := deny(RuleSendRateHour, "hourly send limit of %d reached", r.PerHour)
		d.RetryAfter = time.Hour
		return d
	}
	if r.PerDay > 0 && sentDay >= r.PerDay {
		d := deny(RuleSendRateDay, "daily send limit of %d reached", r.PerDay)
		d.RetryAfter = 24 * time.Hour
		return d
	}
	return allow()
}

// EvalAttachmentDownload checks one attachment against read.attachments:
// download enabled, extension on the allow-list, size within the cap.
func (p Policy) EvalAttachmentDownload(a Attachment) Decision {
	ap := p.Read.Attachments
	if !ap.Download {
		return deny(RuleAttachmentDownload, "attachment download is disabled")
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(a.Name), "."))
	if ext == "" {
		return deny(RuleAttachmentType, "attachment has no file extension")
	}
	ok := false
	for _, t := range ap.AllowTypes {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(t), "."), ext) {
			ok = true
			break
		}
	}
	if !ok {
		return deny(RuleAttachmentType, "attachment type %q is not allowed", ext)
	}
	if ap.MaxBytes <= 0 || a.Size > ap.MaxBytes {
		return deny(RuleAttachmentSize, "attachment is %d bytes, the limit is %d", a.Size, ap.MaxBytes)
	}
	return allow()
}

// ResolveOutDir decides where an attachment may be written. An empty request
// means the policy out_dir; otherwise the cleaned request must be the policy
// out_dir or inside it (textual check; the QuarantineStore additionally
// resolves symlinks). It returns the directory to pass to the store.
func (p Policy) ResolveOutDir(requested string) (string, Decision) {
	base := strings.TrimSpace(p.Read.Attachments.OutDir)
	if base == "" {
		return "", deny(RuleAttachmentOutDir, "no quarantine out_dir is configured")
	}
	if !filepath.IsAbs(base) {
		return "", deny(RuleAttachmentOutDir, "the quarantine out_dir must be an absolute path")
	}
	base = filepath.Clean(base)
	req := strings.TrimSpace(requested)
	if req == "" {
		return base, allow()
	}
	if !filepath.IsAbs(req) {
		return "", deny(RuleAttachmentOutDir, "--out must be an absolute path inside the quarantine directory")
	}
	req = filepath.Clean(req)
	rel, err := filepath.Rel(base, req)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", deny(RuleAttachmentOutDir, "--out is outside the quarantine directory")
	}
	return req, allow()
}
