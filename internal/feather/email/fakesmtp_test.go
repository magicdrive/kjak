package email_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is a scripted SMTP server for the tests.
type fakeSMTP struct {
	t        *testing.T
	l        net.Listener
	tls      *tls.Config // STARTTLS (or implicit TLS) when set
	implicit bool        // TLS from the first byte
	noTLS    bool        // do not offer STARTTLS
	user     string
	pass     string

	// replies overrides the reply to a verb ("GREETING", "MAIL", "RCPT",
	// "DATA", "END" = the reply to the end of the data).
	replies map[string]string
	// authReply, when set, answers AUTH; "%s" is replaced by the client's
	// AUTH argument (a server that echoes what it got).
	authReply string
	// injectAfterSTARTTLS is sent in clear text together with the 220 reply
	// to STARTTLS (a STARTTLS response injection).
	injectAfterSTARTTLS string
	// hugeGreeting sends a greeting line of this many bytes.
	hugeGreeting int
	// rcptRefuse refuses the recipients that contain it (550).
	rcptRefuse string
	// closeAfterData closes the connection after the data, without a reply.
	closeAfterData bool
	// stallAfterData waits for release after the data, without a reply.
	stallAfterData chan struct{}
	// stallOnQuit waits for release after QUIT, without a reply.
	stallOnQuit  chan struct{}
	dataReceived chan struct{}

	mu       sync.Mutex
	messages []string
	verbs    []string
}

func newFakeSMTP(t *testing.T, configure func(*fakeSMTP)) *fakeSMTP {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{t: t, l: l, replies: map[string]string{}, dataReceived: make(chan struct{}, 4)}
	if configure != nil {
		configure(s)
	}
	t.Cleanup(func() { _ = l.Close() })
	go s.serve()
	return s
}

func (s *fakeSMTP) port() int { return s.l.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) serve() {
	for {
		c, err := s.l.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *fakeSMTP) record(verb string) {
	s.mu.Lock()
	s.verbs = append(s.verbs, verb)
	s.mu.Unlock()
}

func (s *fakeSMTP) sawVerb(verb string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.verbs {
		if v == verb {
			return true
		}
	}
	return false
}

func (s *fakeSMTP) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.messages...)
}

func (s *fakeSMTP) reply(tp *textproto.Conn, verb, def string) bool {
	r, ok := s.replies[verb]
	if !ok {
		r = def
	}
	_ = tp.PrintfLine("%s", r)
	return !ok || strings.HasPrefix(r, "2") || strings.HasPrefix(r, "3")
}

func (s *fakeSMTP) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	secure := false
	if s.implicit {
		tc := tls.Server(c, s.tls)
		if err := tc.Handshake(); err != nil {
			return
		}
		c, secure = tc, true
	}
	tp := textproto.NewConn(c)
	if s.hugeGreeting > 0 {
		_, _ = c.Write([]byte("220 " + strings.Repeat("x", s.hugeGreeting) + "\r\n"))
		return
	}
	if !s.reply(tp, "GREETING", "220 fake ESMTP") {
		return
	}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(verb)
		s.record(verb)
		switch verb {
		case "EHLO":
			lines := []string{"250-fake"}
			if s.tls != nil && !secure && !s.noTLS {
				lines = append(lines, "250-STARTTLS")
			}
			lines = append(lines, "250-AUTH PLAIN LOGIN", "250 8BITMIME")
			for _, l := range lines {
				_ = tp.PrintfLine("%s", l)
			}
		case "STARTTLS":
			if s.injectAfterSTARTTLS != "" {
				_, _ = c.Write([]byte("220 go ahead\r\n" + s.injectAfterSTARTTLS))
			} else {
				_ = tp.PrintfLine("220 go ahead")
			}
			tc := tls.Server(c, s.tls)
			if err := tc.Handshake(); err != nil {
				return
			}
			c, secure = tc, true
			tp = textproto.NewConn(c)
		case "AUTH":
			if s.authReply != "" {
				_ = tp.PrintfLine("%s", strings.ReplaceAll(s.authReply, "%s", arg))
				continue
			}
			mech, token, _ := strings.Cut(arg, " ")
			if mech != "PLAIN" {
				_ = tp.PrintfLine("504 unsupported")
				continue
			}
			raw, _ := base64.StdEncoding.DecodeString(token)
			if string(raw) == "\x00"+s.user+"\x00"+s.pass {
				_ = tp.PrintfLine("235 ok")
			} else {
				_ = tp.PrintfLine("535 5.7.8 bad credentials")
			}
		case "MAIL":
			s.reply(tp, "MAIL", "250 ok")
		case "RCPT":
			if s.rcptRefuse != "" && strings.Contains(arg, s.rcptRefuse) {
				_ = tp.PrintfLine("550 5.1.1 no such user")
				continue
			}
			s.reply(tp, "RCPT", "250 ok")
		case "DATA":
			if !s.reply(tp, "DATA", "354 go ahead") {
				continue
			}
			b, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.messages = append(s.messages, string(b))
			s.mu.Unlock()
			s.dataReceived <- struct{}{}
			if s.closeAfterData {
				return
			}
			if s.stallAfterData != nil {
				<-s.stallAfterData
				return
			}
			s.reply(tp, "END", "250 2.0.0 queued")
		case "QUIT":
			if s.stallOnQuit != nil {
				<-s.stallOnQuit
				return
			}
			_ = tp.PrintfLine("221 bye")
			return
		default:
			_ = tp.PrintfLine("502 unknown")
		}
	}
}

// testCert is a self-signed certificate for 127.0.0.1.
func testCert(t *testing.T) (server *tls.Config, roots *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kjak test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	roots = x509.NewCertPool()
	roots.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, roots
}
