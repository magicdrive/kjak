package commandline_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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

func TestContractCommandDocuments(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL) + "[groups.g]\nfeathers = [\"a\"]\n\n[templates.t]\nbody = \"x\"\n")
	for _, row := range stabilityRows(t, "command-document-keys") {
		command := span.FindStringSubmatch(row[0])[1]
		var want []string
		for _, m := range span.FindAllStringSubmatch(row[1], -1) {
			want = append(want, m[1])
		}
		r := e.run(append(strings.Fields(command)[1:], "-o", "json")...)
		if r.code != 0 {
			t.Errorf("%s -o json: exit %d %s", command, r.code, r.stderr)
			continue
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
			t.Errorf("%s -o json: %v\n%s", command, err, r.stdout)
			continue
		}
		var got []string
		for k := range doc {
			got = append(got, k)
		}
		sort.Strings(got)
		sort.Strings(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s -o json: keys %v, docs/stability.md lists %v", command, got, want)
		}
	}
}

// config show -o json names the keys of a group and a template as the
// configuration does (v0.0.4 printed the Go field names, "Feathers" and
// "Body").
func TestContractConfigShowKeys(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal("[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"H\" }\n\n[groups.g]\nfeathers = [\"a\"]\n\n[templates.t]\ntitle = \"T\"\nbody = \"B\"\nlevel = \"info\"\nurl = \"https://example.com\"\n")
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
	if got := keys(doc.Groups["g"]); !slices.Equal(got, []string{"feathers"}) {
		t.Errorf("group keys %v", got)
	}
	if got := keys(doc.Templates["t"]); !slices.Equal(got, []string{"body", "level", "title", "url"}) {
		t.Errorf("template keys %v", got)
	}
}
