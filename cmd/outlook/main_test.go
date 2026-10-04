package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/output"
)

func TestDaemonClientStubIsUnreachableExit3(t *testing.T) {
	t.Setenv("AGENT_OKTA_D_SOCKET", "/tmp/x.sock")
	c := newDaemonClient()
	for name, f := range map[string]func(context.Context, string) (auth.Token, error){"fetch": c.Fetch, "refresh": c.Refresh} {
		_, err := f(context.Background(), "msgraph")
		var u *auth.UnreachableError
		if !errors.As(err, &u) || u.Socket != "/tmp/x.sock" {
			t.Fatalf("%s: got %v", name, err)
		}
		if output.ExitOf(err) != output.ExitCode(3) {
			t.Errorf("%s: exit %d", name, output.ExitOf(err))
		}
	}
}

func TestDefaultSocket(t *testing.T) {
	t.Setenv("AGENT_OKTA_D_SOCKET", "")
	if daemonSocket() != defaultDaemonSocket {
		t.Error("default socket not used")
	}
}

func TestBuildAppFailsClosedWithoutPolicy(t *testing.T) {
	if _, err := commandsFor(prodConfig())(context.Background()); err == nil {
		t.Error("expected a policy error (default path absent)")
	}
}

func TestDepsWiresBuildInfo(t *testing.T) {
	d := deps()
	if d.NewCommands == nil || d.Build.Version != version {
		t.Error("deps incomplete")
	}
}
