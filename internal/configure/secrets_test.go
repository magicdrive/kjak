package configure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/credential"
)

const secretValue = "s3cr3t-VALUE-never-printed"

// secretsFixture is a fixture with both secrets files in place.
func secretsFixture(t *testing.T) fixture {
	t.Helper()
	fx := newFixture(t)
	fx.paths.Secrets = filepath.Join(filepath.Dir(fx.paths.Global), SecretsFileName)
	fx.paths.SecretsFrom = FromUser
	fx.paths.ProjectSecrets = filepath.Join(filepath.Dir(fx.paths.Project), ProjectSecretsFileName)
	return fx
}

func lookup(t *testing.T, src credential.SecretSource, name string) (string, error) {
	t.Helper()
	s, err := src.LookupSecret(context.Background(), name)
	return s.Reveal(), err
}

func TestParseSecrets(t *testing.T) {
	values, err := ParseSecrets([]byte("top = \"a\"\n[slack]\nbot_token = \"b\"\n[slack.ops]\nhook = \"c\"\n\"quoted-key\" = \"d\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"top": "a", "slack.bot_token": "b", "slack.ops.hook": "c", "slack.ops.quoted-key": "d"}
	if len(values) != len(want) {
		t.Fatalf("got %v", values)
	}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("%s = %q, want %q", k, values[k], v)
		}
	}
	bad := map[string]string{
		"integer":       "n = 1\n",
		"array":         "n = [\"" + secretValue + "\"]\n",
		"dotted key":    "\"a.b\" = \"" + secretValue + "\"\n",
		"bad segment":   "\"-x\" = \"" + secretValue + "\"\n",
		"syntax":        "x = \"" + secretValue + "\n",
		"duplicate":     "x = \"" + secretValue + "\"\nx = \"" + secretValue + "\"\n",
		"too deep":      "[a.b.c.d.e.f.g.h]\ni = \"" + secretValue + "\"\n",
		"date":          "d = 2026-01-01\n",
		"table array":   "[[a]]\nb = \"" + secretValue + "\"\n",
		"unicode label": "\"ü\" = \"" + secretValue + "\"\n",
	}
	for name, doc := range bad {
		_, err := ParseSecrets([]byte(doc))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error(), secretValue) {
			t.Errorf("%s: the error quotes a value: %v", name, err)
		}
	}
}

func TestEncodeSecretsRoundTrip(t *testing.T) {
	in := map[string]string{"a.webhook_url": "https://h/x", "b.password": "p\"w\n", "b.auth.token": "t"}
	doc, err := EncodeSecrets(in)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseSecrets(doc)
	if err != nil {
		t.Fatalf("%v\n%s", err, doc)
	}
	for k, v := range in {
		if back[k] != v {
			t.Errorf("%s = %q, want %q", k, back[k], v)
		}
	}
	if _, err := EncodeSecrets(map[string]string{"a": "x", "a.b": "y"}); err == nil {
		t.Error("a secret that is also a table was encoded")
	}
	if _, err := EncodeSecrets(map[string]string{"a..b": "x"}); err == nil {
		t.Error("an invalid name was encoded")
	}
}

func TestReadSecretsFileMissingAndLimits(t *testing.T) {
	dir := t.TempDir()
	sf, err := ReadSecretsFile(filepath.Join(dir, "none.toml"), true)
	if err != nil || sf.Exists {
		t.Fatalf("missing file: %+v %v", sf, err)
	}
	big := filepath.Join(dir, "big.toml")
	write(t, big, "x = \""+strings.Repeat("a", MaxConfigSize)+"\"\n")
	if _, err := ReadSecretsFile(big, false); err == nil || !errors.Is(err, credential.ErrCredential) {
		t.Fatalf("a file over the limit: %v", err)
	}
	if _, err := ReadSecretsFile(dir, false); err == nil {
		t.Fatal("a directory was read as a secrets file")
	}
}

// The global configuration's secret references read the personal file
// only: a project secrets file cannot redirect a global feather.
func TestCritical_SecretScopeGlobalNeverReadsProjectSecrets(t *testing.T) {
	fx := secretsFixture(t)
	write(t, fx.paths.Global, "[feathers.a]\ntype = \"slack\"\nwebhook_url = { secret = \"a.hook\" }\n")
	write(t, fx.paths.Project, "[feathers.p]\ntype = \"slack\"\nwebhook_url = { secret = \"p.hook\" }\n")
	write(t, fx.paths.ProjectSecrets, "[a]\nhook = \"https://evil.example/x\"\n[p]\nhook = \"https://project.example/x\"\n")
	sf, err := ReadSecretsFile(fx.paths.ProjectSecrets, true)
	if err != nil {
		t.Fatal(err)
	}
	src, _ := ReadFile(fx.paths.Project)
	canon, _ := CanonicalPath(fx.paths.ProjectSecrets)
	if err := TrustWith(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0, TrustGrant{SecretsPath: canon, SecretsSHA256: sf.Hash}); err != nil {
		t.Fatal(err)
	}
	l, err := Load(fx.paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lookup(t, l.SecretSource("global"), "a.hook"); err == nil || !strings.Contains(err.Error(), "user secrets file") {
		t.Fatalf("a global reference read the project secrets file: %v", err)
	}
	if v, err := lookup(t, l.SecretSource("project"), "p.hook"); err != nil || v != "https://project.example/x" {
		t.Fatalf("project reference: %q %v", v, err)
	}
	// the personal file is read once per run: a new run sees the new file
	write(t, fx.paths.Secrets, "[a]\nhook = \"https://user.example/x\"\n")
	if l, err = Load(fx.paths); err != nil {
		t.Fatal(err)
	}
	if v, err := lookup(t, l.SecretSource("global"), "a.hook"); err != nil || v != "https://user.example/x" {
		t.Fatalf("global reference: %q %v", v, err)
	}
}

// A project configuration reads a personal secret only when its trust
// allows that name: a trusted project cannot read any other.
func TestCritical_ProjectCannotReadUserSecretsWithoutGrant(t *testing.T) {
	fx := secretsFixture(t)
	write(t, fx.paths.Secrets, "[slack]\nbot_token = \""+secretValue+"\"\nother = \"other-value\"\n")
	write(t, fx.paths.Project, "[feathers.p]\ntype = \"slack\"\nwebhook_url = { secret = \"slack.bot_token\" }\n")
	src, _ := ReadFile(fx.paths.Project)
	if err := Trust(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0); err != nil {
		t.Fatal(err)
	}
	l, err := Load(fx.paths)
	if err != nil {
		t.Fatal(err)
	}
	v, err := lookup(t, l.SecretSource("project"), "slack.bot_token")
	if err == nil || v != "" || !errors.Is(err, credential.ErrCredential) || !strings.Contains(err.Error(), "does not allow") {
		t.Fatalf("an ungranted personal secret was read: %q %v", v, err)
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Fatal("the error quotes the secret")
	}
	if err := TrustWith(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0, TrustGrant{UserSecrets: []string{"slack.bot_token"}}); err != nil {
		t.Fatal(err)
	}
	if l, err = Load(fx.paths); err != nil {
		t.Fatal(err)
	}
	if v, err := lookup(t, l.SecretSource("project"), "slack.bot_token"); err != nil || v != secretValue {
		t.Fatalf("a granted secret: %q %v", v, err)
	}
	if _, err := lookup(t, l.SecretSource("project"), "slack.other"); err == nil {
		t.Fatal("the grant of one name allowed another")
	}
}

// The project secrets file is part of the trust: not covered, changed or
// gone, the project is untrusted.
func TestCritical_ProjectSecretsAreTrustedWithTheProject(t *testing.T) {
	fx := secretsFixture(t)
	write(t, fx.paths.Project, "[feathers.p]\ntype = \"slack\"\nwebhook_url = { secret = \"p.hook\" }\n")
	src, _ := ReadFile(fx.paths.Project)
	if err := Trust(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(fx.paths); err != nil {
		t.Fatalf("no secrets file: %v", err)
	}
	// a secrets file that appears after the trust
	write(t, fx.paths.ProjectSecrets, "[p]\nhook = \"https://a.example/x\"\n")
	if _, err := Load(fx.paths); !errors.Is(err, ErrUntrustedProject) {
		t.Fatalf("a secrets file the trust does not cover: %v", err)
	}
	sf, _ := ReadSecretsFile(fx.paths.ProjectSecrets, true)
	canon, _ := CanonicalPath(fx.paths.ProjectSecrets)
	if err := TrustWith(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0, TrustGrant{SecretsPath: canon, SecretsSHA256: sf.Hash}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(fx.paths); err != nil {
		t.Fatalf("trusted: %v", err)
	}
	// replaced after the approval
	write(t, fx.paths.ProjectSecrets, "[p]\nhook = \"https://evil.example/x\"\n")
	if _, err := Load(fx.paths); !errors.Is(err, ErrUntrustedProject) {
		t.Fatalf("a changed secrets file: %v", err)
	}
	// gone
	write(t, fx.paths.ProjectSecrets, "[p]\nhook = \"https://a.example/x\"\n")
	if _, err := Load(fx.paths); err != nil {
		t.Fatalf("restored: %v", err)
	}
	if err := os.Remove(fx.paths.ProjectSecrets); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(fx.paths); !errors.Is(err, ErrUntrustedProject) {
		t.Fatalf("a removed secrets file: %v", err)
	}
}

// kjak's own edit of a trusted project keeps the grant of the names the
// new content still refers to, and nothing more.
func TestUpdateKeepsTheGrantOfRemainingNames(t *testing.T) {
	fx := secretsFixture(t)
	write(t, fx.paths.Project, "[feathers.p]\ntype = \"slack\"\nwebhook_url = { secret = \"a.x\" }\n[feathers.q]\ntype = \"slack\"\nwebhook_url = { secret = \"b.x\" }\n")
	src, _ := ReadFile(fx.paths.Project)
	if err := TrustWith(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0, TrustGrant{UserSecrets: []string{"a.x", "b.x"}}); err != nil {
		t.Fatal(err)
	}
	err := Update(fx.paths, ScopeProject, t0, func(f *File) error {
		delete(f.Feathers, "q")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := ReadTrustStore(fx.paths.TrustStore)
	e, ok := ts.Entry(fx.paths.Project)
	if !ok || len(e.UserSecrets) != 1 || e.UserSecrets[0] != "a.x" {
		t.Fatalf("entry after the edit: %+v", e)
	}
}

// A trust store of an earlier kjak (entries without the new keys) is read,
// and an entry without secrets is written without them.
func TestTrustStoreCompatibility(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.TrustStore, "[[projects]]\npath = '/p/.kjak.toml'\nsha256 = 'abc'\ntrusted_at = 2026-10-01T12:00:00Z\n")
	if _, err := ReadTrustStore(fx.paths.TrustStore); err != nil {
		t.Fatal(err)
	}
	write(t, fx.paths.Project, projectTOML)
	src, _ := ReadFile(fx.paths.Project)
	if err := Trust(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(fx.paths.TrustStore)
	b := string(raw)
	if strings.Contains(b, "secrets") || strings.Contains(b, "user_secrets") {
		t.Fatalf("an entry without secrets mentions them:\n%s", b)
	}
}

func TestDefaultPathsSecrets(t *testing.T) {
	env := Env{
		Getenv:  func(k string) string { return map[string]string{"XDG_CONFIG_HOME": absPath("/xdg")}[k] },
		HomeDir: func() (string, error) { return absPath("/home/u"), nil },
		Getwd:   func() (string, error) { return absPath("/work"), nil },
	}
	p, err := DefaultPaths(env, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Secrets != filepath.Join(absPath("/xdg"), "kjak", "secrets.toml") || p.SecretsFrom != FromUser || p.GlobalFrom != FromUser {
		t.Fatalf("%+v", p)
	}
	if p.ProjectSecrets != filepath.Join(absPath("/work"), ".kjak.secrets.toml") {
		t.Fatalf("project secrets %s", p.ProjectSecrets)
	}
	env.Getenv = func(k string) string {
		return map[string]string{"XDG_CONFIG_HOME": absPath("/xdg"), "KJAK_SECRETS": "/env.toml", "KJAK_CONFIG": "/c.toml"}[k]
	}
	p, _ = DefaultPaths(env, "", true)
	if p.Secrets != "/env.toml" || p.SecretsFrom != FromEnv || p.GlobalFrom != FromEnv || p.ProjectSecrets != "" {
		t.Fatalf("%+v", p)
	}
	p.SetExplicitSecrets("/flag.toml")
	if p.Secrets != "/flag.toml" || p.SecretsFrom != FromFlag || p.SecretsFrom.Kind() != "explicit" {
		t.Fatalf("%+v", p)
	}
}

func TestSecretRefValidation(t *testing.T) {
	for _, doc := range []string{
		"[feathers.a]\ntype = \"slack\"\nwebhook_url = { secret = \"\" }\n",
		"[feathers.a]\ntype = \"slack\"\nwebhook_url = { secret = \"a..b\" }\n",
		"[feathers.a]\ntype = \"slack\"\nwebhook_url = { secret = \"a\", env = \"B\" }\n",
		"[feathers.a]\ntype = \"slack\"\nwebhook_url = { secret = \"${HOME}\" }\n",
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("accepted:\n%s", doc)
		}
	}
	f := mustParse(t, "[feathers.a]\ntype = \"slack\"\nwebhook_url = { secret = \"a.b-c_d\" }\n")
	if got := SecretNames(f); len(got) != 1 || got[0] != "a.b-c_d" {
		t.Fatalf("names %v", got)
	}
}
