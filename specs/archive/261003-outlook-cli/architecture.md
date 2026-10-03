# Architecture: outlook-cli (2026-10-03) - Status: Phase A foundation

## Layers (dependencies point inward only)

```
cmd/outlook                      composition root: builds adapters, newDaemonClient() stub, runs CLI
  └─ internal/adapter/*          cli, graph, ledger, policyfile, auditlog, selftestcfg
       └─ internal/usecase       use cases + ports (interfaces owned here)
            └─ internal/domain   entities, policy data model, error taxonomy
```

Enforced by `internal/archtest`:
- `internal/domain` imports the standard library, itself, and `agent-cli-core/output` (only for `output.Category`).
- `internal/usecase` imports domain and `agent-cli-core/output`; no adapters.
- `internal/adapter/*` imports domain, usecase and core; adapters do not import each other; nothing under `internal/` imports `cmd/`.
- No import of `agent-okta-d` anywhere.

## Ports (internal/usecase/ports.go)

| Port | Implemented by | Purpose |
|---|---|---|
| `MailReader` | graph adapter (WS2) | `/me` reads: profile, folders, list, search, get, attachments, drafts |
| `MailWriter` | graph adapter (WS2) | send, draft create/send/delete, reply-to-sender, mark, move |
| `SentItemsProbe` | graph adapter (P1, optional) | Sent Items idempotency check (unverified assumption) |
| `Ledger` | file ledger (WS2) | Reserve / Complete / Fail by idempotency key; `SentSince` for rate caps |
| `QuarantineStore` | file adapter (WS2) | save attachment under the policy directory, never open it |
| `PolicyProvider` | policyfile (WS4) | strict, fail-closed load of the policy into `domain.Policy` |
| `ContentFilter` | policyfile (WS4) | secret / classification scans, findings without matched text |
| `AuditSink` | auditlog (WS4) | one record per command via core `audit` |
| `Clock` | composition | the only time source in inner layers |

`Commands` is the use-case facade (WS1 implements, WS3 calls, the selftest probe drives). `Deps` is its constructor input.

## Data flow

`CLI flag parse -> Commands.X -> policy load -> mailbox check (once per run) -> policy evaluation (domain) -> ports -> Graph -> domain.Message / SendResult -> CLI presenter wraps untrusted fields -> core output.Write (envelope + exit code) ; AuditSink.Record per command`.

Send path: validate (CRLF, dedupe, caps) -> policy decision (allow | dry_run_only | draft_only | deny) -> content filters -> apply prefix, footer, `X-Agent-Id` / `X-Agent-Run` -> `Ledger.Reserve` -> `MailWriter.SendMail` -> `Ledger.Complete` (or `Fail` when the adapter returns `domain.NotSent(err)`; an ambiguous failure leaves the entry pending and later attempts fail closed with a conflict).

## Error and exit-code model

All use-case errors are `domain.Error` (implements `output.CategoryError` and `output.Hinter`), so `output.FromError` and `output.ExitOf` need no mapping table. Errors from core `httpx` (429/503 -> 8, 401 -> 3, 403 -> 4) and `auth` (daemon unreachable / reauth -> 3) pass through wrapped with `%w`. Policy denial is exit 6, ledger pending is exit 7, bad page token is exit 2.

## Auth and HTTP wiring

`newDaemonClient()` (stub, exit 3) -> `auth.NewDaemonTokenSource(client, "msgraph", WithRemediation(...))` -> `auth.NewAuthorizer` -> `httpx.NewClient(Config{Refresher, AllowedHosts: graph.microsoft.com})` -> Graph adapter. The adapter never sees a token. Write POSTs are never marked safe to retry.

## Untrusted content

Untrusted fields: `Message.Subject`, `Message.Body.Text`, `Address.Name`, `Attachment.Name`, `Link.Text`. Only the CLI presenter wraps them in `output.Untrusted`. HTML is converted to text in the use case, links are extracted and defanged, images are never fetched.

## Build

`make build|test|race|cover|lint|fmt|vet|skill|cross`. Version stamped with `-ldflags -X main.version/commit/date`. Cross targets: darwin/arm64, linux/amd64, linux/arm64 (CGO off). `make skill` calls the hidden `outlook skill` command (WS3) which prints the docgen output to `dist/outlook-cli.md`.

## Decisions and open items

- ADR for the daemon-client stub: WS3/WS5 (`docs/adr`). Core gaps: `docs/requested-core-changes.md`.
- Sequence diagrams per command: added with the integration phase.
