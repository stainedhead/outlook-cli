# Product Details

Authoritative requirements: the PRD and `specs/archive/261003-outlook-cli/spec.md`. This page describes what the built product does. Items marked "unverified" depend on Graph or tenant behaviour that has not been checked against a real tenant (see `unverified-assumptions.md`).

## Goals and non-goals

Goals: the agent reads and sends as its own mailbox with no credential readable by its OS user; it cannot read or send as anyone else; recipients, volume and content are controlled client-side by policy (and server-side by Exchange, which this repository does not configure); inbound content is marked untrusted; every send is attributable, audited and idempotent.

Non-goals: reading other people's mailboxes, human mode, forwarding, inbox rules, delegation, mailbox settings, contacts, permanent deletion, calendar writes. There is no calendar command at all.

## Command surface

Run `outlook --help` for the list. All commands print one JSON envelope (`ok`, `data` or `error`, `meta`). Global flags on every command: `--format json|table|text`, `--output-max-bytes N`, `--offset N`.

| Command | Verb | Behaviour |
|---|---|---|
| `whoami` | read | Mailbox, agent id, run id, profile, send mode, limits. Also performs the mailbox check. |
| `folder list` | read | Folders with unread and total counts. |
| `mail list` | read | Summaries (no bodies), newest first; `--folder --unread --from --since --limit --page-token`. |
| `mail get <id>` | read | One message. Body is plain text, bounded, untrusted; links listed and defanged; attachment metadata; `auth_results` when present. `--body text|none --max-bytes N`. |
| `mail search "<q>"` | read | Free-text search in readable folders. |
| `mail send` | send | New message. `--to --cc --bcc --subject --body|--body-file (or -) --dry-run --idempotency-key`. |
| `mail reply <id>` | send | Reply to the sender only. `--all` exists and is denied by default. |
| `mail draft create|list|send|delete` | send/read/write | Drafts; `draft delete` removes only drafts. |
| `mail mark <id>` | write | `--read` or `--unread`. |
| `mail move <id> --folder NAME` | write | Move to a folder in the readable set; never Deleted Items. |
| `attachment list <mail-id>` | read | Metadata only. |
| `attachment get <mail-id> <att-id> --out DIR` | read | Off by default; quarantine download when policy enables it. |
| `selftest` | - | Allow/deny matrix of 13 rows, every write row a dry-run. |
| `version` | - | Version, commit, build date. |
| `skill` (hidden) | - | Prints the generated agent skill Markdown (used by `make skill`). |

Absent on purpose: forward, delete of non-drafts, rules, delegates, settings, contacts, send-on-behalf, calendar, any mailbox parameter.

## Send behaviour

1. Policy is loaded first; a missing, invalid or not-root-authored policy (ownership check on file and ancestors) blocks every command.
2. The `mailbox` policy value is compared with `GET /me` once per run; a mismatch is refused (unverified).
3. Recipients are lower-cased and de-duplicated. The policy decision is one of allow, dry_run_only, draft_only, deny. The deciding rule id is written to the audit log.
4. The subject prefix and footer (with `{agent_id}`) are applied, and `X-Agent-Id`, `X-Agent-Run` and `X-Agent-Idempotency-Key` headers are added (header survival unverified). Draft send and reply carry no such headers on the send call and skip the Sent Items probe.
5. With `--dry-run`, or policy `dry_run_only`, nothing is sent and the rendered message is returned. With `external: draft_only` and an external recipient, a draft is saved instead.
6. The idempotency ledger reserves the key before the POST. A repeated key returns `already_sent`; an ambiguous earlier failure returns a conflict (exit 7). Writes are never auto-retried.

## Client-side policy

One YAML file, default `/etc/agent-cli/outlook.policy.yaml` (override `OUTLOOK_POLICY`). Strict (unknown and duplicate keys rejected), fail closed, and refused unless the file and every ancestor directory are owned by root (or a trusted uid) and not group or world writable. Fields and a sample are in `user-docs/configuration.md`. The policy is a guardrail; it is not the security boundary (Entra scopes, Exchange mail flow and Conditional Access are).

## Output and exit codes

Success: `{"ok":true,"data":...,"meta":{"truncated":false,"next_offset":null,"count":N}}`. Failure: `{"ok":false,"error":{"code","message","hint"}}`. Exit codes 0-9 come from the core: 0 ok, 1 general, 2 usage, 3 auth, 4 forbidden, 5 not_found, 6 policy_denied, 7 conflict, 8 rate_limited, 9 validation. List commands return `data` as a JSON array; a Graph continuation is a final element `{"next_page_token":"..."}` (core change requests 2, 13 and 18). The token is signed and bound to the command, folder and query. `send`, `reply` and `draft send` results carry `already_drafted`, `prefix_applied` and optional `warnings`. See `technical-details.md` for the threat model.

## Delivery state

Implemented and tested with fakes. Deferred: real daemon adapter, M0 real-tenant spikes, Sent Items idempotency probe (P1), negative cross-mailbox selftest probes, release workflows, calendar. See `deferred.md`.
