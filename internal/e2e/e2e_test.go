// Package e2e runs the kjak binary as a user does: built with `go build`,
// with its own configuration directory, against local test servers. It runs
// natively on Linux, macOS and Windows in CI.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// binary is kjak, plugin the test plugin, both built by TestMain.
var binary, plugin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kjak-e2e-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "kjak")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	// the test plugin of feather/external (protocol v1), installed by the
	// tests as kjak-feather-<name> (plugin_test.go)
	plugin = filepath.Join(dir, "plugin-under-test")
	if runtime.GOOS == "windows" {
		plugin += ".exe"
	}
	for _, b := range [][2]string{{binary, "../.."}, {plugin, "../feather/external/testdata/plugin"}} {
		build := exec.Command("go", "build", "-o", b[0], b[1])
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			_, _ = os.Stderr.Write(out)
			panic(err)
		}
	}
	// the first run of a new executable waits for the system's scan of it
	// (settlePlugin): kjak's is done here, not inside a test's timeout
	if out, err := exec.Command(binary, "version").CombinedOutput(); err != nil {
		_, _ = os.Stderr.Write(out)
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type sandbox struct {
	t   *testing.T
	dir string
	env []string
	exe string // the binary to run; empty: the one TestMain built
}

func newSandbox(t *testing.T) *sandbox {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	_ = os.MkdirAll(work, 0o755)
	env := []string{
		"XDG_CONFIG_HOME=" + filepath.Join(dir, "xdg"),
		"HOME=" + dir, "USERPROFILE=" + dir,
		"PATH=" + os.Getenv("PATH"),
		"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"), // Windows networking needs it
		"TEMP=" + os.Getenv("TEMP"), "TMP=" + os.Getenv("TMP"),
	}
	return &sandbox{t: t, dir: work, env: env}
}

type run struct {
	code           int
	stdout, stderr string
}

func (s *sandbox) kjak(stdin string, args ...string) run {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exe := s.exe
	if exe == "" {
		exe = binary
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = s.dir
	cmd.Env = s.env
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		s.t.Fatalf("kjak %v: %v", args, err)
	}
	return run{code, out.String(), errOut.String()}
}

func (s *sandbox) must(args ...string) run {
	s.t.Helper()
	r := s.kjak("", args...)
	if r.code != 0 {
		s.t.Fatalf("kjak %s: exit %d\n%s%s", strings.Join(args, " "), r.code, r.stdout, r.stderr)
	}
	return r
}

func TestBinaryBasics(t *testing.T) {
	s := newSandbox(t)
	if r := s.kjak(""); r.code != 2 {
		t.Fatalf("no arguments: exit %d", r.code)
	}
	v := s.must("version").stdout
	if !strings.HasPrefix(v, "kjak version ") {
		t.Fatal(v)
	}
	// the flags print the same line (the Homebrew formula's test runs --version)
	for _, flag := range []string{"--version", "-v"} {
		if r := s.must(flag); r.stdout != v {
			t.Fatalf("%s prints %q, version %q", flag, r.stdout, v)
		}
	}
	s.must("--help")
	s.must("config", "init")
	r := s.must("config", "path", "-o", "json")
	var paths map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &paths); err != nil || !strings.HasSuffix(filepath.ToSlash(paths["global"].(string)), "xdg/kjak/config.toml") {
		t.Fatalf("paths = %s (%v)", r.stdout, err)
	}
	s.must("config", "validate")
}

// The quick start of README.md, against a local Slack-like endpoint.
func TestBinaryQuickStart(t *testing.T) {
	var hits atomic.Int32
	var last atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		last.Store(string(b))
		hits.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	s := newSandbox(t)
	s.env = append(s.env, "KJAK_DEV_SLACK_WEBHOOK="+srv.URL+"/services/T/B/X")

	s.must("feather", "add", "dev_slack", "--type", "slack", "--secret-env", "KJAK_DEV_SLACK_WEBHOOK")
	s.must("group", "set", "development", "dev_slack")
	s.must("send", "--to", "development", "--dry-run", "build finished")
	if hits.Load() != 0 {
		t.Fatal("dry run sent")
	}
	r := s.must("send", "--to", "development", "--title", "CI", "--level", "success", "build finished")
	if !strings.Contains(r.stdout, "ok    dev_slack (slack) delivered") || hits.Load() != 1 {
		t.Fatalf("stdout = %s", r.stdout)
	}
	if !strings.Contains(last.Load().(string), "[SUCCESS] CI: build finished") {
		t.Fatalf("payload = %s", last.Load())
	}
	r = s.kjak("tests failed\nline 2\n", "send", "--to", "dev_slack", "--stdin", "--level", "error", "-o", "json")
	if r.code != 0 || !strings.Contains(r.stdout, `"schema_version": 1`) {
		t.Fatalf("stdin: %+v", r)
	}
}

func TestBinaryExitCodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	s := newSandbox(t)
	s.env = append(s.env, "HOOK="+srv.URL)
	s.must("feather", "add", "f", "--type", "discord", "--secret-env", "HOOK")
	if r := s.kjak("", "send", "--to", "f", "x"); r.code != 1 {
		t.Fatalf("rejected: exit %d", r.code)
	}
	if r := s.kjak("", "send", "--to", "f"); r.code != 2 {
		t.Fatalf("usage: exit %d", r.code)
	}
	// an untrusted project file
	if err := os.WriteFile(filepath.Join(s.dir, ".kjak.toml"), []byte("[templates.t]\nbody = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := s.kjak("", "send", "--to", "f", "x"); r.code != 2 || !strings.Contains(r.stderr, "not trusted") {
		t.Fatalf("untrusted: %+v", r)
	}
	// with -o json: the error document on stdout, the diagnostic on stderr
	r := s.kjak("", "send", "--to", "f", "-o", "json", "x")
	var doc struct {
		SchemaVersion int    `json:"schema_version"`
		Outcome       string `json:"outcome"`
		Error         struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || r.code != 2 || doc.SchemaVersion != 1 || doc.Outcome != "error" || doc.Error.Code != "untrusted_project" || r.stderr == "" {
		t.Fatalf("untrusted with -o json: %v %+v", err, r)
	}
	s.must("config", "trust", "--yes")
	if r := s.kjak("", "send", "--to", "f", "--template", "t"); r.code != 1 {
		t.Fatalf("trusted template: exit %d %s", r.code, r.stderr)
	}
}

// Every `kjak ...` command line in the README and the guides names a real
// command and action: its --help succeeds.
func TestReadmeCommandsExist(t *testing.T) {
	for _, doc := range []string{"README.md", "README_ja.md", "docs/getting-started.md", "docs/getting-started_ja.md", "docs/configuration.md", "docs/configuration_ja.md", "docs/discord-e2e.md"} {
		t.Run(doc, func(t *testing.T) { checkDocCommands(t, "../../"+doc) })
	}
}

func checkDocCommands(t *testing.T, path string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	s := newSandbox(t)
	checked := map[string]bool{}
	inCode := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if !inCode {
			continue
		}
		line = strings.TrimPrefix(line, "$ ")
		if i := strings.Index(line, "| kjak "); i >= 0 {
			line = line[i+2:]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "kjak" || strings.HasPrefix(fields[1], "-") {
			continue
		}
		args := []string{fields[1]}
		if fields[1] != "send" && fields[1] != "version" && len(fields) > 2 && !strings.HasPrefix(fields[2], "-") {
			args = append(args, fields[2])
		}
		key := strings.Join(args, " ")
		if checked[key] || fields[1] == "version" {
			continue
		}
		checked[key] = true
		if r := s.kjak("", append(args, "--help")...); r.code != 0 {
			t.Errorf("%s: kjak %s --help: exit %d %s", path, key, r.code, r.stderr)
		}
	}
	if len(checked) < 3 { // the parsing above found nothing
		t.Fatalf("%s: only %d commands found", path, len(checked))
	}
}

// A reader that went away (| head) ends a command other than send with exit
// 2 and nothing on stderr, on every system: a closed pipe is EPIPE on Unix,
// ERROR_NO_DATA or ERROR_BROKEN_PIPE on Windows.
func TestBinaryClosedPipeIsQuiet(t *testing.T) {
	s := newSandbox(t)
	s.env = append(s.env, "HOOK=https://hooks.example/x")
	s.must("feather", "add", "f", "--type", "slack", "--secret-env", "HOOK")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	cmd := exec.Command(binary, "feather", "list", "-o", "json")
	cmd.Dir, cmd.Env, cmd.Stdout = s.dir, s.env, w
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	err = cmd.Run()
	_ = w.Close()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 || errOut.Len() != 0 {
		t.Fatalf("exit %v, stderr %q", err, errOut.String())
	}
}
