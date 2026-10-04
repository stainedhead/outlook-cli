package auditlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/audit"
	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

type fakeClock struct{ t time.Time }

func (f fakeClock) Now() time.Time { return f.t }

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func entry() usecase.AuditEntry {
	return usecase.AuditEntry{
		Verb: domain.VerbSend, Resource: "mail.send", Outcome: "denied",
		HTTPStatus: 0, Duration: 150 * time.Millisecond, PolicyDecision: "deny:send.recipients.external",
	}
}

func TestRecordWritesOneLine(t *testing.T) {
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{AgentID: "agent-1", RunID: "run-9", Clock: fakeClock{t0}})
	if err := s.Record(context.Background(), entry(), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), "\n") != 1 || !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("want exactly one line: %q", buf.String())
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"schema_version": float64(1), "tool": "outlook", "agent_id": "agent-1", "run_id": "run-9",
		"verb": "send", "resource": "mail.send", "outcome": "denied",
		"policy_decision": "deny:send.recipients.external", "duration": "150ms",
		"ts": "2026-10-03T12:00:00Z",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
	for k := range m {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected field %q (audit must not carry bodies or extras)", k)
		}
	}
}

func TestRecordHTTPStatusAndPassThroughError(t *testing.T) {
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{Clock: fakeClock{t0}})
	e := entry()
	e.HTTPStatus = 403
	boom := errors.New("action failed")
	if err := s.Record(context.Background(), e, boom); err != boom {
		t.Fatalf("want the action error back, got %v", err)
	}
	if !strings.Contains(buf.String(), `"http_status":403`) {
		t.Fatal(buf.String())
	}
}

func TestRecordUsesSystemClockWhenNone(t *testing.T) {
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{})
	if err := s.Record(context.Background(), entry(), nil); err != nil {
		t.Fatal(err)
	}
	var m struct{ Ts time.Time }
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil || m.Ts.IsZero() {
		t.Fatalf("ts missing: %v %s", err, buf.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestBlockModeFailsTheAction(t *testing.T) {
	s := NewWithWriter(failWriter{}, Config{FailureMode: audit.Block})
	action := errors.New("send failed")
	err := s.Record(context.Background(), entry(), action)
	if !errors.Is(err, audit.ErrWrite) || !errors.Is(err, action) {
		t.Fatalf("block mode must join write error and action error: %v", err)
	}
	if err := s.Record(context.Background(), entry(), nil); !errors.Is(err, audit.ErrWrite) {
		t.Fatalf("block mode must fail an otherwise successful action: %v", err)
	}
}

func TestWarnModeKeepsActionError(t *testing.T) {
	var seen error
	s := NewWithWriter(failWriter{}, Config{FailureMode: audit.Warn, OnWriteError: func(e error) { seen = e }})
	if err := s.Record(context.Background(), entry(), nil); err != nil {
		t.Fatalf("warn mode must not fail the action: %v", err)
	}
	if !errors.Is(seen, audit.ErrWrite) {
		t.Fatalf("OnWriteError not called: %v", seen)
	}
	action := errors.New("x")
	if err := s.Record(context.Background(), entry(), action); err != action {
		t.Fatal(err)
	}
}

func TestOpenCreatesPrivateFileAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "outlook.audit.jsonl")
	s, err := Open(Config{Path: path, AgentID: "a", RunID: "r", Clock: fakeClock{t0}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.Record(context.Background(), entry(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen appends.
	s2, err := Open(Config{Path: path, Clock: fakeClock{t0}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Record(context.Background(), entry(), nil); err != nil {
		t.Fatal(err)
	}
	_ = s2.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != 3 {
		t.Fatalf("want 3 lines, got %d", n)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestRecordAfterCloseIsWriteError(t *testing.T) {
	s, err := Open(Config{Path: filepath.Join(t.TempDir(), "a.jsonl"), FailureMode: audit.Block})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err := s.Record(context.Background(), entry(), nil); !errors.Is(err, audit.ErrWrite) {
		t.Fatal(err)
	}
}

func TestOpenErrors(t *testing.T) {
	_, err := Open(Config{})
	var de *domain.Error
	if !errors.As(err, &de) || !errors.Is(err, audit.ErrNoPath) {
		t.Fatalf("empty path: %v", err)
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Config{Path: filepath.Join(blocker, "x", "a.jsonl")}); !errors.As(err, &de) {
		t.Fatalf("unusable dir: %v", err)
	}
}

func TestNilSinkCloseIsSafe(t *testing.T) {
	var buf bytes.Buffer
	if err := NewWithWriter(&buf, Config{}).Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecordHonoursCancelledContext(t *testing.T) {
	// An audit record is written even if the command's context is cancelled:
	// the action already happened and must be recorded.
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewWithWriter(&buf, Config{}).Record(ctx, entry(), nil); err != nil || buf.Len() == 0 {
		t.Fatalf("%v %q", err, buf.String())
	}
}

func TestSecretsAreRedacted(t *testing.T) {
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{Secrets: []string{"s3cr3t-token-value"}})
	e := entry()
	e.Resource = "mail.send s3cr3t-token-value"
	if err := s.Record(context.Background(), e, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "s3cr3t-token-value") {
		t.Fatal("secret leaked")
	}
}

// FR-R13: core audit.Record (v0.1.0) has no extension fields, so recipient
// count/hash, message id and warnings are folded into policy_decision as
// ";key=value" suffixes (docs/requested-core-changes.md item 17).
func TestRecordFoldsExtensionFieldsIntoPolicyDecision(t *testing.T) {
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{Clock: fakeClock{t0}})
	e := entry()
	e.Outcome, e.PolicyDecision, e.HTTPStatus = "ok", "allow", 202
	e.RecipientCount, e.RecipientHash, e.MessageID = 2, "0123456789abcdef0123456789abcdef", "AAMk=1"
	e.Warnings = []string{"ledger_update_failed", "probe=inconclusive"}
	if err := s.Record(context.Background(), e, nil); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	want := "allow;recipient_count=2;recipient_hash=0123456789abcdef0123456789abcdef;message_id=AAMk=1;warnings=ledger_update_failed,probe=inconclusive"
	if m["policy_decision"] != want {
		t.Errorf("policy_decision = %v, want %v", m["policy_decision"], want)
	}
	if m["http_status"] != float64(202) {
		t.Errorf("http_status = %v", m["http_status"])
	}
}

func TestRecordOmitsEmptyExtensionFields(t *testing.T) {
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{Clock: fakeClock{t0}})
	if err := s.Record(context.Background(), entry(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"policy_decision":"deny:send.recipients.external"`) {
		t.Errorf("plain decision must be untouched: %s", buf.String())
	}
}
