package commandline_test

import (
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// A project's slack, discord and email feathers send to a destination the
// project chooses: like its http feathers, they reach public addresses
// only, unless the project allows a network or a host by name (shown by
// the trust). A global feather is the user's own: not restricted.
func TestCritical_ProjectWebhookFeathersReachPublicAddressesOnly(t *testing.T) {
	p := newProvider(t, 200, "ok")
	for _, typ := range []string{"slack", "discord"} {
		t.Run(typ, func(t *testing.T) {
			e := newEnv(t)
			e.writeProject(featherConfig("proj", typ, p.URL))
			r := e.must("config", "trust", "--yes")
			if !strings.Contains(r.stdout, "address policy: public addresses only") {
				t.Fatalf("trust output: %s", r.stdout)
			}
			before := p.count()
			r = e.run("send", "--to", "proj", "-o", "json", "hi")
			rep := parseHTTPReport(t, r.stdout)
			if r.code == 0 || p.count() != before || rep.Results[0].Error == nil || rep.Results[0].Error.Code != "blocked" || rep.Results[0].Attempts != 1 {
				t.Fatalf("a loopback webhook of a project was reached: %+v", r)
			}
			// allowed by name, it is reached (and the trust shows it)
			e.writeProject(featherConfig("proj", typ, p.URL) + "allow_networks = [\"127.0.0.1/32\"]\n")
			r = e.must("config", "trust", "--yes")
			if !strings.Contains(r.stdout, "address policy: public addresses; allows networks 127.0.0.1/32") {
				t.Fatalf("trust output: %s", r.stdout)
			}
			e.must("send", "--to", "proj", "hi")
			if p.count() != before+1 {
				t.Fatal("the allowed webhook was not reached")
			}
			// a global feather is the user's own
			e.writeGlobal(featherConfig("mine", typ, p.URL))
			e.must("send", "--to", "mine", "hi")
		})
	}
}

func TestCritical_ProjectEmailReachesPublicAddressesOnly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	var accepted atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	mail := func(host, extra string) string {
		return "[feathers.mail]\ntype = \"email\"\nhost = \"" + host + "\"\nport = " + strconv.Itoa(port) + "\nsecurity = \"tls\"\nfrom = \"a@example.com\"\nto = [\"b@example.com\"]\n" + extra
	}
	e := newEnv(t)
	e.writeProject(mail("127.0.0.1", ""))
	if r := e.must("config", "trust", "--yes"); !strings.Contains(r.stdout, "address policy: public addresses only") {
		t.Fatal(r.stdout)
	}
	r := e.run("send", "--to", "mail", "-o", "json", "hi")
	rep := parseHTTPReport(t, r.stdout)
	if r.code == 0 || accepted.Load() != 0 || rep.Results[0].Error == nil || rep.Results[0].Error.Code != "blocked" || rep.Results[0].Attempts != 1 {
		t.Fatalf("a loopback SMTP server of a project was reached: %+v", r)
	}
	// the metadata address is never allowed, whatever the project says
	e.writeProject(mail("169.254.169.254", "allow_networks = [\"169.254.0.0/16\"]\n"))
	e.must("config", "trust", "--yes")
	r = e.run("send", "--to", "mail", "-o", "json", "hi")
	if rep := parseHTTPReport(t, r.stdout); r.code == 0 || rep.Results[0].Error == nil || rep.Results[0].Error.Code != "blocked" || !strings.Contains(rep.Results[0].Error.Message, "never allowed") {
		t.Fatalf("metadata: %+v", r)
	}
	// allowed by name, it is reached
	e.writeProject(mail("127.0.0.1", "allow_networks = [\"127.0.0.1/32\"]\n"))
	e.must("config", "trust", "--yes")
	_ = e.run("send", "--to", "mail", "hi")
	if accepted.Load() == 0 {
		t.Fatal("the allowed SMTP server was not reached")
	}
}
