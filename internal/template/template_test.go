package template

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
)

func TestRender(t *testing.T) {
	tpl := configure.TemplateConfig{Title: "Deployed {{.service}}", Body: "{{.service}} {{.version}} is live", Level: "{{.level}}", URL: "https://ci.example/{{.run}}"}
	m, err := Render("deploy", tpl, map[string]string{"service": "api", "version": "1.2", "level": "success", "run": "7"})
	if err != nil {
		t.Fatal(err)
	}
	want := core.Message{Title: "Deployed api", Body: "api 1.2 is live", Level: core.LevelSuccess, URL: "https://ci.example/7"}
	if m != want {
		t.Fatalf("message = %+v", m)
	}
	if _, err := Render("deploy", tpl, map[string]string{"service": "api"}); !errors.Is(err, ErrTemplate) {
		t.Fatalf("missing variable: %v", err)
	}
	if _, err := Render("x", configure.TemplateConfig{Body: "{{.a}}", Level: "{{.a}}"}, map[string]string{"a": "loud"}); err == nil {
		t.Fatal("a rendered unknown level was accepted")
	}
	if _, err := Render("x", configure.TemplateConfig{Body: "{{.a"}, nil); err == nil {
		t.Fatal("a syntax error was accepted")
	}
}

func TestParseVars(t *testing.T) {
	v, err := ParseVars([]string{"a=1", "b=x=y", "c="})
	if err != nil || v["a"] != "1" || v["b"] != "x=y" || v["c"] != "" {
		t.Fatalf("vars = %v, %v", v, err)
	}
	for _, bad := range [][]string{{"noequals"}, {"=v"}, {"a-b=1"}, {"a=1", "a=2"}} {
		if _, err := ParseVars(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.toml")
	_ = os.WriteFile(p, []byte("title = \"T {{.x}}\"\nbody = \"B\"\nlevel = \"error\"\n"), 0o600)
	tpl, err := ParseFile(p)
	if err != nil || tpl.Title != "T {{.x}}" || tpl.Level != "error" {
		t.Fatalf("%+v %v", tpl, err)
	}
	_ = os.WriteFile(p, []byte("bdy = \"typo\"\n"), 0o600)
	if _, err := ParseFile(p); err == nil {
		t.Fatal("an unknown key was accepted")
	}
	if _, err := ParseFile(filepath.Join(dir, "none.toml")); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestCheck(t *testing.T) {
	if _, err := Check("t", configure.TemplateConfig{Body: "{{.a}}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Check("t", configure.TemplateConfig{Body: "x", Title: "{{"}); err == nil {
		t.Fatal("broken title accepted")
	}
	if _, err := Check("t", configure.TemplateConfig{Body: "x", Level: "loud"}); err == nil {
		t.Fatal("bad level accepted")
	}
}
