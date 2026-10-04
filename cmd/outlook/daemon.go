package main

import (
	"context"
	"os"

	"github.com/stainedhead/agent-cli-core/auth"
)

// defaultDaemonSocket is where agent-okta-d is expected to listen.
// ASSUMPTION (unverified): the real path is set by the daemon deployment; the
// AGENT_OKTA_D_SOCKET environment variable overrides it.
const defaultDaemonSocket = "/run/agent-okta-d/agent-okta-d.sock"

func daemonSocket() string {
	if s := os.Getenv("AGENT_OKTA_D_SOCKET"); s != "" {
		return s
	}
	return defaultDaemonSocket
}

// unreachableClient is the temporary auth.DaemonClient: agent-okta-d has no
// Go client yet (see docs/adr-daemon-client-stub.md), so every call reports
// the daemon as unreachable, naming the socket (exit 3, no fallback creds).
type unreachableClient struct{ socket string }

func (c unreachableClient) Fetch(context.Context, string) (auth.Token, error) {
	return auth.Token{}, &auth.UnreachableError{Socket: c.socket}
}

func (c unreachableClient) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, &auth.UnreachableError{Socket: c.socket}
}

// newDaemonClient returns the credential-daemon client. Swapping this one
// function for the real adapter is the whole change.
func newDaemonClient() auth.DaemonClient { return unreachableClient{socket: daemonSocket()} }
