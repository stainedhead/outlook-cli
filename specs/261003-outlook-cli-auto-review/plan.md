# Plan: outlook-cli-auto-review (2026-10-03)
Status: Planning

## Development Approach
Strict TDD per FR, per-FR review-code (second agent for P0/P1), one worktree and branch per group off feat/outlook-cli (`git worktree add -b fix/<grp> .worktrees/outlook-cli-<grp> feat/outlook-cli`), merge into feat/outlook-cli, rerun go test -race ./... after each merge. Never force-push, never touch main.

## Workstreams (disjoint file ownership; grouped by real file overlap)
Overlap analysis: FR-R3, R4, R6, R8, R9, R13 all edit usecase/send.go, so they share one owner (C). FR-R1, R11, R4/R6 graph halves all edit adapter/graph (client.go, read.go, dto.go, probe.go), so owner A. FR-R8, R12, R13 ledger parts all in adapter/ledger, owner D. FR-R5, R10 share domain/untrusted.go and cli/present.go with R7 in cli, owner E. FR-R2, R14 and R13 docs/audit share app.go, policyfile, owner B.

| Group | FRs | Exclusive files |
|---|---|---|
| A graph and token | R11, R1, R4 (graph half), R6 (graph half) | internal/adapter/graph/** , internal/usecase/read.go |
| B trust anchor and composition | R2, R13 (docs, auditlog), R14 (cmd coverage) | cmd/outlook/** , internal/adapter/policyfile/** , internal/adapter/selftestcfg/** , internal/adapter/auditlog/** , internal/adapter/cli/meta.go |
| C use-case send path | R3, R4 (use case), R6 (use case), R8 (use case), R9, R13 (audit fields) | internal/usecase/send.go, ports.go, service.go, triage.go, attachment.go, internal/domain/{render,types,policy_types,policy_eval,errors}.go |
| D ledger | R8 (ledger half), R12, R13 (ledger perms) | internal/adapter/ledger/** |
| E untrusted and CLI input | R5, R10, R7 | internal/domain/{untrusted,address}.go, internal/adapter/cli/{present,flags,cli,commands}.go |
| F docs (last) | R14 docs, threat model, reconcile | docs/**, user-docs/** (groups A-E must NOT edit these; they leave notes in implementation-notes.md or PR text) |

Rule: a group needing a change in a file it does not own asks the owner or waits; no cross-group edits.

## Cross-group dependencies and ordering
1. A3 (single header constant, probe 400) and A4 (dto replyTo/sender) merge first (small); C2/C3 start after.
2. C4 lands the ports.go ReserveWithin/ledger-kind interface first; D1 implements it afterward. If D is ready earlier, D waits for the merge, not the other way around.
3. A2 (FR-R1) needs a per-install HMAC key wired in the composition root: B adds the key option in cmd/outlook (small, early merge); A consumes it behind an interface/option with a test double. A also changes usecase/read.go for per-page folder recheck; cli flag plumbing needed by R1 goes through E (commands.go) if required.
4. B1 (FR-R2) precedes B3 (coverage). F runs last after A-E merged.
5. R14 per-FR regression tests ride with each FR's group.

## Phase Breakdown
Wave 1: A, B, C, D, E in parallel (with ordering above). Wave 2: F docs and threat model. Wave 3: full gates, cross-compile.

## Critical Path
A3/A4 -> C2/C3; C4 -> D1; B key -> A2.

## Testing Strategy
Failing-first tests named with FR id; httptest zero-hit assertions; concurrency tests under -race repeated; injected stat for ownership tests.

## Rollout Strategy
Merged into feat/outlook-cli, then PR; page-token break documented.

## Success Metrics
All acceptance criteria met; coverage domain/usecase >=90%, cmd/outlook >=85%; no Must Fix findings.
