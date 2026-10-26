package httpapi

import (
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/configure"
)

// origin is the scheme, lower-case host and port (the scheme's default when
// none) of u, computed here independently of hostPort.
func origin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return u.Scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

// The redirect policy of an http feather, for any redirect: past
// max_redirects the last response is used; a redirect with user
// information, or to a scheme other than https (http with allow_http), is
// refused; Referer is never sent on; the credentials (Authorization, Cookie
// and the configured auth header) go to the origin they were configured
// for only, and are kept there.
func FuzzCheckRedirect(f *testing.F) {
	f.Add("https://api.example.com/v1", "https://api.example.com/v2", false, uint8(1), uint8(3))
	f.Add("https://api.example.com/v1", "https://API.example.com:443/v2", false, uint8(1), uint8(3))
	f.Add("https://api.example.com/v1", "https://evil.example.net/", false, uint8(1), uint8(3))
	f.Add("https://api.example.com/v1", "https://api.example.com:8443/", false, uint8(2), uint8(3))
	f.Add("https://api.example.com/v1", "http://api.example.com/", true, uint8(1), uint8(3))
	f.Add("https://api.example.com/v1", "http://api.example.com/", false, uint8(1), uint8(3))
	f.Add("https://api.example.com/v1", "https://u:p@api.example.com/", false, uint8(1), uint8(3))
	f.Add("https://api.example.com/v1", "ftp://api.example.com/", true, uint8(1), uint8(3))
	f.Add("https://api.example.com/v1", "https://api.example.com/", false, uint8(4), uint8(3))
	f.Add("http://127.0.0.1:8080/", "http://127.0.0.1/", true, uint8(1), uint8(0))
	f.Fuzz(func(t *testing.T, from, to string, allowHTTP bool, hops, maxRedirects uint8) {
		first, err := url.Parse(from)
		if err != nil || first.Host == "" {
			return
		}
		next, err := url.Parse(to)
		if err != nil {
			return
		}
		limit := int(maxRedirects % 11)
		n := int(hops%12) + 1
		const authHeader = "X-Api-Key"
		c := newClient(&Guard{}, nil, configure.FeatherConfig{MaxRedirects: limit, AllowHTTP: allowHTTP}, Auth{Header: authHeader})
		via := make([]*http.Request, n)
		for i := range via {
			via[i] = &http.Request{URL: first, Header: http.Header{}}
		}
		req := &http.Request{URL: next, Header: http.Header{
			"Authorization":                        {"Bearer s3cr3t"},
			"Cookie":                               {"session=s3cr3t"},
			http.CanonicalHeaderKey(authHeader):    {"s3cr3t"},
			"Referer":                              {first.String()},
			http.CanonicalHeaderKey("X-Unrelated"): {"kept"},
		}}
		err = c.CheckRedirect(req, via)

		var blocked *BlockedError
		switch {
		case n > limit:
			if !errors.Is(err, http.ErrUseLastResponse) {
				t.Fatalf("hop %d over max_redirects %d: %v", n, limit, err)
			}
			return
		case next.User != nil:
			if !errors.As(err, &blocked) {
				t.Fatalf("redirect to %q with user information: %v", to, err)
			}
			return
		case next.Scheme != "https" && (next.Scheme != "http" || !allowHTTP):
			if !errors.As(err, &blocked) {
				t.Fatalf("redirect to scheme %q (allow_http %v): %v", next.Scheme, allowHTTP, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("redirect to %q refused: %v", to, err)
		}
		if req.Header.Get("Referer") != "" {
			t.Fatal("Referer sent on")
		}
		if req.Header.Get("X-Unrelated") != "kept" {
			t.Fatal("an unrelated header was removed")
		}
		same := origin(next) == origin(first)
		for _, h := range []string{"Authorization", "Cookie", authHeader} {
			if kept := req.Header.Get(h) != ""; kept != same {
				t.Fatalf("%s kept=%v on a redirect from %s to %s (same origin %v)", h, kept, origin(first), origin(next), same)
			}
		}
	})
}

// The address policy for any IPv6 or IPv4 address: what is never allowed
// stays refused even when allow_networks contains every address; a public
// address needs no allowance; allow_hosts only ever opens private addresses;
// a zone or an IPv4-mapped form does not change the decision.
func FuzzGuardAllowed(f *testing.F) {
	for _, s := range []string{
		"169.254.169.254", "::ffff:169.254.169.254", "fd00:ec2::254", "127.0.0.1", "::1", "10.1.2.3",
		"fe80::1", "64:ff9b::a9fe:a9fe", "2002:a9fe:a9fe::1", "2001::1", "93.184.216.34", "2606:2800:220:1::1",
		"0.0.0.0", "::", "ff02::1", "240.0.0.1", "fc00::1",
	} {
		b := netip.MustParseAddr(s).As16()
		f.Add(b[:], "api.example.com", false)
		f.Add(b[:], "api.example.com", true)
	}
	everything := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	f.Fuzz(func(t *testing.T, raw []byte, host string, allowAll bool) {
		var b [16]byte
		copy(b[:], raw)
		a := netip.AddrFrom16(b)
		c := Classify(a)
		if a.Is4In6() && Classify(a.Unmap()) != c {
			t.Fatalf("%s is %s, unmapped %s", a, c, Classify(a.Unmap()))
		}
		if !a.Is4In6() && Classify(a.WithZone("eth0")) != c {
			t.Fatalf("%s: a zone changes the category", a)
		}
		g := &Guard{AllowHosts: []string{host}}
		if allowAll {
			g.AllowNetworks = everything
		}
		err := g.Allowed(host, a)
		if err != nil && !errors.Is(err, ErrBlocked) {
			t.Fatalf("%s: an error that is not ErrBlocked: %v", a, err)
		}
		switch {
		case never(c):
			if err == nil {
				t.Fatalf("%s (%s) allowed", a, c)
			}
		case c == CatPublic:
			if err != nil {
				t.Fatalf("public %s refused: %v", a, err)
			}
		case allowAll:
			if err != nil {
				t.Fatalf("%s (%s) refused with allow_networks of everything: %v", a, c, err)
			}
		case c == CatPrivate:
			if err != nil {
				t.Fatalf("private %s refused for its allowed host: %v", a, err)
			}
		default:
			if err == nil {
				t.Fatalf("%s (%s) allowed by allow_hosts", a, c)
			}
		}
		for _, m := range metadata {
			if a.Unmap() == m && err == nil {
				t.Fatalf("metadata address %s allowed", a)
			}
		}
	})
}
