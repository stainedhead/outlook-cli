# NOTES-ws4

go.mod: `go mod tidy` promotes `github.com/goccy/go-yaml v1.19.2` (same version core uses) from indirect to direct. The lead must keep that change when merging.

Core gaps (for docs/requested-core-changes.md, lead to record):
- core `policy` has no standalone "is this file agent-writable" check and its schema is a generic rule engine; policyfile mirrors `policy.Load`'s access(2) W_OK check on file and directory itself.
- core `audit.WithClock` takes the internal `clock.Clock`; the sink avoids it by stamping `Record.Timestamp` from `usecase.Clock`.

Behaviour decisions the lead/WS1/WS3 should know:
- policyfile.Parse/Load: invalid content -> domain validation error (exit 9); agent-writable file or dir -> policy_denied (exit 6); `policyfile.AllowWritable()` is dev/test only.
- Omitted `send` section or `send.mode` -> deny; `send.mode` allow/dry_run_only requires `recipients.max_total >= 1`.
- `read.attachments`: `{download: deny}` or `{allow_types, max_bytes, out_dir}` (abs path); mixing is an error.
- `policyfile.Filters(names)` builds the `usecase.Deps.Filters` map; `KnownFilters()` are the only valid `content_filters` names.
- auditlog.Sink: composition root should use `audit.Block` failure mode for send/write; Warn is the zero value.
- selftestcfg.Runner(cmds, policy, readOnly): live mode passes readOnly=true; write rows use DryRun requests.
  Selftest expectations assume WS1 denies with policy_denied (exit 6) before touching Graph for: unlisted folder,
  over max_total, bcc, send attachments, reply --all, filter hits, move to Deleted Items, and attachment --out outside quarantine.
