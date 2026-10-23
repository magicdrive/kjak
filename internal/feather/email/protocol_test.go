package email_test

import (
	"context"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/delivery"
	"github.com/magicdrive/kjak/internal/feather/email"
)

// Through the engine, retries allowed: each stage of an SMTP transaction
// that fails gives its acceptance and its number of attempts, and the server
// accepts the message once at most. A refusal before the end of the data, or
// of the data itself, is retried when transient (4xx); a reply lost after
// the data is unknown and never retried.
func TestCritical_SMTPProtocolMatrix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*fakeSMTP)
		status    core.Status
		want      core.Acceptance
		attempts  int
		accepted  int // messages the server answered 2xx to, or kept without a reply
	}{
		{"accepted", nil, core.StatusDelivered, core.AcceptanceConfirmed, 1, 1},
		{"greeting 421", func(s *fakeSMTP) { s.replies["GREETING"] = "421 busy" }, core.StatusFailed, core.AcceptanceRejected, 4, 0},
		{"greeting 554", func(s *fakeSMTP) { s.replies["GREETING"] = "554 no service" }, core.StatusFailed, core.AcceptanceRejected, 1, 0},
		{"MAIL FROM 451", func(s *fakeSMTP) { s.replies["MAIL"] = "451 4.3.0 try later" }, core.StatusFailed, core.AcceptanceRejected, 4, 0},
		{"MAIL FROM 553", func(s *fakeSMTP) { s.replies["MAIL"] = "553 5.7.1 sender refused" }, core.StatusFailed, core.AcceptanceRejected, 1, 0},
		{"RCPT TO 550", func(s *fakeSMTP) { s.rcptRefuse = "b@" }, core.StatusFailed, core.AcceptanceRejected, 1, 0},
		{"RCPT TO 452", func(s *fakeSMTP) { s.replies["RCPT"] = "452 4.5.3 too many recipients" }, core.StatusFailed, core.AcceptanceRejected, 4, 0},
		{"DATA 554", func(s *fakeSMTP) { s.replies["DATA"] = "554 5.5.1 no valid recipients" }, core.StatusFailed, core.AcceptanceRejected, 1, 0},
		{"data refused 451", func(s *fakeSMTP) { s.replies["END"] = "451 4.3.0 queue full" }, core.StatusFailed, core.AcceptanceRejected, 4, 0},
		{"data refused 554", func(s *fakeSMTP) { s.replies["END"] = "554 5.7.1 spam" }, core.StatusFailed, core.AcceptanceRejected, 1, 0},
		{"reply to the data lost", func(s *fakeSMTP) { s.closeAfterData = true }, core.StatusFailed, core.AcceptanceUnknown, 1, 1},
		{"reply to the data malformed", func(s *fakeSMTP) { s.replies["END"] = "ok then" }, core.StatusFailed, core.AcceptanceUnknown, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeSMTP(t, tc.configure)
			f := newFeather(s, nil, email.SecurityNone)
			f.User, f.Rand = "", nil
			opt := delivery.Options{
				Timeout: 20 * time.Second, MaxRetries: 3, Concurrency: 1, Clock: clock.Real{},
				Backoff: delivery.Backoff{Base: time.Millisecond, Max: 2 * time.Millisecond},
			}
			r := delivery.Run(context.Background(), msg, []delivery.Target{{Name: "m", Type: "email", Feather: f}}, opt)
			res := r.Results[0]
			accepted := len(s.received())
			if _, refused := s.replies["END"]; refused && tc.name != "reply to the data malformed" {
				accepted = 0 // the server answered 4xx or 5xx to the data
			}
			if res.Status != tc.status || res.Acceptance != tc.want || res.Attempts != tc.attempts || accepted != tc.accepted {
				t.Fatalf("%s %s after %d attempts, the server accepted %d (%+v)", res.Status, res.Acceptance, res.Attempts, accepted, res.Error)
			}
			if res.Acceptance == core.AcceptanceUnknown && r.Summary.Unknown != 1 {
				t.Fatalf("summary %+v", r.Summary)
			}
		})
	}
}

// A message to several recipients is one transaction: each recipient in a
// RCPT TO, the data once, then QUIT.
func TestSMTPRecipientsInOneTransaction(t *testing.T) {
	s := newFakeSMTP(t, nil)
	f := newFeather(s, nil, email.SecurityNone)
	f.User = ""
	f.To = []string{"a@example.com", "b@example.com", "c@example.com"}
	if o := f.Deliver(context.Background(), msg); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("%+v (%v)", o, o.Err)
	}
	s.mu.Lock()
	verbs := append([]string(nil), s.verbs...)
	s.mu.Unlock()
	want := []string{"EHLO", "MAIL", "RCPT", "RCPT", "RCPT", "DATA", "QUIT"}
	if len(verbs) != len(want) {
		t.Fatalf("verbs %v, want %v", verbs, want)
	}
	for i := range want {
		if verbs[i] != want[i] {
			t.Fatalf("verbs %v, want %v", verbs, want)
		}
	}
	if n := len(s.received()); n != 1 {
		t.Fatalf("%d messages", n)
	}
}
