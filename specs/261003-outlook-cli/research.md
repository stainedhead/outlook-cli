# Research: outlook-cli (2026-10-03) - Source: outlook-cli-PRD.md

## Research Questions
1. Which core v0.1.0 interfaces does the daemon-client stub implement, and what is the exact exit-3 message?
2. Which Graph v1.0 endpoint shapes, `$search` syntax, `Prefer` body header and `x-` header behavior hold (unverified against a real tenant)?
3. Does core `policy` cover recipient/rate rules or only the engine and file loading?
4. How does core `selftest` express fake-backed vs live mode?
5. Is an Sent Items header lookup viable for idempotency (P1)?

## Industry Standards / Existing Implementations / API Documentation / Best Practices
[TBD during implementation]

## Open Questions
PRD sections 15 and 16.8 (all tenant/admin decisions, out of scope here).

## References
PRD; agent-cli-core README and docs/technical-details.md.
