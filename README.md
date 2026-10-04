# outlook-cli

`outlook` is a Go CLI that lets an autonomous agent read, triage and send corporate email **as its own Entra user, from its own mailbox**, through Microsoft Graph.

> **Status: implemented, not yet usable end to end.** The command surface, client-side policy, untrusted-content handling, idempotency ledger, audit log and selftest are built and tested against fakes. Two things are not done: the real `agent-okta-d` credential adapter is **deferred** (the daemon has no Go client yet, so every command that needs a token exits 3, "daemon unavailable"), and **nothing has been verified against a real Microsoft 365 tenant** (the real-tenant spikes are a checklist in [`docs/m0-spike-checklist.md`](docs/m0-spike-checklist.md)). See [`docs/deferred.md`](docs/deferred.md) and [`docs/unverified-assumptions.md`](docs/unverified-assumptions.md).

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
- **Client-side policy** (YAML, trusted only when root-owned with root-owned ancestors): recipient allowlists, max recipients, BCC and reply-all denied, no forward, no attachments on send by default, secret-pattern filter, per-hour/day rate caps, `external: deny | draft_only | allow`.
- **Untrusted-content envelope.** Subject, body, display names and attachment names are marked `untrusted`; HTML is converted to text, links are listed separately and defanged, images are never fetched, attachment download is off by default.
- **Attribution and idempotency.** Subject prefix and footer naming the agent id, `X-Agent-Id` / `X-Agent-Run` headers ⚠️, a local idempotency ledger, and `--dry-run`.
- **Deliberately absent:** forward, permanent delete, inbox rules, delegates, mailbox settings, contacts, send-on-behalf, any mailbox parameter.
- **Shared core.** Builds on [agent-cli-core](https://github.com/stainedhead/agent-cli-core), its own repository (output envelope, exit codes, bounds, policy, audit), specified in its `agent-cli-core-PRD.md`; it originated in `snow-cli-PRD.md` section 5. `go.mod` requires `agent-cli-core v0.1.0` (no `replace`). `agent-okta-d` is not a dependency.

Commands: `whoami`, `folder list`, `mail list|get|search|send|reply|draft create|list|send|delete|mark|move`, `attachment list|get` (download off by default), `selftest`, `version`. Run `outlook --help` for usage. There is no calendar command (calendar is P2, read-only, deferred). Full table in PRD section 6.

## Companion repositories

| Repo | Relationship |
|---|---|
| [agent-okta-d](https://github.com/stainedhead/agent-okta-d) | Daemon that holds the refresh token and serves the `msgraph` provider (PRD section 7.5) |
| [agent-cli-core](https://github.com/stainedhead/agent-cli-core) | Build dependency: shared CLI core library (envelope, exit codes, bounds, policy, audit) |
| [snow-cli](https://github.com/stainedhead/snow-cli) | Sibling ServiceNow CLI built on the same core |
| [teams-cli](https://github.com/stainedhead/teams-cli) | Sibling Microsoft Graph CLI; shares the same delegated token |
| [outlook-cli](https://github.com/stainedhead/outlook-cli) | This repository |
| [agentic-team-w-paperclip](https://github.com/stainedhead/agentic-team-w-paperclip) | Related project, part of the set rooted at [agentic-teams](https://github.com/stainedhead/agentic-teams) |

## Layout

```
cmd/outlook/     composition root (main, wiring, daemon client stub)
internal/        domain, usecase, adapter/{cli,graph,policyfile,ledger,auditlog,selftestcfg}, archtest
docs/            contributor docs: product, technical, ADRs, deferred work, unverified assumptions
user-docs/       end-user docs: install, configuration, usage, troubleshooting
specs/           feature specs; completed ones move to specs/archive/
INTENT.md        purpose and wider context
AGENTS.md        rules for agents and contributors
```

## Documentation

- User documentation: [`user-docs/`](user-docs/)
  - [Getting started](user-docs/getting-started.md)
  - [Configuration reference and sample policy](user-docs/configuration.md)
  - [Usage examples](user-docs/usage.md)
  - [Exit codes](user-docs/exit-codes.md)
  - [Troubleshooting](user-docs/troubleshooting.md) (including daemon unavailable, exit 3)
- Contributor docs: [`docs/`](docs/) - [product summary](docs/product-summary.md), [product details](docs/product-details.md), [technical details](docs/technical-details.md), [ADRs](docs/architectural-decision-record.md), [deferred work](docs/deferred.md), [unverified assumptions](docs/unverified-assumptions.md), [requested core changes](docs/requested-core-changes.md)
- Requirements: the PRD under [`specs/archive/261003-outlook-cli/`](specs/archive/261003-outlook-cli/)
- Contributor and agent rules: [`AGENTS.md`](AGENTS.md)

## Development

```
make fmt lint test build     # also: make check, make race, make cross, make skill
```

Requires Go 1.27 and golangci-lint. The core module is private: set `GOPRIVATE=github.com/stainedhead/*` and have repository access. Never commit credentials.

## License

MIT. See [LICENSE](LICENSE).
