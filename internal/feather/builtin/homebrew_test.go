package builtin

import (
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/configure"
)

// What lets a plugin in Homebrew's admin-writable directories run: the
// file's SHA-256 equal to the global feather's pin, or, for a feather of a
// project, the trust recording that path and that SHA-256. A pin that is
// merely set, a mismatch, or a project's own pin without its trust is
// refused.
func TestCritical_HomebrewAccept(t *testing.T) {
	const real = "/opt/homebrew/Cellar/kjak-notify-macos/0.1.0/bin/kjak-feather-notify-macos"
	sum := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	fc := func(pin string) configure.FeatherConfig {
		return configure.FeatherConfig{Type: configure.TypeExternal, Plugin: "notify-macos", SHA256: pin}
	}
	trust := func(path, sha string) *configure.TrustEntry {
		return &configure.TrustEntry{Plugins: []configure.TrustedPlugin{{Name: "notify-macos", Path: path, SHA256: sha}}}
	}
	for _, c := range []struct {
		name  string
		fc    configure.FeatherConfig
		trust *configure.TrustEntry
		want  string // "" accepted
	}{
		{"global, pinned, equal", fc(sum), nil, ""},
		{"global, not pinned", fc(""), nil, `sha256 = "` + sum + `"`},
		{"global, pinned, other", fc(other), nil, "not the pinned " + other},
		{"project, trust records it", fc(""), trust(real, sum), ""},
		{"project, its own pin equal, trust without the plugin", fc(sum), &configure.TrustEntry{}, "project's trust"},
		{"project, trust of another content", fc(sum), trust(real, other), "project's trust"},
		{"project, trust of another path", fc(sum), trust("/usr/local/Cellar/kjak-notify-macos/0.1.0/bin/kjak-feather-notify-macos", sum), "project's trust"},
		{"project, trust of another plugin", fc(sum), &configure.TrustEntry{Plugins: []configure.TrustedPlugin{{Name: "other", Path: real, SHA256: sum}}}, "project's trust"},
	} {
		err := homebrewAccept(c.fc, c.trust)(real, sum)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: refused: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: want %q, got %v", c.name, c.want, err)
		}
	}
}
