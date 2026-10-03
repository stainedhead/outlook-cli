package main

import (
	"context"

	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

// buildApp is the composition root: it constructs the real adapters (Graph
// reader/writer, ledger, quarantine, policy file, audit log, content filters,
// auth.Authorizer over newDaemonClient()) and returns usecase.New(deps).
//
// PHASE C: fill this in. It is deliberately one function so the integration
// step touches nothing else. Until then every mailbox command reports that
// the binary is not wired (exit 1); version, skill and help work.
func buildApp(_ context.Context) (usecase.Commands, error) {
	_ = newDaemonClient // keeps the stub referenced until Phase C wires auth
	return nil, domain.NewGeneral("outlook is not wired to a mailbox backend in this build")
}
