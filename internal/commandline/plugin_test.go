package commandline_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
)

// testPlugin is the test plugin of feather/external, built once.
var testPlugin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kjak-cli-plugin-")
	if err != nil {
		panic(err)
	}
	testPlugin = filepath.Join(dir, "plugin-under-test")
	if runtime.GOOS == "windows" {
		testPlugin += ".exe"
	}
	build := exec.Command("go", "build", "-o", testPlugin, "../feather/external/testdata/plugin")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		_, _ = os.Stderr.Write(out)
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// installPlugin puts the test plugin in dir as plugin name and returns its
// path.
func (e *env) installPlugin(dir, name string) string {
	e.t.Helper()
	b, err := os.ReadFile(testPlugin)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	p := filepath.Join(dir, configure.PluginFile(name, runtime.GOOS))
	if err := os.WriteFile(p, b, 0o755); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) pluginDir() string { return filepath.Join(filepath.Dir(e.project), "plugins") }

func externalFeather(name, plugin, extra string) string {
	return fmt.Sprintf("[feathers.%s]\ntype = \"external\"\nplugin = %q\n%s\n", name, plugin, extra)
}

func TestExternalFeatherSend(t *testing.T) {
	e := newEnv(t)
	e.installPlugin(e.pluginDir(), "pager")
	e.vars["PATH"] = e.pluginDir()
	dump := filepath.Join(e.project, "dump.json")
	e.writeGlobal(externalFeather("pd", "pager", fmt.Sprintf("settings = { mode = \"dump\", dump = %q }\nsecrets = { routing_key = { secret = \"pd.key\" } }\nenv = [\"ALLOWED_X\"]", dump)))
	e.writeFile(e.userSecrets(), "[pd]\nkey = \"rk-SECRET-1\"\nother = \"OTHER-SECRET-2\"\n", 0o600)
	e.vars["ALLOWED_X"] = "x"
	e.vars["AWS_SECRET_ACCESS_KEY"] = "AWS-SECRET-3"
	e.vars["KJAK_SECRETS"] = "/elsewhere"
	r := e.must("send", "--to", "pd", "-o", "json", "hi")
	rep := parseHTTPReport(t, r.stdout)
	if rep.Outcome != "success" || rep.Results[0].Type != "external" {
		t.Fatalf("%s", r.stdout)
	}
	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Request struct {
			Secrets map[string]string `json:"secrets"`
		} `json:"request"`
		Env  []string `json:"env"`
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Request.Secrets) != 1 || d.Request.Secrets["routing_key"] != "rk-SECRET-1" || len(d.Args) != 1 {
		t.Fatalf("request %v args %v", d.Request.Secrets, d.Args)
	}
	for _, kv := range d.Env {
		if strings.Contains(kv, "SECRET") || strings.HasPrefix(kv, "KJAK_SECRETS") || strings.HasPrefix(kv, "AWS_") {
			t.Errorf("passed to the plugin: %s", kv)
		}
	}
	if !strings.Contains(strings.Join(d.Env, " "), "ALLOWED_X=x") {
		t.Errorf("allowed variable missing: %v", d.Env)
	}
	r = e.must("feather", "show", "pd")
	if !strings.Contains(r.stdout, "routing_key from secret:pd.key") || strings.Contains(r.stdout, "rk-SECRET-1") {
		t.Fatal(r.stdout)
	}
}

// Found is not allowed: kjak feather plugins runs nothing, and only a
// feather of type external that names a plugin runs it. A plugin named
// like a built-in type is refused, and never replaces the built-in.
func TestCritical_PluginDiscoveryIsNotAuthorization(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	// the spies are the test plugin, which logs every run of itself to
	// runs (an executable of the platform: a shell script is not one on
	// Windows)
	for _, name := range []string{"spy", "slack"} {
		e.installPlugin(e.pluginDir(), name)
	}
	runs := filepath.Join(filepath.Dir(e.pluginDir()), "kjak-test-plugin-runs.log")
	e.writeFile(runs, "", 0o600)
	ran := func() string {
		b, err := os.ReadFile(runs)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	e.vars["PATH"] = e.pluginDir() + string(os.PathListSeparator) + "relative/bin" + string(os.PathListSeparator)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	r := e.must("feather", "plugins")
	if !strings.Contains(r.stdout, "spy") || !strings.Contains(r.stdout, `not searched: PATH element "relative/bin"`) || !strings.Contains(r.stdout, `not searched: PATH element ""`) {
		t.Fatalf("plugins: %s", r.stdout)
	}
	if !strings.Contains(r.stdout, "slack") || !strings.Contains(r.stdout, "reserved") {
		t.Fatalf("a plugin named slack is not marked: %s", r.stdout)
	}
	e.must("feather", "plugins", "-o", "json")
	e.must("send", "--to", "a", "hi")
	if got := ran(); got != "" {
		t.Fatalf("a discovered plugin ran: %s", got)
	}
	if p.count() != 1 {
		t.Fatal("the built-in slack feather did not deliver")
	}
	e.writeGlobal(externalFeather("x", "slack", ""))
	if r := e.run("send", "--to", "x", "hi"); r.code != 2 || !strings.Contains(r.stderr, "reserved") {
		t.Fatalf("plugin slack: %+v", r)
	}
	if got := ran(); got != "" {
		t.Fatalf("a reserved plugin ran: %s", got)
	}
	// the spy works: a feather that names it runs it
	e.writeGlobal(externalFeather("s", "spy", ""))
	e.must("send", "--to", "s", "hi")
	if got := ran(); !strings.Contains(got, configure.PluginFile("spy", runtime.GOOS)) {
		t.Fatalf("the spy did not log its run: %q", got)
	}
}

// PATH elements that are not absolute are never searched: a repository's
// directory cannot provide a plugin by being the current one, or by PATH
// naming it relatively.
func TestCritical_PluginPATHHijack(t *testing.T) {
	e := newEnv(t)
	e.installPlugin(filepath.Join(e.project, "bin"), "pager")
	e.installPlugin(e.project, "pager")
	e.writeGlobal(externalFeather("pd", "pager", "settings = { mode = \"ok\" }"))
	t.Chdir(e.project) // the plugins above are in the current directory
	sep := string(os.PathListSeparator)
	for _, path := range []string{"bin", "", "." + sep + "bin", sep} {
		e.vars["PATH"] = path
		if r := e.run("send", "--to", "pd", "hi"); r.code != 2 || !strings.Contains(r.stderr, "not on PATH") {
			t.Fatalf("PATH %q: %+v", path, r)
		}
	}
	// two different files on PATH: which one runs is not left to PATH
	e.installPlugin(filepath.Join(e.pluginDir(), "a"), "pager")
	b := e.installPlugin(filepath.Join(e.pluginDir(), "b"), "pager")
	e.vars["PATH"] = filepath.Join(e.pluginDir(), "a") + sep + filepath.Join(e.pluginDir(), "b")
	if r := e.run("send", "--to", "pd", "hi"); r.code != 2 || !strings.Contains(r.stderr, "several PATH directories") {
		t.Fatalf("duplicates: %+v", r)
	}
	e.writeGlobal(externalFeather("pd", "pager", fmt.Sprintf("path = %q\nsettings = { mode = \"ok\" }", b)))
	e.must("send", "--to", "pd", "hi")
	// a pinned digest that does not match
	e.writeGlobal(externalFeather("pd", "pager", fmt.Sprintf("path = %q\nsha256 = %q\nsettings = { mode = \"ok\" }", b, strings.Repeat("0", 64))))
	if r := e.run("send", "--to", "pd", "hi"); r.code != 2 || !strings.Contains(r.stderr, "pins") {
		t.Fatalf("sha256: %+v", r)
	}
	// missing
	e.writeGlobal(externalFeather("pd", "nothere", ""))
	if r := e.run("send", "--to", "pd", "hi"); r.code != 2 {
		t.Fatalf("missing: %+v", r)
	}
}

// What a plugin echoes — on stdout or stderr — is redacted.
func TestCritical_PluginOutputRedacted(t *testing.T) {
	e := newEnv(t)
	e.installPlugin(e.pluginDir(), "pager")
	e.vars["PATH"] = e.pluginDir()
	e.writeGlobal(externalFeather("pd", "pager", "settings = { mode = \"echo-secret\" }\nsecrets = { token = { secret = \"pd.token\" } }"))
	e.writeFile(e.userSecrets(), "[pd]\ntoken = \"tok-SECRET-9\"\n", 0o600)
	r := e.run("send", "--to", "pd", "-o", "json", "hi")
	if r.code != 1 || strings.Contains(r.stdout+r.stderr, "tok-SECRET-9") || !strings.Contains(r.stdout, "[REDACTED]") {
		t.Fatalf("%+v", r)
	}
}

func TestPluginRetriesAndUnknownThroughTheEngine(t *testing.T) {
	e := newEnv(t)
	e.installPlugin(e.pluginDir(), "pager")
	e.vars["PATH"] = e.pluginDir()
	for _, c := range []struct {
		extra    string
		attempts int
		accept   string
	}{
		{"settings = { mode = \"rejected\" }", 1, "rejected"},
		{"settings = { mode = \"rejected\" }\n[feathers.pd.retry]\npolicy = \"plugin\"", 3, "rejected"},
		{"settings = { mode = \"rejected\" }\n[feathers.pd.retry]\npolicy = \"plugin\"\nmax_attempts = 2", 2, "rejected"},
		{"settings = { mode = \"unknown\" }\n[feathers.pd.retry]\npolicy = \"plugin\"", 1, "unknown"},
		{"settings = { mode = \"crash\" }\n[feathers.pd.retry]\npolicy = \"plugin\"", 1, "unknown"},
	} {
		e.writeGlobal(externalFeather("pd", "pager", c.extra))
		r := e.run("send", "--to", "pd", "-o", "json", "hi")
		rep := parseHTTPReport(t, r.stdout)
		if r.code != 1 || rep.Results[0].Attempts != c.attempts || rep.Results[0].Acceptance != c.accept {
			t.Errorf("%q: exit %d %s", c.extra, r.code, r.stdout)
		}
	}
	e.writeGlobal(externalFeather("pd", "pager", "settings = { mode = \"sleep\" }\ntimeout = \"300ms\""))
	start := time.Now()
	r := e.run("send", "--to", "pd", "hi")
	if r.code != 1 || !strings.Contains(r.stdout, "????") || time.Since(start) > 15*time.Second {
		t.Fatalf("timeout: %+v", r)
	}
}

// A project's plugin is trusted as the executable it was: another file, or
// a changed one, withdraws the trust.
func TestCritical_ProjectPluginPinnedByTrust(t *testing.T) {
	e := newEnv(t)
	p := e.installPlugin(e.pluginDir(), "pager")
	e.vars["PATH"] = e.pluginDir()
	e.writeProject(externalFeather("pd", "pager", "settings = { mode = \"ok\" }"))
	r := e.must("config", "trust", "--yes")
	if !strings.Contains(r.stdout, "feather pd: external plugin pager (runs with your rights)") || !strings.Contains(r.stdout, "plugin pager: "+p) {
		t.Fatalf("trust: %s", r.stdout)
	}
	e.must("send", "--to", "pd", "hi")
	// replaced (an upgrade, or an attacker who can write there)
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p+".new", append(b, 0), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(p+".new", p); err != nil {
		t.Fatal(err)
	}
	r = e.run("send", "--to", "pd", "-o", "json", "hi")
	if r.code != 2 || !strings.Contains(r.stderr, "not the executable that was trusted") || !strings.Contains(r.stdout, "untrusted_project") {
		t.Fatalf("replaced: %+v", r)
	}
	e.must("config", "trust", "--yes")
	e.must("send", "--to", "pd", "hi")
	// a trust that does not cover the plugin
	src, _ := configure.ReadFile(filepath.Join(e.project, ".kjak.toml"))
	if err := configure.Trust(filepath.Join(e.xdg, "kjak", "trust.toml"), src.Path, src.Hash, time.Now()); err != nil {
		t.Fatal(err)
	}
	if r := e.run("send", "--to", "pd", "hi"); r.code != 2 || !strings.Contains(r.stderr, "not part of the trust") {
		t.Fatalf("uncovered: %+v", r)
	}
	// trusting needs the plugin to be there
	e.vars["PATH"] = ""
	if r := e.run("config", "trust", "--yes"); r.code != 2 || !strings.Contains(r.stderr, "install the plugin") {
		t.Fatalf("trust without the plugin: %+v", r)
	}
}

func TestFeatherAddExternalIsInTheFile(t *testing.T) {
	e := newEnv(t)
	if r := e.run("feather", "add", "x", "--type", "external"); r.code != 2 || !strings.Contains(r.stderr, "docs/plugins.md") {
		t.Fatalf("%+v", r)
	}
	if r := e.must("feather", "types"); !strings.Contains(r.stdout, "external") {
		t.Fatal(r.stdout)
	}
}

// A project's plugin gets one of the user's environment variables, or an
// env-sourced secret, only by a name allowed at the trust.
func TestCritical_ProjectPluginEnvNeedsAGrant(t *testing.T) {
	e := newEnv(t)
	e.installPlugin(e.pluginDir(), "pager")
	e.vars["PATH"] = e.pluginDir()
	e.vars["AWS_SECRET_ACCESS_KEY"] = "AWS-SECRET"
	e.vars["TOKEN"] = "TOKEN-SECRET"
	e.writeProject(externalFeather("pe", "pager", "settings = { mode = \"ok\" }\nenv = [\"AWS_SECRET_ACCESS_KEY\"]") +
		externalFeather("ps", "pager", "settings = { mode = \"ok\" }\nsecrets = { t = { env = \"TOKEN\" } }"))
	r := e.run("config", "trust", "--yes")
	if r.code != 2 || !strings.Contains(r.stdout, "env:AWS_SECRET_ACCESS_KEY") || !strings.Contains(r.stdout, "env:TOKEN") {
		t.Fatalf("trust: %+v", r)
	}
	// trusted without the names: refused
	src, _ := configure.ReadFile(filepath.Join(e.project, ".kjak.toml"))
	ts := filepath.Join(e.xdg, "kjak", "trust.toml")
	exe, _ := filepath.EvalSymlinks(filepath.Join(e.pluginDir(), configure.PluginFile("pager", runtime.GOOS)))
	b, _ := os.ReadFile(exe)
	sum := fmt.Sprintf("%x", sha256.Sum256(b))
	if err := configure.TrustWith(ts, src.Path, src.Hash, time.Now(), configure.TrustGrant{Plugins: []configure.TrustedPlugin{{Name: "pager", Path: exe, SHA256: sum}}}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pe", "ps"} {
		r = e.run("send", "--to", f, "hi")
		if r.code != 2 || !strings.Contains(r.stderr, "does not allow") {
			t.Fatalf("%s ungranted: %+v", f, r)
		}
	}
	e.must("config", "trust", "--yes", "--allow-user-secrets")
	e.must("send", "--to", "pe,ps", "hi")
}
