package credential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Redacted replaces a secret in every output.
const Redacted = "[REDACTED]"

// Secret holds a resolved secret. Every way of printing it (fmt verbs, JSON,
// text marshaling) prints Redacted; only Reveal returns the value.
type Secret struct {
	value string
}

func NewSecret(v string) Secret { return Secret{value: v} }

// Reveal returns the secret, for the one place that needs it (a request).
func (s Secret) Reveal() string { return s.value }

func (s Secret) IsZero() bool { return s.value == "" }

func (s Secret) String() string               { return Redacted }
func (s Secret) GoString() string             { return Redacted }
func (s Secret) Format(f fmt.State, _ rune)   { fmt.Fprint(f, Redacted) }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + Redacted + `"`), nil }
func (s Secret) MarshalText() ([]byte, error) { return []byte(Redacted), nil }

// Redactor removes registered secrets from text. It is the last defense: the
// adapters do not put secrets into errors in the first place.
type Redactor struct {
	mu      sync.RWMutex
	secrets []string
}

// minRedactLength: shorter secrets are not searched for (they would redact
// ordinary words); a secret that short is refused by no one, but is rare.
const minRedactLength = 4

// Add registers s and the forms it takes in URLs, query strings, JSON and
// single-line text. A URL secret also registers its path and query, which
// carry the token.
func (r *Redactor) Add(s Secret) {
	v := s.Reveal()
	if len(v) < minRedactLength {
		return
	}
	forms := []string{v, url.QueryEscape(v), url.PathEscape(v)}
	if u, err := url.Parse(v); err == nil && u.Host != "" {
		if len(u.Path) > 1 {
			forms = append(forms, u.Path, strings.TrimPrefix(u.Path, "/"))
		}
		if u.RawQuery != "" {
			forms = append(forms, u.RawQuery)
		}
	}
	// as a JSON string (an API that echoes it in a JSON body), with and
	// without the escaping of <, > and &, and with "/" escaped as "\/"
	forms = append(forms, jsonForms(v)...)
	// as error text is printed: control characters (the line breaks of a
	// PEM key) made spaces, or whitespace runs collapsed
	forms = append(forms, Printable(v), strings.Join(strings.Fields(v), " "))
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range forms {
		if len(f) >= minRedactLength && !slices.Contains(r.secrets, f) {
			r.secrets = append(r.secrets, f)
		}
	}
	// longest first: a secret that contains another is replaced whole
	sort.SliceStable(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
}

func jsonForms(v string) []string {
	var forms []string
	for _, html := range []bool{true, false} {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(html)
		if enc.Encode(v) != nil {
			continue
		}
		q := strings.TrimSuffix(b.String(), "\n")
		q = q[1 : len(q)-1] // the quotes
		forms = append(forms, q, strings.ReplaceAll(q, "/", `\/`))
	}
	return forms
}

// Redact replaces every registered secret in text.
func (r *Redactor) Redact(text string) string {
	if r == nil {
		return text
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.secrets {
		text = strings.ReplaceAll(text, s, Redacted)
	}
	return text
}

// Printable makes every control character of s a space (a line break, an
// escape sequence's ESC): what kjak prints of an untrusted text.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// Clip is untrusted text (a response body, a plugin's message or stderr)
// made printable by clean and cut to max runes, the start kept (or the end,
// with keepEnd), with "..." where it was cut. Every registered secret is
// redacted before the text is changed, in its cleaned form after, and a
// secret the cut leaves part of (at least minRedactLength bytes of its
// start, or of its end with keepEnd) is redacted too. A nil Redactor only
// cleans and cuts.
func (r *Redactor) Clip(text string, clean func(string) string, max int, keepEnd bool) string {
	text = r.Redact(text)
	text = clean(text)
	if r != nil {
		r.mu.RLock()
		for _, s := range r.secrets {
			if c := clean(s); c != s && len(c) >= minRedactLength {
				text = strings.ReplaceAll(text, c, Redacted)
			}
		}
		r.mu.RUnlock()
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if keepEnd {
		cut := len(text) - len(string(runes[len(runes)-max:]))
		return "..." + r.redactCut(text[keepRedacted(text, cut, true):], clean, true)
	}
	cut := len(string(runes[:max]))
	return r.redactCut(text[:keepRedacted(text, cut, false)], clean, false) + "..."
}

// keepRedacted moves a cut at byte cut of text out of a Redacted marker
// (after it, or before it with atStart), so the marker is kept whole.
func keepRedacted(text string, cut int, atStart bool) int {
	for i := 0; ; {
		j := strings.Index(text[i:], Redacted)
		if j < 0 {
			return cut
		}
		start := i + j
		end := start + len(Redacted)
		if start >= cut {
			return cut
		}
		if cut < end {
			if atStart {
				return start
			}
			return end
		}
		i = end
	}
}

// redactCut redacts the end of text when it is the start of a secret (or
// the start of text when it is the end of one, atStart): what a cut leaves
// of it.
func (r *Redactor) redactCut(text string, clean func(string) string, atStart bool) string {
	if r == nil {
		return text
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	longest := 0
	for _, s := range r.secrets {
		for _, f := range []string{s, clean(s)} {
			for n := min(len(f), len(text)); n >= minRedactLength && n > longest; n-- {
				if atStart && strings.HasPrefix(text, f[len(f)-n:]) || !atStart && strings.HasSuffix(text, f[:n]) {
					longest = n
					break
				}
			}
		}
	}
	if longest == 0 {
		return text
	}
	if atStart {
		return Redacted + text[longest:]
	}
	return text[:len(text)-longest] + Redacted
}
