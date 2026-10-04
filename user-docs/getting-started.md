# Getting started

## What `outlook` is

A command-line tool for an autonomous agent to read, triage and send email as its own Microsoft 365 user, through Microsoft Graph. It only ever addresses the agent's own mailbox (`/me`), applies a client-side policy to everything it sends, and marks inbound mail as untrusted.

## Current limitations

- **Needs a running credential daemon.** `outlook` gets short-lived tokens only from the `agent-okta-d` daemon (provider `msgraph`), over its unix socket (`AGENT_OKTA_D_SOCKET`). Without a reachable daemon, every command that needs a token (everything except `help`, `version`, `skill`) fails with exit code 3 after the policy loads. There are no fallback credentials. The adapter has been tested against the daemon's fake, not a live daemon. See [Troubleshooting](troubleshooting.md#exit-3-credential-daemon-unreachable).
- **Not verified against a real tenant.** The Graph request shapes and several Microsoft 365 behaviours (custom headers, Sent Items search, error codes, anti-spoofing results) are assumptions that have not been checked against a real Microsoft 365 tenant. Treat first use in a sandbox tenant as a test.
- **Server-side controls are yours to set up.** The policy is a client-side guardrail. Recipient restrictions, DLP and a disclaimer should also be enforced by Exchange mail flow rules, which the Exchange team owns. `outlook` does not configure them.
- Supported platforms: macOS (Apple silicon) and Linux (amd64, arm64). Native Windows is not supported.

## Install

Build from source with Go 1.27 (the private core module needs `GOPRIVATE=github.com/stainedhead/*` and access to the repository):

```
git clone https://github.com/stainedhead/outlook-cli.git
cd outlook-cli
make build          # writes bin/outlook
bin/outlook version
```

`make cross` writes `dist/outlook-darwin-arm64`, `dist/outlook-linux-amd64` and `dist/outlook-linux-arm64`. There are no published release binaries yet.

## Prerequisites for real use (outside this tool)

1. A licensed Microsoft 365 user for the agent, with its own mailbox.
2. The `agent-okta-d` daemon running for that user, enrolled once by a human, with the `msgraph` provider. (Not usable with this version; see above.)
3. Delegated Graph scopes limited to `Mail.ReadWrite`, `Mail.Send`, `User.Read`, `offline_access`. Do not grant `.Shared` mail scopes or `MailboxSettings.ReadWrite`.
4. A policy file installed by an administrator (see [Configuration](configuration.md)).

## First commands

Commands that need no policy or credentials:

```
$ outlook version
{"ok":true,"data":{"commit":"904844a","date":"2026-10-03T23:46:52Z","version":"904844a"},"meta":{"truncated":false,"next_offset":null,"count":0}}

$ outlook --help        # JSON list of every command with its usage
$ outlook mail send --help
$ outlook skill         # prints the agent skill document (Markdown)
```

Install a policy (administrator, owned by root, in a root-owned directory not writable by others, see [Configuration](configuration.md)), then:

```
$ OUTLOOK_POLICY=/etc/agent-cli/outlook.policy.yaml outlook whoami
```

Today this prints the exit-3 error described in [Troubleshooting](troubleshooting.md). With a working daemon it would print the mailbox, agent id and effective limits (see [Usage](usage.md#whoami)).

## Where things are written

- Audit log: the `audit.path` from the policy (JSON lines, one per command, no message bodies).
- Idempotency ledger: `outlook.idempotency.json`, in the same directory as the audit log, plus the page-token key `outlook.pagekey`.
- Attachments, only if enabled: under `read.attachments.out_dir`.

The audit directory must be private to the agent user (owned by it, mode 0700); the policy file and its directory must be root-owned and not writable by it.

## Next

[Configuration reference](configuration.md), [Usage examples](usage.md), [Exit codes](exit-codes.md).
