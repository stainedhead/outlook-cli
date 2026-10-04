package domain

import (
	"strings"
	"unicode"
)

// Recipients is the validated, de-duplicated recipient set of one outgoing
// message. An address appears once: To wins over Cc, Cc over Bcc.
type Recipients struct {
	To, Cc, Bcc []Address
}

// All returns To, Cc and Bcc in that order.
func (r Recipients) All() []Address {
	out := make([]Address, 0, r.Total())
	out = append(out, r.To...)
	out = append(out, r.Cc...)
	return append(out, r.Bcc...)
}

// Total is the number of distinct recipients.
func (r Recipients) Total() int { return len(r.To) + len(r.Cc) + len(r.Bcc) }

const maxAddressLen = 254

// ParseAddress validates one plain mail address (no display name, no angle
// brackets, no lists) and returns it trimmed and lower-cased. It rejects
// control characters (CR/LF header injection), whitespace and the characters
// that would let one string name several recipients.
func ParseAddress(raw string) (Address, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Address{}, NewValidation("recipient address is empty")
	}
	if len(s) > maxAddressLen {
		return Address{}, NewValidation("recipient address is too long")
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune(`<>(),;:"\[]`, r) {
			return Address{}, NewValidation("recipient address contains a forbidden character")
		}
	}
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return Address{}, NewValidation("recipient must be a single address of the form user@domain")
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return Address{}, NewValidation("recipient domain is malformed")
	}
	return Address{Address: strings.ToLower(s)}, nil
}

// AddressDomain returns the lower-case domain of an address, or "" when the
// address has no usable domain.
func AddressDomain(addr string) string {
	i := strings.LastIndex(addr, "@")
	if i < 0 || i == len(addr)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(addr[i+1:]))
}

// NormalizeRecipients parses and de-duplicates (case-insensitively) the three
// recipient lists. At least one To is required.
func NormalizeRecipients(to, cc, bcc []string) (Recipients, error) {
	var out Recipients
	seen := map[string]bool{}
	add := func(raw []string, dst *[]Address) error {
		for _, s := range raw {
			a, err := ParseAddress(s)
			if err != nil {
				return err
			}
			if seen[a.Address] {
				continue
			}
			seen[a.Address] = true
			*dst = append(*dst, a)
		}
		return nil
	}
	if err := add(to, &out.To); err != nil {
		return Recipients{}, err
	}
	if err := add(cc, &out.Cc); err != nil {
		return Recipients{}, err
	}
	if err := add(bcc, &out.Bcc); err != nil {
		return Recipients{}, err
	}
	if len(out.To) == 0 {
		return Recipients{}, NewValidation("at least one --to recipient is required")
	}
	return out, nil
}

// SenderTrustOf is the sender_trust heuristic: internal when the sender's
// domain is exactly one of Policy.InternalDomains, external for any other
// domain, unknown without a parsable address. Advisory only; a sender address
// can be spoofed, so this is never an authorization decision.
func (p Policy) SenderTrustOf(a Address) SenderTrust {
	d := AddressDomain(a.Address)
	if d == "" {
		return TrustUnknown
	}
	if containsFold(p.InternalDomains, d) {
		return TrustInternal
	}
	return TrustExternal
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), v) {
			return true
		}
	}
	return false
}
