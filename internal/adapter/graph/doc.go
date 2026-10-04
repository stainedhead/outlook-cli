// Package graph implements the usecase.MailReader, usecase.MailWriter and
// usecase.SentItemsProbe ports against Microsoft Graph v1.0.
//
// Every request goes through core httpx (retries, Retry-After, one 401
// refresh, typed errors) with the Host allowlist pinned to the Graph host, and
// every request path starts with /me: the adapter never builds a /users/{id}
// URL, so the agent can only ever see its own mailbox.
//
// ASSUMPTION(unverified against a real tenant): every endpoint shape, query
// option, header and response field in this package is taken from public Graph
// documentation and has never been exercised against a real tenant. Each
// endpoint is tagged in code with that phrase and covered by a test whose name
// contains "Assumed". The list is mirrored in NOTES-ws2.md.
//
// Error contract: errors carry no response bodies. httpx errors pass through
// unchanged (401 -> exit 3, 403 -> 4, 429/503 -> 8); 404 maps to
// domain.NewNotFound; malformed JSON maps to domain.NewGeneral. Write methods
// wrap domain.NotSent around failures that provably left the message
// unaccepted (any 4xx other than 408, a refusal before sending, 429); a
// timeout, 5xx or dropped connection stays ambiguous.
package graph
