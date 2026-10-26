package configure

import (
	"bufio"
	"bytes"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// A static header that passes checkHeaderName and checkHeaderValue is one
// header line on the wire: it cannot inject another header or end the
// header block, and it reads back as the same name and value.
func FuzzHeaderChecks(f *testing.F) {
	f.Add("X-Api-Version", "2024-01-01")
	f.Add("X-Trace", "a\tb")
	f.Add("X-Bad", "a\r\nX-Injected: 1")
	f.Add("X-Bad", "a\nb")
	f.Add("Bad Name", "v")
	f.Add("X-Nul", "a\x00b")
	f.Add("X-Utf8", "caf\u00e9")
	f.Fuzz(func(t *testing.T, name, value string) {
		if checkHeaderName(name) != nil || checkHeaderValue(value) != nil {
			return
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			t.Fatalf("value %q accepted with CR, LF or NUL", value)
		}
		req, err := http.NewRequest("GET", "https://api.example.com/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(name, value)
		var wire bytes.Buffer
		if err := req.Write(&wire); err != nil {
			t.Fatalf("an accepted header does not write: %v", err)
		}
		back, err := http.ReadRequest(bufio.NewReader(&wire))
		if err != nil {
			t.Fatalf("an accepted header %q: %q does not read back: %v", name, value, err)
		}
		extra := 0
		for k := range back.Header {
			if http.CanonicalHeaderKey(k) != http.CanonicalHeaderKey(name) && k != "User-Agent" {
				extra++
			}
		}
		if extra != 0 {
			t.Fatalf("header %q: %q became %d other header(s): %v", name, value, extra, back.Header)
		}
		if got, want := back.Header.Get(name), strings.Trim(value, " \t"); got != want {
			t.Fatalf("header %q reads back %q, want %q", name, got, want)
		}
	})
}

// An accepted plugin name makes one plain file name on every OS: the
// prefix, the name, no separator or parent element, no reserved name.
func FuzzPluginFile(f *testing.F) {
	for _, s := range []string{"pagerduty", "a", "my-plugin_2", "../x", "a/b", `a\b`, "slack", "external", "UPPER", "a.exe", "-x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if ValidPluginName(name) != nil {
			return
		}
		if contains(FeatherTypes, name) || reservedPlugin[name] {
			t.Fatalf("reserved name %q accepted", name)
		}
		for _, goos := range []string{"linux", "darwin", "windows"} {
			file := PluginFile(name, goos)
			if !strings.HasPrefix(file, PluginPrefix) || strings.ContainsAny(file, `/\:`) || strings.Contains(file, "..") {
				t.Fatalf("%s: plugin file %q for %q", goos, file, name)
			}
			if filepath.Base(file) != file {
				t.Fatalf("%s: plugin file %q is not a base name", goos, file)
			}
		}
	})
}

// A trust store either fails to parse, or encodes and parses back to the
// same entries: the paths, hashes, personal secrets and plugins a project
// was trusted with survive kjak rewriting the store.
func FuzzParseTrustStore(f *testing.F) {
	f.Add([]byte(`[[projects]]
path = "/home/u/p/.kjak.toml"
sha256 = "0000000000000000000000000000000000000000000000000000000000000000"
trusted_at = 2026-01-02T03:04:05Z
user_secrets = ["slack.token", "env:API_KEY"]
[[projects.plugins]]
name = "pagerduty"
path = "/usr/local/bin/kjak-feather-pagerduty"
sha256 = "1111111111111111111111111111111111111111111111111111111111111111"
`))
	f.Add([]byte("projects = []\n"))
	f.Add([]byte("[[projects]]\npath = 1\n"))
	f.Fuzz(func(t *testing.T, doc []byte) {
		ts, err := parseTrustStore("trust.toml", doc, true)
		if err != nil {
			return
		}
		b, err := encodeTrustStore(ts)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		back, err := parseTrustStore("trust.toml", b, true)
		if err != nil {
			t.Fatalf("a parsed trust store does not parse back: %v\n%s", err, b)
		}
		if len(back.Projects) != len(ts.Projects) {
			t.Fatalf("%d entries became %d", len(ts.Projects), len(back.Projects))
		}
		for i, e := range ts.Projects {
			g := back.Projects[i]
			if g.Path != e.Path || g.SHA256 != e.SHA256 || g.SecretsPath != e.SecretsPath || g.SecretsSHA256 != e.SecretsSHA256 || !g.TrustedAt.Equal(e.TrustedAt) {
				t.Fatalf("entry %d changed: %+v -> %+v", i, e, g)
			}
			if strings.Join(g.UserSecrets, "\x00") != strings.Join(e.UserSecrets, "\x00") || len(g.Plugins) != len(e.Plugins) {
				t.Fatalf("entry %d grant changed: %+v -> %+v", i, e, g)
			}
			for j, p := range e.Plugins {
				if g.Plugins[j] != p {
					t.Fatalf("entry %d plugin %d changed: %+v -> %+v", i, j, p, g.Plugins[j])
				}
				if q, ok := g.Plugin(p.Name); !ok || q.Name != p.Name {
					t.Fatalf("plugin %q not found after the round trip", p.Name)
				}
			}
			for _, n := range e.UserSecrets {
				if !g.AllowsUserSecret(n) {
					t.Fatalf("user secret %q lost", n)
				}
			}
		}
	})
}
