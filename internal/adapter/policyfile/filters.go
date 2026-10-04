package policyfile

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/stainedhead/outlook-cli/internal/usecase"
)

// Names of the content filters (policy send.content_filters).
const (
	FilterSecretPatterns        = "secret_patterns"
	FilterClassificationMarkers = "classification_markers"
)

// KnownFilters lists the filter names the policy may reference, sorted.
func KnownFilters() []string {
	return []string{FilterClassificationMarkers, FilterSecretPatterns}
}

// Filters builds the ContentFilter set for the names a policy lists, keyed by
// name as usecase.Deps.Filters expects. An unknown name is an error.
func Filters(names []string) (map[string]usecase.ContentFilter, error) {
	out := make(map[string]usecase.ContentFilter, len(names))
	for _, n := range names {
		switch n {
		case FilterSecretPatterns:
			out[n] = NewSecretPatterns()
		case FilterClassificationMarkers:
			out[n] = NewClassificationMarkers()
		default:
			return nil, fmt.Errorf("policyfile: unknown content filter %q", n)
		}
	}
	return out, nil
}

type pattern struct {
	kind string
	re   *regexp.Regexp
}

// regexFilter scans text with a fixed list of patterns. Findings carry the
// kind and byte offset only, never the matched text.
type regexFilter struct {
	name     string
	patterns []pattern
}

func (f regexFilter) Name() string { return f.name }

func (f regexFilter) Scan(text string) []usecase.FilterFinding {
	var out []usecase.FilterFinding
	for _, p := range f.patterns {
		for _, loc := range p.re.FindAllStringIndex(text, -1) {
			out = append(out, usecase.FilterFinding{Filter: f.name, Kind: p.kind, Offset: loc[0]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Offset < out[j].Offset })
	return out
}

// NewSecretPatterns returns the secret_patterns filter: well-known credential
// formats. It is a best-effort guardrail against an agent echoing a secret it
// can see, not a DLP product (Exchange DLP is the real control). Patterns are
// deliberately anchored to distinctive prefixes to keep false positives low.
func NewSecretPatterns() usecase.ContentFilter {
	return regexFilter{name: FilterSecretPatterns, patterns: []pattern{
		{"aws_access_key_id", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
		{"private_key", regexp.MustCompile(`-----BEGIN (?:[A-Z]+ )*PRIVATE KEY-----`)},
		{"github_token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`)},
		{"slack_token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
		{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
		{"bearer_token", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{20,}`)},
		{"google_api_key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
		{"api_key_sk", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
		{"credential_assignment", regexp.MustCompile(`(?i)\b(?:password|passwd|secret|api[_-]?key|access[_-]?token|client[_-]?secret)\b\s*[:=]\s*["']?[^\s"']{8,}`)},
		{"url_credentials", regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s/:@]+:[^\s/@]+@`)},
	}}
}

// NewClassificationMarkers returns the classification_markers filter: handling
// labels that mean a message must not leave the organisation by an agent.
func NewClassificationMarkers() usecase.ContentFilter {
	m := func(kind, words string) pattern {
		return pattern{kind, regexp.MustCompile(`(?i)(?:^|[^\pL\pN])(` + words + `)(?:$|[^\pL\pN])`)}
	}
	return markerFilter{regexFilter{name: FilterClassificationMarkers, patterns: []pattern{
		m("confidential", `confidential`),
		m("top_secret", `top[ _-]secret`),
		m("internal_only", `internal[ _-]only`),
		m("restricted", `restricted`),
		m("privileged", `(?:attorney[ _-]client[ _-])?privileged`),
		m("official_use_only", `fouo|for official use only`),
		m("no_distribution", `do not distribute|not for distribution`),
	}}}
}

// markerFilter reports the offset of the marker itself, not of the delimiter
// character the pattern consumes before it.
type markerFilter struct{ regexFilter }

func (f markerFilter) Scan(text string) []usecase.FilterFinding {
	var out []usecase.FilterFinding
	for _, p := range f.patterns {
		for _, loc := range p.re.FindAllStringSubmatchIndex(text, -1) {
			out = append(out, usecase.FilterFinding{Filter: f.name, Kind: p.kind, Offset: loc[2]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Offset < out[j].Offset })
	return out
}
