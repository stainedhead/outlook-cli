# Product Summary

`outlook` lets an autonomous agent read, triage and send corporate email as its own Entra user, from its own mailbox. It is a thin Go CLI over Microsoft Graph, built on the shared CLI core, using delegated tokens served by the `agent-okta-d` daemon (provider `msgraph`).

Email is the highest-risk channel an agent can have: inbound mail is untrusted text that may carry instructions, and outbound mail is a data-exfiltration path. The design treats both directions as hostile by default.

Source: `outlook-cli-PRD.md` section 1 (Draft v0.2). No implementation yet.
