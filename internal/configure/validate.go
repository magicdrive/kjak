package configure

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/magicdrive/kjak/internal/core"
)

// ErrInvalidConfig wraps every validation error.
var ErrInvalidConfig = errors.New("invalid configuration")

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ValidName checks a feather, group or template name.
func ValidName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid name %q (letters, digits, '_', '.', '-'; at most 64; starting with a letter or digit)", name)
	}
	return nil
}

// Validate checks a file on its own: names, defaults, every feather,
// template and the shape of every group. References between entries are
// checked on the merged configuration (ValidateReferences): a project
// group may name a global feather.
func Validate(f *File) error {
	var errs []error
	add := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf(format, a...))
	}
	d := f.Defaults
	if d.Timeout != nil && (d.Timeout.Duration <= 0 || d.Timeout.Duration > MaxTimeout) {
		add("defaults.timeout must be between 0s (exclusive) and %s", MaxTimeout)
	}
	if d.MaxRetries != nil && (*d.MaxRetries < 0 || *d.MaxRetries > MaxMaxRetries) {
		add("defaults.max_retries must be between 0 and %d", MaxMaxRetries)
	}
	if d.Concurrency != nil && (*d.Concurrency < 1 || *d.Concurrency > MaxConcurrency) {
		add("defaults.concurrency must be between 1 and %d", MaxConcurrency)
	}
	if d.Level != nil {
		if _, err := core.ParseLevel(*d.Level); err != nil {
			add("defaults.level: %v", err)
		}
	}
	for _, name := range sortedKeys(f.Feathers) {
		if err := ValidName(name); err != nil {
			errs = append(errs, err)
			continue
		}
		for _, p := range featherProblems(f.Feathers[name]) {
			add("feathers.%s: %s", name, p)
		}
	}
	for _, name := range sortedKeys(f.Groups) {
		if err := ValidName(name); err != nil {
			errs = append(errs, err)
			continue
		}
		if len(f.Groups[name].Feathers) == 0 {
			add("groups.%s: feathers is empty", name)
		}
		seen := map[string]bool{}
		for _, m := range f.Groups[name].Feathers {
			if seen[m] {
				add("groups.%s: feather %q is listed twice", name, m)
			}
			seen[m] = true
		}
	}
	for _, name := range sortedKeys(f.Templates) {
		if err := ValidName(name); err != nil {
			errs = append(errs, err)
			continue
		}
		t := f.Templates[name]
		if strings.TrimSpace(t.Body) == "" {
			add("templates.%s: body is required", name)
		}
		if t.Level != "" && !strings.Contains(t.Level, "{{") {
			if _, err := core.ParseLevel(t.Level); err != nil {
				add("templates.%s: %v", name, err)
			}
		}
	}
	return wrapAll(errs)
}

// ValidateReferences checks the merged configuration: every group member is
// a feather (a group is not: no nesting), every default target exists, and
// no name is both a feather and a group.
func ValidateReferences(f *File) error {
	var errs []error
	add := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf(format, a...))
	}
	for _, name := range sortedKeys(f.Groups) {
		if _, ok := f.Feathers[name]; ok {
			add("%q is both a feather and a group", name)
		}
		for _, m := range f.Groups[name].Feathers {
			if _, ok := f.Feathers[m]; ok {
				continue
			}
			if _, ok := f.Groups[m]; ok {
				add("groups.%s: %q is a group; nested groups are not supported", name, m)
				continue
			}
			add("groups.%s: no feather named %q", name, m)
		}
	}
	for _, t := range f.Defaults.Targets {
		_, isFeather := f.Feathers[t]
		_, isGroup := f.Groups[t]
		if !isFeather && !isGroup {
			add("defaults.targets: no feather or group named %q", t)
		}
	}
	return wrapAll(errs)
}

// ValidateFeather checks one feather: its type, and the fields of its type
// (only those).
func ValidateFeather(fc FeatherConfig) error {
	var errs []error
	for _, p := range featherProblems(fc) {
		errs = append(errs, errors.New(p))
	}
	return wrapAll(errs)
}

func featherProblems(fc FeatherConfig) []string {
	var problems []string
	add := func(format string, a ...any) {
		problems = append(problems, fmt.Sprintf(format, a...))
	}
	for _, l := range fc.Levels {
		if _, err := core.ParseLevel(l); err != nil {
			add("levels: %v", err)
		}
	}
	// text kjak prints (config trust, feather show, errors) or puts in a
	// message header: no control character, which a terminal would run
	// (an escape sequence) or which would break the line
	for _, f := range textFields(fc) {
		if HasControl(f.value) {
			add("%s: contains a control character", f.name)
		}
	}
	webhookFields := fc.WebhookURL != nil || fc.Username != ""
	emailFields := fc.Host != "" || fc.Port != 0 || fc.Security != "" || fc.User != "" || fc.Password != nil ||
		fc.From != "" || len(fc.To) > 0 || fc.SubjectPrefix != ""
	if fc.Type != TypeHTTP && httpFieldsSet(fc) {
		add("%s feathers take no http settings (url, method, headers, query, auth, body, ...)", fc.Type)
	}
	if fc.Type != TypeExternal && externalFieldsSet(fc) {
		add("%s feathers take no plugin settings (plugin, path, sha256, secrets, settings, env)", fc.Type)
	}
	if fc.Type != TypeHTTP && fc.Type != TypeExternal && (fc.Timeout != nil || fc.Retry != nil) {
		add("%s feathers take no timeout or retry of their own (use [defaults])", fc.Type)
	}
	switch fc.Type {
	case TypeOS:
		if webhookFields || emailFields {
			add("os feathers take no webhook_url, username or email settings (they show the message on this machine)")
		}
		if len(fc.AllowNetworks) > 0 || len(fc.AllowHosts) > 0 {
			add("os feathers take no allow_networks or allow_hosts (they make no connection)")
		}
	case TypeExternal:
		if webhookFields || emailFields {
			add("external feathers take no webhook_url, username or email settings (give the plugin settings and secrets)")
		}
		if len(fc.AllowNetworks) > 0 || len(fc.AllowHosts) > 0 {
			add("external feathers take no allow_networks or allow_hosts (a plugin makes its own connections)")
		}
		problems = append(problems, externalProblems(fc)...)
	case TypeHTTP:
		if webhookFields || emailFields {
			add("http feathers take no webhook_url, username or email settings (use url, url_secret, [auth])")
		}
		problems = append(problems, httpProblems(fc)...)
	case TypeSlack, TypeDiscord:
		if emailFields {
			add("%s feathers take no email settings (host, port, security, user, password, from, to, subject_prefix)", fc.Type)
		}
		if fc.Type == TypeSlack && fc.Username != "" {
			add("slack feathers take no username (an incoming webhook posts as its app)")
		}
		problems = append(problems, addressPolicyProblems(fc)...)
		if fc.WebhookURL == nil {
			add("webhook_url is required")
		} else if err := fc.WebhookURL.Validate(); err != nil {
			add("webhook_url: %v", err)
		} else if fc.WebhookURL.Literal != nil {
			if err := CheckWebhookURL(*fc.WebhookURL.Literal); err != nil {
				add("webhook_url: %v", err)
			}
		}
	case TypeEmail:
		if webhookFields {
			add("email feathers take no webhook_url or username")
		}
		problems = append(problems, addressPolicyProblems(fc)...)
		if fc.Host == "" {
			add("host is required")
		}
		if fc.Port < 0 || fc.Port > 65535 {
			add("port must be between 1 and 65535")
		}
		switch fc.Security {
		case "", "starttls", "tls":
		case "none":
			if fc.User != "" || fc.Password != nil {
				add("security = \"none\" sends the password in clear text; use starttls or tls with user/password")
			}
		default:
			add("security must be starttls, tls or none")
		}
		if (fc.User == "") != (fc.Password == nil) {
			add("user and password go together")
		}
		if fc.Password != nil {
			if err := fc.Password.Validate(); err != nil {
				add("password: %v", err)
			}
		}
		if err := checkAddress(fc.From); err != nil {
			add("from: %v", err)
		}
		if len(fc.To) == 0 {
			add("to is required")
		}
		for _, a := range fc.To {
			if err := checkAddress(a); err != nil {
				add("to: %v", err)
			}
		}
		if strings.ContainsAny(fc.SubjectPrefix, "\r\n") || strings.ContainsAny(fc.Host, "\r\n ") {
			add("host and subject_prefix must be single line")
		}
	case "":
		add("type is required (%s)", strings.Join(FeatherTypes, ", "))
	default:
		if why, ok := UnsupportedFeatherTypes[fc.Type]; ok {
			add("type %q: %s", fc.Type, why)
		} else {
			add("unknown type %q (%s)", fc.Type, strings.Join(FeatherTypes, ", "))
		}
	}
	return problems
}

type textField struct{ name, value string }

// textFields are the free-text fields of fc (not the message templates,
// not the secrets).
func textFields(fc FeatherConfig) []textField {
	fs := []textField{
		{"username", fc.Username}, {"host", fc.Host}, {"user", fc.User}, {"from", fc.From},
		{"subject_prefix", fc.SubjectPrefix}, {"plugin", fc.Plugin}, {"path", fc.Path},
	}
	for _, a := range fc.To {
		fs = append(fs, textField{"to", a})
	}
	for _, k := range sortedKeys(fc.Settings) {
		fs = append(fs, textField{"settings." + k, fc.Settings[k]})
	}
	for _, k := range sortedKeys(fc.Query) {
		fs = append(fs, textField{"query (a key)", k})
	}
	for _, h := range fc.AllowHosts {
		fs = append(fs, textField{"allow_hosts", h})
	}
	if fc.Auth != nil {
		fs = append(fs, textField{"auth.username", fc.Auth.Username}, textField{"auth.header", fc.Auth.Header})
	}
	return fs
}

// HasControl reports whether s holds a control character: C0 (ESC, BEL,
// a line break, a tab), DEL or C1.
func HasControl(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

// CheckWebhookURL checks a webhook URL, which is a secret: https, or http to
// this machine only (a local relay, a test server); no user information.
// Its errors never quote the URL.
func CheckWebhookURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return errors.New("must be an absolute https URL")
	}
	if u.User != nil {
		return errors.New("must not contain user information")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
		return errors.New("must be https (http is accepted for localhost only: the URL is the credential)")
	}
	return errors.New("must be an absolute https URL")
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// checkAddress accepts one bare address (no display name, no CR/LF).
func checkAddress(s string) error {
	if s == "" {
		return errors.New("is required")
	}
	if strings.ContainsAny(s, "\r\n<>,\"") {
		return fmt.Errorf("%q is not a bare address", s)
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || a.Name != "" {
		return fmt.Errorf("%q is not a valid address", s)
	}
	return nil
}

func wrapAll(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return fmt.Errorf("%w:\n  %s", ErrInvalidConfig, strings.Join(msgs, "\n  "))
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
