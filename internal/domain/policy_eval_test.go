package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
)

func sendPolicy() Policy {
	return Policy{
		Mailbox:         "agent@corp.example.com",
		InternalDomains: []string{"corp.example.com"},
		Read:            ReadPolicy{Folders: []string{"inbox", "Processed"}},
		Send: SendPolicy{
			Mode: SendAllow,
			Recipients: RecipientPolicy{
				AllowDomains: []string{"corp.example.com"}, AllowAddresses: []string{"ext@partner.com"},
				External: ExternalDeny, MaxTotal: 3,
			},
			BodyMaxBytes: 50,
			Rate:         SendRate{PerHour: 2, PerDay: 3},
		},
		Limits: Limits{MaxResults: 10, MaxWritesPerRun: 2},
	}
}

func recips(to ...string) Recipients {
	r, err := NormalizeRecipients(to, nil, nil)
	if err != nil {
		panic(err)
	}
	return r
}

func TestErrFor(t *testing.T) {
	if ErrFor(Decision{Mode: DecisionAllow}) != nil {
		t.Error("allow must be nil")
	}
	e := ErrFor(Decision{Mode: DecisionDeny, RuleID: RuleSendBcc, Reason: "x"})
	if output.ExitOf(e) != 6 {
		t.Errorf("deny exit = %d", output.ExitOf(e))
	}
	e = ErrFor(Decision{Mode: DecisionDeny, RuleID: RuleSendRateHour, Reason: "x", RetryAfter: time.Hour})
	var de *Error
	if !errors.As(e, &de) || output.ExitOf(e) != 8 || de.RuleID != RuleSendRateHour || de.HintMsg == "" {
		t.Errorf("rate err = %+v exit %d", de, output.ExitOf(e))
	}
	e = ErrFor(Decision{Mode: DecisionDeny, RuleID: RuleSendRateDay, Reason: "x"})
	if output.ExitOf(e) != 8 || e.(*Error).HintMsg != "" {
		t.Error("rate day without retry-after")
	}
	if output.ExitOf(ErrFor(Decision{Mode: DecisionDraftOnly, RuleID: "r"})) != 6 {
		t.Error("draft_only as error must be policy denied")
	}
}

func TestEvalMailbox(t *testing.T) {
	p := sendPolicy()
	cases := []struct {
		name string
		p    Policy
		pr   Profile
		ok   bool
	}{
		{"mail", p, Profile{Mail: "Agent@corp.example.com"}, true},
		{"upn", p, Profile{UserPrincipalName: "agent@corp.example.com"}, true},
		{"mismatch", p, Profile{Mail: "other@corp.example.com", UserPrincipalName: "other@corp.example.com"}, false},
		{"empty profile", p, Profile{}, false},
		{"empty policy", Policy{}, Profile{Mail: "agent@corp.example.com"}, false},
	}
	for _, c := range cases {
		d := c.p.EvalMailbox(c.pr)
		if d.Allowed() != c.ok || (!c.ok && d.RuleID != RuleMailbox) {
			t.Errorf("%s: %+v", c.name, d)
		}
	}
}

func TestEffectiveBounds(t *testing.T) {
	p := sendPolicy()
	if p.EffectiveMaxResults(0) != 10 || p.EffectiveMaxResults(99) != 10 || p.EffectiveMaxResults(3) != 3 {
		t.Error("max results clamp")
	}
	if (Policy{}).EffectiveMaxResults(0) != DefaultMaxResults {
		t.Error("default max results")
	}
	p.Read.MaxBodyBytes = 100
	if p.EffectiveBodyBytes(0) != 100 || p.EffectiveBodyBytes(500) != 100 || p.EffectiveBodyBytes(40) != 40 {
		t.Error("body bytes can only be lowered")
	}
	if (Policy{}).EffectiveBodyBytes(0) != DefaultMaxBodyBytes {
		t.Error("default body bytes")
	}
}

func TestFolderRules(t *testing.T) {
	p := sendPolicy()
	if !p.FolderAllowed(Folder{Name: "Inbox", WellKnown: WellKnownInbox}) {
		t.Error("inbox by alias")
	}
	if !p.FolderAllowed(Folder{Name: "processed"}) {
		t.Error("Processed by name, case-insensitive")
	}
	if p.FolderAllowed(Folder{Name: "Archive", WellKnown: WellKnownArchive}) {
		t.Error("archive allowed")
	}
	if p.FolderAllowed(Folder{Name: ""}) {
		t.Error("empty folder allowed")
	}
	pe := Policy{Read: ReadPolicy{Folders: []string{"", " "}}}
	if pe.FolderAllowed(Folder{Name: ""}) {
		t.Error("blank policy entry matched")
	}
	if d := p.EvalReadFolder(Folder{Name: "Secret"}); d.Allowed() || d.RuleID != RuleReadFolders {
		t.Errorf("read folder: %+v", d)
	}
	if d := p.EvalReadFolder(Folder{Name: "Inbox", WellKnown: WellKnownInbox}); !d.Allowed() {
		t.Errorf("read inbox: %+v", d)
	}
}

func TestEvalMove(t *testing.T) {
	p := sendPolicy()
	p.Read.Folders = append(p.Read.Folders, "deleteditems", "Deleted Items")
	cases := []struct {
		name string
		f    Folder
		rule string
	}{
		{"processed ok", Folder{Name: "Processed"}, ""},
		{"deleted wellknown", Folder{Name: "x", WellKnown: WellKnownDeletedItems}, RuleMoveDeletedItems},
		{"deleted by name even if listed", Folder{Name: "Deleted Items"}, RuleMoveDeletedItems},
		{"not listed", Folder{Name: "Other"}, RuleMoveFolder},
	}
	for _, c := range cases {
		d := p.EvalMove(c.f)
		if c.rule == "" && !d.Allowed() || c.rule != "" && d.RuleID != c.rule {
			t.Errorf("%s: %+v", c.name, d)
		}
	}
}

func TestEvalWrites(t *testing.T) {
	p := sendPolicy()
	if !p.EvalWrites(1).Allowed() || p.EvalWrites(2).Allowed() || p.EvalWrites(2).RuleID != RuleWritesPerRun {
		t.Error("write cap")
	}
	if (Policy{}).EvalWrites(0).Allowed() {
		t.Error("zero policy must deny writes")
	}
}

func TestEvalSend(t *testing.T) {
	cases := []struct {
		name   string
		mut    func(*Policy)
		in     SendInput
		mode   DecisionMode
		ruleID string
	}{
		{"internal allowed", nil, SendInput{Recipients: recips("a@corp.example.com"), Body: "hi"}, DecisionAllow, ""},
		{"zero policy denies", func(p *Policy) { *p = Policy{} }, SendInput{Recipients: recips("a@corp.example.com")}, DecisionDeny, RuleSendMode},
		{"mode deny", func(p *Policy) { p.Send.Mode = SendDeny }, SendInput{Recipients: recips("a@corp.example.com")}, DecisionDeny, RuleSendMode},
		{"mode junk", func(p *Policy) { p.Send.Mode = "weird" }, SendInput{Recipients: recips("a@corp.example.com")}, DecisionDeny, RuleSendMode},
		{"dry_run_only", func(p *Policy) { p.Send.Mode = SendDryRunOnly }, SendInput{Recipients: recips("a@corp.example.com")}, DecisionDryRunOnly, RuleSendMode},
		{"dry_run_only still denies bad recipient", func(p *Policy) { p.Send.Mode = SendDryRunOnly }, SendInput{Recipients: recips("a@evil.com")}, DecisionDeny, RuleSendExternal},
		{"attachments denied", nil, SendInput{Recipients: recips("a@corp.example.com"), HasAttachments: true}, DecisionDeny, RuleSendAttachments},
		{"attachments allowed by policy", func(p *Policy) { p.Send.Attachments = true }, SendInput{Recipients: recips("a@corp.example.com"), HasAttachments: true}, DecisionAllow, ""},
		{"bcc denied", nil, SendInput{Recipients: recips("a@corp.example.com"), BccRequested: true}, DecisionDeny, RuleSendBcc},
		{"bcc allowed", func(p *Policy) { p.Send.Recipients.BccAllowed = true }, SendInput{Recipients: recips("a@corp.example.com"), BccRequested: true}, DecisionAllow, ""},
		{"body too big", nil, SendInput{Recipients: recips("a@corp.example.com"), Body: string(make([]byte, 51))}, DecisionDeny, RuleSendBodyMax},
		{"body default limit", func(p *Policy) { p.Send.BodyMaxBytes = 0 }, SendInput{Recipients: recips("a@corp.example.com"), Body: string(make([]byte, 20001))}, DecisionDeny, RuleSendBodyMax},
		{"external denied", nil, SendInput{Recipients: recips("a@evil.com")}, DecisionDeny, RuleSendExternal},
		{"external zero mode denied", func(p *Policy) { p.Send.Recipients.External = "" }, SendInput{Recipients: recips("a@evil.com")}, DecisionDeny, RuleSendExternal},
		{"external allowed", func(p *Policy) { p.Send.Recipients.External = ExternalAllow }, SendInput{Recipients: recips("a@evil.com")}, DecisionAllow, ""},
		{"external draft_only", func(p *Policy) { p.Send.Recipients.External = ExternalDraftOnly }, SendInput{Recipients: recips("a@evil.com", "b@corp.example.com")}, DecisionDraftOnly, RuleSendExternal},
		{"allow_addresses external", nil, SendInput{Recipients: recips("ext@partner.com")}, DecisionAllow, ""},
		{"internal domain not allow-listed", func(p *Policy) { p.Send.Recipients.AllowDomains = nil }, SendInput{Recipients: recips("a@corp.example.com")}, DecisionDeny, RuleSendAllowlist},
		{"max total", nil, SendInput{Recipients: recips("a@corp.example.com", "b@corp.example.com", "c@corp.example.com", "d@corp.example.com")}, DecisionDeny, RuleSendMaxTotal},
		{"max total zero", func(p *Policy) { p.Send.Recipients.MaxTotal = 0 }, SendInput{Recipients: recips("a@corp.example.com")}, DecisionDeny, RuleSendMaxTotal},
	}
	for _, c := range cases {
		p := sendPolicy()
		if c.mut != nil {
			c.mut(&p)
		}
		d := p.EvalSend(c.in)
		if d.Mode != c.mode || d.RuleID != c.ruleID {
			t.Errorf("%s: got %+v, want %s/%s", c.name, d, c.mode, c.ruleID)
		}
	}
}

func TestEvalSendDedupeBeforeMaxTotal(t *testing.T) {
	p := sendPolicy()
	p.Send.Recipients.MaxTotal = 2
	r, _ := NormalizeRecipients([]string{"a@corp.example.com", "A@corp.example.com"}, []string{"a@corp.example.com", "b@corp.example.com"}, nil)
	if d := p.EvalSend(SendInput{Recipients: r}); !d.Allowed() {
		t.Errorf("deduped total is 2: %+v", d)
	}
}

func TestEvalReplyAll(t *testing.T) {
	p := sendPolicy()
	if !p.EvalReplyAll(false).Allowed() {
		t.Error("plain reply")
	}
	if d := p.EvalReplyAll(true); d.Allowed() || d.RuleID != RuleSendReplyAll {
		t.Errorf("reply-all: %+v", d)
	}
	p.Send.ReplyAll = true
	if !p.EvalReplyAll(true).Allowed() {
		t.Error("reply-all enabled")
	}
}

func TestEvalRate(t *testing.T) {
	p := sendPolicy()
	if !p.EvalRate(1, 1).Allowed() {
		t.Error("under caps")
	}
	d := p.EvalRate(2, 2)
	if d.Allowed() || d.RuleID != RuleSendRateHour || d.RetryAfter != time.Hour {
		t.Errorf("hour: %+v", d)
	}
	d = p.EvalRate(0, 3)
	if d.Allowed() || d.RuleID != RuleSendRateDay || d.RetryAfter != 24*time.Hour {
		t.Errorf("day: %+v", d)
	}
	if !(Policy{}).EvalRate(1000, 1000).Allowed() {
		t.Error("zero caps mean no limit")
	}
}

func TestEvalAttachmentDownload(t *testing.T) {
	p := Policy{}
	if d := p.EvalAttachmentDownload(Attachment{Name: "a.pdf", Size: 1}); d.Allowed() || d.RuleID != RuleAttachmentDownload {
		t.Errorf("default deny: %+v", d)
	}
	p.Read.Attachments = AttachmentPolicy{Download: true, AllowTypes: []string{"pdf", ".TXT"}, MaxBytes: 100, OutDir: "/q"}
	cases := []struct {
		a    Attachment
		rule string
	}{
		{Attachment{Name: "a.pdf", Size: 100}, ""},
		{Attachment{Name: "A.PDF", Size: 1}, ""},
		{Attachment{Name: "n.txt", Size: 1}, ""},
		{Attachment{Name: "a.pdf", Size: 101}, RuleAttachmentSize},
		{Attachment{Name: "a.exe", Size: 1}, RuleAttachmentType},
		{Attachment{Name: "a.pdf.exe", Size: 1}, RuleAttachmentType},
		{Attachment{Name: "noext", Size: 1}, RuleAttachmentType},
	}
	for _, c := range cases {
		d := p.EvalAttachmentDownload(c.a)
		if c.rule == "" && !d.Allowed() || c.rule != "" && d.RuleID != c.rule {
			t.Errorf("%+v: %+v", c.a, d)
		}
	}
	p.Read.Attachments.MaxBytes = 0
	if p.EvalAttachmentDownload(Attachment{Name: "a.pdf", Size: 0}).Allowed() {
		t.Error("zero max_bytes must deny")
	}
}

func TestResolveOutDir(t *testing.T) {
	p := Policy{Read: ReadPolicy{Attachments: AttachmentPolicy{OutDir: "/var/q/"}}}
	cases := []struct {
		req  string
		want string
		ok   bool
	}{
		{"", "/var/q", true},
		{"/var/q", "/var/q", true},
		{"/var/q/sub/dir", "/var/q/sub/dir", true},
		{"/var/q/../etc", "", false},
		{"/var/q/sub/../../etc", "", false},
		{"/var/qq", "", false},
		{"/etc", "", false},
		{"relative", "", false},
		{"/var/q/..", "", false},
	}
	for _, c := range cases {
		got, d := p.ResolveOutDir(c.req)
		if d.Allowed() != c.ok || got != c.want {
			t.Errorf("ResolveOutDir(%q) = %q, %+v", c.req, got, d)
		}
	}
	if _, d := (Policy{}).ResolveOutDir(""); d.Allowed() || d.RuleID != RuleAttachmentOutDir {
		t.Error("no out_dir must deny")
	}
	rel := Policy{Read: ReadPolicy{Attachments: AttachmentPolicy{OutDir: "relative/q"}}}
	if _, d := rel.ResolveOutDir(""); d.Allowed() {
		t.Error("relative out_dir must deny")
	}
}
