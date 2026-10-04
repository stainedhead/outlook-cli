package policyfile

import (
	"strings"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// DefaultAuditPath is the audit log location used by Default and the sample.
const DefaultAuditPath = "/var/log/agent-cli/outlook.audit.jsonl"

// Default returns the PRD section 9 reference policy for mailbox. It is what
// testdata/outlook.policy.yaml expresses (a test keeps them equal) and is the
// baseline for tests and for the selftest matrix. It is deliberately
// restrictive: internal recipients only, no BCC, no reply-all, no attachments
// either way, rate capped, secret and classification filters on.
//
// Default is not loaded implicitly: running without a policy file is a hard
// failure, never a fallback to this value.
func Default(mailbox string) domain.Policy {
	mailbox = strings.ToLower(strings.TrimSpace(mailbox))
	var internal []string
	if i := strings.LastIndex(mailbox, "@"); i >= 0 && i < len(mailbox)-1 {
		internal = []string{mailbox[i+1:]}
	}
	return domain.Policy{
		Profile:         "agent",
		Mailbox:         mailbox,
		InternalDomains: internal,
		Read: domain.ReadPolicy{
			Folders:      []string{"inbox", "Processed"},
			MaxBodyBytes: 16000,
			HTMLToText:   true,
			DefangLinks:  true,
		},
		Send: domain.SendPolicy{
			Mode: domain.SendAllow,
			Recipients: domain.RecipientPolicy{
				AllowDomains:   append([]string(nil), internal...),
				AllowAddresses: nil,
				External:       domain.ExternalDeny,
				MaxTotal:       5,
			},
			SubjectPrefix:  "[agent] ",
			Footer:         "Automated message from agent {agent_id}. A human owns decisions.",
			BodyMaxBytes:   20000,
			ContentFilters: []string{FilterSecretPatterns, FilterClassificationMarkers},
			Rate:           domain.SendRate{PerHour: 20, PerDay: 100},
		},
		Limits:    domain.Limits{MaxResults: 100, MaxWritesPerRun: 20},
		AuditPath: DefaultAuditPath,
	}
}
