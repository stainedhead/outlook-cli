package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode"
)

// Custom header names added to every outgoing message.
//
// ASSUMPTION(unverified against a real tenant): Graph accepts custom x-
// internet headers on created messages.
const (
	HeaderAgentID        = "X-Agent-Id"
	HeaderAgentRun       = "X-Agent-Run"
	HeaderIdempotencyKey = "X-Agent-Idempotency-Key"
)

const (
	maxSubjectLen = 998
	maxKeyLen     = 128
	footerSep     = "\n\n-- \n"
)

// hasControl reports whether s contains any control character, which in a
// header value is CR/LF injection or worse.
func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// ValidateSubject rejects an empty subject, control characters (CRLF header
// injection) and an overlong subject.
func ValidateSubject(s string) error {
	if strings.TrimSpace(s) == "" {
		return NewValidation("subject is empty")
	}
	if hasControl(s) {
		return NewValidation("subject contains a control character or line break")
	}
	if len(s) > maxSubjectLen {
		return NewValidation("subject is too long")
	}
	return nil
}

// ValidateBody rejects an empty body.
func ValidateBody(s string) error {
	if strings.TrimSpace(s) == "" {
		return NewValidation("body is empty")
	}
	return nil
}

// ValidateIdempotencyKey accepts an empty key (no idempotency) or a printable
// key of at most 128 bytes with no whitespace or control characters.
func ValidateIdempotencyKey(k string) error {
	if len(k) > maxKeyLen {
		return NewValidation("idempotency key is too long (max 128 bytes)")
	}
	for _, r := range k {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return NewValidation("idempotency key must not contain whitespace or control characters")
		}
	}
	return nil
}

// SanitizeHeaderValue replaces control characters with a space and trims.
// Used for values we compose ourselves (agent id, run id) so a hostile
// environment cannot inject headers.
func SanitizeHeaderValue(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s))
}

// ApplySubjectPrefix prepends the policy prefix unless the subject already
// starts with it. The prefix is policy text; control characters are removed.
func ApplySubjectPrefix(prefix, subject string) string {
	prefix = SanitizeHeaderValue(prefix)
	if prefix == "" || strings.HasPrefix(subject, prefix) {
		return subject
	}
	if strings.HasSuffix(prefix, " ") || strings.HasPrefix(subject, " ") {
		return prefix + subject
	}
	return prefix + " " + subject
}

// ComposeBody appends the policy footer to the body with {agent_id} replaced.
// A policy without a footer leaves the body as is. The footer is separated by
// the conventional "-- " signature line.
func ComposeBody(body, footer, agentID string) string {
	f := strings.TrimSpace(strings.ReplaceAll(footer, "{agent_id}", SanitizeHeaderValue(agentID)))
	if f == "" {
		return body
	}
	return strings.TrimRight(body, "\n ") + footerSep + f + "\n"
}

// AgentHeaders returns the X-Agent-* headers for an outgoing message. Empty
// values are omitted.
func AgentHeaders(agentID, runID, idempotencyKey string) []Header {
	var hs []Header
	for _, h := range []Header{
		{HeaderAgentID, SanitizeHeaderValue(agentID)},
		{HeaderAgentRun, SanitizeHeaderValue(runID)},
		{HeaderIdempotencyKey, SanitizeHeaderValue(idempotencyKey)},
	} {
		if h.Value != "" {
			hs = append(hs, h)
		}
	}
	return hs
}

// Fingerprint is a stable hash of what a send means: the kind of operation and
// the recipients, subject and body. It deliberately excludes headers (the run
// id differs between retries) and never stores the content itself. The ledger
// compares it so a key reused for a different message is refused.
func Fingerprint(kind string, m OutgoingMessage) string {
	h := sha256.New()
	put := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte{':'})
		h.Write([]byte(s))
	}
	put(kind)
	for _, list := range [][]Address{m.To, m.Cc, m.Bcc} {
		put(strconv.Itoa(len(list)))
		for _, a := range list {
			put(a.Address)
		}
	}
	put(m.Subject)
	put(m.Body)
	return hex.EncodeToString(h.Sum(nil))
}
