package webhook

import (
	"context"
	"net"
	"net/http"
	"testing"

	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
)

// A guarded client connects only through its dialer: no proxy from the
// environment (the policy would check the proxy's address), and a refusal
// of the policy is "blocked", not retried.
func TestGuardedClientUsesNoProxyAndReportsBlocked(t *testing.T) {
	refuse := func(context.Context, string, string) (net.Conn, error) {
		return nil, &feather.BlockedError{Reason: "address 127.0.0.1 is loopback: allow it with allow_networks"}
	}
	c := NewGuardedClient(refuse)
	if tr := c.Transport.(*http.Transport); tr.Proxy != nil || tr.DialContext == nil {
		t.Fatal("a guarded client must not use a proxy")
	}
	if tr := NewClient().Transport.(*http.Transport); tr.Proxy == nil {
		t.Fatal("the plain client keeps the proxies of the environment")
	}
	p := &Poster{Client: c}
	o := p.Post(context.Background(), credential.NewSecret("https://hooks.example/T/B/SECRET"), []byte("{}"))
	if o.Code != core.ErrCodeBlocked || o.Retryable || o.Acceptance != core.AcceptanceRejected || o.Err == nil {
		t.Fatalf("%+v", o)
	}
	if s := o.Err.Error(); s != "the request was not sent: address 127.0.0.1 is loopback: allow it with allow_networks" {
		t.Fatalf("%q", s)
	}
}
