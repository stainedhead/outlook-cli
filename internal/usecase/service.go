package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// Audit outcomes (AuditEntry.Outcome).
const (
	outcomeOK          = "ok"
	outcomeDenied      = "denied"
	outcomeDryRun      = "dry_run"
	outcomeDraft       = "draft"
	outcomeError       = "error"
	outcomeAlreadySent = "already_sent"
)

// service implements Commands. One instance serves one process run: it caches
// the policy, the FR-002 mailbox check, the resolved readable folders and the
// in-memory write counter (limits.max_writes_per_run).
type service struct {
	d Deps

	mu       sync.Mutex
	policy   *domain.Policy
	verified bool
	readable []domain.Folder
	resolved bool
	writes   int
	seq      int
}

// New builds the use-case facade. A Deps missing a required port does not
// panic: every command then fails with a general error naming the port.
func New(d Deps) Commands { return &service{d: d} }

// call collects what one command wants written to its single audit entry.
type call struct {
	outcome  string
	decision *domain.Decision
}

func (c *call) setDecision(d domain.Decision) { c.decision = &d }

// exec runs fn and writes exactly one audit entry for it.
func (s *service) exec(ctx context.Context, verb domain.Verb, resource string, fn func(c *call) error) error {
	if err := s.checkDeps(); err != nil {
		return err
	}
	start := s.d.Clock.Now()
	c := &call{}
	err := fn(c)
	e := AuditEntry{
		Verb:           verb,
		Resource:       resource,
		Outcome:        c.outcome,
		Duration:       s.d.Clock.Now().Sub(start),
		PolicyDecision: string(domain.DecisionAllow),
	}
	var de *domain.Error
	denied := errors.As(err, &de) && (de.Cat == output.CategoryPolicyDenied || (de.Cat == output.CategoryRateLimited && de.RuleID != ""))
	switch {
	case denied && de.RuleID != "":
		e.PolicyDecision = domain.Decision{Mode: domain.DecisionDeny, RuleID: de.RuleID}.AuditString()
	case c.decision != nil:
		e.PolicyDecision = c.decision.AuditString()
	case denied:
		e.PolicyDecision = string(domain.DecisionDeny)
	}
	switch {
	case err != nil && denied:
		e.Outcome = outcomeDenied
	case err != nil:
		e.Outcome = outcomeError
	case e.Outcome == "":
		e.Outcome = outcomeOK
	}
	return s.d.Audit.Record(ctx, e, err)
}

func (s *service) checkDeps() error {
	missing := ""
	switch {
	case s.d.Reader == nil:
		missing = "Reader"
	case s.d.Writer == nil:
		missing = "Writer"
	case s.d.Ledger == nil:
		missing = "Ledger"
	case s.d.Policy == nil:
		missing = "Policy"
	case s.d.Audit == nil:
		missing = "Audit"
	case s.d.Clock == nil:
		missing = "Clock"
	}
	if missing != "" {
		return domain.NewGeneral("internal error: use-case dependency not configured: " + missing)
	}
	return nil
}

// begin loads the policy (once per run) and performs the FR-002 mailbox check
// (once per run, success only). Every command calls it first.
func (s *service) begin(ctx context.Context) (domain.Policy, error) {
	s.mu.Lock()
	if s.policy != nil && s.verified {
		p := *s.policy
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()

	p, err := s.d.Policy.Policy(ctx)
	if err != nil {
		return domain.Policy{}, err
	}
	prof, err := s.d.Reader.Me(ctx)
	if err != nil {
		return domain.Policy{}, err
	}
	if d := p.EvalMailbox(prof); !d.Allowed() {
		return domain.Policy{}, domain.ErrFor(d)
	}
	s.mu.Lock()
	s.policy, s.verified = &p, true
	s.mu.Unlock()
	return p, nil
}

// readableFolders resolves the policy read.folders entries to real folders.
// Entries that do not exist yet (for example a Processed folder not created)
// are skipped; any other error is returned.
func (s *service) readableFolders(ctx context.Context, p domain.Policy) ([]domain.Folder, error) {
	s.mu.Lock()
	if s.resolved {
		out := append([]domain.Folder(nil), s.readable...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()

	var out []domain.Folder
	seen := map[string]bool{}
	for _, name := range p.Read.Folders {
		f, err := s.d.Reader.ResolveFolder(ctx, name)
		if err != nil {
			if isCategory(err, output.CategoryNotFound) {
				continue
			}
			return nil, err
		}
		if !p.FolderAllowed(f) || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		out = append(out, f)
	}
	s.mu.Lock()
	s.readable, s.resolved = out, true
	s.mu.Unlock()
	return append([]domain.Folder(nil), out...), nil
}

// resolveReadable resolves one named folder and checks it against policy.
func (s *service) resolveReadable(ctx context.Context, p domain.Policy, name string) (domain.Folder, error) {
	f, err := s.d.Reader.ResolveFolder(ctx, name)
	if err != nil {
		return domain.Folder{}, err
	}
	if d := p.EvalReadFolder(f); !d.Allowed() {
		return domain.Folder{}, domain.ErrFor(d)
	}
	return f, nil
}

// readableMessage fetches a message and refuses it unless it lives in a
// folder named by read.folders. A message whose folder cannot be determined
// is refused (fail closed).
func (s *service) readableMessage(ctx context.Context, p domain.Policy, id string, wantHeaders bool) (domain.RawMessage, error) {
	if id == "" {
		return domain.RawMessage{}, domain.NewUsage("message id is required")
	}
	raw, err := s.d.Reader.GetMessage(ctx, id, wantHeaders)
	if err != nil {
		return domain.RawMessage{}, err
	}
	folders, err := s.readableFolders(ctx, p)
	if err != nil {
		return domain.RawMessage{}, err
	}
	for _, f := range folders {
		if raw.ParentFolderID != "" && f.ID == raw.ParentFolderID {
			return raw, nil
		}
	}
	return domain.RawMessage{}, domain.ErrFor(domain.Decision{
		Mode: domain.DecisionDeny, RuleID: domain.RuleReadFolders,
		Reason: "the message is not in a folder allowed by policy read.folders",
	})
}

// countWrite reserves one slot of limits.max_writes_per_run.
func (s *service) countWrite(p domain.Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := p.EvalWrites(s.writes); !d.Allowed() {
		return domain.ErrFor(d)
	}
	s.writes++
	return nil
}

// nextSeq returns a per-run counter used to build unique ledger keys.
func (s *service) nextSeq() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return s.seq
}

func isCategory(err error, c output.Category) bool {
	var ce output.CategoryError
	return errors.As(err, &ce) && ce.Category() == c
}

func ledgerErr(err error) error {
	return domain.NewGeneral("idempotency ledger unavailable; nothing was sent").WithCause(err)
}

func (s *service) cleanSummary(p domain.Policy, m domain.MessageSummary) domain.MessageSummary {
	m.Subject = domain.CleanText(m.Subject)
	m.From.Name = domain.CleanText(m.From.Name)
	m.From.Address = lower(m.From.Address)
	if len(m.To) > 0 {
		to := make([]domain.Address, len(m.To))
		for i, a := range m.To {
			to[i] = domain.Address{Address: lower(a.Address), Name: domain.CleanText(a.Name)}
		}
		m.To = to
	}
	m.SenderTrust = p.SenderTrustOf(m.From)
	return m
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

const timeHour = time.Hour
