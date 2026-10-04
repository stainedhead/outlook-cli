// Package domain holds the entities, value objects, policy data model and
// error taxonomy of the outlook CLI. It is the innermost layer.
//
// # Dependency rule
//
// domain imports the standard library and exactly one package from
// agent-cli-core: output, and only for output.Category so that domain errors
// reach a process exit code with no mapping table (see Error). It never
// imports internal/usecase, any adapter, httpx, auth, policy, audit, selftest
// or docgen. internal/archtest enforces this.
//
// # What lives here
//
//   - types.go: mail entities (Address, Folder, MessageSummary, Message,
//     Attachment, Link, ...), request value objects and Page.
//   - policy_types.go: the client-side policy data model (PRD section 9) and
//     the Decision type. Evaluation logic (recipient rules, caps, rate,
//     content filters) is WS1 code in this package; loading YAML is WS4 code
//     in internal/adapter/policyfile.
//   - errors.go: Error, its constructors, and ErrNotSent.
//
// # Untrusted content
//
// Message.Subject, Message.Body.Text, Address.Name and Attachment.Name are
// free text written by other people. The domain stores them as plain strings;
// the CLI presenter (WS3) is the only place that wraps them in
// output.Untrusted. See ports.go in internal/usecase for the field list.
package domain
