// Package template renders message templates: Go text/template strings for
// the title, body, level and url of a message, with string variables.
//
//	[templates.deploy]
//	title = "Deployed {{.service}}"
//	body  = "{{.service}} {{.version}} is live"
//	level = "success"
//
// A variable the template uses but the caller did not give is an error; no
// function beyond text/template's built-ins is available, and a template
// cannot loop (range, define/template/block).
package template

import (
	"bytes"
	"encoding/json"
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
	return render(name, src, vars, false)
}

// MaxRendered bounds what one template renders.
const MaxRendered = 1 << 20

// errTooLong ends a rendering that goes past MaxRendered.
var errTooLong = fmt.Errorf("the rendered text is longer than %d bytes", MaxRendered)

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > MaxRendered {
		return 0, errTooLong
	}
	return b.Buffer.Write(p)
}

// render parses and executes src into a bounded buffer. printf refuses a
// width or a precision of 4 digits or more (fmt would allocate it before
// the buffer sees it). No template can loop (range, template/define/block):
// its data are strings, and a range over a number, or a recursion, could
// run without end and without output for the buffer to bound.
func render(name, src string, vars map[string]string, body bool) (string, error) {
	tpl, err := template.New(name).Option("missingkey=error").Funcs(template.FuncMap{"printf": safePrintf}).Parse(src)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTemplate, err)
	}
	problem := configure.MessageTemplateProblem
	if body {
		problem = configure.BodyTemplateProblem
	}
	if err := problem(tpl); err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrTemplate, name, err)
	}
	var buf boundedBuffer
	if err := tpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("%w: %v", ErrTemplate, err)
	}
	return buf.String(), nil
}

var bigVerb = regexp.MustCompile(`%[-+# 0]*(\d{4,}|\*|\[)|\.(\d{4,}|\*)`)

func safePrintf(format string, args ...any) (string, error) {
	if bigVerb.MatchString(format) {
		return "", errors.New("printf: a width or precision of 1000 or more, '*' or an argument index is not allowed")
	}
	s := fmt.Sprintf(format, args...)
	if len(s) > MaxRendered {
		return "", errTooLong
	}
	return s, nil
}

// Text renders src (a body template of an http feather) with data, as
// text. A value of data is never parsed as a template.
func Text(name, src string, data map[string]string) (string, error) {
	if src == "" {
		return "", nil
	}
	return render(name, src, data, true)
}

// JSON renders a JSON document whose strings are templates: each string
// value is rendered with data and the document is encoded again, so a value
// can never change its structure (no key or element can be injected). Keys
// are kept as they are.
func JSON(name, src string, data map[string]string) ([]byte, error) {
	// numbers stay as written (json.Number), not float64
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: %s: not a JSON document: %v", ErrTemplate, name, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: %s: more than one JSON document", ErrTemplate, name)
	}
	var walk func(any) (any, error)
	walk = func(v any) (any, error) {
		switch t := v.(type) {
		case string:
			if t == "" {
				return t, nil
			}
			return render(name, t, data, true)
		case []any:
			for i, e := range t {
				r, err := walk(e)
				if err != nil {
					return nil, err
				}
				t[i] = r
			}
			return t, nil
		case map[string]any:
			for k, e := range t {
				r, err := walk(e)
				if err != nil {
					return nil, err
				}
				t[k] = r
			}
			return t, nil
		}
		return v, nil
	}
	out, err := walk(doc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrTemplate, name, err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
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
		tpl, err := template.New(name).Option("missingkey=error").Parse(src)
		if err != nil {
			return false, fmt.Errorf("%w: %v", ErrTemplate, err)
		}
		if err := configure.MessageTemplateProblem(tpl); err != nil {
			return false, fmt.Errorf("%w: %s: %v", ErrTemplate, name, err)
		}
	}
	if t.Level != "" && !strings.Contains(t.Level, "{{") {
		if _, err := core.ParseLevel(t.Level); err != nil {
			return false, fmt.Errorf("%w: %s: %v", ErrTemplate, name, err)
		}
	}
	return true, nil
}
