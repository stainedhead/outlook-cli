# Intent

## Purpose
Give an autonomous agent a **safe way to use corporate email** as its own Entra user, from its own
mailbox, through Microsoft Graph. `outlook` is a small Go CLI: it reads, triages and sends mail,
treats inbound mail as untrusted text that may carry instructions, and treats outbound mail as a
data-exfiltration path. Both directions are hostile by default.

**Status: no code exists yet.** This repository holds a draft PRD (`outlook-cli-PRD.md`, v0.2) and
project scaffolding only.

### The wider project
The agentic-teammate project lets autonomous SDLC agents work as real teammates. The Go tools in
this set exist so that **Okta secures the agents' access to the key tooling** they are given when they
run inside Hermes or a CLI harness we provide (the harness images come from
`agentic-team-w-paperclip`). Each agent gets its own identity, so every action is attributable to that
agent rather than to a shared service account or a person, and one agent can be revoked without
touching the others.

`outlook` is deployed to the same machine or container the agent identity runs inside:

```
agent host:  harness (Hermes / CLI) in a container
               -> agent-okta-d daemon + CLIs (snow, outlook, teams; stock aws, git, gh)
               -> Okta (identity root)
               -> AWS / GitHub / ServiceNow / Microsoft 365 / Atlassian
```

For mail, the agent's identity is its own Entra user. The daemon holds the agent's delegated refresh
token (enrolled once by a human) and serves short-lived Graph tokens; the agent never reads a
long-lived secret. Okta does not gate Graph directly: access is gated by the Entra user's state plus
the daemon holding the refresh token behind an Okta-federated role, so disabling the Entra user and
disabling the Okta app are both part of the kill switch (`agent-okta-d-PRD.md` §13). Server-side controls (Exchange mail flow, Conditional Access) are the real
authorization; this CLI's policy is a guardrail on top.

## Where this fits
Root set: [agentic-teams](https://github.com/stainedhead/agentic-teams). Details live in each repo.

| Repository | Relationship to `outlook` |
|---|---|
| [agentic-team-w-paperclip](https://github.com/stainedhead/agentic-team-w-paperclip) | Provides the harness container images `outlook` runs inside. |
| [agent-okta-d](https://github.com/stainedhead/agent-okta-d) | Provides the Graph token (provider `msgraph`) and the Okta identity chain. `outlook` never holds credentials itself. |
| [snow-cli](https://github.com/stainedhead/snow-cli) | Defines the shared CLI core (`agent-cli-core`: envelope, exit codes, policy, audit) that `outlook` is built from. Where that module lives is an open question and is not decided here. |
| [teams-cli](https://github.com/stainedhead/teams-cli) | Sibling Graph CLI; shares the same delegated token and app registration. |
| [outlook-cli](https://github.com/stainedhead/outlook-cli) | This repository. |

## Goals
- **Mail as the agent's own mailbox**, with no credential readable by the agent's OS user.
- **No acting as anyone else.** Only `/me` is addressed, there is no mailbox parameter, and no
  `.Shared` scopes are consented, so a compromised or manipulated agent cannot reach other mailboxes.
- **Controlled egress.** Recipients, volume and content leaving the mailbox are limited server-side
  (mail flow) and client-side (policy). Mailbox scoping does not restrict recipients (PRD §5), so the
  mail-flow layer is part of the product.
- **Inbound content marked untrusted** before it reaches the model.
- **Attributable, idempotent sends**: agent id in header and footer, an audit log, safe retries.

## Non-goals
- Reading **people's** mailboxes on their behalf, or any human mode (people use Outlook itself).
- Forwarding, inbox rules, delegation, mailbox settings, contacts and permanent deletion.
- Calendar writes (calendar read is a later, P2 item).
- Issuing or storing credentials, or running the Okta/Entra setup: that is `agent-okta-d` plus the
  tenant admins' work.
- Building the harness images or deploying agent hosts.

## Caution on unconfirmed claims
The PRD marks claims as confirmed against vendor docs or as unconfirmed (the warning-sign marker).
Graph endpoint shapes and several Exchange and Conditional Access behaviours are unconfirmed and need
verification before build. Do not restate those as fact when copying from the PRD.

## Scope boundary in one line
> This repository is the agent's mail gateway only: a thin, policy-bound CLI over the agent's own
> mailbox, not the identity provider, the harness, or the Exchange controls around it.

## How this file is used
INTENT.md captures *why* this tool exists and where it sits; the *how* is in
[`outlook-cli-PRD.md`](outlook-cli-PRD.md). Update it when goals, scope or the tool's place in the
wider set change, not when implementation details change.
