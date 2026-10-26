package httpapi

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/magicdrive/kjak/internal/core"
)

func TestClassify(t *testing.T) {
	cases := map[string]Category{
		"93.184.216.34":            CatPublic,
		"2606:2800:220:1::1":       CatPublic,
		"127.0.0.1":                CatLoopback,
		"127.255.0.9":              CatLoopback,
		"::1":                      CatLoopback,
		"::ffff:127.0.0.1":         CatLoopback,
		"169.254.1.1":              CatLinkLocal,
		"fe80::1":                  CatLinkLocal,
		"fe80::1%eth0":             CatLinkLocal,
		"169.254.169.254":          CatMetadata,
		"::ffff:169.254.169.254":   CatMetadata,
		"fd00:ec2::254":            CatMetadata,
		"100.100.100.200":          CatMetadata,
		"169.254.170.2":            CatMetadata,
		"0.0.0.0":                  CatUnspecified,
		"::":                       CatUnspecified,
		"0.1.2.3":                  CatReserved,
		"255.255.255.255":          CatReserved,
		"240.0.0.1":                CatReserved,
		"224.0.0.1":                CatMulticast,
		"ff02::1":                  CatMulticast,
		"10.1.2.3":                 CatPrivate,
		"172.16.0.1":               CatPrivate,
		"172.31.255.255":           CatPrivate,
		"172.32.0.1":               CatPublic,
		"192.168.1.1":              CatPrivate,
		"100.64.0.1":               CatPrivate,
		"fd12:3456::1":             CatPrivate,
		"::ffff:10.0.0.1":          CatPrivate,
		"64:ff9b::7f00:1":          CatLoopback, // NAT64 of 127.0.0.1
		"64:ff9b::a9fe:a9fe":       CatMetadata, // NAT64 of 169.254.169.254
		"64:ff9b::5db8:d822":       CatTunnel,   // NAT64 of a public address
		"2002:7f00:1::":            CatLoopback, // 6to4 of 127.0.0.1
		"2002:a9fe:a9fe::1":        CatMetadata, // 6to4 of 169.254.169.254
		"2001:0:4136:e378::1":      CatTunnel,   // Teredo
		"::7f00:1":                 CatReserved, // IPv4-compatible
		"2001:db8::1":              CatPrivate,  // documentation
		"::ffff:0.0.0.0":           CatUnspecified,
		"::ffff:255.255.255.255":   CatReserved,
		"::ffff:100.100.100.200":   CatMetadata,
		"fd00:ec2:0:0:0:0:0:254":   CatMetadata,
		"FD00:EC2::254":            CatMetadata,
		"::ffff:7f00:1":            CatLoopback,
		"0:0:0:0:0:ffff:7f00:0001": CatLoopback,
	}
	for s, want := range cases {
		a, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if got := Classify(a); got != want {
			t.Errorf("%s: %s, want %s", s, got, want)
		}
	}
}

func TestGuardAllowed(t *testing.T) {
	g := &Guard{
		AllowNetworks: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("0.0.0.0/0")},
		AllowHosts:    []string{"intranet.example.com."},
	}
	ok := func(host, addr string) error { return g.Allowed(host, netip.MustParseAddr(addr)) }
	if err := ok("x", "127.0.0.1"); err != nil {
		t.Errorf("allowed loopback: %v", err)
	}
	if err := ok("x", "::ffff:127.0.0.1"); err != nil {
		t.Errorf("allowed mapped loopback: %v", err)
	}
	// never, whatever the networks say
	for _, a := range []string{"169.254.169.254", "0.0.0.0", "224.0.0.1", "255.255.255.255", "fd00:ec2::254"} {
		if err := ok("x", a); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: %v", a, err)
		}
	}
	// a host name allows private addresses only
	g = &Guard{AllowHosts: []string{"intranet.example.com"}}
	if err := ok("INTRANET.example.com.", "10.0.0.1"); err != nil {
		t.Errorf("allowed host: %v", err)
	}
	for _, a := range []string{"127.0.0.1", "169.254.1.1", "::1"} {
		if err := ok("intranet.example.com", a); !errors.Is(err, ErrBlocked) {
			t.Errorf("allow_hosts allowed %s: %v", a, err)
		}
	}
	if err := ok("other.example.com", "10.0.0.1"); !errors.Is(err, ErrBlocked) {
		t.Errorf("another host: %v", err)
	}
}

// fakeResolver answers from a table; answers can change per call.
type fakeResolver struct {
	mu    sync.Mutex
	calls int
	fn    func(host string, call int) []string
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	r.calls++
	n := r.calls
	r.mu.Unlock()
	var out []netip.Addr
	for _, s := range r.fn(host, n) {
		out = append(out, netip.MustParseAddr(s))
	}
	if len(out) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return out, nil
}

// dialRecorder records the addresses a guard connects to (and connects to
// none).
type dialRecorder struct {
	mu   sync.Mutex
	addr []string
}

var errDialed = errors.New("test: dialed")

func (d *dialRecorder) dial(_ context.Context, _, addr string) (net.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.addr = append(d.addr, addr)
	return nil, errDialed
}

func (d *dialRecorder) dialed() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.addr...)
}

// The guard connects only to an address it checked, from the one lookup it
// made: an answer that changes between a check and a connection (DNS
// rebinding) is never used, and blocked addresses of a name are skipped.
func TestCritical_GuardDNSRebinding(t *testing.T) {
	res := &fakeResolver{fn: func(_ string, call int) []string {
		if call%2 == 0 {
			return []string{"127.0.0.1"}
		}
		return []string{"93.184.216.34", "169.254.169.254", "10.0.0.1"}
	}}
	rec := &dialRecorder{}
	g := &Guard{Resolver: res, dial: rec.dial}
	for i := 0; i < 6; i++ {
		_, err := g.DialContext(context.Background(), "tcp", "rebind.example:443")
		if err == nil {
			t.Fatal("connected")
		}
	}
	for _, a := range rec.dialed() {
		if a != "93.184.216.34:443" {
			t.Fatalf("dialed %s", a)
		}
	}
	if len(rec.dialed()) != 3 {
		t.Fatalf("dialed %v", rec.dialed())
	}
}

func TestGuardRefusesNonTCPAndChecksTheSocket(t *testing.T) {
	g := &Guard{}
	if _, err := g.DialContext(context.Background(), "unix", "/var/run/docker.sock"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("unix: %v", err)
	}
	ctl := g.control("x")
	if err := ctl("tcp4", "127.0.0.1:80", nil); !errors.Is(err, ErrBlocked) {
		t.Fatalf("control loopback: %v", err)
	}
	if err := ctl("tcp6", "[fd00:ec2::254]:80", nil); !errors.Is(err, ErrBlocked) {
		t.Fatalf("control metadata: %v", err)
	}
	if err := ctl("tcp4", "93.184.216.34:443", nil); err != nil {
		t.Fatalf("control public: %v", err)
	}
	if err := ctl("unix", "/x", nil); !errors.Is(err, ErrBlocked) {
		t.Fatalf("control unix: %v", err)
	}
	// a literal address is checked as it is, without a lookup
	if _, err := g.DialContext(context.Background(), "tcp", "[::ffff:169.254.169.254]:80"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("mapped metadata literal: %v", err)
	}
}

// Every request of an http feather goes through the guard: a URL to a
// blocked address is refused before anything is sent, with acceptance
// rejected and the code blocked, never retried.
func TestCritical_SSRFRefusedBeforeSending(t *testing.T) {
	names := map[string][]string{
		"loopback.test":  {"127.0.0.1"},
		"metadata.test":  {"169.254.169.254"},
		"private.test":   {"10.0.0.7"},
		"v6.test":        {"::1"},
		"mapped.test":    {"::ffff:127.0.0.1"},
		"linklocal.test": {"fe80::1"},
		"aws6.test":      {"fd00:ec2::254"},
		"zero.test":      {"0.0.0.0"},
	}
	res := &fakeResolver{fn: func(host string, _ int) []string { return names[host] }}
	for host := range names {
		rec := &dialRecorder{}
		f := mustFeather(t, testConfig("https://"+host+"/x"), Options{Resolver: res, dial: rec.dial})
		o := f.Deliver(context.Background(), msg())
		if o.Acceptance != core.AcceptanceRejected || o.Code != core.ErrCodeBlocked || o.Retryable {
			t.Errorf("%s: %+v", host, o)
		}
		if len(rec.dialed()) != 0 {
			t.Errorf("%s: dialed %v", host, rec.dialed())
		}
	}
	for _, u := range []string{"https://127.0.0.1/x", "https://[::1]/x", "https://169.254.169.254/latest/meta-data", "https://[::ffff:a9fe:a9fe]/x", "https://0x7f000001/x"} {
		rec := &dialRecorder{}
		// a resolver that reads 0x7f000001 as an address (inet_aton does)
		hex := &fakeResolver{fn: func(h string, _ int) []string { return map[string][]string{"0x7f000001": {"127.0.0.1"}}[h] }}
		f := mustFeather(t, testConfig(u), Options{Resolver: hex, dial: rec.dial})
		o := f.Deliver(context.Background(), msg())
		if o.Acceptance != core.AcceptanceRejected || o.Code != core.ErrCodeBlocked || o.Retryable || len(rec.dialed()) != 0 {
			t.Errorf("%s: %+v dialed %v", u, o, rec.dialed())
		}
		if strings.Contains(o.Err.Error(), "/x") || strings.Contains(o.Err.Error(), "meta-data") {
			t.Errorf("%s: the error quotes the URL: %v", u, o.Err)
		}
	}
}

// An address and its IPv4-mapped form are the same category; no address
// that carries a blocked IPv4 address is public.
func FuzzClassify(f *testing.F) {
	for _, s := range []string{"127.0.0.1", "169.254.169.254", "10.0.0.1", "93.184.216.34", "0.0.0.0"} {
		a := netip.MustParseAddr(s).As4()
		f.Add(a[0], a[1], a[2], a[3])
	}
	f.Fuzz(func(t *testing.T, b0, b1, b2, b3 byte) {
		b := [4]byte{b0, b1, b2, b3}
		v4 := netip.AddrFrom4(b)
		c := Classify(v4)
		if m := Classify(netip.AddrFrom16(v4.As16())); m != c {
			t.Fatalf("%s is %s, mapped %s", v4, c, m)
		}
		var nat [16]byte
		copy(nat[:], netip.MustParseAddr("64:ff9b::").AsSlice())
		copy(nat[12:], b[:])
		var six [16]byte
		six[0], six[1] = 0x20, 0x02
		copy(six[2:], b[:])
		for _, a := range []netip.Addr{netip.AddrFrom16(nat), netip.AddrFrom16(six)} {
			if c != CatPublic && Classify(a) == CatPublic {
				t.Fatalf("%s carries %s (%s) but is public", a, v4, c)
			}
		}
	})
}
