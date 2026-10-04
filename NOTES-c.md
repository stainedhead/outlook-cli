# Group C notes (doc-impacting and cross-group)

Ports / domain changes (SHA 35da530 for the port; rest in later commits):
- usecase.Ledger gains ReserveWithin(key, fp, kind, at, windows) returning (entry, created, counts, err); LedgerKind (send|draft); LedgerOverCap status; LedgerEntry.Kind. SentSince must count send-kind entries with status sent OR pending. Group D implements; adapter/ledger does not compile against the interface until then (`var _ usecase.Ledger = (*File)(nil)`).
- domain.SendResult gains AlreadyDrafted, PrefixApplied, Warnings. CLI (group E) must present them: `already_drafted`, `prefix_applied`, `warnings` (values `ledger_update_failed`, `probe=inconclusive`).
- usecase.AuditEntry gains RecipientCount, RecipientHash (32 hex of sha256 of sorted lower-cased address set), MessageID, Warnings; outcome `already_drafted` added. auditlog adapter (group B) must map them to the audit record.
- domain.Error gains HTTPStatus (+WithHTTPStatus); group A should set it on Graph errors so audit HTTPStatus is populated.
- domain.RawMessage gains ReplyTo []Address and Sender Address; group A's dto/read must populate them ($select replyTo,sender).

Behavior (for docs, group F):
- FR-R3: draft send denies attachments (send.attachments) using HasAttachments/Attachments; size and content filters run on the raw draft body (HTML markup, attributes, comments, beyond 2 MiB), plus the converted text and subject.
- FR-R4: reply recipients = Reply-To when set else From; policy evaluated on them. Assumption (unverified against a real tenant): Graph /reply addresses Reply-To. Add to docs/unverified-assumptions.md and M0 checklist. Sender is not used.
- FR-R6: probe skipped for draft send and reply; draft-send dry run shows no X-Agent headers (POST .../send cannot add them). A validation-category (HTTP 400) probe error is inconclusive (send proceeds on ledger, warning probe=inconclusive); other probe errors fail closed. Probe header name: writer uses domain.HeaderIdempotencyKey; group A must make the probe use the same constant. TestProbeErrorFailsClosedAndFreesKey kept (non-400 path).
- FR-R8: rate check + reservation atomic in ledger; pending sends count (conservative); keyed drafts are kind=draft, do not consume budget, replay as already_drafted; Complete failure after send is a warning.
- FR-R9: draft with empty From refused (send and delete); second read before POST compares recipients, subject, raw body, author and attachment state; mismatch -> conflict exit 7, key freed. Residual window: between that read and POST. Drafts are sent as-is; prefix_applied reports whether prefix and footer are present.
- FR-R13: audit has recipient count/hash and message/draft id; never subject/body/addresses.
