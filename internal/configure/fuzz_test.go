package configure

import (
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/credential"
)

// Any document either fails, or parses into a configuration that encodes
// and parses back to the same settings and names.
func FuzzParse(f *testing.F) {
	f.Add([]byte(globalTOML))
	f.Add([]byte(projectTOML))
	f.Add([]byte("[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"X\" }\n"))
	f.Fuzz(func(t *testing.T, doc []byte) {
		cfg, err := Parse(doc)
		if err != nil {
			return
		}
		b, err := Encode(cfg)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		back, err := Parse(b)
		if err != nil {
			t.Fatalf("a parsed configuration does not parse back: %v\n%s", err, b)
		}
		if len(back.Feathers) != len(cfg.Feathers) || len(back.Groups) != len(cfg.Groups) || len(back.Templates) != len(cfg.Templates) {
			t.Fatal("names lost in the round trip")
		}
		if back.Settings().Timeout != cfg.Settings().Timeout || back.Settings().MaxRetries != cfg.Settings().MaxRetries {
			t.Fatal("settings changed in the round trip")
		}
	})
}

// An accepted webhook URL is https, or http to this machine.
func FuzzCheckWebhookURL(f *testing.F) {
	for _, s := range []string{"https://hooks.slack.com/x", "http://127.0.0.1:1/x", "http://evil/x", "https://u:p@h/x", "//x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if CheckWebhookURL(s) != nil {
			return
		}
		lower := strings.ToLower(s)
		if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
			t.Fatalf("accepted %q", s)
		}
	})
}

// A secrets document either fails without quoting a value, or every name
// it yields is a valid secret name that encodes and parses back.
func FuzzParseSecrets(f *testing.F) {
	f.Add([]byte("[slack]\nbot_token = \"xoxb-1\"\n"))
	f.Add([]byte("a = \"b\"\n[c.d]\ne = \"f\"\n"))
	f.Add([]byte("\"a.b\" = \"zzzzzz\"\n"))
	f.Fuzz(func(t *testing.T, doc []byte) {
		values, err := ParseSecrets(doc)
		if err != nil {
			for _, line := range strings.Split(string(doc), "\n") {
				if _, v, ok := strings.Cut(line, "= \""); ok && len(v) > 12 && strings.Contains(err.Error(), strings.TrimSuffix(v, "\"")) {
					t.Fatalf("the error quotes a value: %v", err)
				}
			}
			return
		}
		for name := range values {
			if credential.ValidSecretName(name) != nil {
				t.Fatalf("invalid name %q accepted", name)
			}
		}
		b, err := EncodeSecrets(values)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		back, err := ParseSecrets(b)
		if err != nil || len(back) != len(values) {
			t.Fatalf("round trip: %v", err)
		}
		for k, v := range values {
			if back[k] != v {
				t.Fatalf("%s changed in the round trip", k)
			}
		}
	})
}

// An accepted http feather URL is https (or http with the opt-in), has a
// host and carries no user information.
func FuzzCheckHTTPURL(f *testing.F) {
	for _, s := range []string{"https://api.example.com/x?a=1", "http://10.0.0.1:8080/", "https://u:p@h/", "unix:///x", "https://[::1]:443/", "//x"} {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, s string, allowHTTP bool) {
		u, err := CheckHTTPURL(s, allowHTTP)
		if err != nil {
			if strings.Contains(err.Error(), s) && len(s) > 8 {
				t.Fatalf("the error quotes the URL: %v", err)
			}
			return
		}
		if u.User != nil || u.Hostname() == "" || (u.Scheme != "https" && !(allowHTTP && u.Scheme == "http")) {
			t.Fatalf("accepted %q", s)
		}
	})
}
