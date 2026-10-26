package commandline_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// leaks reports the pieces of secret (any 6 consecutive characters of a
// line of it) found in out.
func leaks(out, secret string) []string {
	var found []string
	for _, line := range strings.Split(secret, "\n") {
		for i := 0; i+6 <= len(line); i++ {
			if p := line[i : i+6]; strings.Contains(out, p) {
				found = append(found, p)
			}
		}
	}
	return found
}

// An API that echoes the token past the point where kjak cuts its error
// text: the cut must not leave a prefix of the token in the report.
func TestCritical_TruncatedResponseDoesNotLeakATokenPrefix(t *testing.T) {
	const token = "tkn-9f8e7d6c5b4a39281706"
	for _, at := range []int{180, 190, 195, 199} {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(400)
				fmt.Fprint(w, strings.Repeat("x", at)+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")+" was refused")
			}))
			defer srv.Close()
			e := newEnv(t)
			e.vars["API_TOKEN"] = token
			e.writeGlobal(httpFeather("api", srv.URL, "[feathers.api.auth]\ntype = \"bearer\"\ntoken = { env = \"API_TOKEN\" }"))
			for _, format := range []string{"text", "json"} {
				r := e.run("send", "--to", "api", "-o", format, "hi")
				if r.code == 0 {
					t.Fatalf("%+v", r)
				}
				if l := leaks(r.stdout+r.stderr, token); len(l) > 0 {
					t.Fatalf("%s: the token leaks (%q):\n%s%s", format, l, r.stdout, r.stderr)
				}
			}
		})
	}
}

// A secret with line breaks (a PEM key) that a plugin echoes on stderr and
// in its error message: kjak makes the text single-line before it prints
// it, which must not turn the secret into a form the redaction misses.
func TestCritical_MultiLineSecretEchoedByAPluginIsRedacted(t *testing.T) {
	const pem = "-----BEGIN TEST KEY-----\nMIIEpAIBAAKCAQEA1x2y3z4\nQWERTYuiopASDFghjkZXCV\n-----END TEST KEY-----"
	e := newEnv(t)
	e.installPlugin(e.pluginDir(), "pager")
	e.vars["PATH"] = e.pluginDir()
	e.vars["PEM_KEY"] = pem
	e.writeGlobal(externalFeather("pd", "pager", "settings = { mode = \"echo-secret\" }\nsecrets = { key = { env = \"PEM_KEY\" } }"))
	for _, format := range []string{"text", "json"} {
		r := e.run("send", "--to", "pd", "-o", format, "hi")
		if r.code == 0 {
			t.Fatalf("%+v", r)
		}
		if l := leaks(r.stdout+r.stderr, pem); len(l) > 0 {
			t.Fatalf("%s: the key leaks (%q):\n%s%s", format, l, r.stdout, r.stderr)
		}
		if !strings.Contains(r.stdout+r.stderr, "[REDACTED]") {
			t.Fatalf("%s: no redaction:\n%s%s", format, r.stdout, r.stderr)
		}
	}
}
