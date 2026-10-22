package email_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/feather/email"
)

// A server that echoes the AUTH argument (base64 of the user and the
// password) in a refusal: the reply text never reaches the result.
func TestCritical_SMTPAuthReplyNotEchoed(t *testing.T) {
	srvTLS, roots := testCert(t)
	for _, reply := range []string{"454 4.7.0 temporary failure for %s", "421 4.7.0 closing, got %s", "535 5.7.8 bad %s", "504 unsupported %s"} {
		s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls = srvTLS; withAuth(s); s.authReply = reply })
		o := newFeather(s, roots, email.SecuritySTARTTLS).Deliver(context.Background(), msg)
		token := base64.StdEncoding.EncodeToString([]byte("\x00kjak\x00s3cret-pass"))
		if o.Err == nil || strings.Contains(o.Err.Error(), token) || strings.Contains(o.Err.Error(), "PLAIN") {
			t.Errorf("%q: error = %v", reply, o.Err)
		}
		if o.Acceptance != core.AcceptanceRejected {
			t.Errorf("%q: outcome = %+v", reply, o)
		}
	}
}

// Clear-text bytes that arrive with the 220 reply to STARTTLS, before the
// TLS handshake, were not protected by TLS: the session is refused instead
// of carrying on (STARTTLS response injection).
func TestSTARTTLSInjectionRefused(t *testing.T) {
	srvTLS, roots := testCert(t)
	s := newFakeSMTP(t, func(s *fakeSMTP) {
		s.tls = srvTLS
		withAuth(s)
		s.injectAfterSTARTTLS = "250-injected\r\n250 AUTH PLAIN\r\n"
	})
	o := newFeather(s, roots, email.SecuritySTARTTLS).Deliver(context.Background(), msg)
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || o.Code != core.ErrCodeProtocol {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
	if s.sawVerb("AUTH") || len(s.received()) != 0 {
		t.Fatal("the session went on after the injection")
	}
}

// One refused recipient refuses the message: no DATA is sent to the others
// (a partial delivery would be reported as a failure and retried whole).
func TestRecipientPartlyRefused(t *testing.T) {
	srvTLS, roots := testCert(t)
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls = srvTLS; withAuth(s); s.rcptRefuse = "b@example.com" })
	o := newFeather(s, roots, email.SecuritySTARTTLS).Deliver(context.Background(), msg)
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || o.StatusCode != 550 {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
	if s.sawVerb("DATA") {
		t.Fatal("DATA was sent")
	}
}

// Replies to the end of the data: a multi-line 250 confirms; a reply that is
// not SMTP after the terminator is ambiguous, never a success or a retry.
func TestFinalReplyParsing(t *testing.T) {
	srvTLS, roots := testCert(t)
	for reply, want := range map[string]core.Acceptance{
		"250-2.0.0 queued\r\n250 2.0.0 as 12345": core.AcceptanceConfirmed,
		"2x0 garbage":                            core.AcceptanceUnknown,
		"354 again?":                             core.AcceptanceUnknown,
		"250":                                    core.AcceptanceConfirmed,
	} {
		s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls = srvTLS; withAuth(s); s.replies["END"] = reply })
		o := newFeather(s, roots, email.SecuritySTARTTLS).Deliver(context.Background(), msg)
		if o.Acceptance != want || o.Retryable {
			t.Errorf("%q: outcome = %+v (%v)", reply, o, o.Err)
		}
	}
}

// Lines that are or start with "." arrive intact (dot stuffing), and a body
// line "." never ends the data early.
func TestDotStuffingEndToEnd(t *testing.T) {
	srvTLS, roots := testCert(t)
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.tls = srvTLS; withAuth(s) })
	m := core.Message{Body: ".\n..\n.leading\nlast", Level: core.LevelInfo}
	if o := newFeather(s, roots, email.SecuritySTARTTLS).Deliver(context.Background(), m); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
	got := s.received()[0]
	_, body, _ := strings.Cut(got, "\n\n")
	if !strings.HasPrefix(body, ".\n..\n.leading\nlast") {
		t.Fatalf("body = %q", body)
	}
}

// A server (or, before STARTTLS, anyone on the path) cannot make kjak read
// without bound: replies are limited.
func TestHugeReplyRefused(t *testing.T) {
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.hugeGreeting = 8 << 20 })
	f := newFeather(s, nil, email.SecurityNone)
	f.User = ""
	o := f.Deliver(context.Background(), msg)
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || o.Code != core.ErrCodeProtocol {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
}
