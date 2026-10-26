package credential

import (
	"strings"
	"testing"
)

// A URL secret (a webhook URL, url_secret) is redacted in every form an
// error may quote it: whole, its path with or without the leading slash,
// its query.
func TestCritical_RedactURLSecretParts(t *testing.T) {
	var r Redactor
	r.Add(NewSecret("https://hooks.example.com/services/T0AAA/B0BBB/xyzSECRET?token=tk-SECRET-42"))
	for _, text := range []string{
		"POST https://hooks.example.com/services/T0AAA/B0BBB/xyzSECRET?token=tk-SECRET-42 failed",
		"no such webhook: /services/T0AAA/B0BBB/xyzSECRET",
		"no such webhook: services/T0AAA/B0BBB/xyzSECRET",
		"invalid query token=tk-SECRET-42",
	} {
		if got := r.Redact(text); strings.Contains(got, "SECRET") {
			t.Errorf("Redact(%q) = %q", text, got)
		}
	}
}

// A secret that contains another registered secret is replaced whole: no
// part of the longer one is left next to the marker.
func TestCritical_RedactLongestFirst(t *testing.T) {
	var r Redactor
	r.Add(NewSecret("abcd1234"))
	r.Add(NewSecret("abcd1234efgh5678"))
	if got := r.Redact("key abcd1234efgh5678 and abcd1234."); got != "key [REDACTED] and [REDACTED]." {
		t.Fatalf("Redact = %q", got)
	}
}
