# Exit codes

Every command prints one JSON envelope and exits with a code that always agrees with `error.code` in it. Check `ok` first.

| Code | `error.code` | Meaning | What to do |
|---|---|---|---|
| 0 | (ok) | Success | Use `data`. Check `meta.truncated`; if true, re-run with `--offset` set to `meta.next_offset`. |
| 1 | `general` | Other error, including a failing selftest | Read `error.message`. Do not retry blindly. |
| 2 | `usage` | Bad command line (unknown command, missing flag, bad `--since`, invalid page token) | Fix the arguments and retry once. |
| 3 | `auth` | Credentials unavailable: the credential daemon is unreachable, or re-enrollment is needed | Stop. A human must act. Do not look for other credentials. See [Troubleshooting](troubleshooting.md#exit-3-credential-daemon-unreachable). |
| 4 | `forbidden` | The server refused the request (HTTP 403) | Final. Report it; do not work around it. |
| 5 | `not_found` | The message, draft, attachment or folder does not exist | Check the id. |
| 6 | `policy_denied` | Refused by client-side policy, or the policy file or its directory is writable by the agent user | Final for that request. Do not retry with altered arguments. Ask an administrator to change policy if the rule is wrong. |
| 7 | `conflict` | Precondition failed, including an earlier send with the same idempotency key whose outcome is unknown | Check the mailbox (Sent Items) before deciding to resend; an administrator may need to clear the pending ledger entry. |
| 8 | `rate_limited` | Throttled by Graph after bounded retries | Wait and retry later. (A policy send-rate cap is exit 6, not 8.) |
| 9 | `validation` | Input or policy file failed validation, or Graph returned 400 | Fix the field named in `error.message`. |

Failure envelope:

```
{"ok":false,"error":{"code":"usage","message":"--to is required"}}
```

`error.hint`, when present, says what to do next.
