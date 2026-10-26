// Package credential resolves the secrets of the configuration (webhook URLs,
// SMTP passwords) at run time, and keeps them out of every output.
//
// A credential reference names exactly one source:
//
//	webhook_url = { env = "KJAK_SLACK_WEBHOOK" }
//	webhook_url = { keychain = { service = "kjak", account = "dev_slack" } }
//	webhook_url = { secret = "dev_slack.webhook_url" }
//	webhook_url = { literal = "https://hooks.slack.com/services/..." }
//
// A secret reference names an entry of a secrets file (a TOML file of its
// own, kept out of the configuration: configure.SecretsFile); a literal puts
// the secret in the configuration itself and is deprecated.
package credential

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Source is the kind of a reference.
type Source string

const (
	SourceEnv      Source = "env"
	SourceKeychain Source = "keychain"
	SourceLiteral  Source = "literal"
	SourceSecret   Source = "secret"
)

// KeychainRef names an entry of the OS keychain.
type KeychainRef struct {
	Service string `toml:"service" json:"service"`
	Account string `toml:"account" json:"account"`
}

// Ref is a credential reference as the configuration holds it. Exactly one
// field must be set (Validate).
type Ref struct {
	Env      *string      `toml:"env,omitempty"`
	Keychain *KeychainRef `toml:"keychain,omitempty,inline"`
	Literal  *string      `toml:"literal,omitempty"`
	// Secret names an entry of a secrets file ("slack.bot_token").
	Secret *string `toml:"secret,omitempty"`
}

// ErrCredential wraps every error of a credential.
var ErrCredential = errors.New("credential error")

// Source tells which source the reference names ("" when none or several).
func (r *Ref) Source() Source {
	if r == nil {
		return ""
	}
	var s []Source
	if r.Env != nil {
		s = append(s, SourceEnv)
	}
	if r.Keychain != nil {
		s = append(s, SourceKeychain)
	}
	if r.Literal != nil {
		s = append(s, SourceLiteral)
	}
	if r.Secret != nil {
		s = append(s, SourceSecret)
	}
	if len(s) != 1 {
		return ""
	}
	return s[0]
}

// Validate checks that exactly one source is named, and named completely.
func (r *Ref) Validate() error {
	if r == nil {
		return fmt.Errorf("%w: missing", ErrCredential)
	}
	n := 0
	for _, set := range []bool{r.Env != nil, r.Keychain != nil, r.Literal != nil, r.Secret != nil} {
		if set {
			n++
		}
	}
	switch {
	case n == 0:
		return fmt.Errorf("%w: name one of env, keychain, secret or literal", ErrCredential)
	case n > 1:
		return fmt.Errorf("%w: name only one of env, keychain, secret or literal", ErrCredential)
	}
	switch r.Source() {
	case SourceEnv:
		if !validEnvName(*r.Env) {
			return fmt.Errorf("%w: env %q is not a valid environment variable name", ErrCredential, *r.Env)
		}
	case SourceKeychain:
		if strings.TrimSpace(r.Keychain.Service) == "" || strings.TrimSpace(r.Keychain.Account) == "" {
			return fmt.Errorf("%w: keychain needs both service and account", ErrCredential)
		}
	case SourceLiteral:
		if *r.Literal == "" {
			return fmt.Errorf("%w: literal is empty", ErrCredential)
		}
	case SourceSecret:
		if err := ValidSecretName(*r.Secret); err != nil {
			return fmt.Errorf("%w: %v", ErrCredential, err)
		}
	}
	return nil
}

// Describe tells where the secret comes from without telling the secret.
func (r *Ref) Describe() string {
	switch r.Source() {
	case SourceEnv:
		return "env:" + *r.Env
	case SourceKeychain:
		return "keychain:" + r.Keychain.Service + "/" + r.Keychain.Account
	case SourceLiteral:
		return "literal:" + Redacted
	case SourceSecret:
		return "secret:" + *r.Secret
	}
	return "(invalid)"
}

// Secret names: dot-separated segments ("slack.bot_token"), each a letter
// or a digit followed by letters, digits, '_' or '-' (at most 64), at most
// MaxSecretDepth segments and MaxSecretNameLength bytes in all. A segment
// never holds a dot, so a name maps to exactly one key path of a secrets
// file.
const (
	MaxSecretDepth      = 8
	MaxSecretNameLength = 255
)

var secretSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ValidSecretName checks the name of a secret reference.
func ValidSecretName(name string) error {
	if name == "" || len(name) > MaxSecretNameLength {
		return fmt.Errorf("invalid secret name %q (dot-separated segments of letters, digits, '_' and '-'; at most %d bytes)", name, MaxSecretNameLength)
	}
	parts := strings.Split(name, ".")
	if len(parts) > MaxSecretDepth {
		return fmt.Errorf("invalid secret name %q (at most %d segments)", name, MaxSecretDepth)
	}
	for _, p := range parts {
		if !secretSegment.MatchString(p) {
			return fmt.Errorf("invalid secret name %q (dot-separated segments of letters, digits, '_' and '-', each starting with a letter or a digit)", name)
		}
	}
	return nil
}

// SecretSource looks up the entries of the secrets files that a secret
// reference may read (configure.Loaded.SecretSource: which files depends on
// the configuration file the reference is in). Its errors name the secret,
// never its value.
type SecretSource interface {
	LookupSecret(ctx context.Context, name string) (Secret, error)
}

func validEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c == '_', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// Resolver resolves references. Its zero value is not usable: use
// NewResolver.
type Resolver struct {
	lookupEnv func(string) (string, bool)
	keychain  Keychain
	secrets   SecretSource
}

// NewResolver resolves env references with lookupEnv (os.LookupEnv) and
// keychain references with kc (DefaultKeychain()).
func NewResolver(lookupEnv func(string) (string, bool), kc Keychain) *Resolver {
	return &Resolver{lookupEnv: lookupEnv, keychain: kc}
}

// WithSecrets is a copy of res that resolves secret references from src
// (nil: a secret reference is an error).
func (res *Resolver) WithSecrets(src SecretSource) *Resolver {
	c := *res
	c.secrets = src
	return &c
}

// Resolve returns the secret of r. Its errors name the reference, never the
// value.
func (res *Resolver) Resolve(ctx context.Context, r *Ref) (Secret, error) {
	if err := r.Validate(); err != nil {
		return Secret{}, err
	}
	switch r.Source() {
	case SourceEnv:
		v, ok := res.lookupEnv(*r.Env)
		if !ok || v == "" {
			return Secret{}, fmt.Errorf("%w: environment variable %s is not set", ErrCredential, *r.Env)
		}
		return NewSecret(v), nil
	case SourceKeychain:
		if res.keychain == nil {
			return Secret{}, fmt.Errorf("%w: %s: %v", ErrCredential, r.Describe(), ErrKeychainUnsupported)
		}
		v, err := res.keychain.Lookup(ctx, r.Keychain.Service, r.Keychain.Account)
		if err != nil {
			return Secret{}, fmt.Errorf("%w: %s: %v", ErrCredential, r.Describe(), err)
		}
		if v == "" {
			return Secret{}, fmt.Errorf("%w: %s: the keychain entry is empty", ErrCredential, r.Describe())
		}
		return NewSecret(v), nil
	case SourceSecret:
		if res.secrets == nil {
			return Secret{}, fmt.Errorf("%w: %s: no secrets file is available here", ErrCredential, r.Describe())
		}
		v, err := res.secrets.LookupSecret(ctx, *r.Secret)
		if err != nil {
			if errors.Is(err, ErrCredential) {
				return Secret{}, err
			}
			return Secret{}, fmt.Errorf("%w: %s: %v", ErrCredential, r.Describe(), err)
		}
		if v.IsZero() {
			return Secret{}, fmt.Errorf("%w: %s: the secret is empty", ErrCredential, r.Describe())
		}
		return v, nil
	default:
		return NewSecret(*r.Literal), nil
	}
}
