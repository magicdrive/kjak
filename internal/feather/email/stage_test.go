package email_test

import (
	"context"
	"mime"
	"net"
	"net/mail"
	"strings"
	"syscall"
	"testing"

	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/email"
)

// failingConn fails the first write that fail selects, as a reset
// connection would.
type failingConn struct {
	net.Conn
	fail func([]byte) bool
}

func (c *failingConn) Write(p []byte) (int, error) {
	if c.fail(p) {
		return 0, &net.OpError{Op: "write", Net: "tcp", Err: syscall.ECONNRESET}
	}
	return c.Conn.Write(p)
}

// The stage of a broken write decides the acceptance: the content without
// its terminator cannot be accepted (rejected, retryable); from the
// terminator on, the server may have it (unknown, never retried).
func TestCritical_SMTPWriteFailureByStage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fail      func([]byte) bool
		want      core.Acceptance
		retryable bool
	}{
		{"during the content", func(p []byte) bool { return strings.Contains(string(p), "Subject:") }, core.AcceptanceRejected, true},
		{"at the terminator", func(p []byte) bool { return string(p) == ".\r\n" }, core.AcceptanceUnknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeSMTP(t, nil)
			f := newFeather(s, nil, email.SecurityNone)
			f.User = ""
			f.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
				c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				return &failingConn{Conn: c, fail: tc.fail}, nil
			}
			o := f.Deliver(context.Background(), msg)
			if o.Acceptance != tc.want || o.Retryable != tc.retryable {
				t.Fatalf("outcome = %+v (%v)", o, o.Err)
			}
		})
	}
}

// SIGINT (the interrupted cause) before the data is a certain non-delivery,
// not retried; after the terminator it is unknown.
func TestCritical_SMTPInterrupted(t *testing.T) {
	s := newFakeSMTP(t, nil)
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(feather.ErrInterrupted)
	f := newFeather(s, nil, email.SecurityNone)
	f.User = ""
	if o := f.Deliver(ctx, msg); o.Acceptance != core.AcceptanceRejected || o.Retryable || o.Code != core.ErrCodeCanceled {
		t.Fatalf("before the data: %+v (%v)", o, o.Err)
	}

	release := make(chan struct{})
	defer close(release)
	s = newFakeSMTP(t, func(s *fakeSMTP) { s.stallAfterData = release })
	ctx, cancel = context.WithCancelCause(context.Background())
	go func() {
		<-s.dataReceived
		cancel(feather.ErrInterrupted)
	}()
	f = newFeather(s, nil, email.SecurityNone)
	f.User = ""
	if o := f.Deliver(ctx, msg); o.Acceptance != core.AcceptanceUnknown || o.Retryable {
		t.Fatalf("after the terminator: %+v (%v)", o, o.Err)
	}
}

// RFC 5322 2.1.1: a line is at most 998 characters. A long encoded subject
// and many recipients are folded, and read back unchanged.
func TestComposeFoldsLongHeaders(t *testing.T) {
	var to []string
	for i := 0; i < 60; i++ {
		to = append(to, strings.Repeat("r", 20)+string(rune('a'+i%26))+"@example.com")
	}
	title := strings.Repeat("通知テスト", 40)
	f := newFeather(newFakeSMTP(t, nil), nil, email.SecurityNone)
	f.To = to
	b, err := f.Compose(core.Message{Title: title, Body: "x", Level: core.LevelInfo})
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(b), "\r\n\r\n")
	for _, line := range strings.Split(head, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("a header line of %d characters: %.60s...", len(line), line)
		}
	}
	m, err := mail.ReadMessage(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil || subject != title {
		t.Fatalf("subject reads back as %q (%v)", subject, err)
	}
	list, err := m.Header.AddressList("To")
	if err != nil || len(list) != len(to) {
		t.Fatalf("To reads back as %d addresses (%v)", len(list), err)
	}
	for i, a := range list {
		if a.Address != to[i] {
			t.Fatalf("recipient %d: %s, want %s", i, a.Address, to[i])
		}
	}
	if !strings.Contains(head, "\r\n ") {
		t.Fatal("nothing was folded")
	}
}
