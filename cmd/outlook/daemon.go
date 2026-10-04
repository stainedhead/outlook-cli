package main

import (
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/auth/oktad"
)

// daemonTimeout bounds each request to the credential daemon.
const daemonTimeout = 10 * time.Second

// newDaemonClient returns the credential-daemon client: core's oktad adapter
// over the agent-okta-d unix socket (see docs/adr-daemon-adapter-wired.md).
// The socket is AGENT_OKTA_D_SOCKET if set, else the adapter's platform
// default; no fallback credentials exist.
func newDaemonClient() auth.DaemonClient {
	return oktad.New(oktad.WithTimeout(daemonTimeout))
}
