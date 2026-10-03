# Architectural Decision Record

One entry per decision. Status values: Accepted, Superseded, Proposed. Questions still open in the PRD (section 15) are not decisions and are not listed here.

## ADR-1: Depend on agent-cli-core v0.1.0

- Status: Accepted
- Context: The envelope, exit codes, auth, policy, audit, HTTP client, selftest and skill generation are shared by all the CLIs in the set. The core is tagged v0.1.0.
- Decision: `go.mod` requires `github.com/stainedhead/agent-cli-core v0.1.0`. No `replace` directive and no pseudo-version. The private-module fetch (`GOPRIVATE=github.com/stainedhead/*`) is already configured in CI. The core is never edited here.
- Consequences: Behaviour and exit codes match the other CLIs. Gaps in the core are recorded in `requested-core-changes.md` with a local workaround, and fixed upstream. Bumping the version is an ordinary pull request that must pass CI. `agent-okta-d` is not a dependency.

## ADR-2: Daemon client is a stub in the composition root

- Status: Accepted
- Context: The token source needs a client for `agent-okta-d`, which has not published `pkg/client`.
- Decision: `cmd/outlook` has `newDaemonClient()` returning an implementation of the core's client interface that reports the daemon as unavailable (exit 3, with the core's message naming the socket). Everything downstream uses the core interface. Detail: `adr-daemon-client-stub.md`. The real adapter is deferred until `agent-okta-d` publishes a Go client; the daemon socket path is an unverified assumption.
- Consequences: The CLI builds, tests and runs without the daemon; any token request fails closed. The swap to the real adapter is a single function change. Authentication paths are tested with the core's `authtest.Fake`.

## ADR-3: Delegated access to /me only

- Status: Accepted
- Context: The agent is a licensed Entra user with its own mailbox, using delegated Graph tokens. Delegated scopes decide which mailbox, not which recipients.
- Decision: The CLI calls only `/me/...` and has no mailbox parameter. Only `Mail.ReadWrite` and `Mail.Send` are expected; `.Shared` scopes and `MailboxSettings.ReadWrite` are not. The policy `mailbox` value is compared with `GET /me` once per run, and a mismatch is refused. The HTTP client allows only the Graph host. No rules, forwarding or settings commands exist.
- Consequences: Cross-mailbox access is not reachable from the CLI, and the server-side 403 is checked by `selftest`. Recipient control relies on policy plus Exchange mail flow rules owned by the Exchange team. Unverified against a real tenant (UA-4).

## ADR-4: Inbound mail is untrusted content

- Status: Accepted
- Context: Email is a strong prompt-injection and exfiltration path.
- Decision: Subject, body text, display names, attachment names and link text are untrusted. HTML is converted to text in the use case, links are listed separately and defanged, images are not fetched, and attachment download is off by default (opt-in, quarantine directory, type and size limits, never opened). Only the CLI presenter wraps these fields with the core `output.Untrusted` marker. `sender_trust` is advisory and never authorizes anything. The audit log carries no bodies by default.
- Consequences: Inner layers handle plain domain values and the wrapping happens at one place, which the architecture test and presenter tests guard. The core's `Untrusted` JSON shape differs slightly from the PRD example (change request 3).

## ADR-5: Idempotency ledger with fail-closed semantics

- Status: Accepted
- Context: A retried or crashed send must not email twice, and every CLI run is a new process, so in-memory state is not enough. The core policy rate limits are per process.
- Decision: A local file ledger keyed by `--idempotency-key` with `Reserve`, `Complete` and `Fail`. `Fail` is only recorded when the adapter proves nothing was sent (`domain.NotSent`). An ambiguous failure leaves the entry pending, and later attempts with the same key fail with a conflict (exit 7) rather than risk a duplicate. The ledger also supplies `SentSince` for hourly and daily caps. Writes are never auto-retried at the HTTP layer. A Sent Items probe is an optional P1 addition.
- Consequences: Safety is preferred over availability. A stuck pending entry needs an operator to clear it. The ledger is a local file, so it does not protect against two hosts using one mailbox.

## ADR-6: Policy as strict, fail-closed configuration

- Status: Accepted
- Context: Guardrails (recipients, domains, caps, content filters, folders, write modes) must be reviewable and not agent-controlled.
- Decision: The policy is a typed data model in `internal/domain`, loaded from a file by the policy adapter. The load is strict (unknown fields rejected), fails closed (missing or invalid means deny), and refuses a file the agent can write. Evaluation is a pure domain function with one decision per send: allow, dry_run_only, draft_only or deny. Every decision records the deciding rule id in the audit entry. Inbound content filters report findings without the matched text.
- Consequences: The core `policy` engine is generic and lacks typed recipient rules, so the typed model sits beside it (change request 4). Policy changes are made by configuration management, not by the tool or the agent. The policy is client-side only; Exchange rules remain the server-side control.

## ADR-7: Clean Architecture with an enforced import graph

- Status: Accepted
- Decision: `cmd/outlook` composition root, adapters, use cases with ports, then domain, with dependencies pointing inward. `internal/archtest` fails the build on violations. The use-case layer owns the port interfaces, and a single `Clock` port is the only time source in inner layers.
- Consequences: Tests use fakes and `httptest`; no test touches a real network or sends mail.

## ADR-8: List commands return a JSON array with a trailing page-token element

- Status: Accepted
- Context: Core output bounding cuts whole array items but cannot cut an object (`ErrBoundTooSmall`), and the core `Meta` has no continuation token.
- Decision: `mail list`, `mail search`, `mail draft list`, `folder list` and `attachment list` emit `data` as an array. A Graph continuation is a final element `{"next_page_token":"..."}`. When output is truncated that element is cut too and the caller resumes with `--offset`.
- Consequences: Differs from the object form shown in PRD section 10. Removable if the core adds `Meta.next_page_token` (requested-core-changes items 2 and 13).

## ADR-9: Audit failures block commands

- Status: Accepted
- Context: A write that goes unrecorded defeats attribution.
- Decision: The audit sink runs in core `audit.Block` mode for every command, reads included. The audit directory must be writable by the agent user, and the idempotency ledger is stored in that directory.
- Consequences: An unwritable audit path stops the tool; this is a configuration fault surfaced early. The policy directory must be read-only while the audit directory is writable (UA-23, UA-24).

## ADR-10: Selftest runs the whole matrix as dry-runs

- Status: Accepted
- Decision: `outlook selftest` runs 13 allow/deny rows. Write rows are dry-run requests and denial rows are refused before any Graph write, so nothing is sent. Negative cross-mailbox probes (PRD section 11) are not implemented because they need a real tenant (UA-25).
- Consequences: With the stub daemon every row fails with exit 1 (the rows report the daemon error). The selftest passes only against a working backend or fakes.
