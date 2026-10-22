// Package template renders message templates: Go text/template strings for
// the title, body, level and url of a message, with string variables.
//
//	[templates.deploy]
//	title = "Deployed {{.service}}"
//	body  = "{{.service}} {{.version}} is live"
//	level = "success"
//
// A variable the template uses but the caller did not give is an error; no
// function beyond text/template's built-ins is available.
package template

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"

	"github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
)

// ErrTemplate wraps every template error.
var ErrTemplate = errors.New("template error")

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseVars parses "key=value" assignments.
func ParseVars(assignments []string) (map[string]string, error) {
	vars := map[string]string{}
	for _, a := range assignments {
		k, v, ok := strings.Cut(a, "=")
		if !ok || !varName.MatchString(k) {
			return nil, fmt.Errorf("%w: invalid variable %q (use key=value; a key is letters, digits and '_')", ErrTemplate, a)
		}
		if _, dup := vars[k]; dup {
			return nil, fmt.Errorf("%w: variable %q given twice", ErrTemplate, k)
		}
		vars[k] = v
	}
	return vars, nil
}

// Render renders t with vars into a message (not validated: the caller
// applies its overrides first).
func Render(name string, t configure.TemplateConfig, vars map[string]string) (core.Message, error) {
	var m core.Message
	fields := []struct {
		label string
		src   string
		dst   *string
	}{
		{"title", t.Title, &m.Title},
		{"body", t.Body, &m.Body},
		{"url", t.URL, &m.URL},
	}
	for _, f := range fields {
		out, err := renderOne(name+"."+f.label, f.src, vars)
		if err != nil {
			return core.Message{}, err
		}
		*f.dst = out
	}
	level, err := renderOne(name+".level", t.Level, vars)
	if err != nil {
		return core.Message{}, err
	}
	if level != "" {
		l, err := core.ParseLevel(level)
		if err != nil {
			return core.Message{}, fmt.Errorf("%w: %s: %v", ErrTemplate, name, err)
		}
		m.Level = l
	}
	return m, nil
}

func renderOne(name, src string, vars map[string]string) (string, error) {
	if src == "" {
		return "", nil
	}
	tpl, err := template.New(name).Option("missingkey=error").Parse(src)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTemplate, err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("%w: %v", ErrTemplate, err)
	}
	return buf.String(), nil
}

// ParseFile reads a template file: a TOML document with the keys of a
// [templates.<name>] table (title, body, level, url).
func ParseFile(path string) (configure.TemplateConfig, error) {
	var t configure.TemplateConfig
	content, err := os.ReadFile(path)
	if err != nil {
		return t, fmt.Errorf("%w: %v", ErrTemplate, err)
	}
	dec := toml.NewDecoder(bytes.NewReader(content))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return t, fmt.Errorf("%w: %s: %v", ErrTemplate, path, err)
	}
	if strings.TrimSpace(t.Body) == "" {
		return t, fmt.Errorf("%w: %s: body is required", ErrTemplate, path)
	}
	return t, nil
}

// Check parses every field of t without rendering it.
func Check(name string, t configure.TemplateConfig) (bool, error) {
	if strings.TrimSpace(t.Body) == "" {
		return false, fmt.Errorf("%w: %s: body is required", ErrTemplate, name)
	}
	for _, src := range []string{t.Title, t.Body, t.Level, t.URL} {
		if _, err := template.New(name).Option("missingkey=error").Parse(src); err != nil {
			return false, fmt.Errorf("%w: %v", ErrTemplate, err)
		}
	}
	if t.Level != "" && !strings.Contains(t.Level, "{{") {
		if _, err := core.ParseLevel(t.Level); err != nil {
			return false, fmt.Errorf("%w: %s: %v", ErrTemplate, name, err)
		}
	}
	return true, nil
}
