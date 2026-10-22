// Package feather defines what a feather (a notification destination
// adapter) is to the delivery engine: something that makes one attempt to
// deliver a message and classifies its outcome. The adapters are built in
// (feather/slack, feather/discord, feather/email; built by feather/builtin).
package feather

import (
	"context"
	"errors"
	"time"

	"github.com/magicdrive/kjak/internal/core"
)

// Feather makes delivery attempts. Deliver makes exactly one attempt; the
// engine decides about retries from the Outcome. It must return when ctx is
// done.
type Feather interface {
	Deliver(ctx context.Context, msg core.Message) Outcome
}

// Outcome is the classified result of one attempt.
type Outcome struct {
	Acceptance core.Acceptance
	// Retryable: the provider certainly did not accept the message and a
	// later attempt may succeed. Never true with AcceptanceUnknown.
	Retryable bool
	// RetryAfter is the wait the provider asked for (0: none).
	RetryAfter time.Duration
	// StatusCode is the HTTP status or the SMTP reply code (0: none).
	StatusCode int
	Code       core.ErrorCode // "" when confirmed
	Err        error          // nil when confirmed
}

func Confirmed(status int) Outcome {
	return Outcome{Acceptance: core.AcceptanceConfirmed, StatusCode: status}
}

// Rejected: the message was certainly not accepted.
func Rejected(code core.ErrorCode, status int, retryable bool, err error) Outcome {
	return Outcome{Acceptance: core.AcceptanceRejected, Retryable: retryable, StatusCode: status, Code: code, Err: err}
}

// Unknown: the message may have been accepted. Not retryable, by
// construction: a retry could deliver it twice.
func Unknown(code core.ErrorCode, err error) Outcome {
	return Outcome{Acceptance: core.AcceptanceUnknown, Code: code, Err: err}
}

// The causes the engine cancels an attempt's context with.
var (
	ErrDeadline    = errors.New("delivery deadline exceeded")
	ErrInterrupted = errors.New("interrupted")
)

// ContextCode classifies a done context: deadline or canceled.
func ContextCode(ctx context.Context) core.ErrorCode {
	cause := context.Cause(ctx)
	if errors.Is(cause, ErrDeadline) || errors.Is(cause, context.DeadlineExceeded) {
		return core.ErrCodeDeadline
	}
	return core.ErrCodeCanceled
}

// ContextErr is the error of a done context, by its cause.
func ContextErr(ctx context.Context) error {
	if ContextCode(ctx) == core.ErrCodeDeadline {
		return ErrDeadline
	}
	return ErrInterrupted
}
