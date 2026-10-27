package external

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
)

// built is the test plugin, built once (testdata/plugin).
var built string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kjak-plugin-")
	if err != nil {
		panic(err)
	}
	built = filepath.Join(dir, "plugin-under-test")
	if runtime.GOOS == "windows" {
		built += ".exe"
	}
	build := exec.Command("go", "build", "-o", built, "./testdata/plugin")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		_, _ = os.Stderr.Write(out)
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// install copies the test plugin into dir as plugin name.
func install(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, configure.PluginFile(name, runtime.GOOS))
	if err := os.WriteFile(p, b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	settle(t, p)
	return p
}

// settle runs a new copy of the test plugin once, to its end and with no
// deadline, before a test runs it under a plugin timeout. The first exec of
// an executable just written is held until the system has scanned it
// (macOS: XProtect, an endpoint security agent): about half a second when
// idle, but seconds under load, one copy at a time machine-wide. Measured
// on macOS under load: median 3s and up to the full 10s from exec to the
// plugin's main for a new copy, 12ms for the next run of the same copy; a
// copy killed by a short plugin timeout while it was being scanned held up
// the scan of the next new copy for about 10s. That wait belongs to the
// machine, not to kjak: counted inside a plugin timeout, it made a test of
// the protocol time out. Run here, it is not counted, and no copy is ever
// killed before its main.
func settle(t testing.TB, path string) {
	t.Helper()
	start := time.Now()
	if err := runOnce(path); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Logf("the first run of %s took %s (the system's scan of a new executable)", path, d)
	}
}

// runOnce runs the test plugin with an empty request: it reads nothing it
// can decode and exits 8.
func runOnce(path string) error {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 8 {
		return nil
	}
	return fmt.Errorf("the first run of %s: %v, want exit status 8", path, err)
}

func tempDir(t *testing.T) string {
	d := t.TempDir()
	if r, err := filepath.EvalSymlinks(d); err == nil {
		d = r
	}
	return d
}

func msg() core.Message {
	return core.Message{Title: "T", Body: "body", Level: core.LevelError, URL: "https://ci.example/1"}
}

// plugin builds a feather of the test plugin with settings.
func plugin(t *testing.T, settings map[string]string, change func(*configure.FeatherConfig)) *Feather {
	t.Helper()
	dir := filepath.Join(tempDir(t), "bin")
	install(t, dir, "test")
	return pluginIn(t, dir, settings, change)
}

// sharedDir holds one installed copy of the test plugin for the tests that
// run it many times: the first run of every new copy of an executable is
// scanned on macOS (about half a second, one at a time machine-wide).
var sharedDir = sync.OnceValues(func() (string, error) {
	dir := filepath.Join(filepath.Dir(built), "shared-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, err := os.ReadFile(built)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, configure.PluginFile("test", runtime.GOOS))
	if err := os.WriteFile(p, b, 0o755); err != nil {
		return "", err
	}
	return dir, runOnce(p) // settled, as install does
})

// sharedPlugin is plugin with the shared copy of the test plugin.
func sharedPlugin(t *testing.T, settings map[string]string, change func(*configure.FeatherConfig)) *Feather {
	t.Helper()
	dir, err := sharedDir()
	if err != nil {
		t.Fatal(err)
	}
	return pluginIn(t, dir, settings, change)
}

// pluginIn builds a feather of the test plugin installed in dir.
func pluginIn(t *testing.T, dir string, settings map[string]string, change func(*configure.FeatherConfig)) *Feather {
	t.Helper()
	fc := configure.FeatherConfig{Type: configure.TypeExternal, Plugin: "test", Settings: settings}
	if change != nil {
		change(&fc)
	}
	if err := configure.ValidateFeather(fc); err != nil {
		t.Fatal(err)
	}
	exe, err := Resolve("test", fc.Path, dir, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PATH": dir, "HOME": "/home/test", "LANG": "C", "SECRET_ENV": "env-SECRET-value", "ALLOWED_X": "x"}
	secrets := map[string]credential.Secret{}
	for k := range fc.PluginSecrets {
		secrets[k] = credential.NewSecret("value-of-" + k + "-SECRET")
	}
	f, err := New("pager", fc, exe, secrets, Options{LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPluginNames(t *testing.T) {
	for _, ok := range []string{"pagerduty", "a", "my-plugin_2", "0x"} {
		if err := configure.ValidPluginName(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "PagerDuty", "Slack", "slack", "discord", "email", "http", "external", "os", "kjak", "a.b", "../x", "a/b", "é", "ｓｌａｃｋ", "-x", "_x", "a b", strings.Repeat("a", 49), "x\x00"} {
		if err := configure.ValidPluginName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestResolveSearchesAbsolutePATHOnly(t *testing.T) {
	root := tempDir(t)
	abs := filepath.Join(root, "abs")
	install(t, abs, "test")
	sep := string(os.PathListSeparator)
	exe, err := Resolve("test", "", "relative"+sep+""+sep+abs, runtime.GOOS)
	if err != nil || exe.Path != filepath.Join(abs, configure.PluginFile("test", runtime.GOOS)) {
		t.Fatalf("%+v %v", exe, err)
	}
	// a plugin in the current directory, or a relative one, is never found
	t.Chdir(root)
	install(t, filepath.Join(root, "rel"), "hijack")
	install(t, root, "hijack")
	for _, path := range []string{"rel", "", "." + sep + "rel", sep + "rel"} {
		if _, err := Resolve("hijack", "", path, runtime.GOOS); err == nil || !strings.Contains(err.Error(), "not on PATH") {
			t.Errorf("PATH %q: %v", path, err)
		}
	}
	_, skipped := Discover("rel"+sep+""+sep+abs, runtime.GOOS)
	if len(skipped) != 2 {
		t.Fatalf("skipped %v", skipped)
	}
	if _, err := Resolve("missing", "", abs, runtime.GOOS); !errors.Is(err, ErrPlugin) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := Resolve("Test", "", abs, runtime.GOOS); err == nil {
		t.Fatal("an upper-case name")
	}
}

func TestResolveDuplicates(t *testing.T) {
	root := tempDir(t)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	install(t, a, "test")
	pb := install(t, b, "test")
	sep := string(os.PathListSeparator)
	if _, err := Resolve("test", "", a+sep+b, runtime.GOOS); err == nil || !strings.Contains(err.Error(), "several PATH directories") {
		t.Fatalf("two files: %v", err)
	}
	// pinned: the PATH is not searched
	if exe, err := Resolve("test", pb, a+sep+b, runtime.GOOS); err != nil || exe.Path != pb {
		t.Fatalf("pinned: %+v %v", exe, err)
	}
	// the same directory twice is one file
	if _, err := Resolve("test", "", a+sep+a, runtime.GOOS); err != nil {
		t.Fatalf("same dir twice: %v", err)
	}
	found, _ := Discover(a+sep+b, runtime.GOOS)
	if len(found) != 2 || found[0].Shadowed || !found[1].Shadowed || found[0].SHA256 == "" {
		t.Fatalf("%+v", found)
	}
}

func TestPluginV1(t *testing.T) {
	f := plugin(t, map[string]string{"mode": "ok"}, nil)
	o := f.Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceConfirmed || o.StatusCode != 202 {
		t.Fatalf("%+v", o)
	}
	f = plugin(t, map[string]string{"mode": "extra"}, nil)
	if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("unknown fields: %+v", o)
	}
	f = plugin(t, map[string]string{"mode": "unknown"}, nil)
	if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceUnknown || o.Retryable {
		t.Fatalf("unknown: %+v", o)
	}
	f = plugin(t, map[string]string{"mode": "permanent"}, nil)
	o = f.Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || o.Code != core.ErrCodeRejected || o.StatusCode != 400 || strings.Contains(o.Err.Error(), "\x1b") {
		t.Fatalf("permanent: %+v", o)
	}
}

// What a plugin gets: the request on stdin with the secrets the feather
// names (no other), no argument, the minimal environment and the variables
// it was allowed, no secret in either, the temporary directory.
func TestCritical_PluginSecretAndEnvironmentIsolation(t *testing.T) {
	dump := filepath.Join(tempDir(t), "dump.json")
	f := plugin(t, map[string]string{"mode": "dump", "dump": dump}, func(fc *configure.FeatherConfig) {
		fc.PluginSecrets = map[string]*credential.Ref{"routing_key": {Secret: strp("pd.key")}}
		fc.Env = []string{"ALLOWED_X"}
	})
	if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("%+v", o)
	}
	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Request Request  `json:"request"`
		Args    []string `json:"args"`
		Env     []string `json:"env"`
		Cwd     string   `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Args) != 1 {
		t.Fatalf("arguments: %v", d.Args)
	}
	if len(d.Request.Secrets) != 1 || d.Request.Secrets["routing_key"] != "value-of-routing_key-SECRET" {
		t.Fatalf("secrets: %v", d.Request.Secrets)
	}
	if d.Request.ProtocolVersion != 1 || d.Request.Feather != "pager" || d.Request.Attempt != 1 || d.Request.Message.Body != "body" || d.Request.Settings["mode"] != "dump" || d.Request.IdempotencyKey == "" || d.Request.Deadline.IsZero() {
		t.Fatalf("request: %+v", d.Request)
	}
	allowed := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "ALLOWED_X": true, "KJAK_PLUGIN_PROTOCOL": true}
	for _, kv := range d.Env {
		k, v, _ := strings.Cut(kv, "=")
		if runtime.GOOS == "windows" && (k == "SYSTEMROOT" || strings.HasPrefix(k, "=")) {
			continue // set by Windows itself
		}
		if !allowed[k] {
			t.Errorf("environment variable %s was passed", k)
		}
		if strings.Contains(v, "SECRET") {
			t.Errorf("a secret in the environment: %s", k)
		}
	}
	for _, a := range d.Args {
		if strings.Contains(a, "SECRET") {
			t.Error("a secret in the arguments")
		}
	}
	if tmp, _ := filepath.EvalSymlinks(os.TempDir()); d.Cwd != tmp && d.Cwd != os.TempDir() {
		t.Errorf("working directory %s", d.Cwd)
	}
	// a variable that is the source of a secret is not passed as one
	if _, err := New("x", configure.FeatherConfig{Type: configure.TypeExternal, Plugin: "test", Env: []string{"TOKEN"},
		PluginSecrets: map[string]*credential.Ref{"t": {Env: strp("TOKEN")}}}, f.Exe, nil, Options{}); err == nil {
		t.Fatal("a secret's variable was allowed")
	}
}

func strp(s string) *string { return &s }

// A plugin that answers wrong, too much, or not at all — after it ran — has
// an unknown acceptance: it may have delivered.
func TestCritical_PluginMisbehavior(t *testing.T) {
	for _, mode := range []string{"badjson", "empty", "twojson", "version2", "noversion", "inconsistent", "huge", "exit3", "crash"} {
		f := plugin(t, map[string]string{"mode": mode}, nil)
		start := time.Now()
		o := f.Deliver(context.Background(), msg())
		if o.Acceptance != core.AcceptanceUnknown || o.Retryable || o.Code != core.ErrCodeProtocol {
			t.Errorf("%s: %+v", mode, o)
		}
		if time.Since(start) > 30*time.Second {
			t.Errorf("%s took %s", mode, time.Since(start))
		}
		switch mode {
		case "version2":
			if !strings.Contains(o.Err.Error(), "unsupported protocol_version 2") {
				t.Errorf("version2: %v", o.Err)
			}
		case "crash":
			if !strings.Contains(o.Err.Error(), "plugin crashed") {
				t.Errorf("crash: no stderr in %v", o.Err)
			}
		case "exit3":
			if !strings.Contains(o.Err.Error(), "status 3") {
				t.Errorf("exit3: %v", o.Err)
			}
		}
		if len(o.Err.Error()) > 2000 {
			t.Errorf("%s: an error of %d bytes", mode, len(o.Err.Error()))
		}
	}
	// a lot on stderr is cut, not a failure
	f := plugin(t, map[string]string{"mode": "hugeerr"}, nil)
	if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("hugeerr: %+v", o)
	}
	// a plugin that cannot start ran nothing
	f = plugin(t, map[string]string{"mode": "ok"}, nil)
	if err := os.Remove(f.Exe.Real); err != nil {
		t.Fatal(err)
	}
	if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceRejected || o.Retryable {
		t.Fatalf("removed: %+v", o)
	}
}

func TestPluginTimeoutAndCancellation(t *testing.T) {
	f := plugin(t, map[string]string{"mode": "sleep"}, func(fc *configure.FeatherConfig) {
		fc.Timeout = &configure.Duration{Duration: 300 * time.Millisecond}
	})
	start := time.Now()
	o := f.Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceUnknown || o.Retryable || !strings.Contains(o.Err.Error(), "timeout") {
		t.Fatalf("timeout: %+v", o)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("the timeout took %s", d)
	}
	// the plugin timeout is long: the cancellation, not the timeout, must
	// stop the plugin
	f = plugin(t, map[string]string{"mode": "sleep"}, func(fc *configure.FeatherConfig) {
		fc.Timeout = &configure.Duration{Duration: time.Minute}
	})
	ctx, cancel := context.WithCancelCause(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel(errors.New("interrupted"))
	}()
	start = time.Now()
	o = f.Deliver(ctx, msg())
	if o.Acceptance != core.AcceptanceUnknown || o.Code != core.ErrCodeCanceled {
		t.Fatalf("canceled: %+v", o)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("the cancellation stopped the plugin after %s", d)
	}
}

func TestPluginRetryPolicy(t *testing.T) {
	f := plugin(t, map[string]string{"mode": "rejected"}, nil)
	o := f.Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || o.Code != core.ErrCodeRateLimited {
		t.Fatalf("default never: %+v", o)
	}
	log := filepath.Join(tempDir(t), "log")
	f = plugin(t, map[string]string{"mode": "rejected", "log": log}, func(fc *configure.FeatherConfig) {
		fc.Retry = &configure.HTTPRetry{Policy: "plugin", MaxAttempts: 2}
	})
	o = f.Deliver(context.Background(), msg())
	if !o.Retryable || o.RetryAfter != 5*time.Millisecond {
		t.Fatalf("plugin policy: %+v", o)
	}
	if o = f.Deliver(context.Background(), msg()); o.Retryable {
		t.Fatalf("max_attempts: %+v", o)
	}
	b, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "1 ") || !strings.HasPrefix(lines[1], "2 ") || lines[0][2:] != lines[1][2:] {
		t.Fatalf("attempts and keys: %q", lines)
	}
}

func TestConcurrentPlugins(t *testing.T) {
	f := plugin(t, map[string]string{"mode": "ok"}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceConfirmed {
				t.Errorf("%+v", o)
			}
		}()
	}
	wg.Wait()
}

// The executable is checked again before each run: replaced, or changed
// in place (even with its size and time put back, when its digest is
// pinned), it is not run.
func TestCritical_PluginIdentityRechecked(t *testing.T) {
	f := plugin(t, map[string]string{"mode": "ok"}, nil)
	// replaced by another file
	b, _ := os.ReadFile(f.Exe.Real)
	tmp := f.Exe.Real + ".new"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, f.Exe.Real); err != nil {
		t.Fatal(err)
	}
	if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceRejected || o.Code != core.ErrCodeBlocked || o.Retryable {
		t.Fatalf("replaced: %+v", o)
	}
	// changed in place, size and time restored: only the digest sees it
	f = plugin(t, map[string]string{"mode": "ok"}, nil)
	f.Rehash = true
	st, _ := os.Stat(f.Exe.Real)
	h, err := os.OpenFile(f.Exe.Real, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	last := []byte{0}
	r, _ := os.Open(f.Exe.Real)
	_, _ = r.ReadAt(last, st.Size()-1)
	_ = r.Close()
	if _, err := h.WriteAt([]byte{last[0] ^ 0xff}, st.Size()-1); err != nil {
		t.Fatal(err)
	}
	_ = h.Close()
	if err := os.Chtimes(f.Exe.Real, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceRejected || o.Code != core.ErrCodeBlocked {
		t.Fatalf("changed in place: %+v", o)
	}
}

func TestParseResponse(t *testing.T) {
	now := time.Now()
	good := map[string]core.Acceptance{
		`{"protocol_version":1,"status":"delivered"}`:                                      core.AcceptanceConfirmed,
		`{"protocol_version":1,"status":"delivered","acceptance":"confirmed"}` + "\n\n":    core.AcceptanceConfirmed,
		`{"protocol_version":1,"status":"failed","acceptance":"rejected"}`:                 core.AcceptanceRejected,
		`{"protocol_version":1,"status":"failed"}`:                                         core.AcceptanceUnknown,
		`{"protocol_version":1,"status":"failed","acceptance":"unknown","retryable":true}`: core.AcceptanceUnknown,
	}
	for in, want := range good {
		o, err := parseResponse(nil, []byte(in), now)
		if err != nil || o.Acceptance != want || (want == core.AcceptanceUnknown && o.Retryable) {
			t.Errorf("%s: %+v %v", in, o, err)
		}
	}
	for _, bad := range []string{``, `null`, `[]`, `"x"`, `{"protocol_version":1}`, `{"protocol_version":"1","status":"delivered"}`, `{"protocol_version":1,"status":"ok"}`, `{"protocol_version":1,"status":"failed","acceptance":"confirmed"}`, `{"protocol_version":1,"status":"delivered"} x`} {
		if _, err := parseResponse(nil, []byte(bad), now); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	o, _ := parseResponse(nil, []byte(`{"protocol_version":1,"status":"failed","acceptance":"rejected","retryable":true,"retry_after_ms":99999999999}`), now)
	if o.RetryAfter != 24*time.Hour {
		t.Fatalf("retry after %s", o.RetryAfter)
	}
}
