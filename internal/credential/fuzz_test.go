package credential

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// For any registered secret (long enough to be searched for), no text that
// contains it keeps it after Redact, in its plain, query-escaped or
// path-escaped form; two secrets registered together are both removed; and
// a Secret never prints or marshals its value.
//
// Secrets that share a character with "[REDACTED]" are skipped: the marker
// itself could then spell (part of) them, which reveals nothing.
func FuzzRedact(f *testing.F) {
	f.Add("xoxb-1234-abcd", "token=", " end", "other-secret")
	f.Add("https://hooks.slack.com/services/t0/b0/xyz", "posting to ", "", "zzzz")
	f.Add("p@ss w0rd/?&", "a", "b", "p@ss")
	f.Add("abcd", "abcabcd", "dabcd", "bcda")
	f.Fuzz(func(t *testing.T, secret, prefix, suffix, other string) {
		usable := func(s string) bool {
			return len(s) >= minRedactLength && !strings.ContainsAny(s, Redacted)
		}
		if !usable(secret) {
			return
		}
		var r Redactor
		s := NewSecret(secret)
		r.Add(s)
		if usable(other) {
			r.Add(NewSecret(other))
		}
		for _, text := range []string{
			prefix + secret + suffix,
			prefix + secret + secret + suffix,
			prefix + url.QueryEscape(secret) + suffix,
			prefix + url.PathEscape(secret) + suffix,
		} {
			out := r.Redact(text)
			if strings.Contains(out, secret) {
				t.Fatalf("Redact(%q) = %q keeps the secret", text, out)
			}
			// a replacement inserts only the marker, so it can neither
			// leave nor create a whole occurrence of the other secret
			if usable(other) && strings.Contains(out, other) {
				t.Fatalf("Redact(%q) = %q keeps the second secret", text, out)
			}
		}
		for _, shown := range []string{s.String(), s.GoString(), fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s %q", s, s, s, s, s)} {
			if strings.Contains(shown, secret) {
				t.Fatalf("a Secret prints its value: %q", shown)
			}
		}
		if b, err := json.Marshal(struct{ S Secret }{s}); err != nil || strings.Contains(string(b), secret) {
			t.Fatalf("a Secret marshals its value: %s %v", b, err)
		}
	})
}
