// Package policyfile loads the client-side policy of PRD section 9 from a
// YAML file into domain.Policy and supplies the content filters named by
// send.content_filters.
//
// The load is strict and fails closed: unknown keys, duplicate keys, an empty
// file, more than one YAML document, an invalid value, and a policy file whose
// owner or ancestors are not provably someone other than the agent (FR-R2) are all refused. A parsed
// policy is a guardrail, not a security control: Exchange mail flow and
// Conditional Access remain the boundary.
//
// Core's policy package (agent-cli-core v0.1.0) is a generic verb/resource
// rule engine with a different schema, so only its trust idea is
// mirrored here, strengthened to an ownership check (see trust.go).
package policyfile
