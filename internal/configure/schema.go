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
)

// FeatherTypes are the types v0.1.0 implements. OS-native notifications are
// a future feather and are refused (UnsupportedFeatherTypes).
var FeatherTypes = []string{TypeSlack, TypeDiscord, TypeEmail}

// UnsupportedFeatherTypes are known but not implemented in v0.1.0; a feather
// of one of them is a configuration error, never a silent no-op.
var UnsupportedFeatherTypes = map[string]string{
	"os":      "OS-native notifications are not supported in kjak v0.1.0",
	"native":  "OS-native notifications are not supported in kjak v0.1.0",
	"macos":   "OS-native notifications are not supported in kjak v0.1.0",
	"linux":   "OS-native notifications are not supported in kjak v0.1.0",
	"windows": "OS-native notifications are not supported in kjak v0.1.0",
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
}

// IsEnabled: a feather is enabled unless it says otherwise.
func (f FeatherConfig) IsEnabled() bool { return f.Enabled == nil || *f.Enabled }

// GroupConfig is a named list of feathers (no groups: nesting is not
// supported in v0.1.0).
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
