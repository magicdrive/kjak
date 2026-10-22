package configure

import (
	"strings"
	"testing"
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
