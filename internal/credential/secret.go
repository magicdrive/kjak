package credential

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
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

// Add registers s and the forms it takes in URLs and query strings. A URL
// secret also registers its path and query, which carry the token.
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
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range forms {
		if len(f) >= minRedactLength {
			r.secrets = append(r.secrets, f)
		}
	}
	// longest first: a secret that contains another is replaced whole
	sort.SliceStable(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
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
