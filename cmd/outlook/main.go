// Command outlook is the agent-facing Microsoft Graph mail CLI.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/stainedhead/outlook-cli/internal/adapter/cli"
)

// Build metadata, stamped with -ldflags by the Makefile (REL-4).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], deps())
	stop()
	os.Exit(int(code))
}

// deps assembles the CLI dependencies. The only wiring point for the real
// graph is buildApp (app.go).
func deps() cli.Deps {
	return cli.Deps{
		NewCommands: buildApp,
		Selftest:    selftestFor(prodConfig()),
		Build:       cli.BuildInfo{Version: version, Commit: commit, Date: date},
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}
}
