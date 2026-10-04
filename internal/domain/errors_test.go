package domain

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
)

func TestErrorConstructorsMapToExitCodes(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		exit output.ExitCode
	}{
		{"usage", NewUsage("x"), output.ExitUsage},
		{"validation", NewValidation("x"), output.ExitValidation},
		{"policy", NewPolicyDenied("x"), output.ExitPolicyDenied},
		{"not found", NewNotFound("x"), output.ExitNotFound},
		{"conflict", NewConflict("x"), output.ExitConflict},
		{"forbidden", NewForbidden("x"), output.ExitForbidden},
		{"auth", NewAuth("x"), output.ExitAuth},
		{"rate", NewRateLimited("x"), output.ExitRateLimited},
		{"general", NewGeneral("x"), output.ExitGeneral},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := output.ExitOf(fmt.Errorf("wrapped: %w", tc.err)); got != tc.exit {
				t.Fatalf("exit = %d, want %d", got, tc.exit)
			}
		})
	}
}

func TestErrorHintCauseAndEnvelope(t *testing.T) {
	cause := errors.New("boom")
	e := NewConflict("pending").WithHint("do not resend").WithCause(cause)
	if !errors.Is(e, cause) || e.Hint() != "do not resend" || e.Error() != "pending" {
		t.Fatalf("unexpected error: %+v", e)
	}
	env := output.FromError(e)
	if env.OK || env.Error.Hint != "do not resend" || env.Error.Code != output.CategoryConflict {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestNotSentKeepsCategoryAndSentinel(t *testing.T) {
	if NotSent(nil) != nil {
		t.Fatal("NotSent(nil) must be nil")
	}
	err := NotSent(NewForbidden("denied"))
	if !errors.Is(err, ErrNotSent) {
		t.Fatal("errors.Is(ErrNotSent) must hold")
	}
	if output.ExitOf(err) != output.ExitForbidden {
		t.Fatalf("category lost: %d", output.ExitOf(err))
	}
	if err.Error() != "denied" {
		t.Fatalf("message = %q", err.Error())
	}
	if errors.Is(NewForbidden("x"), ErrNotSent) {
		t.Fatal("plain error must not match ErrNotSent")
	}
}

func TestDecision(t *testing.T) {
	allow := Decision{Mode: DecisionAllow}
	if !allow.Allowed() || allow.Err() != nil || allow.AuditString() != "allow" {
		t.Fatalf("allow decision wrong: %+v", allow)
	}
	deny := Decision{Mode: DecisionDeny, RuleID: "send.bcc", Reason: "bcc denied", RetryAfter: time.Second}
	err := deny.Err()
	var de *Error
	if !errors.As(err, &de) || de.RuleID != "send.bcc" || output.ExitOf(err) != output.ExitPolicyDenied {
		t.Fatalf("deny error wrong: %v", err)
	}
	if deny.AuditString() != "deny:send.bcc" {
		t.Fatalf("audit string = %q", deny.AuditString())
	}
	if (Decision{Mode: DecisionDryRunOnly}).Err() == nil {
		t.Fatal("dry_run_only must not be Allowed")
	}
	if (Decision{Mode: DecisionDraftOnly}).AuditString() != "draft_only" {
		t.Fatal("rule-less decision audit string must be the mode")
	}
}
