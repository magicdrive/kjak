package builtin

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather/email"
	"github.com/magicdrive/kjak/internal/feather/httpapi"
	"github.com/magicdrive/kjak/internal/feather/webhook"
)

// The proxy environment variables, decided by name and observed on real
// connections (docs/security.md "http feathers and SSRF",
// docs/configuration.md "Address policy of a project's slack, discord and
// email feathers"):
//
//   - a global slack or discord feather uses HTTPS_PROXY (the control: it
//     shows the variables were read);
//   - http feathers (global or project) never use a proxy;
//   - a project's slack, discord and email feathers never use a proxy;
//   - email speaks SMTP directly.
//
// Every destination is a name under example.test, which no resolver of the
// machine knows. net/http decides whether to use a proxy by the name of
// the destination, never for "localhost" or a loopback address: with a
// name, the decision is the real one, while every connection still lands
// on 127.0.0.1. The names reach 127.0.0.1 at the resolver and the dialer
// stages only:
//
//   - through the proxy, the proxy resolves the name of the CONNECT;
//   - directly, the address policy of the feather resolves it with the
//     test resolver (Deps.HTTPAPI.Resolver), or, for a global webhook or
//     email feather (no address policy), the test dialer does.
//
// The test resolver, the test dialer, the proxy and the destinations record
// what they saw, so each case shows which way its connection went. No route
// other than loopback is needed (TestProxyEnvironment of internal/e2e does
// the same with the binary).
//
// net/http reads the proxy variables once per process and crypto/x509 the
// roots once: the test runs itself again in a new process (proxyChildEnv)
// with the variables set before any connection.
func TestProxyDecisionByConnection(t *testing.T) {
	if os.Getenv(proxyChildEnv) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestProxyDecisionByConnection$", "-test.v", "-test.count=1")
		var env []string
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			switch strings.ToUpper(k) {
			case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "REQUEST_METHOD", "SSL_CERT_FILE", "SSL_CERT_DIR":
				continue
			}
			env = append(env, kv)
		}
		cmd.Env = append(env, proxyChildEnv+"=1")
		out, err := cmd.CombinedOutput()
		t.Logf("the test in a new process:\n%s", out)
		if err != nil {
			t.Fatalf("the test in a new process failed: %v", err)
		}
		if !strings.Contains(string(out), "--- PASS: TestProxyDecisionByConnection") {
			t.Fatal("the test did not run in the new process")
		}
		return
	}
	proxyDecisionChild(t)
}

const proxyChildEnv = "KJAK_TEST_PROXY_CHILD"

// The names of the destinations; all of them are 127.0.0.1 for the test.
const (
	hookHost  = "hooks.example.test"
	plainHost = "plain.example.test"
	mailHost  = "mail.example.test"
)

func proxyDecisionChild(t *testing.T) {
	loop := netip.MustParseAddr("127.0.0.1")
	names := map[string]netip.Addr{hookHost: loop, plainHost: loop, mailHost: loop}
	ca := newNameCA(t, hookHost)
	tlsDest := newNamedTLSDest(t, ca)
	plainDest := newNamedPlainDest(t)
	smtp := newRecordingSMTP(t)
	proxy := newNameProxy(t, names)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, ca.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	proxyURL := "http://" + proxy.addr()
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(k, proxyURL)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("SSL_CERT_FILE", caFile)
	// nothing read the variables before: the decision below is the one
	// net/http makes for these names
	probe, _ := http.NewRequest(http.MethodPost, "https://"+hookHost+"/", nil)
	if u, err := http.ProxyFromEnvironment(probe); err != nil || u == nil || u.Host != proxy.addr() {
		t.Fatalf("net/http does not see the proxy of this process for %s: %v %v", hookHost, u, err)
	}

	resolver := &recordingResolver{names: names}
	dialer := &recordingDialer{names: names}
	tlsPort := tlsDest.Listener.Addr().(*net.TCPAddr).Port
	plainPort := plainDest.Listener.Addr().(*net.TCPAddr).Port
	hookURL := func(path string) string { return fmt.Sprintf("https://%s:%d%s", hookHost, tlsPort, path) }
	allow := []string{"127.0.0.1/32"}
	env := map[string]string{}
	config := func(prefix, kind string) configure.FeatherConfig {
		switch kind {
		case "slack", "discord":
			key := strings.ToUpper("HOOK_" + prefix + "_" + kind)
			env[key] = hookURL("/" + kind + "/" + prefix)
			return configure.FeatherConfig{Type: kind, WebhookURL: &credential.Ref{Env: strp(key)}, AllowNetworks: allow}
		case "mail":
			return configure.FeatherConfig{Type: "email", Host: mailHost, Port: smtp.port(), Security: "none",
				From: "kjak@example.com", To: []string{"ops@example.com"}, AllowNetworks: allow}
		case "http":
			return configure.FeatherConfig{Type: "http", URL: hookURL("/http/" + prefix), AllowNetworks: allow}
		}
		return configure.FeatherConfig{Type: "http", URL: fmt.Sprintf("http://%s:%d/plain/%s", plainHost, plainPort, prefix),
			AllowHTTP: true, AllowNetworks: allow}
	}

	type want struct {
		proxied bool   // the proxy tunnels it (CONNECT <name>:<port>)
		dest    string // "tls", "plain" or "smtp": where it lands
	}
	cases := []struct {
		prefix, kind string
		want
	}{
		// the control: a global webhook feather uses the proxy
		{"global", "slack", want{proxied: true, dest: "tls"}},
		{"global", "discord", want{proxied: true, dest: "tls"}},
		{"global", "mail", want{dest: "smtp"}},
		{"global", "http", want{dest: "tls"}},
		{"global", "plain", want{dest: "plain"}},
		{"project", "slack", want{dest: "tls"}},
		{"project", "discord", want{dest: "tls"}},
		{"project", "mail", want{dest: "smtp"}},
		{"project", "http", want{dest: "tls"}},
		{"project", "plain", want{dest: "plain"}},
	}
	for _, c := range cases {
		name := c.prefix + "_" + c.kind
		t.Run(name, func(t *testing.T) {
			fc := config(c.prefix, c.kind)
			d := deps(env)
			// as kjak send does (internal/commandline/subcmd/send.go): one
			// webhook client for the send, the trust for a project's feather
			d.HTTPClient = webhook.NewClient()
			d.HTTPAPI = httpapi.Options{Resolver: resolver}
			if c.prefix == "project" {
				d.ProjectTrust = &configure.TrustEntry{}
			}
			f, err := Build(context.Background(), name, fc, d)
			if err != nil {
				t.Fatal(err)
			}
			// the test dialer stands in for the system's only where kjak
			// itself would dial a name with no address policy: the
			// transport of the global webhook client (its proxy decision
			// is kept) and a global email feather
			if c.prefix == "global" {
				tr := d.HTTPClient.Transport.(*http.Transport)
				tr.DialContext = dialer.dial
				if ef, ok := f.(*email.Feather); ok {
					ef.Dial = dialer.dial
				}
			}
			proxy.reset()
			resolver.reset()
			dialer.reset()
			hellos, requests, plain, mails := len(tlsDest.hellos()), len(tlsDest.requests()), len(plainDest.paths()), len(smtp.all())

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			o := f.Deliver(ctx, core.Message{Title: "T", Body: "proxy check " + name, Level: core.LevelInfo})
			if o.Acceptance != core.AcceptanceConfirmed {
				t.Fatalf("not delivered: %+v", o)
			}

			host := map[string]string{"tls": hookHost, "plain": plainHost, "smtp": mailHost}[c.dest]
			port := map[string]int{"tls": tlsPort, "plain": plainPort, "smtp": smtp.port()}[c.dest]
			target := net.JoinHostPort(host, fmt.Sprint(port))
			conns := proxy.connections()
			if c.proxied {
				if !slices.Equal(conns, []string{"CONNECT " + target}) {
					t.Fatalf("the proxy saw %q, want one CONNECT %s", conns, target)
				}
				// kjak connected to the proxy only, and resolved nothing
				if got := dialer.dialed(); !slices.Equal(got, []string{proxy.addr()}) {
					t.Errorf("kjak dialed %q, want the proxy %s only", got, proxy.addr())
				}
				if got := resolver.lookups(); len(got) != 0 {
					t.Errorf("kjak resolved %q itself", got)
				}
			} else {
				if len(conns) != 0 {
					t.Fatalf("the proxy saw %q: a %s feather must connect directly", conns, name)
				}
				// the destination's name was resolved or dialed by kjak
				// itself: through the address policy (the test resolver),
				// or, with none, by the test dialer
				looked, dialed := resolver.lookups(), dialer.dialed()
				switch {
				case slices.Equal(looked, []string{host}) && len(dialed) == 0:
				case len(looked) == 0 && slices.Equal(dialed, []string{target}):
				default:
					t.Errorf("resolved %q, dialed %q: want %s resolved by the address policy, or dialed directly", looked, dialed, target)
				}
			}
			switch c.dest {
			case "tls":
				got := tlsDest.hellos()[hellos:]
				if len(got) == 0 {
					t.Fatal("the destination saw no TLS handshake")
				}
				for _, h := range got {
					if h.serverName != hookHost {
						t.Errorf("SNI %q, want %s", h.serverName, hookHost)
					}
					// through the tunnel the peer is the proxy's outgoing
					// connection, else kjak itself
					if through := proxy.dialedFrom(h.peer); through != c.proxied {
						t.Errorf("handshake from %s: through the proxy %v, want %v", h.peer, through, c.proxied)
					}
				}
				if r := tlsDest.requests()[requests:]; len(r) != 1 || !strings.Contains(r[0], "proxy check "+name) {
					t.Fatalf("the destination received %q", r)
				}
			case "plain":
				if p := plainDest.paths()[plain:]; !slices.Equal(p, []string{"/plain/" + c.prefix}) {
					t.Fatalf("the plain destination received %q", p)
				}
			case "smtp":
				if m := smtp.all()[mails:]; len(m) != 1 || !strings.Contains(m[0], "proxy check "+name) {
					t.Fatalf("the SMTP server received %q", m)
				}
			}
		})
	}
}

// recordingResolver resolves the names of the test, and records them.
type recordingResolver struct {
	names map[string]netip.Addr
	mu    sync.Mutex
	seen  []string
}

func (r *recordingResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	r.seen = append(r.seen, host)
	r.mu.Unlock()
	if a, ok := r.names[host]; ok {
		return []netip.Addr{a}, nil
	}
	return nil, &net.DNSError{Err: "not a name of the test", Name: host, IsNotFound: true}
}

func (r *recordingResolver) reset() { r.mu.Lock(); r.seen = nil; r.mu.Unlock() }

func (r *recordingResolver) lookups() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

// recordingDialer records the address it is asked for (a name, or the
// proxy's) and connects to it, the names of the test at 127.0.0.1.
type recordingDialer struct {
	names map[string]netip.Addr
	mu    sync.Mutex
	seen  []string
}

func (d *recordingDialer) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	d.mu.Lock()
	d.seen = append(d.seen, addr)
	d.mu.Unlock()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if a, ok := d.names[host]; ok {
		addr = net.JoinHostPort(a.String(), port)
	} else if ip, err := netip.ParseAddr(host); err != nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("the test dialer connects to loopback only, not %s", addr)
	}
	var nd net.Dialer
	return nd.DialContext(ctx, network, addr)
}

func (d *recordingDialer) reset() { d.mu.Lock(); d.seen = nil; d.mu.Unlock() }

func (d *recordingDialer) dialed() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.seen...)
}

type nameCA struct {
	pem  []byte
	leaf tls.Certificate
}

// newNameCA is a CA and a certificate it issued for the name host.
func newNameCA(t *testing.T, host string) nameCA {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kjak proxy test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: host},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		DNSNames: []string{host}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return nameCA{
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		leaf: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
	}
}

type clientHello struct{ peer, serverName string }

// namedTLSDest is an HTTPS server on 127.0.0.1 that records every
// ClientHello (peer, SNI) and every request.
type namedTLSDest struct {
	*httptest.Server
	mu   sync.Mutex
	hs   []clientHello
	reqs []string
}

func newNamedTLSDest(t *testing.T, ca nameCA) *namedTLSDest {
	d := &namedTLSDest{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		d.mu.Lock()
		d.reqs = append(d.reqs, r.URL.Path+" "+string(b))
		d.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/discord/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{ca.leaf},
		MinVersion:   tls.VersionTLS12,
		GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
			d.mu.Lock()
			d.hs = append(d.hs, clientHello{peer: chi.Conn.RemoteAddr().String(), serverName: chi.ServerName})
			d.mu.Unlock()
			return nil, nil
		},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	d.Server = srv
	return d
}

func (d *namedTLSDest) hellos() []clientHello {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]clientHello(nil), d.hs...)
}

func (d *namedTLSDest) requests() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.reqs...)
}

// namedPlainDest is an HTTP server on 127.0.0.1 that records the paths it
// is sent to.
type namedPlainDest struct {
	*httptest.Server
	mu sync.Mutex
	ps []string
}

func newNamedPlainDest(t *testing.T) *namedPlainDest {
	d := &namedPlainDest{}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		d.mu.Lock()
		d.ps = append(d.ps, r.URL.Path)
		d.mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(d.Close)
	return d
}

func (d *namedPlainDest) paths() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.ps...)
}

// recordingSMTP is an SMTP server on 127.0.0.1 that keeps every message.
type recordingSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	msgs []string
}

func newRecordingSMTP(t *testing.T) *recordingSMTP {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &recordingSMTP{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *recordingSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *recordingSMTP) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	say := func(line string) { _, _ = io.WriteString(c, line+"\r\n") }
	say("220 mail.example.test ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250-mail.example.test")
			say("250 8BITMIME")
		case cmd == "DATA":
			say("354 go ahead")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			s.mu.Lock()
			s.msgs = append(s.msgs, data.String())
			s.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (s *recordingSMTP) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs...)
}

// nameProxy is an HTTP proxy on 127.0.0.1: it records the request line of
// every connection, tunnels a CONNECT to a name of the test (resolved by
// names, never by the system), and answers anything else with 502.
type nameProxy struct {
	ln    net.Listener
	names map[string]netip.Addr
	mu    sync.Mutex
	lines []string
	local map[string]bool // the local addresses of the tunnels it dialed
}

func newNameProxy(t *testing.T, names map[string]netip.Addr) *nameProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &nameProxy{ln: ln, names: names, local: map[string]bool{}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.serve(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return p
}

func (p *nameProxy) addr() string { return p.ln.Addr().String() }

func (p *nameProxy) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		p.record("unreadable request: " + err.Error())
		return
	}
	if req.Method != http.MethodConnect {
		p.record(req.Method + " " + req.URL.String())
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return
	}
	host, port, _ := net.SplitHostPort(req.Host)
	a, ok := p.names[host]
	if !ok {
		p.record("CONNECT " + req.Host + " (not a name of the test)")
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(a.String(), port), 10*time.Second)
	if err != nil {
		p.record("CONNECT " + req.Host + " (dial: " + err.Error() + ")")
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer func() { _ = upstream.Close() }()
	p.mu.Lock()
	p.lines = append(p.lines, "CONNECT "+req.Host)
	p.local[upstream.LocalAddr().String()] = true
	p.mu.Unlock()
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, br); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, upstream); done <- struct{}{} }()
	<-done
}

func (p *nameProxy) record(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lines = append(p.lines, line)
}

func (p *nameProxy) reset() { p.mu.Lock(); p.lines = nil; p.mu.Unlock() }

func (p *nameProxy) connections() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.lines...)
}

// dialedFrom reports whether peer is the local address of a tunnel the
// proxy dialed.
func (p *nameProxy) dialedFrom(peer string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.local[peer]
}
