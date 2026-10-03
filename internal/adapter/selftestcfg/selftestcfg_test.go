package selftestcfg

import (
	"context"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/selftest"
	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

// prdPolicy mirrors PRD section 9 (adapters must not import each other, so the
// policyfile default is not used here).
func prdPolicy() domain.Policy {
	return domain.Policy{
		Profile: "agent", Mailbox: "agent@corp.example.com",
		InternalDomains: []string{"corp.example.com"},
		Read:            domain.ReadPolicy{Folders: []string{"inbox", "Processed"}},
		Send: domain.SendPolicy{
			Mode: domain.SendAllow,
			Recipients: domain.RecipientPolicy{
				AllowDomains: []string{"corp.example.com"}, External: domain.ExternalDeny, MaxTotal: 5,
			},
			ContentFilters: []string{"secret_patterns", "classification_markers"},
		},
	}
}

// simCommands is an independent fake of the use cases: it inspects each
// request and denies exactly what the policy forbids. Methods not overridden
// panic (nil embedded interface), proving the matrix touches only these.
type simCommands struct {
	usecase.Commands
	p     domain.Policy
	calls []string
}

func denied() error { return domain.NewPolicyDenied("denied") }

func (s *simCommands) folderOK(name string) bool {
	for _, f := range s.p.Read.Folders {
		if strings.EqualFold(f, name) {
			return true
		}
	}
	return false
}

func (s *simCommands) ListMessages(_ context.Context, r usecase.ListRequest) (domain.Page[domain.MessageSummary], error) {
	s.calls = append(s.calls, "list")
	if !s.folderOK(r.Folder) {
		return domain.Page[domain.MessageSummary]{}, denied()
	}
	return domain.Page[domain.MessageSummary]{}, nil
}

func (s *simCommands) SearchMessages(_ context.Context, r usecase.SearchRequest) (domain.Page[domain.MessageSummary], error) {
	s.calls = append(s.calls, "search")
	if !s.folderOK(r.Folder) {
		return domain.Page[domain.MessageSummary]{}, denied()
	}
	return domain.Page[domain.MessageSummary]{}, nil
}

func (s *simCommands) GetAttachment(context.Context, usecase.AttachmentRequest) (usecase.AttachmentResult, error) {
	s.calls = append(s.calls, "attachment")
	return usecase.AttachmentResult{}, denied()
}

func (s *simCommands) recipientOK(a string) bool {
	r := s.p.Send.Recipients
	a = strings.ToLower(a)
	for _, x := range r.AllowAddresses {
		if x == a {
			return true
		}
	}
	dom := a[strings.LastIndexByte(a, '@')+1:]
	for _, d := range r.AllowDomains {
		if d == dom {
			return true
		}
	}
	return r.External == domain.ExternalAllow || r.External == domain.ExternalDraftOnly
}

func (s *simCommands) Send(_ context.Context, r usecase.SendRequest) (domain.SendResult, error) {
	s.calls = append(s.calls, "send")
	if !r.DryRun {
		panic("selftest must only dry-run sends")
	}
	if s.p.Send.Mode == domain.SendDeny {
		return domain.SendResult{}, denied()
	}
	all := append(append([]string{}, r.To...), r.Cc...)
	all = append(all, r.Bcc...)
	if len(r.Bcc) > 0 && !s.p.Send.Recipients.BccAllowed {
		return domain.SendResult{}, denied()
	}
	if n := s.p.Send.Recipients.MaxTotal; n > 0 && len(all) > n {
		return domain.SendResult{}, denied()
	}
	for _, a := range all {
		if !s.recipientOK(a) {
			return domain.SendResult{}, denied()
		}
	}
	if r.HasAttachments && !s.p.Send.Attachments {
		return domain.SendResult{}, denied()
	}
	for _, f := range s.p.Send.ContentFilters {
		if f == "secret_patterns" && strings.Contains(r.Body, "AKIA") {
			return domain.SendResult{}, denied()
		}
		if f == "classification_markers" && strings.Contains(strings.ToLower(r.Body), "confidential") {
			return domain.SendResult{}, denied()
		}
	}
	return domain.SendResult{DryRun: true}, nil
}

func (s *simCommands) Reply(_ context.Context, r usecase.ReplyRequest) (domain.SendResult, error) {
	s.calls = append(s.calls, "reply")
	if r.All && !s.p.Send.ReplyAll {
		return domain.SendResult{}, denied()
	}
	return domain.SendResult{DryRun: true}, nil
}

func (s *simCommands) Move(_ context.Context, r usecase.MoveRequest) (usecase.MoveResult, error) {
	s.calls = append(s.calls, "move")
	if strings.EqualFold(r.Folder, "Deleted Items") {
		return usecase.MoveResult{}, denied()
	}
	return usecase.MoveResult{}, nil
}

func run(t *testing.T, p domain.Policy, readOnly bool) (selftest.Result, *simCommands) {
	t.Helper()
	sim := &simCommands{p: p}
	res, err := Runner(sim, p, readOnly).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res, sim
}

func requireAllPass(t *testing.T, res selftest.Result) {
	t.Helper()
	for _, r := range res.Rows {
		if r.Status == selftest.StatusFail {
			t.Errorf("row %s (%s) failed: %s", r.Name, r.Expect, r.Detail)
		}
	}
}

func TestPRDMatrixPassesAgainstFaithfulCommands(t *testing.T) {
	res, sim := run(t, prdPolicy(), false)
	requireAllPass(t, res)
	if res.Passed != len(Rows(prdPolicy())) || res.Skipped != 0 {
		t.Fatalf("%+v", res)
	}
	if len(sim.calls) != len(res.Rows) {
		t.Fatalf("one call per row: %v", sim.calls)
	}
}

func TestPRDRowExpectations(t *testing.T) {
	want := map[string]selftest.Outcome{
		"read.folder.allowed":                selftest.Allow,
		"read.folder.unlisted":               selftest.Deny,
		"read.search.unlisted-folder":        selftest.Deny,
		"attachment.outside-quarantine":      selftest.Deny,
		"send.allowed-recipient.dry-run":     selftest.Allow,
		"send.external-recipient.dry-run":    selftest.Deny,
		"send.bcc.dry-run":                   selftest.Deny,
		"send.over-max-total.dry-run":        selftest.Deny,
		"send.attachments.dry-run":           selftest.Deny,
		"send.content-filter.secret":         selftest.Deny,
		"send.content-filter.classification": selftest.Deny,
		"reply.all":                          selftest.Deny,
		"move.deleted-items":                 selftest.Deny,
	}
	rows := Rows(prdPolicy())
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	names := map[string]bool{}
	for _, r := range rows {
		if names[r.Name] {
			t.Fatalf("duplicate row %s", r.Name)
		}
		names[r.Name] = true
		if want[r.Name] != r.Expect {
			t.Errorf("%s expect %s, want %s", r.Name, r.Expect, want[r.Name])
		}
		ro := strings.HasPrefix(r.Name, "read.") || strings.HasPrefix(r.Name, "attachment.")
		if r.ReadOnly != ro {
			t.Errorf("%s ReadOnly=%v", r.Name, r.ReadOnly)
		}
		if r.Verb == "" || r.Resource == "" {
			t.Errorf("%s missing verb/resource", r.Name)
		}
	}
}

func TestReadOnlySkipsWriteRows(t *testing.T) {
	res, sim := run(t, prdPolicy(), true)
	requireAllPass(t, res)
	if res.Skipped == 0 || res.Passed != 4 {
		t.Fatalf("%+v", res)
	}
	for _, c := range sim.calls {
		if c == "send" || c == "reply" || c == "move" {
			t.Fatalf("read-only run made a write call: %v", sim.calls)
		}
	}
}

func TestMatrixFollowsPolicyVariants(t *testing.T) {
	variants := map[string]func(*domain.Policy){
		"send deny":        func(p *domain.Policy) { p.Send.Mode = domain.SendDeny },
		"dry run only":     func(p *domain.Policy) { p.Send.Mode = domain.SendDryRunOnly },
		"external allow":   func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalAllow },
		"external draft":   func(p *domain.Policy) { p.Send.Recipients.External = domain.ExternalDraftOnly },
		"bcc allowed":      func(p *domain.Policy) { p.Send.Recipients.BccAllowed = true },
		"reply-all":        func(p *domain.Policy) { p.Send.ReplyAll = true },
		"send attachments": func(p *domain.Policy) { p.Send.Attachments = true },
		"no filters":       func(p *domain.Policy) { p.Send.ContentFilters = nil },
		"no max total":     func(p *domain.Policy) { p.Send.Recipients.MaxTotal = 0 },
		"no read folders":  func(p *domain.Policy) { p.Read.Folders = nil },
		"address allowlist": func(p *domain.Policy) {
			p.Send.Recipients.AllowDomains = nil
			p.Send.Recipients.AllowAddresses = []string{"boss@partner.example"}
		},
		"no allowlist": func(p *domain.Policy) { p.Send.Recipients.AllowDomains = nil },
		"no internal domains": func(p *domain.Policy) {
			p.InternalDomains = nil
			p.Send.Recipients.AllowDomains = nil
		},
		"all folders listed": func(p *domain.Policy) {
			p.Read.Folders = []string{"deleteditems", "Junk Email", "SentItems", "Drafts", "Archive"}
		},
	}
	for name, mut := range variants {
		t.Run(name, func(t *testing.T) {
			p := prdPolicy()
			mut(&p)
			res, _ := run(t, p, false)
			requireAllPass(t, res)
			if len(res.Rows) == 0 {
				t.Fatal("no rows")
			}
		})
	}
}

func TestVariantRowPresence(t *testing.T) {
	has := func(p domain.Policy, name string) bool {
		for _, r := range Rows(p) {
			if r.Name == name {
				return true
			}
		}
		return false
	}
	p := prdPolicy()
	p.Send.ReplyAll, p.Send.Attachments, p.Send.Recipients.BccAllowed = true, true, true
	p.Send.ContentFilters = nil
	for _, n := range []string{"reply.all", "send.attachments.dry-run", "send.bcc.dry-run", "send.content-filter.secret", "send.content-filter.classification"} {
		if has(p, n) {
			t.Errorf("row %s must be omitted when policy allows it / filter off", n)
		}
	}
	p = prdPolicy()
	p.Send.Recipients.AllowDomains = nil
	if has(p, "send.allowed-recipient.dry-run") {
		t.Error("no allowlist -> no allowed-recipient row")
	}
	p = prdPolicy()
	p.Read.Folders = nil
	if has(p, "read.folder.allowed") {
		t.Error("no folders -> no allowed-folder row")
	}
}

func TestDenyRowsCatchAPermissiveBackend(t *testing.T) {
	// A backend that allows everything must fail every Deny row.
	p := prdPolicy()
	res, err := Runner(allowAll{}, p, false).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	denyRows := 0
	for _, r := range Rows(p) {
		if r.Expect == selftest.Deny {
			denyRows++
		}
	}
	if res.Failed != denyRows || res.OK() {
		t.Fatalf("failed=%d want %d", res.Failed, denyRows)
	}
}

type allowAll struct{ usecase.Commands }

func (allowAll) ListMessages(context.Context, usecase.ListRequest) (domain.Page[domain.MessageSummary], error) {
	return domain.Page[domain.MessageSummary]{}, nil
}
func (allowAll) SearchMessages(context.Context, usecase.SearchRequest) (domain.Page[domain.MessageSummary], error) {
	return domain.Page[domain.MessageSummary]{}, nil
}
func (allowAll) GetAttachment(context.Context, usecase.AttachmentRequest) (usecase.AttachmentResult, error) {
	return usecase.AttachmentResult{}, nil
}
func (allowAll) Send(context.Context, usecase.SendRequest) (domain.SendResult, error) {
	return domain.SendResult{}, nil
}
func (allowAll) Reply(context.Context, usecase.ReplyRequest) (domain.SendResult, error) {
	return domain.SendResult{}, nil
}
func (allowAll) Move(context.Context, usecase.MoveRequest) (usecase.MoveResult, error) {
	return usecase.MoveResult{}, nil
}

type failing struct{ allowAll }

func (failing) ListMessages(context.Context, usecase.ListRequest) (domain.Page[domain.MessageSummary], error) {
	return domain.Page[domain.MessageSummary]{}, domain.NewGeneral("graph exploded")
}

func TestNonPolicyErrorFailsRowNotDeny(t *testing.T) {
	p := prdPolicy()
	got, err := Probe(failing{}, p)(context.Background(), Rows(p)[0])
	if err == nil || got != "" || !strings.Contains(err.Error(), "graph exploded") {
		t.Fatalf("%q %v", got, err)
	}
}

func TestPolicyDeniedMapsToDenyViaWrappedError(t *testing.T) {
	p := prdPolicy()
	sim := &simCommands{p: p}
	row := selftest.Row{Name: "read.folder.unlisted"}
	got, err := Probe(sim, p)(context.Background(), row)
	if err != nil || got != selftest.Deny {
		t.Fatalf("%v %v", got, err)
	}
	if output.ExitOf(denied()) != 6 {
		t.Fatal("policy denied must be exit 6")
	}
}

func TestUnknownRow(t *testing.T) {
	p := prdPolicy()
	if _, err := Probe(allowAll{}, p)(context.Background(), selftest.Row{Name: "nope"}); err == nil {
		t.Fatal("unknown row must error")
	}
}

func TestOverMaxTotalAddressesAreDistinct(t *testing.T) {
	p := prdPolicy()
	var got usecase.SendRequest
	cap := captureSend{out: &got}
	if _, err := Probe(cap, p)(context.Background(), selftest.Row{Name: "send.over-max-total.dry-run"}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, a := range got.To {
		seen[a] = true
	}
	if len(got.To) != 6 || len(seen) != 6 || !got.DryRun {
		t.Fatalf("%+v", got)
	}
}

type captureSend struct {
	allowAll
	out *usecase.SendRequest
}

func (c captureSend) Send(_ context.Context, r usecase.SendRequest) (domain.SendResult, error) {
	*c.out = r
	return domain.SendResult{}, nil
}
