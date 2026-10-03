# outlook CLI — Product Requirements Document

| | |
|---|---|
| **Status** | Draft v0.2 (delegated user-account model) |
| **Date** | 2026-10-03 |
| **Owner** | Enterprise Architecture (owner TBD) |
| **Companion docs** | `agent-okta-d-PRD.md` (§7.5 `msgraph` provider), `agent-cli-core-PRD.md` in [agent-cli-core](https://github.com/stainedhead/agent-cli-core) (shared CLI core: envelope, exit codes, bounds, policy, audit; originated in `snow-cli-PRD.md` §5), `snow-cli-PRD.md`, `teams-cli-PRD.md` |
| **Binary** | `outlook` |

**Evidence legend.** ✅ = confirmed against vendor documentation during research (2026-10-03). ⚠️ = not confirmed in vendor docs this session (engineering judgment, secondary source, or general Graph knowledge). Graph endpoint shapes in §6 are from general knowledge of Graph v1.0 and need verification against the current reference before build.

---

## 1. Summary

`outlook` lets an autonomous agent read, triage and send corporate email **as its own Entra user, from its own mailbox**. It is a thin Go CLI over Microsoft Graph, built on the shared CLI core ([agent-cli-core](https://github.com/stainedhead/agent-cli-core), its own repository). Identity is the agent's own user account: the `agent-okta-d` daemon holds a delegated refresh token (enrolled once by a human) and serves short-lived Graph access tokens (provider `msgraph`). Delegated `Mail.*` scopes reach only the signed-in user's mailbox; a client-side policy layer, Exchange mail-flow rules and Conditional Access add recipient controls, rate limits and safe handling of untrusted content.

This replaces v0.1's app-only design (Okta→Entra federated credential plus Exchange RBAC for Applications). The delegated model needs one shared app registration instead of one per agent, shares its token with the `teams` CLI, and was chosen because Teams cannot be reached app-only.

Email is the highest-risk channel an agent can have: inbound mail is untrusted text that may carry instructions, and outbound mail is a data-exfiltration path. The design treats both directions as hostile by default.

## 2. Goals and non-goals

**Goals**

- G1. The agent reads and sends mail as its own mailbox, with no credential readable by the agent's OS user.
- G2. A compromised or manipulated agent cannot read or send as anyone else: the token is the agent user's own delegated token, the agent user holds no delegations, and no `.Shared` scopes are consented.
- G3. Recipients, volume and content leaving the mailbox are controlled both server-side (mail flow) and client-side (policy).
- G4. Inbound content is delivered to the model clearly marked as untrusted.
- G5. Every send is attributable (agent id header/footer, audit log) and idempotent on retry.

**Non-goals**

- Reading **people's** mailboxes on their behalf ("assistant" scenarios). That needs per-person consent; out of scope.
- Human mode. People use Outlook itself.
- Forwarding, rules, delegation, mailbox settings, contacts, permanent deletion.
- Calendar writes (calendar read is P2).

## 3. Why build: options evaluated

| Option | Verdict | Reasoning |
|---|---|---|
| **CLI for Microsoft 365 (`m365`, PnP community)** | Not as the agent surface | Covers Outlook and Teams workloads ✅. Supported auth: certificate, client secret, managed identity, device code, password, browser, federated identity ✅, but federated identity is currently only for GitHub Actions ✅. Certificate login **persists the private key and thumbprint in the CLI's own token cache** ✅, which breaks the no-key-on-agent rule. No documented way to inject an externally minted token ⚠️. Very large admin surface (SharePoint, Entra, Power Platform). Community-supported, not covered by Microsoft support ✅. Good for **human admin and diagnostics**. |
| **Microsoft Graph CLI (`mgc`)** | No | Retired; full retirement was scheduled for 2026-08-28 ✅. |
| **Microsoft Graph PowerShell SDK** | Admin/setup only | Microsoft's recommended replacement for `mgc` ✅. PowerShell runtime, broad surface, no policy hooks. Useful (with Exchange Online PowerShell) for the §8 setup checks. A token-accepting connect option exists ⚠️ but does not change the verdict. |
| **Azure CLI `az rest`** | No | Generic passthrough with its own credential cache; no guardrails. |
| **Microsoft Work IQ / Agent 365 MCP servers** | Revisit later | Microsoft documents MCP servers for Microsoft 365 data and actions as part of Agent 365 ✅; licensing/preview dependencies and the stack's "CLI over MCP" preference apply. |
| **Delegated Graph with the agent user's token** | **Chosen** | Same Graph surface; token comes from the daemon, not from the CLI. |
| **Build `outlook`** | **Yes** | Small surface, shared core gives policy, untrusted-content handling, audit and the daemon token path. Estimated at a few thousand lines of Go. |

## 4. Architecture and identity

```
 outlook (Go, agent-cli-core: github.com/stainedhead/agent-cli-core, spec in agent-cli-core-PRD.md)
   ├─ auth: daemon client → provider "msgraph" (delegated Graph access token, agent user)
   ├─ policy: recipients, rate, content filters (client-side guardrails)
   ├─ graph: /me/… calls
   └─ output: untrusted-content envelope, bounds, audit
            │ Bearer <Graph token>
            ▼
 Microsoft Graph ──► Exchange Online (agent user's mailbox)
   Shared Entra public-client app, user assignment required (agent group only)  ⚠️
   Delegated scopes: Mail.ReadWrite, Mail.Send (no .Shared variants)             ✅ scope names
   Conditional Access for the agent group (non-interactive refresh-token use)    ⚠️
   Mail flow rules on the agent mailbox: recipient/DLP/disclaimer controls       ✅
```

- **Delegated means `/me`.** The CLI never takes a mailbox parameter and calls only `/me/...`. The policy `mailbox` value is checked against `GET /me` at start-up.
- **One shared app registration**, one refresh token per agent held by the daemon (`agent-okta-d-PRD.md` §7.5). Enrollment is a one-time device-code sign-in as the agent user.
- **Mailbox:** a licensed user mailbox per agent (the user must also be Teams-licensed). A shared mailbox cannot be signed into, so it does not fit the delegated model ⚠️.

## 5. Threat model and controls

| Threat | Server-side control | Client-side control (`outlook` policy) |
|---|---|---|
| **Agent reads/sends as another user** | Delegated token reaches only the agent user's mailbox; **no `.Shared` scopes consented; no mailbox delegations granted to the agent user** ⚠️ (verify with `Get-MailboxPermission` and the §11 negative test) | CLI addresses only `/me` |
| **Data exfiltration by email** | Mail flow rules: block or hold messages from the agent mailbox to external domains except an allowlist; DLP policy; attachment rules; mailbox scoping does not restrict recipients ✅, so this layer is mandatory | Recipient allowlist by domain/address, max recipients, BCC denied, reply-all denied, no forward, no attachments on send by default, secret-pattern filter on body |
| **Prompt injection via inbound mail** | Agent mailbox accepts internal senders only, or quarantines external mail unless allowlisted ⚠️ | Body converted to text, links listed separately and defanged, content wrapped as untrusted, attachment download off by default |
| **Spoofed/compromised internal sender** | Standard Exchange anti-spoofing (SPF/DKIM/DMARC) ⚠️ | `sender_trust` is a heuristic (domain match), surfaced but never treated as authorization; commands from mail are never executed without a human-visible action policy |
| **Runaway sending** | Exchange recipient-rate limits on the mailbox ⚠️ | Per-hour/day caps, idempotency, max recipients |
| **Malicious attachments** | Exchange malware filtering ⚠️ | Downloads off by default; if enabled: type allowlist, size cap, saved to a quarantine directory, never opened |
| **Persistence via mailbox rules/forwarding** | Do not consent `MailboxSettings.ReadWrite`; block auto-forwarding in the transport policy ⚠️ | No rules, forwarding or settings commands exist |
| **Stolen refresh token** | Conditional Access (named location, sign-in frequency, CAE) ⚠️; token stored behind the Okta-federated secret store; revoke sessions on suspicion | Token never reaches the agent process |
| **Kill switch lag** | Disable the Entra user and revoke sign-in sessions; disable the Okta app so the daemon cannot read the stored refresh token (propagation lag ⚠️) | Short Graph token reuse; policy `mode: deny` can be pushed by config management |

## 6. Command surface

All output uses the shared envelope, exit codes and bounds specified in `agent-cli-core-PRD.md` in https://github.com/stainedhead/agent-cli-core (origin: `snow-cli-PRD.md` §5).

| Command | Purpose | Graph call (⚠️ verify) | Policy-gated |
|---|---|---|---|
| `outlook whoami` | Mailbox, agent id, policy profile, effective limits | local + `GET /me?$select=mail,userPrincipalName` | |
| `outlook folder list` | Folders and unread counts | `GET /me/mailFolders` | |
| `outlook mail list [--folder inbox] [--unread] [--from X] [--since DATE] [--limit N] [--page-token T]` | List message summaries (no bodies) | `GET /me/mailFolders/{f}/messages?$select=…&$orderby=receivedDateTime desc` | read |
| `outlook mail get <id> [--body text\|none] [--max-bytes N]` | One message; body as text via `Prefer: outlook.body-content-type="text"` | `GET /me/messages/{id}` | read |
| `outlook mail search "<query>" [--folder F] [--limit N]` | Search | `GET …/messages?$search="…"` | read |
| `outlook mail send --to … [--cc …] --subject S (--body T \| --body-file F) [--dry-run] [--idempotency-key K]` | Send a new message | `POST /me/sendMail` | send |
| `outlook mail reply <id> --body T` | Reply to sender (`--all` denied by default) | `POST …/messages/{id}/reply` | send |
| `outlook mail draft create\|list\|send\|delete` | Draft workflow (deletes only the agent's own drafts) | `POST …/messages`, `POST …/messages/{id}/send` | send |
| `outlook mail mark <id> --read\|--unread` | Triage | `PATCH …/messages/{id}` | write |
| `outlook mail move <id> --folder NAME` | Move to an allowed folder (e.g. `Processed`); not to Deleted Items | `POST …/messages/{id}/move` | write |
| `outlook attachment list <mail-id>` | Attachment metadata | `GET …/messages/{id}/attachments` | read |
| `outlook attachment get <mail-id> <att-id> --out DIR` | Download to quarantine dir | `GET …/attachments/{att}` | **off by default** |
| `outlook calendar list --from D --to D` (P2) | Read agent's calendar | `GET /me/calendarView` | read |
| `outlook selftest` | Allow/deny matrix (§11) | various | |

**Deliberately absent:** forward, permanent delete, inbox rules, delegates, mailbox settings, contacts, send-on-behalf, any mailbox parameter.

**Send behavior**

- `From` is always the agent mailbox. Subject is prefixed per policy (`[agent] …`), and a footer line states the agent id and that the message is automated.
- A custom internet header `X-Agent-Id: <id>` and `X-Agent-Run: <run_id>` is added ⚠️ (Graph supports custom `x-` headers on created messages).
- **Idempotency:** `--idempotency-key` is checked against a local ledger first (P0) and, in P1, against Sent Items headers ⚠️, so a retried command does not email twice.
- `--dry-run` validates policy and renders the final message without sending.
- External recipients: policy `external: deny | draft_only | allow`. `draft_only` creates a draft for a human to review and send.

## 7. Authentication requirements

| ID | Requirement | Pri |
|---|---|---|
| AUTH-1 | Obtain the Graph token from the daemon (`msgraph`); no fallback credentials; no token output command; if the daemon is unreachable, exit 3 with a clear message that says so (`agent-cli-core-PRD.md`, CORE-AUTH-3) | P0 |
| AUTH-2 | On 401, force one daemon refresh and retry once; if the daemon reports `reauth_required`, exit 3 with a message that a human must run `agent-okta-d enroll msgraph` | P0 |
| AUTH-3 | On 403 from Graph/Exchange, exit 4 and report the Graph error code without the request body | P0 |
| AUTH-4 | Handle 429/503 with `Retry-After`; exit 8 after bounded retries | P0 |

Identity setup (shared app, scopes, Conditional Access, enrollment) is in `agent-okta-d-PRD.md` §7.5.

## 8. Permissions and configuration

### 8.1 Entra

- One shared app registration `agent-graph-cli`: public client flows on, **assignment required**, assigned to the agent group only. No client secrets or certificates.
- Delegated permissions for this CLI: `Mail.ReadWrite`, `Mail.Send`, `User.Read`, `offline_access`. Do **not** consent `Mail.ReadWrite.Shared`, `Mail.Send.Shared`, `MailboxSettings.ReadWrite` or any `*.All` mail scope.
- Conditional Access for the agent group: agreed policy for non-interactive refresh-token use (see daemon PRD §7.5). The tested token lifetime sets how often humans must re-enroll.

### 8.2 Exchange Online (the agent user's mailbox)

- Licensed mailbox; no full-access or send-as delegations to or from the agent user except where explicitly approved.
- If you also want a server-side backstop against over-broad scopes, keep an inventory check that fails when the shared app is granted any `.All` or `.Shared` mail permission.

### 8.3 Exchange mail flow (recipient and content controls)

Delegated scopes control *which mailbox*, not *which recipients* ✅ (same as app-only). Add, on the agent mailbox ⚠️ (exact rule syntax to be written by your Exchange team):

1. Reject or hold outbound mail to external domains unless the domain is on an allowlist.
2. Apply DLP policy and block attachments from the agent mailbox unless explicitly allowed.
3. Append an "automated message" disclaimer.
4. Inbound: accept internal senders only, or quarantine external mail pending review.
5. Retention/journaling per policy: agent email is a business record.

## 9. Client-side policy

```yaml
# /etc/agent-cli/outlook.policy.yaml  (root-owned; agent user read-only)
profile: agent
mailbox: agent-sdlc-reviewer-01@corp.example.com     # the only mailbox the CLI will address
internal_domains: [corp.example.com]
read:
  folders: [inbox, Processed]
  max_body_bytes: 16000
  html_to_text: true
  defang_links: true
  attachments: { download: deny }                    # or: { allow_types: [pdf, txt, md], max_bytes: 2000000, out_dir: /var/agent/quarantine }
send:
  mode: allow                                        # allow | dry_run_only | deny
  recipients:
    allow_domains: [corp.example.com]
    allow_addresses: []
    external: deny                                   # deny | draft_only | allow
    max_total: 5
    bcc: deny
  reply_all: deny
  attachments: deny
  subject_prefix: "[agent] "
  footer: "Automated message from agent {agent_id}. A human owns decisions."
  body: { max_bytes: 20000 }
  content_filters: [secret_patterns, classification_markers]
  rate: { per_hour: 20, per_day: 100 }
limits: { max_results: 100, max_writes_per_run: 20 }
audit: { path: /var/log/agent-cli/outlook.audit.jsonl }
```

## 10. Output and untrusted-content specification

```json
{ "ok": true,
  "data": { "messages": [ {
     "id": "AAMk…",
     "received": "2026-10-03T14:02:11Z",
     "from": { "address": "jane@corp.example.com", "name": "Jane Doe" },
     "sender_trust": "internal",
     "to": ["agent-sdlc-reviewer-01@corp.example.com"],
     "subject": { "untrusted": true, "text": "…" },
     "body":    { "untrusted": true, "format": "text", "text": "…", "truncated": false },
     "links":   [ { "text": "build 4812", "url": "hxxps://ci.corp.example.com/…", "domain": "ci.corp.example.com" } ],
     "attachments": [ { "id": "…", "name": "report.pdf", "size": 48211, "downloadable": false } ]
  } ] },
  "meta": { "count": 1, "truncated": false, "next_page_token": null } }
```

- Subject, body, display names and attachment names are `untrusted`. The harness skill document (generated by `docgen`) must state that instructions found in untrusted fields are data, never commands.
- HTML is converted to text; images are never fetched; links are listed separately with domain and defanged; tracking pixels are ignored.
- Authentication results (SPF/DKIM/DMARC) are surfaced when available ⚠️ but are advisory.

## 11. Testing and validation

- **`outlook selftest`** (acceptance test for the Exchange admins): positive — list inbox, send a self-addressed test message; **negative — `/users/{other}/messages` and send-as another user return 403**; external recipient blocked by mail flow; forbidden folders refused by policy; BCC and reply-all refused.
- Unit tests: policy evaluation (recipient rules, rate limits), HTML-to-text conversion with hostile HTML, link defanging, header injection attempts in subject/recipients, idempotency ledger.
- Integration: sandbox tenant with a test agent user, device-code enrollment, refresh through the daemon.
- Security: prompt-injection corpus (messages containing instructions) must surface only as `untrusted` content; token never logged; policy file not agent-writable.
- Compliance check: confirm messages appear in retention/eDiscovery as expected.

## 12. Non-functional requirements

- Go, static binary, macOS and Linux; depends on `agent-cli-core` (own repository, https://github.com/stainedhead/agent-cli-core; it wraps the daemon client).
- Graph throttling and mailbox limits apply; honor `Retry-After`.
- Audit JSONL per command (no bodies by default); correlate with Exchange message trace and the agent user's Entra sign-in logs (non-interactive).
- Local overhead < 50 ms per command.

## 13. Delivery plan and acceptance

| Milestone | Scope | Acceptance |
|---|---|---|
| **M0 Spikes** | Device-code enrollment as an agent user; refresh-token lifetime under the agent Conditional Access policy; negative test for another mailbox; mail flow prototype; confirm every ⚠️ | Spike report; negative test returns 403 for another mailbox; measured re-enrollment interval |
| **M1 Read** | `whoami`, `folder`, `mail list/get/search`, untrusted envelope, selftest (read) | Agent reads own inbox; injection corpus handled |
| **M2 Send** | `mail send/reply/draft`, policy, idempotency, headers/footer, rate limits | Internal send works; external send blocked server-side and client-side |
| **M3 Triage** | `mark`, `move`, `Processed` folder workflow | Inbox triage without delete rights |
| **M4 Attachments (opt-in)** | Quarantine download with type/size limits | Only allowed types download; never executed |
| **M5 Hardening** | Signed policy, redaction hook, release signing, harness skill doc | Security review sign-off |
| **P2** | Calendar read; Agent User transport (`teams-cli-PRD.md` §10) | Go/no-go memo |

## 14. Concerns and recommendations

1. **The agent is now a user with a long-lived refresh token.** Whoever steals the token is the agent. *Recommendation:* keep it in the secret store behind the Okta-federated role, restrict by Conditional Access named location, alert on sign-ins from unexpected IPs, and rehearse session revocation.
2. **Delegated scopes do not control recipients** ✅. *Recommendation:* treat mail flow rules as part of the product, owned by the Exchange team, and test them in `selftest`.
3. **Email is the best prompt-injection and exfiltration path you will give an agent.** *Recommendation:* internal-only mailbox in v1; `external: deny`; revisit with a human-in-the-loop draft workflow before enabling external.
4. **Conditional Access can break unattended use.** MFA, compliant-device and sign-in-frequency policies applied to the agent group will invalidate refresh tokens or block non-interactive refresh. *Recommendation:* agree a dedicated policy early and test it.
5. **Mailbox and AD topology.** Hybrid Exchange changes who creates mailboxes and how objects sync. *Recommendation:* resolve in M0.
6. **`m365` CLI for humans.** Reasonable admin tool for people under their own identity. *Recommendation:* allow for human admins, deny on agent hosts.
7. **Agent User alternative.** Microsoft's Agent 365 model offers a real user with a federated-credential token chain that removes the human enrollment step, but depends on Graph beta and licensing today ✅ (`agent-okta-d-PRD.md` §7.7). *Recommendation:* the daemon exposes the same `msgraph` provider either way; the CLI does not change.

## 15. Open questions

1. Is an agent-owned licensed mailbox per agent acceptable (cost, provisioning)?
2. Which external domains, if any, may agents email?
3. Is an internal-only inbound rule acceptable, or must agents receive external mail?
4. Who owns the mail flow rules and DLP policy for agent mailboxes?
5. Retention and legal-hold requirements for agent mail.
6. Which Conditional Access policy applies to the agent group, and who signs in as the agent user to enroll?

## 16. CI/CD and release requirements

Applies to this repository only; the four Go repositories in the set (`agent-okta-d`, `snow-cli`, `outlook-cli`, `teams-cli`) use the same pipeline shape so a pipeline change is made once and copied. Pipelines are GitHub Actions workflows under `.github/workflows/`. The scaffolded `ci.yml` is a starting point and must be brought in line with this section. Items marked ⚠️ are not confirmed against vendor documentation and need a spike before the pipeline depends on them.

**Terminology.** *CI* verifies a change. *CD* produces and publishes a **release**: a semver-versioned set of signed artifacts. **Publishing a release is the whole of "deploy" in this section.** Rolling a release out to agent hosts, harness images or AWS accounts is the swarm owner's job (see REL-12).

### 16.1 Continuous integration

| ID | Requirement |
|---|---|
| BLD-1 | CI runs on **every pull request targeting `main`** and **on demand** (`workflow_dispatch`, optionally against a chosen ref). CI also runs as the first stage of every release (REL-9), so nothing is released untested. |
| BLD-2 | Checks: `gofmt -l .` is empty; `go mod tidy` leaves no diff; `go vet ./...`; `golangci-lint` at a pinned version; `go test -race ./...`; `govulncheck ./...`. |
| BLD-3 | Every release target (REL-1) is **cross-compiled on each PR**, so a portability break is found before merge, not at release time. |
| BLD-4 | PR CI needs **no credentials and no network access to real systems**: tests use fakes, mock endpoints and fake clocks. `outlook selftest` (§11) needs a sandbox Entra tenant and a test agent mailbox, so it runs **only on demand**, never in PR CI. CI must never send mail to real addresses. |
| BLD-5 | The CI workflow is a **required status check** on `main` once branch protection is enabled. Branch protection is not configured yet; enabling it is a separate step. |
| BLD-6 | Workflows use least privilege (`permissions: contents: read` for CI), pin the Go version from `go.mod`, and pin third-party actions to a version or commit SHA. |

### 16.2 Release targets and artifacts

| ID | Target | Build | Artifact |
|---|---|---|---|
| REL-1a | **macOS, Apple silicon** | `darwin/arm64` | `.tar.gz` containing the `outlook` binary, signed and notarized with an Apple Developer ID ⚠️ (see 16.8 item 1). |
| REL-1b | **Windows via WSL2** | `linux/amd64` (and `linux/arm64` for WSL on Arm, see 16.8) | `.tar.gz`; WSL runs Linux binaries, so **this is the Linux build** and no native Windows `.exe` is produced. Native Windows is not a target. |
| REL-1c | **Linux, AWS-hosted container** | `linux/amd64` and `linux/arm64` (Graviton) | Multi-arch **OCI image** `ghcr.io/stainedhead/outlook-cli:vX.Y.Z`, non-root, minimal base, plus the same Linux binaries as `.tar.gz` |

Common to all targets:

- REL-2. Each release also publishes `SHA256SUMS`, an SBOM (SPDX or CycloneDX), a build-provenance attestation, and a signature for every artifact. Linux and container artifacts are signed with `cosign` keyless signing from the workflow's GitHub OIDC identity ⚠️. The install documentation in `user-docs/` states how to verify them.
- REL-3. Builds are reproducible as far as Go allows: pinned toolchain, `-trimpath`, `CGO_ENABLED=0` where possible, and a build timestamp taken from the commit.
- REL-4. The binary reports its version (`outlook version`: semver, commit, build date), stamped with `-ldflags`. The version is also surfaced in the generated harness skill document.
- REL-4a. `outlook` is deployed into the agent's host or container, so the **tarball is the primary artifact** for baking into a harness image. The OCI image is also published for use as a build stage (`COPY --from`).

### 16.3 Versioning

| ID | Requirement |
|---|---|
| REL-5 | Releases follow **semantic versioning** (`MAJOR.MINOR.PATCH`). The git tag `vX.Y.Z` on `main` is the release identity. Tags are immutable: a version is never re-tagged or re-published. |
| REL-6 | Releases start at `0.1.0` and stay `0.y.z` while this PRD is a draft. `1.0.0` is cut by an explicit decision, never automatically. |
| REL-7 | The bump is taken from a **PR label** (`release:major`, `release:minor`, `release:patch`). An unlabeled PR that changes shipped code defaults to `patch`. A PR that touches only `docs/`, `user-docs/`, `specs/`, `*.md` or `INTENT.md` does **not** cause a release. This tool builds on the shared `agent-cli-core`, which lives in its own repository (https://github.com/stainedhead/agent-cli-core) and is specified in its `agent-cli-core-PRD.md` (origin: `snow-cli-PRD.md` §5). `outlook` pins a released semver tag of it (see 16.7). |

### 16.4 Continuous delivery

| ID | Requirement |
|---|---|
| REL-8 | CD runs **on merge of a pull request to `main`** and **on demand** (`workflow_dispatch` with a `bump` of `major`, `minor` or `patch`, an optional explicit `version`, and a `dry_run` option that builds and verifies but publishes nothing). |
| REL-9 | Stages, in order: CI gate (all of 16.1), compute version, cross-build every target, package, checksum, SBOM, sign and attest, **smoke-verify**, publish. Publishing creates the tag, a GitHub Release with notes generated from merged PR titles, and pushes the container image tagged `vX.Y.Z` and `vX.Y`. No `latest` tag is relied on; consumers pin a version. |
| REL-10 | Smoke-verify runs the built artifact before anything is published: the `linux/amd64` binary and the container image on a Linux runner, the `darwin/arm64` binary on an Apple-silicon runner. Each must run `outlook version` and report the expected version. |
| REL-11 | **All-or-nothing:** if any target fails to build, sign or verify, nothing is published. A failed run is safe to re-run, and a version is never published twice. |
| REL-12 | CD **does not roll out** a release. It does not deploy to AWS accounts, restart daemons, or rebuild harness images. The harness images in `agentic-team-w-paperclip` are intended to consume a released artifact by pinned version ⚠️ (to be agreed with that repository), rather than build this tool from source. |
| REL-13 | The release job gets only what it needs (`contents: write`, `packages: write`, `id-token: write`, attestations) from a protected `release` environment. Apple signing material lives only in that environment's secrets. On-demand runs require write access to the repository, and a `major` bump on demand should require a reviewer approval on the environment. No long-lived cloud credentials are stored in the repository. |
| REL-14 | A bad release is not deleted. It is superseded by a newer patch release and marked as withdrawn in its release notes; its tags and images stay in place. |

### 16.5 Repository-specific requirements

- **Release contents:** the `outlook` binary, a sample client-side policy file with placeholder recipients and domains only, and the generated harness skill document.
- **No mail credentials in CI:** no refresh token, Graph token or mailbox credential exists in the pipeline. Enrollment (`agent-okta-d enroll msgraph`) is a human step done outside CI.
- **Server-side controls are not released from here:** Exchange mail-flow rules, Conditional Access and DLP are configured by the tenant admins (§8), not by this pipeline.

### 16.6 Milestone placement

BLD-1 to BLD-6 are in place before the first milestone that merges Go code. The release pipeline (REL-1 to REL-14) is in place before the first tagged build, and no later than the first milestone that produces a runnable binary. Release signing and notarization may land later, in the hardening milestone, but unsigned builds are labelled pre-release until then.

### 16.7 Dependency on agent-cli-core

`agent-cli-core` (Go module `github.com/stainedhead/agent-cli-core`) is a library with no binary and is a build dependency of this repository. The dependency chain is `agent-okta-d` (`pkg/client`) <- `agent-cli-core` <- `snow-cli`, `outlook-cli`, `teams-cli`. The shared-core specification is owned by `agent-cli-core-PRD.md` in that repository.

| ID | Requirement |
|---|---|
| DEP-1 | `go.mod` declares `github.com/stainedhead/agent-cli-core` at a **released semver tag**. No pseudo-versions and no `replace` directives on `main`. |
| DEP-2 | Every workflow job that builds or tests resolves dependencies using the job's dynamic `GITHUB_TOKEN` (no PAT, no stored secret), with `permissions: contents: read` and `packages: read`. |
| DEP-3 | Before `go mod download`, the job sets `GOPRIVATE=github.com/stainedhead/*` and configures git `url."https://x-access-token:${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/"` from the job token. The token is never echoed and never written to caches or artifacts. |
| DEP-4 | The repositories are public today, so the token is not strictly needed. The step is standard so that behavior is identical if visibility changes. |
| DEP-5 | ⚠️ `GITHUB_TOKEN` is scoped to the repository running the workflow, so it cannot read a different private repository's contents (unconfirmed). If `agent-cli-core` or `agent-okta-d` ever become private, they must be published through GitHub Packages with consumer repositories granted read on the package, decided before any visibility change. GitHub Packages has no Go module registry ⚠️ (unconfirmed). |
| DEP-6 | Bumping the `agent-cli-core` version is an ordinary PR and must pass CI. |

**Milestone note.** `outlook` cannot compile against `agent-cli-core` until the core has a tagged release, which itself needs `agent-okta-d` to tag a release containing `pkg/client`. No code or releases exist yet, so `go.mod` has no `require` for the core.

### 16.8 Open items (CI/CD)

1. **Apple signing.** Is an Apple Developer ID and notarization account available for CD? Until it is, darwin artifacts carry only the `cosign` signature and users must clear the quarantine attribute themselves ⚠️.
2. **Registry.** `ghcr.io` is assumed, matching `agentic-team-w-paperclip`. Should images also be pushed to Amazon ECR for the AWS-hosted container case?
3. **What "deploy" means.** This section treats it as publishing a release (REL-12). Confirm that no automatic rollout into an AWS environment is wanted.
4. **Version bump rule.** PR labels are assumed (REL-7). Conventional commits are the alternative.
5. **WSL on Arm.** Is `linux/arm64` for WSL wanted, or `linux/amd64` only?
6. **Shared pipeline.** Should the common workflow steps live in one reusable workflow? `agent-cli-core` is its own repository (decided), but whether the shared workflow steps live there, in a dedicated repository, or stay copied per repository is still open.
7. **WSL service support.** Running the daemon's service definition under WSL needs systemd in the WSL distribution ⚠️; confirm before documenting it as supported. Applies only where this tool installs a service.

## Appendix — Sources consulted

- Microsoft: [Graph delegated access](https://learn.microsoft.com/en-us/graph/auth-v2-user) · [ROPC limitations](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth-ropc) · [Exchange RBAC (superseded v0.1 design) for Applications](https://learn.microsoft.com/en-us/exchange/permissions-exo/application-rbac) · [SMTP onboarding to App RBAC (cmdlets)](https://learn.microsoft.com/en-us/exchange/client-developer/legacy-protocols/smtp-app-rbac-onboarding) · [Announcing RBAC for Applications (scope example)](https://techcommunity.microsoft.com/blog/exchange/announcing-public-preview-of-role-based-access-control-for-applications-in-excha/3688228) · [Mail.Send control with RBAC for Applications](https://office365itpros.com/2026/02/17/mail-send-rbac-for-applications/) · [Recipient limits are not controlled by app access policies (Q&A)](https://learn.microsoft.com/en-us/answers/questions/5639177/management-scope) · [Workload identity federation](https://learn.microsoft.com/en-us/entra/workload-id/workload-identity-federation) · [Create federatedIdentityCredential](https://learn.microsoft.com/graph/api/application-post-federatedidentitycredentials) · [Graph CLI retirement](https://github.com/microsoftgraph/msgraph-cli/issues/585)
- Tools: [CLI for Microsoft 365 (repo)](https://github.com/pnp/cli-microsoft365) · [m365 login docs](https://pnp.github.io/cli-microsoft365/cmd/login/) · [m365 connecting guide (certificate persistence)](https://github.com/pnp/cli-microsoft365/blob/main/docs/docs/user-guide/connecting-microsoft-365.mdx) · [Microsoft Agent 365 / Work IQ reference](https://microsoft.github.io/entrabot/platform-docs/microsoft-agent-365/)
