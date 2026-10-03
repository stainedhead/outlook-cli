# outlook-cli

`outlook` is a planned Go CLI that lets an autonomous agent read, triage and send corporate email **as its own Entra user, from its own mailbox**, through Microsoft Graph.

> **Status: Draft PRD (v0.2), no implementation yet.** This repository currently contains the requirements document and project scaffolding only. See [`outlook-cli-PRD.md`](outlook-cli-PRD.md).

**Evidence legend (from the PRD).** ✅ = confirmed against vendor documentation during research (2026-10-03). ⚠️ = not confirmed in vendor docs (engineering judgment, secondary source, or general Graph knowledge). Graph endpoint shapes in the PRD are from general knowledge of Graph v1.0 and need verification before build.

For the purpose, wider context and scope of this tool, read [`INTENT.md`](INTENT.md).

## Why

Email is the highest-risk channel an agent can have: inbound mail is untrusted text that may carry instructions, and outbound mail is a data-exfiltration path. `outlook` treats both directions as hostile by default. Goals from the PRD:

- The agent reads and sends mail as its own mailbox, with no credential readable by the agent's OS user.
- A compromised or manipulated agent cannot read or send as anyone else.
- Recipients, volume and content leaving the mailbox are controlled both server-side (Exchange mail flow) and client-side (policy).
- Inbound content reaches the model clearly marked as untrusted.
- Every send is attributable (agent id header/footer, audit log) and idempotent on retry.

Non-goals: reading people's mailboxes on their behalf, human mode, forwarding, rules, delegation, mailbox settings, contacts, permanent deletion, and calendar writes (calendar read is P2).

## Key design points

- **Delegated user identity.** Identity is the agent's own Entra user. The `agent-okta-d` daemon holds a delegated refresh token (enrolled once by a human via device-code sign-in) and serves short-lived Graph access tokens (provider `msgraph`). This replaces the v0.1 app-only design.
- **`/me` only.** The CLI never takes a mailbox parameter. Delegated scopes are `Mail.ReadWrite`, `Mail.Send`, `User.Read`, `offline_access`; no `.Shared` or `*.All` mail scopes. The policy `mailbox` value is checked against `GET /me` at start-up.
- **Delegated scopes do not control recipients** ✅, so Exchange mail flow rules (external-domain allowlist, DLP, disclaimer) are treated as part of the product and are owned by the Exchange team ⚠️.
- **Client-side policy** (root-owned YAML): recipient allowlists, max recipients, BCC and reply-all denied, no forward, no attachments on send by default, secret-pattern filter, per-hour/day rate caps, `external: deny | draft_only | allow`.
- **Untrusted-content envelope.** Subject, body, display names and attachment names are marked `untrusted`; HTML is converted to text, links are listed separately and defanged, images are never fetched, attachment download is off by default.
- **Attribution and idempotency.** Subject prefix and footer naming the agent id, `X-Agent-Id` / `X-Agent-Run` headers ⚠️, a local idempotency ledger, and `--dry-run`.
- **Deliberately absent:** forward, permanent delete, inbox rules, delegates, mailbox settings, contacts, send-on-behalf, any mailbox parameter.
- **Shared core.** Builds on the shared `agent-cli-core` module (output envelope, exit codes, bounds, policy, audit) defined in `snow-cli-PRD.md` section 5. **Where `agent-cli-core` lives is an open question and is not decided here.**

Planned commands include `whoami`, `folder list`, `mail list|get|search|send|reply|draft|mark|move`, `attachment list|get` (off by default), `calendar list` (P2) and `selftest`. Full table in PRD section 6.

## Companion repositories

| Repo | Relationship |
|---|---|
| [agent-okta-d](https://github.com/stainedhead/agent-okta-d) | Daemon that holds the refresh token and serves the `msgraph` provider (PRD section 7.5) |
| [snow-cli](https://github.com/stainedhead/snow-cli) | Defines the shared CLI core (`snow-cli-PRD.md` section 5) |
| [teams-cli](https://github.com/stainedhead/teams-cli) | Sibling Microsoft Graph CLI; shares the same delegated token |
| [outlook-cli](https://github.com/stainedhead/outlook-cli) | This repository |
| [agentic-team-w-paperclip](https://github.com/stainedhead/agentic-team-w-paperclip) | Related project, part of the set rooted at [agentic-teams](https://github.com/stainedhead/agentic-teams) |

## Planned layout

```
cmd/outlook/     main package (not yet created)
internal/        application packages (not yet created)
docs/            contributor-facing product and technical docs
user-docs/       end-user docs: install, configuration, usage, troubleshooting
specs/archive/   completed feature specs
outlook-cli-PRD.md
AGENTS.md        rules for agents and contributors
```

## Documentation

- Requirements: [`outlook-cli-PRD.md`](outlook-cli-PRD.md)
- Contributor docs: [`docs/`](docs/)
- User documentation: [`user-docs/`](user-docs/) (empty until there is something to use)
- Contributor and agent rules: [`AGENTS.md`](AGENTS.md)

## Development

```
make fmt lint test build
```

Requires Go 1.27 and golangci-lint. Never commit credentials.
