package commandline_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
)

// Every feather of a project sends to a destination the project chooses:
// an email feather to its host, slack and discord to their webhook URL.
// Their env and keychain references are allowed by name at the trust, like
// those of the http feathers; a trust --yes alone does not hand a variable
// of the user to an SMTP server of the project's choosing.
func TestCritical_ProjectEmailEnvSecretNeedsAGrant(t *testing.T) {
	e := newEnv(t)
	e.vars["GITHUB_TOKEN"] = "ghp-USER-SECRET"
	e.writeProject(`[feathers.mail]
type = "email"
host = "127.0.0.1"
port = 1
security = "tls"
user = "u"
password = { env = "GITHUB_TOKEN" }
from = "a@example.com"
to = ["b@example.com"]
`)
	r := e.run("config", "trust", "--yes")
	if r.code != 2 || !strings.Contains(r.stdout, "env:GITHUB_TOKEN") || !strings.Contains(r.stderr, "--allow-user-secrets") {
		t.Fatalf("trust --yes without --allow-user-secrets: %+v", r)
	}
	// a trust that does not name it (one made by an earlier kjak)
	trustPlain(t, e)
	r = e.run("send", "--to", "mail", "hi")
	if r.code != 2 || !strings.Contains(r.stderr, "does not allow") || !strings.Contains(r.stderr, "env:GITHUB_TOKEN") || !strings.Contains(r.stderr, "--allow-user-secrets") || strings.Contains(r.stderr, "ghp-USER-SECRET") {
		t.Fatalf("ungranted env secret of an email feather: %+v", r)
	}
	e.must("config", "trust", "--yes", "--allow-user-secrets")
	if r := e.run("send", "--dry-run", "--to", "mail", "hi"); r.code != 0 {
		t.Fatalf("granted: %+v", r)
	}
}

func TestCritical_ProjectWebhookEnvAndKeychainNeedAGrant(t *testing.T) {
	p := newProvider(t, 200, "ok")
	for _, c := range []struct{ typ, ref, grant string }{
		{"slack", `{ env = "HOOK" }`, "env:HOOK"},
		{"discord", `{ keychain = { service = "kjak", account = "kc" } }`, "keychain:kjak/kc"},
	} {
		t.Run(c.typ, func(t *testing.T) {
			e := newEnv(t)
			e.vars["HOOK"] = p.URL
			e.keychain = keychain{"kjak/kc": p.URL}
			e.writeProject("[feathers.hook]\ntype = \"" + c.typ + "\"\nwebhook_url = " + c.ref + "\n")
			r := e.run("config", "trust", "--yes")
			if r.code != 2 || !strings.Contains(r.stdout, c.grant) || !strings.Contains(r.stderr, "--allow-user-secrets") {
				t.Fatalf("trust --yes: %+v", r)
			}
			trustPlain(t, e)
			before := p.count()
			if r := e.run("send", "--to", "hook", "hi"); r.code != 2 || !strings.Contains(r.stderr, "does not allow") || p.count() != before {
				t.Fatalf("ungranted: %+v", r)
			}
			e.must("config", "trust", "--yes", "--allow-user-secrets")
			e.must("send", "--to", "hook", "hi")
			if p.count() != before+1 {
				t.Fatal("the granted feather did not send")
			}
		})
	}
}

// trustPlain trusts the project file with no grant, as a trust made before
// the grant was asked for would be.
func trustPlain(t *testing.T, e *env) {
	t.Helper()
	src, err := configure.ReadFile(filepath.Join(e.project, ".kjak.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := configure.Trust(filepath.Join(e.xdg, "kjak", "trust.toml"), src.Path, src.Hash, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// The trust shows the host a literal webhook URL sends to (not its path or
// query, which are the secret).
func TestTrustShowsTheWebhookHost(t *testing.T) {
	e := newEnv(t)
	e.writeProject(featherConfig("proj", "slack", "https://hooks.attacker.example/services/T0/B0/SECRETPATH?token=QUERYSECRET"))
	r := e.must("config", "trust", "--yes")
	if !strings.Contains(r.stdout, `feather proj: slack, webhook URL from literal:[REDACTED], to host "hooks.attacker.example"`) || strings.Contains(r.stdout, "SECRETPATH") || strings.Contains(r.stdout, "QUERYSECRET") {
		t.Fatal(r.stdout)
	}
}
