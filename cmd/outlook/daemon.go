package main

import (
	"context"
	"errors"
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

// daemonSource wraps core's DaemonTokenSource to keep the adapter's own
// errors visible. As of agent-cli-core v0.2.0 the token source wraps every
// error it does not know in an auth.TokenError, whose category (auth, exit 3)
// hides the adapter's *oktad.TransientError (rate_limited, exit 8, retry
// hint) and *oktad.AccessError (hint). Workaround until core passes them
// through; see docs/requested-core-changes.md item 20.
type daemonSource struct{ *auth.DaemonTokenSource }

func (s daemonSource) Token(ctx context.Context) (auth.Token, error) {
	t, err := s.DaemonTokenSource.Token(ctx)
	return t, surfaceAdapterError(err)
}

func (s daemonSource) Refresh(ctx context.Context) (auth.Token, error) {
	t, err := s.DaemonTokenSource.Refresh(ctx)
	return t, surfaceAdapterError(err)
}

func surfaceAdapterError(err error) error {
	var te *oktad.TransientError
	if errors.As(err, &te) {
		return te
	}
	var ae *oktad.AccessError
	if errors.As(err, &ae) {
		return ae
	}
	return err
}
