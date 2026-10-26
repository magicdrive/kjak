package configure

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Plugins: the executable of plugin NAME is kjak-feather-NAME (.exe on
// Windows). A name is lower-case ASCII (the same on every file system,
// whatever its case folding or Unicode normalization), and never the name
// of a built-in type: a plugin cannot be mistaken for one.
const (
	PluginPrefix        = "kjak-feather-"
	MaxPluginSettings   = 64
	MaxPluginSecrets    = 32
	MaxPluginSettingLen = 4 << 10
)

var (
	pluginName     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)
	pluginKey      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	sha256Hex      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	envName        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	reservedPlugin = map[string]bool{"external": true, "builtin": true, "kjak": true, "plugin": true, "plugins": true}
)

// ValidPluginName checks the name of a plugin.
func ValidPluginName(name string) error {
	if !pluginName.MatchString(name) {
		return fmt.Errorf("invalid plugin name %q (lower-case letters, digits, '_', '-'; at most 48; starting with a letter or digit)", name)
	}
	if reservedPlugin[name] || contains(FeatherTypes, name) {
		return fmt.Errorf("plugin name %q is reserved (a built-in type or a kjak name)", name)
	}
	if _, ok := UnsupportedFeatherTypes[name]; ok {
		return fmt.Errorf("plugin name %q is reserved", name)
	}
	return nil
}

// PluginFile is the file name of the executable of plugin name on goos.
func PluginFile(name, goos string) string {
	if goos == "windows" {
		return PluginPrefix + name + ".exe"
	}
	return PluginPrefix + name
}

// externalFieldsSet reports whether fc sets a key of the external type.
func externalFieldsSet(fc FeatherConfig) bool {
	return fc.Plugin != "" || fc.Path != "" || fc.SHA256 != "" || len(fc.PluginSecrets) > 0 || len(fc.Settings) > 0 || len(fc.Env) > 0
}

// pluginEnvDenied are names a plugin never gets through env: kjak's own,
// and the dynamic loader's (code injection into the plugin).
func pluginEnvDenied(name string) bool {
	u := strings.ToUpper(name)
	return strings.HasPrefix(u, "KJAK_") || strings.HasPrefix(u, "LD_") || strings.HasPrefix(u, "DYLD_") || u == "GODEBUG" || u == "GOTRACEBACK"
}

func externalProblems(fc FeatherConfig) []string {
	var problems []string
	add := func(format string, a ...any) {
		problems = append(problems, fmt.Sprintf(format, a...))
	}
	if fc.Plugin == "" {
		add("plugin is required (the executable kjak-feather-<plugin> on PATH)")
	} else if err := ValidPluginName(fc.Plugin); err != nil {
		add("%v", err)
	}
	if fc.Path != "" {
		base := filepath.Base(fc.Path)
		switch {
		case !filepath.IsAbs(fc.Path):
			add("path must be absolute")
		case base != PluginFile(fc.Plugin, "linux") && base != PluginFile(fc.Plugin, "windows"):
			add("path must name the file %s", PluginFile(fc.Plugin, "linux"))
		}
	}
	if fc.SHA256 != "" && !sha256Hex.MatchString(fc.SHA256) {
		add("sha256 must be 64 lower-case hex digits")
	}
	if len(fc.PluginSecrets) > MaxPluginSecrets {
		add("secrets: at most %d", MaxPluginSecrets)
	}
	for _, k := range sortedKeys(fc.PluginSecrets) {
		if !pluginKey.MatchString(k) {
			add("secrets: invalid key %q (lower-case letters, digits, '_')", k)
		}
		if r := fc.PluginSecrets[k]; r == nil {
			add("secrets.%s: a credential reference is required", k)
		} else if err := r.Validate(); err != nil {
			add("secrets.%s: %v", k, err)
		}
	}
	if len(fc.Settings) > MaxPluginSettings {
		add("settings: at most %d", MaxPluginSettings)
	}
	for _, k := range sortedKeys(fc.Settings) {
		if !pluginKey.MatchString(k) {
			add("settings: invalid key %q (lower-case letters, digits, '_')", k)
		}
		if len(fc.Settings[k]) > MaxPluginSettingLen {
			add("settings.%s: longer than %d bytes", k, MaxPluginSettingLen)
		}
	}
	seen := map[string]bool{}
	for _, e := range fc.Env {
		switch {
		case !envName.MatchString(e):
			add("env: %q is not an environment variable name", e)
		case pluginEnvDenied(e):
			add("env: %s is never passed to a plugin", e)
		case seen[e]:
			add("env: %s is listed twice", e)
		}
		seen[e] = true
	}
	if fc.Timeout != nil && (fc.Timeout.Duration <= 0 || fc.Timeout.Duration > MaxTimeout) {
		add("timeout must be between 0s (exclusive) and %s", MaxTimeout)
	}
	if r := fc.Retry; r != nil {
		switch r.Policy {
		case "", RetryNever, RetryPlugin:
		default:
			add("retry.policy must be never or plugin")
		}
		if r.MaxAttempts < 0 || r.MaxAttempts > MaxHTTPAttempts {
			add("retry.max_attempts must be between 1 and %d (0: no cap of its own)", MaxHTTPAttempts)
		}
	}
	return problems
}
