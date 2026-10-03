# User documentation

How to adopt, configure and use `outlook`, the CLI that lets an agent read and send mail as its own Microsoft 365 mailbox.

| Guide | Contents |
|---|---|
| [Getting started](getting-started.md) | Install, prerequisites, first commands, what works today |
| [Configuration reference](configuration.md) | Environment variables, the policy file field by field, a sample policy |
| [Usage examples](usage.md) | Every command with example output |
| [Exit codes](exit-codes.md) | What each exit code means and what to do |
| [Troubleshooting](troubleshooting.md) | Common failures, including "daemon unavailable" (exit 3) |

Important status note: in this version no command can reach Microsoft Graph, because the real client for the `agent-okta-d` credential daemon is not built yet. Everything that needs a token exits 3. Also, nothing has been verified against a real tenant. See [Getting started](getting-started.md#current-limitations).
