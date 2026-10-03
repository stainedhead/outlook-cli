package domain

import (
	"strings"
	"testing"
)

func TestParseAddress(t *testing.T) {
	good := map[string]string{
		"Jane@Corp.Example.com":   "jane@corp.example.com",
		"  a.b+tag@corp.example ": "a.b+tag@corp.example",
	}
	for in, want := range good {
		a, err := ParseAddress(in)
		if err != nil || a.Address != want {
			t.Errorf("ParseAddress(%q) = %v, %v; want %q", in, a, err, want)
		}
	}
	bad := []string{
		"", "   ", "nodomain", "@corp.example", "a@", "a@b@c", "a b@corp.example",
		"a@corp.example\r\nBcc: evil@x.com", "a@corp\x00.example",
		"Jane <jane@corp.example>", "a@x.com,b@x.com", "a@x.com;b@x.com", `"a"@x.com`,
		"a@.x.com", "a@x.com.", "a@x..com", strings.Repeat("a", 250) + "@x.com",
	}
	for _, in := range bad {
		if _, err := ParseAddress(in); err == nil {
			t.Errorf("ParseAddress(%q) accepted", in)
		}
	}
}

func TestNormalizeRecipientsDedupe(t *testing.T) {
	r, err := NormalizeRecipients(
		[]string{"A@x.com", "a@X.com", "b@x.com"},
		[]string{"b@x.com", "c@x.com"},
		[]string{"C@x.com", "d@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.To) != 2 || len(r.Cc) != 1 || len(r.Bcc) != 1 || r.Total() != 4 {
		t.Fatalf("got %+v", r)
	}
	all := r.All()
	if len(all) != 4 || all[0].Address != "a@x.com" || all[3].Address != "d@x.com" {
		t.Fatalf("All() = %+v", all)
	}
}

func TestNormalizeRecipientsErrors(t *testing.T) {
	if _, err := NormalizeRecipients(nil, nil, nil); err == nil {
		t.Error("no recipients accepted")
	}
	if _, err := NormalizeRecipients([]string{"bad"}, nil, nil); err == nil {
		t.Error("bad to accepted")
	}
	if _, err := NormalizeRecipients([]string{"a@x.com"}, []string{"bad"}, nil); err == nil {
		t.Error("bad cc accepted")
	}
	if _, err := NormalizeRecipients([]string{"a@x.com"}, nil, []string{"bad"}); err == nil {
		t.Error("bad bcc accepted")
	}
}

func TestAddressDomain(t *testing.T) {
	for in, want := range map[string]string{"a@Corp.com": "corp.com", "a": "", "a@": "", "": ""} {
		if got := AddressDomain(in); got != want {
			t.Errorf("AddressDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSenderTrustOf(t *testing.T) {
	p := Policy{InternalDomains: []string{"Corp.Example.com"}}
	cases := []struct {
		addr string
		want SenderTrust
	}{
		{"jane@corp.example.com", TrustInternal},
		{"JANE@CORP.EXAMPLE.COM", TrustInternal},
		{"jane@evil.com", TrustExternal},
		{"jane@corp.example.com.evil.com", TrustExternal},
		{"jane@sub.corp.example.com", TrustExternal},
		{"", TrustUnknown},
		{"nodomain", TrustUnknown},
	}
	for _, c := range cases {
		if got := p.SenderTrustOf(Address{Address: c.addr}); got != c.want {
			t.Errorf("SenderTrustOf(%q) = %q, want %q", c.addr, got, c.want)
		}
	}
	if (Policy{}).SenderTrustOf(Address{Address: "a@b.com"}) != TrustExternal {
		t.Error("zero policy must treat everything as external")
	}
}
