package usecase

// This file is the Phase A contract between the parallel workstreams. Change
// it only through the lead; additive changes are cheap, signature changes are
// not. Nothing in this package may import an adapter, cmd/outlook, httpx,
// auth, policy, audit, selftest, docgen or authtest from agent-cli-core
// (output is allowed for output.Category; see internal/archtest).
//
// # Who implements what
//
//	Commands            WS1 (usecase.New)           consumed by WS3 (CLI), WS4 (selftest probe)
//	MailReader          WS2 (adapter/graph)         consumed by WS1
//	MailWriter          WS2 (adapter/graph)         consumed by WS1
//	SentItemsProbe      WS2 (adapter/graph), P1     consumed by WS1 (optional, may be nil)
//	Ledger              WS2 (adapter/ledger)        consumed by WS1
//	QuarantineStore     WS2 (adapter/ledger pkg or adapter/quarantine) consumed by WS1
//	PolicyProvider      WS4 (adapter/policyfile)    consumed by WS1/WS3
//	ContentFilter       WS4 (adapter/policyfile)    consumed by WS1
//	AuditSink           WS4 (adapter/auditlog)      consumed by WS1
//	Clock               WS3 composition (system)    fakes in all tests
//
// Policy EVALUATION (recipient rules, caps, rate, bcc, reply-all, content
// filters) is pure domain/use-case code owned by WS1: PolicyProvider only
// supplies the parsed domain.Policy. Rate counters survive process exit
// through Ledger.SentSince, because every CLI run is a separate process.
//
// # How agent-cli-core v0.1.0 is used (read this before coding)
//
// output (envelope, exit codes, untrusted marking, bounds). Used by WS3 and,
// for output.Category only, by internal/domain.
//   - Every command writes ONE envelope with output.Write(w, env, opts):
//     output.Success(data, &output.Meta{...}) or output.FromError(err).
//     The process exit code is env.ExitCode() / output.ExitOf(err); they agree.
//   - Exit codes: 0 ok, 1 general, 2 usage, 3 auth (reauth_required, second
//     401, daemon unreachable), 4 forbidden (403), 5 not_found, 6 policy_denied,
//     7 conflict, 8 rate_limited (after bounded retries), 9 validation.
//   - Errors reach a code by implementing output.CategoryError. domain.Error
//     does; use domain.NewXxx constructors. Errors from httpx and auth already
//     do: wrap them with %w, never re-categorize them.
//   - Free text from other people is output.Untrusted{Value, Author, Timestamp}.
//     Wrap in the presenter (WS3), exactly these fields: Message.Subject,
//     Message.Body.Text, Address.Name (from and to), Attachment.Name, Link.Text.
//     Do not wrap ids, addresses, dates, sizes, folder names we configured.
//   - Output is bounded (default 32768 bytes, Bounds.MaxBytes / Offset) and
//     arrays are cut by whole items; meta carries next_offset. NOTE the core
//     Meta has no next_page_token: put the Graph continuation in data
//     ({"messages": [...], "next_page_token": "..."}), see docs/requested-core-changes.md.
//   - Wire Options.Secrets only if the process ever holds a literal secret
//     (it must not: tokens never leave auth.Authorizer).
//
// auth (tokens). Used by WS3 (composition) and WS2 (as httpx.TokenRefresher).
//   - Daemon access is auth.DaemonClient {Fetch(ctx, provider), Refresh(ctx,
//     provider)}. The provider name is "msgraph". cmd/outlook's
//     newDaemonClient() returns core's auth/oktad adapter (unreachable and
//     reauth/revoked/access errors exit 3, transient exit 8).
//   - auth.NewDaemonTokenSource(client, "msgraph", auth.WithRemediation(
//     "a human must run: agent-okta-d enroll msgraph")) then
//     auth.NewAuthorizer(src). *auth.Authorizer satisfies httpx.TokenRefresher
//     structurally and is the only thing the Graph adapter receives. The
//     adapter never sees a token; auth.Token prints as [redacted].
//   - Tests: authtest.New(authtest.Valid|ExpiredNeedsRefresh|ReauthRequired|
//     Unreachable|UnauthorizedThenSuccess|UnauthorizedTwice).Fetch/Refresh is
//     a DaemonClient; its Handler() is a fake resource server that checks the
//     bearer token, usable as (or behind) the httptest Graph.
//
// httpx (HTTP). Used by WS2 only. httpx.NewClient(httpx.Config{Refresher: a,
// AllowedHosts: []string{"graph.microsoft.com"}, VendorCode: ...}).
//   - Retries 429/502/503/504/network errors with jitter, honours Retry-After
//     (up to MaxWait), one 401 -> Refresh -> resend (same budget), then typed
//     errors: *httpx.RateLimitedError (8), *httpx.AuthError (3),
//     *httpx.ForbiddenError (4), *httpx.ForbiddenHostError (4). Bodies are
//     never in errors.
//   - Only idempotent methods retry. POST sendMail / reply / send-draft MUST NOT
//     be passed through httpx.MarkSafeToRetry (a replay could double-send; the
//     ledger is the idempotency mechanism). POST-create-draft and move are also
//     not marked. Use Config.Clock (fake in tests) and a tiny BaseDelay/MaxWait.
//   - VendorCode(h http.Header) is offered only response headers (never the
//     body), so the Graph error code of a 403 comes from a header such as
//     request-id/x-ms-* (ASSUMPTION unverified against a real tenant; gap
//     recorded in docs/requested-core-changes.md).
//   - Host allowlist: only graph.microsoft.com (tests: the httptest host).
//     Redirects elsewhere are refused. Never build a /users/{id} URL.
//
// policy (rule engine). v0.1.0's policy package is a generic verb/resource/
// field rule engine with its own YAML schema (version, rules, limits,
// rate_limit). It does NOT understand the PRD section 9 schema (mailbox,
// recipients, domains) and its rate limits are in-memory per process. So
// outlook models its typed policy in domain.Policy and evaluates it itself.
// WS4 may reuse core's policy.WritableMode semantic but core exposes it only
// through policy.Load (no standalone "is this file agent-writable" check): WS4
// implements a small stat-based check and records the gap. Strict YAML:
// github.com/goccy/go-yaml with yaml.Strict() and DisallowDuplicateKey
// (add as a direct dependency in go.mod when WS4 lands; same module core uses).
//
// audit (JSONL). Used by WS4 only. audit.Open(audit.Config{Path,
// OnFailure}) -> *audit.Logger; logger.Handle(rec, actionErr) returns the error
// the command should return (Block mode fails the action on a failed write).
// audit.Record fields: Tool "outlook", AgentID, RunID, Verb, Resource, Outcome,
// HTTPStatus, Duration, PolicyDecision. It has no body field by design;
// AuditEntry (below) maps 1:1. Close() the logger at exit.
//
// selftest (matrix). Used by WS4 (rows + probe) and WS3 (command).
// selftest.Runner{Rows, Probe, ReadOnly}.Run(ctx) -> Result; Result.Write(w,
// opts) returns the exit code (1 if any row failed). Rows are {Name, Verb,
// Resource, Expect Allow|Deny, ReadOnly}. The probe drives Commands (fake
// backed in CI; live only on demand, never in PR CI).
//
// docgen (skill). Used by WS3: docgen.Generate(docgen.CommandTree{Name:
// "outlook", Description, Commands: []docgen.Command{{Name, Description,
// Usage, Examples, Forbidden}}}) writes dist/outlook-cli.md via `make skill`.
//
// Clock. core's clock package is internal (not importable), so outlook defines
// its own Clock below. audit and httpx take their own clock options; pass an
// adapter around ours in tests.

import (
	"context"
	"io"
	"time"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// ----------------------------------------------------------------------------
// Ports consumed by use cases (implemented by adapters)
// ----------------------------------------------------------------------------

// Clock is the only source of time in domain and use-case code.
type Clock interface {
	Now() time.Time
}

// MailReader is the read side of Graph, always against /me. ASSUMPTION
// (unverified against a real tenant): every endpoint shape, $select/$orderby/
// $search behaviour and Prefer header handling. No method takes a mailbox.
//
// Contract for all methods: honour ctx; map HTTP outcomes to errors (401/403/
// 429/503 come from httpx; 404 -> domain.NewNotFound; malformed JSON ->
// domain.NewGeneral; never panic; never include bodies in errors); return
// only metadata unless the method says otherwise.
type MailReader interface {
	// Me returns GET /me?$select=mail,userPrincipalName.
	Me(ctx context.Context) (domain.Profile, error)
	// ListFolders returns all top-level mail folders with counts and, for
	// well-known folders, domain.Folder.WellKnown set.
	ListFolders(ctx context.Context) ([]domain.Folder, error)
	// ResolveFolder finds a folder by well-known alias or display name
	// (case-insensitive). Unknown -> domain.NewNotFound (exit 5).
	ResolveFolder(ctx context.Context, name string) (domain.Folder, error)
	// ListMessages returns message summaries, newest first, no bodies.
	ListMessages(ctx context.Context, q domain.MessageQuery) (domain.Page[domain.MessageSummary], error)
	// SearchMessages runs a free-text search.
	SearchMessages(ctx context.Context, q domain.SearchQuery) (domain.Page[domain.MessageSummary], error)
	// GetMessage returns one message with its body as text where Graph allows
	// (Prefer: outlook.body-content-type="text"), attachment metadata and, when
	// wantHeaders, the internet headers needed for auth results. The adapter
	// does NOT truncate or convert; the use case does.
	GetMessage(ctx context.Context, id string, wantHeaders bool) (domain.RawMessage, error)
	// ListAttachments returns attachment metadata (no content).
	ListAttachments(ctx context.Context, messageID string) ([]domain.Attachment, error)
	// OpenAttachment streams one attachment's content. The caller closes the
	// reader and must read at most the policy size cap; the adapter reports
	// the declared Attachment metadata first so size checks precede download.
	OpenAttachment(ctx context.Context, messageID, attachmentID string) (domain.Attachment, io.ReadCloser, error)
	// ListDrafts returns the agent's own drafts (Drafts folder).
	ListDrafts(ctx context.Context, limit int, pageToken string) (domain.Page[domain.MessageSummary], error)
}

// MailWriter is the write side of Graph. Adapters send exactly what they are
// given: policy, validation, prefix, footer and headers are already applied by
// the use case. Never mark these POSTs safe to retry (see httpx notes above).
//
// Error contract: when the adapter can prove the request was not accepted it
// returns domain.NotSent(err) (errors.Is(err, domain.ErrNotSent)); otherwise the
// outcome is ambiguous and the use case leaves the ledger entry pending.
type MailWriter interface {
	// SendMail is POST /me/sendMail (saveToSentItems true).
	SendMail(ctx context.Context, m domain.OutgoingMessage) error
	// CreateDraft is POST /me/messages and returns the saved draft.
	CreateDraft(ctx context.Context, m domain.OutgoingMessage) (domain.Draft, error)
	// SendDraft is POST /me/messages/{id}/send.
	SendDraft(ctx context.Context, draftID string) error
	// DeleteDraft deletes one draft. The use case has already verified the
	// message is a draft authored by the agent mailbox.
	DeleteDraft(ctx context.Context, draftID string) error
	// ReplyToSender is POST /me/messages/{id}/reply. Reply-all has no port
	// method on purpose.
	ReplyToSender(ctx context.Context, r domain.Reply) error
	// SetRead is PATCH /me/messages/{id} isRead.
	SetRead(ctx context.Context, messageID string, read bool) error
	// MoveMessage is POST /me/messages/{id}/move and returns the new id.
	MoveMessage(ctx context.Context, messageID, folderID string) (newID string, err error)
}

// SentItemsProbe is the optional P1 idempotency check against Sent Items
// headers (X-Agent-Run / an idempotency header). ASSUMPTION(unverified against
// a real tenant). May be nil in Deps.
type SentItemsProbe interface {
	// FindSentByKey reports whether a message carrying the idempotency key is
	// already in Sent Items.
	FindSentByKey(ctx context.Context, key string) (found bool, err error)
}

// LedgerStatus is the state of one idempotency ledger entry.
type LedgerStatus string

// The ledger states.
const (
	// LedgerNew is returned by Reserve for a key not seen before; the entry
	// was created as pending.
	LedgerNew LedgerStatus = "new"
	// LedgerPending: an attempt reserved the key but never recorded a result.
	// Its outcome is unknown: fail closed with domain.NewConflict.
	LedgerPending LedgerStatus = "pending"
	// LedgerSent: the send completed; return the recorded result, no POST.
	LedgerSent LedgerStatus = "sent"
	// LedgerFailed: the attempt provably failed; the key may be retried.
	LedgerFailed LedgerStatus = "failed"
	// LedgerOverCap is returned only by ReserveWithin when a rate window is
	// full: nothing was reserved or written.
	LedgerOverCap LedgerStatus = "over_cap"
)

// LedgerKind distinguishes real sends from draft creations (FR-R8). Only
// LedgerKindSend entries count toward the rate caps. An entry with an empty
// kind (written by an older build) is treated as LedgerKindSend.
type LedgerKind string

// The ledger entry kinds.
const (
	LedgerKindSend  LedgerKind = "send"
	LedgerKindDraft LedgerKind = "draft"
)

// RateWindow is one rate-cap window for ReserveWithin: the number of send
// entries (sent or pending) with At >= Since must be below Cap. A Cap <= 0
// means no limit for that window.
type RateWindow struct {
	Since time.Time
	Cap   int
}

// LedgerEntry is one record of the idempotency ledger. It never stores bodies:
// only the key, a fingerprint (hash) of the request, status, time and ids.
type LedgerEntry struct {
	Key         string
	Fingerprint string
	Status      LedgerStatus
	// Kind is LedgerKindSend or LedgerKindDraft (empty means send).
	Kind LedgerKind
	// At is the time of the last state change (from Clock).
	At time.Time
	// RefID is the draft id for draft-only results, empty otherwise.
	RefID string
	// Count is the number of recipients, used by nothing but audit/debug.
	Count int
}

// Ledger is the local idempotency and send-history store (FR-008). The file
// implementation (WS2, internal/adapter/ledger) must use a file lock plus
// atomic write so two processes cannot both reserve a key, and must FAIL
// CLOSED: a corrupt or unreadable ledger returns an error (no send happens).
// It also provides the send history used for per-hour/per-day caps.
type Ledger interface {
	// Reserve atomically creates a pending entry for key if absent and returns
	// (entry, true). If an entry exists it returns (existing, false) untouched,
	// except that a LedgerFailed entry is reset to pending and returned with
	// true so the retry proceeds. Fingerprint comparison is the use case's job.
	Reserve(ctx context.Context, key, fingerprint string, at time.Time) (LedgerEntry, bool, error)
	// ReserveWithin is Reserve plus the rate-cap check performed under the same
	// lock (FR-R8), so concurrent processes cannot both pass the check.
	//
	// kind is stored on a newly created entry. For kind LedgerKindSend, counts
	// has one element per window: the number of LedgerKindSend entries with
	// status sent OR pending and At >= window.Since (pending counts,
	// conservatively, because an ambiguous send may have gone out). The key's
	// own entry is not counted. If an entry for key already exists (and is not
	// failed) it is returned untouched with created=false and counts nil, as
	// Reserve does. Otherwise, if any window has Cap > 0 and its count >= Cap,
	// nothing is written: it returns an entry with Status LedgerOverCap,
	// created=false and the counts, so the caller can evaluate the policy
	// (Policy.EvalRate with counts[0]=hour, counts[1]=day). Otherwise it
	// reserves like Reserve (a failed entry is reset to pending, kind
	// replaced) and returns created=true with the counts. LedgerKindDraft
	// reservations ignore windows and return nil counts.
	ReserveWithin(ctx context.Context, key, fingerprint string, kind LedgerKind, at time.Time, windows []RateWindow) (entry LedgerEntry, created bool, counts []int, err error)
	// Complete marks the entry sent (refID optional) at time at.
	Complete(ctx context.Context, key, refID string, at time.Time) error
	// Fail marks the entry failed (provably not sent) so the key can be retried.
	Fail(ctx context.Context, key string, at time.Time) error
	// SentSince counts LedgerKindSend entries (status sent or pending, never
	// draft kind or failed) with At at or after since. Sends with no
	// idempotency key are recorded under a generated key by the use case so
	// rate caps see them too.
	SentSince(ctx context.Context, since time.Time) (int, error)
}

// QuarantineStore saves an attachment under the policy quarantine directory.
// The implementation must: resolve outDir and refuse anything outside the
// policy OutDir (including symlinks and ../), sanitize name, create the file
// 0600 exclusively, copy at most maxBytes (error if exceeded, removing the
// partial file), and NEVER open, execute or interpret the content.
type QuarantineStore interface {
	Save(ctx context.Context, outDir, name string, content io.Reader, maxBytes int64) (path string, written int64, err error)
}

// PolicyProvider supplies the parsed, validated client-side policy. Load
// failure is fatal: the command exits (usage/validation or policy_denied for an
// agent-writable file) and must not run. It may be called once per run; the
// result is immutable.
type PolicyProvider interface {
	Policy(ctx context.Context) (domain.Policy, error)
}

// FilterFinding is one content-filter hit. It never contains the matched text
// (it could be the secret), only where and what kind.
type FilterFinding struct {
	Filter string
	Kind   string
	Offset int
}

// ContentFilter scans an outgoing body (policy send.content_filters). Names
// used by the PRD: "secret_patterns", "classification_markers". Findings make
// the use case refuse the send with a policy denial whose RuleID is
// "send.content_filter.<name>".
type ContentFilter interface {
	Name() string
	Scan(text string) []FilterFinding
}

// AuditEntry is one audit record minus the fields the sink owns (tool name,
// agent id, run id, timestamp, schema version). It has no body field.
type AuditEntry struct {
	Verb     domain.Verb
	Resource string // for example "mail.send", "mail.list", "attachment.get"
	// Outcome is "ok", "denied", "dry_run", "draft", "error" or "already_sent".
	Outcome        string
	HTTPStatus     int
	Duration       time.Duration
	PolicyDecision string // domain.Decision.AuditString()
	// RecipientCount, RecipientHash and MessageID are set for send, reply,
	// draft send and move (FR-R13). Never subject, body or addresses.
	RecipientCount int
	RecipientHash  string
	MessageID      string
	// Warnings are non-fatal notes such as "ledger_update_failed" or
	// "probe=inconclusive".
	Warnings []string
}

// AuditSink records one entry per command. Record returns the error the
// command should act on: pass the command's own error in and return it; in
// block mode a failed write is joined to it (maps to audit.Logger.Handle).
type AuditSink interface {
	Record(ctx context.Context, e AuditEntry, actionErr error) error
}

// ----------------------------------------------------------------------------
// Dependencies and the use-case facade
// ----------------------------------------------------------------------------

// RunInfo identifies the calling agent and run. AgentID is substituted for
// {agent_id} in the footer and written as X-Agent-Id; RunID as X-Agent-Run.
type RunInfo struct {
	AgentID string
	RunID   string
}

// Deps is everything the use cases need. WS1 builds usecase.New(Deps) and
// returns Commands. Nil Probe and Quarantine are allowed (feature off); every
// other field is required. Filters is keyed by ContentFilter.Name().
type Deps struct {
	Reader     MailReader
	Writer     MailWriter
	Probe      SentItemsProbe
	Ledger     Ledger
	Quarantine QuarantineStore
	Policy     PolicyProvider
	Filters    map[string]ContentFilter
	Audit      AuditSink
	Clock      Clock
	Run        RunInfo
	// PolicyPath is the policy file in force; whoami reports it (FR-R2).
	PolicyPath string
}

// Commands is the use-case surface the CLI (WS3) calls and the selftest probe
// drives. Every method: loads policy, checks the mailbox once per run (FR-002),
// applies policy, calls ports, writes ONE audit entry, and returns domain
// errors (never panics, never returns bodies in error text).
type Commands interface {
	Whoami(ctx context.Context) (WhoamiResult, error)
	ListFolders(ctx context.Context) ([]domain.Folder, error)
	ListMessages(ctx context.Context, r ListRequest) (domain.Page[domain.MessageSummary], error)
	GetMessage(ctx context.Context, r GetRequest) (domain.Message, error)
	SearchMessages(ctx context.Context, r SearchRequest) (domain.Page[domain.MessageSummary], error)

	Send(ctx context.Context, r SendRequest) (domain.SendResult, error)
	Reply(ctx context.Context, r ReplyRequest) (domain.SendResult, error)
	CreateDraft(ctx context.Context, r DraftRequest) (domain.Draft, error)
	ListDrafts(ctx context.Context, r DraftListRequest) (domain.Page[domain.MessageSummary], error)
	SendDraft(ctx context.Context, r SendDraftRequest) (domain.SendResult, error)
	DeleteDraft(ctx context.Context, draftID string) error

	MarkRead(ctx context.Context, messageID string, read bool) error
	Move(ctx context.Context, r MoveRequest) (MoveResult, error)

	ListAttachments(ctx context.Context, messageID string) ([]domain.Attachment, error)
	GetAttachment(ctx context.Context, r AttachmentRequest) (AttachmentResult, error)
}

// WhoamiResult is the whoami payload.
type WhoamiResult struct {
	Mailbox string
	AgentID string
	RunID   string
	Profile string
	// PolicyPath is the policy file in force (FR-R2); empty when the provider
	// does not know a path.
	PolicyPath string
	Limits     domain.Limits
	SendMode   domain.SendMode
	External   domain.ExternalMode
	MaxTotal   int
	Rate       domain.SendRate
}

// ListRequest is `mail list`. Folder is a name; empty means inbox.
type ListRequest struct {
	Folder     string
	UnreadOnly bool
	From       string
	Since      time.Time
	Limit      int
	PageToken  string
}

// GetRequest is `mail get`. BodyFormat is domain.BodyText (default) or
// domain.BodyNone. MaxBytes zero means policy read.max_body_bytes; it can only
// lower the policy bound, never raise it.
type GetRequest struct {
	ID         string
	BodyFormat domain.BodyFormat
	MaxBytes   int
}

// SearchRequest is `mail search`.
type SearchRequest struct {
	Query     string
	Folder    string
	Limit     int
	PageToken string
}

// SendRequest is `mail send`. Raw user input: the use case validates it
// (CRLF/header injection, empty fields, duplicate/case-variant recipients).
// Body is already resolved from --body or --body-file by the CLI.
type SendRequest struct {
	To, Cc, Bcc    []string
	Subject        string
	Body           string
	DryRun         bool
	IdempotencyKey string
	// HasAttachments is true if the caller asked for attachments (always
	// refused unless policy allows); the CLI has no flag today, kept so the
	// refusal is testable.
	HasAttachments bool
}

// ReplyRequest is `mail reply`. All=true is `--all` and is denied by default.
type ReplyRequest struct {
	MessageID      string
	Body           string
	All            bool
	DryRun         bool
	IdempotencyKey string
}

// DraftRequest is `mail draft create`.
type DraftRequest struct {
	To, Cc, Bcc []string
	Subject     string
	Body        string
}

// DraftListRequest is `mail draft list`.
type DraftListRequest struct {
	Limit     int
	PageToken string
}

// SendDraftRequest is `mail draft send`. The draft is re-validated against
// policy at send time (recipients may have been edited).
type SendDraftRequest struct {
	DraftID        string
	DryRun         bool
	IdempotencyKey string
}

// MoveRequest is `mail move`. Folder is a name; Deleted Items is refused.
type MoveRequest struct {
	MessageID string
	Folder    string
}

// MoveResult reports the moved message.
type MoveResult struct {
	NewID  string
	Folder domain.Folder
}

// AttachmentRequest is `attachment get`. OutDir must resolve inside policy
// read.attachments.out_dir.
type AttachmentRequest struct {
	MessageID    string
	AttachmentID string
	OutDir       string
}

// AttachmentResult reports a quarantined attachment. Path is where it was
// saved; the content is never opened.
type AttachmentResult struct {
	Attachment domain.Attachment
	Path       string
	Written    int64
}
