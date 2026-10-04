# Spec: outlook-cli Review Fixes (261003)

## Executive Summary
Fix the 14 findings (2 P0, 6 P1, 6 P2) from the automated review of feat/outlook-cli. Two guardrails (folder scope via page token, policy trust anchor) are defeatable by an agent with shell access; several policy rules cover a narrower path than intended.

## Problem Statement
See `outlook-cli-auto-review-PRD.md` sections 1 and 3: page-token URL forgery, policy file authorship check defeated by chmod and OUTLOOK_POLICY, draft-send and reply policy gaps, invisible-character gaps, idempotency probe mismatch, unbounded --body-file, racy rate cap, plus P2 hardening.

## Goals / Non-Goals
Goals: close every FR-R1..R14 with failing-first tests and acceptance criteria met. Non-goals: Graph adapter rewrite, new commands, server-side enforcement, DLP beyond best effort, changing agent-cli-core.

## Functional Requirements
| ID | Pri | Summary |
|---|---|---|
| FR-R1 | P0 | Opaque authenticated page token; no URL acceptance |
| FR-R2 | P0 | Policy integrity by ownership (fstat, O_NOFOLLOW), restrict OUTLOOK_POLICY |
| FR-R3 | P1 | draft send honors send.attachments; scan raw body |
| FR-R4 | P1 | Reply policy evaluated on Reply-To |
| FR-R5 | P1 | CleanText strips tag chars, variation selectors, all Cf |
| FR-R6 | P1 | Single idempotency header constant; probe 400 inconclusive |
| FR-R7 | P1 | Bounded, regular-file-only --body-file |
| FR-R8 | P1 | Atomic rate reserve, pending counts, drafts distinct |
| FR-R9 | P2 | Draft author/fingerprint integrity |
| FR-R10 | P2 | Untrusted marking of addresses/urls; authserv-id check |
| FR-R11 | P2 | Validate ids as path segments |
| FR-R12 | P2 | Quarantine containment before mkdir; collision suffix |
| FR-R13 | P2 | Trust-model docs, audit forensic fields, ledger dir perms |
| FR-R14 | P2 | Regression tests, cmd/outlook >=85%, docs reconcile, threat model |
Full acceptance criteria: `outlook-cli-auto-review-PRD.md` section 3 (authoritative).

## Non-Functional Requirements
PRD section 4: fail closed, no secrets in logs, 50 ms overhead, no regression of exactly-once, compat except page-token format, stable rule ids.

## System Architecture
Layers touched: domain (untrusted, render, types), usecase (send, read, service, ports), adapters graph, cli, ledger, policyfile, auditlog, selftestcfg, composition root cmd/outlook. See plan.md for file ownership.

## Scope of Changes
See plan.md ownership table. No new dependencies.

## Breaking Changes
Page token format changes (documented in user-docs/usage.md). Exit codes, envelope, flags unchanged.

## Success and Acceptance Criteria
Per-FR criteria in the PRD; gates: gofmt, go vet, golangci-lint, go test -race -cover (domain/usecase >=90%, cmd/outlook >=85%), cross-compile 3 targets, review-code reports no Must Fix per FR.

## Risks and Mitigation
Merge conflicts on send.go (mitigated by single owner); unverified Graph behavior (Reply-To, header filter) assumed worst case; ports.go change ordering (see plan.md).

## Timeline and Milestones
Wave 1 parallel groups A-E; wave 2 docs/coverage pass F; final gates.

## References
Source PRD: `specs/261003-outlook-cli-auto-review/outlook-cli-auto-review-PRD.md`
