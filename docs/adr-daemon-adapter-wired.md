# ADR: newDaemonClient() returns core's oktad adapter

Status: accepted. Supersedes `adr-daemon-client-stub.md`.

Context: `agent-cli-core` v0.2.0 ships `auth/oktad`, an `auth.DaemonClient` over the `agent-okta-d` unix socket (built on `agent-okta-d` v0.1.0 `pkg/client`). The reason for the stub, no published Go client, is gone.

Decision: `cmd/outlook/daemon.go` `newDaemonClient()` returns `oktad.New(oktad.WithTimeout(10s))`. The socket is `AGENT_OKTA_D_SOCKET` if set, else the adapter's platform default; the CLI no longer keeps its own default path. Provider stays `msgraph`, and `auth.NewDaemonTokenSource` keeps the remediation text (a human must run `agent-okta-d enroll msgraph`). `agent-okta-d v0.1.0` is now an indirect requirement of `go.mod` (and a test import for the `clienttest` fake daemon).

Error mapping (unchanged exit codes): unreachable, `reauth_required`, `revoked` and `not_configured`/`unauthorized` exit 3; `degraded` or any retry-hinted answer exits 8. In v0.2.0 core's `DaemonTokenSource` wraps unknown errors in `auth.TokenError` (category auth), which hides the adapter's `*oktad.TransientError` (exit 8) and `*oktad.AccessError` hint. `daemonSource` in `cmd/outlook/daemon.go` re-surfaces those two types so exit 8 and the retry hint reach the user. Recorded as `requested-core-changes.md` item 20; remove the wrapper when core passes them through.

Consequences: commands reach Graph when a daemon serves a token. Not yet verified against a live daemon or tenant. The socket ownership check stays deferred (`deferred.md`).
