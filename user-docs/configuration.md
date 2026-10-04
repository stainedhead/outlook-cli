# Configuration reference

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `OUTLOOK_POLICY` | `/etc/agent-cli/outlook.policy.yaml` | Path of the policy file. The file it points to must pass the same ownership check as the default |
| `AGENT_ID` | the policy `profile` | Agent identifier written to audit records, message headers and the footer |
| `AGENT_RUN_ID` | random `run-<hex>` per process | Run identifier for audit and the `X-Agent-Run` header |
| `AGENT_OKTA_D_SOCKET` | platform default: `/var/run/agentd/agentd.sock` (macOS), `/run/agentd/agentd.sock` (Linux) | Unix socket of the `agent-okta-d` credential daemon. Each token request has a 10 second timeout. The socket's ownership is not checked yet |

Exit 3 on any command that needs a token means the credential daemon cannot give `outlook` a token: it is not running or the socket path is wrong, re-enrollment is needed (a human runs `agent-okta-d enroll msgraph`), the credential was revoked, or the provider is not configured for this agent. Exit 8 means the daemon is temporarily degraded; wait for the hinted time and retry. See [Troubleshooting](troubleshooting.md#exit-3-credential-daemon-unreachable).

Only `OUTLOOK_POLICY` could be considered stable; the other names are choices of this build and unverified against a real deployment. There are no credentials in the environment or on disk: tokens come only from the daemon.

## Global flags (every command)

| Flag | Meaning |
|---|---|
| `--format json\|table\|text` | Output format; default `json` |
| `--output-max-bytes N` | Bound the output size; when output is cut, `meta.truncated` is true |
| `--offset N` | Resume a truncated output |

## The policy file

A single YAML document. Installation rules:

- Install it where only an administrator can change it. The file and every directory above it must be owned by root, and none may be writable by group or others. Ownership is what is checked, not whether you can write it: a file owned by the agent user is refused even if it is read-only. Running `outlook` as root is also refused. Symlinks are allowed only if their owner and every directory they pass through are trusted. A failed check is exit 6 and every command stops.
- Typical installation:

  ```
  sudo install -d -o root -m 0755 /etc/agent-cli
  sudo install -o root -m 0644 outlook.policy.yaml /etc/agent-cli/outlook.policy.yaml
  ```
- Unknown keys, duplicate keys, an empty file, more than one YAML document, or a file over 1 MiB are rejected (exit 9). A missing file is also exit 9. A policy that cannot be loaded blocks every command; there is no fallback policy.
- The policy is read once per run.
- Omitted settings fall to the safest value, except the numeric bounds noted below.

### Fields

| Key | Required | Values and default | Notes |
|---|---|---|---|
| `profile` | yes | text | Used as the agent id when `AGENT_ID` is unset |
| `mailbox` | yes | `name@domain` | The only mailbox the tool will address. Compared with the signed-in user (`GET /me`, unverified) once per run; a mismatch is refused |
| `internal_domains` | yes | list of bare domains | Defines "internal" for `sender_trust` and for `external` handling |
| `read.folders` | no | list of folder names or aliases (`inbox`, `Processed`); default none | Only these folders can be read, listed, searched or used as `mail move` targets. Matching is case-insensitive |
| `read.max_body_bytes` | no | integer; default 16000 | Upper bound on message body text returned |
| `read.html_to_text` | no | boolean; default true | |
| `read.defang_links` | no | boolean; default true | |
| `read.auth_results_authserv_ids` | no | list of single tokens, for example `[mx.example.com]`; default empty | Authentication-Results headers are believed only when their authserv-id (the text before the first `;`, case-insensitive) is in this list. With the list empty, `auth_results` reports `spf`, `dkim` and `dmarc` as `unverified`. It is a policy key rather than an environment variable because the environment is controllable by the agent |
| `read.attachments` | no | `{download: deny}` (default), or `{allow_types, max_bytes, out_dir}` | All three of the second form are required together. `allow_types` are bare extensions (`pdf`). `out_dir` is an absolute path; `attachment get --out` must resolve inside it |
| `send.mode` | no | `allow`, `dry_run_only`, `deny`; default `deny` | `dry_run_only` renders but never sends |
| `send.recipients.allow_domains` | no | list of bare domains | Recipients in these domains are allowed |
| `send.recipients.allow_addresses` | no | list of addresses | Individually allowed addresses |
| `send.recipients.external` | no | `deny` (default), `draft_only`, `allow` | What happens to a recipient outside the allow lists: refuse, save a draft for a human, or send |
| `send.recipients.max_total` | when mode is `allow` or `dry_run_only` | integer, at least 1 | to + cc + bcc after de-duplication |
| `send.recipients.bcc` | no | `allow` or `deny` (default) | |
| `send.reply_all` | no | `allow` or `deny` (default) | |
| `send.attachments` | no | `allow` or `deny` (default) | Attachments on outgoing mail |
| `send.subject_prefix` | no | text | Prepended to the subject |
| `send.footer` | no | text; `{agent_id}` is replaced | Appended after a `-- ` separator |
| `send.body.max_bytes` | no | integer; default 20000 | |
| `send.content_filters` | no | `secret_patterns`, `classification_markers`; each at most once | Scan outgoing bodies; a hit refuses the send. Findings never include the matched text |
| `send.rate.per_hour`, `send.rate.per_day` | no | integer; 0 or omitted means no limit | Counted from the idempotency ledger across runs on this host |
| `limits.max_results` | no | integer; default 100 | Cap on list size |
| `limits.max_writes_per_run` | no | integer; default 20; explicit `0` denies all writes | |
| `audit.path` | yes | absolute path | JSON-lines audit log. The directory is created (mode 0700) if missing, must be writable by the agent user, and must be owned by the agent user with mode 0700 because the idempotency ledger and the page-token key live there too. A shared directory such as a 0755 log directory is refused (exit 1); give the ledger a private directory |

### Sample policy

```yaml
# Install as /etc/agent-cli/outlook.policy.yaml, root-owned, read-only for the agent user.
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

`corp.example.com` is a placeholder; use your own domain and mailbox.

### Audit log

One JSON line per command, for example:

```
{"schema_version":1,"ts":"2026-10-03T23:47:18.832279Z","tool":"outlook","agent_id":"agent","run_id":"run-eeb078445bf45ee7","verb":"read","resource":"mail.list","outcome":"error","duration":"9µs","policy_decision":"allow"}
```

`policy_decision` is `allow` or `deny:<rule-id>`. On sends it also carries `;recipient_count=N;recipient_hash=H;message_id=ID;warnings=a,b` (the hash is 32 hex characters of the recipient set; no addresses, subjects or bodies are recorded). `http_status` is filled in for failed Graph calls. If the audit log cannot be written, the command fails (it never runs unrecorded).

### Safety notes

- Keep the audit directory private to the agent user (0700) and the policy directory not writable by it.
- The page-token key `outlook.pagekey` (mode 0600) is created next to the idempotency ledger on first use. Deleting it invalidates outstanding page tokens; callers simply restart the listing.
- Rate caps and the ledger are guardrails against a runaway agent, not protection against a hostile local user who can edit those files.
- `--body-file` can read any file the agent user can read (up to 4 MiB). There is no directory restriction yet; do not rely on it to keep files private.
- Remove or edit the idempotency ledger only as an administrator; a `pending` entry means an earlier send's outcome is unknown and later sends with that key fail with exit 7.
- Attachment download stays off until the policy enables it. Downloaded files are only written into the quarantine directory and are never opened by the tool.
