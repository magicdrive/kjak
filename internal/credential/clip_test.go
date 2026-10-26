package credential

import (
	"strings"
	"testing"
)

func oneLine(s string) string { return strings.TrimSpace(Printable(s)) }

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// Clip redacts before it changes the text: a secret cut by the shortening,
// or one whose line breaks the cleaning turns into spaces, leaves nothing.
func TestCritical_ClipRedactsBeforeItCuts(t *testing.T) {
	const tok = "tok-0123456789abcdef"
	const pem = "-----BEGIN K-----\nAAAABBBBCCCC\r\nDDDDEEEEFFFF\n-----END K-----"
	r := &Redactor{}
	r.Add(NewSecret(tok))
	r.Add(NewSecret(pem))
	r.Add(NewSecret("https://hooks.example/a/b<c>&d"))
	pad := strings.Repeat("x", 20)
	for _, c := range []struct {
		name, in string
		clean    func(string) string
		max      int
		keepEnd  bool
		leak     []string
	}{
		{"cut at the end", pad + tok + " tail", oneLine, 25, false, []string{"tok-0"}},
		{"cut at the end, 4 bytes left", pad[:16] + tok, oneLine, 20, false, []string{"tok-"}},
		{"cut at the start", "head " + tok + pad, oneLine, 30, true, []string{"cdef", "89ab"}},
		{"line breaks made spaces", "key: " + pem + " end", oneLine, 1000, false, []string{"AAAABBBB", "DDDD", "BEGIN K", "END K"}},
		{"whitespace collapsed", "key: " + pem + " end", collapse, 1000, false, []string{"AAAABBBB", "DDDD", "BEGIN K"}},
		{"line breaks of the text", strings.ReplaceAll(pem, "\n", "\r\n"), collapse, 1000, false, []string{"AAAABBBB", "DDDD"}},
		{"JSON escaped", `{"url":"https:\/\/hooks.example\/a\/b<c>&d"}`, oneLine, 1000, false, []string{"hooks.example", `a\/b`}},
		{"JSON unescaped HTML", `{"url":"https:\/\/hooks.example\/a\/b<c>&d"}`, oneLine, 1000, false, []string{"hooks.example", `a\/b`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := r.Clip(c.in, c.clean, c.max, c.keepEnd)
			for _, l := range c.leak {
				if strings.Contains(out, l) {
					t.Fatalf("%q leaks %q", out, l)
				}
			}
			if !strings.Contains(out, Redacted) {
				t.Fatalf("%q: nothing redacted", out)
			}
		})
	}
}

// Clip without secrets, or with a nil Redactor, only cleans and cuts; text
// that merely shares fewer than four bytes with a secret is kept.
func TestClipKeepsOrdinaryText(t *testing.T) {
	var nilR *Redactor
	if got := nilR.Clip("a\x1b[31mb\nc", oneLine, 100, false); got != "a [31mb c" {
		t.Fatalf("%q", got)
	}
	if got := nilR.Clip("abcdefgh", oneLine, 4, false); got != "abcd..." {
		t.Fatalf("%q", got)
	}
	if got := nilR.Clip("abcdefgh", oneLine, 4, true); got != "...efgh" {
		t.Fatalf("%q", got)
	}
	r := &Redactor{}
	r.Add(NewSecret("tok-0123456789"))
	if got := r.Clip("xxxxxxxxtok", oneLine, 11, false); got != "xxxxxxxxtok" {
		t.Fatalf("not cut: %q", got)
	}
	if got := r.Clip("xxxxxxxxxtok-zzzz", oneLine, 12, false); got != "xxxxxxxxxtok..." {
		t.Fatalf("three bytes are not a secret: %q", got)
	}
}
