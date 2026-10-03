package domain

import (
	"errors"

	"github.com/stainedhead/agent-cli-core/output"
)

// Error is the single error type of the domain and use cases. It implements
// output.CategoryError and output.Hinter, so output.FromError(err) yields the
// right envelope and output.ExitOf(err) the right exit code with no mapping.
//
// Errors from adapters that already carry a category (httpx.ForbiddenError,
// auth.UnreachableError, ...) pass through use cases unchanged (wrap with %w).
type Error struct {
	Cat output.Category
	// Code is an optional stable sub-code, for example a Graph error code on a
	// 403 ("ErrorAccessDenied"). Never a response body.
	Code    string
	Message string
	HintMsg string
	// RuleID is set on policy-denied errors.
	RuleID string
	Cause  error
}

// Error implements error.
func (e *Error) Error() string { return e.Message }

// Category implements output.CategoryError.
func (e *Error) Category() output.Category { return e.Cat }

// Hint implements output.Hinter.
func (e *Error) Hint() string { return e.HintMsg }

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Cause }

// WithHint returns e with its hint set, for chaining.
func (e *Error) WithHint(h string) *Error { e.HintMsg = h; return e }

// WithCause returns e with its cause set, for chaining.
func (e *Error) WithCause(c error) *Error { e.Cause = c; return e }

func newErr(c output.Category, msg string) *Error { return &Error{Cat: c, Message: msg} }

// NewUsage is a malformed command line or token (exit 2).
func NewUsage(msg string) *Error { return newErr(output.CategoryUsage, msg) }

// NewValidation is input that failed validation (exit 9).
func NewValidation(msg string) *Error { return newErr(output.CategoryValidation, msg) }

// NewPolicyDenied is a client-side policy refusal (exit 6).
func NewPolicyDenied(msg string) *Error { return newErr(output.CategoryPolicyDenied, msg) }

// NewNotFound is a missing target (exit 5).
func NewNotFound(msg string) *Error { return newErr(output.CategoryNotFound, msg) }

// NewConflict is a conflict or failed precondition (exit 7), for example a
// ledger entry left pending by an earlier attempt of unknown outcome.
func NewConflict(msg string) *Error { return newErr(output.CategoryConflict, msg) }

// NewForbidden is a server refusal (exit 4). Adapters normally return
// httpx.ForbiddenError instead; use this for refusals the adapter derives
// itself.
func NewForbidden(msg string) *Error { return newErr(output.CategoryForbidden, msg) }

// NewAuth is an authentication failure (exit 3).
func NewAuth(msg string) *Error { return newErr(output.CategoryAuth, msg) }

// NewRateLimited is rate limiting that persisted (exit 8), including a local
// rate-limit denial that carries RetryAfter semantics.
func NewRateLimited(msg string) *Error { return newErr(output.CategoryRateLimited, msg) }

// NewGeneral is an unexpected failure (exit 1), for example malformed Graph
// JSON. Never panic on remote data.
func NewGeneral(msg string) *Error { return newErr(output.CategoryGeneral, msg) }

// ErrNotSent marks a write failure after which the adapter can prove the
// message was NOT accepted (for example a 4xx, a 429, a refusal before the
// request left the process). The idempotency use case then marks the ledger
// entry failed so the key can be retried. Any write error that is not
// ErrNotSent (timeout, 5xx, dropped connection) is ambiguous: the ledger entry
// stays pending and later attempts with that key fail closed with a conflict.
var ErrNotSent = errors.New("message was not accepted")

type notSent struct{ err error }

func (n notSent) Error() string        { return n.err.Error() }
func (n notSent) Unwrap() error        { return n.err }
func (n notSent) Is(target error) bool { return target == ErrNotSent }

// NotSent wraps err so errors.Is(err, ErrNotSent) holds while the category and
// hint of err remain reachable through errors.As. Nil stays nil.
func NotSent(err error) error {
	if err == nil {
		return nil
	}
	return notSent{err}
}
