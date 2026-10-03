# Product Summary

`outlook` lets an autonomous agent read, triage and send corporate email as its own Entra user, from its own mailbox. It is a thin Go CLI over Microsoft Graph, built on the shared `agent-cli-core` library (v0.1.0). Tokens are meant to come from the `agent-okta-d` daemon (provider `msgraph`).

Email is the highest-risk channel an agent can have: inbound mail is untrusted text that may carry instructions, and outbound mail is a data-exfiltration path. The design treats both directions as hostile by default.

## What exists today

The command surface, policy engine, untrusted-content handling, idempotency ledger, audit log, selftest matrix and generated agent skill are implemented and unit/integration tested against fakes and `httptest` servers. Coverage is 96.8% overall (domain 99.6%, use cases 97.9%).

## What does not work yet

- **No command can reach Graph.** The real `agent-okta-d` adapter is deferred: the daemon has not published a Go client. The composition root uses a stub that reports the daemon as unavailable, so every command that needs a token exits 3. Commands that need no token (`help`, `version`, `skill`) work.
- **Nothing has been verified against a real Microsoft 365 tenant.** Every Graph endpoint shape and tenant behaviour is an explicit, tracked assumption (`unverified-assumptions.md`). The real-tenant spikes (M0) are written as a checklist (`m0-spike-checklist.md`) but have not been run.

Sources: `outlook-cli-PRD.md` section 1 (Draft v0.2) and `specs/261003-outlook-cli/spec.md`. Deferred and out-of-scope items: `deferred.md`.
