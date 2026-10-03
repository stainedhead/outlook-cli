# Requested changes to agent-cli-core

Gaps found while building `outlook` against `agent-cli-core v0.1.0`. The core is never edited from this repository; each item records the workaround in use. Source: Phase A review of the core's public API.

| # | Area | Gap in v0.1.0 | Workaround here | Priority |
|---|---|---|---|---|
| 1 | `httpx` | `Config.VendorCode` receives only response headers, but Graph puts its error code (for example `ErrorAccessDenied`) in the JSON body. AUTH-3 asks for the Graph error code on a 403. | Report the `request-id` / `x-ms-*` header value if present; otherwise a generic 403 without a code. Unverified against a real tenant. | Medium |
| 2 | `output` | `Meta` has `next_offset` (item index) but no continuation token. Graph paging uses opaque tokens (`--page-token`). | Return `next_page_token` inside `data`. | Medium |
| 3 | `output` | `Untrusted` marshals as `{"untrusted":true,"value":...}`. The PRD example shows `text`, `format`, `truncated` next to `untrusted`. | Use `output.Untrusted` and put `format` / `truncated` in sibling fields. The PRD example is adjusted to match in the skill. | Low |
| 4 | `policy` | The engine is a generic verb/resource/field rule set with its own schema. It has no typed recipient, domain or mailbox rules, and rate limits are in memory per process (every CLI run is a new process). | Typed policy modelled in `internal/domain`; persistent send history in the idempotency ledger. | Medium |
| 5 | `policy` | The agent-writable-file check exists only inside `policy.Load`; there is no standalone function. | Small stat-based check in the policy-file adapter. | Low |
| 6 | clock | `internal/clock` is not importable; `policy`, `audit` and `httpx` each declare a small clock interface. | `outlook` declares its own `Clock` and adapts it where needed. | Low |
| 7 | `auth` | No real daemon adapter (known; waits for `agent-okta-d` `pkg/client`). | `newDaemonClient()` stub in `cmd/outlook` returns `*auth.UnreachableError` (exit 3). | Known |
| 8 | `audit` | `Record` has no field for the deciding rule id. | Encode as `PolicyDecision` = `deny:<rule-id>`. | Low |
| 9 | `docgen` | `CommandTree.Commands` is flat. | Nested commands are named with a space (`mail send`). | Low |

## Additional gaps seen when reading the core against the PRD (docs-skeleton review)

These come from reading the PRD requirements against the core's documented behaviour, not from running code. Confirm each when the adapters are implemented and move it to the table above or delete it.

| # | Area | Possible gap | Working assumption | Priority |
|---|---|---|---|---|
| 10 | `httpx` | PRD AUTH-2 asks for exactly one forced refresh and retry on 401. Confirm the core retries once only and never retries a non-idempotent POST. | Wrap writes so they are never retried; test with an `httptest` server. | Medium |
| 11 | `selftest` | PRD s11 negative probes (other mailbox returns 403) need a way for a probe to expect a 403 and treat it as a pass. Confirm the core's probe result type can express expected-denial. | Probe code maps 403 to pass inside the adapter. | Low |
| 12 | `audit` | PRD says no bodies by default; confirm the record type has no free-form field that a caller could fill with content. | Adapter passes metadata only. | Low |
