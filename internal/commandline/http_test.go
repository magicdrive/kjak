package commandline_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
)

// api is an HTTP API that records requests and answers with a status (the
// handler may echo a header back).
type api struct {
	*httptest.Server
	mu     sync.Mutex
	reqs   []*http.Request
	bodies []string
	status int
	echo   string
}

func newAPI(t *testing.T, status int) *api {
	a := &api{status: status}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		a.mu.Lock()
		a.reqs = append(a.reqs, r.Clone(r.Context()))
		a.bodies = append(a.bodies, string(b))
		st, echo := a.status, a.echo
		a.mu.Unlock()
		w.WriteHeader(st)
		if echo != "" {
			_, _ = fmt.Fprintf(w, "you sent %s: %s", echo, r.Header.Get(echo))
		}
	}))
	t.Cleanup(a.Close)
	return a
}

func (a *api) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.reqs)
}

func (a *api) req(i int) *http.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reqs[i]
}

// httpFeather is an http feather that may reach the test API (http to the
// loopback address, allowed explicitly).
func httpFeather(name, url, extra string) string {
	return fmt.Sprintf("[feathers.%s]\ntype = \"http\"\nurl = %q\nallow_http = true\nallow_networks = [\"127.0.0.1/32\"]\n%s\n", name, url, extra)
}

type httpReport struct {
	Outcome string `json:"outcome"`
	Results []struct {
		Feather    string `json:"feather"`
		Type       string `json:"type"`
		Status     string `json:"status"`
		Acceptance string `json:"acceptance"`
		Attempts   int    `json:"attempts"`
		Error      *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"results"`
}

func parseHTTPReport(t *testing.T, s string) httpReport {
	t.Helper()
	var r httpReport
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatalf("%v\n%s", err, s)
	}
	return r
}

func TestHTTPFeatherSend(t *testing.T) {
	a := newAPI(t, 201)
	e := newEnv(t)
	e.writeGlobal(httpFeather("api", a.URL+"/v1/alerts", "headers = { \"X-Team\" = \"ops\" }\nquery = { source = \"kjak\" }\nsuccess_status = [\"201\"]\n[feathers.api.auth]\ntype = \"bearer\"\ntoken = { secret = \"api.token\" }\n[feathers.api.body]\ntemplate = '{\"summary\": \"[{{.level}}] {{.title}}\", \"details\": \"{{.body}}\"}'"))
	e.writeFile(e.userSecrets(), "[api]\ntoken = \"tok-SECRET-1\"\n", 0o600)
	r := e.must("send", "--to", "api", "--title", "Deploy", "-o", "json", `api is "live"`)
	rep := parseHTTPReport(t, r.stdout)
	if rep.Outcome != "success" || rep.Results[0].Type != "http" || a.count() != 1 {
		t.Fatalf("%s", r.stdout)
	}
	req := a.req(0)
	if req.Method != "POST" || req.URL.Path != "/v1/alerts" || req.URL.Query().Get("source") != "kjak" || req.Header.Get("Authorization") != "Bearer tok-SECRET-1" || req.Header.Get("X-Team") != "ops" {
		t.Fatalf("request: %s %s %v", req.Method, req.URL, req.Header)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(a.bodies[0]), &body); err != nil || body["summary"] != "[info] Deploy" || body["details"] != `api is "live"` {
		t.Fatalf("body %s %v", a.bodies[0], err)
	}
	// dry run: nothing sent
	e.must("send", "--to", "api", "--dry-run", "x")
	if a.count() != 1 {
		t.Fatal("a dry run sent")
	}
	r = e.must("feather", "show", "api")
	for _, want := range []string{"method", "POST", "auth", "bearer from secret:api.token", "allows http; allows networks 127.0.0.1/32"} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("feather show lacks %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "tok-SECRET-1") {
		t.Fatal("feather show printed the token")
	}
}

// What an API echoes back is redacted: the token, and the encoded basic
// credentials.
func TestCritical_HTTPErrorsRedactCredentials(t *testing.T) {
	a := newAPI(t, 401)
	a.echo = "Authorization"
	e := newEnv(t)
	e.writeGlobal(httpFeather("bearer", a.URL, "[feathers.bearer.auth]\ntype = \"bearer\"\ntoken = { secret = \"api.token\" }") +
		httpFeather("basic", a.URL, "[feathers.basic.auth]\ntype = \"basic\"\nusername = \"kjak\"\npassword = { secret = \"api.password\" }") +
		httpFeather("header", a.URL+"/h", "[feathers.header.auth]\ntype = \"header\"\nheader = \"Authorization\"\nvalue = { secret = \"api.raw\" }") +
		"[feathers.secret_url]\ntype = \"http\"\nurl_secret = { secret = \"api.url\" }\nallow_http = true\nallow_networks = [\"127.0.0.1/32\"]\n")
	e.writeFile(e.userSecrets(), fmt.Sprintf("[api]\ntoken = \"tok-SECRET-2\"\npassword = \"pw-SECRET-3\"\nraw = \"Token raw-SECRET-4\"\nurl = %q\n", a.URL+"/hooks/PATH-SECRET-5?key=Q-SECRET-6"), 0o600)
	r := e.run("send", "--to", "bearer,basic,header,secret_url", "-o", "json", "x")
	if r.code != 1 || a.count() != 4 {
		t.Fatalf("exit %d, %d requests\n%s%s", r.code, a.count(), r.stdout, r.stderr)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("kjak:pw-SECRET-3"))
	for _, s := range []string{"tok-SECRET-2", "pw-SECRET-3", encoded, "raw-SECRET-4", "PATH-SECRET-5", "Q-SECRET-6"} {
		if strings.Contains(r.stdout+r.stderr, s) {
			t.Errorf("%s was printed:\n%s", s, r.stdout)
		}
	}
	if !strings.Contains(r.stdout, "[REDACTED]") {
		t.Fatalf("nothing was redacted: the test does not test\n%s", r.stdout)
	}
}

// The engine's retries follow the feather's policy: POST is not repeated
// without a key, PUT is, max_attempts caps them, and every attempt of a
// send carries the same idempotency key.
func TestHTTPRetriesThroughTheEngine(t *testing.T) {
	cases := []struct {
		extra    string
		attempts int
	}{
		{"", 1},
		{"method = \"PUT\"", 3},
		{"method = \"PUT\"\n[feathers.x.retry]\nmax_attempts = 2", 2},
		{"idempotency_key = true", 3},
		{"[feathers.x.retry]\npolicy = \"always\"", 3},
		{"method = \"GET\"\n[feathers.x.retry]\npolicy = \"never\"", 1},
	}
	for _, c := range cases {
		a := newAPI(t, 503)
		e := newEnv(t)
		e.writeGlobal(httpFeather("x", a.URL, c.extra))
		r := e.run("send", "--to", "x", "-o", "json", "hi")
		rep := parseHTTPReport(t, r.stdout)
		if r.code != 1 || rep.Results[0].Attempts != c.attempts || a.count() != c.attempts {
			t.Errorf("%q: exit %d, %d attempts, %d requests", c.extra, r.code, rep.Results[0].Attempts, a.count())
		}
		if c.extra == "idempotency_key = true" {
			k := a.req(0).Header.Get("Idempotency-Key")
			for i := 1; i < a.count(); i++ {
				if a.req(i).Header.Get("Idempotency-Key") != k || k == "" {
					t.Errorf("attempt %d: another idempotency key", i+1)
				}
			}
		}
	}
}

// An address the policy refuses is a failed delivery with the code
// blocked: nothing reached it.
func TestCritical_HTTPBlockedAddressThroughTheCLI(t *testing.T) {
	a := newAPI(t, 200)
	e := newEnv(t)
	e.writeGlobal("[feathers.x]\ntype = \"http\"\nurl = \"" + a.URL + "\"\nallow_http = true\n")
	r := e.run("send", "--to", "x", "-o", "json", "hi")
	rep := parseHTTPReport(t, r.stdout)
	if r.code != 1 || a.count() != 0 || rep.Results[0].Error == nil || rep.Results[0].Error.Code != "blocked" || rep.Results[0].Acceptance != "rejected" || rep.Results[0].Attempts != 1 {
		t.Fatalf("exit %d, %d requests\n%s", r.code, a.count(), r.stdout)
	}
	// http needs its opt-in: a configuration error, nothing sent
	e.writeGlobal("[feathers.x]\ntype = \"http\"\nurl = \"" + a.URL + "\"\nallow_networks = [\"127.0.0.1/32\"]\n")
	if r := e.run("send", "--to", "x", "hi"); r.code != 2 || !strings.Contains(r.stderr, "allow_http") {
		t.Fatalf("%+v", r)
	}
}

// A project's http feather is shown with its address policy when it is
// trusted, and reads none of the user's secrets it was not allowed.
func TestHTTPFeatherInAProject(t *testing.T) {
	a := newAPI(t, 200)
	e := newEnv(t)
	e.writeFile(e.userSecrets(), "[slack]\nbot_token = \"xoxb-USER-SECRET\"\n", 0o600)
	e.writeProject(httpFeather("exfil", a.URL, "[feathers.exfil.auth]\ntype = \"bearer\"\ntoken = { secret = \"slack.bot_token\" }"))
	r := e.run("config", "trust", "--yes")
	if r.code != 2 || !strings.Contains(r.stdout, "feather exfil: http POST "+a.URL+", auth bearer from secret:slack.bot_token") || !strings.Contains(r.stdout, "address policy: allows http; allows networks 127.0.0.1/32") {
		t.Fatalf("%+v", r)
	}
	if r := e.run("send", "--to", "exfil", "hi"); r.code != 2 || a.count() != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestFeatherAddHTTPIsInTheFile(t *testing.T) {
	e := newEnv(t)
	if r := e.run("feather", "add", "x", "--type", "http"); r.code != 2 || !strings.Contains(r.stderr, "docs/http.md") {
		t.Fatalf("%+v", r)
	}
	if r := e.must("feather", "types"); !strings.Contains(r.stdout, "http ") {
		t.Fatal(r.stdout)
	}
}

// A project's http feather sends to a URL of the project's choosing: its
// env and keychain references are allowed by name at the trust, like its
// references to the user's secrets file. A global feather is not gated.
func TestCritical_ProjectHTTPEnvSecretsNeedAGrant(t *testing.T) {
	a := newAPI(t, 200)
	e := newEnv(t)
	e.vars["AWS_SECRET_ACCESS_KEY"] = "AWS-SECRET-VALUE"
	e.writeProject(httpFeather("exfil", a.URL, "[feathers.exfil.auth]\ntype = \"header\"\nheader = \"X-Key\"\nvalue = { env = \"AWS_SECRET_ACCESS_KEY\" }"))
	r := e.run("config", "trust", "--yes")
	if r.code != 2 || !strings.Contains(r.stdout, "env:AWS_SECRET_ACCESS_KEY") || !strings.Contains(r.stderr, "--allow-user-secrets") {
		t.Fatalf("trust --yes: %+v", r)
	}
	// a trust that does not name it (an earlier kjak's, or a plain one)
	src, err := configure.ReadFile(filepath.Join(e.project, ".kjak.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := configure.Trust(filepath.Join(e.xdg, "kjak", "trust.toml"), src.Path, src.Hash, time.Now()); err != nil {
		t.Fatal(err)
	}
	r = e.run("send", "--to", "exfil", "hi")
	if r.code != 2 || a.count() != 0 || !strings.Contains(r.stderr, "does not allow") || strings.Contains(r.stderr, "AWS-SECRET-VALUE") {
		t.Fatalf("ungranted env secret: %+v", r)
	}
	e.must("config", "trust", "--yes", "--allow-user-secrets")
	e.must("send", "--to", "exfil", "hi")
	if a.count() != 1 || a.req(0).Header.Get("X-Key") != "AWS-SECRET-VALUE" {
		t.Fatal("the granted secret was not sent")
	}
	// the global file is the user's own: not gated
	e.writeGlobal(httpFeather("mine", a.URL, "[feathers.mine.auth]\ntype = \"bearer\"\ntoken = { env = \"AWS_SECRET_ACCESS_KEY\" }"))
	e.must("send", "--no-project", "--to", "mine", "hi")
}
