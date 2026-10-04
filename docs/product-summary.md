# Product Summary

`outlook` lets an autonomous agent read, triage and send corporate email as its own Entra user, from its own mailbox. It is a thin Go CLI over Microsoft Graph, built on the shared `agent-cli-core` library (v0.2.0). Tokens come from the `agent-okta-d` daemon (provider `msgraph`).

Email is the highest-risk channel an agent can have: inbound mail is untrusted text that may carry instructions, and outbound mail is a data-exfiltration path. The design treats both directions as hostile by default.

## What exists today

The command surface, policy engine, untrusted-content handling, idempotency ledger, audit log, selftest matrix and generated agent skill are implemented and unit/integration tested against fakes and `httptest` servers. Coverage is 96.8% overall (domain 99.6%, use cases 97.9%).

## What does not work yet

- **Not run against a live daemon.** The `agent-okta-d` adapter (core's `auth/oktad`) is wired and tested end to end against the daemon's fake on a real unix socket, but not against a running daemon. Without a daemon, every command that needs a token exits 3.
- **Nothing has been verified against a real Microsoft 365 tenant.** Every Graph endpoint shape and tenant behaviour is an explicit, tracked assumption (`unverified-assumptions.md`). The real-tenant spikes (M0) are written as a checklist (`m0-spike-checklist.md`) but have not been run.

Sources: `outlook-cli-PRD.md` section 1 (Draft v0.2) and `specs/archive/261003-outlook-cli/spec.md`. Deferred and out-of-scope items: `deferred.md`.
