package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/core"
)

func sample() core.Report {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return core.Aggregate([]core.Result{
		{Feather: "a", Type: "slack", Status: core.StatusDelivered, Acceptance: core.AcceptanceConfirmed, Attempts: 2, Duration: 1500 * time.Millisecond,
			History: []core.Attempt{
				{Number: 1, StartedAt: at, Duration: 200 * time.Millisecond, Acceptance: core.AcceptanceRejected, Retryable: true, StatusCode: 429, RetryAfter: time.Second, Wait: time.Second, Error: &core.ErrorInfo{Code: core.ErrCodeRateLimited, Message: "HTTP 429"}},
				{Number: 2, StartedAt: at.Add(1200 * time.Millisecond), Duration: 300 * time.Millisecond, Acceptance: core.AcceptanceConfirmed, StatusCode: 200},
			}},
		{Feather: "b", Type: "email", Status: core.StatusFailed, Acceptance: core.AcceptanceUnknown, Attempts: 1, Error: &core.ErrorInfo{Code: core.ErrCodeAmbiguous, Message: "lost"}, History: []core.Attempt{{Number: 1, StartedAt: at, Acceptance: core.AcceptanceUnknown}}},
		{Feather: "c", Type: "discord", Groups: []string{"g"}, Status: core.StatusSkipped, Acceptance: core.AcceptanceNone, Skipped: &core.SkipInfo{Reason: "disabled", Group: "g"}, History: []core.Attempt{}},
	}, false, 2*time.Second)
}

func TestWriteJSONSchema(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, sample(), core.LevelError, false); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema_version", "outcome", "canceled", "dry_run", "duration_ms", "level", "summary", "results"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if doc["schema_version"].(float64) != 1 || doc["outcome"] != "partial" || doc["duration_ms"].(float64) != 2000 {
		t.Fatalf("doc = %v", doc)
	}
	results := doc["results"].([]any)
	a := results[0].(map[string]any)
	for _, k := range []string{"feather", "type", "groups", "status", "acceptance", "attempts", "duration_ms", "error", "skipped", "history"} {
		if _, ok := a[k]; !ok {
			t.Errorf("result lacks %s", k)
		}
	}
	h := a["history"].([]any)[0].(map[string]any)
	if h["retry_after_ms"].(float64) != 1000 || h["wait_ms"].(float64) != 1000 || h["status_code"].(float64) != 429 || h["started_at"] != "2026-10-01T12:00:00Z" {
		t.Fatalf("attempt = %v", h)
	}
	if a["error"] != nil {
		t.Fatal("a delivered result has an error")
	}
}

func TestWriteText(t *testing.T) {
	var buf bytes.Buffer
	_ = WriteText(&buf, sample(), false)
	out := buf.String()
	for _, want := range []string{
		"ok    a (slack) delivered in 1.5s (2 attempts)",
		"????  b (email) failed (1 attempt): lost [acceptance unknown: it may have been delivered; not retried]",
		"skip  c (discord): disabled (group g)",
		"partial: 1 delivered, 1 failed (1 with unknown acceptance), 1 skipped",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Fatal("yaml accepted")
	}
}
