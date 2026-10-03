# Troubleshooting

Every failure is a JSON envelope with `error.code`, `error.message` and often `error.hint`. See [Exit codes](exit-codes.md).

## Exit 3: credential daemon unreachable

```
$ outlook whoami
{"ok":false,"error":{"code":"auth","message":"credential daemon unreachable at socket \"/run/agent-okta-d/agent-okta-d.sock\": the agent-okta-d service may not be running","hint":"Check that the credential daemon is running and listening on \"/run/agent-okta-d/agent-okta-d.sock\". No fallback credentials are used."}}
```

This is what every token-requiring command prints in this version, whether or not a daemon is running. The real client for `agent-okta-d` is deferred: `outlook` currently contains a placeholder that always reports the daemon as unavailable. Setting `AGENT_OKTA_D_SOCKET` only changes the path named in the message. There is no workaround; do not look for other credentials. When the real adapter ships, this error means the socket is missing or the daemon is down: check the service, the socket path (`AGENT_OKTA_D_SOCKET`) and that the agent user may read the socket. If the daemon reports that re-enrollment is required, a human must run `agent-okta-d enroll msgraph`.

Commands that still work: `outlook --help`, `outlook <command> --help`, `outlook version`, `outlook skill`.

## Policy problems

| Symptom | Cause | Fix |
|---|---|---|
| Exit 9, `policy: cannot read /etc/agent-cli/outlook.policy.yaml` | No policy file at the path | Install it, or point `OUTLOOK_POLICY` at it |
| Exit 9, `policy invalid: ...` | Unknown or duplicate key, missing required key, bad value | The message names the key. Fix it; see [Configuration](configuration.md) |
| Exit 6, `policy file ... is writable by the current user` (or `directory`) | The agent user can edit its own guardrails | Make the file and directory root-owned and not writable by the agent user |
| Exit 6, `cannot check permissions` | The file or directory permissions could not be read | Fix access to the path |
| Exit 6 on a mailbox check | Policy `mailbox` differs from the signed-in user | Correct `mailbox` or the daemon enrollment |
| Exit 6 on `mail send` | Recipient not allowed, `external: deny`, too many recipients, bcc, reply-all, attachments, body too large, content filter hit, or rate cap | The message names the rule. Use `--dry-run` to see the decision without sending |
| Exit 6 on `mail list` with a folder | Folder not in `read.folders` | Add it to the policy or use an allowed folder |

## Other failures

- **Exit 1 from `outlook selftest`, every row failing with a daemon error:** expected with the placeholder client (exit 3 cause). The selftest needs a working backend.
- **Audit log cannot be opened (exit 1):** `audit.path` must have a directory the agent user can create and append to.
- **Exit 7 on send:** an earlier run with the same `--idempotency-key` did not finish and its outcome is unknown, or the key was reused with a different message. Check Sent Items; to resend deliberately use a new key. An administrator can clear a stuck pending entry in `outlook.idempotency.json`.
- **Exit 2, `unknown or missing subcommand for "mail"`:** the hint lists valid subcommands.
- **Exit 2, `--since must be an RFC 3339 time or a YYYY-MM-DD date`:** fix the date.
- **Output cut short:** `meta.truncated` is true; re-run with `--offset` set to `meta.next_offset`. For lists, the continuation is the final array element `{"next_page_token":"..."}`; pass it to `--page-token`.
- **Attachment download refused:** download is off unless the policy enables it, and `--out` must be inside `read.attachments.out_dir`.
- **Unexpected Graph behaviour on a real tenant** (headers dropped, search unsupported, different error body): the Graph shapes are unverified assumptions. Report the request id and the deviation.
