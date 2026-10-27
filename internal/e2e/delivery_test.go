package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The end-to-end delivery tests: the built kjak against local HTTP APIs
// (httptest, loopback only: allow_http and allow_networks say so) and the
// test plugin built with go build, through a group. Nothing leaves this
// machine.

const (
	apiToken = "TOKEN-e2e-S3CR3T-1234" // auth.token of the http feathers
	pdKey    = "PDKEY-e2e-S3CR3T-5678" // the plugin's routing_key
)

// e2eAPI is a local HTTP API: it answers with the statuses given in turn
// (the last one repeated), records the requests, and echoes the
// Authorization header in its body when asked (as some providers do in
// their errors).
type e2eAPI struct {
	*httptest.Server
	mu       sync.Mutex
	statuses []int
	echo     bool
	auth     []string
	methods  []string
	hits     atomic.Int32
}

func newE2EAPI(t *testing.T, echo bool, statuses ...int) *e2eAPI {
	a := &e2eAPI{statuses: statuses, echo: echo}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := int(a.hits.Add(1))
		a.mu.Lock()
		a.auth = append(a.auth, r.Header.Get("Authorization"))
		a.methods = append(a.methods, r.Method)
		st := a.statuses[min(n, len(a.statuses))-1]
		a.mu.Unlock()
		if st == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "0")
		}
		w.WriteHeader(st)
		if a.echo {
			_, _ = fmt.Fprintf(w, "invalid credentials: %s", r.Header.Get("Authorization"))
		}
	}))
	t.Cleanup(a.Close)
	return a
}

func (a *e2eAPI) seen() (auth, methods []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.auth...), append([]string(nil), a.methods...)
}

// e2eReport is the JSON report (docs/cli.md), every field a test checks.
type e2eReport struct {
	SchemaVersion int    `json:"schema_version"`
	Outcome       string `json:"outcome"`
	Canceled      bool   `json:"canceled"`
	DryRun        bool   `json:"dry_run"`
	Level         string `json:"level"`
	Summary       struct {
		Total, Delivered, Failed, Unknown, Skipped int
	} `json:"summary"`
	Results []e2eResult `json:"results"`
	Error   *e2eError   `json:"error"`
}

type e2eError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type e2eResult struct {
	Feather    string    `json:"feather"`
	Type       string    `json:"type"`
	Groups     []string  `json:"groups"`
	Status     string    `json:"status"`
	Acceptance string    `json:"acceptance"`
	Attempts   int       `json:"attempts"`
	Error      *e2eError `json:"error"`
	Skipped    *struct {
		Reason string `json:"reason"`
		Group  string `json:"group"`
	} `json:"skipped"`
	History []struct {
		Number     int       `json:"number"`
		Acceptance string    `json:"acceptance"`
		Retryable  bool      `json:"retryable"`
		StatusCode int       `json:"status_code"`
		Error      *e2eError `json:"error"`
	} `json:"history"`
}

// resultKeys are the keys of every result of schema version 1.
var resultKeys = []string{"feather", "type", "groups", "status", "acceptance", "attempts", "duration_ms", "error", "skipped", "history"}

// parseReport decodes a JSON report and checks the contract every report
// keeps: schema version 1, every top-level and result key present (fields
// are only added within a schema version), a summary that adds up.
func parseReport(t *testing.T, stdout string) e2eReport {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &top); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	for _, k := range []string{"schema_version", "outcome", "canceled", "dry_run", "duration_ms", "level", "summary", "results"} {
		if _, ok := top[k]; !ok {
			t.Fatalf("report without %q: %s", k, stdout)
		}
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(top["results"], &raw); err != nil {
		t.Fatal(err)
	}
	for _, r := range raw {
		for _, k := range resultKeys {
			if _, ok := r[k]; !ok {
				t.Fatalf("result without %q: %s", k, stdout)
			}
		}
	}
	var rep e2eReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.SchemaVersion != 1 {
		t.Fatalf("schema_version %d", rep.SchemaVersion)
	}
	s := rep.Summary
	if rep.Outcome != "error" && (s.Total != len(rep.Results) || s.Delivered+s.Failed+s.Skipped > s.Total) {
		t.Fatalf("summary %+v for %d results", s, len(rep.Results))
	}
	return rep
}

func (r e2eReport) result(t *testing.T, name string) e2eResult {
	t.Helper()
	for _, x := range r.Results {
		if x.Feather == name {
			return x
		}
	}
	t.Fatalf("no result for %s", name)
	return e2eResult{}
}

// installPlugin puts the test plugin on the sandbox's PATH as
// kjak-feather-<name>.
func (s *sandbox) installPlugin(name string) {
	s.t.Helper()
	b, err := os.ReadFile(plugin)
	if err != nil {
		s.t.Fatal(err)
	}
	dir := filepath.Join(filepath.Dir(s.dir), "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.t.Fatal(err)
	}
	file := "kjak-feather-" + name
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, file), b, 0o755); err != nil {
		s.t.Fatal(err)
	}
	settlePlugin(s.t, filepath.Join(dir, file))
	for i, kv := range s.env {
		if strings.HasPrefix(kv, "PATH=") && !strings.Contains(kv, dir) {
			s.env[i] = "PATH=" + dir + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
		}
	}
}

// writeConfig writes the global configuration and the user's secrets file
// (mode 0600), the way a user would.
func (s *sandbox) writeConfig(config, secrets string) {
	s.t.Helper()
	dir := filepath.Join(filepath.Dir(s.dir), "xdg", "kjak")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0o600); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.toml"), []byte(secrets), 0o600); err != nil {
		s.t.Fatal(err)
	}
}

// httpFeatherTOML is an http feather to a local API, its bearer token from
// the secrets file.
func httpFeatherTOML(name, url, extra string) string {
	return fmt.Sprintf(`[feathers.%s]
type = "http"
url = %q
allow_http = true
allow_networks = ["127.0.0.1/32", "::1/128"]
%s
[feathers.%s.auth]
type = "bearer"
token = { secret = "api.token" }

`, name, url+"/hook", extra, name)
}

func pluginFeatherTOML(name, mode, extra string) string {
	return fmt.Sprintf(`[feathers.%s]
type = "external"
plugin = "pager"
settings = { mode = %q }
secrets = { routing_key = { secret = "pd.key" } }
%s
`, name, mode, extra)
}

const e2eSecrets = "[api]\ntoken = \"" + apiToken + "\"\n[pd]\nkey = \"" + pdKey + "\"\n"

// noSecret fails when a secret value appears in the output or the
// diagnostics of a run.
func noSecret(t *testing.T, what string, r run) {
	t.Helper()
	for _, secret := range []string{apiToken, pdKey, "S3CR3T"} {
		if strings.Contains(r.stdout, secret) || strings.Contains(r.stderr, secret) {
			t.Fatalf("%s: a secret is printed:\nstdout:\n%s\nstderr:\n%s", what, r.stdout, r.stderr)
		}
	}
}

// A group of every kind of member, sent at once: delivered (http and
// plugin), delivered after a retry, rejected (http, and a plugin that
// echoes its secret), skipped (disabled, level filter). The exit status,
// the JSON contract, the requests made, and that no secret is printed, in
// JSON and in text.
func TestBinaryGroupDelivery(t *testing.T) {
	ok := newE2EAPI(t, false, 200)
	fail := newE2EAPI(t, true, 401)
	flaky := newE2EAPI(t, false, 503, 200)
	muted := newE2EAPI(t, false, 200)
	critical := newE2EAPI(t, false, 200)

	s := newSandbox(t)
	s.installPlugin("pager")
	members := []string{"api_ok", "api_fail", "api_retry", "pager_ok", "pager_leak", "muted", "critical_only"}
	config := httpFeatherTOML("api_ok", ok.URL, "") +
		httpFeatherTOML("api_fail", fail.URL, "") +
		// PUT is idempotent: retried under the default policy (safe)
		httpFeatherTOML("api_retry", flaky.URL, "method = \"PUT\"") +
		pluginFeatherTOML("pager_ok", "ok", "") +
		pluginFeatherTOML("pager_leak", "echo-secret", "") +
		httpFeatherTOML("muted", muted.URL, "enabled = false") +
		httpFeatherTOML("critical_only", critical.URL, "levels = [\"critical\"]") +
		fmt.Sprintf("[groups.everyone]\nfeathers = [\"%s\"]\n", strings.Join(members, `", "`))
	s.writeConfig(config, e2eSecrets)
	if r := s.must("config", "validate"); strings.Contains(r.stdout+r.stderr, "S3CR3T") {
		t.Fatalf("config validate prints a secret: %+v", r)
	}

	// the dry run resolves everything and sends nothing
	r := s.kjak("", "send", "--to", "everyone", "--level", "error", "--dry-run", "-o", "json", "deploy failed")
	noSecret(t, "dry run", r)
	rep := parseReport(t, r.stdout)
	if r.code != 0 || !rep.DryRun || rep.result(t, "api_ok").Status != "planned" || rep.result(t, "pager_ok").Status != "planned" {
		t.Fatalf("dry run: exit %d %s", r.code, r.stdout)
	}
	if n := ok.hits.Load() + fail.hits.Load() + flaky.hits.Load(); n != 0 {
		t.Fatalf("the dry run made %d requests", n)
	}

	r = s.kjak("", "send", "--to", "everyone", "--level", "error", "--title", "CI", "-o", "json", "deploy failed")
	noSecret(t, "send -o json", r)
	if r.code != 1 {
		t.Fatalf("a partial send: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	rep = parseReport(t, r.stdout)
	if rep.Outcome != "partial" || rep.Canceled || rep.DryRun || rep.Level != "error" || rep.Error != nil {
		t.Fatalf("report: %+v", rep)
	}
	if got := rep.Summary; got.Total != 7 || got.Delivered != 3 || got.Failed != 2 || got.Unknown != 0 || got.Skipped != 2 {
		t.Fatalf("summary %+v", got)
	}
	for i, name := range members {
		if rep.Results[i].Feather != name || len(rep.Results[i].Groups) != 1 || rep.Results[i].Groups[0] != "everyone" {
			t.Fatalf("result %d: %+v (want %s of group everyone, in order)", i, rep.Results[i], name)
		}
	}

	// success: http and plugin, the secrets given where they belong
	for _, name := range []string{"api_ok", "pager_ok"} {
		if x := rep.result(t, name); x.Status != "delivered" || x.Acceptance != "confirmed" || x.Attempts != 1 || x.Error != nil {
			t.Fatalf("%s: %+v", name, x)
		}
	}
	if auth, methods := ok.seen(); len(auth) != 1 || auth[0] != "Bearer "+apiToken || methods[0] != "POST" {
		t.Fatalf("api_ok got %v %v", auth, methods)
	}
	if x := rep.result(t, "pager_ok"); x.Type != "external" {
		t.Fatalf("pager_ok type %q", x.Type)
	}

	// retry: 503 (Retry-After: 0), then 200
	x := rep.result(t, "api_retry")
	if x.Status != "delivered" || x.Attempts != 2 || len(x.History) != 2 || flaky.hits.Load() != 2 {
		t.Fatalf("api_retry: %+v (%d requests)", x, flaky.hits.Load())
	}
	if h := x.History[0]; h.StatusCode != 503 || !h.Retryable || h.Acceptance != "rejected" || h.Error == nil || h.Error.Code != "server_error" {
		t.Fatalf("api_retry first attempt: %+v", h)
	}
	if _, methods := flaky.seen(); methods[0] != "PUT" || methods[1] != "PUT" {
		t.Fatalf("api_retry methods %v", methods)
	}

	// failure: a 401 that echoes the token, a plugin that prints its
	// secret on stderr and in its error: rejected, permanent, redacted
	x = rep.result(t, "api_fail")
	if x.Status != "failed" || x.Acceptance != "rejected" || x.Attempts != 1 || x.Error == nil || x.Error.Code != "rejected" || len(x.History) != 1 || x.History[0].StatusCode != 401 {
		t.Fatalf("api_fail: %+v", x)
	}
	if !strings.Contains(x.Error.Message, "[REDACTED]") {
		t.Fatalf("api_fail: the echoed token is not shown redacted: %q", x.Error.Message)
	}
	x = rep.result(t, "pager_leak")
	if x.Status != "failed" || x.Acceptance != "rejected" || x.Error == nil || x.Error.Code != "rejected" || !strings.Contains(x.Error.Message, "[REDACTED]") {
		t.Fatalf("pager_leak: %+v", x)
	}

	// skipped: disabled, level filter; nothing sent to them
	for name, reason := range map[string]string{"muted": "disabled", "critical_only": "level_filter"} {
		x := rep.result(t, name)
		if x.Status != "skipped" || x.Acceptance != "none" || x.Attempts != 0 || x.Skipped == nil || x.Skipped.Reason != reason || x.Skipped.Group != "everyone" || x.Error != nil || len(x.History) != 0 {
			t.Fatalf("%s: %+v", name, x)
		}
	}
	if muted.hits.Load() != 0 || critical.hits.Load() != 0 {
		t.Fatal("a skipped member was sent to")
	}

	// the same in text: exit 1, one line per member, no secret
	r = s.kjak("", "send", "--to", "everyone", "--level", "error", "deploy failed")
	noSecret(t, "send (text)", r)
	if r.code != 1 || !strings.Contains(r.stdout, "partial: 3 delivered, 2 failed, 2 skipped") {
		t.Fatalf("text: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	for _, line := range []string{"ok    api_ok (http)", "FAIL  api_fail (http)", "ok    pager_ok (external)", "FAIL  pager_leak (external)", "skip  muted (http): disabled (group everyone)", "skip  critical_only (http): level_filter (group everyone)"} {
		if !strings.Contains(r.stdout, line) {
			t.Fatalf("text output without %q:\n%s", line, r.stdout)
		}
	}
}

// The outcome and the exit status of a send that only succeeds, only
// fails, or is refused before anything is sent (exit 2 with the JSON error
// document).
func TestBinaryDeliveryExitContract(t *testing.T) {
	ok := newE2EAPI(t, false, 204)
	fail := newE2EAPI(t, false, 400)
	s := newSandbox(t)
	s.installPlugin("pager")
	s.writeConfig(httpFeatherTOML("api_ok", ok.URL, "")+httpFeatherTOML("api_fail", fail.URL, "")+
		pluginFeatherTOML("pager_ok", "ok", "")+pluginFeatherTOML("pager_bad", "badjson", ""), e2eSecrets)

	r := s.kjak("", "send", "--to", "api_ok,pager_ok", "-o", "json", "x")
	if rep := parseReport(t, r.stdout); r.code != 0 || rep.Outcome != "success" || rep.Summary.Delivered != 2 {
		t.Fatalf("success: exit %d %s", r.code, r.stdout)
	}
	r = s.kjak("", "send", "--to", "api_fail", "-o", "json", "x")
	if rep := parseReport(t, r.stdout); r.code != 1 || rep.Outcome != "failure" || rep.Summary.Failed != 1 || rep.result(t, "api_fail").History[0].StatusCode != 400 {
		t.Fatalf("failure: exit %d %s", r.code, r.stdout)
	}
	// a plugin that breaks the protocol: failed, never confirmed
	r = s.kjak("", "send", "--to", "pager_bad", "-o", "json", "x")
	if rep := parseReport(t, r.stdout); r.code != 1 || rep.Outcome != "failure" || rep.result(t, "pager_bad").Status != "failed" {
		t.Fatalf("protocol error: exit %d %s", r.code, r.stdout)
	}
	// preflight: an unknown target, a secret that does not resolve
	r = s.kjak("", "send", "--to", "nosuch", "-o", "json", "x")
	if rep := parseReport(t, r.stdout); r.code != 2 || rep.Outcome != "error" || rep.Error == nil || rep.Error.Code != "routing" || len(rep.Results) != 0 || r.stderr == "" {
		t.Fatalf("unknown target: exit %d %s %s", r.code, r.stdout, r.stderr)
	}
	s.writeConfig(httpFeatherTOML("api_ok", ok.URL, ""), "[other]\nx = \"y\"\n")
	before := ok.hits.Load()
	r = s.kjak("", "send", "--to", "api_ok", "-o", "json", "x")
	if rep := parseReport(t, r.stdout); r.code != 2 || rep.Error == nil || rep.Error.Code != "credential" || ok.hits.Load() != before {
		t.Fatalf("missing secret: exit %d %s %s", r.code, r.stdout, r.stderr)
	}
}

// A plugin that never answers is stopped at the send's deadline: the
// delivery fails (the plugin may have acted: acceptance unknown), the
// other members are not held, and kjak exits 1, on every OS.
func TestBinaryPluginDeadline(t *testing.T) {
	ok := newE2EAPI(t, false, 200)
	s := newSandbox(t)
	s.installPlugin("pager")
	s.writeConfig(httpFeatherTOML("api_ok", ok.URL, "")+pluginFeatherTOML("pager_hang", "sleep", "")+
		"[groups.g]\nfeathers = [\"api_ok\", \"pager_hang\"]\n", e2eSecrets)
	r := s.kjak("", "send", "--to", "g", "--timeout", "3s", "-o", "json", "x")
	noSecret(t, "deadline", r)
	rep := parseReport(t, r.stdout)
	x := rep.result(t, "pager_hang")
	if r.code != 1 || rep.Outcome != "partial" || rep.result(t, "api_ok").Status != "delivered" || x.Status != "failed" || x.Error == nil {
		t.Fatalf("deadline: exit %d %s %s", r.code, r.stdout, r.stderr)
	}
	if x.Error.Code != "deadline" || x.Acceptance != "unknown" {
		t.Fatalf("deadline: %+v", x)
	}
}

// settlePlugin runs a new copy of the test plugin once, to its end and with
// no deadline, before kjak runs it under a timeout: the first exec of an
// executable just written waits for the system's scan of it (macOS:
// seconds under load; see settle in internal/feather/external), which is
// the machine's time, not the plugin's. With an empty request the plugin
// exits 8.
func settlePlugin(t testing.TB, path string) {
	t.Helper()
	start := time.Now()
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 8 {
		t.Fatalf("the first run of %s: %v, want exit status 8", path, err)
	}
	if d := time.Since(start); d > time.Second {
		t.Logf("the first run of %s took %s (the system's scan of a new executable)", path, d)
	}
}
