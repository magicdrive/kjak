package core

import (
	"errors"
	"strings"
	"testing"
)

func TestMessageValidate(t *testing.T) {
	good := []Message{
		{Body: "hi", Level: LevelInfo},
		{Title: "Deploy", Body: "line 1\nline 2\ttab", Level: LevelCritical, URL: "https://example.com/x?y=1"},
		{Body: strings.Repeat("あ", MaxBodyLength), Level: LevelSuccess},
	}
	for _, m := range good {
		if err := m.Validate(); err != nil {
			t.Errorf("%+v: %v", m.Title, err)
		}
	}
	bad := map[string]Message{
		"empty body":     {Body: "  \n", Level: LevelInfo},
		"no level":       {Body: "x"},
		"unknown level":  {Body: "x", Level: "loud"},
		"title newline":  {Title: "a\r\nBcc: x@evil", Body: "x", Level: LevelInfo},
		"title control":  {Title: "a\x1b[31m", Body: "x", Level: LevelInfo},
		"body control":   {Body: "a\x00b", Level: LevelInfo},
		"url scheme":     {Body: "x", Level: LevelInfo, URL: "javascript:alert(1)"},
		"url relative":   {Body: "x", Level: LevelInfo, URL: "/path"},
		"url space":      {Body: "x", Level: LevelInfo, URL: "https://e.com/a b"},
		"body too long":  {Body: strings.Repeat("x", MaxBodyLength+1), Level: LevelInfo},
		"title too long": {Title: strings.Repeat("x", MaxTitleLength+1), Body: "x", Level: LevelInfo},
		"invalid utf8":   {Body: "\xff", Level: LevelInfo},
	}
	for name, m := range bad {
		if err := m.Validate(); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestParseLevel(t *testing.T) {
	for _, s := range []string{"info", "SUCCESS", " Warning ", "error", "critical"} {
		if _, err := ParseLevel(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	if _, err := ParseLevel("debug"); err == nil {
		t.Error("debug accepted")
	}
	m := Message{Body: "x"}
	m.Normalize()
	if m.Level != LevelInfo {
		t.Error("default level")
	}
}

func TestCritical_ResultAggregation(t *testing.T) {
	r := func(s Status, a Acceptance) Result { return Result{Status: s, Acceptance: a} }
	cases := []struct {
		name     string
		results  []Result
		canceled bool
		outcome  Outcome
		summary  Summary
	}{
		{"none", nil, false, OutcomeSuccess, Summary{}},
		{"all delivered", []Result{r(StatusDelivered, AcceptanceConfirmed), r(StatusDelivered, AcceptanceConfirmed)}, false, OutcomeSuccess, Summary{Total: 2, Delivered: 2}},
		{"delivered and skipped", []Result{r(StatusDelivered, AcceptanceConfirmed), r(StatusSkipped, AcceptanceNone)}, false, OutcomeSuccess, Summary{Total: 2, Delivered: 1, Skipped: 1}},
		{"all skipped", []Result{r(StatusSkipped, AcceptanceNone)}, false, OutcomeSuccess, Summary{Total: 1, Skipped: 1}},
		{"partial", []Result{r(StatusDelivered, AcceptanceConfirmed), r(StatusFailed, AcceptanceRejected)}, false, OutcomePartial, Summary{Total: 2, Delivered: 1, Failed: 1}},
		{"unknown counts as failed", []Result{r(StatusFailed, AcceptanceUnknown)}, false, OutcomeFailure, Summary{Total: 1, Failed: 1, Unknown: 1}},
		{"failure", []Result{r(StatusFailed, AcceptanceRejected), r(StatusSkipped, AcceptanceNone)}, false, OutcomeFailure, Summary{Total: 2, Failed: 1, Skipped: 1}},
		{"canceled wins", []Result{r(StatusDelivered, AcceptanceConfirmed)}, true, OutcomeCanceled, Summary{Total: 1, Delivered: 1}},
		{"planned", []Result{r(StatusPlanned, AcceptanceNone)}, false, OutcomeSuccess, Summary{Total: 1}},
	}
	for _, tc := range cases {
		rep := Aggregate(tc.results, tc.canceled, 0)
		if rep.Outcome != tc.outcome || rep.Summary != tc.summary || rep.Results == nil {
			t.Errorf("%s: %s %+v", tc.name, rep.Outcome, rep.Summary)
		}
		if len(rep.Results) != len(tc.results) {
			t.Errorf("%s: results reordered or lost", tc.name)
		}
	}
}
