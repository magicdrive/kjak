package commandline_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/commandline"
	"github.com/magicdrive/kjak/internal/configure"
)

func (e *env) userSecrets() string { return filepath.Join(e.xdg, "kjak", "secrets.toml") }

func (e *env) projectSecrets() string { return filepath.Join(e.project, ".kjak.secrets.toml") }

func (e *env) writeFile(path, content string, mode os.FileMode) {
	e.t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		e.t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		e.t.Fatal(err)
	}
}

func secretFeather(name, secret string) string {
	return "[feathers." + name + "]\ntype = \"slack\"\nwebhook_url = { secret = \"" + secret + "\" }\n\n"
}

func TestSendWithSecretReferences(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(secretFeather("a", "dev.hook"))
	if r := e.run("send", "--to", "a", "hi"); r.code != 2 || !strings.Contains(r.stderr, "does not exist") || p.count() != 0 {
		t.Fatalf("no secrets file: %+v", r)
	}
	e.writeFile(e.userSecrets(), "[dev]\nhook = \""+p.URL+"\"\n", 0o600)
	e.must("send", "--to", "a", "hi")
	if p.count() != 1 {
		t.Fatalf("%d deliveries", p.count())
	}
	// --secrets replaces the user file
	explicit := filepath.Join(e.project, "ci-secrets.toml")
	e.writeFile(explicit, "[other]\nx = \"y\"\n", 0o600)
	if r := e.run("send", "--secrets", explicit, "--to", "a", "hi"); r.code != 2 || !strings.Contains(r.stderr, "explicit secrets file") {
		t.Fatalf("explicit file without the name: %+v", r)
	}
	// $KJAK_SECRETS too
	e.writeFile(explicit, "[dev]\nhook = \""+p.URL+"\"\n", 0o600)
	r := e.runWith(func(rt *commandline.Runtime) {
		get := rt.ConfigEnv.Getenv
		rt.ConfigEnv.Getenv = func(k string) string {
			if k == "KJAK_SECRETS" {
				return explicit
			}
			return get(k)
		}
	}, "config", "path", "-o", "json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc["secrets"] != explicit || doc["secrets_source"] != "explicit" {
		t.Fatalf("config path: %s %v", r.stdout, err)
	}
	e.must("send", "--secrets", explicit, "--to", "a", "hi")
	if p.count() != 2 {
		t.Fatalf("%d deliveries", p.count())
	}
}

// A secrets file other users can read is refused: nothing is sent.
func TestCritical_LooseSecretsFileRefused(t *testing.T) {
	if !configure.SecretsChecksPermissions {
		t.Skip("permissions are not checked on this OS")
	}
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(secretFeather("a", "dev.hook"))
	e.writeFile(e.userSecrets(), "[dev]\nhook = \""+p.URL+"\"\n", 0o644)
	r := e.run("send", "--to", "a", "-o", "json", "hi")
	if r.code != 2 || !strings.Contains(r.stderr, "group or others") || p.count() != 0 {
		t.Fatalf("a readable secrets file: %+v", r)
	}
	if !strings.Contains(r.stdout, `"code": "credential"`) {
		t.Fatalf("json error: %s", r.stdout)
	}
	if strings.Contains(r.stdout+r.stderr, p.URL) {
		t.Fatal("the secret was printed")
	}
}

// A cloned repository's configuration that asks for one of the user's
// secrets gets nothing, even once its file is trusted, unless the user
// allowed that name: the trust shows the names, and a non-interactive trust
// needs --allow-user-secrets for them.
func TestCritical_ProjectCannotExfiltrateUserSecrets(t *testing.T) {
	attacker := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeFile(e.userSecrets(), "[slack]\nbot_token = \"xoxb-USER-SECRET-VALUE\"\n", 0o600)
	e.writeProject(secretFeather("leak", "slack.bot_token"))

	r := e.run("config", "trust", "--yes")
	if r.code != 2 || !strings.Contains(r.stderr, "--allow-user-secrets") || !strings.Contains(r.stdout, "secret slack.bot_token") {
		t.Fatalf("trust --yes without --allow-user-secrets: %+v", r)
	}
	if r := e.run("send", "--to", "leak", "hi"); r.code != 2 {
		t.Fatalf("send after a refused trust: %+v", r)
	}
	// a trust from before (or one that did not allow the name) is not enough
	src, err := configure.ReadFile(filepath.Join(e.project, ".kjak.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := configure.Trust(filepath.Join(e.xdg, "kjak", "trust.toml"), src.Path, src.Hash, time.Now()); err != nil {
		t.Fatal(err)
	}
	r = e.run("send", "--to", "leak", "hi")
	if r.code != 2 || !strings.Contains(r.stderr, "does not allow") || attacker.count() != 0 {
		t.Fatalf("an ungranted user secret was used: %+v", r)
	}
	if strings.Contains(r.stdout+r.stderr, "xoxb-USER-SECRET-VALUE") {
		t.Fatal("the secret was printed")
	}
	// interactive: the second question decides, and no means not trusted
	e.tty, e.stdin = true, "y\nn\n"
	if r := e.run("config", "trust"); r.code != 2 || !strings.Contains(r.stdout, "Allow it to read the 1 secret(s)") {
		t.Fatalf("declined grant: %+v", r)
	}
	e.stdin = "y\ny\n"
	e.must("config", "trust")
	e.tty = false
	ts, err := configure.ReadTrustStore(filepath.Join(e.xdg, "kjak", "trust.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if entry, ok := ts.Entry(src.Path); !ok || !entry.AllowsUserSecret("slack.bot_token") {
		t.Fatalf("grant not recorded: %+v", entry)
	}
	// granted, the value is used (here: an invalid webhook URL, refused
	// before sending, which shows the value was read)
	if r := e.run("send", "--to", "leak", "hi"); r.code != 2 || !strings.Contains(r.stderr, "webhook_url") {
		t.Fatalf("granted: %+v", r)
	}
	if attacker.count() != 0 {
		t.Fatal("sent")
	}
}

// The project secrets file is trusted with the project: one replaced after
// the approval, or a link to the user's file, is refused.
func TestCritical_ProjectSecretsReplacementAndLinks(t *testing.T) {
	p := newProvider(t, 200, "ok")
	evil := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeProject(secretFeather("proj", "proj.hook"))
	e.writeFile(e.projectSecrets(), "[proj]\nhook = \""+p.URL+"\"\n", 0o600)
	r := e.must("config", "trust", "--yes")
	if !strings.Contains(r.stdout, "project secrets ") || strings.Contains(r.stdout, p.URL) {
		t.Fatalf("trust output: %s", r.stdout)
	}
	e.must("send", "--to", "proj", "hi")
	e.writeFile(e.projectSecrets(), "[proj]\nhook = \""+evil.URL+"\"\n", 0o600)
	if r := e.run("send", "--to", "proj", "hi"); r.code != 2 || !strings.Contains(r.stderr, "changed since it was trusted") {
		t.Fatalf("replaced secrets: %+v", r)
	}
	if r := e.must("config", "path"); !strings.Contains(r.stdout, "untrusted") {
		t.Fatalf("config path: %s", r.stdout)
	}
	if evil.count() != 0 || p.count() != 1 {
		t.Fatalf("deliveries %d %d", p.count(), evil.count())
	}
	// the global configuration never reads the project secrets file
	e.must("config", "untrust")
	_ = os.Remove(e.projectSecrets())
	e.writeGlobal(secretFeather("mine", "proj.hook"))
	e.writeFile(e.projectSecrets(), "[proj]\nhook = \""+evil.URL+"\"\n", 0o600)
	if r := e.run("send", "--no-project", "--to", "mine", "hi"); r.code != 2 || evil.count() != 0 {
		t.Fatalf("a global feather read the project secrets: %+v", r)
	}
}

func TestProjectSecretsLinkRefused(t *testing.T) {
	if !configure.SecretsChecksPermissions {
		t.Skip("symbolic links need a privilege on Windows")
	}
	e := newEnv(t)
	e.writeFile(e.userSecrets(), "[slack]\nbot_token = \"xoxb-1\"\n", 0o600)
	e.writeProject(secretFeather("leak", "slack.bot_token"))
	if err := os.Symlink(e.userSecrets(), e.projectSecrets()); err != nil {
		t.Fatal(err)
	}
	r := e.run("config", "trust", "--yes", "--allow-user-secrets")
	if r.code != 2 || !strings.Contains(r.stderr, "symbolic link") {
		t.Fatalf("trust of a linked secrets file: %+v", r)
	}
}

// Without a terminal, nothing waits for an answer.
func TestTrustNeverWaitsWithoutTerminal(t *testing.T) {
	e := newEnv(t)
	e.writeProject(secretFeather("p", "a.b"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r := e.runWith(func(rt *commandline.Runtime) { rt.Stdin = blockingReader{ctx} }, "config", "trust")
	if r.code != 2 || !strings.Contains(r.stderr, "not a terminal") || ctx.Err() != nil {
		t.Fatalf("%+v", r)
	}
}

type blockingReader struct{ ctx context.Context }

func (b blockingReader) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func TestMigrateSecrets(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal("[feathers.\"dev.slack\"]\ntype = \"slack\"\nwebhook_url = { literal = \"" + p.URL + "\" }\n\n" +
		"[feathers.mail]\ntype = \"email\"\nhost = \"127.0.0.1\"\nuser = \"u\"\npassword = { literal = \"MailPassw0rdSecret\" }\nfrom = \"a@example.com\"\nto = [\"b@example.com\"]\n")
	global := filepath.Join(e.xdg, "kjak", "config.toml")
	before, _ := os.ReadFile(global)

	r := e.must("send", "--to", "dev.slack", "hi")
	if !strings.Contains(r.stderr, "literal (deprecated)") {
		t.Fatalf("no deprecation warning: %s", r.stderr)
	}
	if r := e.run("config", "validate", "--strict"); r.code != 2 || !strings.Contains(r.stderr, "feathers.dev.slack.webhook_url") {
		t.Fatalf("validate --strict: %+v", r)
	}
	r = e.must("config", "migrate-secrets", "--dry-run")
	if !strings.Contains(r.stdout, `would move feathers.dev.slack.webhook_url -> { secret = "dev_slack.webhook_url" }`) {
		t.Fatalf("dry run: %s", r.stdout)
	}
	if _, err := os.Stat(e.userSecrets()); err == nil {
		t.Fatal("a dry run wrote the secrets file")
	}
	r = e.must("config", "migrate-secrets")
	if strings.Contains(r.stdout+r.stderr, "MailPassw0rdSecret") || strings.Contains(r.stdout+r.stderr, p.URL) {
		t.Fatal("a secret was printed")
	}
	if !strings.Contains(r.stderr, "still holds the 2 literal secret(s)") {
		t.Fatalf("no warning about the old file: %s", r.stderr)
	}
	after, _ := os.ReadFile(global)
	if string(before) != string(after) {
		t.Fatal("the original file was changed")
	}
	if configure.SecretsChecksPermissions {
		if st, err := os.Stat(e.userSecrets()); err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("secrets file mode: %v %v", st, err)
		}
	}
	migrated, err := os.ReadFile(global + ".migrated")
	if err != nil || strings.Contains(string(migrated), "literal") || strings.Contains(string(migrated), "MailPassw0rdSecret") {
		t.Fatalf("migrated config: %v\n%s", err, migrated)
	}
	// a second run never overwrites what the first wrote
	if r := e.run("config", "migrate-secrets"); r.code != 2 || !strings.Contains(r.stderr, "exists") {
		t.Fatalf("second run: %+v", r)
	}
	if err := os.Rename(global+".migrated", global); err != nil {
		t.Fatal(err)
	}
	e.must("config", "validate", "--strict")
	r = e.must("send", "--to", "dev.slack", "hi")
	if strings.Contains(r.stderr, "deprecated") || p.count() != 2 {
		t.Fatalf("after the migration: %+v (%d)", r, p.count())
	}
	if r := e.must("config", "migrate-secrets"); !strings.Contains(r.stdout, "nothing to migrate") {
		t.Fatal(r.stdout)
	}
}

func TestMigrateProjectSecrets(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeProject(featherConfig("proj", "slack", p.URL))
	r := e.must("config", "migrate-secrets", "--scope", "project")
	if !strings.Contains(r.stdout, ".gitignore") {
		t.Fatalf("no reminder: %s", r.stdout)
	}
	project := filepath.Join(e.project, ".kjak.toml")
	if err := os.Rename(project+".migrated", project); err != nil {
		t.Fatal(err)
	}
	e.must("config", "trust", "--yes")
	e.must("send", "--to", "proj", "hi")
	if p.count() != 1 {
		t.Fatalf("%d deliveries", p.count())
	}
}

func TestFeatherAddSecretRef(t *testing.T) {
	e := newEnv(t)
	e.must("feather", "add", "a", "--type", "slack", "--secret-ref", "dev.hook")
	r := e.must("feather", "show", "a")
	if !strings.Contains(r.stdout, "secret:dev.hook") {
		t.Fatal(r.stdout)
	}
	if r := e.run("feather", "add", "b", "--type", "slack", "--secret-ref", "bad..name"); r.code != 2 {
		t.Fatalf("invalid name: %+v", r)
	}
	if r := e.run("feather", "add", "c", "--type", "slack", "--secret-ref", "x", "--secret-env", "Y"); r.code != 2 {
		t.Fatalf("two sources: %+v", r)
	}
	e.stdin = "https://hooks.example/x\n"
	if r := e.must("feather", "add", "d", "--type", "slack", "--secret-stdin"); !strings.Contains(r.stderr, "deprecated") {
		t.Fatalf("no deprecation warning: %s", r.stderr)
	}
}

// A message that looks like a reference or a template is sent as it is:
// nothing in kjak expands it.
func TestSecretReferenceInjectionIsInert(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(secretFeather("a", "dev.hook"))
	e.writeFile(e.userSecrets(), "[dev]\nhook = \""+p.URL+"\"\nother = \"OTHER-SECRET-VALUE\"\n", 0o600)
	e.must("send", "--to", "a", `{ secret = "dev.other" } {{.dev.other}} ${KJAK_SECRET} secret:dev.other`)
	p.mu.Lock()
	body := p.bodies[0]
	p.mu.Unlock()
	if strings.Contains(body, "OTHER-SECRET-VALUE") || !strings.Contains(body, "secret:dev.other") {
		t.Fatalf("body: %s", body)
	}
}
