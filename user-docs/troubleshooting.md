# Troubleshooting

Every failure is a JSON envelope with `error.code`, `error.message` and often `error.hint`. See [Exit codes](exit-codes.md).

## Exit 3: credential daemon unreachable

```
$ outlook whoami
{"ok":false,"error":{"code":"auth","message":"credential daemon unreachable at socket \"/run/agent-okta-d/agent-okta-d.sock\": the agent-okta-d service may not be running","hint":"Check that the credential daemon is running and listening on \"/run/agent-okta-d/agent-okta-d.sock\". No fallback credentials are used."}}
```

This is what a token-requiring command prints when nothing is listening on the daemon socket (the path in the message is `AGENT_OKTA_D_SOCKET`, or the platform default). Check that the `agent-okta-d` service is running, that the socket path is right and that the agent user may read the socket. There is no workaround; do not look for other credentials.

Other exit 3 causes, with the same envelope shape:

| Message says | Meaning | Fix |
|---|---|---|
| a human must run: agent-okta-d enroll msgraph | The daemon needs re-enrollment, or the credential was revoked | A human runs `agent-okta-d enroll msgraph` |
| not configured in the credential daemon / not authorized | The daemon has no `msgraph` provider for this agent, or does not allow it | Ask the daemon's administrator |

Exit 8 means the daemon is temporarily degraded or asked for a delay; the hint names the wait. Retry after that time.

Commands that still work: `outlook --help`, `outlook <command> --help`, `outlook version`, `outlook skill`.

## Policy problems

| Symptom | Cause | Fix |
|---|---|---|
| Exit 9, `policy: cannot read /etc/agent-cli/outlook.policy.yaml` | No policy file at the path | Install it, or point `OUTLOOK_POLICY` at it |
| Exit 9, `policy invalid: ...` | Unknown or duplicate key, missing required key, bad value | The message names the key. Fix it; see [Configuration](configuration.md) |
| Exit 6, `policy file ... is not owned by a trusted account (root)` | The file, a parent directory or a symlink is owned by the agent user (or another untrusted account) | Reinstall as root: `sudo install -d -o root -m 0755 /etc/agent-cli && sudo install -o root -m 0644 outlook.policy.yaml /etc/agent-cli/outlook.policy.yaml` |
| Exit 6, `... is writable by group or others` | The file or a parent directory is group or world writable | `chmod go-w` on it as root |
| Exit 6 when running as root | Root is never trusted as the running user | Run as the agent user |
| Exit 6, `policy: cannot check ownership of ...` | A component of the path could not be inspected | Fix access to the path |
| Exit 6 on a mailbox check | Policy `mailbox` differs from the signed-in user | Correct `mailbox` or the daemon enrollment |
| Exit 6 on `mail send` | Recipient not allowed, `external: deny`, too many recipients, bcc, reply-all, attachments, body too large, content filter hit, or rate cap | The message names the rule. Use `--dry-run` to see the decision without sending |
| Exit 6 on `mail list` with a folder | Folder not in `read.folders` | Add it to the policy or use an allowed folder |

## Other failures

- **Exit 1 from `outlook selftest`, every row failing with a daemon error:** expected with the placeholder client (exit 3 cause). The selftest needs a working backend.
- **Audit log or ledger directory refused (exit 1):** `audit.path` must be in a directory the agent user can create and append to, owned by the agent user, with mode 0700. A directory shared with other services (for example 0755) is refused; use a private one.
- **Exit 2 on `--page-token`:** the token is invalid, was made for a different command, folder or query, or the page-token key file `outlook.pagekey` was deleted. Restart the listing without a token and keep the same flags and `--folder` for the whole walk.
- **Exit 1 on a list with more pages:** the page-token key could not be read or created; check that the audit/ledger directory is writable.
- **Exit 2, `must be a regular file` or `exceeds the 4 MiB limit` on `--body-file`:** the path is a device, pipe or directory, or the body is over 4 MiB.
- **Exit 2 on an id:** ids may contain only letters, digits, `_`, `=` and `-` (at most 512 characters).
- **Exit 7 `conflict` on `mail draft send`:** the draft changed between the first and second read, or the draft has no From. Review it and retry.
- **Result has `warnings`:** `ledger_update_failed` means the mail was sent but the ledger update failed (check the directory); `probe=inconclusive` means the Sent Items lookup was refused by Graph and the send relied on the local ledger only.
- **Exit 7 on send:** an earlier run with the same `--idempotency-key` did not finish and its outcome is unknown, or the key was reused with a different message. Check Sent Items; to resend deliberately use a new key. An administrator can clear a stuck pending entry in `outlook.idempotency.json`. Pending entries also count toward the hourly and daily rate caps.
- **Exit 2, `unknown or missing subcommand for "mail"`:** the hint lists valid subcommands.
- **Exit 2, `--since must be an RFC 3339 time or a YYYY-MM-DD date`:** fix the date.
- **Output cut short:** `meta.truncated` is true; re-run with `--offset` set to `meta.next_offset`. For lists, the continuation is the final array element `{"next_page_token":"..."}`; pass it to `--page-token`.
- **Attachment download refused:** download is off unless the policy enables it, and `--out` must be inside `read.attachments.out_dir`. A file with an existing name is saved as `name (1).ext`, `name (2).ext` and so on.
- **Unexpected Graph behaviour on a real tenant** (headers dropped, search unsupported, different error body): the Graph shapes are unverified assumptions. Report the request id and the deviation.
