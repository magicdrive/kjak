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
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The proxy environment variables, with real connections (docs/security.md
// "http feathers and SSRF", docs/configuration.md "Address policy of a
// project's slack, discord and email feathers"):
//
//   - http feathers (global or project) never use a proxy;
//   - a project's slack, discord and email feathers never use a proxy (the
//     address policy checks the destination, which a proxy would hide);
//   - a global slack or discord feather uses HTTPS_PROXY / HTTP_PROXY;
//   - email speaks SMTP directly: no HTTP proxy applies to it.
//
// The kjak binary runs with HTTP_PROXY, HTTPS_PROXY (and the lower-case
// names, and ALL_PROXY) pointing at a recording CONNECT proxy. The
// destinations listen on a non-loopback address of this machine: Go never
// uses a proxy for a loopback destination (net/http.ProxyFromEnvironment),
// so a test against 127.0.0.1 would pass whatever kjak did. The global
// slack and discord feathers are the control: the proxy must see their
// CONNECT, which shows the binary did read the variables.
//
// TLS: the destinations' certificate is issued by a CA of the test, given
// to kjak by SSL_CERT_FILE. Go reads it on Linux only (macOS and Windows
// verify with the system's roots), so there a TLS send fails verification
// after the handshake started: the destination still records the
// ClientHello of the direct connection, which is what is checked, and the
// delivery itself is checked on Linux and by the plain-http and SMTP
// feathers on every platform.
func TestProxyEnvironment(t *testing.T) {
	ip := nonLoopbackIPv4(t)
	ca := newTestCA(t, ip)
	proxy := newRecordingProxy(t)
	tlsDest := newTLSDestination(t, ip, ca)
	plainDest := newPlainDestination(t, ip)
	smtp := newSMTPOn(t, net.JoinHostPort(ip.String(), "0"))
	verifies := runtime.GOOS == "linux" // SSL_CERT_FILE is read

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
	allow := fmt.Sprintf("allow_networks = [%q]\n", ip.String()+"/32")
	feathers := func(prefix string) string {
		var b strings.Builder
		fmt.Fprintf(&b, "[feathers.%[1]s_slack]\ntype = \"slack\"\nwebhook_url = { env = \"HOOK_%[1]s_SLACK\" }\n%[2]s\n", prefix, allow)
		fmt.Fprintf(&b, "[feathers.%[1]s_discord]\ntype = \"discord\"\nwebhook_url = { env = \"HOOK_%[1]s_DISCORD\" }\n%[2]s\n", prefix, allow)
		fmt.Fprintf(&b, "[feathers.%s_mail]\ntype = \"email\"\nhost = %q\nport = %d\nsecurity = \"none\"\nfrom = \"kjak@example.com\"\nto = [\"ops@example.com\"]\n%s\n", prefix, ip.String(), smtp.port(), allow)
		fmt.Fprintf(&b, "[feathers.%s_http]\ntype = \"http\"\nurl = %q\n%s\n", prefix, tlsDest.URL+"/http/"+prefix, allow)
		fmt.Fprintf(&b, "[feathers.%s_plain]\ntype = \"http\"\nurl = %q\nallow_http = true\n%s\n", prefix, plainDest.URL+"/plain/"+prefix, allow)
		return b.String()
	}
	for _, prefix := range []string{"global", "project"} {
		s.env = append(s.env,
			"HOOK_"+prefix+"_SLACK="+tlsDest.URL+"/slack/"+prefix,
			"HOOK_"+prefix+"_DISCORD="+tlsDest.URL+"/discord/"+prefix)
	}
	if err := os.WriteFile(filepath.Join(xdg, "config.toml"), []byte(feathers("global")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, ".kjak.toml"), []byte(feathers("project")), 0o644); err != nil {
		t.Fatal(err)
	}
	s.must("config", "trust", "--yes", "--allow-user-secrets")

	type want struct {
		proxied bool   // the proxy sees a CONNECT to the destination
		dest    string // "tls", "plain" or "smtp": where the direct connection lands
	}
	cases := []struct {
		feather string
		want
	}{
		// the control: a global webhook feather uses the proxy
		{"global_slack", want{proxied: true, dest: "tls"}},
		{"global_discord", want{proxied: true, dest: "tls"}},
		{"global_mail", want{dest: "smtp"}},
		{"global_http", want{dest: "tls"}},
		{"global_plain", want{dest: "plain"}},
		{"project_slack", want{dest: "tls"}},
		{"project_discord", want{dest: "tls"}},
		{"project_mail", want{dest: "smtp"}},
		{"project_http", want{dest: "tls"}},
		{"project_plain", want{dest: "plain"}},
	}
	for _, c := range cases {
		t.Run(c.feather, func(t *testing.T) {
			proxy.reset()
			tlsDest.reset()
			hellosBefore, plainBefore, mailBefore := len(tlsDest.hellos()), len(plainDest.paths()), len(smtp.all())
			r := s.kjak("", "send", "--to", c.feather, "-o", "json", "proxy check")
			rep := parseReport(t, r.stdout)
			res := rep.result(t, c.feather)
			conns := proxy.connections()
			dest := tlsDest.Listener.Addr().String()
			if c.proxied {
				if len(conns) == 0 {
					t.Fatalf("the proxy saw no connection: kjak ignored HTTPS_PROXY for a global %s feather\n%s%s", res.Type, r.stdout, r.stderr)
				}
				for _, line := range conns {
					if line != "CONNECT "+dest {
						t.Errorf("the proxy saw %q, want CONNECT %s", line, dest)
					}
				}
			} else if len(conns) != 0 {
				t.Fatalf("the proxy saw %q: a %s feather must connect directly\n%s%s", conns, c.feather, r.stdout, r.stderr)
			}
			switch c.dest {
			case "tls":
				hellos := tlsDest.hellos()[hellosBefore:]
				if len(hellos) == 0 {
					t.Fatalf("the destination saw no TLS handshake\n%s%s", r.stdout, r.stderr)
				}
				for _, h := range hellos {
					// through the CONNECT tunnel the peer is the proxy's
					// outgoing connection, else kjak itself
					if got := proxy.dialedFrom(h.peer); got != c.proxied {
						t.Errorf("handshake from %s: through the proxy %v, want %v", h.peer, got, c.proxied)
					}
				}
				if verifies {
					if res.Status != "delivered" || r.code != 0 {
						t.Fatalf("status %s, exit %d\n%s%s", res.Status, r.code, r.stdout, r.stderr)
					}
					if got := tlsDest.requests(); len(got) != 1 || !strings.Contains(got[0], "proxy check") {
						t.Fatalf("the destination received %q", got)
					}
				} else if res.Status == "delivered" {
					// the test CA is not trusted here: a delivery would mean
					// kjak skipped the verification
					t.Fatalf("delivered without a trusted certificate\n%s", r.stdout)
				}
			case "plain":
				if r.code != 0 || res.Status != "delivered" {
					t.Fatalf("status %s, exit %d\n%s%s", res.Status, r.code, r.stdout, r.stderr)
				}
				if got := plainDest.paths()[plainBefore:]; len(got) != 1 || got[0] != "/plain/"+strings.TrimSuffix(c.feather, "_plain") {
					t.Fatalf("the plain destination received %q", got)
				}
			case "smtp":
				if r.code != 0 || res.Status != "delivered" {
					t.Fatalf("status %s, exit %d\n%s%s", res.Status, r.code, r.stdout, r.stderr)
				}
				if got := smtp.all()[mailBefore:]; len(got) != 1 || !strings.Contains(got[0], "proxy check") {
					t.Fatalf("the SMTP server received %q", got)
				}
			}
		})
	}
}

// nonLoopbackIPv4 is an address of this machine that is not loopback (nor
// link-local), on which a test server can listen.
func nonLoopbackIPv4(t *testing.T) net.IP {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := n.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			ln, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
			if err != nil {
				continue
			}
			_ = ln.Close()
			return ip
		}
	}
	// CI runners always have one; a machine without a network has nothing
	// a proxy could be asked for
	if os.Getenv("CI") != "" {
		t.Fatal("no non-loopback IPv4 address to listen on")
	}
	t.Skip("no non-loopback IPv4 address to listen on (a proxy is never used for loopback)")
	return nil
}

type testCA struct {
	pem  []byte
	leaf tls.Certificate
}

// newTestCA is a CA and a certificate it issued for ip.
func newTestCA(t *testing.T, ip net.IP) testCA {
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
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: ip.String()},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IPAddresses: []net.IP{ip}, KeyUsage: x509.KeyUsageDigitalSignature,
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

func newTLSDestination(t *testing.T, ip net.IP, ca testCA) *tlsDestination {
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
	ln, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	_ = srv.Listener.Close()
	srv.Listener = ln
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

func (d *tlsDestination) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reqs = nil
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

func newPlainDestination(t *testing.T, ip net.IP) *plainDestination {
	d := &plainDestination{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		d.mu.Lock()
		d.ps = append(d.ps, r.URL.Path)
		d.mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	}))
	ln, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	_ = srv.Listener.Close()
	srv.Listener = ln
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
// connection it accepts, tunnels a CONNECT to its target, and answers
// anything else (an absolute-form request through HTTP_PROXY) with 502.
type recordingProxy struct {
	ln    net.Listener
	mu    sync.Mutex
	lines []string
	local map[string]bool // the local addresses of the tunnels it dialed
}

func newRecordingProxy(t *testing.T) *recordingProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &recordingProxy{ln: ln, local: map[string]bool{}}
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
	upstream, err := net.DialTimeout("tcp", req.Host, 10*time.Second)
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
