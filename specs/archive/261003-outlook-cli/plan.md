# Plan: outlook-cli (2026-10-03) - Status: Planning

## Development Approach
Strict TDD, Clean Architecture, small commits on worker branches merged into `feat/outlook-cli`. Workers use their own git worktrees (`.worktrees/outlook-cli-<name>`) created from `feat/outlook-cli`.

## Phase Breakdown
- Phase A (serial, foundation): `go.mod` require core v0.1.0, skeleton dirs, shared domain types/ports (`internal/domain` types and `internal/usecase` port interfaces), arch test, Makefile. Owner: lead. Must land before workers branch.
- Phase B (parallel workstreams, disjoint package ownership):
  - WS1 domain+usecases: `internal/domain/**`, `internal/usecase/**` (policy evaluation rules, send/reply/draft/triage/read use cases, idempotency port, untrusted-content transforms: html-to-text, defang, sender_trust). Tests with fakes of ports.
  - WS2 graph adapter: `internal/adapter/graph/**` (+ `internal/adapter/ledger/**`). Implements ports against Graph via core `httpx`; httptest-based tests; 401/403/429/503 handling; every endpoint shape tagged as assumption. Never `/users/{id}`.
  - WS3 CLI/composition: `cmd/outlook/**`, `internal/adapter/cli/**`. Commands, flags, core `output` envelope and exit codes, `version`, `newDaemonClient()` stub + ADR, `make skill` docgen wiring. Depends on port interfaces from Phase A only (uses fakes until WS1/WS2 merge).
  - WS4 policy/audit wiring: `internal/adapter/policyfile/**`, `internal/adapter/auditlog/**`, `internal/adapter/selftestcfg/**` (core `policy` loading of the YAML in PRD section 9, core `audit` JSONL, `selftest` matrix, secret-pattern filter adapter).
  - WS5 docs: `docs/**`, `user-docs/**`, sample policy, `docs/unverified-assumptions.md`, `docs/m0-spike-checklist.md`, `docs/deferred.md`, `docs/requested-core-changes.md`. Touches no Go code.
- Phase C (serial): merge all branches, integration tests through the CLI with fakes + httptest, cross-compile, lint, coverage, skill generation, fix-ups.

## Ownership rules
No worker edits another workstream's directories. Changes to shared ports go through the lead (Phase A files `internal/usecase/ports.go`, `internal/domain/types.go`).

## Critical Path
Phase A -> WS1/WS2 -> WS3 integration -> Phase C.

## Testing Strategy
Table-driven unit tests, fakes (`authtest.Fake`), httptest Graph, fake clocks, hostile-HTML and injection corpus, header-injection tests, arch test, `go test -race ./...`, >=90% on domain/usecase.

## Rollout Strategy
No release in this feature; PR to main only.

## Success Metrics
All acceptance criteria in spec.md section 9 satisfied; quality gates green.
