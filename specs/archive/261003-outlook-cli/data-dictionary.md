# Data Dictionary: outlook-cli

Source of truth is the code: `internal/domain/types.go`, `internal/domain/policy_types.go`, `internal/usecase/ports.go`. Fields marked U are untrusted free text.

## Entities (internal/domain)

| Type | Fields | Notes |
|---|---|---|
| `Address` | Address, Name (U) | address lower-cased, compared case-insensitively |
| `Profile` | Mail, UserPrincipalName | from `GET /me`; checked against `Policy.Mailbox` |
| `Folder` | ID, Name, WellKnown, UnreadCount, TotalCount | `WellKnown` set by the adapter; used to refuse Deleted Items |
| `MessageSummary` | ID, ConversationID, Received, From, To, Subject (U), SenderTrust, IsRead, IsDraft, HasAttachments | list form, no body |
| `RawMessage` | MessageSummary, Cc, Body (`RawBody`), Attachments, InternetMessageID, InternetHeaders, ParentFolderID | adapter output |
| `Message` | MessageSummary, Cc, Body (Format, Text (U), Truncated), Links, Attachments, AuthResults | use-case output |
| `Attachment` | ID, Name (U), ContentType, Size, IsInline, Downloadable | metadata only; `Downloadable` set from policy |
| `Link` | Text (U), URL (defanged), Domain | extracted from body, listed separately |
| `AuthResults` | SPF, DKIM, DMARC | advisory, unverified assumption |
| `Header` | Name, Value | custom `X-Agent-Id`, `X-Agent-Run` (unverified assumption) |
| `OutgoingMessage` | To, Cc, Bcc, Subject, Body, InternetHeaders | no From, no attachments by construction |
| `Reply` | MessageID, Body | reply to sender only |
| `Draft` | ID, MessageSummary | |
| `SendResult` | DryRun, AlreadySent, DraftID, IdempotencyKey, Rendered, Decision | |
| `Page[T]` | Items, NextPageToken | opaque token, invalid -> usage error |
| `MessageQuery`, `SearchQuery` | folder id, filters, Limit, PageToken | |

## Value objects and enumerations

| Name | Values |
|---|---|
| `SenderTrust` | internal, external, unknown (heuristic, never authorization) |
| `WellKnown` | inbox, drafts, sentitems, deleteditems, archive, junkemail, outbox |
| `BodyFormat` | none, text, html (html never leaves the use case) |
| `Verb` | read, send, write |
| `ExternalMode` | deny, draft_only, allow |
| `SendMode` | allow, dry_run_only, deny |
| `DecisionMode` | allow, dry_run_only, draft_only, deny |

## Policy (PRD section 9, parsed)

`Policy{Profile, Mailbox, InternalDomains, Read ReadPolicy, Send SendPolicy, Limits, AuditPath}`; `ReadPolicy{Folders, MaxBodyBytes, HTMLToText, DefangLinks, Attachments AttachmentPolicy}`; `AttachmentPolicy{Download, AllowTypes, MaxBytes, OutDir}`; `SendPolicy{Mode, Recipients, ReplyAll, Attachments, SubjectPrefix, Footer, BodyMaxBytes, ContentFilters, Rate}`; `RecipientPolicy{AllowDomains, AllowAddresses, External, MaxTotal, BccAllowed}`; `SendRate{PerHour, PerDay}`; `Limits{MaxResults, MaxWritesPerRun}`. The zero value denies everything.

`Decision{Mode, RuleID, Reason, RetryAfter}`; audit string is `mode` or `mode:rule-id`.

## Errors

`domain.Error{Cat, Code, Message, HintMsg, RuleID, Cause}` implements core `output.CategoryError` and `Hinter`. Constructors: usage 2, validation 9, policy denied 6, not found 5, conflict 7, forbidden 4, auth 3, rate limited 8, general 1. `ErrNotSent` / `NotSent(err)` mark a provably unsent write.

## Ports (internal/usecase)

`Clock`, `MailReader`, `MailWriter`, `SentItemsProbe`, `Ledger` (`LedgerEntry{Key, Fingerprint, Status, At, RefID, Count}`, status new, pending, sent, failed), `QuarantineStore`, `PolicyProvider`, `ContentFilter` (`FilterFinding{Filter, Kind, Offset}`), `AuditSink` (`AuditEntry`). Request types: `ListRequest`, `GetRequest`, `SearchRequest`, `SendRequest`, `ReplyRequest`, `DraftRequest`, `DraftListRequest`, `SendDraftRequest`, `MoveRequest`, `AttachmentRequest`; results `WhoamiResult`, `MoveResult`, `AttachmentResult`.

## API types

Graph request and response DTOs live only in `internal/adapter/graph`. YAML DTOs live only in `internal/adapter/policyfile`. Ledger file format lives only in `internal/adapter/ledger`.
