package external

import (
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
)

// Whatever a plugin writes, the outcome keeps kjak's rules: delivered only
// when it says so, an unknown acceptance never retryable, no control
// character in an error.
func FuzzParseResponse(f *testing.F) {
	for _, s := range []string{
		`{"protocol_version":1,"status":"delivered"}`,
		`{"protocol_version":1,"status":"failed","acceptance":"rejected","retryable":true,"retry_after_ms":10,"error":{"code":"x","message":"m"}}`,
		`{"protocol_version":1,"status":"failed","acceptance":"unknown","retryable":true}`,
		`{"protocol_version":2}`, `[]`, `{"protocol_version":1,"status":"delivered"}{}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		o, err := parseResponse(nil, b, time.Now())
		if err != nil {
			return
		}
		if o.Acceptance == core.AcceptanceConfirmed && !strings.Contains(string(b), "delivered") {
			t.Fatalf("confirmed without delivered: %s", b)
		}
		if o.Acceptance == core.AcceptanceUnknown && o.Retryable {
			t.Fatal("an unknown acceptance is retryable")
		}
		if o.Err != nil && strings.ContainsAny(o.Err.Error(), "\x00\x1b\r\n") {
			t.Fatalf("a control character in %q", o.Err)
		}
	})
}

// An accepted plugin name is lower-case ASCII and no built-in type.
func FuzzValidPluginName(f *testing.F) {
	for _, s := range []string{"pagerduty", "Slack", "slack", "a-b_c", "é", "../x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if configure.ValidPluginName(s) != nil {
			return
		}
		for _, r := range s {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
				t.Fatalf("accepted %q", s)
			}
		}
		for _, ty := range configure.FeatherTypes {
			if s == ty {
				t.Fatalf("accepted the built-in type %q", s)
			}
		}
	})
}
