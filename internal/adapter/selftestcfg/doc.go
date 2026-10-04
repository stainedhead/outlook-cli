// Package selftestcfg builds the allow/deny matrix for `outlook selftest` from
// the loaded policy and the probe that drives it through usecase.Commands.
//
// Rows are derived from the policy, so the matrix always describes the policy
// actually in force: a row expects Deny where the policy forbids and Allow
// where it permits. Rows marked ReadOnly never write and are the only ones run
// in live mode; the rest run against fakes (CI) and use dry-run requests
// wherever the command supports it.
//
// The probe maps a nil error to Allow and a policy_denied error (exit 6) to
// Deny. Any other error fails the row with the error text, so a broken
// backend is never mistaken for a policy decision.
package selftestcfg
