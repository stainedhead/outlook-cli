# Technical Details

Language and toolchain: Go 1.27, `CGO_ENABLED=0`, static binary `outlook`. Dependencies: `github.com/stainedhead/agent-cli-core v0.2.0` (envelope, exit codes, auth, `auth/oktad`, httpx, audit, selftest, docgen) and a YAML library (`goccy/go-yaml`). `agent-okta-d v0.1.0` is an indirect requirement (the adapter's client; its `clienttest` fake daemon is used in tests).

## Package layout (Clean Architecture)

| Package | Role | May import |
|---|---|---|
| `cmd/outlook` | Composition root: `main.go` (build stamps, signals), `app.go` (builds adapters, use cases, config and env), `daemon.go` (`newDaemonClient()` returns core's `oktad` adapter; `daemonSource` keeps its exit-8 and access errors visible) | everything |
| `internal/adapter/cli` | Command table, flag parsing, presenter (wraps untrusted fields), one envelope per run, generated skill | usecase, domain, core `output` |
| `internal/adapter/graph` | Graph client over core `httpx`; `/me/...` only; read, write, paging, Sent Items probe | usecase ports, domain |
| `internal/adapter/policyfile` | Strict YAML loader, ownership trust check, content filters (`secret_patterns`, `classification_markers`) | usecase, domain |
| `internal/adapter/ledger` | File idempotency ledger (lock, atomic write, fsync); attachment quarantine writer | usecase, domain |
| `internal/adapter/auditlog` | Audit JSONL sink over core `audit` | usecase, domain |
| `internal/adapter/selftestcfg` | The 13-row selftest matrix over core `selftest` | usecase, domain |
| `internal/usecase` | Facade `Commands`, ports (`Reader`, `Writer`, `Ledger`, `PolicyProvider`, `AuditSink`, `Clock`, ...), read/send/triage/attachment logic | domain |
| `internal/domain` | Types, policy model and pure evaluation, address and subject validation, HTML-to-text, link defanging, sender trust, rendering | standard library |
| `internal/archtest` | Fails the build when the import graph is violated | - |

## Run flow

1. `cli.Run` finds the command. `help`, `version` and `skill` never build the object graph, so they work without policy, credentials or a daemon.
2. Other commands call the composition root, which loads the policy first (missing, invalid or not provably root-authored means stop), then builds the token source over `newDaemonClient()`, the Graph client, the ledger, the audit sink (Block mode: a failed audit write fails the command) and the content filters.
3. The use-case `begin` step loads the policy once and checks `GET /me` against the policy mailbox once per run.
4. The use case evaluates policy, reserves the idempotency key, calls the port, records an audit entry, and returns domain values. The presenter wraps untrusted fields with the core `output.Untrusted` marker.

## Configuration surface

| Item | Value |
|---|---|
| Policy file | `/etc/agent-cli/outlook.policy.yaml`, override `OUTLOOK_POLICY` (the target must pass the same ownership check). A dev-only escape, `OUTLOOK_POLICY_INSECURE=1`, exists only in binaries built with `-tags outlookdev`; release builds ignore it |
| Agent id | `AGENT_ID`, default the policy `profile` |
| Run id | `AGENT_RUN_ID`, default random `run-<hex>` |
| Daemon socket | `AGENT_OKTA_D_SOCKET`, else the adapter's platform default (`/var/run/agentd/agentd.sock` on macOS, `/run/agentd/agentd.sock` on Linux, per core's guide); request timeout 10 s |
| Audit log | `audit.path` in the policy (JSONL, 0700 dir, 0600 file) |
| Idempotency ledger | `outlook.idempotency.json` in the audit directory (must be mode 0700, owned by the agent user) |
| Page-token key | `outlook.pagekey` next to the ledger (32 random bytes, file 0600); deleting it invalidates outstanding page tokens |
| Quarantine | `read.attachments.out_dir` |

The environment variable names and file names other than the policy path are choices of this build, not PRD requirements (UA-22, UA-23, UA-26).

## Graph mapping

Every call is `https://graph.microsoft.com/v1.0/me/...`; the HTTP client permits only the Graph host; a `/users/{id}` path is never built (a test scans every endpoint). The full table of endpoints, query options and assumed behaviour is in `unverified-assumptions.md`. All of it is unverified against a real tenant. Error mapping: 400 validation (9), 404/410 not_found (5), 409/412 conflict (7), 401/403/429/5xx via core `httpx` (3, 4, 8). Reads honour `Retry-After` with bounded retries; writes are never replayed after an ambiguous failure. Page tokens are signed (HMAC with the per-install key), bound to the operation, folder and query, and wrap the Graph `@odata.nextLink`; a forged, tampered, mismatched or foreign-host token is a usage error (exit 2) and no request is made. If the key cannot be read or created, a list that has a next page fails (exit 1). Ids used as path segments must match `[A-Za-z0-9_=-]`, at most 512 characters, otherwise usage (exit 2).

## Policy evaluation

`domain` holds the typed policy and a pure evaluation function returning allow, dry_run_only, draft_only or deny with a rule id (`default-deny`, `send.recipients.external`, `send.rate.per_hour`, ...). Rate caps count ledger entries of kind `send` with status `sent` or `pending` (pending counts, conservatively), since every CLI run is a new process; the cap check and the reservation are one atomic step under the ledger lock. Keyed draft creations are kind `draft`, never count, and replay as `already_drafted`. The caps are guardrails against a runaway agent, not a defence against a hostile local user who can edit the ledger. Content filters return findings (kind and offset) without the matched text. Loader defaults and zero-value semantics: `unverified-assumptions.md`, "Reference: policy semantics".

## Policy file trust (FR-R2)

The loader proves the policy was authored by an administrator rather than by checking whether the current user can write it: the file and every ancestor directory must be owned by root (or a uid configured as trusted), never the effective uid (running as root is refused), and none may be group or world writable. Symlinks are checked by owner, the file is opened with `O_NOFOLLOW` and its content is read from the descriptor that was verified with `fstat`. This replaces the earlier `access(2)` W_OK test. `OUTLOOK_POLICY` selects a path but the target passes the same check.

## Send and read hardening

- Reply (FR-R4): recipients are Reply-To when set, else From; policy is evaluated on that set (unverified that Graph does the same, UA-29).
- Draft send (FR-R3, FR-R9): attachments are denied by `send.attachments`; size and content filters run on the raw draft body (markup, attributes, comments, beyond 2 MiB) as well as the converted text and subject. A draft with an empty From is refused for send and delete. A second read before the POST compares recipients, subject, raw body, author and attachment state; a change is a conflict (exit 7) and frees the key. A residual window remains between that read and the POST. Drafts are sent as-is; `prefix_applied` reports whether the prefix and footer are present.
- Idempotency probe (FR-R6): runs for new sends only. Draft send and reply skip it (`POST .../send` cannot carry headers, so a draft-send dry run shows no `X-Agent` headers). An HTTP 400 from the probe is inconclusive: the send proceeds on the ledger and the result carries warning `probe=inconclusive`. Any other probe error fails closed.
- `--body-file` (FR-R7): regular files only, at most 4 MiB, symlinks resolved; stdin is bounded the same way and an over-cap input is an error, not a silent cut. Any file the agent user can read is readable; path roots are deferred (`deferred.md`).
- Untrusted text (FR-R5, FR-R10): every `Cf` format character, variation selector, tag character (U+E0000 to U+E007F) and U+2028/2029 is stripped from text, including emoji ZWJ sequences, which split into component emoji. Non-conforming from/to/cc addresses are emitted as an untrusted object with `address_flag: non_conforming`; link url and domain and folder names are untrusted objects.
- `auth_results` (FR-R10): a header counts only if its authserv-id is in policy `read.auth_results_authserv_ids`; otherwise spf, dkim and dmarc read `unverified`. It is a policy key, not an environment variable, because the policy file is root-owned and the environment is agent-controllable.
- Attachment quarantine (FR-R12): the root is created if missing, containment is checked on the deepest existing ancestor before any mkdir, and a name collision gets a suffix (`name (1).ext`, up to 1000, then conflict).

## Threat model

Scope: the client-side tool. Entra scopes, Exchange mail flow and Conditional Access are the real boundary (see `product-details.md`) and are not configured here.

| Asset | Attacker | Control | Residual risk |
|---|---|---|---|
| Policy (the guardrails) | Agent user or a prompt-injected agent editing or redirecting the policy file | Ownership trust check on file and ancestors, `O_NOFOLLOW`, `fstat` on the descriptor, euid never trusted, release build ignores the dev override | A compromised root account; a trusted-uid list set too wide. Not applicable on platforms without the check (it fails closed). |
| Mailbox contents and outbound reach | Sender of a hostile email (prompt injection) | Untrusted wrapping of all third-party text, hidden-character stripping, link defanging, HTML to text, no image fetch, folder allowlist, bounded output | An agent that obeys text it should treat as data. Sender trust and `auth_results` are advisory; the authserv-id is unverified (UA-33). |
| Recipient set and content leaving the mailbox | Prompt-injected agent | Recipient allowlist, external deny or draft-only, bcc and reply-all denied, Reply-To evaluated, content filters, size caps, attachment denial, draft re-read before send | `--body-file` can read any file the agent user can (OQ-3, deferred); filters are pattern based; Exchange mail flow must back this up. |
| Send volume and duplicates | Runaway or hostile agent, concurrent runs | Atomic cap-and-reserve in the ledger, pending counted, idempotency key, Sent Items probe | Caps are guardrails: a local user who can edit the ledger or its directory can reset them. The probe is unverified (UA-12, UA-30). Residual draft-send window. |
| Page tokens | Agent or injected text supplying a crafted `--page-token` | HMAC signature with per-install key, binding to operation, folder and query, host pinning, id charset | Whoever can read `outlook.pagekey` can mint tokens, but they still only reach the Graph host under `/me/`. |
| Credentials | Agent user | No credentials on disk or in the environment; tokens only from the daemon; Graph host pinned | `AGENT_OKTA_D_SOCKET` can be redirected until the socket ownership check lands (deferred). |
| Audit and ledger state | Local user | 0700 directory owned by the agent user (a shared 0755 directory is refused), 0600 files, fail closed on audit write failure, no bodies, subjects or addresses (counts and a 32 hex recipient-set hash only) | The agent user owns these files, so they are not tamper-proof; ship the audit log off host. |
| Attachment quarantine | Hostile attachment name or path | Type and size allowlist, containment check before mkdir, collision suffix, never opened | Files on disk are still hostile content. |

## Audit

One JSONL record per command: schema version, timestamp, tool, agent id, run id, verb, resource, outcome, HTTP status, duration, `policy_decision` (`allow` or `deny:<rule-id>`, with `;recipient_count=N;recipient_hash=H;message_id=ID;warnings=a,b` suffixes on sends, because the core record has no extension fields; `http_status` is set for every upstream failure). The recipient hash is 32 hex characters of a SHA-256 over the sorted, lower-cased address set. No bodies, subjects or addresses.

## Test strategy

Strict TDD, table-driven tests. Fakes for every port; `httptest` for Graph; `authtest.Fake` for the daemon; no test touches a real network, credentials or sends mail. Integration tests (`cmd/outlook/integration_test.go`) drive the real command tree. Assumption tests are named `TestAssumed...`. Gates: `gofmt -l .` empty, `go vet ./...`, `golangci-lint run`, `go test -race ./...`, at least 90% coverage in domain and use cases (actual: see `product-summary.md`; `cmd/outlook` is 87.1% with tests for the `assemble` failure paths; `main`, signal handling and `prodConfig` need a live process).

## Build

`make build` (stamps version, commit, date via ldflags into `bin/outlook`), `make cross` (darwin/arm64, linux/amd64, linux/arm64 into `dist/`), `make skill` (`dist/outlook-cli.md` via core docgen from the same command table that drives dispatch), `make check`. CI is `.github/workflows/ci.yml` (private module fetch with the job token). There are no release workflows.
