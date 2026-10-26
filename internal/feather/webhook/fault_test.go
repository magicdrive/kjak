package webhook_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/delivery"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/webhook"
)

// rawServer is an HTTP endpoint scripted at the TCP level. processed counts
// the requests it read whole and treats as accepted: the provider's truth,
// which kjak must never contradict with "rejected".
type rawServer struct {
	l         net.Listener
	processed atomic.Int32
	conns     atomic.Int32
	wg        sync.WaitGroup
}

func newRawServer(t *testing.T, handle func(s *rawServer, c *net.TCPConn, r *bufio.Reader)) *rawServer {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &rawServer{l: l}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			s.conns.Add(1)
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer func() { _ = c.Close() }()
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				handle(s, c.(*net.TCPConn), bufio.NewReader(c))
			}()
		}
	}()
	t.Cleanup(func() { _ = l.Close(); s.wg.Wait() })
	return s
}

func (s *rawServer) url() string { return "http://" + s.l.Addr().String() + "/services/T/B/X" }

// readWhole reads one request with its body: the provider has it.
func (s *rawServer) readWhole(r *bufio.Reader) bool {
	req, err := http.ReadRequest(r)
	if err != nil {
		return false
	}
	if _, err := io.Copy(io.Discard, req.Body); err != nil {
		return false
	}
	s.processed.Add(1)
	return true
}

type scenario struct {
	name   string
	handle func(s *rawServer, c *net.TCPConn, r *bufio.Reader)
	// want: the acceptance of one attempt ("" = rejected or unknown, by
	// timing; both are safe)
	want core.Acceptance
}

var scenarios = []scenario{
	{"close on accept", func(s *rawServer, c *net.TCPConn, r *bufio.Reader) {}, ""},
	{"reset after the headers", func(s *rawServer, c *net.TCPConn, r *bufio.Reader) {
		if _, err := http.ReadRequest(r); err == nil {
			_ = c.SetLinger(0) // RST: the body is not read
		}
	}, ""},
	{"close after the whole request", func(s *rawServer, c *net.TCPConn, r *bufio.Reader) {
		s.readWhole(r)
	}, core.AcceptanceUnknown},
	{"partial status line", func(s *rawServer, c *net.TCPConn, r *bufio.Reader) {
		if s.readWhole(r) {
			_, _ = c.Write([]byte("HTTP/1.1 2"))
		}
	}, core.AcceptanceUnknown},
	{"headers then cut", func(s *rawServer, c *net.TCPConn, r *bufio.Reader) {
		if s.readWhole(r) {
			_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nok"))
		}
	}, core.AcceptanceConfirmed},
	{"garbage reply", func(s *rawServer, c *net.TCPConn, r *bufio.Reader) {
		if s.readWhole(r) {
			_, _ = c.Write([]byte("SMTP 220 hello\r\n\r\n"))
		}
	}, core.AcceptanceUnknown},
	{"503 refusal", func(s *rawServer, c *net.TCPConn, r *bufio.Reader) {
		if req, err := http.ReadRequest(r); err == nil {
			_, _ = io.Copy(io.Discard, req.Body)
			_, _ = c.Write([]byte("HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\n\r\n"))
		}
	}, core.AcceptanceRejected},
}

func payload() []byte {
	b := make([]byte, 60<<10) // larger than a socket buffer may hold at once
	for i := range b {
		b[i] = 'x'
	}
	return append(append([]byte(`{"text":"`), b...), []byte(`"}`)...)
}

// One attempt per scenario: the classification of each fault.
func TestCritical_WebhookFaultClassification(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			srv := newRawServer(t, sc.handle)
			o := poster().Post(context.Background(), credential.NewSecret(srv.url()), payload())
			if sc.want != "" && o.Acceptance != sc.want {
				t.Fatalf("acceptance %s, want %s (%v)", o.Acceptance, sc.want, o.Err)
			}
			if o.Acceptance == core.AcceptanceConfirmed && srv.processed.Load() != 1 {
				t.Fatal("confirmed what the server did not have")
			}
			if srv.processed.Load() == 1 && o.Acceptance == core.AcceptanceRejected && sc.name != "503 refusal" {
				t.Fatalf("rejected a request the server read whole (%v)", o.Err)
			}
			if o.Acceptance == core.AcceptanceUnknown && o.Retryable {
				t.Fatal("unknown and retryable")
			}
			if got := srv.conns.Load(); got != 1 {
				t.Fatalf("%d connections for one attempt: the transport retried by itself", got)
			}
		})
	}
}

// Through the engine, with retries allowed: whatever the fault, the provider
// never gets a message twice, and a message it got is never reported as
// rejected.
func TestCritical_WebhookNoDuplicateDelivery(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			srv := newRawServer(t, sc.handle)
			f := &postFeather{p: poster(), url: srv.url()}
			opt := delivery.Options{
				Timeout: 20 * time.Second, MaxRetries: 3, Concurrency: 1, Clock: clock.Real{},
				Backoff: delivery.Backoff{Base: time.Millisecond, Max: 2 * time.Millisecond},
			}
			r := delivery.Run(context.Background(), core.Message{Body: "x", Level: core.LevelInfo}, []delivery.Target{{Name: "a", Feather: f}}, opt)
			res := r.Results[0]
			got := srv.processed.Load()
			if got > 1 {
				t.Fatalf("the provider got the message %d times (%d attempts)", got, res.Attempts)
			}
			if got == 1 && res.Acceptance == core.AcceptanceRejected && sc.name != "503 refusal" {
				t.Fatalf("the provider has it, kjak says rejected: %+v", res.Error)
			}
			if res.Status == core.StatusDelivered && got != 1 {
				t.Fatal("delivered without the provider having it")
			}
			if int(srv.conns.Load()) != res.Attempts {
				t.Fatalf("%d connections for %d attempts", srv.conns.Load(), res.Attempts)
			}
		})
	}
}

type postFeather struct {
	p   *webhook.Poster
	url string
}

func (f *postFeather) Deliver(ctx context.Context, _ core.Message) feather.Outcome {
	return f.p.Post(ctx, credential.NewSecret(f.url), payload())
}

// No connection is reused: a request never goes out on a connection the
// server may have closed while it was idle (which would turn a request the
// server never read into an "unknown").
func TestCritical_WebhookNoConnectionReuse(t *testing.T) {
	srv := newRawServer(t, func(s *rawServer, c *net.TCPConn, r *bufio.Reader) {
		for s.readWhole(r) {
			if _, err := c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")); err != nil {
				return
			}
		}
	})
	p := poster()
	for i := 0; i < 3; i++ {
		if o := p.Post(context.Background(), credential.NewSecret(srv.url()), []byte(`{}`)); o.Acceptance != core.AcceptanceConfirmed {
			t.Fatalf("attempt %d: %+v", i, o)
		}
	}
	if got := srv.conns.Load(); got != 3 {
		t.Fatalf("3 requests used %d connections", got)
	}
}

// A DNS failure and a broken TLS handshake send nothing: rejected,
// retryable; nothing is dialed twice by the transport.
func TestWebhookDialAndHandshakeFailures(t *testing.T) {
	var dials atomic.Int32
	p := poster()
	tr := p.Client.Transport.(*http.Transport)
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		return nil, &net.DNSError{Err: "no such host", Name: "hooks.example.invalid", IsNotFound: true}
	}
	o := p.Post(context.Background(), credential.NewSecret("https://hooks.example.invalid/x"), []byte(`{}`))
	if o.Acceptance != core.AcceptanceRejected || !o.Retryable || o.Code != core.ErrCodeNetwork || dials.Load() != 1 {
		t.Fatalf("dns: %+v dials=%d", o, dials.Load())
	}
	var dnsErr *net.DNSError
	if errors.As(o.Err, &dnsErr) {
		t.Fatal("the raw error (with the host) is passed on")
	}

	junk := newRawServer(t, func(s *rawServer, c *net.TCPConn, r *bufio.Reader) {
		_, _ = c.Write([]byte("this is not TLS\r\n"))
	})
	o = poster().Post(context.Background(), credential.NewSecret("https://"+junk.l.Addr().String()+"/x"), []byte(`{}`))
	if o.Acceptance != core.AcceptanceRejected || !o.Retryable || junk.processed.Load() != 0 {
		t.Fatalf("tls: %+v", o)
	}
}
