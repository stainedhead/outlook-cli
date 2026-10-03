# Technical Details

Language and toolchain: Go 1.27, `CGO_ENABLED=0`, static binary `outlook`. Dependencies: `github.com/stainedhead/agent-cli-core v0.1.0` (envelope, exit codes, auth, httpx, audit, selftest, docgen) and a YAML library (`goccy/go-yaml`). `agent-okta-d` is not a dependency.

## Package layout (Clean Architecture)

| Package | Role | May import |
|---|---|---|
| `cmd/outlook` | Composition root: `main.go` (build stamps, signals), `app.go` (builds adapters, use cases, config and env), `daemon.go` (`newDaemonClient()` stub) | everything |
| `internal/adapter/cli` | Command table, flag parsing, presenter (wraps untrusted fields), one envelope per run, generated skill | usecase, domain, core `output` |
| `internal/adapter/graph` | Graph client over core `httpx`; `/me/...` only; read, write, paging, Sent Items probe | usecase ports, domain |
| `internal/adapter/policyfile` | Strict YAML loader, agent-writable check, content filters (`secret_patterns`, `classification_markers`) | usecase, domain |
| `internal/adapter/ledger` | File idempotency ledger (lock, atomic write, fsync); attachment quarantine writer | usecase, domain |
| `internal/adapter/auditlog` | Audit JSONL sink over core `audit` | usecase, domain |
| `internal/adapter/selftestcfg` | The 13-row selftest matrix over core `selftest` | usecase, domain |
| `internal/usecase` | Facade `Commands`, ports (`Reader`, `Writer`, `Ledger`, `PolicyProvider`, `AuditSink`, `Clock`, ...), read/send/triage/attachment logic | domain |
| `internal/domain` | Types, policy model and pure evaluation, address and subject validation, HTML-to-text, link defanging, sender trust, rendering | standard library |
| `internal/archtest` | Fails the build when the import graph is violated | - |

## Run flow

1. `cli.Run` finds the command. `help`, `version` and `skill` never build the object graph, so they work without policy, credentials or a daemon.
2. Other commands call the composition root, which loads the policy first (missing, invalid or writable means stop), then builds the token source over `newDaemonClient()`, the Graph client, the ledger, the audit sink (Block mode: a failed audit write fails the command) and the content filters.
3. The use-case `begin` step loads the policy once and checks `GET /me` against the policy mailbox once per run.
4. The use case evaluates policy, reserves the idempotency key, calls the port, records an audit entry, and returns domain values. The presenter wraps untrusted fields with the core `output.Untrusted` marker.

## Configuration surface

| Item | Value |
|---|---|
| Policy file | `/etc/agent-cli/outlook.policy.yaml`, override `OUTLOOK_POLICY` |
| Agent id | `AGENT_ID`, default the policy `profile` |
| Run id | `AGENT_RUN_ID`, default random `run-<hex>` |
| Daemon socket | `AGENT_OKTA_D_SOCKET`, default `/run/agent-okta-d/agent-okta-d.sock` (unverified) |
| Audit log | `audit.path` in the policy (JSONL, 0700 dir, 0600 file) |
| Idempotency ledger | `outlook.idempotency.json` in the audit directory |
| Quarantine | `read.attachments.out_dir` |

The environment variable names and file names other than the policy path are choices of this build, not PRD requirements (UA-22, UA-23, UA-26).

## Graph mapping

Every call is `https://graph.microsoft.com/v1.0/me/...`; the HTTP client permits only the Graph host; a `/users/{id}` path is never built (a test scans every endpoint). The full table of endpoints, query options and assumed behaviour is in `unverified-assumptions.md`. All of it is unverified against a real tenant. Error mapping: 400 validation (9), 404/410 not_found (5), 409/412 conflict (7), 401/403/429/5xx via core `httpx` (3, 4, 8). Reads honour `Retry-After` with bounded retries; writes are never replayed after an ambiguous failure. Page tokens are base64url of the Graph `@odata.nextLink` and a token pointing elsewhere is a usage error.

## Policy evaluation

`domain` holds the typed policy and a pure evaluation function returning allow, dry_run_only, draft_only or deny with a rule id (`default-deny`, `send.recipients.external`, `send.rate.per_hour`, ...). Rate caps count sends recorded in the ledger, since every CLI run is a new process. Content filters return findings (kind and offset) without the matched text. Loader defaults and zero-value semantics: `unverified-assumptions.md`, "Reference: policy semantics".

## Audit

One JSONL record per command: schema version, timestamp, tool, agent id, run id, verb, resource, outcome, HTTP status, duration, `policy_decision` (`allow` or `deny:<rule-id>`). No bodies, subjects or addresses by default.

## Test strategy

Strict TDD, table-driven tests. Fakes for every port; `httptest` for Graph; `authtest.Fake` for the daemon; no test touches a real network, credentials or sends mail. Integration tests (`cmd/outlook/integration_test.go`) drive the real command tree. Assumption tests are named `TestAssumed...`. Gates: `gofmt -l .` empty, `go vet ./...`, `golangci-lint run`, `go test -race ./...`, at least 90% coverage in domain and use cases (actual: see `product-summary.md`; `cmd/outlook` is 74.3% because `main`, signal handling and `prodConfig` need a live process).

## Build

`make build` (stamps version, commit, date via ldflags into `bin/outlook`), `make cross` (darwin/arm64, linux/amd64, linux/arm64 into `dist/`), `make skill` (`dist/outlook-cli.md` via core docgen from the same command table that drives dispatch), `make check`. CI is `.github/workflows/ci.yml` (private module fetch with the job token). There are no release workflows.
