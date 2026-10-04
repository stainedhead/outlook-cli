package usecase

import "github.com/stainedhead/agent-cli-core/output"

func exitOf(err error) int { return int(output.ExitOf(err)) }
