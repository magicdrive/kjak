package core

import (
	"time"
)

// SchemaVersion is the version of the JSON result document (output.WriteJSON).
const SchemaVersion = 1

// Status is the outcome of one target.
type Status string

const (
	StatusDelivered Status = "delivered" // the provider confirmed acceptance
	StatusFailed    Status = "failed"    // not confirmed: see Acceptance for whether it may have arrived
	StatusSkipped   Status = "skipped"   // a group member excluded before delivery
	StatusPlanned   Status = "planned"   // a dry run: would be delivered to
)

// Acceptance is what kjak knows about whether the provider accepted the
// message.
type Acceptance string

const (
	// AcceptanceConfirmed: the provider answered that it accepted the message.
	AcceptanceConfirmed Acceptance = "confirmed"
	// AcceptanceRejected: the message was certainly not accepted, because the
	// provider answered with a refusal or because it never fully reached the
	// provider.
	AcceptanceRejected Acceptance = "rejected"
	// AcceptanceUnknown: the message fully reached the provider but no answer
	// came back (the connection broke, the deadline passed). It may have been
	// accepted; kjak does not retry it, a retry could deliver it twice.
	AcceptanceUnknown Acceptance = "unknown"
	// AcceptanceNone: no attempt was made (skipped, or the deadline passed
	// before the first attempt).
	AcceptanceNone Acceptance = "none"
)

// ErrorCode classifies an error of an attempt or a result.
type ErrorCode string

const (
	ErrCodeRejected    ErrorCode = "rejected"     // the provider refused the message (permanent)
	ErrCodeRateLimited ErrorCode = "rate_limited" // HTTP 429
	ErrCodeServer      ErrorCode = "server_error" // HTTP 5xx, SMTP 4xx
	ErrCodeNetwork     ErrorCode = "network"      // the request did not fully reach the provider
	ErrCodeAmbiguous   ErrorCode = "ambiguous"    // the request reached the provider, its answer was lost
	ErrCodeDeadline    ErrorCode = "deadline"     // the delivery deadline passed
	ErrCodeCanceled    ErrorCode = "canceled"     // interrupted (SIGINT)
	ErrCodeProtocol    ErrorCode = "protocol"     // an unexpected answer (redirect, malformed reply)
	ErrCodeBlocked     ErrorCode = "blocked"      // kjak's own policy refused the request before it was sent (http: an address or a redirect)
	ErrCodeUnsupported ErrorCode = "unsupported"  // os feather: kjak has no OS notifications on this system
	ErrCodeUnavailable ErrorCode = "unavailable"  // os feather: no GUI or desktop session, or the OS program is missing or failed
	ErrCodeInternal    ErrorCode = "internal"
)

// ErrorInfo is an error as it is reported. Message never holds a secret: it
// is redacted when the result is built (delivery.Engine).
type ErrorInfo struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// Attempt is one try of a delivery.
type Attempt struct {
	Number     int           `json:"number"`
	StartedAt  time.Time     `json:"started_at"`
	Duration   time.Duration `json:"-"`
	Acceptance Acceptance    `json:"acceptance"`
	Retryable  bool          `json:"retryable"`
	// StatusCode is the HTTP status or the SMTP reply code, 0 without one.
	StatusCode int `json:"status_code,omitempty"`
	// RetryAfter is the wait the provider asked for, Wait the wait before
	// the next attempt (0: none followed).
	RetryAfter time.Duration `json:"-"`
	Wait       time.Duration `json:"-"`
	Error      *ErrorInfo    `json:"error,omitempty"`
}

// SkipInfo tells why a target was skipped.
type SkipInfo struct {
	Reason string `json:"reason"`
	Group  string `json:"group,omitempty"`
}

// Result is the outcome of one target.
type Result struct {
	Feather    string        `json:"feather"`
	Type       string        `json:"type"`
	Groups     []string      `json:"groups,omitempty"` // the groups that routed it here, if any
	Status     Status        `json:"status"`
	Acceptance Acceptance    `json:"acceptance"`
	Attempts   int           `json:"attempts"`
	Duration   time.Duration `json:"-"`
	Error      *ErrorInfo    `json:"error,omitempty"`
	History    []Attempt     `json:"history"`
	Skipped    *SkipInfo     `json:"skipped,omitempty"`
}

// Outcome is the overall outcome of a send.
type Outcome string

const (
	OutcomeSuccess  Outcome = "success"  // every attempted target delivered (some may be skipped)
	OutcomePartial  Outcome = "partial"  // some delivered, some failed
	OutcomeFailure  Outcome = "failure"  // none delivered, at least one failed
	OutcomeCanceled Outcome = "canceled" // interrupted
)

// Summary counts the results of a report.
type Summary struct {
	Total     int `json:"total"`
	Delivered int `json:"delivered"`
	Failed    int `json:"failed"`
	Unknown   int `json:"unknown"` // failed with acceptance unknown (also counted in Failed)
	Skipped   int `json:"skipped"`
}

// Report is the outcome of a send: one result per target, in the order of
// the targets.
type Report struct {
	Outcome  Outcome       `json:"outcome"`
	Canceled bool          `json:"canceled"`
	Duration time.Duration `json:"-"`
	Summary  Summary       `json:"summary"`
	Results  []Result      `json:"results"`
}

// Aggregate builds the report of results (kept in their order).
func Aggregate(results []Result, canceled bool, d time.Duration) Report {
	r := Report{Results: results, Canceled: canceled, Duration: d}
	if r.Results == nil {
		r.Results = []Result{}
	}
	for _, res := range results {
		r.Summary.Total++
		switch res.Status {
		case StatusDelivered:
			r.Summary.Delivered++
		case StatusSkipped:
			r.Summary.Skipped++
		case StatusPlanned:
		default:
			r.Summary.Failed++
			if res.Acceptance == AcceptanceUnknown {
				r.Summary.Unknown++
			}
		}
	}
	switch {
	case canceled:
		r.Outcome = OutcomeCanceled
	case r.Summary.Failed == 0:
		r.Outcome = OutcomeSuccess
	case r.Summary.Delivered > 0:
		r.Outcome = OutcomePartial
	default:
		r.Outcome = OutcomeFailure
	}
	return r
}
