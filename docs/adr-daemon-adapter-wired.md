# ADR: newDaemonClient() returns core's oktad adapter

Status: accepted. Supersedes `adr-daemon-client-stub.md`.

Context: `agent-cli-core` v0.2.1 ships `auth/oktad`, an `auth.DaemonClient` over the `agent-okta-d` unix socket (built on `agent-okta-d` v0.1.0 `pkg/client`). The reason for the stub, no published Go client, is gone.

Decision: `cmd/outlook/daemon.go` `newDaemonClient()` returns `oktad.New(oktad.WithTimeout(10s))`. The socket is `AGENT_OKTA_D_SOCKET` if set, else the adapter's platform default; the CLI no longer keeps its own default path. Provider stays `msgraph`, and `auth.NewDaemonTokenSource` keeps the remediation text (a human must run `agent-okta-d enroll msgraph`). `agent-okta-d v0.1.0` is now an indirect requirement of `go.mod` (and a test import for the `clienttest` fake daemon).

Error mapping (core v0.2.1, passed through unchanged): unreachable, `reauth_required`, `revoked` and `not_configured`/`unauthorized` exit 3; `degraded` or any retry-hinted answer exits 8 with the wait in the hint; a cancelled caller context exits 1.

Consequences: commands reach Graph when a daemon serves a token. Not yet verified against a live daemon or tenant. The socket ownership check stays deferred (`deferred.md`).
