# Tasks: outlook-cli (2026-10-03) - Status: Planning
Progress: 0/22 tasks complete

## Phase A (lead, serial)
- A1 go.mod require core v0.1.0; verify fetch (GOPRIVATE). Acceptance: `go mod tidy` clean.
- A2 Skeleton dirs, ports (`internal/usecase/ports.go`), domain types, arch test, Makefile targets. Depends A1.
## WS1 domain/usecases (depends A2)
- W1.1 Policy evaluation (recipients, caps, rate, bcc, reply-all, content filters)
- W1.2 Untrusted transforms (html-to-text, link defang, sender_trust)
- W1.3 Read use cases (whoami, folders, list, get, search, mailbox check)
- W1.4 Send/reply/draft use cases + idempotency + dry-run + headers/footer
- W1.5 Triage + attachment use cases
## WS2 graph adapter (depends A2)
- W2.1 Graph client on httpx + auth retry/error mapping (AUTH-1..4)
- W2.2 Read endpoints; W2.3 Write endpoints; W2.4 file idempotency ledger; W2.5 assumption tags/tests
## WS3 CLI/composition (depends A2)
- W3.1 Command tree + flags + envelope; W3.2 `version`; W3.3 `newDaemonClient()` stub + ADR; W3.4 `make skill`/docgen; W3.5 cross-compile targets
## WS4 policy/audit
- W4.1 YAML policy loader; W4.2 audit JSONL; W4.3 selftest matrix; W4.4 secret-pattern filter
## WS5 docs
- W5.1 docs/ set; W5.2 user-docs/ set + sample policy
## Phase C
- C1 Merge + integration tests; C2 quality gates (fmt, vet, lint, race, coverage, cross-compile)
