# AGENTS.md

Rules for AI agents and human contributors working in this repository.

## Project summary

`outlook` is a Go CLI (binary name `outlook`) that lets an autonomous agent read, triage and send corporate email as its own Entra user, from its own mailbox, through Microsoft Graph. It is built on the shared `agent-cli-core` module (defined in `snow-cli-PRD.md` section 5; where that module lives is an open question) and obtains short-lived delegated Graph tokens from the `agent-okta-d` daemon (provider `msgraph`). It calls only `/me/...` endpoints and applies a client-side policy layer (recipients, rate limits, content filters) and an untrusted-content envelope to inbound mail.

Status: Draft PRD (`outlook-cli-PRD.md`), no implementation yet. The PRD is the source of truth for requirements. Its evidence legend (confirmed vs. unconfirmed claims) must be preserved when copying statements into other docs.

## Go layout

Planned layout (create directories only when code needs them):

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
