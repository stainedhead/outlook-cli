# outlook-cli Specification

Date: 2026-10-03 | Source PRD: `specs/261003-outlook-cli/outlook-cli-PRD.md` | Status: Draft

## 1. Executive Summary
`outlook` is a Go CLI giving an autonomous agent read/triage/send access to its own Microsoft 365 mailbox via Microsoft Graph `/me/...`, built on `github.com/stainedhead/agent-cli-core v0.1.0` (packages `output`, `auth`, `policy`, `audit`, `httpx`, `selftest`, `docgen`, `auth/authtest`). Tokens come from the `agent-okta-d` daemon (provider `msgraph`); in this feature the daemon client is a stub reporting "daemon unavailable" (exit 3).

## 2. Problem Statement
Email is the riskiest agent channel: inbound is untrusted text, outbound is an exfiltration path. Agents need a guarded, attributable, idempotent mail tool with no credential in the agent process.

## 3. Goals / Non-Goals
Goals G1-G5 and non-goals as in the PRD section 2 (own mailbox only; no human mode; no forward/rules/delegation/settings/contacts/permanent delete; no calendar writes). Out of scope for this feature: M0 real-tenant spikes (delivered as a checklist doc), Exchange mail-flow/CA/DLP configuration, native Windows, release workflows, signing/notarization.

## 4. Functional Requirements
- FR-001 `outlook whoami`: mailbox, agent id, policy profile, effective limits; `GET /me?$select=mail,userPrincipalName`.
- FR-002 Start-up check: policy `mailbox` must equal `GET /me` mail/UPN, otherwise refuse (policy denial exit code from core).
- FR-003 `folder list` with unread counts.
- FR-004 `mail list` with `--folder --unread --from --since --limit --page-token`; summaries only, no bodies; folder restricted by policy `read.folders`.
- FR-005 `mail get <id> [--body text|none] [--max-bytes N]`; body as text via `Prefer: outlook.body-content-type="text"`; bounded by `max_body_bytes`.
- FR-006 `mail search "<q>" [--folder] [--limit]`.
- FR-007 `mail send` (--to/--cc/--subject/--body|--body-file/--dry-run/--idempotency-key); subject prefix, footer, `X-Agent-Id`/`X-Agent-Run` headers; policy: allow_domains, allow_addresses, external deny|draft_only|allow, max_total, bcc deny, attachments deny, body max, secret/classification content filters, rate per hour/day.
- FR-008 Idempotency: local ledger keyed by `--idempotency-key` (P0); a retried send does not send twice. Sent Items header check (P1) is an assumption.
- FR-009 `--dry-run` validates policy and renders final message without sending.
- FR-010 `mail reply <id> --body`; `--all` denied by default.
- FR-011 `mail draft create|list|send|delete` (delete only agent's own drafts); `external: draft_only` creates a draft.
- FR-012 `mail mark --read|--unread`; FR-013 `mail move --folder NAME` (never Deleted Items).
- FR-014 `attachment list`; FR-015 `attachment get --out DIR` off by default; when enabled: type allowlist, size cap, quarantine dir, never opened.
- FR-016 Untrusted-content envelope (PRD section 10): subject/body/display names/attachment names flagged `untrusted`; HTML to text, images never fetched, links listed separately and defanged, `sender_trust` heuristic (domain match, never authorization).
- FR-017 `outlook selftest` via core `selftest`: allow/deny matrix (read; fake-backed in CI, live mode on demand only).
- FR-018 `outlook version` (semver, commit, build date via ldflags; REL-4).
- FR-019 Auth handling: 401 force one refresh and retry once; `reauth_required` exit 3 with enroll message; 403 exit 4 with Graph error code, no body; 429/503 honor `Retry-After`, exit 8 after bounded retries (AUTH-1..4).
- FR-020 Audit JSONL per command (no bodies) via core `audit`; token never logged.
- FR-021 `make skill` generates the skill document via core `docgen` into `dist/` (git-ignored); sample policy file with placeholders shipped under `docs/` or `user-docs/`.
- FR-022 `calendar list` is P2 and NOT built here (documented in docs/deferred.md).
- FR-023 No command accepts a mailbox parameter; no `/users/{id}` call exists anywhere (enforced by a test scanning Graph URLs).

## 5. Non-Functional Requirements
Go 1.27, static binary (CGO off), darwin/arm64, linux/amd64, linux/arm64 cross-compile; local overhead <50 ms; >=90% coverage for domain and use-case packages; gofmt, go vet, golangci-lint (v2 config), `go test -race ./...`; no network or credentials in tests; never send real mail; deny by default.

## 6. Architecture
Clean Architecture: `internal/domain` (entities, policy rules, link defanging, ledger interface), `internal/usecase` (use cases + ports), `internal/adapter/graph` (Graph HTTP client on core `httpx`), `internal/adapter/ledger` (file ledger), `internal/adapter/cli` (command wiring on core `output`/`docgen`), `cmd/outlook` (composition root, `newDaemonClient()` stub). Inner layers never import adapters; an arch test enforces it.

### Daemon-client stub rule
`cmd/outlook` defines `newDaemonClient()` returning an implementation of the core's auth/daemon-client interface that always reports daemon unavailable (exit 3 with the core's message naming the socket). Swapping to the real client is a one-line change. ADR in `docs/` and entry in `docs/deferred.md`. `agent-okta-d` MUST NOT appear in `go.mod`.

### Core-dependency rules
`go.mod` has `require github.com/stainedhead/agent-cli-core v0.1.0`; no `replace`, no pseudo-versions. Use core packages; never copy core code. Missing core capability: do not edit core; record in `docs/requested-core-changes.md`. Private fetch: `GOPRIVATE=github.com/stainedhead/*`.

### Unverified-assumption rules
Every item the PRD marks unconfirmed (all Graph endpoint shapes, `$search`, `Prefer` header behavior, custom `x-` headers, Sent Items idempotency, auth-results, folder well-known names) is implemented behind an explicit assumption: a code comment `ASSUMPTION(unverified against a real tenant)`, a test whose name contains `Assumption`, and a row in `docs/unverified-assumptions.md`. M0 spikes are a checklist in `docs/m0-spike-checklist.md`.

## 7. Scope of Changes
Create: `go.mod/go.sum`, `cmd/outlook/`, `internal/...`, `docs/{architecture,adr,deferred,requested-core-changes,unverified-assumptions,m0-spike-checklist}`, `user-docs/{install,getting-started,configuration,usage,troubleshooting}.md`, sample policy, Makefile targets (`skill`, cross-compile). Keep `.github/workflows/ci.yml` working. No release workflows.

## 8. Breaking Changes
None (greenfield).

## 9. Acceptance Criteria and Quality Gates
- AC-1 (FR-001..006, 014) read commands return the envelope with bounded output against an httptest Graph; folders outside policy refused.
- AC-2 (FR-007..011) send/reply/draft honor every policy rule; external denied; BCC, reply-all, attachments refused; dry-run sends nothing.
- AC-3 (FR-008) same idempotency key twice results in exactly one POST.
- AC-4 (FR-012,013) mark/move work; move to Deleted Items refused.
- AC-5 (FR-015) attachment download refused by default; with policy enabled only allowed types/sizes, saved to quarantine.
- AC-6 (FR-016) injection corpus and hostile HTML appear only inside `untrusted` fields; links defanged; images never fetched.
- AC-7 (FR-019) 401 retries once; reauth_required exit 3; 403 exit 4 without body; 429/503 honor Retry-After then exit 8.
- AC-8 (FR-002, 023) mailbox mismatch refused; test scan proves no `/users/` URL or mailbox flag exists.
- AC-9 (FR-018, 021, NFR) `outlook version` stamped; `make skill` writes `dist/outlook-cli.md`; three targets cross-compile.
- AC-10 (stub rule) with the stub daemon every Graph command exits 3 with core's message; `go.mod` has core v0.1.0, no replace, no agent-okta-d.
- AC-11 (FR-020) audit line per command, no bodies, no tokens.
- Quality gates: gofmt empty, go vet, golangci-lint, `go test -race ./...`, >=90% coverage domain/usecase, arch test.

## 9a. Edge Cases and Error Paths
Empty/missing fields (no subject, empty body, null recipients) validated; CRLF/header injection in subject and recipients rejected; duplicate and case-variant recipients deduplicated before max_total; oversized body/`--body-file` truncated or refused per bounds; pagination token invalid/expired gives usage error; Graph 404 on message id maps to not-found exit code; malformed/partial Graph JSON gives an internal error, never panics; network/timeouts bounded via context; concurrent runs: ledger uses file locking with atomic write so two writers cannot both send; ledger corruption fails closed (no send); clock skew in rate windows uses core clock; unreadable or agent-writable policy file fails closed; unknown policy keys rejected; permission boundary: 403 never retried.

## 9b. Open Questions (owner / resolution path)
1. Exact core interface names for the daemon-client stub (lead; read core in Phase A).
2. Whether core `policy` supplies recipient/rate primitives or only an engine (WS4; if lacking, record in docs/requested-core-changes.md).
3. All Graph shapes (WS2; resolved only by M0 spike on a real tenant, out of scope).
4. Tenant/admin questions in PRD sections 15 and 16.8 (EA/Exchange team; out of scope).

## 10. Risks and Mitigation
Graph shapes wrong (assumption rules); core missing features (requested-core-changes); daemon not available (stub); parallel workstream merge conflicts (disjoint package ownership).

## 11. Timeline and Milestones
Implementation phases in plan.md: foundation, domain/usecases + adapters in parallel, CLI composition, policy/audit wiring, docs, integration.

## 12. References
Source PRD: `specs/261003-outlook-cli/outlook-cli-PRD.md`. Review: `outlook-cli-auto-review-PRD.md`.
