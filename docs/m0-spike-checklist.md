# M0 spike checklist (real tenant)

These spikes need a sandbox Microsoft 365 tenant, a licensed test agent user, and the shared Entra app. They cannot run in this repository's CI or on a developer machine without those, and they are out of scope for the build. The output of M0 is a spike report that confirms or refutes each item in `unverified-assumptions.md`.

Exit criteria from the PRD (s13): negative test returns 403 for another mailbox, and a measured re-enrollment interval is recorded.

Safety rules for every spike: use only the sandbox tenant and the test agent user; send only to addresses you own; never reuse production mailboxes or tokens; record request ids but never tokens or message bodies in the report.

| Spike | Task | Procedure | Pass criteria | Settles |
|---|---|---|---|---|
| S-1 | Graph shape confirmation | Issue each call from the PRD command table against the sandbox mailbox with a delegated token (list folders, list/search/get messages, attachments, drafts, `GET /me`, reply, move, mark). Capture status, headers and one redacted JSON sample per call. Check `Retry-After` on a forced throttle if practical. | Each call works as the adapter assumes, or the deviation is written down. Graph error body shape for 403 recorded. | UA-1, 2, 6, 13, 14, 17, 20 |
| S-2 | Device-code enrollment | A human signs in as the agent user through the device-code flow against the shared public-client app with user assignment required. Try the same flow as an unassigned user. | Assigned user enrolls with scopes `Mail.ReadWrite`, `Mail.Send`; unassigned user is refused. | UA-3 |
| S-3 | Negative isolation test | With the agent token, call `/users/{other}/messages`, `/users/{other}/sendMail`, and a send-as another user. Check `Get-MailboxPermission` shows no delegations. Confirm `MailboxSettings.ReadWrite` is not consented and auto-forwarding is blocked. | All cross-mailbox calls return 403; no delegations; no rule or forwarding path. | UA-4, 10 |
| S-4 | Refresh-token lifetime under Conditional Access | Apply the proposed agent-group CA policy (named location, sign-in frequency, CAE). Let the daemon refresh non-interactively over several days; record when refresh first fails and the `reauth_required` signal. Force a 401 and observe one forced refresh. | Non-interactive refresh works; re-enrollment interval measured and recorded. | UA-5, 15 |
| S-5 | Send-path behaviours | Send a self-addressed message with `X-Agent-Id` and `X-Agent-Run` headers; inspect the received message. Search Sent Items by header. Test reply (sender only) and confirm recipient set. | Headers present on delivery; Sent Items lookup works or is documented as unsupported; reply reaches only the sender. | UA-11, 12, 16 |
| S-6 | Inbound controls and filtering | Send an internal and an external message to the agent mailbox; check inbound rule or quarantine. Inspect `Authentication-Results` availability. Send a test attachment type and a message triggering malware filtering; probe recipient rate limits within sandbox limits. | Behaviour matches s5 assumptions or gaps are documented. | UA-7, 8, 9 |
| S-7 | Mail flow prototype | The Exchange team writes the rule that blocks external recipients, adds the disclaimer and a DLP test. Send to an external address you own. Check retention and eDiscovery show the messages. | External send blocked server-side and message traceable. | UA-18 |
| S-8 | Kill-switch rehearsal | Disable the Entra user, revoke sessions, and disable the Okta app; time how long until the daemon and Graph calls fail. | Lag measured and recorded; runbook updated. | UA-19 |
| S-9 | Selftest dry run | Run `outlook selftest` against the sandbox once the real daemon adapter exists. | Positive and negative probes behave as in PRD s11. | UA-4, UA-18 |

Report template for each spike: date, operator, tenant, steps taken, observed result, pass or fail, request ids, follow-up (code change, PRD change or core change).

S-9 needs a running `agent-okta-d` with an enrolled `msgraph` provider (the adapter itself is wired).
