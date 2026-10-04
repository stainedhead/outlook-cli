package domain

import "time"

// Verb classifies an operation for policy purposes and for audit records.
type Verb string

// The verbs. They are the strings written to audit and passed to selftest.
const (
	VerbRead  Verb = "read"
	VerbSend  Verb = "send"
	VerbWrite Verb = "write"
)

// ExternalMode says what happens to a message with an external recipient.
type ExternalMode string

// The external modes (policy send.recipients.external).
const (
	// ExternalDeny refuses the send (default).
	ExternalDeny ExternalMode = "deny"
	// ExternalDraftOnly saves a draft for a human to review and send.
	ExternalDraftOnly ExternalMode = "draft_only"
	// ExternalAllow sends.
	ExternalAllow ExternalMode = "allow"
)

// SendMode is policy send.mode.
type SendMode string

// The send modes.
const (
	SendAllow      SendMode = "allow"
	SendDryRunOnly SendMode = "dry_run_only"
	SendDeny       SendMode = "deny"
)

// Policy is the client-side guardrail policy of PRD section 9, already parsed
// and validated. The zero value denies everything. It is a guardrail, not a
// security control: Exchange mail flow and Conditional Access are the
// boundary. The adapter that loads it (internal/adapter/policyfile, WS4) must
// fail closed on unknown keys, duplicate keys, an empty file and a file the
// agent user can write.
type Policy struct {
	Profile string
	// Mailbox is the only mailbox the CLI will address; it must equal the
	// Graph /me mail or UPN (FR-002).
	Mailbox         string
	InternalDomains []string
	Read            ReadPolicy
	Send            SendPolicy
	Limits          Limits
	AuditPath       string
}

// ReadPolicy is policy section read.
type ReadPolicy struct {
	// Folders lists the folders (well-known alias or display name) that may be
	// read or listed. Empty means none.
	Folders      []string
	MaxBodyBytes int
	HTMLToText   bool
	DefangLinks  bool
	// TrustedAuthservIDs are the Authentication-Results authserv-ids whose
	// SPF/DKIM/DMARC verdicts are reported (FR-R10). Empty means every verdict
	// is "unverified".
	TrustedAuthservIDs []string
	Attachments        AttachmentPolicy
}

// AttachmentPolicy governs attachment download. The zero value denies.
type AttachmentPolicy struct {
	// Download is false (deny) unless the policy enables it.
	Download bool
	// AllowTypes are lower-case file extensions without dot ("pdf").
	AllowTypes []string
	MaxBytes   int64
	// OutDir is the quarantine directory; --out must resolve inside it.
	OutDir string
}

// SendPolicy is policy section send.
type SendPolicy struct {
	Mode          SendMode
	Recipients    RecipientPolicy
	ReplyAll      bool // true only if the policy allows reply-all (default false)
	Attachments   bool // true only if the policy allows send attachments (default false)
	SubjectPrefix string
	// Footer may contain the {agent_id} placeholder.
	Footer string
	// BodyMaxBytes bounds the outgoing body; zero means the use case default.
	BodyMaxBytes int
	// ContentFilters names the filters to run on outgoing bodies, for example
	// "secret_patterns" and "classification_markers". Each name must resolve
	// to a ContentFilter port implementation or the policy is invalid.
	ContentFilters []string
	Rate           SendRate
}

// RecipientPolicy is policy send.recipients.
type RecipientPolicy struct {
	AllowDomains   []string
	AllowAddresses []string
	External       ExternalMode
	// MaxTotal bounds to+cc(+bcc) after de-duplication.
	MaxTotal int
	// BccAllowed is true only if the policy allows BCC (default false).
	BccAllowed bool
}

// SendRate is policy send.rate. Zero means no limit.
type SendRate struct {
	PerHour int
	PerDay  int
}

// Limits is policy limits.
type Limits struct {
	MaxResults int
	// MaxWritesPerRun bounds writes in one process run (in memory).
	MaxWritesPerRun int
}

// DecisionMode is the outcome of a policy evaluation.
type DecisionMode string

// The decision modes.
const (
	DecisionAllow      DecisionMode = "allow"
	DecisionDryRunOnly DecisionMode = "dry_run_only"
	DecisionDraftOnly  DecisionMode = "draft_only"
	DecisionDeny       DecisionMode = "deny"
)

// Decision is the plain-data result of evaluating a request against Policy.
// RuleID names the deciding rule (stable strings such as "send.recipients.
// external", "send.rate.per_hour", "default-deny") and is written to audit.
type Decision struct {
	Mode   DecisionMode
	RuleID string
	Reason string
	// RetryAfter is set when a rate limit denied the request.
	RetryAfter time.Duration
}

// Allowed reports whether the action may run as asked.
func (d Decision) Allowed() bool { return d.Mode == DecisionAllow }

// AuditString is the value written to audit Record.PolicyDecision, for example
// "allow", "deny:send.recipients.external".
func (d Decision) AuditString() string {
	if d.Mode == DecisionAllow || d.RuleID == "" {
		return string(d.Mode)
	}
	return string(d.Mode) + ":" + d.RuleID
}

// Err returns nil when the decision allows the action, and a policy-denied
// Error (exit 6) for every other mode. Callers that support dry-run or draft
// outcomes must test Mode first.
func (d Decision) Err() error {
	if d.Allowed() {
		return nil
	}
	e := NewPolicyDenied(d.Reason)
	e.RuleID = d.RuleID
	return e
}
