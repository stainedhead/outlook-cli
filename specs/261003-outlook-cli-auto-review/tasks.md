# Tasks: outlook-cli-auto-review (2026-10-03)
Status: Planning
Progress: 0/17 tasks complete

Each FR task: failing test first (name references FR id), fix, gates, per-FR review-code, merge.

- [ ] A1 FR-R11 id validation (graph) - deps: none
- [ ] A2 FR-R1 signed page token (graph, usecase/read.go) - deps: A1 (shared client.go), B-key wiring
- [ ] A3 FR-R6 graph half: single header const, probe 400 inconclusive - deps: none; hand to C
- [ ] A4 FR-R4 graph half: dto replyTo/sender and select - deps: none; hand to C
- [ ] B1 FR-R2 policy ownership check, OUTLOOK_POLICY restriction, whoami/selftest path
- [ ] B2 FR-R13 docs trust model; auditlog sink fields
- [ ] B3 FR-R14 cmd/outlook coverage >=85% (after B1)
- [ ] C1 FR-R3 draft send attachments/raw scan
- [ ] C2 FR-R4 usecase half (after A4)
- [ ] C3 FR-R6 usecase half (after A3)
- [ ] C4 FR-R8 usecase half + ports.go ReserveWithin (port first; D implements)
- [ ] C5 FR-R9 draft integrity
- [ ] C6 FR-R13 audit recipient hash/ids/HTTPStatus (service.go)
- [ ] D1 FR-R8 ledger half (after C4 port)
- [ ] D2 FR-R12 quarantine
- [ ] D3 FR-R13 ledger dir perm check
- [ ] E1 FR-R5, E2 FR-R10, E3 FR-R7 (cli/domain untrusted)
- [ ] F1 FR-R14 docs reconcile, threat model, unverified-assumptions
