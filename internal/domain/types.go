package domain

import "time"

// SenderTrust is the sender_trust heuristic of PRD section 10. It compares the
// sender's domain with Policy.InternalDomains. It is advisory and is NEVER an
// authorization decision (the sender address can be spoofed).
type SenderTrust string

// The sender_trust values.
const (
	// TrustInternal: the sender domain is one of Policy.InternalDomains.
	TrustInternal SenderTrust = "internal"
	// TrustExternal: the sender has a domain that is not internal.
	TrustExternal SenderTrust = "external"
	// TrustUnknown: no parsable sender address.
	TrustUnknown SenderTrust = "unknown"
)

// Address is one mail address with an optional display name.
//
// Name is untrusted free text (a sender can set any display name). Address is
// stored lower-cased and trimmed by constructors in WS1; compare addresses and
// domains case-insensitively.
type Address struct {
	Address string
	Name    string
}

// Profile is the signed-in mailbox as reported by Graph GET /me.
//
// ASSUMPTION(unverified against a real tenant): Mail may be empty for some
// accounts, in which case UserPrincipalName is the mailbox identity.
type Profile struct {
	Mail              string
	UserPrincipalName string
}

// WellKnown names a mail folder Graph addresses by alias.
//
// ASSUMPTION(unverified against a real tenant): the alias spellings below are
// the Graph v1.0 well-known folder names.
type WellKnown string

// The well-known folders the CLI cares about.
const (
	WellKnownNone         WellKnown = ""
	WellKnownInbox        WellKnown = "inbox"
	WellKnownDrafts       WellKnown = "drafts"
	WellKnownSentItems    WellKnown = "sentitems"
	WellKnownDeletedItems WellKnown = "deleteditems"
	WellKnownArchive      WellKnown = "archive"
	WellKnownJunk         WellKnown = "junkemail"
	WellKnownOutbox       WellKnown = "outbox"
)

// Folder is a mailbox folder.
type Folder struct {
	// ID is the opaque Graph folder id.
	ID string
	// Name is the display name (for example "Processed").
	Name string
	// WellKnown is set when the folder is a well-known one; adapters MUST set
	// it so use cases can refuse moves to Deleted Items.
	WellKnown WellKnown
	// UnreadCount and TotalCount are item counts.
	UnreadCount int
	TotalCount  int
}

// Attachment is attachment metadata. Content is never part of this type.
//
// Name is untrusted free text.
type Attachment struct {
	ID          string
	Name        string
	ContentType string
	Size        int64
	IsInline    bool
	// Downloadable is set by the use case from policy (read.attachments); it
	// is false by default. Adapters leave it false.
	Downloadable bool
}

// Link is a hyperlink extracted from a message body, listed separately from
// the text. URL is defanged (hxxps://, [.] style per WS1) and Domain is the
// host of the original link in lower case. Images are never fetched.
type Link struct {
	Text   string
	URL    string
	Domain string
}

// BodyFormat is the format of a message body.
type BodyFormat string

// The body formats.
const (
	BodyNone BodyFormat = "none"
	BodyText BodyFormat = "text"
	BodyHTML BodyFormat = "html"
)

// RawBody is a body as the adapter received it (before bounds, HTML to text
// and link extraction). The adapter asks Graph for text with
// Prefer: outlook.body-content-type="text"; if Graph still returns HTML,
// Format is BodyHTML and the use case converts it.
type RawBody struct {
	Format  BodyFormat
	Content string
}

// Body is the processed body delivered to the caller. Text is untrusted.
type Body struct {
	// Format is BodyText or BodyNone (BodyHTML never leaves the use case).
	Format BodyFormat
	Text   string
	// Truncated is true when Text was cut to max_body_bytes / --max-bytes.
	Truncated bool
}

// AuthResults carries SPF/DKIM/DMARC results when available. Advisory only.
//
// ASSUMPTION(unverified against a real tenant): Graph exposes
// Authentication-Results through internet message headers.
type AuthResults struct {
	SPF   string
	DKIM  string
	DMARC string
}

// MessageSummary is the list form of a message: no body, no preview.
type MessageSummary struct {
	ID             string
	ConversationID string
	Received       time.Time
	From           Address
	To             []Address
	// Subject is untrusted free text.
	Subject        string
	SenderTrust    SenderTrust
	IsRead         bool
	IsDraft        bool
	HasAttachments bool
}

// RawMessage is what MailReader.GetMessage returns: the adapter's view,
// unprocessed. The use case turns it into a Message.
type RawMessage struct {
	MessageSummary
	Cc []Address
	// Bcc is filled by the adapter from the message's bccRecipients (only
	// meaningful for drafts) so a send-time re-validation sees them.
	Bcc []Address
	// ReplyTo and Sender are the Reply-To and Sender header addresses (FR-R4).
	// A reply via Graph is addressed to ReplyTo when it is set (ASSUMPTION,
	// unverified against a real tenant). Empty when absent.
	ReplyTo           []Address
	Sender            Address
	Body              RawBody
	Attachments       []Attachment
	InternetMessageID string
	// InternetHeaders are returned only when the adapter requested them
	// (for Authentication-Results). May be nil.
	InternetHeaders []Header
	// ParentFolderID identifies the folder holding the message.
	ParentFolderID string
}

// Message is the processed single-message form returned by the get use case.
// Subject, Body.Text, Address.Name and Attachment.Name are untrusted.
type Message struct {
	MessageSummary
	Cc          []Address
	Body        Body
	Links       []Link
	Attachments []Attachment
	AuthResults *AuthResults
}

// Header is an internet message header.
//
// ASSUMPTION(unverified against a real tenant): custom x- headers
// (X-Agent-Id, X-Agent-Run) are accepted on created messages.
type Header struct {
	Name  string
	Value string
}

// OutgoingMessage is a fully validated, policy-approved message ready to be
// sent or saved as a draft. It is built by the use case (subject prefix,
// footer and agent headers already applied); adapters send it verbatim and
// never alter content. There is deliberately no field for attachments or for
// a From address: the sender is always the signed-in mailbox.
type OutgoingMessage struct {
	To      []Address
	Cc      []Address
	Bcc     []Address
	Subject string
	// Body is plain text.
	Body            string
	InternetHeaders []Header
}

// Reply is a reply to the sender of an existing message (never reply-all).
type Reply struct {
	// MessageID is the message being answered.
	MessageID string
	// Body is plain text, footer already applied.
	Body string
	// InternetHeaders are the X-Agent-* headers. ASSUMPTION(unverified against
	// a real tenant): sent in the reply call's "message" parameter.
	InternetHeaders []Header
}

// Draft identifies a saved draft.
type Draft struct {
	ID string
	MessageSummary
}

// SendResult describes a completed send.
type SendResult struct {
	// DryRun is true when nothing was sent.
	DryRun bool
	// AlreadySent is true when the idempotency ledger recorded an earlier
	// successful send for the same key (no second POST was made).
	AlreadySent bool
	// DraftID is set when the message was saved as a draft instead of sent
	// (external: draft_only).
	DraftID string
	// IdempotencyKey echoes the key used, if any.
	IdempotencyKey string
	// Rendered is the final message (set for dry runs and for sends).
	Rendered OutgoingMessage
	// Decision is the policy decision that applied.
	Decision Decision
	// AlreadyDrafted is true when the key replayed an earlier draft creation
	// (DraftID holds the draft); AlreadySent stays false (FR-R8).
	AlreadyDrafted bool
	// PrefixApplied reports whether the policy subject prefix and footer are
	// on the message that is (or would be) sent. A draft sent via
	// `draft send` is never rewritten, so it is false when the draft lacks
	// them (FR-R9).
	PrefixApplied bool
	// Warnings are non-fatal notes: a ledger update that failed after a real
	// send ("ledger_update_failed") or an inconclusive probe
	// ("probe=inconclusive").
	Warnings []string
}

// Page is one page of results. NextPageToken is empty on the last page. The
// token is opaque to callers; adapters encode the Graph continuation (skip
// token / next link) into it and validate it on the way back (invalid token
// is a usage error, exit 2).
type Page[T any] struct {
	Items         []T
	NextPageToken string
}

// MessageQuery selects messages for listing (PRD: mail list).
type MessageQuery struct {
	// FolderID is a resolved folder id (use cases resolve names first).
	FolderID   string
	UnreadOnly bool
	// From filters by sender address (exact, case-insensitive).
	From string
	// Since keeps messages received at or after this instant; zero is no filter.
	Since     time.Time
	Limit     int
	PageToken string
}

// SearchQuery selects messages by free-text search (PRD: mail search).
//
// ASSUMPTION(unverified against a real tenant): Graph $search semantics.
type SearchQuery struct {
	Text string
	// FolderID optionally restricts the search; empty means the use case
	// searches each policy-allowed folder.
	FolderID  string
	Limit     int
	PageToken string
}
