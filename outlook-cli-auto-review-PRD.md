# Review of outlook-cli-PRD.md (automated, dev-flow:review-prd)

Verdict: **Needs revision** (minor; defects below fixed or carried into the spec as explicit assumptions). Not "Major gaps".

| Dimension | Status | Note |
|---|---|---|
| Problem clarity | Pass | Agent email as own Entra user; risks named |
| Scope definition | Pass | G1-G5 and explicit non-goals |
| Functional completeness | Warn | Command table covers goals but requirements are not numbered FR-ids; spec assigns IDs |
| NFR coverage | Pass | Performance (<50 ms), reliability (throttling), security (section 5), observability (audit) |
| Acceptance criteria | Warn | Milestone acceptance is coarse; spec adds testable criteria. Real-tenant criteria (M0) out of scope here |
| Dependency identification | Pass | Graph, daemon, core, Exchange, CA named |
| Open questions | Pass | Sections 15, 16.8, 17.1 |

## PRD defects found
1. 16.7 milestone note and AGENTS.md said core has no tag / no `require`; core v0.1.0 now exists. Fixed in both.
2. Command table lacks FR numbering; handled by spec (FR-ids assigned there).
3. Graph endpoint shapes, `X-Agent-*` headers, Sent Items idempotency, auth-results are unconfirmed; kept as explicit assumptions ("unverified against a real tenant").
4. M0 spikes need a real tenant; out of scope for implementation, delivered as a checklist doc.
