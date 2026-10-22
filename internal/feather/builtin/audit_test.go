package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/credential"
)

// A webhook URL is its secret: it travels over HTTPS, except to this machine
// (local relays and tests).
func TestWebhookURLMustBeHTTPS(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://hooks.slack.com/services/T/B/X": true,
		"http://127.0.0.1:8080/hook":             true,
		"http://localhost:8080/hook":             true,
		"http://[::1]:8080/hook":                 true,
		"http://hooks.slack.com/services/T/B/X":  false,
		"http://10.0.0.1/hook":                   false,
		"ftp://hooks.example/x":                  false,
		"https://user:pass@hooks.example/x":      false,
	} {
		d := deps(map[string]string{"HOOK": url})
		_, err := Build(context.Background(), "s", configure.FeatherConfig{Type: "slack", WebhookURL: &credential.Ref{Env: strp("HOOK")}}, d)
		if (err == nil) != ok {
			t.Errorf("%s: err = %v", url, err)
		}
		if err != nil && strings.Contains(err.Error(), "hooks.slack.com/services") {
			t.Errorf("the error quotes the URL: %v", err)
		}
	}
	// a literal is checked when the configuration is read
	if err := configure.ValidateFeather(configure.FeatherConfig{Type: "discord", WebhookURL: &credential.Ref{Literal: strp("http://discord.com/api/webhooks/1/x")}}); err == nil {
		t.Fatal("a plain-http literal was accepted")
	}
}
