# NOTES-b (group B)

## Done
- FR-R2: policyfile.Load now proves authorship by ownership (root or WithTrustedUIDs, never the euid), no group/world-writable file/ancestor, symlink owner check, O_NOFOLLOW open + fstat on the descriptor. `AllowWritable` renamed `AllowUntrusted` (tests only). OUTLOOK_POLICY still selects a path but the target must pass the same check. Dev override `OUTLOOK_POLICY_INSECURE=1` exists only with `-tags outlookdev` (cmd/outlook/devpolicy_dev.go); release build ignores it.
- FR-R14 cmd/outlook coverage 86.6 percent (assemble failure paths).
- Per-install page-token key provider: `newPageKeyProvider(path) func() ([]byte, error)` and `pageKeyPath(ledgerPath)` in cmd/outlook/pagekey.go (32 random bytes, 0600, dir 0700, file `outlook.pagekey` next to the ledger, atomic create, cached). Wiring after merging A: `gcfg.PageTokenKey = newPageKeyProvider(pageKeyPath(ledgerPath))` in assemble (after ledgerPath is computed; move graph.New below it).

## Deferred (need files I do not own / core)
- FR-R2 "whoami and selftest report the policy path in force": needs `WhoamiResult.PolicyPath` (usecase/ports.go, C) + presentWhoami (cli/present.go, E); selftest needs a field on cli.Deps/selftest output (cli.go, E). appConfig.PolicyPath (cmd/outlook) is the value to pass.
- FR-R13 audit fields (recipient count, recipient-set hash, message/draft id): core audit.Record (v0.1.0) has no extension fields, so the sink cannot emit them. Needs either a core change (add to docs/requested-core-changes.md) or C adding AuditEntry.Detail that the sink folds into policy_decision. HTTPStatus is already forwarded by the sink; C/A must populate it. Ledger dir 0700/owner check belongs to D.
- AGENT_OKTA_D_SOCKET redirect (FR-R2 text): daemon client is a stub; socket path ownership check should be added when the real client lands.

## Doc-impacting (for F)
- user-docs/configuration.md: policy must be root-owned (or trusted uid), file and every ancestor not group/world-writable; install commands: `sudo install -d -o root -m 0755 /etc/agent-cli && sudo install -o root -m 0644 outlook.policy.yaml /etc/agent-cli/outlook.policy.yaml`. Running as root is refused (euid is never trusted).
- docs: ownership check replaces access(2) W_OK; OUTLOOK_POLICY semantics; outlookdev tag; page-token key file `outlook.pagekey` (0600) next to the ledger, deleting it invalidates outstanding page tokens.
- docs/requested-core-changes.md: audit.Record extension fields.
