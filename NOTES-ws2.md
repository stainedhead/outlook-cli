# NOTES-ws2: Graph adapter and ledger

No change to shared files (types.go, policy_types.go, errors.go, ports.go) is needed.

## Placement decision
`QuarantineStore` is implemented in `internal/adapter/ledger` (`ledger.Quarantine{Root}`), because WS2 owns only `graph/**` and `ledger/**`. The composition root (WS3) builds it with `Root` = policy `read.attachments.out_dir`; with an empty `Root` only `outDir` itself is resolved (no containment check), so set `Root`. Exclusive create 0600 with O_NOFOLLOW, name sanitised by `ledger.SanitizeName`, size cap with partial-file removal (policy_denied), existing name is a conflict (never overwritten).

## Wiring for WS3
- `graph.New(graph.Config{Refresher: authorizer, IdempotencyHeader: "X-Agent-Run" (optional)})`; the returned `*graph.Client` implements `MailReader`, `MailWriter`, `SentItemsProbe`. Defaults: base URL `https://graph.microsoft.com/v1.0`, host allowlist = that host.
- `ledger.New(ledger.Config{Path: ...})` implements `Ledger`. Unix only (flock).
- `Page.NextPageToken` is base64url of Graph's `@odata.nextLink`; a token not pointing at the configured host and `/me/` is a usage error (exit 2).

## Assumptions for the docs workstream (all "unverified against a real tenant")
Every endpoint is `/v1.0/me/...`; `/users/{id}` is never used (a test asserts it for all methods).

| Port method | Request | Assumed behaviour |
|---|---|---|
| Me | `GET /me?$select=mail,userPrincipalName` | `mail` may be null; UPN then identifies the mailbox |
| ListFolders | `GET /me/mailFolders?$top=100&$select=id,displayName,unreadItemCount,totalItemCount`, follows `@odata.nextLink` | top-level folders only; well-known flag derived by `GET /me/mailFolders/{alias}?$select=id` for inbox, drafts, sentitems, deleteditems, archive, junkemail, outbox (404 skipped, cached per process) |
| ResolveFolder | alias: `GET /me/mailFolders/{alias}`; else match displayName (case-insensitive) in ListFolders | aliases accept spaces/case ("Sent Items"); "junk" means junkemail |
| ListMessages | `GET /me/mailFolders/{id}/messages?$top&$orderby=receivedDateTime desc&$select=id,conversationId,receivedDateTime,from,toRecipients,subject,isRead,isDraft,hasAttachments[&$filter=...]` | `$filter` conditions: `receivedDateTime ge <t>` (always first, epoch when no --since, to avoid InefficientFilter with `$orderby`), `isRead eq false`, `from/emailAddress/address eq '<addr>'`; limit default 25, max 1000 |
| SearchMessages | `GET /me/messages?$search="<text>"&$top&$select` (or `/me/mailFolders/{id}/messages`) | no `$orderby`/`$filter` with `$search`; relevance order; paging by nextLink |
| GetMessage | `GET /me/messages/{id}?$select=<summary>,body,ccRecipients,internetMessageId,parentFolderId[,internetMessageHeaders]&$expand=attachments($select=id,name,contentType,size,isInline)` | header `Prefer: outlook.body-content-type="text"` returns a text body; any other contentType is treated as HTML. Internal senders may appear as X.500 addresses |
| ListAttachments | `GET /me/messages/{id}/attachments?$select=id,name,contentType,size,isInline` | metadata only |
| OpenAttachment | metadata `GET .../attachments/{aid}?$select=...` then `GET .../attachments/{aid}/$value` | `$value` streams raw bytes for file attachments |
| ListDrafts | `GET /me/mailFolders/drafts/messages?$orderby=lastModifiedDateTime desc` | "drafts" alias works as a folder id |
| SendMail | `POST /me/sendMail {"message":{subject,body{contentType:"Text",content},toRecipients,ccRecipients,bccRecipients,internetMessageHeaders},"saveToSentItems":true}` | 202 no body; custom headers must start with `x-`/`X-`, and Graph limits their number; no `from` field ever |
| CreateDraft | `POST /me/messages` (message resource) | 201 with the saved message and `id` |
| SendDraft | `POST /me/messages/{id}/send` | 202 no body |
| DeleteDraft | `DELETE /me/messages/{id}` | 204 |
| ReplyToSender | `POST /me/messages/{id}/reply {"comment": "<body>"}` | 202; comment is inserted above the quoted original; may be treated as HTML. No reply-all call exists |
| SetRead | `PATCH /me/messages/{id} {"isRead": bool}` | 200 |
| MoveMessage | `POST /me/messages/{id}/move {"destinationId": "<folder id>"}` | 201 with the moved message; its id is the new id |
| FindSentByKey | `GET /me/mailFolders/sentitems/messages?$filter=internetMessageHeaders/any(h:h/name eq '<hdr>' and h/value eq '<key>')&$top=1&$select=id` | filtering on `internetMessageHeaders` may be unsupported (HTTP 400 => returned as validation error; the use case must treat as inconclusive); header name defaults to `X-Idempotency-Key`, configurable |

Error and header assumptions:
- 403 vendor code: Graph's real error code is in the JSON body, which core `httpx` never offers. The adapter reports the first present of `x-ms-error-code`, `request-id`, `client-request-id` (the last two are correlation ids, not codes). See docs/requested-core-changes.md item 1.
- Status mapping for statuses httpx lets through: 400 validation (exit 9), 404 and 410 not_found (5), 409 and 412 conflict (7), other 4xx and 5xx general (1). 401/403/429/502/503/504 come from httpx (3/4/8). Retry-After honoured on 429/503 by httpx for reads.
- Writes are never replayed by httpx (POST/PATCH not marked safe). DELETE is idempotent to httpx and may be retried. `domain.NotSent` wraps: local validation, any 4xx except 408, 401 (second), 403, 429, auth/daemon failures (no request made). Ambiguous (no NotSent): 408, 5xx incl. 502/503/504, timeouts, dropped connections, context cancel. Note: a POST gets one 401 refresh-and-replay by httpx (the first 401 means the request was rejected before processing).
- Request id and bodies never appear in errors.

## Ledger file format (docs-ready)
`<path>` JSON `{"version":1,"entries":{"<key>":{"fingerprint","status":"pending|sent|failed","at","ref_id","count"}}}`, dir 0700, file 0600, lock file `<path>.lock`, atomic temp+fsync+rename. Corrupt, unreadable, or unknown-version file fails closed (error, nothing sent; hint tells a human to inspect). Sent/failed entries older than 90 days (measured against the `at` passed in) are pruned on write; pending entries are never pruned.
`Reserve` returns status `new` for an unseen key and `pending` when it resets a failed key; `LedgerEntry.Count` is never set by this adapter (the port has no way to pass it).
