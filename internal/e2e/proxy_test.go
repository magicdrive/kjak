package e2e

import (
	"bufio"
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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The proxy environment variables, with the kjak binary and real
// connections (docs/security.md "http feathers and SSRF",
// docs/configuration.md "Address policy of a project's slack, discord and
// email feathers"):
//
//   - a global slack or discord feather uses HTTPS_PROXY / HTTP_PROXY;
//   - http feathers (global or project) never use a proxy;
//   - a project's slack, discord and email feathers never use a proxy;
//   - email speaks SMTP directly: no HTTP proxy applies to it.
//
// The binary runs with HTTP_PROXY, HTTPS_PROXY (and the lower-case names,
// and ALL_PROXY) pointing at a recording CONNECT proxy on 127.0.0.1. Every
// destination is a name under example.test (RFC 6761: no resolver knows
// it). net/http decides whether to use a proxy by the destination's name,
// never for "localhost" or a loopback address: a test against 127.0.0.1
// would pass whatever kjak did, a test against a name sees the real
// decision. Only the proxy knows the names (they are 127.0.0.1 to it), so:
//
//   - a feather that uses the proxy reaches its destination through the
//     tunnel and delivers: the proxy records CONNECT <name>:<port>, the
//     destination the handshake from the tunnel (the control: the binary
//     read the variables);
//   - a feather that connects directly asks the system's resolver for the
//     name and fails before anything is sent: the proxy records nothing,
//     the destinations receive nothing.
//
// Nothing needs a route other than loopback. That the direct feathers do
// connect, by name, to their destination is shown in-process with a test
// resolver and dialer (internal/feather/builtin TestProxyDecisionByConnection).
//
// TLS: the destination's certificate is for its name, issued by a CA of the
// test given to kjak by SSL_CERT_FILE (read on every platform since Go
// 1.27, which go.mod pins). Without SSL_CERT_FILE the same send through the
// proxy must fail: kjak verifies the certificate.
func TestProxyEnvironment(t *testing.T) {
	ca := newTestCA(t, destHost)
	proxy := newRecordingProxy(t, map[string]string{destHost: "127.0.0.1", plainDestHost: "127.0.0.1", mailDestHost: "127.0.0.1"})
	tlsDest := newTLSDestination(t, ca)
	plainDest := newPlainDestination(t)
	smtp := newSMTPOn(t, "127.0.0.1:0")
	tlsPort := tlsDest.Listener.Addr().(*net.TCPAddr).Port
	plainPort := plainDest.Listener.Addr().(*net.TCPAddr).Port
	tlsURL := fmt.Sprintf("https://%s:%d", destHost, tlsPort)

	s := newSandbox(t)
	xdg := filepath.Join(filepath.Dir(s.dir), "xdg", "kjak")
	if err := os.MkdirAll(xdg, 0o700); err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(filepath.Dir(s.dir), "ca.pem")
	if err := os.WriteFile(caFile, ca.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	proxyURL := "http://" + proxy.addr()
	s.env = append(s.env,
		"HTTPS_PROXY="+proxyURL, "https_proxy="+proxyURL,
		"HTTP_PROXY="+proxyURL, "http_proxy="+proxyURL,
		"ALL_PROXY="+proxyURL, "all_proxy="+proxyURL,
		"NO_PROXY=", "no_proxy=",
		"SSL_CERT_FILE="+caFile,
	)
	allow := "allow_networks = [\"127.0.0.1/32\"]\n"
	feathers := func(prefix string) string {
		var b strings.Builder
		fmt.Fprintf(&b, "[feathers.%[1]s_slack]\ntype = \"slack\"\nwebhook_url = { env = \"HOOK_%[1]s_SLACK\" }\n%[2]s\n", prefix, allow)
		fmt.Fprintf(&b, "[feathers.%[1]s_discord]\ntype = \"discord\"\nwebhook_url = { env = \"HOOK_%[1]s_DISCORD\" }\n%[2]s\n", prefix, allow)
		fmt.Fprintf(&b, "[feathers.%s_mail]\ntype = \"email\"\nhost = %q\nport = %d\nsecurity = \"none\"\nfrom = \"kjak@example.com\"\nto = [\"ops@example.com\"]\n%s\n", prefix, mailDestHost, smtp.port(), allow)
		fmt.Fprintf(&b, "[feathers.%s_http]\ntype = \"http\"\nurl = %q\n%s\n", prefix, tlsURL+"/http/"+prefix, allow)
		fmt.Fprintf(&b, "[feathers.%s_plain]\ntype = \"http\"\nurl = %q\nallow_http = true\n%s\n", prefix, fmt.Sprintf("http://%s:%d/plain/%s", plainDestHost, plainPort, prefix), allow)
		return b.String()
	}
	for _, prefix := range []string{"global", "project"} {
		s.env = append(s.env,
			"HOOK_"+prefix+"_SLACK="+tlsURL+"/slack/"+prefix,
			"HOOK_"+prefix+"_DISCORD="+tlsURL+"/discord/"+prefix)
	}
	if err := os.WriteFile(filepath.Join(xdg, "config.toml"), []byte(feathers("global")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, ".kjak.toml"), []byte(feathers("project")), 0o644); err != nil {
		t.Fatal(err)
	}
	s.must("config", "trust", "--yes", "--allow-user-secrets")

	cases := []struct {
		feather string
		proxied bool // through the proxy (and delivered), else directly (by name: not found)
	}{
		// the control: a global webhook feather uses the proxy
		{"global_slack", true},
		{"global_discord", true},
		{"global_mail", false},
		{"global_http", false},
		{"global_plain", false},
		{"project_slack", false},
		{"project_discord", false},
		{"project_mail", false},
		{"project_http", false},
		{"project_plain", false},
	}
	for _, c := range cases {
		t.Run(c.feather, func(t *testing.T) {
			proxy.reset()
			hellos, requests, plain, mails := len(tlsDest.hellos()), len(tlsDest.requests()), len(plainDest.paths()), len(smtp.all())
			r := s.kjak("", "send", "--to", c.feather, "--timeout", "20s", "-o", "json", "proxy check")
			rep := parseReport(t, r.stdout)
			res := rep.result(t, c.feather)
			conns := proxy.connections()
			if !c.proxied {
				if len(conns) != 0 {
					t.Fatalf("the proxy saw %q: a %s feather must connect directly\n%s%s", conns, c.feather, r.stdout, r.stderr)
				}
				// directly, the name is the system's to resolve: nothing
				// is sent anywhere
				if res.Status == "delivered" || r.code == 0 {
					t.Fatalf("delivered to a name no resolver knows: status %s, exit %d\n%s%s", res.Status, r.code, r.stdout, r.stderr)
				}
				if len(tlsDest.hellos()) != hellos || len(tlsDest.requests()) != requests || len(plainDest.paths()) != plain || len(smtp.all()) != mails {
					t.Fatalf("a destination was reached without the proxy\n%s%s", r.stdout, r.stderr)
				}
				return
			}
			target := fmt.Sprintf("%s:%d", destHost, tlsPort)
			if len(conns) == 0 {
				t.Fatalf("the proxy saw no connection: kjak ignored HTTPS_PROXY for a global %s feather\n%s%s", res.Type, r.stdout, r.stderr)
			}
			for _, line := range conns {
				if line != "CONNECT "+target {
					t.Errorf("the proxy saw %q, want CONNECT %s", line, target)
				}
			}
			got := tlsDest.hellos()[hellos:]
			if len(got) == 0 {
				t.Fatalf("the destination saw no TLS handshake\n%s%s", r.stdout, r.stderr)
			}
			for _, h := range got {
				// through the CONNECT tunnel the peer is the proxy's
				// outgoing connection
				if !proxy.dialedFrom(h.peer) {
					t.Errorf("handshake from %s, not from the proxy's tunnel", h.peer)
				}
			}
			if res.Status != "delivered" || r.code != 0 {
				t.Fatalf("status %s, exit %d\n%s%s", res.Status, r.code, r.stdout, r.stderr)
			}
			if got := tlsDest.requests()[requests:]; len(got) != 1 || !strings.Contains(got[0], "proxy check") {
				t.Fatalf("the destination received %q", got)
			}
		})
	}

	// the control of the TLS check: without the test CA, the certificate
	// is refused through the tunnel, and nothing is received
	var env []string
	for _, kv := range s.env {
		if !strings.HasPrefix(kv, "SSL_CERT_FILE=") {
			env = append(env, kv)
		}
	}
	s.env = env
	for _, name := range []string{"global_slack", "global_discord"} {
		proxy.reset()
		requests := len(tlsDest.requests())
		r := s.kjak("", "send", "--to", name, "--timeout", "20s", "-o", "json", "untrusted")
		res := parseReport(t, r.stdout).result(t, name)
		if r.code == 0 || res.Status == "delivered" || len(tlsDest.requests()) != requests || len(proxy.connections()) == 0 {
			t.Errorf("%s without the test CA: exit %d, status %s, received %q, proxy %q\n%s", name, r.code, res.Status, tlsDest.requests()[requests:], proxy.connections(), r.stdout)
		}
	}
}

// The names of the destinations: only the proxy of the test knows them.
const (
	destHost      = "hooks.example.test"
	plainDestHost = "plain.example.test"
	mailDestHost  = "mail.example.test"
)

type testCA struct {
	pem  []byte
	leaf tls.Certificate
}

// newTestCA is a CA and a certificate it issued for the name host.
func newTestCA(t *testing.T, host string) testCA {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kjak e2e proxy test CA"},
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
	return testCA{
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		leaf: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
	}
}

type hello struct{ peer string }

// tlsDestination is an HTTPS server that records every ClientHello (with
// the peer address) and every request it reads.
type tlsDestination struct {
	*httptest.Server
	mu   sync.Mutex
	hs   []hello
	reqs []string
}

func newTLSDestination(t *testing.T, ca testCA) *tlsDestination {
	d := &tlsDestination{}
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
			d.hs = append(d.hs, hello{peer: chi.Conn.RemoteAddr().String()})
			d.mu.Unlock()
			return nil, nil
		},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	d.Server = srv
	return d
}

func (d *tlsDestination) hellos() []hello {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]hello(nil), d.hs...)
}

func (d *tlsDestination) requests() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.reqs...)
}

// plainDestination is an HTTP server (an http feather with allow_http) that
// records the paths it is sent to.
type plainDestination struct {
	*httptest.Server
	mu sync.Mutex
	ps []string
}

func newPlainDestination(t *testing.T) *plainDestination {
	d := &plainDestination{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		d.mu.Lock()
		d.ps = append(d.ps, r.URL.Path)
		d.mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	}))
	srv.Start()
	t.Cleanup(srv.Close)
	d.Server = srv
	return d
}

func (d *plainDestination) paths() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.ps...)
}

// recordingProxy is an HTTP proxy: it records the request line of every
// connection it accepts, tunnels a CONNECT to a name it knows (names: the
// address of each, never asked of the system), and answers anything else
// (an absolute-form request through HTTP_PROXY, an unknown name) with 502.
type recordingProxy struct {
	ln    net.Listener
	names map[string]string
	mu    sync.Mutex
	lines []string
	local map[string]bool // the local addresses of the tunnels it dialed
}

func newRecordingProxy(t *testing.T, names map[string]string) *recordingProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &recordingProxy{ln: ln, names: names, local: map[string]bool{}}
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

func (p *recordingProxy) addr() string { return p.ln.Addr().String() }

func (p *recordingProxy) serve(c net.Conn) {
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
	ip, ok := p.names[host]
	if !ok {
		p.record("CONNECT " + req.Host + " (not a name of the test)")
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), 10*time.Second)
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

func (p *recordingProxy) record(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lines = append(p.lines, line)
}

func (p *recordingProxy) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lines = nil
}

func (p *recordingProxy) connections() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.lines...)
}

// dialedFrom reports whether peer is the local address of a tunnel the
// proxy dialed.
func (p *recordingProxy) dialedFrom(peer string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.local[peer]
}
