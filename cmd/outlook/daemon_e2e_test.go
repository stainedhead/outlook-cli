package main

// End-to-end tests of the real daemon adapter: the CLI command path runs
// against agent-okta-d's clienttest fake daemon on a real unix socket and the
// httptest Graph fake from integration_test.go. Nothing here sends real mail.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-okta-d/pkg/client/clienttest"
)

const e2eToken = "tok-E2E-SECRET-0123456789"

func e2eCred() clienttest.Credential {
	now := time.Now()
	return clienttest.Credential{
		TokenType: "Bearer", AccessToken: e2eToken, IssuedAt: now,
		ExpiresAt: now.Add(time.Hour), Audience: "https://graph.microsoft.com",
	}
}

// newDaemonEnv points the integration env at a fake daemon via the real
// adapter, selecting the socket the way production does: AGENT_OKTA_D_SOCKET.
func newDaemonEnv(t *testing.T, socket string) *itEnv {
	t.Helper()
	t.Setenv("AGENT_OKTA_D_SOCKET", socket)
	e := newITEnv(t, nil)
	e.cfg.Daemon = newDaemonClient()
	return e
}

func (e *itEnv) assertNoToken(t *testing.T, r result) {
	t.Helper()
	log, _ := os.ReadFile(e.audit)
	for name, text := range map[string]string{"stdout": r.raw, "stderr": r.errs, "audit": string(log)} {
		if strings.Contains(text, e2eToken) {
			t.Errorf("token leaked into %s", name)
		}
	}
}

func TestE2EReadWithTokenFromFakeDaemon(t *testing.T) {
	d := clienttest.New(t)
	d.SetCredential(graphProvider, e2eCred())
	e := newDaemonEnv(t, d.SocketPath())
	for _, args := range [][]string{{"whoami"}, {"mail", "list"}} {
		r := e.run(args...)
		r.wantExit(t, 0)
		e.assertNoToken(t, r)
	}
	if len(e.g.auths) == 0 {
		t.Fatal("Graph saw no requests")
	}
	for _, a := range e.g.auths {
		if a != "Bearer "+e2eToken {
			t.Errorf("Graph Authorization = %q, want the daemon's token", a)
		}
	}
	if e.g.totalWrites() != 0 {
		t.Errorf("read commands wrote to Graph: %v", e.g.posts)
	}
}

func TestE2ENoDaemonExit3NamesSocket(t *testing.T) {
	dead := clienttest.DeadSocketPath(t)
	e := newDaemonEnv(t, dead)
	r := e.run("mail", "list")
	r.wantExit(t, 3)
	if !strings.Contains(r.raw, dead) {
		t.Errorf("error should name the socket %s: %s", dead, r.raw)
	}
	if len(e.g.paths) != 0 {
		t.Error("no Graph request should be made without a token")
	}
}

func TestE2EReauthAndRevokedExit3WithRemediation(t *testing.T) {
	for _, code := range []string{clienttest.CodeReauthRequired, clienttest.CodeRevoked} {
		t.Run(code, func(t *testing.T) {
			d := clienttest.New(t)
			d.SetProviderError(graphProvider, clienttest.Error{Code: code})
			e := newDaemonEnv(t, d.SocketPath())
			r := e.run("mail", "list")
			r.wantExit(t, 3)
			if !strings.Contains(r.raw, "agent-okta-d enroll msgraph") {
				t.Errorf("missing remediation text: %s", r.raw)
			}
			if len(e.g.paths) != 0 {
				t.Error("no Graph request should be made without a token")
			}
		})
	}
}

func TestE2EAccessErrorsExit3(t *testing.T) {
	d := clienttest.New(t) // serves no credential: not_configured
	e := newDaemonEnv(t, d.SocketPath())
	r := e.run("mail", "list")
	r.wantExit(t, 3)
	if !strings.Contains(r.raw, "not configured in the credential daemon") {
		t.Errorf("access error hint lost: %s", r.raw)
	}
}

func TestE2EDegradedAndRetryHintExit8(t *testing.T) {
	cases := map[string]struct {
		err  clienttest.Error
		hint string
	}{
		"degraded":   {clienttest.Error{Code: clienttest.CodeDegraded, State: "degraded"}, "Retry later."},
		"retry-hint": {clienttest.Error{Code: clienttest.CodeInternal, RetryAfter: 7 * time.Second}, "Retry after 7 seconds."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := clienttest.New(t)
			d.SetProviderError(graphProvider, tc.err)
			e := newDaemonEnv(t, d.SocketPath())
			r := e.run("mail", "list")
			r.wantExit(t, 8)
			if !strings.Contains(r.raw, tc.hint) {
				t.Errorf("missing retry hint %q: %s", tc.hint, r.raw)
			}
			if len(e.g.paths) != 0 {
				t.Error("no Graph request should be made without a token")
			}
		})
	}
}
