// Package output renders reports as text or as the JSON document of schema
// version 1 (docs/cli.md "JSON output").
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/magicdrive/kjak/internal/core"
)

// Formats.
const (
	FormatText = "text"
	FormatJSON = "json"
)

func ParseFormat(s string) (string, error) {
	switch s {
	case FormatText, FormatJSON:
		return s, nil
	}
	return "", fmt.Errorf("unknown output format %q (use text or json)", s)
}

type errorDoc struct {
	Code    core.ErrorCode `json:"code"`
	Message string         `json:"message"`
}

type attemptDoc struct {
	Number       int             `json:"number"`
	StartedAt    string          `json:"started_at"`
	DurationMS   int64           `json:"duration_ms"`
	Acceptance   core.Acceptance `json:"acceptance"`
	Retryable    bool            `json:"retryable"`
	StatusCode   int             `json:"status_code,omitempty"`
	RetryAfterMS int64           `json:"retry_after_ms,omitempty"`
	WaitMS       int64           `json:"wait_ms,omitempty"`
	Error        *errorDoc       `json:"error,omitempty"`
}

type resultDoc struct {
	Feather    string          `json:"feather"`
	Type       string          `json:"type"`
	Groups     []string        `json:"groups"`
	Status     core.Status     `json:"status"`
	Acceptance core.Acceptance `json:"acceptance"`
	Attempts   int             `json:"attempts"`
	DurationMS int64           `json:"duration_ms"`
	Error      *errorDoc       `json:"error"`
	Skipped    *core.SkipInfo  `json:"skipped"`
	History    []attemptDoc    `json:"history"`
}

type reportDoc struct {
	SchemaVersion int          `json:"schema_version"`
	Outcome       core.Outcome `json:"outcome"`
	Canceled      bool         `json:"canceled"`
	DryRun        bool         `json:"dry_run"`
	DurationMS    int64        `json:"duration_ms"`
	Level         *core.Level  `json:"level"`
	Summary       core.Summary `json:"summary"`
	Results       []resultDoc  `json:"results"`
	// Error is the error that stopped the command before any delivery
	// (outcome "error"), null otherwise.
	Error *errorDoc `json:"error"`
}

// OutcomeError is the outcome of a command that failed before delivering
// anything (exit status 2, or 130 when interrupted).
const OutcomeError core.Outcome = "error"

// Error codes of a failed command.
const (
	CodeUsage            = "usage"             // the command line (after the flags were parsed)
	CodeConfig           = "config"            // a configuration file (syntax, validation, lock, concurrent change)
	CodeUntrustedProject = "untrusted_project" // ./.kjak.toml is not trusted
	CodeRouting          = "routing"           // a target (unknown, disabled, filtered out)
	CodeCredential       = "credential"        // a secret that does not resolve or is not acceptable
	CodeInput            = "input"             // the message or its input
	CodeNotFound         = "not_found"         // a named feather, group or template
	CodeInterrupted      = "interrupted"       // SIGINT
	CodeError            = "error"             // anything else
)

// WriteErrorJSON writes the document of a command that failed with code. A
// send (report) writes the shape of its report, empty, with the error; the
// other commands write schema_version, outcome and error. The message must
// already be free of secrets.
func WriteErrorJSON(w io.Writer, report bool, code, message string) error {
	e := &errorDoc{Code: core.ErrorCode(code), Message: message}
	var doc any = struct {
		SchemaVersion int          `json:"schema_version"`
		Outcome       core.Outcome `json:"outcome"`
		Error         *errorDoc    `json:"error"`
	}{core.SchemaVersion, OutcomeError, e}
	if report {
		doc = reportDoc{SchemaVersion: core.SchemaVersion, Outcome: OutcomeError, Results: []resultDoc{}, Error: e}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func ms(d time.Duration) int64 { return d.Milliseconds() }

func errDoc(e *core.ErrorInfo) *errorDoc {
	if e == nil {
		return nil
	}
	return &errorDoc{Code: e.Code, Message: e.Message}
}

// WriteJSON writes the report document of r.
func WriteJSON(w io.Writer, r core.Report, level core.Level, dryRun bool) error {
	doc := reportDoc{
		SchemaVersion: core.SchemaVersion,
		Outcome:       r.Outcome,
		Canceled:      r.Canceled,
		DryRun:        dryRun,
		DurationMS:    ms(r.Duration),
		Level:         &level,
		Summary:       r.Summary,
		Results:       make([]resultDoc, 0, len(r.Results)),
	}
	for _, res := range r.Results {
		rd := resultDoc{
			Feather: res.Feather, Type: res.Type, Groups: res.Groups,
			Status: res.Status, Acceptance: res.Acceptance, Attempts: res.Attempts,
			DurationMS: ms(res.Duration), Error: errDoc(res.Error), Skipped: res.Skipped,
			History: make([]attemptDoc, 0, len(res.History)),
		}
		if rd.Groups == nil {
			rd.Groups = []string{}
		}
		for _, a := range res.History {
			rd.History = append(rd.History, attemptDoc{
				Number: a.Number, StartedAt: a.StartedAt.UTC().Format(time.RFC3339Nano),
				DurationMS: ms(a.Duration), Acceptance: a.Acceptance, Retryable: a.Retryable,
				StatusCode: a.StatusCode, RetryAfterMS: ms(a.RetryAfter), WaitMS: ms(a.Wait),
				Error: errDoc(a.Error),
			})
		}
		doc.Results = append(doc.Results, rd)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// WriteText writes one line per result and a summary line.
func WriteText(w io.Writer, r core.Report, dryRun bool) error {
	var b strings.Builder
	for _, res := range r.Results {
		name := fmt.Sprintf("%s (%s)", res.Feather, res.Type)
		switch res.Status {
		case core.StatusPlanned:
			fmt.Fprintf(&b, "plan  %s\n", name)
		case core.StatusDelivered:
			fmt.Fprintf(&b, "ok    %s delivered in %s (%s)\n", name, round(res.Duration), attempts(res.Attempts))
		case core.StatusSkipped:
			fmt.Fprintf(&b, "skip  %s: %s", name, res.Skipped.Reason)
			if res.Skipped.Group != "" {
				fmt.Fprintf(&b, " (group %s)", res.Skipped.Group)
			}
			b.WriteString("\n")
		default:
			mark, note := "FAIL", ""
			if res.Acceptance == core.AcceptanceUnknown {
				mark, note = "????", " [acceptance unknown: it may have been delivered; not retried]"
			}
			msg := ""
			if res.Error != nil {
				msg = res.Error.Message
			}
			fmt.Fprintf(&b, "%s  %s failed (%s): %s%s\n", mark, name, attempts(res.Attempts), msg, note)
		}
	}
	s := r.Summary
	if dryRun {
		fmt.Fprintf(&b, "dry run: %d target(s), %d skipped; nothing was sent\n", s.Total-s.Skipped, s.Skipped)
	} else {
		fmt.Fprintf(&b, "%s: %d delivered, %d failed", r.Outcome, s.Delivered, s.Failed)
		if s.Unknown > 0 {
			fmt.Fprintf(&b, " (%d with unknown acceptance)", s.Unknown)
		}
		fmt.Fprintf(&b, ", %d skipped\n", s.Skipped)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func attempts(n int) string {
	if n == 1 {
		return "1 attempt"
	}
	return fmt.Sprintf("%d attempts", n)
}

func round(d time.Duration) time.Duration {
	if d < time.Second {
		return d.Round(time.Millisecond)
	}
	return d.Round(10 * time.Millisecond)
}
