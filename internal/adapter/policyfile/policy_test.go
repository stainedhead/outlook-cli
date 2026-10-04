package policyfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/domain"
)

const sampleMailbox = "agent-sdlc-reviewer-01@corp.example.com"

func readSample(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "outlook.policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSampleParsesToDefault(t *testing.T) {
	got, err := Parse(readSample(t))
	if err != nil {
		t.Fatal(err)
	}
	want := Default(sampleMailbox)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sample != Default\n got %+v\nwant %+v", got, want)
	}
}

func TestDefaultValues(t *testing.T) {
	p := Default("Bot@Example.org")
	if p.Mailbox != "bot@example.org" || !reflect.DeepEqual(p.InternalDomains, []string{"example.org"}) {
		t.Fatalf("mailbox/domains: %+v", p)
	}
	if p.Send.Recipients.External != domain.ExternalDeny || p.Send.Recipients.BccAllowed ||
		p.Send.ReplyAll || p.Send.Attachments || p.Read.Attachments.Download {
		t.Fatalf("default must deny: %+v", p)
	}
	if p.Send.Mode != domain.SendAllow || p.Send.Rate.PerHour != 20 || p.Send.Rate.PerDay != 100 {
		t.Fatalf("send: %+v", p.Send)
	}
	if p.Send.BodyMaxBytes != 20000 || p.Read.MaxBodyBytes != 16000 || p.Limits.MaxResults != 100 || p.Limits.MaxWritesPerRun != 20 {
		t.Fatalf("bounds: %+v", p)
	}
}

func TestDefaultWithoutAtSign(t *testing.T) {
	p := Default("nodomain")
	if len(p.InternalDomains) != 0 {
		t.Fatalf("got %v", p.InternalDomains)
	}
}

// valid is a minimal valid policy used as a base for mutation tests.
const valid = `
profile: agent
mailbox: a@corp.example.com
internal_domains: [corp.example.com]
audit: { path: /var/log/o.jsonl }
`

func TestMinimalPolicyFailsClosed(t *testing.T) {
	p, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if p.Send.Mode != domain.SendDeny || p.Send.Recipients.External != domain.ExternalDeny {
		t.Fatalf("omitted send must deny: %+v", p.Send)
	}
	if len(p.Read.Folders) != 0 || p.Read.Attachments.Download {
		t.Fatalf("omitted read must be empty/deny: %+v", p.Read)
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		name, yaml, want string
	}{
		{"empty", "", "empty"},
		{"whitespace only", "  \n\n", "empty"},
		{"comment only", "# nothing\n", "empty"},
		{"unknown top key", valid + "extra: 1\n", "extra"},
		{"unknown nested key", valid + "read: { folderz: [inbox] }\n", "folderz"},
		{"duplicate key", valid + "profile: other\n", "duplicate"},
		{"two documents", valid + "---\n" + valid, "document"},
		{"not yaml", "mailbox: [", ""},
		{"scalar root", "hello", ""},
		{"missing profile", strings.Replace(valid, "profile: agent\n", "", 1), "profile"},
		{"missing mailbox", strings.Replace(valid, "mailbox: a@corp.example.com\n", "", 1), "mailbox"},
		{"bad mailbox", strings.Replace(valid, "a@corp.example.com", "not-an-address", 1), "mailbox"},
		{"mailbox with space", strings.Replace(valid, "a@corp.example.com", "a b@corp.example.com", 1), "mailbox"},
		{"missing internal domains", strings.Replace(valid, "internal_domains: [corp.example.com]\n", "", 1), "internal_domains"},
		{"bad internal domain", strings.Replace(valid, "[corp.example.com]", "[\"@corp.example.com\"]", 1), "internal_domains"},
		{"missing audit path", strings.Replace(valid, "audit: { path: /var/log/o.jsonl }\n", "", 1), "audit.path"},
		{"relative audit path", strings.Replace(valid, "/var/log/o.jsonl", "o.jsonl", 1), "audit.path"},
		{"bad send mode", valid + "send: { mode: yes }\n", "send.mode"},
		{"bad external", valid + "send: { recipients: { external: maybe } }\n", "external"},
		{"bad bcc", valid + "send: { recipients: { bcc: sometimes } }\n", "bcc"},
		{"bad reply_all", valid + "send: { reply_all: 1 }\n", "reply_all"},
		{"bad send attachments", valid + "send: { attachments: perhaps }\n", "send.attachments"},
		{"negative max_total", valid + "send: { recipients: { max_total: -1 } }\n", "max_total"},
		{"allow without max_total", valid + "send: { mode: allow }\n", "max_total"},
		{"dry_run_only without max_total", valid + "send: { mode: dry_run_only }\n", "max_total"},
		{"bad allow domain", valid + "send: { recipients: { allow_domains: [\"x y\"] } }\n", "allow_domains"},
		{"bad allow address", valid + "send: { recipients: { allow_addresses: [nope] } }\n", "allow_addresses"},
		{"unknown filter", valid + "send: { content_filters: [bogus] }\n", "bogus"},
		{"duplicate filter", valid + "send: { content_filters: [secret_patterns, secret_patterns] }\n", "duplicate"},
		{"negative rate", valid + "send: { rate: { per_hour: -1 } }\n", "per_hour"},
		{"negative per_day", valid + "send: { rate: { per_day: -1 } }\n", "per_day"},
		{"negative body", valid + "send: { body: { max_bytes: -1 } }\n", "body.max_bytes"},
		{"negative read body", valid + "read: { max_body_bytes: -1 }\n", "max_body_bytes"},
		{"empty folder", valid + "read: { folders: [\" \"] }\n", "folders"},
		{"negative max_results", valid + "limits: { max_results: -1 }\n", "max_results"},
		{"negative writes", valid + "limits: { max_writes_per_run: -1 }\n", "max_writes_per_run"},
		{"bad download value", valid + "read: { attachments: { download: maybe } }\n", "download"},
		{"download deny with types", valid + "read: { attachments: { download: deny, allow_types: [pdf], max_bytes: 1, out_dir: /q } }\n", "download"},
		{"allow_types without max_bytes", valid + "read: { attachments: { allow_types: [pdf], out_dir: /q } }\n", "max_bytes"},
		{"allow_types without out_dir", valid + "read: { attachments: { allow_types: [pdf], max_bytes: 1 } }\n", "out_dir"},
		{"relative out_dir", valid + "read: { attachments: { allow_types: [pdf], max_bytes: 1, out_dir: q } }\n", "out_dir"},
		{"dotted type", valid + "read: { attachments: { allow_types: [\".pdf\"], max_bytes: 1, out_dir: /q } }\n", "allow_types"},
		{"download allow without types", valid + "read: { attachments: { download: allow, max_bytes: 1, out_dir: /q } }\n", "allow_types"},
		{"deny with limits", valid + "read: { attachments: { download: deny, max_bytes: 5 } }\n", "download"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml))
			if err == nil {
				t.Fatal("expected error")
			}
			var de *domain.Error
			if !errors.As(err, &de) || de.Cat != output.CategoryValidation {
				t.Fatalf("want validation domain error, got %T %v", err, err)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(c.want)) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestParseAttachmentsAllow(t *testing.T) {
	y := valid + "read: { attachments: { allow_types: [PDF, txt], max_bytes: 2000000, out_dir: /var/agent/quarantine } }\n"
	p, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	a := p.Read.Attachments
	if !a.Download || !reflect.DeepEqual(a.AllowTypes, []string{"pdf", "txt"}) || a.MaxBytes != 2000000 || a.OutDir != "/var/agent/quarantine" {
		t.Fatalf("%+v", a)
	}
	y = valid + "read: { attachments: { download: allow, allow_types: [pdf], max_bytes: 1, out_dir: /q } }\n"
	if p, err = Parse([]byte(y)); err != nil || !p.Read.Attachments.Download {
		t.Fatalf("explicit allow: %v %+v", err, p)
	}
}

func TestParseNormalises(t *testing.T) {
	y := `
profile: agent
mailbox: " Agent@Corp.Example.COM "
internal_domains: [Corp.Example.COM]
read: { folders: [" Inbox "] }
send:
  mode: dry_run_only
  recipients:
    allow_domains: [Partner.Example]
    allow_addresses: [Boss@Partner.Example]
    external: draft_only
    max_total: 3
    bcc: allow
  reply_all: allow
  attachments: allow
  content_filters: [classification_markers]
audit: { path: /a/b.jsonl }
`
	p, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	s := p.Send
	if p.Mailbox != "agent@corp.example.com" || p.InternalDomains[0] != "corp.example.com" ||
		p.Read.Folders[0] != "Inbox" || s.Mode != domain.SendDryRunOnly ||
		s.Recipients.AllowDomains[0] != "partner.example" || s.Recipients.AllowAddresses[0] != "boss@partner.example" ||
		s.Recipients.External != domain.ExternalDraftOnly || !s.Recipients.BccAllowed || !s.ReplyAll || !s.Attachments ||
		!reflect.DeepEqual(s.ContentFilters, []string{"classification_markers"}) {
		t.Fatalf("%+v", p)
	}
}

func TestErrorsDoNotEchoValues(t *testing.T) {
	_, err := Parse([]byte(valid + "send: { recipients: { external: s3cr3t-value } }\n"))
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---- file loading -------------------------------------------------------

func writeFile(t *testing.T, mode os.FileMode, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "outlook.policy.yaml")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingAndDirectoryAndEmptyAndHuge(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), AllowUntrusted()); err == nil {
		t.Fatal("missing file must fail")
	}
	if _, err := Load(t.TempDir(), AllowUntrusted()); err == nil {
		t.Fatal("directory must fail")
	}
	if _, err := Load(writeFile(t, 0o600, ""), AllowUntrusted()); err == nil {
		t.Fatal("empty file must fail")
	}
	if _, err := Load(writeFile(t, 0o600, strings.Repeat("#", maxPolicyBytes+1)), AllowUntrusted()); err == nil {
		t.Fatal("huge file must fail")
	}
}

func TestProvider(t *testing.T) {
	p := writeFile(t, 0o444, valid)
	pr := NewProvider(p, AllowUntrusted())
	a, err := pr.Policy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Result is cached and immutable: a second call returns equal data even if
	// the file is replaced.
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("garbage: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := pr.Policy(context.Background())
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("cache: %v", err)
	}
	// Mutating the returned slices must not affect later calls.
	a.Read.Folders = append(a.Read.Folders, "x")
	a.InternalDomains[0] = "evil.example"
	c, _ := pr.Policy(context.Background())
	if c.InternalDomains[0] != "corp.example.com" {
		t.Fatalf("policy aliasing: %+v", c)
	}
}

func TestProviderCachesFailure(t *testing.T) {
	pr := NewProvider(filepath.Join(t.TempDir(), "missing.yaml"))
	if _, err := pr.Policy(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := pr.Policy(context.Background()); err == nil {
		t.Fatal("expected cached error")
	}
}

func TestProviderContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewProvider("/nope").Policy(ctx); err == nil {
		t.Fatal("expected ctx error")
	}
}

const minimalPolicy = `profile: agent
mailbox: bot@corp.example.com
internal_domains: [corp.example.com]
audit: { path: /var/log/agent-cli/outlook.audit.jsonl }
`

func TestOmittedBoundsGetSafeDefaults(t *testing.T) {
	p, err := Parse([]byte(minimalPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if p.Limits.MaxWritesPerRun != DefaultMaxWritesPerRun || p.Read.MaxBodyBytes != DefaultMaxBodyBytes ||
		p.Send.BodyMaxBytes != DefaultSendBodyMaxBytes || p.Limits.MaxResults != DefaultMaxResults {
		t.Fatalf("bounds not defaulted: %+v", p)
	}
	if !p.Read.DefangLinks || !p.Read.HTMLToText {
		t.Fatalf("link defanging and html conversion must default on: %+v", p.Read)
	}
	if p.Send.Mode != domain.SendDeny {
		t.Fatalf("send must default to deny: %v", p.Send.Mode)
	}
}

func TestExplicitFalseAndZeroAreHonoured(t *testing.T) {
	p, err := Parse([]byte(minimalPolicy + "read: { defang_links: false, html_to_text: false }\nlimits: { max_writes_per_run: 0 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Read.DefangLinks || p.Read.HTMLToText || p.Limits.MaxWritesPerRun != 0 {
		t.Fatalf("explicit values overridden: %+v %+v", p.Read, p.Limits)
	}
}
