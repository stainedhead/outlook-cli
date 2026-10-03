# Implementation Notes: outlook-cli (2026-10-03)

## Technical Decisions
- Composition root: `cmd/outlook/app.go`. `assemble` loads the policy first (a missing, invalid or agent-writable policy stops the run before any adapter exists), then builds the credential source (core `auth` over `newDaemonClient()`), the Graph client (`graph.New`, `/me` only), the file ledger, the quarantine store (only when `read.attachments` allows download), the audit sink (`audit.Block`) and `usecase.New`. `appConfig` lets integration tests inject a fake daemon, an httptest Graph URL and `policyfile.AllowWritable()`; production uses `prodConfig()` (env `OUTLOOK_POLICY`, `AGENT_ID`, `AGENT_RUN_ID`).
- `selftest` and the mailbox commands share `assemble`; `selftest` runs the full matrix because every write row is a dry run.
- Shared-type requests from the workstreams were applied with TDD: `domain.RawMessage.Bcc` (filled from `bccRecipients`, re-validated by `draft send` against the bcc rule) and `domain.Reply.InternetHeaders` (sent as `message.internetMessageHeaders` on the reply call; unverified).
- Policy defaults are resolved by the loader (`policyfile.Default*`): omitted `max_writes_per_run`, `max_results`, `max_body_bytes`, `body.max_bytes`, `html_to_text`, `defang_links`. Explicit values, including `defang_links: false` and `max_writes_per_run: 0`, are honoured.
- List commands emit a JSON array with a trailing `{"next_page_token": ...}` element rather than the object in `ports.go` (core output bounding cannot cut objects). `ports.go` comments describe the older object form; the CLI is authoritative.

## Edge Cases & Solutions
- A policy file or directory the agent can write is refused (exit 6) in production; tests opt out with `AllowWritable`.
- A message outside `read.folders` is refused (exit 6) because `RawMessage.ParentFolderID` is set by the adapter and compared to the readable folder ids.
- The idempotency ledger sits next to the audit log, the directory the agent user can write.

## Deviations from Plan
- NOTES-ws1..4.md were folded into `docs/unverified-assumptions.md` and `docs/requested-core-changes.md`, then deleted.
- The selftest negative probes for other mailboxes (PRD s11) need a real tenant and are not implemented (UA-4, UA-25).
- `cmd/outlook` statement coverage is 74.3%: `main`, signal wiring and `prodConfig` are exercised only by running the binary. Domain and use-case coverage (the gate) are 99.6% and 97.9%.

## Lessons Learned
- A single stateful httptest Graph double plus the real command tree found no wiring mismatches between workstreams beyond the shared-type gaps above, which suggests the Phase A port contract was sufficient.
