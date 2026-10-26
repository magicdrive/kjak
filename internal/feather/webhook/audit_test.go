package webhook_test

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magicdrive/kjak/internal/core"
)

// The classification (acceptance after the request was written) is the one
// of HTTP/1.1, which the tests exercise. The client must not switch to
// HTTP/2 against a server that offers it (Slack and Discord do).
func TestCritical_WebhookClientUsesHTTP1(t *testing.T) {
	var proto int
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto = r.ProtoMajor
		_, _ = w.Write([]byte("ok"))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	p := poster()
	p.Client.Transport.(*http.Transport).TLSClientConfig.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	if o := post(t, p, srv.URL); o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
	if proto != 1 {
		t.Fatalf("the request used HTTP/%d", proto)
	}
}

// Over TLS too, a connection lost after the request was read is unknown.
func TestAmbiguousOverTLS(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if c, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = c.Close()
		}
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	p := poster()
	p.Client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs, MinVersion: tls.VersionTLS12}
	o := post(t, p, srv.URL)
	if o.Acceptance != core.AcceptanceUnknown || o.Retryable {
		t.Fatalf("outcome = %+v (%v)", o, o.Err)
	}
}
