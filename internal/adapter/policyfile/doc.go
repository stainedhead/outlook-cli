// Package policyfile loads the client-side policy of PRD section 9 from a
// YAML file into domain.Policy and supplies the content filters named by
// send.content_filters.
//
// The load is strict and fails closed: unknown keys, duplicate keys, an empty
// file, more than one YAML document, an invalid value, and a policy file (or
// its directory) that the current user can write are all refused. A parsed
// policy is a guardrail, not a security control: Exchange mail flow and
// Conditional Access remain the boundary.
//
// Core's policy package (agent-cli-core v0.1.0) is a generic verb/resource
// rule engine with a different schema, so only its writable-file semantic is
// mirrored here (see perm_unix.go and docs/requested-core-changes.md).
package policyfile
