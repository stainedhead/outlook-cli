package domain

import (
	"strings"
	"testing"
)

func TestValidateSubject(t *testing.T) {
	for _, s := range []string{"", "  ", "a\r\nBcc: x@y.com", "a\nb", "a\x00", strings.Repeat("a", 999)} {
		if err := ValidateSubject(s); err == nil {
			t.Errorf("ValidateSubject(%q) accepted", s)
		}
	}
	if err := ValidateSubject("Build 4812 failed"); err != nil {
		t.Error(err)
	}
}

func TestValidateBody(t *testing.T) {
	if ValidateBody(" \n") == nil || ValidateBody("") == nil {
		t.Error("empty body accepted")
	}
	if ValidateBody("x") != nil {
		t.Error("body rejected")
	}
}

func TestValidateIdempotencyKey(t *testing.T) {
	for _, k := range []string{"", "run-42/step.1", "a:b"} {
		if err := ValidateIdempotencyKey(k); err != nil {
			t.Errorf("%q: %v", k, err)
		}
	}
	for _, k := range []string{"a b", "a\nb", "a\x00", strings.Repeat("k", 129)} {
		if ValidateIdempotencyKey(k) == nil {
			t.Errorf("%q accepted", k)
		}
	}
}

func TestSanitizeHeaderValue(t *testing.T) {
	if got := SanitizeHeaderValue(" a\r\nBcc: x\t"); got != "a  Bcc: x" {
		t.Errorf("got %q", got)
	}
}

func TestApplySubjectPrefix(t *testing.T) {
	cases := []struct{ prefix, subj, want string }{
		{"[agent] ", "Hi", "[agent] Hi"},
		{"[agent]", "Hi", "[agent] Hi"},
		{"[agent] ", "[agent] Hi", "[agent] Hi"},
		{"", "Hi", "Hi"},
		{"[agent]", " Hi", "[agent] Hi"},
		{"[a\r\ngent] ", "Hi", "[a  gent] Hi"},
	}
	for _, c := range cases {
		if got := ApplySubjectPrefix(c.prefix, c.subj); got != c.want {
			t.Errorf("(%q,%q) = %q, want %q", c.prefix, c.subj, got, c.want)
		}
	}
}

func TestComposeBody(t *testing.T) {
	got := ComposeBody("Hello\n\n", "Automated message from agent {agent_id}. A human owns decisions.", "rev-01")
	want := "Hello\n\n-- \nAutomated message from agent rev-01. A human owns decisions.\n"
	if got != want {
		t.Errorf("got %q", got)
	}
	if ComposeBody("x", "  ", "id") != "x" {
		t.Error("blank footer must leave body")
	}
	if got := ComposeBody("x", "id={agent_id}", "a\r\nb"); !strings.Contains(got, "id=a  b") || strings.Contains(got, "\r") {
		t.Errorf("agent id not sanitized: %q", got)
	}
}

func TestAgentHeaders(t *testing.T) {
	hs := AgentHeaders("rev-01", "run-9", "key1")
	if len(hs) != 3 || hs[0] != (Header{HeaderAgentID, "rev-01"}) || hs[1].Name != HeaderAgentRun || hs[2].Name != HeaderIdempotencyKey {
		t.Errorf("headers = %+v", hs)
	}
	hs = AgentHeaders("rev\r\n01", "", "")
	if len(hs) != 1 || strings.ContainsAny(hs[0].Value, "\r\n") {
		t.Errorf("headers = %+v", hs)
	}
	if len(AgentHeaders("", "", "")) != 0 {
		t.Error("empty headers must be omitted")
	}
}

func TestFingerprint(t *testing.T) {
	m := OutgoingMessage{To: []Address{{Address: "a@x.com"}}, Subject: "s", Body: "b", InternetHeaders: []Header{{"X-Agent-Run", "1"}}}
	f1 := Fingerprint("send", m)
	m2 := m
	m2.InternetHeaders = []Header{{"X-Agent-Run", "2"}}
	if f1 != Fingerprint("send", m2) {
		t.Error("headers must not affect the fingerprint")
	}
	if len(f1) != 64 {
		t.Errorf("fingerprint %q", f1)
	}
	variants := []OutgoingMessage{
		{To: []Address{{Address: "b@x.com"}}, Subject: "s", Body: "b"},
		{To: m.To, Cc: []Address{{Address: "c@x.com"}}, Subject: "s", Body: "b"},
		{To: m.To, Bcc: []Address{{Address: "c@x.com"}}, Subject: "s", Body: "b"},
		{To: m.To, Subject: "s2", Body: "b"},
		{To: m.To, Subject: "s", Body: "b2"},
	}
	seen := map[string]bool{f1: true}
	for i, v := range variants {
		f := Fingerprint("send", v)
		if seen[f] {
			t.Errorf("variant %d collides", i)
		}
		seen[f] = true
	}
	if Fingerprint("reply:1", m) == f1 {
		t.Error("kind must matter")
	}
	// Field boundaries are length-prefixed: shifting bytes between fields differs.
	a := Fingerprint("k", OutgoingMessage{Subject: "ab", Body: "c"})
	b := Fingerprint("k", OutgoingMessage{Subject: "a", Body: "bc"})
	if a == b {
		t.Error("boundary collision")
	}
}
