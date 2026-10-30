// Package configure reads, merges, validates and writes kjak's TOML
// configuration: the global file (XDG) and the project file (.kjak.toml),
// which is used only once it is trusted (trust.go).
package configure

import (
	"fmt"
	"time"

	"github.com/magicdrive/kjak/internal/credential"
)

// Feather types.
const (
	TypeSlack   = "slack"
	TypeDiscord = "discord"
	TypeEmail   = "email"
	// TypeHTTP is a request to any HTTP API (feather/httpapi); not to be
	// confused with feather/webhook, the sender of slack and discord.
	TypeHTTP = "http"
	// TypeExternal runs a plugin: the executable kjak-feather-<plugin>
	// (feather/external, docs/plugins.md).
	TypeExternal = "external"
	// TypeOS is a notification of the operating system kjak runs on
	// (feather/osnotify, docs/os.md).
	TypeOS = "os"
)

// FeatherTypes are the types kjak implements.
var FeatherTypes = []string{TypeOS, TypeSlack, TypeDiscord, TypeEmail, TypeHTTP, TypeExternal}

// UnsupportedFeatherTypes are names kept for no type of their own; a feather
// of one of them is a configuration error, never a silent no-op.
var UnsupportedFeatherTypes = map[string]string{
	"native":  `OS notifications are type = "os"`,
	"macos":   `OS notifications are type = "os" (on every OS)`,
	"linux":   `OS notifications are type = "os" (on every OS)`,
	"windows": `OS notifications are type = "os" (on every OS)`,
}

// Defaults of the engine (spec: timeout 10s, 2 retries, 5 at a time).
const (
	DefaultTimeout     = 10 * time.Second
	DefaultMaxRetries  = 2
	DefaultConcurrency = 5

	MaxTimeout     = 10 * time.Minute
	MaxMaxRetries  = 10
	MaxConcurrency = 64
)

// File is one configuration file.
type File struct {
	Defaults  Defaults                  `toml:"defaults,omitempty"`
	Feathers  map[string]FeatherConfig  `toml:"feathers,omitempty"`
	Groups    map[string]GroupConfig    `toml:"groups,omitempty"`
	Templates map[string]TemplateConfig `toml:"templates,omitempty"`
}

// Defaults are merged field by field: a field the project file sets wins.
type Defaults struct {
	Timeout     *Duration `toml:"timeout,omitempty"`
	MaxRetries  *int      `toml:"max_retries,omitempty"`
	Concurrency *int      `toml:"concurrency,omitempty"`
	// Level is the level of a message that names none.
	Level *string `toml:"level,omitempty"`
	// Targets are the feathers and groups of a send that names none.
	Targets []string `toml:"targets,omitempty"`
}

// FeatherConfig is one named feather. It is replaced whole by a project
// feather of the same name.
type FeatherConfig struct {
	Type    string `toml:"type"`
	Enabled *bool  `toml:"enabled,omitempty"`
	// Levels limits the feather to these levels (empty: every level).
	Levels []string `toml:"levels,omitempty"`

	// slack, discord
	WebhookURL *credential.Ref `toml:"webhook_url,omitempty,inline"`
	// discord: the display name of the webhook's messages
	Username string `toml:"username,omitempty"`

	// email (SMTP)
	Host     string          `toml:"host,omitempty"`
	Port     int             `toml:"port,omitempty"`
	Security string          `toml:"security,omitempty"` // starttls (default), tls, none
	User     string          `toml:"user,omitempty"`
	Password *credential.Ref `toml:"password,omitempty,inline"`
	From     string          `toml:"from,omitempty"`
	To       []string        `toml:"to,omitempty"`
	// SubjectPrefix starts every subject, e.g. "[kjak]".
	SubjectPrefix string `toml:"subject_prefix,omitempty"`

	// http (docs/http.md): exactly one of URL and URLSecret
	URL       string          `toml:"url,omitempty"`
	URLSecret *credential.Ref `toml:"url_secret,omitempty,inline"`
	Method    string          `toml:"method,omitempty"` // POST (default), GET, PUT, PATCH, DELETE
	// Headers are sent as they are (no secret: use Auth); Query is added to
	// the URL's query (no secret is ever put there).
	Headers          map[string]string `toml:"headers,omitempty"`
	Query            map[string]string `toml:"query,omitempty"`
	Auth             *HTTPAuth         `toml:"auth,omitempty"`
	Body             *HTTPBody         `toml:"body,omitempty"`
	Timeout          *Duration         `toml:"timeout,omitempty"`        // of one attempt
	SuccessStatus    []string          `toml:"success_status,omitempty"` // "2xx" (default), "200", ...
	MaxResponseBytes int64             `toml:"max_response_bytes,omitempty"`
	// The SSRF policy: https only unless AllowHTTP; no loopback, link-local,
	// private, ... address unless AllowNetworks (CIDR) or, for private
	// networks, AllowHosts (names) allow it; redirects not followed unless
	// MaxRedirects.
	AllowHTTP         bool     `toml:"allow_http,omitempty"`
	AllowNetworks     []string `toml:"allow_networks,omitempty"`
	AllowHosts        []string `toml:"allow_hosts,omitempty"`
	MaxRedirects      int      `toml:"max_redirects,omitempty"`
	IdempotencyKey    bool     `toml:"idempotency_key,omitempty"`
	IdempotencyHeader string   `toml:"idempotency_header,omitempty"`
	// Retry is the retry policy of an http or an external feather.
	Retry *HTTPRetry `toml:"retry,omitempty"`

	// external (docs/plugins.md); Timeout and Retry apply too
	Plugin string `toml:"plugin,omitempty"`
	// Path and SHA256 pin the executable: an absolute path instead of the
	// PATH search, the SHA-256 of its content.
	Path   string `toml:"path,omitempty"`
	SHA256 string `toml:"sha256,omitempty"`
	// PluginSecrets are the only secrets the plugin gets, by the names it
	// knows them by; Settings are plain values; Env names the environment
	// variables it may see besides kjak's minimal set.
	PluginSecrets map[string]*credential.Ref `toml:"secrets,omitempty"`
	Settings      map[string]string          `toml:"settings,omitempty"`
	Env           []string                   `toml:"env,omitempty"`
}

// HTTP authentication types.
const (
	AuthNone   = "none"
	AuthBearer = "bearer"
	AuthBasic  = "basic"
	AuthHeader = "header"
)

// HTTPAuth is the authentication of an http feather; every secret is a
// credential reference.
type HTTPAuth struct {
	Type     string          `toml:"type"`
	Token    *credential.Ref `toml:"token,omitempty,inline"`    // bearer
	Username string          `toml:"username,omitempty"`        // basic
	Password *credential.Ref `toml:"password,omitempty,inline"` // basic
	Header   string          `toml:"header,omitempty"`          // header: its name
	Value    *credential.Ref `toml:"value,omitempty,inline"`    // header: its value
}

// HTTP body formats.
const (
	BodyJSON = "json"
	BodyText = "text"
	BodyNone = "none"
)

// HTTPBody is the request body of an http feather: the message as JSON or
// text, or a template of either (text/template, with the message's title,
// body, level, url and the feather's name).
type HTTPBody struct {
	Format      string `toml:"format,omitempty"`
	Template    string `toml:"template,omitempty"`
	ContentType string `toml:"content_type,omitempty"`
}

// Retry policies (http: safe, never, always; external: never, plugin).
const (
	RetrySafe   = "safe"   // GET, PUT, DELETE; POST and PATCH with an idempotency key
	RetryNever  = "never"  // one attempt
	RetryAlways = "always" // POST and PATCH too, without a key: may deliver twice
	RetryPlugin = "plugin" // external: as the plugin's answer says (rejected and retryable)
)

// HTTPRetry is the retry policy of an http feather.
type HTTPRetry struct {
	Policy string `toml:"policy,omitempty"`
	// MaxAttempts caps the attempts (first included) below the send's
	// 1 + max_retries (0: no cap of its own).
	MaxAttempts int `toml:"max_attempts,omitempty"`
}

// IsEnabled: a feather is enabled unless it says otherwise.
func (f FeatherConfig) IsEnabled() bool { return f.Enabled == nil || *f.Enabled }

// GroupConfig is a named list of feathers (no groups: nesting is not
// supported).
//
// GroupConfig and TemplateConfig have no json tags on purpose: `config show
// -o json` prints their Go field names ("Feathers"; "Title", "Body", "Level",
// "URL"), as v0.0.4 did, and the v1 contract keeps them (docs/stability.md,
// TestContractConfigShowKeys).
type GroupConfig struct {
	Feathers []string `toml:"feathers"`
}

// TemplateConfig is a message template (internal/template).
type TemplateConfig struct {
	Title string `toml:"title,omitempty"`
	Body  string `toml:"body"`
	Level string `toml:"level,omitempty"`
	URL   string `toml:"url,omitempty"`
}

// Duration is a time.Duration written as a Go duration string ("10s").
type Duration struct{ time.Duration }

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q (e.g. \"10s\", \"1m30s\")", string(b))
	}
	d.Duration = v
	return nil
}

// Settings are the defaults resolved: every field set.
type Settings struct {
	Timeout     time.Duration
	MaxRetries  int
	Concurrency int
	Level       string
	Targets     []string
}

// Settings resolves the defaults of f.
func (f *File) Settings() Settings {
	s := Settings{Timeout: DefaultTimeout, MaxRetries: DefaultMaxRetries, Concurrency: DefaultConcurrency, Level: "info"}
	if f.Defaults.Timeout != nil {
		s.Timeout = f.Defaults.Timeout.Duration
	}
	if f.Defaults.MaxRetries != nil {
		s.MaxRetries = *f.Defaults.MaxRetries
	}
	if f.Defaults.Concurrency != nil {
		s.Concurrency = *f.Defaults.Concurrency
	}
	if f.Defaults.Level != nil {
		s.Level = *f.Defaults.Level
	}
	s.Targets = append([]string(nil), f.Defaults.Targets...)
	return s
}
