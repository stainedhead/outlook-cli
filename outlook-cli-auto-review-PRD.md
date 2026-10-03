# outlook-cli Review-Fixes PRD (automated code review of feat/outlook-cli)

- Source: dev-flow:review-code of branch `feat/outlook-cli` against `specs/261003-outlook-cli/spec.md`
- Reviewed at commit 7f8d8e7 (all 10 packages green under `go test -race`, `go vet`, `golangci-lint`, `gofmt` clean)
- Overall assessment: **Request changes** (2 P0, 6 P1, 6 P2)

## 1. Executive Summary

The implementation is structurally sound. Clean Architecture holds (an arch test enforces layer imports), domain and use-case coverage is 97-99 percent, every spec acceptance criterion has a test, errors never carry response bodies or tokens, header and recipient injection are rejected, and the idempotency ledger is atomic, locked and fails closed.

The review found that two of the guardrails the product exists to enforce can be defeated by an agent (or by a prompt-injected agent) with ordinary shell access, and several policy rules are enforced on a path that is narrower than the spec implies:

1. **P0** The opaque `--page-token` is a base64 Graph URL that is only checked for host and a `/me/` prefix. A caller can substitute any `/me/...` URL (all folders, `$search` across the mailbox) and even `/me/../users/{other}/...`, bypassing `read.folders` and FR-023.
2. **P0** The policy trust anchor is bypassable. `OUTLOOK_POLICY` lets the agent point at any file, and the "agent cannot write its own guardrails" check is `access(W_OK)` on the file and its directory. A file and directory the agent creates and then `chmod a-w` pass the check (verified: `os.access` returns false for both).
3. **P1** `mail draft send` ignores `send.attachments`, does not check Reply-To style redirection, and scans converted text rather than what Graph will send.
4. **P1** `mail reply` evaluates policy against `From`, but Graph's reply goes to Reply-To when present (unverified assumption); a spoofed internal sender with an external Reply-To bypasses the allow-list.
5. **P1** `CleanText` strips bidi and zero-width characters but not Unicode tag characters (U+E0000 block, "ASCII smuggling"), variation selectors, U+180E, U+061C or U+2028/9 (verified by probe). Hidden prompt-injection text reaches the model inside the `untrusted` wrapper.
6. **P1** The Sent Items probe searches `X-Idempotency-Key` but sends emit `X-Agent-Idempotency-Key`; and a probe error (the code's own doc comment predicts HTTP 400) aborts every keyed send, contradicting `docs/unverified-assumptions.md` which says the use case treats it as inconclusive.
7. **P1** `--body-file` reads any file with no size bound (`os.ReadFile`) and no path restriction.
8. **P1** Rate caps count only `sent` ledger entries, so concurrent runs both pass the check, pending (ambiguous) sends never count, and draft creations are counted as sends.

Remaining P2 items cover draft-send integrity, untrusted-marking gaps, ID path segments, quarantine directory creation order, ledger/audit trust and forensic detail, and test and doc gaps.

Nothing was fixed in this review. Every FR below carries acceptance criteria and a regression-test requirement.

## 2. Process Guidance for the Fix Phase (applies to every FR)

- **TDD is mandatory.** For each FR, first write the failing test that reproduces the finding (the exact probe in the FR), watch it fail for the stated reason, then make the smallest change that turns it green, then refactor. Commit test and fix together or test first. Domain and use-case packages stay at or above 90 percent coverage; do not lower `cmd/outlook` coverage.
- **Per-fix code review.** After each FR is green, run `dev-flow:review-code` (or `/code-review`) on that FR's diff before merging it into `feat/outlook-cli`. A fix is not done until the review reports no Must Fix. Security FRs (P0/P1) get a second look from a different agent than the author.
- **Agent teammates.** Independent FRs may be assigned to separate teammates in parallel. Suggested split to avoid file conflicts: A = FR-1, FR-11 (`internal/adapter/graph`); B = FR-2 (`internal/adapter/policyfile`, `cmd/outlook`); C = FR-3, FR-4, FR-9 (`internal/usecase/send.go`, `internal/domain`); D = FR-5, FR-10 (`internal/domain/untrusted.go`, `cli/present.go`); E = FR-6, FR-8, FR-13 (`ledger`, `graph/probe.go`, `usecase/send.go` dispatch; coordinate with C because both touch `send.go`); F = FR-7, FR-12 (`cli`, `ledger/quarantine.go`). Teammates report test output and VERDICT; the lead merges.
- **Git worktrees.** Each teammate works in its own worktree and branch off `feat/outlook-cli`, for example `git worktree add -b fix/<fr> .worktrees/outlook-cli-<fr> feat/outlook-cli`, never in the shared checkout. Merge back into `feat/outlook-cli` one FR at a time after its review, rerun `go test -race ./...` after each merge, never force-push, never touch `main`.
- **Gates for every FR:** `gofmt -l .` empty, `go vet ./...`, `golangci-lint run`, `go test -race -cover ./...`, cross-compile for darwin/arm64, linux/amd64, linux/arm64, no network, no real mail.
- **Docs:** any behavior change updates `docs/`, `user-docs/` (usage only, no design material) and the generated skill table; claims that stay unverified remain listed in `docs/unverified-assumptions.md`.

## 3. Findings

### FR-R1 (P0) Page token can address any `/me/` URL, bypassing folder scope and FR-023

**Where:** `internal/adapter/graph/paging.go` `decodeToken`; consumed by `read.go` `listMessages`, `ListMessages`, `SearchMessages`, `ListDrafts`.

**Finding:** `decodeToken` accepts any URL on the Graph host whose path starts with `<base>/me/`. Probe (temporary test, removed): tokens for `/me/messages?$select=subject&$top=50`, `/me/mailFolders/DELETEDID/messages`, `/me/messages/AAA/attachments` and `/me/../users/boss@x.com/messages` were all accepted and returned verbatim. The use case checks `read.folders` before passing the token, but the token itself carries its own folder. A prompt-injected agent can read summaries (sender, subject, recipients) from Deleted Items, Sent Items, any folder, or the whole mailbox via `$search`, and `..` segments may reach `/users/{id}` if the server normalizes the path.

**Fix direction:** Do not accept URLs. Make the token an opaque, authenticated reference: encode only the `$skiptoken`/`$skip` value plus the folder id and operation (list, search, drafts), sign or bind it (HMAC with a per-install key, or re-derive the query from the request and take only the cursor from the token), and re-check the folder against policy on every page. Reject `..`, `.`, encoded slashes, extra query keys and non-`messages` collection paths.

**Acceptance criteria:**
- A token whose URL targets `/me/messages`, another folder id, `/me/mailFolders/<not-readable>/messages`, any `/users/` path, a `..` or `%2e%2e` segment, an extra host, port, userinfo or fragment is refused with usage exit 2 and no request is sent (httptest asserts zero hits).
- A token minted for folder A with `mail list --folder A` is refused when used with `--folder B` or with `mail search`.
- A legitimate multi-page walk of `mail list`, `mail search --folder` and `mail draft list` still works end to end.
- Existing test `read_test.go` (other-path token) is extended with the cases above, written first and shown failing.
- FR-023 scan test additionally fails if any request path in the whole integration suite contains `..`.

### FR-R2 (P0) Policy file integrity check is defeated by `OUTLOOK_POLICY` and uses the wrong test

**Where:** `cmd/outlook/app.go` (`envPolicy`, `prodConfig`), `internal/adapter/policyfile/policy.go` `Load`, `perm_unix.go`.

**Finding:** The loader refuses a policy that the current user can write, using `access(2) W_OK` on the file and its directory. The agent also controls the environment of its own process, so it can create `~/p/policy.yaml` with any content, `chmod a-w` the file and directory, and set `OUTLOOK_POLICY`. Probe: `os.access(file, W_OK)` and `os.access(dir, W_OK)` are both false for a user-owned read-only file and directory. The check therefore does not prove that someone other than the agent authored the policy, which is the stated purpose ("the agent must not be able to edit its own guardrails"). The audit path inside the policy is attacker-chosen as well. The same applies to `AGENT_OKTA_D_SOCKET` redirecting the credential socket.

**Fix direction:** Prove authorship by ownership, not mode: require the file and its parent directories up to the root to be owned by root (or by a configured uid other than the effective uid), not group- or world-writable, and not reached through a symlink owned by the agent; open with `O_NOFOLLOW` and verify with `fstat` on the opened descriptor (no check-then-read race). Ignore `OUTLOOK_POLICY` in production builds, or accept it only when the target also passes the ownership test; document the supported override for development behind an explicit build tag or flag that selftest reports.

**Acceptance criteria:**
- A policy file owned by the current non-root user is refused with policy_denied exit 6 regardless of mode bits, in a temp-dir test using injected `stat` (owner uid != 0 and == euid refused; root-owned 0644 in root-owned 0755 dir accepted).
- A policy file in a directory whose ancestor is writable by the agent uid is refused.
- A symlinked policy path is resolved and the target and its directories are checked; a swap between check and read is not possible (check uses the opened descriptor).
- `OUTLOOK_POLICY` pointing at an agent-owned read-only file is refused; documented dev override cannot be enabled by environment alone in a release build.
- `outlook whoami` and `selftest` report which policy path is in force.
- Docs (`user-docs/configuration.md`) state the ownership requirement and the install commands.

### FR-R3 (P1) `mail draft send` skips attachment policy and scans converted text

**Where:** `internal/usecase/send.go` `sendDraft`.

**Finding:** `sendDraft` builds `SendInput` without `HasAttachments`, although `GetMessage` already expands attachments (`raw.Attachments`, `raw.HasAttachments`). A draft carrying attachments is sent even when `send.attachments` is deny. The content filters scan `convertBody` output (HTML to text, links defanged, truncated to 2 MiB), while Graph sends the original draft body, so markup, hidden elements, attribute values and bytes past 2 MiB are never scanned. `EvalBodySize` is also evaluated on the converted text, not the sent body.

**Acceptance criteria:**
- A draft with `HasAttachments` or a non-empty `Attachments` list is refused with rule `send.attachments` (exit 6) unless policy allows attachments, and the refusal is audited with that rule id.
- A secret placed only in an HTML attribute, an HTML comment or beyond 2 MiB of a draft body is detected by the filters, or the draft is refused as unscannable; the test names each case.
- Body size is checked against the original (raw) content length.
- Dry run reports the same decision as the real send.

### FR-R4 (P1) Reply policy is evaluated against `From`, but Graph may reply to Reply-To

**Where:** `internal/usecase/send.go` `reply`, `internal/adapter/graph/dto.go` (no `replyTo` field), `write.go` `ReplyToSender`.

**Finding:** The recipient checked is `orig.From`. `POST /me/messages/{id}/reply` addresses the reply to the message's Reply-To when set (assumption to verify against a tenant). A hostile message with `From:` an allow-listed or internal address (From is spoofable; `sender_trust` is advisory) and `Reply-To:` an attacker address makes the agent send its reply, with the quoted thread, outside the allow-list. `Sender`/`replyTo` are not selected at all, so the use case cannot see the problem.

**Fix direction:** Select `replyTo` and `sender`, compute the effective reply recipients, evaluate policy on all of them, and refuse (or require draft_only) when Reply-To differs from From. Alternative: create the reply as a draft via `createReply`, read back its recipients, re-validate, then send.

**Acceptance criteria:**
- With `replyTo` present and different from `from`, the reply is evaluated against the Reply-To addresses; an external or non-allow-listed Reply-To yields deny (or draft_only per policy) with rule `send.recipients.*`, and nothing is POSTed.
- Matching From and Reply-To behave as today.
- The assumption is recorded in `docs/unverified-assumptions.md` and in the M0 checklist as a tenant test.

### FR-R5 (P1) Invisible-character stripping misses Unicode tag characters and other format characters

**Where:** `internal/domain/untrusted.go` `CleanText`.

**Finding:** Probe: `CleanText("hi\U000E0049\U000E0067...")` returns the tag characters unchanged; U+2028, U+180E, U+061C and U+FE0F also survive. Tag characters render invisibly and are interpreted by some models as text, so an injected instruction can be hidden in subject, body, display name or attachment name. The `untrusted` wrapper is the only mitigation.

**Acceptance criteria:**
- `CleanText` removes or visibly escapes (for example `<U+E0049>`) the Unicode tag block U+E0000-U+E007F, variation selectors U+FE00-U+FE0F and U+E0100-U+E01EF, U+061C, U+180E, U+2028, U+2029 and every other `unicode.Cf` rune, while keeping ordinary text, emoji with ZWJ sequences documented as stripped or preserved explicitly by a test.
- A table test with the injection corpus (AC-6) includes a tag-character payload and asserts it does not appear in output bytes.
- The same function is applied to every untrusted field (subject, body, names, link text, attachment names).

### FR-R6 (P1) Idempotency probe: header mismatch, fatal on error, docs say otherwise

**Where:** `internal/adapter/graph/client.go` (`DefaultIdempotencyHeader = "X-Idempotency-Key"`), `internal/domain/render.go` (`HeaderIdempotencyKey = "X-Agent-Idempotency-Key"`), `internal/usecase/send.go` `deliver`, `docs/unverified-assumptions.md`.

**Finding:** (a) The probe looks for a header name that no outgoing message carries, and `app.go` never sets `Config.IdempotencyHeader`, so the probe can never return found. (b) `deliver` treats a probe error as `release()` plus return the error, so if Graph rejects `internetMessageHeaders` filtering (the code's own doc comment says HTTP 400 is likely) every send and draft send that has `--idempotency-key` fails. The test `TestProbeErrorFailsClosedAndFreesKey` locks that behavior in. (c) `docs/unverified-assumptions.md` says the use case treats the 400 as inconclusive, which is not what the code does. (d) For `draft send` the headers are rendered in dry run but `SendDraft(id)` cannot add them, so they are never on the message.

**Acceptance criteria:**
- One header constant is used by writer and probe (the test asserts the outgoing header name equals the probed name).
- A probe error that is a validation/unsupported response (HTTP 400) is treated as inconclusive: the send proceeds on the ledger alone, an audit field or stderr note records `probe=inconclusive`; network, auth and 5xx probe errors keep today's fail-closed behavior. Both paths have tests.
- The probe is skipped, and documented as skipped, for `draft send` and `reply` where the header cannot be applied; dry-run output no longer shows headers that will not be sent.
- Docs match behavior.

### FR-R7 (P1) `--body-file` is an unbounded arbitrary file read

**Where:** `internal/adapter/cli/cli.go` (default `ReadFile` is `os.ReadFile`), `flags.go` `resolveBody`.

**Finding:** Stdin is bounded by `maxBodyInput` (4 MiB) but a path is read in full with no limit: `--body-file /dev/zero` or a multi-GB file exhausts memory, and any file the agent user can read (keys, tokens, `/proc/self/environ`) can be put into a body. The secret filters are best effort and deliberately narrow.

**Acceptance criteria:**
- File input is read through a bounded reader (same cap as stdin, tested with a fake larger-than-cap reader and `/dev/zero`), failing with usage exit 2 and a message naming the cap.
- Only regular files are accepted (no devices, FIFOs, directories); symlinks are resolved and the result must be a regular file.
- Document that `--body-file` can read anything the user can; optionally add policy `send.body_file_roots` and test that a path outside is refused (decide in review; default must not break `--body-file report.txt`).

### FR-R8 (P1) Rate cap is racy, undercounts pending sends and counts drafts as sends

**Where:** `internal/usecase/send.go` `checkRate`, `deliver`; `internal/adapter/ledger/ledger.go` `SentSince`, `Complete`.

**Finding:** (a) `checkRate` runs after `Reserve` but counts only `sent` entries; two concurrent processes both read the count below the cap and both send (no lock spans check and reserve). (b) An ambiguous send (timeout, 5xx) stays `pending` forever and is never counted, so retries with new keys silently exceed the cap. (c) A keyed draft creation (`DecisionDraftOnly` path in `dispatch`) is completed as `sent` and therefore consumes the hourly and daily send budget and is replayed as `already_sent` with a draft id. (d) If `Complete` fails after a successful send the error is swallowed, so the entry stays pending and is not counted.

**Acceptance criteria:**
- A ledger operation `ReserveWithin(key, fp, cap windows)` performs the count and the reservation under one lock; a concurrency test with N goroutines/processes against a cap of 1 results in exactly one send.
- Pending entries younger than the window count toward the cap (documented as conservative); a test shows an ambiguous send reduces remaining quota.
- Drafts are recorded with a distinct kind and are excluded from `SentSince`; replaying a draft key reports `already_drafted` (or equivalent) with the draft id, not `already_sent`.
- A `Complete` failure after a real send is surfaced as a warning field in the envelope and in the audit entry.

### FR-R9 (P2) Draft send and draft delete integrity gaps

**Where:** `internal/usecase/send.go` `ownDraft`, `sendDraft`.

**Finding:** `ownDraft` skips the author check when `From` is empty (`from != ""`), so an unknown author passes. Between the policy read of the draft and `POST .../send` the draft can change (check-then-act); a human edit is fine, but any other writer in the mailbox is not covered. The sent subject/body come from the re-read draft without the policy prefix and footer if the draft was not created by this tool.

**Acceptance criteria:**
- A draft with empty or missing `From` is refused for send and delete.
- The fingerprint of recipients, subject and body evaluated is compared with a second read immediately before send (or `If-Match`/changeKey is sent if Graph supports it); a mismatch aborts with a conflict. Document any residual window.
- Behavior for drafts not created by this tool (no prefix/footer) is decided and documented.

### FR-R10 (P2) Attacker-controlled strings are emitted outside `untrusted` wrappers

**Where:** `internal/adapter/cli/present.go`.

**Finding:** `from.address`, `to[].address`, `cc`, link `url` and `domain`, attachment `content_type`, and folder names are plain strings although all derive from the sender or tenant content (a display-name-style payload can sit in an address local part; a URL can carry prose). `ParseAuthResults` takes the first verdict per mechanism from any `Authentication-Results` header without checking the authserv-id, so a header injected by the sender is trusted if Graph returns it first.

**Acceptance criteria:**
- Addresses from incoming mail are validated with `ParseAddress` rules; a non-conforming address is wrapped as untrusted (and flagged) instead of being emitted plain.
- Link `url` and `domain` are wrapped as untrusted (or documented as safe because defanged and length-bounded, with the bound enforced and tested).
- `auth_results` uses only the header whose authserv-id matches a configured tenant value, otherwise reports `unverified`; tests with a forged header placed first and last.
- The AC-6 corpus test is extended to assert that no raw message text appears outside an `untrusted` field.

### FR-R11 (P2) Message, attachment and folder ids are not validated as path segments

**Where:** `internal/adapter/graph/client.go` `seg`, `requireID`.

**Finding:** `url.PathEscape("..")` is `..`, so `mail get ..` or `attachment list ..` builds `/me/messages/..`, which a server or proxy may normalize to `/me`. Ids come from agent arguments.

**Acceptance criteria:**
- Ids must match a conservative pattern (Graph ids are URL-safe base64 plus `-_=`) and be at most a documented length; `.` and `..` and empty segments are rejected with usage exit 2 before any request (httptest asserts no hit).
- Unit test covers `.`, `..`, `%2e%2e`, `a/b`, whitespace and control characters for message, attachment, draft and folder ids.

### FR-R12 (P2) Quarantine creates directories before the containment check

**Where:** `internal/adapter/ledger/quarantine.go` `resolve`.

**Finding:** `os.MkdirAll(outDir)` runs before the symlink-resolved containment test, so a path that is textually inside `out_dir` but traverses a symlink creates directories outside it. `Root` empty disables the containment check (only wired when download is enabled, but the type allows it). A previously saved same-named file makes later downloads of that name fail with conflict, a small denial-of-service by a sender who controls attachment names.

**Acceptance criteria:**
- Resolve the deepest existing ancestor with `EvalSymlinks`, check containment, then create; a symlink-escape test creates nothing outside root.
- `Quarantine` with an empty `Root` is rejected at construction.
- Name collision resolves to a unique suffix or content-hash directory rather than failing, with a test.

### FR-R13 (P2) Ledger and audit trust model and forensic detail

**Where:** `internal/adapter/ledger/ledger.go`, `cmd/outlook/app.go` (ledger sits next to the audit log), `usecase/service.go` `exec`.

**Finding:** The ledger is a 0600 file owned by the agent user, so the agent can delete or edit it and reset idempotency, `pending` fail-closed protection and rate counters. The audit entry records verb, resource, outcome and decision but not recipients (or their hashes), message id or `HTTPStatus` (never populated), which limits forensics. `limits.max_writes_per_run` is per process, so repeated invocations reset it.

**Acceptance criteria:**
- Docs (`user-docs/configuration.md`, `docs/technical-details.md`) state plainly that client-side caps are guardrails against mistakes and prompt injection, not against a hostile local user, and name the server-side controls (Exchange transport rules, throttling) as the hard limit.
- Audit entries for send, reply, draft send and move include recipient count, a hash of the recipient set, and the message or draft id (never subject or body); `HTTPStatus` is populated where known. Test asserts no body, subject or token text appears.
- Optionally the ledger directory permissions and ownership are verified at open (mode 0700, owned by the current user) with a test.

### FR-R14 (P2) Test and documentation gaps

**Where:** `cmd/outlook` (74.3 percent coverage), `internal/usecase/send_test.go`, docs.

**Finding:** Composition-root coverage is 74 percent and none of the defects above has a regression test; some tests assert the defective behavior (`TestProbeErrorFailsClosedAndFreesKey`). `docs/unverified-assumptions.md` and `docs/technical-details.md` describe the probe and rate caps more generously than the code.

**Acceptance criteria:**
- Each of FR-R1 to FR-R13 lands with its failing-first test; the test names reference the FR id.
- `cmd/outlook` coverage reaches at least 85 percent with tests for `assemble` failure paths (bad policy, unwritable audit path, unknown filter).
- A docs pass reconciles every statement about probe, rate caps, page tokens and policy trust with the final behavior; `docs/unverified-assumptions.md` gains the Reply-To, header-filter and tag-character assumptions.
- A short threat-model table (asset, attacker, control, residual risk) is added to `docs/technical-details.md`.

## 4. Priority Summary

| Priority | Count | FRs |
|---|---|---|
| P0 | 2 | FR-R1, FR-R2 |
| P1 | 6 | FR-R3, FR-R4, FR-R5, FR-R6, FR-R7, FR-R8 |
| P2 | 6 | FR-R9, FR-R10, FR-R11, FR-R12, FR-R13, FR-R14 |

## 5. Positive Observations

- Layering is enforced by `internal/archtest`; Graph DTOs stay in the adapter; core `httpx`, `auth`, `audit`, `output` are used rather than reimplemented.
- Header and CRLF injection are blocked in subject, addresses, idempotency key and agent headers; recipients are strictly parsed and de-duplicated, bcc and reply-all default deny.
- Attachment names are sanitized to a single path element, files are created `O_EXCL|O_NOFOLLOW` with a hard byte cap and removal on overflow.
- Ledger writes are atomic (temp, fsync, rename, dir fsync) under an exclusive flock and fail closed on corruption.
- Error mapping discards Graph response bodies; filter findings carry kind and offset, never matched text; audit never stores bodies.
- Every endpoint shape is an explicit assumption in code, tests and `docs/unverified-assumptions.md`.

## 6. Out of Scope

Changes to `agent-cli-core` (record in `docs/requested-core-changes.md`), real-tenant verification (M0 checklist), calendar, native Windows.
