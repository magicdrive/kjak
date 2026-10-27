package commandline_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The command line of docs/stability.md against the dispatcher: every
// documented command and action exists (its --help succeeds), every action
// the dispatcher knows is documented, and the -o json documents of the
// other commands have the documented top-level keys.

func stabilityRows(t *testing.T, name string) [][]string {
	t.Helper()
	b, err := os.ReadFile("../../docs/stability.md")
	if err != nil {
		t.Fatal(err)
	}
	marker := "<!-- contract: " + name + " -->"
	// a checkout with CRLF line ends (Windows) reads the same
	_, rest, ok := strings.Cut(strings.ReplaceAll(string(b), "\r\n", "\n"), marker)
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

var span = regexp.MustCompile("`([^`]*)`")

// dispatched lists "kjak <noun> <action>" for every action the subcommand
// functions of subcmd dispatch (their switch cases), "kjak send" and "kjak
// version".
func dispatched(t *testing.T) []string {
	t.Helper()
	out := []string{"kjak send", "kjak version"}
	nouns := map[string]string{"Feather": "feather", "Group": "group", "Config": "config", "Template": "template"}
	for fn, noun := range nouns {
		file := "subcmd/" + noun + ".go"
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != fn || fd.Recv != nil {
				continue
			}
			found = true
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				cc, ok := n.(*ast.CaseClause)
				if !ok {
					return true
				}
				for _, e := range cc.List {
					if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, _ := strconv.Unquote(lit.Value)
						if v != "-h" && v != "--help" && v != "help" {
							out = append(out, "kjak "+noun+" "+v)
						}
					}
				}
				return true
			})
		}
		if !found {
			t.Fatalf("%s: no func %s", file, fn)
		}
	}
	sort.Strings(out)
	return out
}

func TestContractCommandsDispatched(t *testing.T) {
	var doc []string
	for _, row := range stabilityRows(t, "commands") {
		doc = append(doc, span.FindStringSubmatch(row[0])[1])
	}
	sort.Strings(doc)
	if got := dispatched(t); !slices.Equal(got, doc) {
		t.Errorf("the dispatcher has\n  %v\ndocs/stability.md lists\n  %v", got, doc)
	}
	e := newEnv(t)
	e.writeGlobal("")
	for _, c := range doc {
		args := append(strings.Fields(c)[1:], "--help")
		if r := e.run(args...); r.code != 0 {
			t.Errorf("%s --help: exit %d %s", c, r.code, r.stderr)
		}
	}
}

// documentKeys are the keys of the command-document-keys row of command
// ("kjak feather show NAME"): those every document has, and those it has
// only when they are set.
type documentKeys struct{ always, whenSet []string }

func commandDocumentKeys(t *testing.T) map[string]documentKeys {
	t.Helper()
	out := map[string]documentKeys{}
	spans := func(cell string) []string {
		var keys []string
		for _, m := range span.FindAllStringSubmatch(cell, -1) {
			keys = append(keys, m[1])
		}
		sort.Strings(keys)
		return keys
	}
	for _, row := range stabilityRows(t, "command-document-keys") {
		if len(row) != 3 {
			t.Fatalf("docs/stability.md command-document-keys: %d cells in %q, want 3", len(row), row)
		}
		out[span.FindStringSubmatch(row[0])[1]] = documentKeys{always: spans(row[1]), whenSet: spans(row[2])}
	}
	return out
}

// documentKeysOf runs a command with -o json and returns the top-level
// keys of its document, sorted.
func documentKeysOf(t *testing.T, e *env, args ...string) ([]string, bool) {
	t.Helper()
	r := e.run(append(args, "-o", "json")...)
	if r.code != 0 {
		t.Errorf("kjak %s -o json: exit %d %s", strings.Join(args, " "), r.code, r.stderr)
		return nil, false
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Errorf("kjak %s -o json: %v\n%s", strings.Join(args, " "), err, r.stdout)
		return nil, false
	}
	var got []string
	for k := range doc {
		got = append(got, k)
	}
	sort.Strings(got)
	return got, true
}

// checkDocumentKeys: every key of always is in got, and every key of got
// is in always or whenSet.
func checkDocumentKeys(t *testing.T, what string, got []string, want documentKeys) {
	t.Helper()
	for _, k := range want.always {
		if !slices.Contains(got, k) {
			t.Errorf("%s: no key %q (keys %v); docs/stability.md lists it always", what, k, got)
		}
	}
	for _, k := range got {
		if !slices.Contains(want.always, k) && !slices.Contains(want.whenSet, k) {
			t.Errorf("%s: key %q is not in docs/stability.md command-document-keys", what, k)
		}
	}
}

func TestContractCommandDocuments(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL) + "[groups.g]\nfeathers = [\"a\"]\n\n[templates.t]\nbody = \"x\"\n")
	// the NAME of a row: the feather, group or template of the fixture
	names := map[string]string{"feather": "a", "group": "g", "template": "t"}
	for command, want := range commandDocumentKeys(t) {
		args := strings.Fields(command)[1:]
		if args[len(args)-1] == "NAME" {
			args[len(args)-1] = names[args[0]]
		}
		got, ok := documentKeysOf(t, e, args...)
		if !ok {
			continue
		}
		checkDocumentKeys(t, command, got, want)
	}
}

// The documents of feather show, group show, template show and template
// render have the keys docs/stability.md lists, for every feather type,
// and every key it lists only when set does appear when it is set. Their
// keys and values for the slack, discord and email feathers, a group and
// a template are those of v0.0.4 (compared with its binary): no
// schema_version, the object itself.
func TestContractShowDocuments(t *testing.T) {
	e := newEnv(t)
	// an absolute path on every OS (not run: show reads the configuration)
	pluginPath := filepath.Join(t.TempDir(), "kjak-feather-pager")
	pathJSON, _ := json.Marshal(pluginPath)
	e.writeGlobal(`[feathers.s]
type = "slack"
webhook_url = { env = "H" }
levels = ["error", "critical"]
enabled = false

[feathers.d]
type = "discord"
webhook_url = { env = "H" }
username = "bot"

[feathers.e]
type = "email"
host = "smtp.example.com"
port = 465
security = "tls"
user = "u"
password = { env = "P" }
from = "a@example.com"
to = ["b@example.com"]
subject_prefix = "[kjak]"

[feathers.h]
type = "http"
url = "https://api.example.com/x"
method = "PUT"
auth = { type = "bearer", token = { env = "T" } }
allow_networks = ["10.0.0.0/8"]

[feathers.x]
type = "external"
plugin = "pager"
path = ` + strconv.Quote(pluginPath) + `
sha256 = "` + strings.Repeat("0", 64) + `"
secrets = { routing_key = { env = "RK" } }
env = ["ALLOWED_X"]

[groups.g]
feathers = ["s", "d"]

[templates.full]
title = "T"
body = "B"
level = "error"
url = "https://example.com/x"

[templates.bare]
body = "B"
`)
	docs := commandDocumentKeys(t)
	seen := map[string]map[string]bool{}
	check := func(command string, args ...string) map[string]json.RawMessage {
		t.Helper()
		want, ok := docs[command]
		if !ok {
			t.Fatalf("docs/stability.md command-document-keys has no row %s", command)
		}
		got, ok := documentKeysOf(t, e, args...)
		if !ok {
			return nil
		}
		checkDocumentKeys(t, strings.Join(args, " "), got, want)
		if seen[command] == nil {
			seen[command] = map[string]bool{}
		}
		for _, k := range got {
			seen[command][k] = true
		}
		var doc map[string]json.RawMessage
		_ = json.Unmarshal([]byte(e.run(append(args, "-o", "json")...).stdout), &doc)
		return doc
	}
	compact := func(raw json.RawMessage) string {
		var b bytes.Buffer
		if err := json.Compact(&b, raw); err != nil {
			return string(raw)
		}
		return b.String()
	}
	values := func(what string, doc map[string]json.RawMessage, want map[string]string) {
		t.Helper()
		for k, v := range want {
			if got := compact(doc[k]); got != v {
				t.Errorf("%s: %s = %s, want %s", what, k, got, v)
			}
		}
		if len(doc) != len(want) {
			t.Errorf("%s: %d keys, want %d (v0.0.4)", what, len(doc), len(want))
		}
	}
	// as v0.0.4 printed them (kjak-v0.0.4 feather show s|d|e -o json, ...)
	values("feather show s", check("kjak feather show NAME", "feather", "show", "s"), map[string]string{
		"name": `"s"`, "origin": `"global"`, "type": `"slack"`, "enabled": `false`, "levels": `["error","critical"]`, "webhook_url": `"env:H"`})
	values("feather show d", check("kjak feather show NAME", "feather", "show", "d"), map[string]string{
		"name": `"d"`, "origin": `"global"`, "type": `"discord"`, "enabled": `true`, "levels": `[]`, "username": `"bot"`, "webhook_url": `"env:H"`})
	values("feather show e", check("kjak feather show NAME", "feather", "show", "e"), map[string]string{
		"name": `"e"`, "origin": `"global"`, "type": `"email"`, "enabled": `true`, "levels": `[]`, "host": `"smtp.example.com"`, "port": `465`,
		"security": `"tls"`, "user": `"u"`, "password": `"env:P"`, "from": `"a@example.com"`, "to": `["b@example.com"]`, "subject_prefix": `"[kjak]"`})
	values("group show g", check("kjak group show NAME", "group", "show", "g"), map[string]string{
		"name": `"g"`, "origin": `"global"`, "feathers": `["s","d"]`})
	values("template show full", check("kjak template show NAME", "template", "show", "full"), map[string]string{
		"name": `"full"`, "origin": `"global"`, "title": `"T"`, "body": `"B"`, "level": `"error"`, "url": `"https://example.com/x"`})
	values("template show bare", check("kjak template show NAME", "template", "show", "bare"), map[string]string{
		"name": `"bare"`, "origin": `"global"`, "title": `""`, "body": `"B"`, "level": `""`, "url": `""`})
	values("template render full", check("kjak template render NAME", "template", "render", "full"), map[string]string{
		"title": `"T"`, "body": `"B"`, "level": `"error"`, "url": `"https://example.com/x"`})
	values("template render bare", check("kjak template render NAME", "template", "render", "bare"), map[string]string{
		"body": `"B"`, "level": `"info"`})
	// the feathers kjak has since v0.0.4
	values("feather show h", check("kjak feather show NAME", "feather", "show", "h"), map[string]string{
		"name": `"h"`, "origin": `"global"`, "type": `"http"`, "enabled": `true`, "levels": `[]`, "url": `"https://api.example.com/x"`,
		"method": `"PUT"`, "auth": `"bearer from env:T"`, "policy": `"allows networks 10.0.0.0/8"`})
	values("feather show x", check("kjak feather show NAME", "feather", "show", "x"), map[string]string{
		"name": `"x"`, "origin": `"global"`, "type": `"external"`, "enabled": `true`, "levels": `[]`, "plugin": `"pager"`,
		"path": string(pathJSON), "sha256": `"` + strings.Repeat("0", 64) + `"`,
		"secrets": `["routing_key from env:RK"]`, "env": `["ALLOWED_X"]`})
	// every key listed only when set was seen set: the list is not stale
	for _, command := range []string{"kjak feather show NAME", "kjak template render NAME"} {
		for _, k := range docs[command].whenSet {
			if !seen[command][k] {
				t.Errorf("%s: docs/stability.md lists %q, no fixture printed it", command, k)
			}
		}
	}
}

// config show -o json names the keys of a group and a template as v0.0.4
// did: the Go field names ("Feathers"; "Title", "Body", "Level", "URL",
// every one present even when empty). They are part of the v1 contract
// (docs/stability.md): a script written for v0.0.4 keeps reading them.
func TestContractConfigShowKeys(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal("[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"H\" }\n\n[groups.g]\nfeathers = [\"a\"]\n\n[templates.t]\ntitle = \"T\"\nbody = \"B\"\nlevel = \"info\"\nurl = \"https://example.com\"\n\n[templates.only_body]\nbody = \"B\"\n")
	r := e.must("config", "show", "-o", "json")
	var doc struct {
		Groups    map[string]map[string]json.RawMessage `json:"groups"`
		Templates map[string]map[string]json.RawMessage `json:"templates"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	keys := func(m map[string]json.RawMessage) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	compact := func(raw json.RawMessage) string {
		var b bytes.Buffer
		if err := json.Compact(&b, raw); err != nil {
			return string(raw)
		}
		return b.String()
	}
	if got := keys(doc.Groups["g"]); !slices.Equal(got, []string{"Feathers"}) {
		t.Errorf("group keys %v, want [Feathers] (v0.0.4)", got)
	}
	if got := compact(doc.Groups["g"]["Feathers"]); got != `["a"]` {
		t.Errorf("group Feathers %s", got)
	}
	want := []string{"Body", "Level", "Title", "URL"}
	for name, values := range map[string]map[string]string{
		"t":         {"Title": `"T"`, "Body": `"B"`, "Level": `"info"`, "URL": `"https://example.com"`},
		"only_body": {"Title": `""`, "Body": `"B"`, "Level": `""`, "URL": `""`},
	} {
		tpl := doc.Templates[name]
		if got := keys(tpl); !slices.Equal(got, want) {
			t.Errorf("template %s keys %v, want %v (v0.0.4)", name, got, want)
		}
		for k, v := range values {
			if got := compact(tpl[k]); got != v {
				t.Errorf("template %s %s = %s, want %s", name, k, got, v)
			}
		}
	}
}

// config validate -o json prints a document on success too (v0.0.4 printed
// the text lines there); a failure is the error document, exit 2.
func TestContractConfigValidateJSON(t *testing.T) {
	type doc struct {
		SchemaVersion  int    `json:"schema_version"`
		Valid          bool   `json:"valid"`
		Global         string `json:"global"`
		GlobalExists   bool   `json:"global_exists"`
		Project        string `json:"project"`
		ProjectState   string `json:"project_state"`
		LiteralSecrets []struct {
			Source, Key string
		} `json:"literal_secrets"`
		Outcome string `json:"outcome"`
		Error   *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decode := func(t *testing.T, stdout string) doc {
		t.Helper()
		var d doc
		dec := json.NewDecoder(strings.NewReader(stdout))
		if err := dec.Decode(&d); err != nil {
			t.Fatalf("not a JSON document: %v\n%s", err, stdout)
		}
		if dec.More() {
			t.Fatalf("more than one document:\n%s", stdout)
		}
		return d
	}
	e := newEnv(t)

	// no global file: valid, and it says so
	r := e.must("config", "validate", "-o", "json")
	d := decode(t, r.stdout)
	if d.SchemaVersion != 1 || !d.Valid || d.GlobalExists || d.Global == "" || d.ProjectState != "none" || d.LiteralSecrets == nil || len(d.LiteralSecrets) != 0 {
		t.Fatalf("no file: %+v\n%s", d, r.stdout)
	}
	if !strings.Contains(r.stdout, `"literal_secrets": []`) {
		t.Fatalf("literal_secrets is not an empty list:\n%s", r.stdout)
	}

	// a literal secret in each file: listed, never its value
	e.writeGlobal("[feathers.a]\ntype = \"slack\"\nwebhook_url = { literal = \"https://hooks.slack.com/services/T/B/GLOBALSECRET\" }\n")
	e.writeProject("[feathers.p]\ntype = \"discord\"\nwebhook_url = { literal = \"https://discord.com/api/webhooks/1/PROJECTSECRET\" }\n")
	r = e.must("config", "validate", "-o", "json")
	d = decode(t, r.stdout)
	if !d.Valid || !d.GlobalExists || d.Project == "" || !strings.HasPrefix(d.ProjectState, "untrusted") || len(d.LiteralSecrets) != 2 ||
		d.LiteralSecrets[0].Source != "global" || d.LiteralSecrets[0].Key != "feathers.a.webhook_url" ||
		d.LiteralSecrets[1].Source != "project" || d.LiteralSecrets[1].Key != "feathers.p.webhook_url" {
		t.Fatalf("literals: %+v\n%s", d, r.stdout)
	}
	if strings.Contains(r.stdout+r.stderr, "SECRET") {
		t.Fatalf("a secret is printed:\n%s%s", r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, "not trusted") {
		t.Fatalf("no notice of the untrusted project on stderr: %q", r.stderr)
	}

	// --strict refuses the literals: the error document
	r = e.run("config", "validate", "--strict", "-o", "json")
	if d := decode(t, r.stdout); r.code != 2 || d.Valid || d.Outcome != "error" || d.Error == nil || d.Error.Code != "config" {
		t.Fatalf("--strict: exit %d %+v\n%s", r.code, d, r.stdout)
	}

	// an invalid file: the error document, as in v0.0.4
	e.writeGlobal("[feathers.a]\ntype = \"nope\"\n")
	r = e.run("config", "validate", "-o", "json")
	if d := decode(t, r.stdout); r.code != 2 || d.Valid || d.Outcome != "error" || d.Error == nil || d.Error.Code != "config" {
		t.Fatalf("invalid: exit %d %+v\n%s", r.code, d, r.stdout)
	}

	// the text output is unchanged
	e.writeGlobal("")
	if r := e.must("config", "validate"); !strings.Contains(r.stdout, ": OK") {
		t.Fatalf("text: %s", r.stdout)
	}
}
