package template

import (
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
)

// A message template cannot loop: range (over a number it runs as long as
// the number says, with no output to bound it) and define/template (a
// recursion that branches runs for an exponential time) are refused when
// the template is checked and when it is rendered.
func TestCritical_MessageTemplatesCannotLoop(t *testing.T) {
	for _, src := range []string{
		`{{range 100000000000000}}{{end}}`,
		`{{if .x}}{{range 100000000000000}}{{end}}{{end}}`,
		`{{define "a"}}{{template "a" .}}{{template "a" .}}{{end}}{{template "a" .}}`,
		`{{block "b" .}}{{end}}`,
	} {
		for _, field := range []string{"title", "body", "level", "url"} {
			tc := configure.TemplateConfig{Body: "b"}
			switch field {
			case "title":
				tc.Title = src
			case "body":
				tc.Body = src
			case "level":
				tc.Level = src
			case "url":
				tc.URL = src
			}
			if _, err := Check("t", tc); err == nil {
				t.Errorf("%s %q: checked", field, src)
			}
			done := make(chan error, 1)
			go func() {
				_, err := Render("t", tc, map[string]string{"x": "1"})
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "not available in a message template") {
					t.Errorf("%s %q: %v", field, src, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s %q: the rendering did not end", field, src)
			}
		}
	}
	// the rest of text/template is still there
	m, err := Render("t", configure.TemplateConfig{Title: `{{if eq .x "1"}}one{{else}}other{{end}}`, Body: `{{with .x}}{{printf "%q" .}}{{end}} {{len .x}}`}, map[string]string{"x": "1"})
	if err != nil || m.Title != "one" || m.Body != `"1" 1` {
		t.Fatalf("%+v %v", m, err)
	}
}
