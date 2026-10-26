package configure

import (
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/credential"
)

// Free-text fields refuse control characters (a terminal escape sequence,
// a line break): kjak prints them, or puts them in a message header.
func TestFreeTextRefusesControlCharacters(t *testing.T) {
	hook := "HOOK"
	for _, bad := range []string{"\x1b[2J", "\a", "\u009b31m", "a\x7fb", "a\tb"} {
		for name, fc := range map[string]FeatherConfig{
			"host":     {Type: TypeEmail, Host: "smtp" + bad, From: "a@example.com", To: []string{"b@example.com"}},
			"subject":  {Type: TypeEmail, Host: "smtp", SubjectPrefix: bad, From: "a@example.com", To: []string{"b@example.com"}},
			"setting":  {Type: TypeExternal, Plugin: "p", Settings: map[string]string{"k": "v" + bad}},
			"username": {Type: TypeDiscord, Username: bad, WebhookURL: &credential.Ref{Env: &hook}},
			"keychain": {Type: TypeSlack, WebhookURL: &credential.Ref{Keychain: &credential.KeychainRef{Service: "s", Account: "a" + bad}}},
		} {
			if err := ValidateFeather(fc); err == nil || !strings.Contains(err.Error(), "control character") {
				t.Errorf("%s %q: %v", name, bad, err)
			}
		}
	}
	ok := FeatherConfig{Type: TypeExternal, Plugin: "p", Settings: map[string]string{"k": "a value, with ünïcode"}}
	if err := ValidateFeather(ok); err != nil {
		t.Fatal(err)
	}
}
