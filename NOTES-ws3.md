# NOTES ws3

- List commands (mail list/search, draft list, folder list, attachment list) emit `data` as a JSON ARRAY, not the object `{"messages":[...],"next_page_token":...}` that ports.go suggests. Reason: core's output bounding cannot cut an object (ErrBoundTooSmall, exit 2 for any list over 32 KiB). The Graph continuation is a final array element `{"next_page_token":"..."}`; when output is truncated it is cut too and the caller resumes with `--offset`. Suggest recording in docs/requested-core-changes.md (Meta.next_page_token would remove the workaround).
- Output-bound flags are `--format`, `--output-max-bytes`, `--offset` on every command (`--max-bytes` is `mail get`'s body bound per the PRD).
- ADR docs/adr-daemon-client-stub.md references docs/deferred.md (owned by WS5); socket default `/run/agent-okta-d/agent-okta-d.sock` and env `AGENT_OKTA_D_SOCKET` are unverified assumptions.
- Phase C: fill `buildApp` in cmd/outlook/app.go and set `Deps.Selftest` in cmd/outlook/main.go (`deps()`); Selftest is nil today (exit 1 "not available").
