package template

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestJSONBodyTemplate(t *testing.T) {
	data := map[string]string{"title": `a"b`, "body": "{{.title}}\n", "level": "info"}
	out, err := JSON("t", `{"text": "{{.title}} {{.body}}", "list": ["{{.level}}", 1.50, true, null], "k{{.level}}": "x"}`, data)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if doc["text"] != "a\"b {{.title}}\n" || doc["k{{.level}}"] != "x" || !strings.Contains(string(out), "1.50") {
		t.Fatalf("%s", out)
	}
	for _, bad := range []string{`{"a": `, `{"a": "{{.missing}}"}`, `{} {}`, `{"a": "{{.title"}`} {
		if _, err := JSON("t", bad, data); !errors.Is(err, ErrTemplate) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if s, err := Text("t", "[{{.level}}] {{.body}}", data); err != nil || s != "[info] {{.title}}\n" {
		t.Fatalf("%q %v", s, err)
	}
}

// Whatever the message holds, a JSON template renders to JSON of the same
// shape: a value never adds a key or an element.
func FuzzJSONBodyTemplate(f *testing.F) {
	f.Add(`x", "admin": true, "y": "`, "{{.title}}")
	f.Add(" </script>\x00", `\"`)
	f.Fuzz(func(t *testing.T, title, body string) {
		out, err := JSON("t", `{"text": "{{.title}}", "nested": {"b": ["{{.body}}"]}}`, map[string]string{"title": title, "body": body})
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Text   string              `json:"text"`
			Nested map[string][]string `json:"nested"`
		}
		dec := json.NewDecoder(strings.NewReader(string(out)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&doc); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		if len(doc.Nested) != 1 || len(doc.Nested["b"]) != 1 {
			t.Fatalf("shape changed: %s", out)
		}
	})
}

// A template cannot make kjak allocate without bound: printf widths, a
// range in a body template, an output over the limit.
func TestTemplateLimits(t *testing.T) {
	data := map[string]string{"body": strings.Repeat("x", 1000)}
	for _, src := range []string{`{{printf "%999999999d" 1}}`, `{{printf "%.99999f" 1.0}}`, `{{printf "%*d" 999999999 1}}`, `{{printf "%[1]*d" 999999999 1}}`} {
		if _, err := Text("t", src, data); !errors.Is(err, ErrTemplate) {
			t.Errorf("%s: %v", src, err)
		}
	}
	for _, src := range []string{`{{range 100000000000}}{{$.body}}{{end}}`, `{{if .body}}{{range 5}}x{{end}}{{end}}`, `{{define "a"}}x{{end}}{{template "a"}}`} {
		if _, err := Text("t", src, data); !errors.Is(err, ErrTemplate) {
			t.Errorf("%s: %v", src, err)
		}
		if _, err := JSON("t", `{"a": "`+strings.ReplaceAll(src, `"`, `\"`)+`"}`, data); !errors.Is(err, ErrTemplate) {
			t.Errorf("json %s: %v", src, err)
		}
	}
	// a message template renders within the output limit
	big := map[string]string{"x": strings.Repeat("y", 1<<16)}
	if _, err := Render("t", configure.TemplateConfig{Body: strings.Repeat("{{.x}}", 17)}, big); !errors.Is(err, ErrTemplate) || !strings.Contains(err.Error(), "longer than") {
		t.Errorf("an output over the limit: %v", err)
	}
	if s, err := Text("t", `{{printf "%-10s|" .body | len}}`, map[string]string{"body": "a"}); err != nil || s != "11" {
		t.Errorf("printf: %q %v", s, err)
	}
}
