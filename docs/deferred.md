# Deferred work

Items that are intentionally not built in this repository now, with the reason and the condition for picking them up again.

| Item | Status | Rationale | Pick up when |
|---|---|---|---|
| Calendar writes | Out of scope | The PRD puts calendar at P2 and read-only; sending invitations is another way to reach external parties and widens the policy surface. No calendar command exists. | A go/no-go memo after M5 approves it, with a policy model for attendees. |
| Windows native | Out of scope | Supported platforms are macOS and Linux (WSL is the Windows path). The release matrix has no `windows/*` target, and file-permission and socket assumptions in the daemon client are POSIX. | A decision to support the daemon on native Windows. |
| Exchange mail-flow rules, Conditional Access and DLP | Out of scope | These are tenant configuration owned by the Exchange and identity teams, not code in this repository. The CLI only checks and reports (selftest) and keeps its own client-side guardrails. | Not applicable; owners are named in PRD s15. |
| M0 real-tenant spikes | Deferred | They need a sandbox tenant, an agent user and a human to enroll. Checklist is in `m0-spike-checklist.md`. Until they run, every Graph shape stays in `unverified-assumptions.md`. | A sandbox tenant is provisioned. |
| Release workflows | Deferred | The PRD asks for signed, notarized release artifacts and cosign signatures, and the signing account and registry are open questions (PRD s16.8). Only CI exists; no secrets are added. | The open items in PRD s16.8 are decided. |
| Sent Items idempotency probe (P1) | Built, unverified | Depends on an unverified header search (UA-12, UA-30). It runs for new sends only; an HTTP 400 is inconclusive. The local ledger covers P0. | S-5 confirms the lookup. |
| Attachment download enablement | Opt-in only | Off by default; the quarantine path ships but is gated by policy. | An operator enables it in policy. |
| Root skill update and `docs` automation | Manual | Updating the root repository's skill is a manual PR (PRD s17.1). | A cross-repository token approach is agreed. |

## AGENT_OKTA_D_SOCKET ownership check

The daemon adapter is wired but the credential socket path is not checked. Apply the same trust rule as the policy file to the socket and its parent directories (owned by root or a configured trusted uid, not group/world-writable, never the agent's own uid) so that `AGENT_OKTA_D_SOCKET` cannot redirect token requests to an agent-controlled socket (FR-R2).

## Body-file path roots (OQ-3)

`--body-file` reads any regular file the agent user can read, up to 4 MiB (devices, FIFOs and directories are refused; symlinks are resolved). There is no policy setting that confines it to a directory, so a prompt-injected agent can send the content of any readable file to an allowed recipient. Policy content filters and recipient limits are the only brake. Adding `send.body_file_roots` (a list of absolute directories, containment checked after symlink resolution) is deferred pending a decision (open question OQ-3).

## Other items carried over from the review-fix pass

| Item | Reason | Pick up when |
|---|---|---|
| Real audit columns for recipient count, recipient hash, message or draft id and warnings | Core `audit.Record` has no extension fields; they are folded into `policy_decision` suffixes. See `requested-core-changes.md` item 17. | Core adds extension fields. |
| `Meta.next_page_token` | Core `output.Meta` has none; the token is the final array element. See items 2, 13 and 18. | Core adds it. |
| Typed Graph error code on 403 | The core `httpx` cannot read the response body. See items 1, 16 and 19. | Core exposes the body or a typed code. |
| Authentication-Results authserv-id for Exchange Online | The real value is unknown; `read.auth_results_authserv_ids` defaults to empty (verdicts read `unverified`). | S-6 reports the value. |
| Residual draft-send window | Between the second read of a draft and `POST .../send` a concurrent edit is not detected. Graph offers no conditional send. | Graph gains a conditional send, or a hold-and-lock approach is designed. |

## Optional agent-cli-core v0.2 follow-ups

Not adopted in the daemon-adapter change; the CLI keeps its local workarounds until each is taken up on its own.

| v0.2 feature | Local workaround it would replace |
|---|---|
| `Meta.next_page_token` | Page token as the final array element (requested-core-changes items 2, 13, 18) |
| Public clock for audit | `usecase.Clock` stamping `Record.Timestamp` (item 14) |
| Trusted-file check | `policyfile` ownership check (item 15); also reusable for the socket ownership check above |
| Vendor error code from body | Header-based 403 code (items 1, 16, 19) |
| Nested docgen | Current skill generation |
| Pass-through of categorized token errors | `daemonSource` wrapper (item 20) |
