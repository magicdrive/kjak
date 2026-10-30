package builtin

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather/discord"
	"github.com/magicdrive/kjak/internal/feather/email"
	"github.com/magicdrive/kjak/internal/feather/slack"
)

func strp(s string) *string { return &s }

func deps(env map[string]string) Deps {
	return Deps{
		Resolver: credential.NewResolver(func(k string) (string, bool) { v, ok := env[k]; return v, ok }, nil),
		Redactor: &credential.Redactor{},
		Clock:    clock.Real{},
	}
}

func TestBuildEachType(t *testing.T) {
	d := deps(map[string]string{"HOOK": "https://hooks.example/abc/def", "PASS": "pw-secret"})
	f, err := Build(context.Background(), "s", configure.FeatherConfig{Type: "slack", WebhookURL: &credential.Ref{Env: strp("HOOK")}}, d)
	if _, ok := f.(*slack.Feather); !ok || err != nil {
		t.Fatalf("slack: %T %v", f, err)
	}
	f, err = Build(context.Background(), "d", configure.FeatherConfig{Type: "discord", Username: "u", WebhookURL: &credential.Ref{Env: strp("HOOK")}}, d)
	if df, ok := f.(*discord.Feather); !ok || err != nil || df.Username != "u" {
		t.Fatalf("discord: %T %v", f, err)
	}
	f, err = Build(context.Background(), "e", configure.FeatherConfig{Type: "email", Host: "h", User: "u", Password: &credential.Ref{Env: strp("PASS")}, From: "a@example.com", To: []string{"b@example.com"}}, d)
	ef, ok := f.(*email.Feather)
	if !ok || err != nil || ef.Security != email.SecuritySTARTTLS || ef.Password.Reveal() != "pw-secret" {
		t.Fatalf("email: %T %v", f, err)
	}
	// every resolved secret is known to the redactor
	if got := d.Redactor.Redact("x https://hooks.example/abc/def pw-secret"); strings.Contains(got, "abc/def") || strings.Contains(got, "pw-secret") {
		t.Fatalf("redacted = %q", got)
	}
	// and so are the forms AUTH sends, which a server may echo after AUTH
	plain, login := email.PlainToken("u", "pw-secret"), base64.StdEncoding.EncodeToString([]byte("pw-secret"))
	if got := d.Redactor.Redact("550 you sent " + plain + " and " + login); strings.Contains(got, plain) || strings.Contains(got, login) {
		t.Fatalf("redacted = %q", got)
	}
}

func TestBuildPreflightErrors(t *testing.T) {
	d := deps(map[string]string{"NOTURL": "just-text-secret"})
	_, err := Build(context.Background(), "s", configure.FeatherConfig{Type: "slack", WebhookURL: &credential.Ref{Env: strp("MISSING")}}, d)
	if !errors.Is(err, credential.ErrCredential) {
		t.Fatalf("missing env: %v", err)
	}
	_, err = Build(context.Background(), "s", configure.FeatherConfig{Type: "slack", WebhookURL: &credential.Ref{Env: strp("NOTURL")}}, d)
	if err == nil || strings.Contains(err.Error(), "just-text-secret") {
		t.Fatalf("not a URL: %v", err)
	}
	if _, err := Build(context.Background(), "o", configure.FeatherConfig{Type: "native"}, d); err == nil || !strings.Contains(err.Error(), `type = "os"`) {
		t.Fatalf("native: %v", err)
	}
	// an os feather needs nothing to resolve; the backend is the OS's
	if f, err := Build(context.Background(), "o", configure.FeatherConfig{Type: "os"}, d); err != nil || f == nil {
		t.Fatalf("os: %v", err)
	}
	_, err = Build(context.Background(), "k", configure.FeatherConfig{Type: "slack", WebhookURL: &credential.Ref{Keychain: &credential.KeychainRef{Service: "s", Account: "a"}}}, d)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("keychain without backend: %v", err)
	}
}
