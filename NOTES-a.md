# NOTES group A (fix/a)

## Needed from other groups
- B (cmd/outlook): set `graph.Config.PageTokenKey` to a per-install key provider (>=16 bytes). While nil, any list/search/draft-list page that has a next page FAILS (general error, exit 1); tokens are refused (usage). Until wired, multi-page walks do not work in the real binary.
- C (domain/types.go + usecase): add `ReplyTo []Address` and `Sender *Address` to `domain.RawMessage` and map them in `graph/dto.go` `messageDTO.raw()` using the existing `m.ReplyToAddresses()` / `m.SenderAddress()` (graph already selects `replyTo,sender`; dto decodes them). Then evaluate reply policy on Reply-To.
- C (usecase send): the idempotency probe `FindSentByKey` returns a validation-category error ONLY for HTTP 400 (treat as inconclusive); network/auth/5xx are other categories (fail closed). Probe header now equals `domain.HeaderIdempotencyKey`. Skipping the probe for draft send/reply is a use-case change.
- usecase/read.go needed no code change: folder policy is already re-resolved and re-checked on every page; added regression test only.

## Doc-impacting (for F)
- Page token format change (signed, bound to op/folder/query, per-install key; old tokens invalid). Changing flags or --folder mid-walk is refused with usage exit 2.
- Ids used as path segments: charset [A-Za-z0-9_=-], max 512; invalid -> usage exit 2 (empty id stays validation).
- Probe header: default now X-Agent-Idempotency-Key; 400 semantic as above. Unverified assumptions: Reply-To handling, header filter 400.
- `ListFolders` still follows @odata.nextLink directly (host pinned by httpx); unchanged.
