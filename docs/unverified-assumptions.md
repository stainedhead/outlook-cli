# Unverified assumptions

Everything below is marked as unconfirmed (a warning sign) in `specs/261003-outlook-cli/outlook-cli-PRD.md`, or follows from a Graph endpoint shape taken from general knowledge of Graph v1.0. None of it has been checked against a real tenant. Each item is an explicit assumption in the code (a comment beginning `ASSUMPTION(UA-n)`) and in a test whose name contains the same id.

How to use this table: the "Code reference" and "Test reference" columns are filled in when the implementing workstreams land (Step 4). The "Verified by" column names the M0 spike (see `m0-spike-checklist.md`) that settles the item. An item stays here until a spike report confirms or refutes it; refuted items move to `requested-core-changes.md` or a PRD change.

| ID | Assumption | PRD ref | Risk if wrong | Code reference | Test reference | Verified by |
|---|---|---|---|---|---|---|
| UA-1 | Every Graph endpoint shape in the command table (paths, `$select`, `$filter`, `$search`, `$orderby`, `$top`, paging via `@odata.nextLink`) matches current Graph v1.0. | s6, s1 legend | Reads or writes fail or return unexpected fields. | TBD (graph adapter) | TBD | S-1 |
| UA-2 | `GET /me` is enough to confirm the configured policy `mailbox` equals the signed-in user (`mail` or `userPrincipalName`). | s4 | Wrong-mailbox guard gives false accept or reject. | TBD (mailbox check) | TBD | S-1 |
| UA-3 | A shared Entra public-client app with user assignment required restricts sign-in to the agent group. | s4 diagram | Non-agent users could enroll against the app. | n/a (tenant setup) | n/a | S-2 |
| UA-4 | Delegated `Mail.ReadWrite` and `Mail.Send` without `.Shared` scopes reach only the agent user's own mailbox, and no mailbox delegation exists. `/users/{other}/messages` and send-as another user return 403. | s5, s11 | Cross-mailbox access; core isolation claim fails. | TBD (selftest negative probe) | TBD | S-3 |
| UA-5 | Conditional Access on the agent group permits non-interactive refresh-token use; refresh-token lifetime and the re-enrollment interval are acceptable. | s4, s14 | Unattended use breaks; frequent human re-enrollment. | n/a (daemon, tenant) | n/a | S-4 |
| UA-6 | A shared mailbox cannot be signed into, so it does not fit the delegated model. | s4 | Design choice for mailbox type may change. | n/a | n/a | S-1 |
| UA-7 | Internal-only inbound or quarantine of external mail on the agent mailbox is available and effective. | s5 | Prompt-injection surface is larger than assumed. | n/a (tenant setup); client-side untrusted handling is the fallback | TBD (injection corpus) | S-6 |
| UA-8 | SPF/DKIM/DMARC anti-spoofing applies, and authentication results are available on messages (`Authentication-Results` header via `internetMessageHeaders`). | s5, s10 | `sender_trust` signal is missing or misleading (it is advisory only). | TBD (message mapper) | TBD | S-6 |
| UA-9 | Exchange recipient-rate limits and malware filtering apply to the agent mailbox as a server-side backstop. | s5 | Only client-side caps and quarantine protect against runaway send and malicious attachments. | TBD (rate caps, quarantine) | TBD | S-6 |
| UA-10 | Not consenting `MailboxSettings.ReadWrite` and blocking auto-forwarding in the transport policy prevents rule or forwarding persistence. | s5 | Persistence path through mailbox rules. | n/a (tenant setup) | n/a | S-3 |
| UA-11 | Custom internet headers `X-Agent-Id` and `X-Agent-Run` can be set on a created message and survive send. | s6 | Agent attribution headers silently dropped. | TBD (send mapper) | TBD | S-5 |
| UA-12 | Sent Items can be searched by the `X-Agent-Id` / idempotency header to detect a prior send (P1 probe). | s6 | P1 idempotency check gives false negatives; the local ledger remains the only guard. | TBD (`SentItemsProbe`) | TBD | S-5 |
| UA-13 | The Graph error code (for example `ErrorAccessDenied`) is available on a 403 without reading the request body. The core `httpx` exposes only headers. | s7 AUTH-3 | Exit 4 message lacks the code. See core change request 1. | TBD (graph error mapper) | TBD | S-1 |
| UA-14 | A 429 or 503 carries `Retry-After`, and bounded retries on reads are safe. Writes are never auto-retried. | s7 AUTH-4 | Throttling loops or duplicate sends. | TBD (httpx wiring) | TBD | S-1 |
| UA-15 | A 401 followed by one forced daemon refresh recovers; the daemon reports `reauth_required` when the refresh token is dead. | s7 AUTH-2 | Wrong exit code or message after token expiry. | TBD (auth wiring; stubbed until the daemon client exists) | TBD | S-4 |
| UA-16 | `mail reply` to sender only (no reply-all) maps to the Graph `reply` or `createReply` call with a restricted recipient set. | s6 | Reply could reach more recipients than intended. | TBD | TBD | S-5 |
| UA-17 | Attachment download via `/me/messages/{id}/attachments/{id}/$value` or base64 `contentBytes`, with size known before download. | s6, s9 | Size cap enforced after the fact. | TBD (quarantine adapter) | TBD | S-1 |
| UA-18 | Exchange mail flow rules (recipient allowlist, DLP, disclaimer) can enforce the external-recipient block server-side, and sent mail shows in retention and eDiscovery. | s8.3, s11 | The server-side control behind `external: deny` is absent. Syntax is owned by the Exchange team. | n/a (tenant setup); selftest probe TBD | TBD | S-7 |
| UA-19 | Kill-switch propagation (disabling the Entra user, revoking sessions, disabling the Okta app) takes effect within an acceptable lag. | s5 | Longer exposure after a compromise. | n/a | n/a | S-8 |
| UA-20 | Hybrid Exchange topology does not change who creates the agent mailbox or how objects sync. | s14 | Provisioning plan is wrong. | n/a | n/a | S-1 |
| UA-21 | Release items: Apple notarization account, cosign keyless signing, `GITHUB_TOKEN` scope for private module fetch, and the skill hand-off by manual PR. | s16 | Release pipeline design changes. | n/a (deferred, see `deferred.md`) | n/a | deferred |

Maintenance rule: adding a Graph call, header or tenant behaviour that is not yet confirmed means adding a row here in the same change.
