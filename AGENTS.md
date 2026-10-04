# AGENTS.md

Rules for AI agents and human contributors working in this repository.

## Project summary

`outlook` is a Go CLI (binary name `outlook`) that lets an autonomous agent read, triage and send corporate email as its own Entra user, from its own mailbox, through Microsoft Graph. It is built on the shared [`agent-cli-core`](https://github.com/stainedhead/agent-cli-core) module (its own repository, specified in its `agent-cli-core-PRD.md`; originated in `snow-cli-PRD.md` section 5) and obtains short-lived delegated Graph tokens from the `agent-okta-d` daemon (provider `msgraph`). It calls only `/me/...` endpoints and applies a client-side policy layer (recipients, rate limits, content filters) and an untrusted-content envelope to inbound mail.

Status: implemented; the `agent-okta-d` adapter (core `auth/oktad`) is wired and tested against the daemon's fake; live-daemon and real-tenant verification are deferred (see `docs/deferred.md`). The PRD (`specs/archive/261003-outlook-cli/outlook-cli-PRD.md`) is the source of truth for requirements. Its evidence legend (confirmed vs. unconfirmed claims) must be preserved when copying statements into other docs.

## Documentation routing

| Kind of change | Update |
|---|---|
| Goal, direction or scope shift (including place in the wider set) | `INTENT.md` |
| New or changed requirement | `outlook-cli-PRD.md` (or the active spec under `specs/`) |
| Contributor-facing design | `docs/` |
| How to install, configure and use | `user-docs/` |
| Orientation for newcomers | `README.md` |

`INTENT.md` records why the tool exists and how it fits with `agentic-teams`, `agentic-team-w-paperclip`, `agent-okta-d`, `agent-cli-core`, `snow-cli` and `teams-cli`. Read it first.

## Go layout

Planned layout (create directories only when code needs them):

- `INTENT.md` - purpose, goals, scope and wider context
- `outlook-cli-PRD.md` - requirements (source of truth)
- `cmd/outlook/` - main package, wiring only
- `internal/` - application packages (domain, use cases, adapters)
- `docs/` - product and technical documentation for contributors
- `user-docs/` - end-user documentation (see rule below)
- `specs/` - feature specs; completed specs move to `specs/archive/`

## Architecture and engineering standards

- Clean Architecture: domain and use-case code must not import adapters (Graph client, daemon client, filesystem, CLI framework). Dependencies point inward; adapters implement interfaces owned by inner layers.
- TDD: write a failing test first, make it pass, then refactor. Every behavior change ships with tests. Prefer table-driven tests.
- Keep functions small, return errors with context, no global mutable state, pass `context.Context` to anything that does I/O.
- Security-sensitive defaults: deny by default, treat all inbound mail content as untrusted, never add a mailbox parameter or `/users/{id}` calls.

## Dependency on agent-cli-core

- Shared-core changes (envelope, exit codes, bounds, policy, audit, daemon-client wrapper) are made in [agent-cli-core](https://github.com/stainedhead/agent-cli-core), never copied into this repository.
- Depend on released semver tags only: no pseudo-versions, no `replace` directives on `main`.
- `go.mod` requires `github.com/stainedhead/agent-cli-core` at the released tag `v0.2.1` (no `replace`, no pseudo-versions). `agent-okta-d` appears in `go.mod` only through core's `auth/oktad` adapter (and the `clienttest` fake in tests); do not import its client directly from application code.

## Verification (run before every commit)

```
gofmt -l .
go vet ./...
golangci-lint run
go test ./...
```

`make fmt`, `make lint`, `make test` and `make build` wrap these. All must pass cleanly.

## Credentials

Never commit credentials, tokens, refresh tokens, client secrets, certificates, private keys, real tenant data, or real mailbox contents. Use obviously fake values (for example `corp.example.com`) in examples and tests.

## `user-docs/` rule

`user-docs/` holds only files that help a user adopt, configure and use the tool: install, getting started, configuration reference, usage examples, and troubleshooting. It is NOT for design, requirements, spec or process material, and must not link into `specs/`. Design and requirements go in `docs/` or `specs/`.

## Commits

Keep commits focused. Do not force-push shared branches.

## Agent skill

How agents use this tool is documented in the root repository's skill document, `skills/outlook-cli.md`, in https://github.com/stainedhead/agentic-teams (see `skills/README.md`). That is its only home; do not copy it here. A change to the command surface, flags, exit codes, policy verbs or write modes, or forbidden actions is not finished until that skill is updated (see SKILL-1..7 in the PRD).
