package httpapi

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"syscall"

	"github.com/magicdrive/kjak/internal/feather"
)

// The SSRF policy of an http feather. A URL in a configuration (a project
// configuration among them) must not turn kjak into a way to reach what only
// this machine can reach: its own services, the cloud metadata endpoint,
// the private network. Every connection is checked where it is made:
//
//   - the host is resolved once by kjak, each address is checked, and the
//     connection goes to a checked address (no second lookup: a DNS answer
//     that changes between the check and the connection — DNS rebinding —
//     is never used);
//   - the dialer's Control checks the address of the socket again, just
//     before connect(2);
//   - only TCP: no Unix socket, whatever the URL;
//   - no proxy: HTTP_PROXY / HTTPS_PROXY / NO_PROXY are ignored (a proxy
//     would make the checked address the proxy's, not the destination's).

// Category is the class of an address for the policy.
type Category string

const (
	CatPublic    Category = "public"
	CatLoopback  Category = "loopback"
	CatLinkLocal Category = "link-local"
	CatPrivate   Category = "private"
	CatTunnel    Category = "IPv6 transition"
	// never allowed, whatever the configuration says
	CatMetadata    Category = "cloud metadata"
	CatUnspecified Category = "unspecified"
	CatMulticast   Category = "multicast"
	CatReserved    Category = "reserved"
)

// metadata are the instance metadata endpoints of cloud providers (AWS,
// GCP, Azure, OpenStack: 169.254.169.254; AWS IPv6: fd00:ec2::254; ECS task
// metadata: 169.254.170.2; Alibaba: 100.100.100.200; Oracle: 192.0.0.192).
var metadata = []netip.Addr{
	netip.MustParseAddr("169.254.169.254"),
	netip.MustParseAddr("169.254.170.2"),
	netip.MustParseAddr("169.254.170.23"),
	netip.MustParseAddr("100.100.100.200"),
	netip.MustParseAddr("192.0.0.192"),
	netip.MustParseAddr("fd00:ec2::254"),
	netip.MustParseAddr("fd00:ec2::23"),
	netip.MustParseAddr("fd20:ce::254"), // GCP IPv6
}

func prefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

var (
	reserved4 = prefixes("0.0.0.0/8", "240.0.0.0/4", "255.255.255.255/32", "192.88.99.0/24" /* 6to4 relay anycast */)
	private4  = prefixes(
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", // RFC 1918
		"100.64.0.0/10",   // carrier-grade NAT (RFC 6598)
		"192.0.0.0/24",    // IETF protocol assignments
		"198.18.0.0/15",   // benchmarking
		"192.0.2.0/24",    // documentation
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
	)
	private6 = prefixes("fc00::/7", "fec0::/10", "2001:db8::/32", "100::/64")
	// IPv4-compatible IPv6 (deprecated, RFC 4291 2.5.5.1): never a
	// destination
	compat4 = netip.MustParsePrefix("::/96")
	nat64   = prefixes("64:ff9b::/96")
	// local-use NAT64 (RFC 8215): the IPv4 address is not at a fixed place
	nat64Local = netip.MustParsePrefix("64:ff9b:1::/48")
	// IPv4-translated (RFC 2765, ::ffff:0:0:0/96)
	translated4 = netip.MustParsePrefix("::ffff:0:0:0/96")
	sixToFour   = netip.MustParsePrefix("2002::/16")
	teredo      = netip.MustParsePrefix("2001::/32")
)

// Classify tells the category of an address. An IPv4-mapped, NAT64 or 6to4
// address is classified by the IPv4 address it carries (the worse of the
// two).
func Classify(a netip.Addr) Category {
	a = a.WithZone("")
	if a.Is4In6() {
		return Classify(a.Unmap())
	}
	for _, m := range metadata {
		if a == m {
			return CatMetadata
		}
	}
	switch {
	case a.IsUnspecified():
		return CatUnspecified
	case a.IsMulticast():
		return CatMulticast
	case a.IsLoopback():
		return CatLoopback
	case a.IsLinkLocalUnicast():
		return CatLinkLocal
	}
	if a.Is4() {
		for _, p := range reserved4 {
			if p.Contains(a) {
				return CatReserved
			}
		}
		for _, p := range private4 {
			if p.Contains(a) {
				return CatPrivate
			}
		}
		return CatPublic
	}
	// IPv6 that carries an IPv4 address
	b := a.As16()
	for _, p := range nat64 {
		if p.Contains(a) {
			return worse(CatTunnel, Classify(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})))
		}
	}
	if translated4.Contains(a) {
		return worse(CatTunnel, Classify(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})))
	}
	if nat64Local.Contains(a) {
		return CatTunnel
	}
	if sixToFour.Contains(a) {
		return worse(CatTunnel, Classify(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]})))
	}
	if teredo.Contains(a) {
		return CatTunnel
	}
	if compat4.Contains(a) {
		return CatReserved
	}
	for _, p := range private6 {
		if p.Contains(a) {
			return CatPrivate
		}
	}
	if !a.IsGlobalUnicast() {
		return CatReserved
	}
	return CatPublic
}

// rank orders the categories from the most to the least allowed.
func rank(c Category) int {
	switch c {
	case CatPublic:
		return 0
	case CatPrivate:
		return 1
	case CatTunnel:
		return 2
	case CatLinkLocal, CatLoopback:
		return 3
	}
	return 4
}

func worse(a, b Category) Category {
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// never: categories no configuration can allow.
func never(c Category) bool { return rank(c) >= 4 }

// Guard is the address policy of one http feather.
type Guard struct {
	// AllowNetworks allow the addresses they contain (loopback, link-local,
	// private, IPv6 transition), never metadata, unspecified, multicast or
	// reserved ones.
	AllowNetworks []netip.Prefix
	// AllowHosts allow private addresses (not loopback or link-local) for
	// these host names.
	AllowHosts []string
	// Resolver resolves host names (net.DefaultResolver when nil).
	Resolver Resolver
	// dial connects to one checked address (tests replace it).
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Resolver resolves a host name to addresses (*net.Resolver).
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// BlockedError: the policy refused every address of a host (the request
// was not sent). It is feather.BlockedError, which every feather that
// connects through a Guard reports as "blocked".
type BlockedError = feather.BlockedError

// ErrBlocked is the error of a refused address or redirect.
var ErrBlocked = feather.ErrBlocked

// Allowed checks one address of host.
func (g *Guard) Allowed(host string, a netip.Addr) error {
	c := Classify(a)
	if c == CatPublic {
		return nil
	}
	shown := a.WithZone("").String()
	if never(c) {
		return &BlockedError{Reason: fmt.Sprintf("address %s is %s: never allowed", shown, c)}
	}
	plain := a.WithZone("").Unmap()
	for _, p := range g.AllowNetworks {
		if p.Contains(plain) || p.Contains(a.WithZone("")) {
			return nil
		}
	}
	if c == CatPrivate {
		h := strings.TrimSuffix(strings.ToLower(host), ".")
		for _, allowed := range g.AllowHosts {
			if h == strings.TrimSuffix(strings.ToLower(allowed), ".") {
				return nil
			}
		}
		return &BlockedError{Reason: fmt.Sprintf("address %s is %s: allow it with allow_networks or allow_hosts", shown, c)}
	}
	return &BlockedError{Reason: fmt.Sprintf("address %s is %s: allow it with allow_networks", shown, c)}
}

// DialContext resolves addr's host, checks every address and connects to
// the first allowed one that answers. It is the transport's only way out.
func (g *Guard) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, &BlockedError{Reason: "only TCP connections are made"}
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else {
		r := g.Resolver
		if r == nil {
			r = net.DefaultResolver
		}
		if addrs, err = r.LookupNetIP(ctx, "ip", host); err != nil {
			return nil, err
		}
		if len(addrs) == 0 {
			return nil, &net.DNSError{Err: "no address", Name: host, IsNotFound: true}
		}
	}
	var firstBlock error
	var allowed []netip.Addr
	for _, a := range addrs {
		if err := g.Allowed(host, a); err != nil {
			if firstBlock == nil {
				firstBlock = err
			}
			continue
		}
		allowed = append(allowed, a)
	}
	if len(allowed) == 0 {
		return nil, firstBlock
	}
	dial := g.dial
	if dial == nil {
		d := &net.Dialer{Control: g.control(host)}
		dial = d.DialContext
	}
	var lastErr error
	for _, a := range allowed {
		conn, err := dial(ctx, network, net.JoinHostPort(a.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

// control checks the address of the socket just before it connects.
func (g *Guard) control(host string) func(network, address string, _ syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		switch network {
		case "tcp4", "tcp6":
		default:
			return &BlockedError{Reason: "only TCP connections are made"}
		}
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return &BlockedError{Reason: "unexpected socket address"}
		}
		return g.Allowed(host, ap.Addr())
	}
}
