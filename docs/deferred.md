# Deferred work

Items that are intentionally not built in this repository now, with the reason and the condition for picking them up again.

| Item | Status | Rationale | Pick up when |
|---|---|---|---|
| Calendar writes | Out of scope | The PRD puts calendar at P2 and read-only; sending invitations is another way to reach external parties and widens the policy surface. No calendar command exists. | A go/no-go memo after M5 approves it, with a policy model for attendees. |
| Windows native | Out of scope | Supported platforms are macOS and Linux (WSL is the Windows path). The release matrix has no `windows/*` target, and file-permission and socket assumptions in the daemon client are POSIX. | A decision to support the daemon on native Windows. |
| Exchange mail-flow rules, Conditional Access and DLP | Out of scope | These are tenant configuration owned by the Exchange and identity teams, not code in this repository. The CLI only checks and reports (selftest) and keeps its own client-side guardrails. | Not applicable; owners are named in PRD s15. |
| M0 real-tenant spikes | Deferred | They need a sandbox tenant, an agent user and a human to enroll. Checklist is in `m0-spike-checklist.md`. Until they run, every Graph shape stays in `unverified-assumptions.md`. | A sandbox tenant is provisioned. |
| Real daemon adapter | Deferred | `agent-okta-d` has not yet published `pkg/client`. The composition root's `newDaemonClient()` returns the core's unreachable error (exit 3). `agent-okta-d` is not in `go.mod`. See `adr-daemon-client-stub.md`. | `agent-okta-d` tags a release with `pkg/client`; the swap is one function. |
| Release workflows | Deferred | The PRD asks for signed, notarized release artifacts and cosign signatures, and the signing account and registry are open questions (PRD s16.8). Only CI exists; no secrets are added. | The open items in PRD s16.8 are decided. |
| Sent Items idempotency probe (P1) | Deferred | Depends on an unverified header search (UA-12). The local ledger covers P0. | S-5 confirms the lookup. |
| Attachment download enablement | Opt-in only | Off by default; the quarantine path ships but is gated by policy. | An operator enables it in policy. |
| Root skill update and `docs` automation | Manual | Updating the root repository's skill is a manual PR (PRD s17.1). | A cross-repository token approach is agreed. |
