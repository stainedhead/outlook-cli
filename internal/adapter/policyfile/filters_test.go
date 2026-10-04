package policyfile

import (
	"sort"
	"strings"
	"testing"

	"github.com/stainedhead/outlook-cli/internal/usecase"
)

func kinds(fs []usecase.FilterFinding) []string {
	var k []string
	for _, f := range fs {
		k = append(k, f.Kind)
	}
	sort.Strings(k)
	return k
}

func TestSecretPatterns(t *testing.T) {
	f := NewSecretPatterns()
	if f.Name() != "secret_patterns" {
		t.Fatal(f.Name())
	}
	cases := []struct{ name, text, kind string }{
		{"aws access key", "key AKIAIOSFODNN7EXAMPLE here", "aws_access_key_id"},
		{"private key", "-----BEGIN RSA PRIVATE KEY-----\nabc", "private_key"},
		{"private key plain", "-----BEGIN PRIVATE KEY-----", "private_key"},
		{"github token", "ghp_" + strings.Repeat("a", 36), "github_token"},
		{"slack token", "xoxb-1234567890-abcdefghij", "slack_token"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r", "jwt"},
		{"bearer", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123", "bearer_token"},
		{"google api key", "AIza" + strings.Repeat("A", 35), "google_api_key"},
		{"sk key", "sk-" + strings.Repeat("a1", 15), "api_key_sk"},
		{"password assignment", "password = hunter2hunter2", "credential_assignment"},
		{"api key assignment", "API_KEY: abcd1234efgh", "credential_assignment"},
		{"url credentials", "postgres://user:pa55w0rd@db.internal/x", "url_credentials"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := f.Scan("hello\n" + c.text)
			if len(fs) == 0 {
				t.Fatal("no finding")
			}
			found := false
			for _, x := range fs {
				if x.Kind == c.kind {
					found = true
				}
				if x.Filter != "secret_patterns" {
					t.Fatalf("filter %q", x.Filter)
				}
				if x.Offset < 6 {
					t.Fatalf("offset %d not past prefix", x.Offset)
				}
			}
			if !found {
				t.Fatalf("kinds %v want %s", kinds(fs), c.kind)
			}
		})
	}
}

func TestSecretPatternsNoFalsePositives(t *testing.T) {
	f := NewSecretPatterns()
	for _, s := range []string{
		"",
		"Hi team, the build is green. Please review PR 42 tomorrow.",
		"The password policy requires rotation.",
		"Contact me at a@corp.example.com or https://corp.example.com/path",
		"token: short",
		"ask-me-anything",
	} {
		if fs := f.Scan(s); len(fs) != 0 {
			t.Errorf("%q -> %v", s, fs)
		}
	}
}

func TestFindingsNeverContainMatchedText(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	for _, fs := range [][]usecase.FilterFinding{NewSecretPatterns().Scan("x " + secret)} {
		for _, f := range fs {
			if strings.Contains(f.Filter+f.Kind, secret) {
				t.Fatal("finding leaks match")
			}
		}
	}
}

func TestFindingsSortedByOffset(t *testing.T) {
	fs := NewSecretPatterns().Scan("password = abcdefgh12 and AKIAIOSFODNN7EXAMPLE")
	if len(fs) < 2 {
		t.Fatalf("%v", fs)
	}
	for i := 1; i < len(fs); i++ {
		if fs[i].Offset < fs[i-1].Offset {
			t.Fatalf("unsorted %v", fs)
		}
	}
}

func TestClassificationMarkers(t *testing.T) {
	f := NewClassificationMarkers()
	if f.Name() != "classification_markers" {
		t.Fatal(f.Name())
	}
	for _, s := range []string{
		"CONFIDENTIAL: plan", "this is Top Secret stuff", "[internal only]", "Company Confidential",
		"FOUO", "for official use only", "Do Not Distribute", "attorney-client privileged",
	} {
		if len(f.Scan(s)) == 0 {
			t.Errorf("missed %q", s)
		}
	}
	fs := f.Scan("hi\nstrictly confidential")
	if len(fs) == 0 || fs[0].Offset != 12 || fs[0].Filter != "classification_markers" {
		t.Fatalf("%v", fs)
	}
	for _, s := range []string{"", "see you at lunch", "confidentiality is important", "unconfidential"} {
		if got := f.Scan(s); len(got) != 0 {
			t.Errorf("%q -> %v", s, got)
		}
	}
}

func TestFilters(t *testing.T) {
	m, err := Filters([]string{"secret_patterns", "classification_markers"})
	if err != nil || len(m) != 2 || m["secret_patterns"] == nil || m["classification_markers"] == nil {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := Filters([]string{"nope"}); err == nil {
		t.Fatal("unknown filter must fail")
	}
	if m, err := Filters(nil); err != nil || len(m) != 0 {
		t.Fatal("empty ok")
	}
	if !reflectKnown() {
		t.Fatal("KnownFilters")
	}
}

func reflectKnown() bool {
	k := KnownFilters()
	return len(k) == 2 && k[0] == "classification_markers" && k[1] == "secret_patterns"
}
