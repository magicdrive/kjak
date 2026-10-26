package configure

import (
	"sort"

	"github.com/magicdrive/kjak/internal/credential"
)

// CredentialField is one credential reference of a feather: where it is and
// how to replace it (kjak config migrate-secrets).
type CredentialField struct {
	Feather string
	Field   string // "webhook_url", "password", ...
	Ref     *credential.Ref
	set     func(*credential.Ref)
}

// Set replaces the reference in the feather it was listed from.
func (c CredentialField) Set(r *credential.Ref) { c.set(r) }

// Credentials lists every credential reference of fc, in a stable order.
// The Set of an entry changes fc.
func (fc *FeatherConfig) Credentials(feather string) []CredentialField {
	var out []CredentialField
	add := func(field string, p **credential.Ref) {
		if *p != nil {
			out = append(out, CredentialField{Feather: feather, Field: field, Ref: *p, set: func(r *credential.Ref) { *p = r }})
		}
	}
	add("webhook_url", &fc.WebhookURL)
	add("password", &fc.Password)
	add("url_secret", &fc.URLSecret)
	if fc.Auth != nil {
		add("auth.token", &fc.Auth.Token)
		add("auth.password", &fc.Auth.Password)
		add("auth.value", &fc.Auth.Value)
	}
	return out
}

// Credentials lists every credential reference of f, by feather name.
// The Set of an entry changes f.
func Credentials(f *File) []CredentialField {
	if f == nil {
		return nil
	}
	var out []CredentialField
	for _, name := range sortedKeys(f.Feathers) {
		fc := f.Feathers[name]
		fields := fc.Credentials(name)
		for i := range fields {
			set := fields[i].set
			fields[i].set = func(r *credential.Ref) {
				set(r)
				f.Feathers[name] = fc
			}
		}
		out = append(out, fields...)
	}
	return out
}

// SecretNames are the names of the secret references of f, sorted, each
// once.
func SecretNames(f *File) []string {
	seen := map[string]bool{}
	var names []string
	for _, c := range Credentials(f) {
		if c.Ref.Secret != nil && !seen[*c.Ref.Secret] {
			seen[*c.Ref.Secret] = true
			names = append(names, *c.Ref.Secret)
		}
	}
	sort.Strings(names)
	return names
}

// GrantName is how a trust allows an env or keychain reference of a
// project ("env:NAME", "keychain:SERVICE/ACCOUNT"); "" for other sources.
func GrantName(r *credential.Ref) string {
	switch r.Source() {
	case credential.SourceEnv, credential.SourceKeychain:
		return r.Describe()
	}
	return ""
}

// GatedType: a feather type that sends to a destination of the
// configuration's choosing (a URL, a plugin) rather than to a provider's
// own endpoint. In a project, its env and keychain references must be
// allowed by name when the project is trusted, like its references to the
// personal secrets file.
func GatedType(t string) bool { return t == TypeHTTP }

// GatedNames are the grant names of the env and keychain references of the
// gated feathers of f, sorted, each once.
func GatedNames(f *File) []string {
	seen := map[string]bool{}
	var names []string
	if f == nil {
		return nil
	}
	for _, c := range Credentials(f) {
		if !GatedType(f.Feathers[c.Feather].Type) {
			continue
		}
		if n := GrantName(c.Ref); n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Literals are the literal credentials of f (deprecated: they keep a
// secret in the configuration file).
func Literals(f *File) []CredentialField {
	var out []CredentialField
	for _, c := range Credentials(f) {
		if c.Ref.Literal != nil {
			out = append(out, c)
		}
	}
	return out
}
