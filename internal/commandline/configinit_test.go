package commandline_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/textbank"
)

// uncomment enables every example of the catalog: the lines that start with
// "# " (settings); "## " lines explain and stay comments.
func uncomment(s string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		b.WriteString(strings.TrimPrefix(line, "# "))
	}
	return b.String()
}

func encoded(t *testing.T, name, doc string) []byte {
	t.Helper()
	f, err := configure.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b, err := configure.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The default file and the minimal one are the same configuration, the one
// v1.0.0 wrote: no feather; only comments differ.
func TestConfigInitFilesAreTheSameConfiguration(t *testing.T) {
	v1, err := os.ReadFile(filepath.Join("testdata", "config-init-v1.0.0.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := encoded(t, "v1.0.0", string(v1))
	for name, doc := range map[string]string{"default": textbank.DefaultConfigToml, "minimal": textbank.MinimalConfigToml} {
		if got := encoded(t, name, doc); !bytes.Equal(got, want) {
			t.Errorf("%s is another configuration than v1.0.0's:\n%s\nwant:\n%s", name, got, want)
		}
	}
	if len(textbank.MinimalConfigToml) > len(textbank.DefaultConfigToml)/5 {
		t.Error("the minimal file is not minimal")
	}
}

// Every example of the catalog is valid, together with all the others (no
// name is used twice, every group member exists), and every feather type
// has one: a new type without an example fails here.
func TestConfigInitCatalog(t *testing.T) {
	all := uncomment(textbank.DefaultConfigToml)
	f, err := configure.Parse([]byte(all))
	if err != nil {
		t.Fatalf("the catalog with every example enabled: %v\n%s", err, all)
	}
	if err := configure.Validate(f); err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, fc := range f.Feathers {
		types[fc.Type] = true
	}
	for _, ty := range configure.FeatherTypes {
		if !types[ty] {
			t.Errorf("the catalog has no %s feather", ty)
		}
	}
	if len(f.Groups) == 0 || len(f.Templates) == 0 || f.Defaults.Timeout == nil || len(f.Defaults.Targets) == 0 {
		t.Errorf("the catalog lacks groups, templates or defaults: %+v", f)
	}
	// no secret value: references only
	for name, fc := range f.Feathers {
		for _, ref := range []*credential.Ref{fc.WebhookURL, fc.Password, fc.URLSecret} {
			if ref != nil && ref.Literal != nil {
				t.Errorf("%s has a literal secret", name)
			}
		}
	}
	if strings.Contains(textbank.DefaultConfigToml, "literal =") {
		t.Error("the catalog suggests a literal secret")
	}
	// every tutorial the catalog links to exists
	for _, line := range strings.Split(textbank.DefaultConfigToml, "\n") {
		i := strings.Index(line, "https://github.com/magicdrive/kjak/blob/main/")
		if i < 0 {
			continue
		}
		rel := strings.Fields(line[i+len("https://github.com/magicdrive/kjak/blob/main/"):])[0]
		if _, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(rel))); err != nil {
			t.Errorf("the catalog links to %s: %v", rel, err)
		}
	}
}

func TestConfigInitCommand(t *testing.T) {
	e := newEnv(t)
	path := filepath.Join(e.xdg, "kjak", "config.toml")
	e.must("config", "init")
	if b, _ := os.ReadFile(path); string(b) != textbank.DefaultConfigToml {
		t.Fatal("config init did not write the catalog")
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows" {
		t.Fatalf("mode: %v %v", fi, err)
	}
	e.must("config", "validate")
	// an existing file is kept, whatever the flags but --force
	if err := os.WriteFile(path, []byte("# mine\n[defaults]\nlevel = \"error\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "init"}, {"config", "init", "--minimal"}} {
		if r := e.run(args...); r.code != 2 {
			t.Fatalf("%v: %+v", args, r)
		}
		if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "# mine") {
			t.Fatalf("%v changed the file", args)
		}
	}
	e.must("config", "init", "--minimal", "--force")
	if b, _ := os.ReadFile(path); string(b) != textbank.MinimalConfigToml {
		t.Fatal("--minimal --force did not write the minimal file")
	}
	e.must("config", "init", "--force")
	if b, _ := os.ReadFile(path); string(b) != textbank.DefaultConfigToml {
		t.Fatal("--force did not write the catalog")
	}
	// then a feather added by the command line keeps the catalog
	e.must("feather", "add", "team_chat", "--type", "discord", "--secret-env", "KJAK_TEAM_HOOK")
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), textbank.DefaultConfigToml) || !strings.Contains(string(b), "[feathers.team_chat]") {
		t.Fatalf("feather add dropped the catalog:\n%s", b)
	}
	if r := e.must("feather", "remove", "team_chat"); !strings.Contains(r.stderr, "without its comments") {
		t.Fatalf("no note about the comments: %+v", r)
	}
}
