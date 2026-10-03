package usecase

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeReader struct {
	profile  domain.Profile
	meErr    error
	folders  []domain.Folder
	listErr  error
	messages map[string]domain.RawMessage
	attach   map[string][]domain.Attachment
	attData  map[string]string
	drafts   []domain.MessageSummary
	pages    map[string]domain.Page[domain.MessageSummary]

	getErr, searchErr, listAttErr, openErr, draftsErr, listMsgErr error

	gotQuery   domain.MessageQuery
	gotSearch  []domain.SearchQuery
	gotHeaders bool
	meCalls    int
	opened     int
	closed     int
}

func (r *fakeReader) Me(context.Context) (domain.Profile, error) {
	r.meCalls++
	return r.profile, r.meErr
}
func (r *fakeReader) ListFolders(context.Context) ([]domain.Folder, error) {
	return r.folders, r.listErr
}
func (r *fakeReader) ResolveFolder(_ context.Context, name string) (domain.Folder, error) {
	for _, f := range r.folders {
		if strings.EqualFold(f.Name, name) || strings.EqualFold(string(f.WellKnown), name) {
			return f, nil
		}
	}
	return domain.Folder{}, domain.NewNotFound("folder not found")
}
func (r *fakeReader) ListMessages(_ context.Context, q domain.MessageQuery) (domain.Page[domain.MessageSummary], error) {
	r.gotQuery = q
	if r.listMsgErr != nil {
		return domain.Page[domain.MessageSummary]{}, r.listMsgErr
	}
	return r.pages["list:"+q.FolderID], nil
}
func (r *fakeReader) SearchMessages(_ context.Context, q domain.SearchQuery) (domain.Page[domain.MessageSummary], error) {
	r.gotSearch = append(r.gotSearch, q)
	if r.searchErr != nil {
		return domain.Page[domain.MessageSummary]{}, r.searchErr
	}
	return r.pages["search:"+q.FolderID], nil
}
func (r *fakeReader) GetMessage(_ context.Context, id string, h bool) (domain.RawMessage, error) {
	r.gotHeaders = h
	if r.getErr != nil {
		return domain.RawMessage{}, r.getErr
	}
	m, ok := r.messages[id]
	if !ok {
		return domain.RawMessage{}, domain.NewNotFound("message not found")
	}
	return m, nil
}
func (r *fakeReader) ListAttachments(_ context.Context, id string) ([]domain.Attachment, error) {
	return r.attach[id], r.listAttErr
}
func (r *fakeReader) OpenAttachment(_ context.Context, mid, aid string) (domain.Attachment, io.ReadCloser, error) {
	if r.openErr != nil {
		return domain.Attachment{}, nil, r.openErr
	}
	for _, a := range r.attach[mid] {
		if a.ID == aid {
			r.opened++
			return a, &trackCloser{Reader: strings.NewReader(r.attData[aid]), r: r}, nil
		}
	}
	return domain.Attachment{}, nil, domain.NewNotFound("attachment")
}
func (r *fakeReader) ListDrafts(context.Context, int, string) (domain.Page[domain.MessageSummary], error) {
	if r.draftsErr != nil {
		return domain.Page[domain.MessageSummary]{}, r.draftsErr
	}
	return domain.Page[domain.MessageSummary]{Items: r.drafts, NextPageToken: "nt"}, nil
}

type trackCloser struct {
	io.Reader
	r *fakeReader
}

func (t *trackCloser) Close() error { t.r.closed++; return nil }

type fakeWriter struct {
	sent     []domain.OutgoingMessage
	drafts   []domain.OutgoingMessage
	replies  []domain.Reply
	sentDraf []string
	deleted  []string
	reads    map[string]bool
	moves    [][2]string

	sendErr, draftErr, sendDraftErr, deleteErr, replyErr, readErr, moveErr error
	newID                                                                  string
}

func (w *fakeWriter) SendMail(_ context.Context, m domain.OutgoingMessage) error {
	if w.sendErr != nil {
		return w.sendErr
	}
	w.sent = append(w.sent, m)
	return nil
}
func (w *fakeWriter) CreateDraft(_ context.Context, m domain.OutgoingMessage) (domain.Draft, error) {
	if w.draftErr != nil {
		return domain.Draft{}, w.draftErr
	}
	w.drafts = append(w.drafts, m)
	return domain.Draft{ID: "draft-1", MessageSummary: domain.MessageSummary{ID: "draft-1", Subject: m.Subject, IsDraft: true}}, nil
}
func (w *fakeWriter) SendDraft(_ context.Context, id string) error {
	if w.sendDraftErr != nil {
		return w.sendDraftErr
	}
	w.sentDraf = append(w.sentDraf, id)
	return nil
}
func (w *fakeWriter) DeleteDraft(_ context.Context, id string) error {
	if w.deleteErr != nil {
		return w.deleteErr
	}
	w.deleted = append(w.deleted, id)
	return nil
}
func (w *fakeWriter) ReplyToSender(_ context.Context, r domain.Reply) error {
	if w.replyErr != nil {
		return w.replyErr
	}
	w.replies = append(w.replies, r)
	return nil
}
func (w *fakeWriter) SetRead(_ context.Context, id string, read bool) error {
	if w.readErr != nil {
		return w.readErr
	}
	if w.reads == nil {
		w.reads = map[string]bool{}
	}
	w.reads[id] = read
	return nil
}
func (w *fakeWriter) MoveMessage(_ context.Context, id, folder string) (string, error) {
	if w.moveErr != nil {
		return "", w.moveErr
	}
	w.moves = append(w.moves, [2]string{id, folder})
	if w.newID == "" {
		return "moved-" + id, nil
	}
	return w.newID, nil
}

func (w *fakeWriter) total() int {
	return len(w.sent) + len(w.drafts) + len(w.replies) + len(w.sentDraf) + len(w.deleted) + len(w.reads) + len(w.moves)
}

type fakeLedger struct {
	mu      sync.Mutex
	entries map[string]*LedgerEntry
	order   []string

	reserveErr, completeErr, failErr, sentErr error
}

func newLedger() *fakeLedger { return &fakeLedger{entries: map[string]*LedgerEntry{}} }

func (l *fakeLedger) Reserve(_ context.Context, key, fp string, at time.Time) (LedgerEntry, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reserveErr != nil {
		return LedgerEntry{}, false, l.reserveErr
	}
	if e, ok := l.entries[key]; ok {
		if e.Status == LedgerFailed {
			e.Status, e.Fingerprint, e.At = LedgerPending, fp, at
			return *e, true, nil
		}
		return *e, false, nil
	}
	e := &LedgerEntry{Key: key, Fingerprint: fp, Status: LedgerPending, At: at}
	l.entries[key] = e
	l.order = append(l.order, key)
	return *e, true, nil
}
func (l *fakeLedger) ReserveWithin(ctx context.Context, key, fp string, kind LedgerKind, at time.Time, windows []RateWindow) (LedgerEntry, bool, []int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reserveErr != nil {
		return LedgerEntry{}, false, nil, l.reserveErr
	}
	e, exists := l.entries[key]
	if exists && e.Status != LedgerFailed {
		return *e, false, nil, nil
	}
	var counts []int
	if kind == LedgerKindSend {
		over := false
		for _, w := range windows {
			n := 0
			for k, o := range l.entries {
				if k != key && o.Kind != LedgerKindDraft && (o.Status == LedgerSent || o.Status == LedgerPending) && !o.At.Before(w.Since) {
					n++
				}
			}
			counts = append(counts, n)
			if w.Cap > 0 && n >= w.Cap {
				over = true
			}
		}
		if over {
			return LedgerEntry{Key: key, Status: LedgerOverCap}, false, counts, nil
		}
	}
	if exists {
		e.Status, e.Fingerprint, e.At, e.Kind = LedgerPending, fp, at, kind
		return *e, true, counts, nil
	}
	ne := &LedgerEntry{Key: key, Fingerprint: fp, Status: LedgerPending, Kind: kind, At: at}
	l.entries[key] = ne
	l.order = append(l.order, key)
	return *ne, true, counts, nil
}
func (l *fakeLedger) Complete(_ context.Context, key, ref string, at time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.completeErr != nil {
		return l.completeErr
	}
	e := l.entries[key]
	e.Status, e.RefID, e.At = LedgerSent, ref, at
	return nil
}
func (l *fakeLedger) Fail(_ context.Context, key string, at time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failErr != nil {
		return l.failErr
	}
	e := l.entries[key]
	e.Status, e.At = LedgerFailed, at
	return nil
}
func (l *fakeLedger) SentSince(_ context.Context, since time.Time) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sentErr != nil {
		return 0, l.sentErr
	}
	n := 0
	for _, e := range l.entries {
		if e.Status == LedgerSent && !e.At.Before(since) {
			n++
		}
	}
	return n, nil
}
func (l *fakeLedger) status(key string) LedgerStatus {
	if e, ok := l.entries[key]; ok {
		return e.Status
	}
	return ""
}

type fakeQuarantine struct {
	gotDir, gotName string
	gotMax          int64
	content         string
	err             error
	calls           int
}

func (q *fakeQuarantine) Save(_ context.Context, dir, name string, r io.Reader, max int64) (string, int64, error) {
	q.calls++
	q.gotDir, q.gotName, q.gotMax = dir, name, max
	if q.err != nil {
		return "", 0, q.err
	}
	b, _ := io.ReadAll(r)
	q.content = string(b)
	return dir + "/" + name, int64(len(b)), nil
}

type fakePolicy struct {
	p     domain.Policy
	err   error
	calls int
}

func (f *fakePolicy) Policy(context.Context) (domain.Policy, error) { f.calls++; return f.p, f.err }

type fakeFilter struct {
	name     string
	patterns map[string]string // substring -> kind
}

func (f fakeFilter) Name() string { return f.name }
func (f fakeFilter) Scan(text string) []FilterFinding {
	var out []FilterFinding
	for sub, kind := range f.patterns {
		if i := strings.Index(text, sub); i >= 0 {
			out = append(out, FilterFinding{Filter: f.name, Kind: kind, Offset: i})
		}
	}
	return out
}

type fakeAudit struct {
	entries []AuditEntry
	err     error
}

func (a *fakeAudit) Record(_ context.Context, e AuditEntry, actionErr error) error {
	a.entries = append(a.entries, e)
	if actionErr != nil {
		return actionErr
	}
	return a.err
}

type fakeProbe struct {
	found bool
	err   error
	keys  []string
}

func (p *fakeProbe) FindSentByKey(_ context.Context, key string) (bool, error) {
	p.keys = append(p.keys, key)
	return p.found, p.err
}

// env bundles a fully wired service over fakes.
type env struct {
	r     *fakeReader
	w     *fakeWriter
	l     *fakeLedger
	q     *fakeQuarantine
	pol   *fakePolicy
	a     *fakeAudit
	clk   *fakeClock
	probe *fakeProbe
	deps  Deps
	cmds  Commands
}

const mailbox = "agent@corp.example.com"

func testPolicy() domain.Policy {
	return domain.Policy{
		Profile:         "agent",
		Mailbox:         mailbox,
		InternalDomains: []string{"corp.example.com"},
		Read: domain.ReadPolicy{
			Folders: []string{"inbox", "Processed"}, MaxBodyBytes: 200, HTMLToText: true, DefangLinks: true,
			Attachments: domain.AttachmentPolicy{Download: true, AllowTypes: []string{"pdf", "txt"}, MaxBytes: 100, OutDir: "/var/q"},
		},
		Send: domain.SendPolicy{
			Mode: domain.SendAllow,
			Recipients: domain.RecipientPolicy{
				AllowDomains: []string{"corp.example.com"}, External: domain.ExternalDeny, MaxTotal: 5,
			},
			SubjectPrefix:  "[agent] ",
			Footer:         "Automated message from agent {agent_id}. A human owns decisions.",
			BodyMaxBytes:   500,
			ContentFilters: []string{"secret_patterns"},
			Rate:           domain.SendRate{PerHour: 3, PerDay: 10},
		},
		Limits: domain.Limits{MaxResults: 10, MaxWritesPerRun: 20},
	}
}

func newEnv(t *testing.T, mut ...func(*domain.Policy)) *env {
	t.Helper()
	p := testPolicy()
	for _, m := range mut {
		m(&p)
	}
	e := &env{
		r: &fakeReader{
			profile: domain.Profile{Mail: mailbox, UserPrincipalName: mailbox},
			folders: []domain.Folder{
				{ID: "f-inbox", Name: "Inbox", WellKnown: domain.WellKnownInbox, UnreadCount: 2, TotalCount: 9},
				{ID: "f-proc", Name: "Processed", UnreadCount: 0, TotalCount: 3},
				{ID: "f-del", Name: "Deleted Items", WellKnown: domain.WellKnownDeletedItems},
				{ID: "f-arch", Name: "Archive", WellKnown: domain.WellKnownArchive},
			},
			messages: map[string]domain.RawMessage{},
			attach:   map[string][]domain.Attachment{},
			attData:  map[string]string{},
			pages:    map[string]domain.Page[domain.MessageSummary]{},
		},
		w:     &fakeWriter{},
		l:     newLedger(),
		q:     &fakeQuarantine{},
		pol:   &fakePolicy{p: p},
		a:     &fakeAudit{},
		clk:   &fakeClock{now: t0},
		probe: &fakeProbe{},
	}
	e.deps = Deps{
		Reader: e.r, Writer: e.w, Probe: e.probe, Ledger: e.l, Quarantine: e.q, Policy: e.pol,
		Filters: map[string]ContentFilter{"secret_patterns": fakeFilter{name: "secret_patterns", patterns: map[string]string{"AKIA": "aws_key", "BEGIN PRIVATE KEY": "private_key"}}},
		Audit:   e.a, Clock: e.clk, Run: RunInfo{AgentID: "rev-01", RunID: "run-7"},
	}
	e.cmds = New(e.deps)
	return e
}

// rebuild re-creates the facade (a new process run) over the same fakes.
func (e *env) rebuild() { e.cmds = New(e.deps) }

func (e *env) addMessage(id, folder string, mut ...func(*domain.RawMessage)) {
	m := domain.RawMessage{
		MessageSummary: domain.MessageSummary{
			ID: id, From: domain.Address{Address: "Jane@corp.example.com", Name: "Jane"}, Subject: "Hello",
			To: []domain.Address{{Address: mailbox}}, Received: t0.Add(-time.Hour),
		},
		Body:           domain.RawBody{Format: domain.BodyText, Content: "hi there"},
		ParentFolderID: folder,
	}
	for _, f := range mut {
		f(&m)
	}
	e.r.messages[id] = m
}

func (e *env) lastAudit(t *testing.T) AuditEntry {
	t.Helper()
	if len(e.a.entries) == 0 {
		t.Fatal("no audit entry")
	}
	return e.a.entries[len(e.a.entries)-1]
}

func mustCat(t *testing.T, err error, exit int) {
	t.Helper()
	if exitOf(err) != exit {
		t.Fatalf("exit = %d (err=%v), want %d", exitOf(err), err, exit)
	}
}

func ruleOf(err error) string {
	var de *domain.Error
	if errors.As(err, &de) {
		return de.RuleID
	}
	return ""
}
