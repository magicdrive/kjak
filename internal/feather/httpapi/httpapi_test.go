package httpapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
)

func msg() core.Message {
	return core.Message{Title: "Deploy", Body: "api 1.2 is live", Level: core.LevelSuccess, URL: "https://ci.example/1"}
}

func testConfig(u string) configure.FeatherConfig {
	return configure.FeatherConfig{Type: configure.TypeHTTP, URL: u}
}

// local is the configuration of a feather that may reach the httptest
// server (http, loopback).
func local(u string) configure.FeatherConfig {
	fc := testConfig(u)
	fc.AllowHTTP = true
	fc.AllowNetworks = []string{"127.0.0.1/32", "::1/128"}
	return fc
}

func mustFeather(t *testing.T, fc configure.FeatherConfig, opt Options) *Feather {
	t.Helper()
	return mustFeatherAuth(t, fc, Auth{}, opt)
}

func mustFeatherAuth(t *testing.T, fc configure.FeatherConfig, auth Auth, opt Options) *Feather {
	t.Helper()
	if err := configure.ValidateFeather(fc); err != nil {
		t.Fatalf("invalid test configuration: %v", err)
	}
	raw := fc.URL
	f, err := New("api", fc, raw, auth, opt)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// recorder is an API that records every request and answers from a script.
type recorder struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []recorded
	// answer writes the response of request n (1-based)
	answer func(w http.ResponseWriter, r *http.Request, n int)
}

type recorded struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   string
}

func newRecorder(t *testing.T, answer func(w http.ResponseWriter, r *http.Request, n int)) *recorder {
	rec := &recorder{answer: answer}
	rec.Server = httptest.NewServer(http.HandlerFunc(rec.serve))
	t.Cleanup(rec.Close)
	return rec
}

func (rec *recorder) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	rec.mu.Lock()
	rec.reqs = append(rec.reqs, recorded{r.Method, r.URL.Path, r.URL.Query(), r.Header.Clone(), string(b)})
	n := len(rec.reqs)
	rec.mu.Unlock()
	if rec.answer == nil {
		w.WriteHeader(200)
		return
	}
	rec.answer(w, r, n)
}

func (rec *recorder) got() []recorded {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]recorded(nil), rec.reqs...)
}

func status(code int) func(http.ResponseWriter, *http.Request, int) {
	return func(w http.ResponseWriter, _ *http.Request, _ int) { w.WriteHeader(code) }
}

func TestMethodsHeadersAndBodies(t *testing.T) {
	rec := newRecorder(t, nil)
	for _, m := range configure.HTTPMethods {
		fc := local(rec.URL + "/v1/" + strings.ToLower(m))
		fc.Method = m
		fc.Headers = map[string]string{"X-Team": "ops", "Accept": "application/json"}
		fc.Query = map[string]string{"source": "kjak", "b": "2"}
		o := mustFeather(t, fc, Options{}).Deliver(context.Background(), msg())
		if o.Acceptance != core.AcceptanceConfirmed || o.StatusCode != 200 {
			t.Fatalf("%s: %+v", m, o)
		}
	}
	got := rec.got()
	if len(got) != 5 {
		t.Fatalf("%d requests", len(got))
	}
	for i, m := range configure.HTTPMethods {
		r := got[i]
		if r.Method != m || r.Path != "/v1/"+strings.ToLower(m) || r.Header.Get("X-Team") != "ops" || r.Header.Get("User-Agent") != "kjak" {
			t.Errorf("%s: %+v", m, r)
		}
		if r.Query.Get("source") != "kjak" || r.Query.Get("b") != "2" {
			t.Errorf("%s: query %v", m, r.Query)
		}
		switch m {
		case "GET", "DELETE":
			if r.Body != "" || r.Header.Get("Content-Type") != "" {
				t.Errorf("%s has a body: %q", m, r.Body)
			}
		default:
			var doc map[string]string
			if err := json.Unmarshal([]byte(r.Body), &doc); err != nil || doc["body"] != "api 1.2 is live" || doc["level"] != "success" || doc["title"] != "Deploy" || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("%s: body %q %v", m, r.Body, err)
			}
		}
	}
}

func TestAuthentication(t *testing.T) {
	rec := newRecorder(t, nil)
	tok := credential.NewSecret("tok-123")
	cases := []struct {
		auth   Auth
		header string
		want   string
	}{
		{Auth{Header: "Authorization", Value: credential.NewSecret("Bearer " + tok.Reveal())}, "Authorization", "Bearer tok-123"},
		{Auth{Header: "Authorization", Value: BasicAuth("kjak", credential.NewSecret("pw"))}, "Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte("kjak:pw"))},
		{Auth{Header: "X-Api-Key", Value: credential.NewSecret("key-1")}, "X-Api-Key", "key-1"},
	}
	for _, c := range cases {
		o := mustFeatherAuth(t, local(rec.URL), c.auth, Options{}).Deliver(context.Background(), msg())
		if o.Acceptance != core.AcceptanceConfirmed {
			t.Fatalf("%+v", o)
		}
	}
	for i, r := range rec.got() {
		if got := r.Header.Get(cases[i].header); got != cases[i].want {
			t.Errorf("%d: %s = %q, want %q", i, cases[i].header, got, cases[i].want)
		}
	}
	if BasicAuth("a", credential.NewSecret("b")).String() != credential.Redacted {
		t.Fatal("a basic header prints itself")
	}
}

func TestBodyTemplates(t *testing.T) {
	rec := newRecorder(t, nil)
	fc := local(rec.URL)
	fc.Body = &configure.HTTPBody{Template: `{"text": "{{.title}}: {{.body}}", "n": 12345678901234567890, "tags": ["{{.level}}", "{{.feather}}"], "nested": {"u": "{{.url}}"}}`}
	injection := core.Message{Title: `x", "admin": true, "y": "`, Body: "{{.feather}}   </script>", Level: core.LevelError}
	if o := mustFeather(t, fc, Options{}).Deliver(context.Background(), injection); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("%+v", o)
	}
	fc.Body = &configure.HTTPBody{Format: "text", Template: "[{{.level}}] {{.title}} {{.body}}", ContentType: "text/markdown"}
	if o := mustFeather(t, fc, Options{}).Deliver(context.Background(), injection); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("%+v", o)
	}
	fc.Body = &configure.HTTPBody{Format: "text"}
	if o := mustFeather(t, fc, Options{}).Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("%+v", o)
	}
	got := rec.got()
	var doc map[string]any
	if err := json.Unmarshal([]byte(got[0].Body), &doc); err != nil {
		t.Fatalf("%v: %s", err, got[0].Body)
	}
	// the message cannot add a key, and a template in it is not run
	if _, injected := doc["admin"]; injected || len(doc) != 4 {
		t.Fatalf("structure changed: %s", got[0].Body)
	}
	if doc["text"] != `x", "admin": true, "y": ": {{.feather}} `+" "+` </script>` {
		t.Fatalf("text = %q", doc["text"])
	}
	if !strings.Contains(got[0].Body, "12345678901234567890") || !strings.Contains(got[0].Body, `"tags":["error","api"]`) {
		t.Fatalf("numbers or arrays changed: %s", got[0].Body)
	}
	if got[1].Body != `[error] x", "admin": true, "y": " {{.feather}} `+" "+` </script>` || got[1].Header.Get("Content-Type") != "text/markdown" {
		t.Fatalf("text template: %q %s", got[1].Body, got[1].Header.Get("Content-Type"))
	}
	if got[2].Body != "[SUCCESS] Deploy\n\napi 1.2 is live\n\nhttps://ci.example/1" || !strings.HasPrefix(got[2].Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("text: %q", got[2].Body)
	}
	// a template that names what is not there fails without sending
	fc.Body = &configure.HTTPBody{Format: "text", Template: "{{.secret}}"}
	n := len(rec.got())
	if o := mustFeather(t, fc, Options{}).Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceRejected || o.Retryable || len(rec.got()) != n {
		t.Fatalf("missing key: %+v", o)
	}
}

func TestClassification(t *testing.T) {
	cases := []struct {
		status     int
		success    []string
		acceptance core.Acceptance
		code       core.ErrorCode
		retryable  bool
	}{
		{200, nil, core.AcceptanceConfirmed, "", false},
		{204, nil, core.AcceptanceConfirmed, "", false},
		{201, []string{"201"}, core.AcceptanceConfirmed, "", false},
		{200, []string{"201", "202"}, core.AcceptanceRejected, core.ErrCodeRejected, false},
		{404, nil, core.AcceptanceRejected, core.ErrCodeRejected, false},
		{404, []string{"2xx", "404"}, core.AcceptanceConfirmed, "", false},
		{401, nil, core.AcceptanceRejected, core.ErrCodeRejected, false},
		{429, nil, core.AcceptanceRejected, core.ErrCodeRateLimited, true},
		{408, nil, core.AcceptanceRejected, core.ErrCodeServer, true},
		{500, nil, core.AcceptanceRejected, core.ErrCodeServer, true},
		{503, nil, core.AcceptanceRejected, core.ErrCodeServer, true},
		{502, nil, core.AcceptanceUnknown, core.ErrCodeAmbiguous, false},
		{504, nil, core.AcceptanceUnknown, core.ErrCodeAmbiguous, false},
		{302, nil, core.AcceptanceRejected, core.ErrCodeProtocol, false},
	}
	for _, c := range cases {
		rec := newRecorder(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
			if c.status == 302 {
				w.Header().Set("Location", "/elsewhere")
			}
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte("detail\x1b[31m text"))
		})
		fc := local(rec.URL)
		fc.Method = "PUT" // idempotent: the classification's retryability stays
		fc.SuccessStatus = c.success
		o := mustFeather(t, fc, Options{}).Deliver(context.Background(), msg())
		if o.Acceptance != c.acceptance || o.Code != c.code || o.Retryable != c.retryable {
			t.Errorf("%d %v: %+v", c.status, c.success, o)
		}
		if c.retryable && o.RetryAfter != 3*time.Second {
			t.Errorf("%d: retry after %s", c.status, o.RetryAfter)
		}
		if o.Err != nil && strings.Contains(o.Err.Error(), "\x1b") {
			t.Errorf("%d: a control character in the error", c.status)
		}
		if len(rec.got()) != 1 {
			t.Errorf("%d: %d requests (a redirect was followed?)", c.status, len(rec.got()))
		}
	}
}

// POST and PATCH are not repeated unless the API can recognize the
// repetition (an idempotency key) or the policy says so; GET, PUT and
// DELETE are. A transient refusal stays a refusal: only Retryable changes.
func TestCritical_RetryPolicyByMethod(t *testing.T) {
	rec := newRecorder(t, status(503))
	cases := []struct {
		method string
		policy string
		key    bool
		want   bool
	}{
		{"GET", "", false, true},
		{"PUT", "", false, true},
		{"DELETE", "", false, true},
		{"POST", "", false, false},
		{"PATCH", "", false, false},
		{"POST", "", true, true},
		{"PATCH", "safe", true, true},
		{"POST", "always", false, true},
		{"PUT", "never", false, false},
		{"POST", "never", true, false},
	}
	for _, c := range cases {
		fc := local(rec.URL)
		fc.Method = c.method
		if c.method == "GET" {
			fc.Body = &configure.HTTPBody{Format: "none"}
		}
		fc.IdempotencyKey = c.key
		if c.policy != "" {
			fc.Retry = &configure.HTTPRetry{Policy: c.policy}
		}
		o := mustFeather(t, fc, Options{}).Deliver(context.Background(), msg())
		if o.Acceptance != core.AcceptanceRejected || o.Retryable != c.want {
			t.Errorf("%s policy %q key %t: retryable %t, want %t (%v)", c.method, c.policy, c.key, o.Retryable, c.want, o.Err)
		}
	}
}

func TestMaxAttemptsAndIdempotencyKey(t *testing.T) {
	rec := newRecorder(t, status(500))
	fc := local(rec.URL)
	fc.IdempotencyKey = true
	fc.IdempotencyHeader = "X-Request-Id"
	fc.Retry = &configure.HTTPRetry{MaxAttempts: 2}
	f := mustFeather(t, fc, Options{})
	if o := f.Deliver(context.Background(), msg()); !o.Retryable {
		t.Fatalf("first: %+v", o)
	}
	o := f.Deliver(context.Background(), msg())
	if o.Retryable || !strings.Contains(o.Err.Error(), "max_attempts 2") {
		t.Fatalf("second: %+v", o)
	}
	other := mustFeather(t, fc, Options{})
	other.Deliver(context.Background(), msg())
	got := rec.got()
	k1, k2, k3 := got[0].Header.Get("X-Request-Id"), got[1].Header.Get("X-Request-Id"), got[2].Header.Get("X-Request-Id")
	if k1 == "" || k1 != k2 || k1 == k3 || len(k1) != 32 {
		t.Fatalf("keys %q %q %q", k1, k2, k3)
	}
}

// A request that reached the API and got no answer is unknown, whatever
// ended the wait: the attempt timeout, the send's deadline or a SIGINT.
func TestTimeoutAndCancellation(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	arrived := make(chan int, 4)
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request, n int) {
		arrived <- n
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	fc := local(rec.URL)
	fc.Timeout = &configure.Duration{Duration: 100 * time.Millisecond}
	o := mustFeather(t, fc, Options{}).Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceUnknown || o.Retryable || !strings.Contains(o.Err.Error(), "attempt timeout") {
		t.Fatalf("attempt timeout: %+v", o)
	}
	<-arrived
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-arrived // the whole request was read: cancel while waiting for the answer
		cancel()
	}()
	// the attempt timeout is long: the cancellation, not the timeout,
	// must end the wait
	fc.Timeout = &configure.Duration{Duration: time.Minute}
	start := time.Now()
	o = mustFeather(t, fc, Options{}).Deliver(ctx, msg())
	if o.Acceptance != core.AcceptanceUnknown || o.Retryable {
		t.Fatalf("canceled after sending: %+v", o)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("the cancellation ended the request after %s", d)
	}
	// before anything was sent: a connection that never comes
	blockDial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	fc.Timeout = &configure.Duration{Duration: 50 * time.Millisecond}
	fc.Method = "PUT"
	o = mustFeather(t, fc, Options{dial: blockDial}).Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceRejected || !o.Retryable || o.Code != core.ErrCodeNetwork {
		t.Fatalf("timeout before sending: %+v", o)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	o = mustFeather(t, fc, Options{dial: blockDial}).Deliver(ctx, msg())
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || o.Code != core.ErrCodeCanceled {
		t.Fatalf("canceled before sending: %+v", o)
	}
}

// A downgrade from https to http on the same host and port (allow_http) is
// another origin: the credentials and the Referer are not sent on.
func TestCritical_RedirectDowngradeDropsCredentials(t *testing.T) {
	auth := Auth{Header: "X-Api-Key", Value: credential.NewSecret("key-SECRET")}
	fc := configure.FeatherConfig{MaxRedirects: 3, AllowHTTP: true}
	c := newClient(&Guard{}, nil, fc, auth)
	first, _ := url.Parse("https://api.example.com:8443/v1")
	next, _ := url.Parse("http://api.example.com:8443/v1")
	req := &http.Request{URL: next, Header: http.Header{
		"Authorization": {"Bearer b-SECRET"}, "Cookie": {"c-SECRET"}, "X-Api-Key": {"key-SECRET"}, "Referer": {first.String()},
	}}
	if err := c.CheckRedirect(req, []*http.Request{{URL: first}}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"Authorization", "Cookie", "X-Api-Key", "Referer"} {
		if v := req.Header.Get(h); v != "" {
			t.Errorf("%s sent on after a downgrade: %q", h, v)
		}
	}
}

// Redirects: not followed by default; followed only as configured, under
// the same address policy, and the credentials stay with their origin.
func TestCritical_RedirectsKeepCredentialsAtTheirOrigin(t *testing.T) {
	other := newRecorder(t, nil)
	origin := newRecorder(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		if r.URL.Path == "/same" {
			http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path == "/final" {
			w.WriteHeader(200)
			return
		}
		http.Redirect(w, r, other.URL+"/landing", http.StatusTemporaryRedirect)
	})
	auth := Auth{Header: "X-Api-Key", Value: credential.NewSecret("key-SECRET")}
	fc := local(origin.URL + "/start")
	fc.Headers = map[string]string{"X-Static": "kept"}
	// not followed by default
	o := mustFeatherAuth(t, fc, auth, Options{}).Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceRejected || o.Code != core.ErrCodeProtocol || len(other.got()) != 0 {
		t.Fatalf("default: %+v", o)
	}
	fc.MaxRedirects = 1
	o = mustFeatherAuth(t, fc, auth, Options{}).Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("followed: %+v", o)
	}
	landed := other.got()
	if len(landed) != 1 || landed[0].Header.Get("X-Api-Key") != "" || landed[0].Header.Get("Authorization") != "" {
		t.Fatalf("credentials went to another origin: %+v", landed)
	}
	if landed[0].Header.Get("Referer") != "" {
		t.Fatalf("the URL went to another origin as Referer: %q", landed[0].Header.Get("Referer"))
	}
	if landed[0].Header.Get("X-Static") != "kept" {
		t.Fatal("the static header was dropped")
	}
	bearer := Auth{Header: "Authorization", Value: credential.NewSecret("Bearer b-SECRET")}
	o = mustFeatherAuth(t, fc, bearer, Options{}).Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceConfirmed || other.got()[1].Header.Get("Authorization") != "" {
		t.Fatalf("bearer to another origin: %+v", other.got())
	}
	// the same origin keeps them
	fc.URL = origin.URL + "/same"
	if o := mustFeatherAuth(t, fc, auth, Options{}).Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("same origin: %+v", o)
	}
	reqs := origin.got()
	if last := reqs[len(reqs)-1]; last.Path != "/final" || last.Header.Get("X-Api-Key") != "key-SECRET" {
		t.Fatalf("same origin lost the key: %+v", last)
	}
	// a redirect to a blocked address is refused like a URL
	blocked := newRecorder(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	})
	fc = local(blocked.URL)
	fc.MaxRedirects = 3
	o = mustFeather(t, fc, Options{}).Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceRejected || o.Code != core.ErrCodeBlocked || o.Retryable {
		t.Fatalf("redirect to metadata: %+v", o)
	}
	// and an https feather does not follow a redirect to http
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	t.Cleanup(tlsSrv.Close)
	fc = testConfig(tlsSrv.URL)
	fc.AllowNetworks = []string{"127.0.0.1/32"}
	fc.MaxRedirects = 3
	n := len(other.got())
	o = mustFeather(t, fc, Options{TLS: testTLS(tlsSrv)}).Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceRejected || o.Code != core.ErrCodeBlocked || len(other.got()) != n {
		t.Fatalf("https -> http: %+v", o)
	}
}

func testTLS(srv *httptest.Server) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{RootCAs: pool}
}

func TestHTTPSAndNoProxy(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	t.Cleanup(srv.Close)
	fc := testConfig(srv.URL)
	fc.AllowNetworks = []string{"127.0.0.1/32"}
	f := mustFeather(t, fc, Options{TLS: testTLS(srv)})
	if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("%+v", o)
	}
	tr := f.Client.Transport.(*http.Transport)
	if tr.Proxy != nil || !tr.DisableKeepAlives || tr.TLSClientConfig.MinVersion < 0x0303 {
		t.Fatal("the transport uses a proxy, keeps connections or allows old TLS")
	}
	// an unknown certificate is a permanent refusal
	f = mustFeather(t, fc, Options{})
	if o := f.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceRejected || o.Retryable {
		t.Fatalf("unknown certificate: %+v", o)
	}
	// http needs allow_http
	if _, err := New("x", testConfig("http://example.com/"), "http://example.com/", Auth{}, Options{}); err == nil {
		t.Fatal("http without allow_http")
	}
	for _, bad := range []string{"https://user:pw@example.com/", "ftp://example.com/", "https:///x", "https://example.com/#f", "unix:///var/run/x.sock"} {
		if _, err := New("x", testConfig("https://example.com/"), bad, Auth{}, Options{}); err == nil || strings.Contains(err.Error(), "pw") {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestResponseLimit(t *testing.T) {
	big := strings.Repeat("x", 2<<20)
	rec := newRecorder(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, big)
	})
	fc := local(rec.URL)
	fc.MaxResponseBytes = 1024
	o := mustFeather(t, fc, Options{}).Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceRejected || !strings.Contains(o.Err.Error(), "max_response_bytes") || len(o.Err.Error()) > 1024 {
		t.Fatalf("%v", o.Err)
	}
}

func TestConcurrentDeliveries(t *testing.T) {
	rec := newRecorder(t, nil)
	f := mustFeather(t, local(rec.URL), Options{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g := mustFeather(t, local(rec.URL), Options{})
			for _, x := range []*Feather{f, g} {
				if o := x.Deliver(context.Background(), msg()); o.Acceptance != core.AcceptanceConfirmed {
					t.Errorf("%+v", o)
				}
			}
		}()
	}
	wg.Wait()
	if n := len(rec.got()); n != 32 {
		t.Fatalf("%d requests", n)
	}
}
