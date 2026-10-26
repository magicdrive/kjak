package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather/external"
	"github.com/magicdrive/kjak/internal/feather/webhook"
	"github.com/magicdrive/kjak/internal/output"
)

// The contract of docs/stability.md against the code: every table marked
// <!-- contract: name --> lists exactly what the code defines. A change of
// a code, a key or a bound fails here until the documented contract is
// changed with it (and a removal or a rename is a breaking change: see
// "Versioning" there).

// stabilityTable returns the rows (cells, trimmed) of the table after the
// marker <!-- contract: name -->.
func stabilityTable(t *testing.T, name string) [][]string {
	t.Helper()
	b, err := os.ReadFile("docs/stability.md")
	if err != nil {
		t.Fatal(err)
	}
	marker := "<!-- contract: " + name + " -->"
	_, rest, ok := strings.Cut(string(b), marker)
	if !ok {
		t.Fatalf("docs/stability.md has no %s", marker)
	}
	var rows [][]string
	for i, line := range strings.Split(strings.TrimLeft(rest, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			break
		}
		if i < 2 {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		t.Fatalf("docs/stability.md: the %s table is empty", name)
	}
	return rows
}

var firstCodeSpan = regexp.MustCompile("^`([^`]+)`")

// documented is the first column of a contract table (its code span).
func documented(t *testing.T, name string) []string {
	t.Helper()
	var out []string
	for _, row := range stabilityTable(t, name) {
		m := firstCodeSpan.FindStringSubmatch(row[0])
		if m == nil {
			t.Fatalf("%s: the row %q does not start with a code span", name, row)
		}
		out = append(out, m[1])
	}
	return out
}

func sameSet(t *testing.T, what string, code, doc []string) {
	t.Helper()
	c, d := slices.Clone(code), slices.Clone(doc)
	sort.Strings(c)
	sort.Strings(d)
	c, d = slices.Compact(c), slices.Compact(d)
	for _, v := range c {
		if !slices.Contains(d, v) {
			t.Errorf("%s: %q is in the code, not in docs/stability.md", what, v)
		}
	}
	for _, v := range d {
		if !slices.Contains(c, v) {
			t.Errorf("%s: %q is in docs/stability.md, not in the code", what, v)
		}
	}
}

// constValues reads the string or integer constants of a Go file that keep
// returns true for (the constant's name and its type as written, "" for an
// untyped constant).
func constValues(t *testing.T, file string, keep func(name, typ string) bool) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			typ := ""
			switch x := vs.Type.(type) {
			case *ast.Ident:
				typ = x.Name
			case *ast.SelectorExpr:
				typ = x.Sel.Name
			}
			for i, n := range vs.Names {
				if i >= len(vs.Values) || !keep(n.Name, typ) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					continue
				}
				v := lit.Value
				if lit.Kind == token.STRING {
					v, _ = strconv.Unquote(v)
				}
				out = append(out, v)
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: no constant found", file)
	}
	return out
}

func ofType(want string) func(string, string) bool {
	return func(_, typ string) bool { return typ == want }
}

func named(prefix string) func(string, string) bool {
	return func(name, _ string) bool { return strings.HasPrefix(name, prefix) }
}

func TestStabilityEnumerations(t *testing.T) {
	sameSet(t, "delivery error codes", constValues(t, "internal/core/result.go", ofType("ErrorCode")), documented(t, "delivery-error-codes"))
	sameSet(t, "command error codes", constValues(t, "internal/output/output.go", named("Code")), documented(t, "command-error-codes"))
	sameSet(t, "exit codes", constValues(t, "internal/commandline/subcmd/runtime.go", named("Exit")), documented(t, "exit-codes"))
	sameSet(t, "statuses", constValues(t, "internal/core/result.go", ofType("Status")), documented(t, "statuses"))
	sameSet(t, "acceptances", constValues(t, "internal/core/result.go", ofType("Acceptance")), documented(t, "acceptances"))
	outcomes := append(constValues(t, "internal/core/result.go", ofType("Outcome")), constValues(t, "internal/output/output.go", ofType("Outcome"))...)
	sameSet(t, "outcomes", outcomes, documented(t, "outcomes"))
	sameSet(t, "skip reasons", constValues(t, "internal/routing/routing.go", named("Skip")), documented(t, "skip-reasons"))
	sameSet(t, "levels", constValues(t, "internal/core/level.go", ofType("Level")), documented(t, "levels"))
	sameSet(t, "feather types", configure.FeatherTypes, documented(t, "feather-types"))
	var reserved []string
	for k := range configure.UnsupportedFeatherTypes {
		reserved = append(reserved, k)
	}
	sameSet(t, "reserved feather types", reserved, documented(t, "reserved-feather-types"))
	sameSet(t, "plugin environment", external.BaseEnv, documented(t, "plugin-environment"))
}

// jsonPaths lists the key paths of a JSON document ("results[].history").
func jsonPaths(t *testing.T, doc []byte) []string {
	t.Helper()
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				out = append(out, p)
				walk(p, c)
			}
		case []any:
			for _, c := range x {
				walk(prefix+"[]", c)
			}
		}
	}
	walk("", v)
	return out
}

// The report of a send, with every optional field present, has exactly the
// documented keys; the error documents too.
func TestStabilityJSONDocuments(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	report := core.Aggregate([]core.Result{
		{Feather: "a", Type: "slack", Groups: []string{"g"}, Status: core.StatusFailed, Acceptance: core.AcceptanceRejected, Attempts: 1,
			Error: &core.ErrorInfo{Code: core.ErrCodeRateLimited, Message: "HTTP 429"},
			History: []core.Attempt{{Number: 1, StartedAt: at, Acceptance: core.AcceptanceRejected, Retryable: true, StatusCode: 429,
				RetryAfter: time.Second, Wait: time.Second, Error: &core.ErrorInfo{Code: core.ErrCodeRateLimited, Message: "HTTP 429"}}}},
		{Feather: "b", Type: "discord", Groups: []string{"g"}, Status: core.StatusSkipped, Acceptance: core.AcceptanceNone,
			Skipped: &core.SkipInfo{Reason: "disabled", Group: "g"}},
	}, false, time.Second)
	var buf bytes.Buffer
	if err := output.WriteJSON(&buf, report, core.LevelError, false); err != nil {
		t.Fatal(err)
	}
	keys := jsonPaths(t, buf.Bytes())

	// a send that fails before delivering anything prints its report with
	// the error
	buf.Reset()
	if err := output.WriteErrorJSON(&buf, true, output.CodeConfig, "x"); err != nil {
		t.Fatal(err)
	}
	sameSet(t, "report keys", append(keys, jsonPaths(t, buf.Bytes())...), documented(t, "report-keys"))
	buf.Reset()
	if err := output.WriteErrorJSON(&buf, false, output.CodeConfig, "x"); err != nil {
		t.Fatal(err)
	}
	sameSet(t, "error document keys", jsonPaths(t, buf.Bytes()), documented(t, "error-document-keys"))
	if core.SchemaVersion != 1 {
		t.Errorf("schema version %d: docs/stability.md documents 1", core.SchemaVersion)
	}
}

// tagPaths lists the key paths of a struct by its tags ("toml" or "json"):
// a struct (or a slice of structs) is walked, a map of structs as "<name>",
// any other type is a leaf. stop are the types that are leaves although
// they are structs.
func tagPaths(typ reflect.Type, tag, prefix string, stop ...reflect.Type) []string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get(tag), ",")
		if name == "" || name == "-" {
			continue
		}
		p := prefix + name
		out = append(out, p)
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if slices.Contains(stop, ft) {
			continue
		}
		switch {
		case ft.Kind() == reflect.Struct:
			out = append(out, tagPaths(ft, tag, p+".", stop...)...)
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
			out = append(out, tagPaths(ft.Elem(), tag, p+".", stop...)...)
		case ft.Kind() == reflect.Map && ft.Elem().Kind() == reflect.Struct:
			out = out[:len(out)-1] // "feathers" is a table of names
			out = append(out, tagPaths(ft.Elem(), tag, p+".<name>.", stop...)...)
		}
	}
	return out
}

// Every configuration key, credential source, trust store key and plugin
// protocol field is documented, and nothing else is.
func TestStabilitySchemas(t *testing.T) {
	leaves := []reflect.Type{reflect.TypeFor[credential.Ref](), reflect.TypeFor[configure.Duration](), reflect.TypeFor[time.Time]()}
	sameSet(t, "configuration keys", tagPaths(reflect.TypeFor[configure.File](), "toml", "", leaves...), documented(t, "config-keys"))
	sameSet(t, "credential sources", tagPaths(reflect.TypeFor[credential.Ref](), "toml", "", reflect.TypeFor[credential.KeychainRef]()), documented(t, "credential-sources"))
	sameSet(t, "trust store keys", tagPaths(reflect.TypeFor[configure.TrustStore](), "toml", "", leaves...), documented(t, "trust-store-keys"))
	sameSet(t, "plugin request keys", tagPaths(reflect.TypeFor[external.Request](), "json", "", leaves...), documented(t, "plugin-request-keys"))
	sameSet(t, "plugin response keys", tagPaths(reflect.TypeFor[external.Response](), "json", "", leaves...), documented(t, "plugin-response-keys"))
	if external.ProtocolVersion != 1 {
		t.Errorf("plugin protocol version %d: docs/stability.md documents 1", external.ProtocolVersion)
	}
}

// The defaults and bounds of a send.
func TestStabilityDeliveryBounds(t *testing.T) {
	want := map[string][2]string{
		"timeout":     {configure.DefaultTimeout.String(), configure.MaxTimeout.String()},
		"max_retries": {strconv.Itoa(configure.DefaultMaxRetries), strconv.Itoa(configure.MaxMaxRetries)},
		"concurrency": {strconv.Itoa(configure.DefaultConcurrency), strconv.Itoa(configure.MaxConcurrency)},
		"retry_after": {"", webhook.MaxRetryAfter.String()},
	}
	norm := func(s string) string {
		s = strings.Trim(s, "`")
		if d, err := time.ParseDuration(s); err == nil {
			return d.String()
		}
		return s
	}
	rows := stabilityTable(t, "delivery-bounds")
	if len(rows) != len(want) {
		t.Errorf("%d rows, want %d", len(rows), len(want))
	}
	for _, row := range rows {
		name := strings.Trim(row[0], "`")
		w, ok := want[name]
		if !ok {
			t.Errorf("unknown setting %q", name)
			continue
		}
		if got := [2]string{norm(row[1]), norm(row[2])}; got != w {
			t.Errorf("%s: docs/stability.md says default %q bound %q, the code %q %q", name, got[0], got[1], w[0], w[1])
		}
	}
}
