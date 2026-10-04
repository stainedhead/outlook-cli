package auditlog

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/stainedhead/agent-cli-core/audit"
	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

// Tool is the tool name written to every record.
const Tool = "outlook"

// Config configures a Sink.
type Config struct {
	// Path is the JSONL file (policy audit.path). Open only.
	Path string
	// AgentID and RunID identify the caller; they are written to every record.
	AgentID, RunID string
	// Clock stamps records; nil uses the system clock.
	Clock usecase.Clock
	// FailureMode is audit.Block (a failed write fails the command) or
	// audit.Warn (the zero value: the failure goes to OnWriteError only). The
	// composition root should choose Block for send and write commands.
	FailureMode audit.WriteFailureMode
	// OnWriteError receives write failures in Warn mode.
	OnWriteError func(error)
	// Secrets are literal values to redact from record fields. The process
	// should never hold one (tokens stay inside auth.Authorizer); this is a
	// second line of defence.
	Secrets []string
}

// Sink implements usecase.AuditSink.
type Sink struct {
	log *audit.Logger
	cfg Config
}

var _ usecase.AuditSink = (*Sink)(nil)

// Open creates (0700 directory, 0600 file) or appends to cfg.Path.
func Open(cfg Config) (*Sink, error) {
	l, err := audit.Open(audit.Config{Path: cfg.Path, OnFailure: cfg.FailureMode}, options(cfg)...)
	if err != nil {
		return nil, domain.NewGeneral("audit log cannot be opened").
			WithHint("check audit.path in the policy: the directory must be creatable and the file appendable by the agent user").
			WithCause(err)
	}
	return &Sink{log: l, cfg: cfg}, nil
}

// NewWithWriter builds a Sink over any writer; used by tests and by callers
// that own the file themselves. Close closes w if it is an io.Closer.
func NewWithWriter(w io.Writer, cfg Config) *Sink {
	return &Sink{log: audit.NewLogger(w, append(options(cfg), audit.WithFailureMode(cfg.FailureMode))...), cfg: cfg}
}

func options(cfg Config) []audit.Option {
	opts := []audit.Option{audit.WithOnWriteError(cfg.OnWriteError)}
	if len(cfg.Secrets) > 0 {
		opts = append(opts, audit.WithSecrets(cfg.Secrets...))
	}
	return opts
}

// Record writes one record and returns the error the command should return:
// actionErr, joined with the write error in Block mode. The context is not
// consulted: the action has already happened and must be recorded even if the
// command was cancelled.
func (s *Sink) Record(_ context.Context, e usecase.AuditEntry, actionErr error) error {
	rec := audit.Record{
		Tool:           Tool,
		AgentID:        s.cfg.AgentID,
		RunID:          s.cfg.RunID,
		Verb:           string(e.Verb),
		Resource:       e.Resource,
		Outcome:        e.Outcome,
		HTTPStatus:     e.HTTPStatus,
		Duration:       e.Duration,
		PolicyDecision: foldDecision(e),
	}
	if s.cfg.Clock != nil {
		rec.Timestamp = s.cfg.Clock.Now()
	}
	return s.log.Handle(rec, actionErr)
}

// foldDecision appends the FR-R13 fields to the policy decision as
// ";key=value" pairs. audit.Record (core v0.1.0) has no extension fields; this
// keeps them in an existing field until the core grows them. Only counts,
// hashes, ids and fixed warning tokens are written, never subject, body or
// addresses.
func foldDecision(e usecase.AuditEntry) string {
	var b strings.Builder
	b.WriteString(e.PolicyDecision)
	if e.RecipientCount > 0 {
		b.WriteString(";recipient_count=" + strconv.Itoa(e.RecipientCount))
	}
	if e.RecipientHash != "" {
		b.WriteString(";recipient_hash=" + e.RecipientHash)
	}
	if e.MessageID != "" {
		b.WriteString(";message_id=" + e.MessageID)
	}
	if len(e.Warnings) > 0 {
		b.WriteString(";warnings=" + strings.Join(e.Warnings, ","))
	}
	return b.String()
}

// Close closes the underlying file. Records after Close fail as write errors.
func (s *Sink) Close() error { return s.log.Close() }
