package commandline_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/configure"
)

// The documentation is tested like code: every configuration it shows is
// valid, every feather type has a tutorial in English and in Japanese, and
// every kjak command line it shows is one the command line accepts.

const docsRoot = "../../docs"

// tutorials are the tutorials of each feather type (email has two: Gmail and
// any SMTP server). A type without one fails TestDocsTutorialsCoverEveryFeather.
var tutorials = map[string][]string{
	configure.TypeOS:       {"os"},
	configure.TypeDiscord:  {"discord"},
	configure.TypeSlack:    {"slack"},
	configure.TypeEmail:    {"gmail", "smtp"},
	configure.TypeHTTP:     {"http"},
	configure.TypeExternal: {"plugin"},
}

// tutorialSections are the sections every tutorial has, in this order.
var tutorialSections = []string{"Overview", "Prerequisites", "Setup", "Minimal configuration", "CLI configuration", "First notification", "Advanced configuration", "Secrets", "Troubleshooting", "Security notes", "Related references"}

// tutorialSectionsJa are their Japanese titles.
var tutorialSectionsJa = []string{"概要", "前提条件", "準備", "最小構成", "CLI での設定", "最初の通知", "応用的な設定", "秘密情報", "トラブルシューティング", "セキュリティ上の注意", "関連資料"}

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(docsRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDocsTutorialsCoverEveryFeather(t *testing.T) {
	for _, ty := range configure.FeatherTypes {
		names, ok := tutorials[ty]
		if !ok {
			t.Errorf("feather type %s has no tutorial", ty)
			continue
		}
		for _, n := range names {
			for _, lang := range []struct {
				file     string
				sections []string
			}{{"tutorials/" + n + ".md", tutorialSections}, {"tutorials/" + n + "_ja.md", tutorialSectionsJa}} {
				doc := readDoc(t, lang.file)
				// the sections, in order (## N. Title)
				last := -1
				for i, s := range lang.sections {
					re := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(strconv.Itoa(i+1)) + `\. ` + regexp.QuoteMeta(s) + `[ \t]*$`)
					loc := re.FindStringIndex(doc)
					if loc == nil {
						t.Errorf("%s: no section %q", lang.file, strconv.Itoa(i+1)+". "+s)
						continue
					}
					if loc[0] < last {
						t.Errorf("%s: section %q is out of order", lang.file, s)
					}
					last = loc[0]
				}
				if !strings.Contains(doc, "type = \""+ty+"\"") {
					t.Errorf("%s: no configuration of type %s", lang.file, ty)
				}
				// both indexes list it
				for _, index := range []string{"tutorials/README.md", "tutorials/README_ja.md"} {
					want := n + ".md"
					if strings.HasSuffix(index, "_ja.md") {
						want = n + "_ja.md"
					}
					if !strings.Contains(readDoc(t, index), "("+want+")") {
						t.Errorf("%s does not link to %s", index, want)
					}
				}
			}
		}
	}
}

// docFiles are the documents whose configurations and commands are tested.
func docFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(docsRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "../../README.md", "../../README_ja.md")
	sort.Strings(files)
	return files
}

var fence = regexp.MustCompile("(?s)```(\\w*)\\n(.*?)```")

// isSecretsFile: a TOML block (or example file) that is a secrets file says
// so on its first line.
func isSecretsFile(s string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.HasPrefix(first, "#") && strings.Contains(first, "secrets.toml") && !strings.Contains(first, "next to this file")
}

// TestDocsConfigurations: every example file and every TOML block of the
// tutorials, the getting started guides and the README is a whole valid
// configuration (or, when its first line says so, a valid secrets file).
func TestDocsConfigurations(t *testing.T) {
	examples, err := filepath.Glob(filepath.Join(docsRoot, "examples", "*.toml"))
	if err != nil || len(examples) < 10 {
		t.Fatalf("examples: %v %v", examples, err)
	}
	check := func(where, doc string) {
		t.Helper()
		if isSecretsFile(doc) {
			if _, err := configure.ParseSecrets([]byte(doc)); err != nil {
				t.Errorf("%s (a secrets file): %v", where, err)
			}
			return
		}
		f, err := configure.Parse([]byte(doc))
		if err != nil {
			t.Errorf("%s: %v\n%s", where, err, doc)
			return
		}
		if err := configure.Validate(f); err != nil {
			t.Errorf("%s: %v", where, err)
		}
		for name, fc := range f.Feathers {
			if fc.WebhookURL != nil && fc.WebhookURL.Literal != nil || fc.Password != nil && fc.Password.Literal != nil {
				t.Errorf("%s: feather %s has a literal secret", where, name)
			}
		}
	}
	for _, p := range examples {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		check(p, string(b))
	}
	blocks := 0
	for _, p := range docFiles(t) {
		if !strings.Contains(p, "tutorials") && !strings.Contains(p, "getting-started") && !strings.Contains(p, "README") {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range fence.FindAllStringSubmatch(string(b), -1) {
			if m[1] == "toml" {
				blocks++
				check(p+": a toml block", m[2])
			}
		}
	}
	if blocks < 20 {
		t.Fatalf("only %d toml blocks found", blocks)
	}
}

// commandLines are the kjak command lines of the shell blocks of a document:
// continuation lines joined, environment assignments and comments dropped;
// a line with a pipe, a substitution or a redirection is left out (it is
// not kjak alone).
func commandLines(doc string) [][]string {
	var out [][]string
	for _, m := range fence.FindAllStringSubmatch(doc, -1) {
		if m[1] != "sh" && m[1] != "bash" && m[1] != "console" && m[1] != "powershell" {
			continue
		}
		text := strings.ReplaceAll(m[2], "\\\n", " ")
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "$ "))
			if i := strings.Index(line, " #"); i >= 0 {
				line = line[:i]
			}
			words, ok := shellSplit(line)
			if !ok || isSynopsis(words) {
				continue
			}
			for len(words) > 0 && regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=`).MatchString(words[0]) {
				words = words[1:]
			}
			if len(words) > 1 && words[0] == "kjak" {
				out = append(out, words[1:])
			}
		}
	}
	return out
}

// isSynopsis: a usage line ("kjak feather list [-o json]", "NAME..."),
// not a command.
func isSynopsis(words []string) bool {
	for _, w := range words {
		if strings.HasPrefix(w, "[") || strings.Contains(w, "...") || strings.Contains(w, "|") {
			return true
		}
	}
	return false
}

// shellSplit splits a simple command line ('...' and "..." quotes); false
// for what it does not handle.
func shellSplit(s string) ([]string, bool) {
	if strings.ContainsAny(s, "|<>;`") || strings.Contains(s, "$(") || strings.Contains(s, "&&") {
		return nil, false
	}
	var words []string
	var cur strings.Builder
	in, quote := false, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, in = c, true
		case c == ' ' || c == '\t':
			if in {
				words = append(words, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if in {
		words = append(words, cur.String())
	}
	return words, true
}

// TestDocsCommandLines runs every kjak command line of the documentation in
// an empty sandbox: none may be a usage error (an unknown command, flag or
// value). Nothing is sent: the sandbox has no feather, so a send stops at
// its routing.
func TestDocsCommandLines(t *testing.T) {
	n := 0
	for _, p := range docFiles(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range commandLines(string(b)) {
			n++
			e := newEnv(t)
			rec := &osRecorder{}
			r := e.runWith(rec.with, args...)
			if strings.Contains(r.stderr, "--help' for usage") || strings.Contains(r.stderr, "unknown command") || strings.Contains(r.stderr, "unknown action") {
				t.Errorf("%s: kjak %s: %s", p, strings.Join(args, " "), r.stderr)
			}
			if len(rec.shown) != 0 {
				t.Fatalf("%s: kjak %s showed a notification", p, strings.Join(args, " "))
			}
		}
	}
	if n < 50 {
		t.Fatalf("only %d command lines found", n)
	}
}
