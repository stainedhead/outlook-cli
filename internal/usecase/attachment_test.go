package usecase

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

func attEnv(t *testing.T, mut ...func(*domain.Policy)) *env {
	e := newEnv(t, mut...)
	e.addMessage("m1", "f-inbox")
	e.r.attach["m1"] = []domain.Attachment{
		{ID: "a-pdf", Name: "re\u202eport.pdf", ContentType: "application/pdf", Size: 20},
		{ID: "a-exe", Name: "setup.exe", Size: 20},
		{ID: "a-big", Name: "big.pdf", Size: 5000},
		{ID: "a-txt", Name: "NOTES.TXT", Size: 10},
	}
	e.r.attData["a-pdf"] = "%PDF-1.4 fake"
	e.r.attData["a-exe"] = "MZ"
	e.r.attData["a-big"] = "xxxx"
	return e
}

func TestListAttachmentsMarksDownloadable(t *testing.T) {
	e := attEnv(t)
	got, err := e.cmds.ListAttachments(context.Background(), "m1")
	if err != nil || len(got) != 4 {
		t.Fatalf("got %+v err %v", got, err)
	}
	want := map[string]bool{"a-pdf": true, "a-exe": false, "a-big": false, "a-txt": true}
	for _, a := range got {
		if a.Downloadable != want[a.ID] {
			t.Errorf("%s downloadable = %v", a.ID, a.Downloadable)
		}
	}
	if got[0].Name != "report.pdf" {
		t.Errorf("name not cleaned: %q", got[0].Name)
	}
	if e.lastAudit(t).Resource != "attachment.list" {
		t.Error("audit")
	}
}

func TestListAttachmentsDefaultNothingDownloadable(t *testing.T) {
	e := attEnv(t, func(p *domain.Policy) { p.Read.Attachments = domain.AttachmentPolicy{} })
	got, _ := e.cmds.ListAttachments(context.Background(), "m1")
	for _, a := range got {
		if a.Downloadable {
			t.Errorf("%s downloadable by default", a.ID)
		}
	}
}

func TestListAttachmentsErrors(t *testing.T) {
	e := attEnv(t)
	e.addMessage("arch", "f-arch")
	ctx := context.Background()
	if _, err := e.cmds.ListAttachments(ctx, "arch"); exitOf(err) != 6 {
		t.Error("forbidden folder")
	}
	if _, err := e.cmds.ListAttachments(ctx, "zzz"); exitOf(err) != 5 {
		t.Error("missing")
	}
	e.r.listAttErr = domain.NewGeneral("boom")
	if _, err := e.cmds.ListAttachments(ctx, "m1"); err == nil {
		t.Error("adapter error swallowed")
	}
}

func TestGetAttachmentQuarantines(t *testing.T) {
	e := attEnv(t)
	res, err := e.cmds.GetAttachment(context.Background(), AttachmentRequest{MessageID: "m1", AttachmentID: "a-pdf", OutDir: "/var/q/run1"})
	if err != nil {
		t.Fatal(err)
	}
	if e.q.gotDir != "/var/q/run1" || e.q.gotName != "report.pdf" || e.q.gotMax != 100 || e.q.content != "%PDF-1.4 fake" {
		t.Errorf("save args dir=%q name=%q max=%d content=%q", e.q.gotDir, e.q.gotName, e.q.gotMax, e.q.content)
	}
	if res.Path != "/var/q/run1/report.pdf" || res.Written != 13 || !res.Attachment.Downloadable || res.Attachment.Name != "report.pdf" {
		t.Errorf("res = %+v", res)
	}
	if e.r.opened != 1 || e.r.closed != 1 {
		t.Errorf("opened=%d closed=%d", e.r.opened, e.r.closed)
	}
	if a := e.lastAudit(t); a.Resource != "attachment.get" || a.Outcome != "ok" {
		t.Errorf("audit = %+v", a)
	}
	// Default out dir.
	if _, err = e.cmds.GetAttachment(context.Background(), AttachmentRequest{MessageID: "m1", AttachmentID: "a-txt"}); err != nil || e.q.gotDir != "/var/q" {
		t.Errorf("default dir %q err %v", e.q.gotDir, err)
	}
}

func TestGetAttachmentOffByDefault(t *testing.T) {
	e := attEnv(t, func(p *domain.Policy) { p.Read.Attachments = domain.AttachmentPolicy{} })
	_, err := e.cmds.GetAttachment(context.Background(), AttachmentRequest{MessageID: "m1", AttachmentID: "a-pdf", OutDir: "/var/q"})
	mustCat(t, err, 6)
	if ruleOf(err) != domain.RuleAttachmentDownload || e.r.opened != 0 || e.q.calls != 0 {
		t.Errorf("rule %q opened %d", ruleOf(err), e.r.opened)
	}
}

func TestGetAttachmentNoQuarantineStore(t *testing.T) {
	e := attEnv(t)
	e.deps.Quarantine = nil
	e.rebuild()
	_, err := e.cmds.GetAttachment(context.Background(), AttachmentRequest{MessageID: "m1", AttachmentID: "a-pdf"})
	mustCat(t, err, 6)
}

func TestGetAttachmentDenials(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		req  AttachmentRequest
		exit int
		rule string
	}{
		{"type not allowed", AttachmentRequest{MessageID: "m1", AttachmentID: "a-exe"}, 6, domain.RuleAttachmentType},
		{"too big", AttachmentRequest{MessageID: "m1", AttachmentID: "a-big"}, 6, domain.RuleAttachmentSize},
		{"out dir escape", AttachmentRequest{MessageID: "m1", AttachmentID: "a-pdf", OutDir: "/var/q/../etc"}, 6, domain.RuleAttachmentOutDir},
		{"out dir elsewhere", AttachmentRequest{MessageID: "m1", AttachmentID: "a-pdf", OutDir: "/tmp"}, 6, domain.RuleAttachmentOutDir},
		{"relative out dir", AttachmentRequest{MessageID: "m1", AttachmentID: "a-pdf", OutDir: "q"}, 6, domain.RuleAttachmentOutDir},
		{"unknown attachment", AttachmentRequest{MessageID: "m1", AttachmentID: "zzz"}, 5, ""},
		{"no attachment id", AttachmentRequest{MessageID: "m1"}, 2, ""},
		{"unknown message", AttachmentRequest{MessageID: "zzz", AttachmentID: "a-pdf"}, 5, ""},
	}
	for _, c := range cases {
		e := attEnv(t)
		_, err := e.cmds.GetAttachment(ctx, c.req)
		if exitOf(err) != c.exit || (c.rule != "" && ruleOf(err) != c.rule) || e.q.calls != 0 || e.r.opened != 0 {
			t.Errorf("%s: exit %d rule %q err %v opened %d", c.name, exitOf(err), ruleOf(err), err, e.r.opened)
		}
	}
	e := attEnv(t, func(p *domain.Policy) { p.Read.Attachments.OutDir = "" })
	if _, err := e.cmds.GetAttachment(ctx, AttachmentRequest{MessageID: "m1", AttachmentID: "a-pdf"}); exitOf(err) != 6 {
		t.Error("no out_dir configured")
	}
}

func TestGetAttachmentForbiddenFolderMessage(t *testing.T) {
	e := attEnv(t)
	e.addMessage("arch", "f-arch")
	e.r.attach["arch"] = []domain.Attachment{{ID: "x", Name: "a.pdf", Size: 1}}
	if _, err := e.cmds.GetAttachment(context.Background(), AttachmentRequest{MessageID: "arch", AttachmentID: "x"}); exitOf(err) != 6 {
		t.Error("attachment of unreadable message")
	}
}

func TestGetAttachmentAdapterErrorsAndMetadataRecheck(t *testing.T) {
	ctx := context.Background()
	req := AttachmentRequest{MessageID: "m1", AttachmentID: "a-pdf"}
	e := attEnv(t)
	e.r.listAttErr = errors.New("boom")
	if _, err := e.cmds.GetAttachment(ctx, req); err == nil {
		t.Error("list error swallowed")
	}
	e = attEnv(t)
	e.r.openErr = domain.NewGeneral("boom")
	if _, err := e.cmds.GetAttachment(ctx, req); err == nil {
		t.Error("open error swallowed")
	}
	e = attEnv(t)
	e.q.err = domain.NewGeneral("exceeded")
	if _, err := e.cmds.GetAttachment(ctx, req); err == nil || e.r.closed != 1 {
		t.Errorf("save error: %v closed=%d", err, e.r.closed)
	}
	// The adapter's declared metadata at open time is re-checked: a lying list.
	e = attEnv(t)
	e.deps.Reader = &swapReader{fakeReader: e.r}
	e.rebuild()
	_, err := e.cmds.GetAttachment(ctx, req)
	mustCat(t, err, 6)
	if e.q.calls != 0 {
		t.Error("saved despite failing the re-check")
	}
}

// swapReader lists a small attachment but opens a huge one.
type swapReader struct{ *fakeReader }

func (s *swapReader) OpenAttachment(ctx context.Context, m, a string) (domain.Attachment, io.ReadCloser, error) {
	att, rc, err := s.fakeReader.OpenAttachment(ctx, m, a)
	att.Size = 1 << 30
	return att, rc, err
}
