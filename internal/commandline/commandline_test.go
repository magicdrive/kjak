package commandline_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/commandline"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/delivery"
)

// env is one isolated kjak: its own configuration directory, project
// directory, environment variables and keychain.
type env struct {
	t       *testing.T
	xdg     string
	project string
	vars    map[string]string
	stdin   string
	tty     bool
	// afterDelivery runs when kjak starts writing its report (stdout)
	afterDelivery func()
	// keychain replaces the test keychain
	keychain commandline.Keychain
}

// hookWriter calls hook before its first write.
type hookWriter struct {
	w    io.Writer
	hook func()
}

func (h *hookWriter) Write(p []byte) (int, error) {
	if h.hook != nil {
		h.hook()
		h.hook = nil
	}
	return h.w.Write(p)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	e := &env{t: t, xdg: filepath.Join(dir, "xdg"), project: filepath.Join(dir, "project"), vars: map[string]string{}}
	if err := os.MkdirAll(e.project, 0o755); err != nil {
		t.Fatal(err)
	}
	return e
}

type keychain map[string]string

func (k keychain) Lookup(_ context.Context, service, account string) (string, error) {
	if v, ok := k[service+"/"+account]; ok {
		return v, nil
	}
	return "", fmt.Errorf("no such keychain entry")
}

type result struct {
	code           int
	stdout, stderr string
}

func (e *env) runCtx(ctx context.Context, args ...string) result {
	return e.runFull(ctx, nil, args...)
}

// runWith runs kjak with a changed runtime.
func (e *env) runWith(change func(*commandline.Runtime), args ...string) result {
	return e.runFull(context.Background(), change, args...)
}

func (e *env) runFull(ctx context.Context, change func(*commandline.Runtime), args ...string) result {
	var out, errOut bytes.Buffer
	rt := commandline.Runtime{
		Stdin:           strings.NewReader(e.stdin),
		Stdout:          &hookWriter{w: &out, hook: e.afterDelivery},
		Stderr:          &errOut,
		StdinIsTerminal: e.tty,
		ConfigEnv: configure.Env{
			Getenv: func(k string) string {
				if k == "XDG_CONFIG_HOME" {
					return e.xdg
				}
				return ""
			},
			HomeDir: func() (string, error) { return "/nonexistent-home", nil },
			Getwd:   func() (string, error) { return e.project, nil },
			GOOS:    "linux",
		},
		LookupEnv: func(k string) (string, bool) { v, ok := e.vars[k]; return v, ok },
		Keychain:  e.keychainOrDefault(),
		Clock:     clock.Real{},
		Backoff:   delivery.Backoff{Base: time.Millisecond, Max: 2 * time.Millisecond},
	}
	if change != nil {
		change(&rt)
	}
	code := commandline.Execute(ctx, "v0.1.0-test", args, rt)
	e.stdin = ""
	return result{code, out.String(), errOut.String()}
}

func (e *env) keychainOrDefault() commandline.Keychain {
	if e.keychain != nil {
		return e.keychain
	}
	return keychain{"kjak/kc": "keychain-value"}
}

func (e *env) run(args ...string) result { return e.runCtx(context.Background(), args...) }

func (e *env) must(args ...string) result {
	e.t.Helper()
	r := e.run(args...)
	if r.code != 0 {
		e.t.Fatalf("kjak %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), r.code, r.stdout, r.stderr)
	}
	return r
}

func (e *env) writeGlobal(content string) {
	e.t.Helper()
	p := filepath.Join(e.xdg, "kjak", "config.toml")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) writeProject(content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.project, ".kjak.toml"), []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// provider is a webhook endpoint that answers with a fixed status.
type provider struct {
	*httptest.Server
	mu     sync.Mutex
	hits   int
	bodies []string
}

func newProvider(t *testing.T, status int, body string) *provider {
	p := &provider{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.hits++
		p.bodies = append(p.bodies, string(b))
		p.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *provider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits
}

func featherConfig(name, typ, url string) string {
	return fmt.Sprintf("[feathers.%s]\ntype = %q\nwebhook_url = { literal = %q }\n\n", name, typ, url)
}

func TestNoDefaultCommand(t *testing.T) {
	e := newEnv(t)
	r := e.run()
	if r.code != 2 || !strings.Contains(r.stderr, "Usage: kjak <command>") {
		t.Fatalf("%+v", r)
	}
	if r := e.run("hello"); r.code != 2 || !strings.Contains(r.stderr, "unknown command") {
		t.Fatalf("kjak hello is not send: %+v", r)
	}
	if r := e.must("--help"); !strings.Contains(r.stdout, "Commands:") {
		t.Fatal(r.stdout)
	}
	if r := e.must("version"); r.stdout != "kjak version v0.1.0-test\n" {
		t.Fatal(r.stdout)
	}
	if r := e.must("--version"); r.stdout != "kjak version v0.1.0-test\n" {
		t.Fatal(r.stdout)
	}
	for _, c := range []string{"send", "feather", "group", "config", "template"} {
		if r := e.must(c, "--help"); !strings.Contains(r.stdout, "Usage:") {
			t.Errorf("%s --help: %s", c, r.stdout)
		}
	}
}

func TestExitCodeContract(t *testing.T) {
	okP := newProvider(t, 200, "ok")
	badP := newProvider(t, 400, "invalid_payload")
	busyP := newProvider(t, 503, "")
	gwP := newProvider(t, 504, "")
	e := newEnv(t)
	e.writeGlobal(featherConfig("good", "slack", okP.URL) + featherConfig("bad", "slack", badP.URL) +
		featherConfig("busy", "slack", busyP.URL) + featherConfig("gateway", "discord", gwP.URL) +
		"[feathers.envhook]\ntype = \"slack\"\nwebhook_url = { env = \"MISSING_HOOK\" }\n")

	cases := []struct {
		name string
		args []string
		code int
	}{
		{"delivered", []string{"send", "--to", "good", "hi"}, 0},
		{"rejected", []string{"send", "--to", "bad", "hi"}, 1},
		{"partial", []string{"send", "--to", "good,bad", "hi"}, 1},
		{"retries exhausted", []string{"send", "--to", "busy", "--max-retries", "2", "hi"}, 1},
		{"unknown acceptance", []string{"send", "--to", "gateway", "hi"}, 1},
		{"usage", []string{"send", "--to", "good"}, 2},
		{"unknown target", []string{"send", "--to", "ghost", "hi"}, 2},
		{"bad level", []string{"send", "--to", "good", "--level", "loud", "hi"}, 2},
		{"credential missing", []string{"send", "--to", "good,envhook", "hi"}, 2},
		{"no target", []string{"send", "hi"}, 2},
	}
	for _, tc := range cases {
		before := okP.count()
		r := e.run(tc.args...)
		if r.code != tc.code {
			t.Errorf("%s: exit %d, want %d\n%s%s", tc.name, r.code, tc.code, r.stdout, r.stderr)
		}
		if tc.code == 2 && okP.count() != before {
			t.Errorf("%s: a preflight error sent something", tc.name)
		}
	}
	if busyP.count() != 3 {
		t.Errorf("busy: %d attempts, want 3", busyP.count())
	}
	if gwP.count() != 1 {
		t.Errorf("gateway: %d attempts, an unknown acceptance was retried", gwP.count())
	}

	// SIGINT: a canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := e.runCtx(ctx, "send", "--to", "good", "hi"); r.code != 130 {
		t.Errorf("interrupted: exit %d", r.code)
	}
}

type report struct {
	SchemaVersion int    `json:"schema_version"`
	Outcome       string `json:"outcome"`
	DryRun        bool   `json:"dry_run"`
	Level         string `json:"level"`
	Summary       struct {
		Total, Delivered, Failed, Unknown, Skipped int
	} `json:"summary"`
	Results []struct {
		Feather    string   `json:"feather"`
		Type       string   `json:"type"`
		Groups     []string `json:"groups"`
		Status     string   `json:"status"`
		Acceptance string   `json:"acceptance"`
		Attempts   int      `json:"attempts"`
		Error      *struct {
			Code, Message string
		} `json:"error"`
		Skipped *struct {
			Reason, Group string
		} `json:"skipped"`
		History []struct {
			Number     int    `json:"number"`
			Acceptance string `json:"acceptance"`
			StatusCode int    `json:"status_code"`
			Retryable  bool   `json:"retryable"`
		} `json:"history"`
	} `json:"results"`
}

func parseReport(t *testing.T, s string) report {
	t.Helper()
	var r report
	dec := json.NewDecoder(strings.NewReader(s))
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return r
}

func TestSendJSONReport(t *testing.T) {
	okP := newProvider(t, 200, "ok")
	badP := newProvider(t, 503, "")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a_ok", "slack", okP.URL) + featherConfig("b_bad", "discord", badP.URL) +
		featherConfig("c_quiet", "slack", okP.URL) +
		"[feathers.d_off]\ntype = \"slack\"\nenabled = false\nwebhook_url = { literal = \"https://x.example/h\" }\n\n" +
		"[groups.team]\nfeathers = [\"b_bad\", \"d_off\", \"a_ok\"]\n")
	r := e.run("send", "--to", "team", "--to", "c_quiet", "--max-retries", "1", "-o", "json", "--level", "warning", "hello")
	if r.code != 1 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	rep := parseReport(t, r.stdout)
	if rep.SchemaVersion != 1 || rep.Outcome != "partial" || rep.Level != "warning" {
		t.Fatalf("report = %+v", rep)
	}
	order := []string{}
	for _, res := range rep.Results {
		order = append(order, res.Feather)
	}
	if strings.Join(order, " ") != "b_bad d_off a_ok c_quiet" {
		t.Fatalf("order = %v", order)
	}
	b := rep.Results[0]
	if b.Status != "failed" || b.Acceptance != "rejected" || b.Attempts != 2 || len(b.History) != 2 || b.History[0].StatusCode != 503 || !b.History[0].Retryable || b.Error.Code != "server_error" || b.Groups[0] != "team" {
		t.Fatalf("b_bad = %+v", b)
	}
	d := rep.Results[1]
	if d.Status != "skipped" || d.Skipped.Reason != "disabled" || d.Skipped.Group != "team" || d.Attempts != 0 || d.Acceptance != "none" {
		t.Fatalf("d_off = %+v", d)
	}
	if rep.Results[2].Status != "delivered" || rep.Results[2].Acceptance != "confirmed" || rep.Results[3].Status != "delivered" {
		t.Fatalf("results = %+v", rep.Results)
	}
	if rep.Summary.Total != 4 || rep.Summary.Delivered != 2 || rep.Summary.Failed != 1 || rep.Summary.Skipped != 1 {
		t.Fatalf("summary = %+v", rep.Summary)
	}
}

func TestDryRunSendsNothing(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	r := e.must("send", "--to", "a", "--dry-run", "-o", "json", "hi")
	rep := parseReport(t, r.stdout)
	if !rep.DryRun || rep.Results[0].Status != "planned" || p.count() != 0 {
		t.Fatalf("report = %+v, hits = %d", rep, p.count())
	}
	if r := e.must("send", "--to", "a", "--dry-run", "hi"); !strings.Contains(r.stdout, "nothing was sent") {
		t.Fatal(r.stdout)
	}
}

func TestCritical_SecretsNeverInOutput(t *testing.T) {
	const token = "T0SECRET/B0SECRET/XyZsecretTOKEN"
	// a provider that echoes the request path in its error, as some do
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = fmt.Fprintf(w, "no such webhook: %s", r.URL.Path)
	}))
	defer echo.Close()
	e := newEnv(t)
	e.vars["HOOK"] = echo.URL + "/services/" + token
	e.writeGlobal(featherConfig("lit", "slack", echo.URL+"/services/"+token) +
		"[feathers.envd]\ntype = \"discord\"\nwebhook_url = { env = \"HOOK\" }\n\n" +
		"[feathers.mail]\ntype = \"email\"\nhost = \"127.0.0.1\"\nport = 1\nuser = \"u\"\npassword = { literal = \"MailPassw0rdSecret\" }\nfrom = \"a@example.com\"\nto = [\"b@example.com\"]\n")
	var all strings.Builder
	for _, args := range [][]string{
		{"send", "--to", "lit,envd", "hi"},
		{"send", "--to", "lit,envd", "-o", "json", "hi"},
		{"send", "--to", "mail", "--timeout", "2s", "-o", "json", "hi"},
		{"feather", "show", "lit"},
		{"feather", "show", "mail", "-o", "json"},
		{"feather", "list", "-o", "json"},
		{"config", "show"},
		{"config", "show", "-o", "json"},
	} {
		r := e.run(args...)
		all.WriteString(r.stdout + r.stderr)
	}
	out := all.String()
	for _, leak := range []string{"XyZsecretTOKEN", "T0SECRET", "MailPassw0rdSecret"} {
		if strings.Contains(out, leak) {
			t.Fatalf("%q leaked:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatal("nothing was redacted: the test does not test")
	}
}

func TestCritical_ProjectTrustFlow(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("global_one", "slack", p.URL))
	e.writeProject(featherConfig("proj", "slack", p.URL))

	r := e.run("send", "--to", "global_one", "hi")
	if r.code != 2 || !strings.Contains(r.stderr, "not trusted") || p.count() != 0 {
		t.Fatalf("untrusted project used: %+v", r)
	}
	// --no-project is the explicit way around it
	e.must("send", "--to", "global_one", "--no-project", "hi")

	// non-interactive trust needs --yes
	if r := e.run("config", "trust"); r.code != 2 || !strings.Contains(r.stderr, "--yes") {
		t.Fatalf("trust without --yes: %+v", r)
	}
	// interactive: the answer decides
	e.tty, e.stdin = true, "n\n"
	if r := e.run("config", "trust"); r.code != 2 {
		t.Fatalf("declined trust: %+v", r)
	}
	e.stdin = "y\n"
	r = e.must("config", "trust")
	e.tty = false
	if !strings.Contains(r.stdout, "sha256 ") || !strings.Contains(r.stdout, "feather proj: slack, webhook URL from literal:[REDACTED]") {
		t.Fatalf("trust output: %s", r.stdout)
	}
	e.must("send", "--to", "proj", "hi")
	if r := e.must("config", "path"); !strings.Contains(r.stdout, "(trusted)") {
		t.Fatal(r.stdout)
	}

	// a change withdraws the trust
	e.writeProject(featherConfig("proj", "slack", p.URL) + "# edited\n")
	if r := e.run("send", "--to", "proj", "hi"); r.code != 2 {
		t.Fatalf("changed project used: %+v", r)
	}
	if r := e.must("config", "path"); !strings.Contains(r.stdout, "changed since it was trusted") {
		t.Fatal(r.stdout)
	}
	e.must("config", "trust", "--yes")
	e.must("config", "untrust")
	if r := e.run("send", "--to", "proj", "hi"); r.code != 2 {
		t.Fatal("untrusted project used")
	}
}

func TestSendInputs(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL) + "[defaults]\ntargets = [\"a\"]\nlevel = \"warning\"\n\n" +
		"[templates.deploy]\ntitle = \"Deployed {{.service}}\"\nbody = \"{{.service}} {{.version}} is live\"\nlevel = \"success\"\n")
	e.stdin = "from stdin\n\n"
	e.must("send", "--stdin")
	e.stdin = `{"title":"T","body":"from json","level":"error","url":"https://e.example/1"}`
	e.must("send", "--json-input", "-")
	e.must("send", "--template", "deploy", "--var", "service=api", "--var", "version=1.2")
	tf := filepath.Join(e.project, "tpl.toml")
	_ = os.WriteFile(tf, []byte("body = \"file {{.x}}\"\n"), 0o600)
	e.must("send", "--template-file", tf, "--var", "x=1", "--level", "critical")
	e.must("send", "default", "target")

	p.mu.Lock()
	bodies := append([]string(nil), p.bodies...)
	p.mu.Unlock()
	wants := []string{`"text":"[WARNING] from stdin"`, `"text":"[ERROR] T: from json"`, `"text":"[SUCCESS] Deployed api: api 1.2 is live"`, `"text":"[CRITICAL] file 1"`, `"text":"[WARNING] default target"`}
	for i, want := range wants {
		if !strings.Contains(bodies[i], want) {
			t.Errorf("message %d = %s, want %s", i, bodies[i], want)
		}
	}
	if r := e.run("send", "--template", "deploy", "--var", "service=api"); r.code != 2 {
		t.Fatalf("missing variable: %+v", r)
	}
	e.stdin = `{"body":"x","extra":1}`
	if r := e.run("send", "--json-input", "-"); r.code != 2 {
		t.Fatalf("unknown JSON field: %+v", r)
	}
	e.stdin = "   \n"
	if r := e.run("send", "--stdin"); r.code != 2 {
		t.Fatalf("empty body: %+v", r)
	}
}

func TestManagementCommands(t *testing.T) {
	e := newEnv(t)
	e.must("config", "init")
	if r := e.run("config", "init"); r.code != 2 {
		t.Fatal("init overwrote the file")
	}
	e.must("feather", "add", "dev_slack", "--type", "slack", "--secret-env", "DEV_HOOK", "--levels", "warning,error")
	e.must("feather", "add", "dev_discord", "--type", "discord", "--secret-keychain", "kjak/kc", "--username", "bot")
	e.stdin = "MailSecret\n"
	e.must("feather", "add", "mail", "--type", "email", "--host", "smtp.example.com", "--user", "u", "--secret-stdin", "--from", "a@example.com", "--mail-to", "b@example.com")
	if r := e.run("feather", "add", "dev_slack", "--type", "slack", "--secret-env", "X"); r.code != 2 {
		t.Fatal("add replaced without --force")
	}
	if r := e.run("feather", "add", "nat", "--type", "os"); r.code != 2 || !strings.Contains(r.stderr, "not supported") {
		t.Fatalf("os feather: %+v", r)
	}
	if r := e.run("feather", "add", "x", "--type", "slack"); r.code != 2 {
		t.Fatal("slack without secret accepted")
	}
	e.must("group", "set", "development", "dev_slack", "dev_discord")
	if r := e.run("group", "set", "nested", "development"); r.code != 2 || !strings.Contains(r.stderr, "nested") {
		t.Fatalf("nested group: %+v", r)
	}
	if r := e.run("feather", "remove", "dev_slack"); r.code != 2 || !strings.Contains(r.stderr, "member of group") {
		t.Fatalf("remove of a member: %+v", r)
	}
	e.must("feather", "disable", "dev_discord")
	e.must("template", "set", "deploy", "--title", "Deployed {{.service}}", "--body", "{{.service}} is live", "--level", "success")
	if r := e.run("template", "set", "broken", "--body", "{{.x"); r.code != 2 {
		t.Fatal("broken template accepted")
	}

	r := e.must("feather", "list", "-o", "json")
	var fl struct {
		Feathers []struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			WebhookURL string `json:"webhook_url"`
			Password   string `json:"password"`
			Enabled    bool   `json:"enabled"`
		}
	}
	_ = json.Unmarshal([]byte(r.stdout), &fl)
	if len(fl.Feathers) != 3 || fl.Feathers[0].Name != "dev_discord" || fl.Feathers[0].Enabled || fl.Feathers[0].WebhookURL != "keychain:kjak/kc" || fl.Feathers[1].WebhookURL != "env:DEV_HOOK" || fl.Feathers[2].Password != "literal:[REDACTED]" {
		t.Fatalf("list = %s", r.stdout)
	}
	if r := e.must("feather", "show", "dev_slack"); !strings.Contains(r.stdout, "warning, error") {
		t.Fatal(r.stdout)
	}
	if r := e.must("group", "show", "development"); !strings.Contains(r.stdout, "dev_slack, dev_discord") {
		t.Fatal(r.stdout)
	}
	if r := e.must("template", "render", "deploy", "--var", "service=api", "-o", "json"); !strings.Contains(r.stdout, `"title": "Deployed api"`) {
		t.Fatal(r.stdout)
	}
	e.must("template", "list")
	e.must("group", "list")
	e.must("config", "validate")
	e.must("config", "show")

	// a group whose members are all excluded sends nothing and succeeds
	r = e.run("send", "--to", "development", "--level", "info", "-o", "json", "hi")
	rep := parseReport(t, r.stdout)
	if r.code != 0 || rep.Summary.Skipped != 2 {
		t.Fatalf("exit %d, report %+v", r.code, rep)
	}

	e.must("group", "remove", "development")
	e.must("feather", "remove", "dev_slack")
	e.must("template", "remove", "deploy")
	if r := e.must("feather", "list"); strings.Contains(r.stdout, "dev_slack") {
		t.Fatal(r.stdout)
	}
	if r := e.must("feather", "types"); !strings.Contains(r.stdout, "OS-native notifications") {
		t.Fatal(r.stdout)
	}
}

func TestProjectScopeEdits(t *testing.T) {
	e := newEnv(t)
	e.must("feather", "add", "p", "--type", "slack", "--secret-env", "H", "--scope", "project")
	r := e.must("feather", "list")
	if !strings.Contains(r.stdout, "project") {
		t.Fatalf("list = %s", r.stdout)
	}
	// the origin decides the file of a later change
	e.must("feather", "disable", "p")
	b, _ := os.ReadFile(filepath.Join(e.project, ".kjak.toml"))
	if !strings.Contains(string(b), "enabled = false") {
		t.Fatalf("project file = %s", b)
	}
	// an untrusted project file is not edited
	e.writeProject(string(b) + "# changed by someone\n")
	if r := e.run("feather", "add", "q", "--type", "slack", "--secret-env", "H", "--scope", "project"); r.code != 2 {
		t.Fatalf("untrusted project edited: %+v", r)
	}
}
