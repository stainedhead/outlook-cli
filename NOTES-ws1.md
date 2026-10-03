# NOTES from ws1 (domain + use cases)

Requested changes to shared files (worked around locally):

1. `domain.RawMessage` has no `Bcc` field. `SendDraft` re-validates To and Cc,
   body size, content filters and the recipient rules, but cannot see Bcc added
   to a draft by a human or another tool. Suggested: add `Bcc []Address` to
   `RawMessage` (adapter fills it from `bccRecipients`) and have `sendDraft`
   pass `BccRequested: len(raw.Bcc) > 0`.
2. `domain.Reply` has no headers/subject, so `mail reply` cannot add the
   `X-Agent-*` headers or the subject prefix (Graph builds the reply itself);
   only the footer is applied. The dry-run rendering shows what is applied.
   Suggested if wanted: `Reply.InternetHeaders []Header`.
3. `PolicyProvider` / WS4 expectations (documented in code):
   - `Limits.MaxWritesPerRun <= 0` DENIES all writes (zero value denies).
     The loader should default it (the PRD sample has 20) or require it.
   - `Limits.MaxResults <= 0` falls back to 25; `Read.MaxBodyBytes <= 0` to
     16000; `Send.BodyMaxBytes <= 0` to 20000. `Send.Rate` zero = no limit.
   - `Recipients.MaxTotal <= 0` denies every send.
   - `Read.DefangLinks=false` really disables defanging: the loader should
     default it to true.
   - Every name in `Send.ContentFilters` must exist in `Deps.Filters`, else
     the send is refused with rule `send.content_filter.<name>`.
   - `Read.Folders` entries match a folder by display name or well-known alias,
     case-insensitively; they also bound `mail move` targets and the folders
     whose messages may be read, marked, moved or replied to.
4. Ledger: entries completed for drafts created with an idempotency key count in
   `SentSince` (the port has no distinction). Keyless sends are recorded under
   a generated `auto-<run>-<nanos>-<n>` key so rate caps see them.
5. Adapter contract relied on: `RawMessage.ParentFolderID` MUST be set by the
   Graph adapter (message reads are refused when it is empty or not a readable
   folder id); `Folder.WellKnown` should be set for Deleted Items.
