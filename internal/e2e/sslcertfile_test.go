package e2e

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
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

// SSL_CERT_FILE and SSL_CERT_DIR decide which certificate authorities the
// TLS clients of kjak trust — the webhook feathers (slack, discord), the
// http feathers and email (docs/security.md "Certificate authorities").
// kjak gives them no roots of its own: they use crypto/x509's system roots,
// which since Go 1.27 (go.mod) read the two variables on every platform.
// On macOS and Windows a variable that is set replaces the platform
// verifier (the Keychain, the Windows certificate store): only the
// certificates of the file or directory are trusted.
//
// A webhook, an http and an email (implicit TLS) feather send to servers
// whose certificate a CA of the test issued for 127.0.0.1:
//
//   - without the variables, all three are refused: the CA is unknown, and
//     nothing is received;
//   - with SSL_CERT_FILE (the CA's file) or SSL_CERT_DIR (a directory that
//     holds it), all three deliver;
//   - with SSL_CERT_FILE naming a file that does not exist, all three are
//     refused.
func TestBinarySSLCertFile(t *testing.T) {
	ca := newLoopbackCA(t)
	srvTLS := &tls.Config{Certificates: []tls.Certificate{ca.leaf}, MinVersion: tls.VersionTLS12}
	var mu sync.Mutex
	var received []string
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(received) }
	hook := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	}))
	hook.TLS = srvTLS
	hook.StartTLS()
	t.Cleanup(hook.Close)
	smtp := newTLSSMTP(t, srvTLS)

	s := newSandbox(t)
	caDir := filepath.Join(filepath.Dir(s.dir), "cadir")
	if err := os.MkdirAll(caDir, 0o700); err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(caDir, "kjak-test-ca.pem")
	if err := os.WriteFile(caFile, ca.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	s.env = append(s.env, "HOOK="+hook.URL+"/slack")
	s.writeConfig(fmt.Sprintf(`[feathers.hook]
type = "slack"
webhook_url = { env = "HOOK" }

[feathers.api]
type = "http"
url = %q
allow_networks = ["127.0.0.1/32"]

[feathers.mail]
type = "email"
host = "127.0.0.1"
port = %d
security = "tls"
from = "kjak@example.com"
to = ["ops@example.com"]
`, hook.URL+"/http", smtp.port()), "")
	base := s.env

	for _, c := range []struct {
		name      string
		env       []string
		delivered bool
	}{
		{"no variable", nil, false},
		{"SSL_CERT_FILE", []string{"SSL_CERT_FILE=" + caFile}, true},
		{"SSL_CERT_DIR", []string{"SSL_CERT_DIR=" + caDir}, true},
		{"SSL_CERT_FILE missing", []string{"SSL_CERT_FILE=" + filepath.Join(caDir, "missing.pem")}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s.env = append(append([]string(nil), base...), c.env...)
			for _, name := range []string{"hook", "api", "mail"} {
				before, mails := count(), len(smtp.all())
				r := s.kjak("", "send", "--to", name, "--timeout", "20s", "-o", "json", "tls check")
				res := parseReport(t, r.stdout).result(t, name)
				got := count() > before || len(smtp.all()) > mails
				if c.delivered {
					if res.Status != "delivered" || r.code != 0 || !got {
						t.Errorf("%s: status %s, exit %d, received %v\n%s%s", name, res.Status, r.code, got, r.stdout, r.stderr)
					}
					continue
				}
				if res.Status == "delivered" || r.code == 0 || got {
					t.Errorf("%s: delivered without a trusted CA: status %s, exit %d\n%s", name, res.Status, r.code, r.stdout)
					continue
				}
				// refused before anything was sent: certainly not accepted
				if res.Acceptance != "rejected" || res.Error == nil || !strings.Contains(strings.ToLower(res.Error.Message), "certificate") {
					t.Errorf("%s: want a certificate refusal, got acceptance %s, error %+v", name, res.Acceptance, res.Error)
				}
			}
		})
	}
}

type loopbackCA struct {
	pem  []byte
	leaf tls.Certificate
}

// newLoopbackCA is a CA and a certificate it issued for 127.0.0.1.
func newLoopbackCA(t *testing.T) loopbackCA {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kjak e2e SSL_CERT_FILE test CA"},
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
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return loopbackCA{
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		leaf: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
	}
}

// newTLSSMTP is the SMTP server of compat_v004_test.go behind implicit TLS
// on 127.0.0.1.
func newTLSSMTP(t *testing.T, cfg *tls.Config) *compatSMTP {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := &compatSMTP{ln: ln}
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
