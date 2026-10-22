package webhook_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/discord"
	"github.com/magicdrive/kjak/internal/feather/webhook"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func poster() *webhook.Poster {
	return &webhook.Poster{Client: webhook.NewClient(), Clock: clock.NewFake(t0)}
}

func post(t *testing.T, p *webhook.Poster, url string) feather.Outcome {
	t.Helper()
	return p.Post(context.Background(), credential.NewSecret(url), []byte(`{"text":"hi"}`))
}

func TestCritical_WebhookStatusClassification(t *testing.T) {
	cases := []struct {
		status     int
		acceptance core.Acceptance
		retryable  bool
		code       core.ErrorCode
	}{
		{200, core.AcceptanceConfirmed, false, ""},
		{204, core.AcceptanceConfirmed, false, ""},
		{400, core.AcceptanceRejected, false, core.ErrCodeRejected},
		{401, core.AcceptanceRejected, false, core.ErrCodeRejected},
		{403, core.AcceptanceRejected, false, core.ErrCodeRejected},
		{404, core.AcceptanceRejected, false, core.ErrCodeRejected},
		{408, core.AcceptanceRejected, true, core.ErrCodeServer},
		{410, core.AcceptanceRejected, false, core.ErrCodeRejected},
		{429, core.AcceptanceRejected, true, core.ErrCodeRateLimited},
		{500, core.AcceptanceRejected, true, core.ErrCodeServer},
		{502, core.AcceptanceUnknown, false, core.ErrCodeAmbiguous},
		{503, core.AcceptanceRejected, true, core.ErrCodeServer},
		{504, core.AcceptanceUnknown, false, core.ErrCodeAmbiguous},
		{302, core.AcceptanceRejected, false, core.ErrCodeProtocol},
	}
	for _, tc := range cases {
		redirected := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/elsewhere" {
				redirected = true
				return
			}
			_, _ = io.Copy(io.Discard, r.Body)
			if tc.status == 302 {
				w.Header().Set("Location", "/elsewhere")
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte("detail text"))
		}))
		o := post(t, poster(), srv.URL+"/hook")
		srv.Close()
		if o.Acceptance != tc.acceptance || o.Retryable != tc.retryable || o.Code != tc.code {
			t.Errorf("HTTP %d: outcome = %+v, want %s retryable=%t %s", tc.status, o, tc.acceptance, tc.retryable, tc.code)
		}
		if o.Acceptance == core.AcceptanceUnknown && o.Retryable {
			t.Errorf("HTTP %d: unknown and retryable", tc.status)
		}
		if redirected {
			t.Errorf("HTTP %d: a redirect was followed", tc.status)
		}
	}
}

func TestCritical_WebhookRetryAfter(t *testing.T) {
	p := poster()
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"7", 7 * time.Second},
		{t0.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{t0.Add(-time.Minute).Format(http.TimeFormat), 0},
		{"soon", 0},
		{"", 0},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.header != "" {
				w.Header().Set("Retry-After", tc.header)
			}
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		o := post(t, p, srv.URL)
		srv.Close()
		if o.Code != core.ErrCodeRateLimited || !o.Retryable || o.RetryAfter != tc.want {
			t.Errorf("Retry-After %q: outcome = %+v, want wait %s", tc.header, o, tc.want)
		}
	}
}

func TestCritical_WebhookRetryAfterDiscordBody(t *testing.T) {
	p := poster()
	p.RetryAfterBody = discord.RetryAfterBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message": "You are being rate limited.", "retry_after": 1.5, "global": false}`))
	}))
	defer srv.Close()
	o := post(t, p, srv.URL)
	if o.RetryAfter != 1500*time.Millisecond {
		t.Fatalf("retry after = %s", o.RetryAfter)
	}
	// the header wins over the body
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"retry_after": 9}`))
	}))
	defer srv2.Close()
	if o := post(t, p, srv2.URL); o.RetryAfter != 2*time.Second {
		t.Fatalf("retry after = %s", o.RetryAfter)
	}
}

// The server read the whole request, then the connection broke: the
// message may have been accepted.
func TestCritical_WebhookAmbiguousAfterRequestSent(t *testing.T) {
	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		received <- struct{}{}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()
	o := post(t, poster(), srv.URL)
	<-received
	if o.Acceptance != core.AcceptanceUnknown || o.Retryable || o.Code != core.ErrCodeAmbiguous {
		t.Fatalf("outcome = %+v", o)
	}
}

// The deadline passes while the server holds the request: ambiguous too.
func TestCritical_WebhookDeadlineAfterRequestSent(t *testing.T) {
	received := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(received)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancelCause(context.Background())
	go func() {
		<-received
		cancel(feather.ErrDeadline)
	}()
	o := poster().Post(ctx, credential.NewSecret(srv.URL), []byte(`{}`))
	if o.Acceptance != core.AcceptanceUnknown || o.Retryable {
		t.Fatalf("outcome = %+v", o)
	}
}

// Before anything is sent, a done context is a certain non-delivery.
func TestCritical_WebhookDeadlineBeforeRequest(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(feather.ErrDeadline)
	o := poster().Post(ctx, credential.NewSecret("http://127.0.0.1:1/x"), []byte(`{}`))
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || o.Code != core.ErrCodeDeadline {
		t.Fatalf("outcome = %+v", o)
	}
	ctx2, cancel2 := context.WithCancelCause(context.Background())
	cancel2(feather.ErrInterrupted)
	if o := poster().Post(ctx2, credential.NewSecret("http://127.0.0.1:1/x"), []byte(`{}`)); o.Code != core.ErrCodeCanceled {
		t.Fatalf("outcome = %+v", o)
	}
}

func closedPortURL(t *testing.T, path string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr + path
}

// A connection that cannot be made sends nothing: retryable, and the error
// does not quote the URL (its path is the webhook's secret).
func TestCritical_WebhookNotSentIsRetryable(t *testing.T) {
	url := closedPortURL(t, "/services/T0001/B0001/SECRETTOKEN")
	o := post(t, poster(), url)
	if o.Acceptance != core.AcceptanceRejected || !o.Retryable || o.Code != core.ErrCodeNetwork {
		t.Fatalf("outcome = %+v", o)
	}
	if strings.Contains(o.Err.Error(), "SECRETTOKEN") || strings.Contains(o.Err.Error(), "T0001") {
		t.Fatalf("the error quotes the URL: %v", o.Err)
	}
}

// An untrusted certificate is not retried: a retry meets the same one.
func TestUntrustedCertificateNotRetried(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	o := post(t, poster(), srv.URL)
	if o.Acceptance != core.AcceptanceRejected || o.Retryable || o.Code != core.ErrCodeProtocol {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
}

func TestRefusalDetailIsShortAndPrintable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte("invalid_payload\x1b[31m" + strings.Repeat("x", 500)))
	}))
	defer srv.Close()
	o := post(t, poster(), srv.URL)
	msg := o.Err.Error()
	if strings.Contains(msg, "\x1b") || len([]rune(msg)) > 230 || !strings.HasPrefix(msg, "HTTP 400: invalid_payload") {
		t.Fatalf("error = %q", msg)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d, ok := webhook.ParseRetryAfter("-3", t0); ok || d != 0 {
		t.Fatal("negative accepted")
	}
	if d, ok := webhook.ParseRetryAfter("999999999", t0); !ok || d != 24*time.Hour {
		t.Fatalf("huge = %s", d)
	}
}
