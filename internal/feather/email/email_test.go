package email_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/email"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

var msg = core.Message{Title: "Disk almost full", Body: "/var at 91%\n.hidden line\nend", Level: core.LevelWarning, URL: "https://example.com/d"}

func newFeather(s *fakeSMTP, roots *x509.CertPool, security string) *email.Feather {
	return &email.Feather{
		Host: "127.0.0.1", Port: s.port(), Security: security,
		User: s.user, Password: credential.NewSecret(s.pass),
		From: "kjak@example.com", To: []string{"a@example.com", "b@example.com"},
		Clock: clock.NewFake(t0), Rand: bytes.NewReader(make([]byte, 64)),
		TLSConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
	}
}

func withAuth(s *fakeSMTP) { s.user, s.pass = "kjak", "s3cret-pass" }

func TestDeliverSTARTTLSWithAuth(t *testing.T) {
	srvTLS, roots := testCert(t)
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls = srvTLS; withAuth(s) })
	o := newFeather(s, roots, email.SecuritySTARTTLS).Deliver(context.Background(), msg)
	if o.Acceptance != core.AcceptanceConfirmed || o.StatusCode != 250 {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
	got := s.received()
	if len(got) != 1 {
		t.Fatalf("%d messages", len(got))
	}
	m := got[0]
	for _, want := range []string{"From: kjak@example.com\n", "To: a@example.com, b@example.com\n", "Subject: Disk almost full\n", "X-Kjak-Level: warning\n", "Date: Thu, 01 Oct 2026 12:00:00 +0000\n", "Message-ID: <00000000000000000000000000000000@kjak>\n"} {
		if !strings.Contains(m, want) {
			t.Errorf("message lacks %q:\n%s", want, m)
		}
	}
	_, body, _ := strings.Cut(m, "\n\n")
	decoded, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if !strings.Contains(string(decoded), ".hidden line") || !strings.Contains(string(decoded), "https://example.com/d") {
		t.Fatalf("body = %q", decoded)
	}
}

func TestDeliverImplicitTLS(t *testing.T) {
	srvTLS, roots := testCert(t)
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls, s.implicit = srvTLS, true; withAuth(s) })
	if o := newFeather(s, roots, email.SecurityTLS).Deliver(context.Background(), msg); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
}

func TestDeliverPlainNoAuth(t *testing.T) {
	s := newFakeSMTP(t, nil)
	f := newFeather(s, nil, email.SecurityNone)
	f.User, f.Password = "", credential.Secret{}
	if o := f.Deliver(context.Background(), msg); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
	if s.sawVerb("AUTH") || s.sawVerb("STARTTLS") {
		t.Fatal("unexpected AUTH or STARTTLS")
	}
}

// starttls never falls back to clear text: no STARTTLS offered, no AUTH sent.
func TestSTARTTLSRequired(t *testing.T) {
	srvTLS, roots := testCert(t)
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls, s.noTLS = srvTLS, true; withAuth(s) })
	o := newFeather(s, roots, email.SecuritySTARTTLS).Deliver(context.Background(), msg)
	if o.Acceptance != core.AcceptanceRejected || o.Retryable {
		t.Fatalf("outcome = %+v", o)
	}
	if s.sawVerb("AUTH") || s.sawVerb("MAIL") {
		t.Fatal("the password or the message went out without TLS")
	}
}

func TestUntrustedCertificateRefused(t *testing.T) {
	srvTLS, _ := testCert(t)
	_, otherRoots := testCert(t)
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls = srvTLS; withAuth(s) })
	o := newFeather(s, otherRoots, email.SecuritySTARTTLS).Deliver(context.Background(), msg)
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || s.sawVerb("AUTH") {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestCritical_SMTPReplyClassification(t *testing.T) {
	srvTLS, roots := testCert(t)
	cases := []struct {
		name       string
		verb       string
		reply      string
		user, pass string
		retryable  bool
		code       core.ErrorCode
		status     int
	}{
		{"greeting busy", "GREETING", "421 4.3.2 busy", "", "", true, core.ErrCodeServer, 421},
		{"mail transient", "MAIL", "451 4.3.0 try later", "", "", true, core.ErrCodeServer, 451},
		{"mail permanent", "MAIL", "550 5.7.1 sender refused", "", "", false, core.ErrCodeRejected, 550},
		{"rcpt permanent", "RCPT", "550 5.1.1 no such user", "", "", false, core.ErrCodeRejected, 550},
		{"rcpt transient", "RCPT", "452 4.2.2 mailbox full", "", "", true, core.ErrCodeServer, 452},
		{"data refused", "DATA", "554 5.5.1 no", "", "", false, core.ErrCodeRejected, 554},
		{"end transient", "END", "451 4.3.0 queue full", "", "", true, core.ErrCodeServer, 451},
		{"end permanent", "END", "554 5.7.1 spam", "", "", false, core.ErrCodeRejected, 554},
		{"auth refused", "", "", "kjak", "wrong", false, core.ErrCodeRejected, 535},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeSMTP(t, func(s *fakeSMTP) {
				s.tls = srvTLS
				s.user, s.pass = "kjak", "right"
				if tc.verb != "" {
					s.replies[tc.verb] = tc.reply
				}
			})
			f := newFeather(s, roots, email.SecuritySTARTTLS)
			f.Password = credential.NewSecret("right")
			if tc.pass != "" {
				f.Password = credential.NewSecret(tc.pass)
			}
			o := f.Deliver(context.Background(), msg)
			if o.Acceptance != core.AcceptanceRejected || o.Retryable != tc.retryable || o.Code != tc.code || o.StatusCode != tc.status {
				t.Fatalf("outcome = %+v (%v)", o, o.Err)
			}
			if strings.Contains(o.Err.Error(), "wrong") || strings.Contains(o.Err.Error(), "right") {
				t.Fatalf("the error shows the password: %v", o.Err)
			}
		})
	}
}

// The server received the whole message, terminator included, and the
// connection broke before its reply: the message may be accepted, so it is
// unknown and must not be retried.
func TestCritical_SMTPAmbiguousAfterDataTerminator(t *testing.T) {
	srvTLS, roots := testCert(t)
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls = srvTLS; s.closeAfterData = true; withAuth(s) })
	o := newFeather(s, roots, email.SecuritySTARTTLS).Deliver(context.Background(), msg)
	if o.Acceptance != core.AcceptanceUnknown || o.Retryable || o.Code != core.ErrCodeAmbiguous {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
	if len(s.received()) != 1 {
		t.Fatal("the server did not get the message")
	}
}

// The deadline passes while the server holds its reply: unknown as well.
func TestCritical_SMTPDeadlineAfterDataTerminator(t *testing.T) {
	srvTLS, roots := testCert(t)
	release := make(chan struct{})
	defer close(release)
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls = srvTLS; s.stallAfterData = release; withAuth(s) })
	ctx, cancel := context.WithCancelCause(context.Background())
	go func() {
		<-s.dataReceived
		cancel(feather.ErrDeadline)
	}()
	o := newFeather(s, roots, email.SecuritySTARTTLS).Deliver(ctx, msg)
	if o.Acceptance != core.AcceptanceUnknown || o.Retryable {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
}

// A connection that cannot be made sends nothing: retryable.
func TestCritical_SMTPConnectRefusedIsRetryable(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	f := &email.Feather{Host: "127.0.0.1", Port: port, Security: email.SecurityNone, From: "a@example.com", To: []string{"b@example.com"}, Clock: clock.NewFake(t0)}
	o := f.Deliver(context.Background(), msg)
	if o.Acceptance != core.AcceptanceRejected || !o.Retryable || o.Code != core.ErrCodeNetwork {
		t.Fatalf("outcome = %+v", o)
	}
}

// A deadline before the data is a certain non-delivery.
func TestCritical_SMTPDeadlineBeforeData(t *testing.T) {
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.replies["GREETING"] = "220 slow" })
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(feather.ErrDeadline)
	f := newFeather(s, nil, email.SecurityNone)
	f.User = ""
	o := f.Deliver(ctx, msg)
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || o.Code != core.ErrCodeDeadline {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
}

func TestComposeEncodesSubjectAndUsesCRLF(t *testing.T) {
	f := &email.Feather{From: "a@example.com", To: []string{"b@example.com"}, SubjectPrefix: "[kjak]", Clock: clock.NewFake(t0), Rand: bytes.NewReader(make([]byte, 64))}
	b, err := f.Compose(core.Message{Title: "デプロイ完了", Body: "line1\nline2", Level: core.LevelSuccess})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(strings.ReplaceAll(s, "\r\n", ""), "\n") {
		t.Fatal("a bare LF in the message")
	}
	var subject string
	for _, line := range strings.Split(s, "\r\n") {
		if v, ok := strings.CutPrefix(line, "Subject: "); ok {
			subject = v
		}
	}
	dec, err := new(mime.WordDecoder).DecodeHeader(subject)
	if err != nil || dec != "[kjak] デプロイ完了" {
		t.Fatalf("subject %q decodes to %q (%v)", subject, dec, err)
	}
	// without a title, the subject is the level and the first line
	if b, err = f.Compose(core.Message{Body: "first\nsecond", Level: core.LevelError}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Subject: [kjak] [ERROR] first\r\n") {
		t.Fatalf("message = %s", b)
	}
}

func TestDefaultPort(t *testing.T) {
	if email.DefaultPort("starttls") != 587 || email.DefaultPort("tls") != 465 || email.DefaultPort("none") != 25 || email.DefaultPort("") != 587 {
		t.Fatal("default ports")
	}
}
