// Command outlook is the agent-facing Microsoft Graph mail CLI.
package main

import (
	"fmt"
	"os"

	"github.com/stainedhead/agent-cli-core/output"
)

// Build metadata, stamped with -ldflags by the Makefile (REL-4).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(int(run()))
}

func run() output.ExitCode {
	env := output.Success(map[string]string{"version": version, "commit": commit, "date": date}, nil)
	if err := output.Write(os.Stdout, env, output.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return output.ExitOf(err)
	}
	return env.ExitCode()
}
