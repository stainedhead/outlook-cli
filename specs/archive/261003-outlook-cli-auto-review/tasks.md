# Tasks: outlook-cli-auto-review (2026-10-03)
Status: In Progress (groups A-E and integration done; group F docs pending)
Progress: 16/17 tasks complete

Each FR task: failing test first (name references FR id), fix, gates, per-FR review-code, merge.

- [x] A1 FR-R11 id validation (graph) - deps: none
- [x] A2 FR-R1 signed page token (graph, usecase/read.go) - deps: A1 (shared client.go), B-key wiring
- [x] A3 FR-R6 graph half: single header const, probe 400 inconclusive - deps: none; hand to C
- [x] A4 FR-R4 graph half: dto replyTo/sender and select - deps: none; hand to C
- [x] B1 FR-R2 policy ownership check, OUTLOOK_POLICY restriction, whoami/selftest path
- [x] B2 FR-R13 docs trust model; auditlog sink fields
- [x] B3 FR-R14 cmd/outlook coverage >=85% (after B1)
- [x] C1 FR-R3 draft send attachments/raw scan
- [x] C2 FR-R4 usecase half (after A4)
- [x] C3 FR-R6 usecase half (after A3)
- [x] C4 FR-R8 usecase half + ports.go ReserveWithin (port first; D implements)
- [x] C5 FR-R9 draft integrity
- [x] C6 FR-R13 audit recipient hash/ids/HTTPStatus (service.go)
- [x] D1 FR-R8 ledger half (after C4 port)
- [x] D2 FR-R12 quarantine
- [x] D3 FR-R13 ledger dir perm check
- [x] E1 FR-R5, E2 FR-R10, E3 FR-R7 (cli/domain untrusted)
- [ ] F1 FR-R14 docs reconcile, threat model, unverified-assumptions
