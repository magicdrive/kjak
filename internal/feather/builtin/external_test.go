package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/feather/external"
)

// pluginExe puts an executable named like plugin name in a new directory
// (Build checks it, never runs it) and returns the directory and the
// checked executable.
func pluginExe(t *testing.T, name string) (string, *external.Executable) {
	t.Helper()
	dir := t.TempDir()
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	p := filepath.Join(dir, configure.PluginFile(name, runtime.GOOS))
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, err := external.Resolve(name, "", dir, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	return dir, exe
}

// A plugin of a project runs only as the executable its trust recorded:
// the same path and the same SHA-256 (a copy elsewhere, or a changed file,
// needs a new trust), and it is hashed again before every run. A plugin of
// the user's own configuration is hashed again only when pinned.
func TestCritical_ProjectPluginIsTheTrustedExecutable(t *testing.T) {
	dir, exe := pluginExe(t, "pager")
	d := deps(map[string]string{"PATH": dir})
	d.LookupEnv = func(k string) (string, bool) { return map[string]string{"PATH": dir}[k], k == "PATH" }
	fc := configure.FeatherConfig{Type: configure.TypeExternal, Plugin: "pager"}
	build := func(trust *configure.TrustEntry) (*external.Feather, error) {
		d.ProjectTrust = trust
		f, err := Build(context.Background(), "pd", fc, d)
		if err != nil {
			return nil, err
		}
		return f.(*external.Feather), nil
	}

	f, err := build(nil)
	if err != nil || f.Rehash {
		t.Fatalf("global, not pinned: rehash %v, %v", f != nil && f.Rehash, err)
	}
	trusted := configure.TrustedPlugin{Name: "pager", Path: exe.Real, SHA256: exe.SHA256}
	f, err = build(&configure.TrustEntry{Plugins: []configure.TrustedPlugin{trusted}})
	if err != nil || !f.Rehash {
		t.Fatalf("project, trusted: rehash %v, %v", f != nil && f.Rehash, err)
	}
	for what, tp := range map[string]configure.TrustedPlugin{
		"another path":     {Name: "pager", Path: filepath.Join(filepath.Dir(dir), "elsewhere", filepath.Base(exe.Real)), SHA256: exe.SHA256},
		"another SHA-256":  {Name: "pager", Path: exe.Real, SHA256: strings.Repeat("0", 64)},
		"another plugin":   {Name: "other", Path: exe.Real, SHA256: exe.SHA256},
		"an empty SHA-256": {Name: "pager", Path: exe.Real},
	} {
		if _, err := build(&configure.TrustEntry{Plugins: []configure.TrustedPlugin{tp}}); !errors.Is(err, configure.ErrUntrustedProject) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if _, err := build(&configure.TrustEntry{}); !errors.Is(err, configure.ErrUntrustedProject) {
		t.Errorf("no plugin in the trust: %v", err)
	}
}
