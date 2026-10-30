package commandline_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/kjak/internal/configure"
)

// The documentation is tested like code: every configuration it shows is
// valid, every feather type has a tutorial in English and in Japanese, and
// every kjak command line it shows is one the command line accepts.

const docsRoot = "../../docs"

// tutorials are the tutorials of each feather type (email has two: Gmail and
// any SMTP server). A type without one fails TestDocsTutorialsCoverEveryFeather.
var tutorials = map[string][]string{
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
	return readFile(t, filepath.Join(docsRoot, filepath.FromSlash(rel)))
}

// readFile reads a document with LF line ends (a Windows checkout has CRLF).
func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
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
	for i, f := range files {
		abs, err := filepath.Abs(f) // TestDocsCommandLines changes the directory
		if err != nil {
			t.Fatal(err)
		}
		files[i] = abs
	}
	sort.Strings(files)
	return files
}

var fence = regexp.MustCompile("(?s)```(\\w*)\\n(.*?)```")

// isSecretsFile: a TOML block (or example file) that is a secrets file says
// so on its first line, and has no feather.
func isSecretsFile(s string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.HasPrefix(first, "#") && strings.Contains(first, "secrets.toml") && !strings.Contains(s, "[feathers")
}

// literalSecret: a credential written as a value ({ literal = ... }, the
// deprecated form) anywhere in a configuration.
var literalSecret = regexp.MustCompile(`\{\s*literal\s*=`)

// isWholeConfig: the documents whose TOML blocks are whole configurations
// to copy; the blocks of the references are parts of one.
func isWholeConfig(p string) bool {
	return strings.Contains(p, "tutorials") || strings.Contains(p, "getting-started") || strings.Contains(p, "README")
}

// posixPathOnWindows: the documents write paths the POSIX way
// (/home/me/...), which are not absolute on Windows; that is the only
// refusal tolerated there.
func posixPathOnWindows(err error) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != "invalid configuration:" && !strings.HasSuffix(line, "must be absolute") {
			return false
		}
	}
	return true
}

// TestDocsConfigurations: every example file, and every TOML block of the
// tutorials, the getting started guides and the READMEs, is a whole valid
// configuration (or, when its first line says so, a valid secrets file);
// the TOML blocks of the other documents parse. None holds a literal
// secret, and every feather type has an example file.
func TestDocsConfigurations(t *testing.T) {
	examples, err := filepath.Glob(filepath.Join(docsRoot, "examples", "*.toml"))
	if err != nil || len(examples) < 10 {
		t.Fatalf("examples: %v %v", examples, err)
	}
	check := func(where, doc string, whole bool) {
		t.Helper()
		if isSecretsFile(doc) {
			if _, err := configure.ParseSecrets([]byte(doc)); err != nil {
				t.Errorf("%s (a secrets file): %v", where, err)
			}
			return
		}
		if whole && literalSecret.MatchString(doc) {
			t.Errorf("%s: a literal secret", where)
		}
		if !whole {
			// a part of a configuration: TOML, not necessarily a whole one
			var v map[string]any
			if err := toml.Unmarshal([]byte(doc), &v); err != nil {
				t.Errorf("%s: %v\n%s", where, err, doc)
			}
			return
		}
		f, err := configure.Parse([]byte(doc))
		if err != nil {
			t.Errorf("%s: %v\n%s", where, err, doc)
			return
		}
		if err := configure.Validate(f); err != nil && !posixPathOnWindows(err) {
			t.Errorf("%s: %v", where, err)
		}
	}
	exampleTypes := map[string]bool{}
	for _, p := range examples {
		doc := readFile(t, p)
		check(p, doc, true)
		if f, err := configure.Parse([]byte(doc)); err == nil {
			for _, fc := range f.Feathers {
				exampleTypes[fc.Type] = true
			}
		}
	}
	for _, ty := range configure.FeatherTypes {
		if !exampleTypes[ty] {
			t.Errorf("no example in docs/examples has a feather of type %s", ty)
		}
	}
	blocks := 0
	for _, p := range docFiles(t) {
		if strings.Contains(p, "release-notes") || strings.HasSuffix(p, "migration.md") {
			continue // a release's notes and the migration guide show past forms
		}
		for _, m := range fence.FindAllStringSubmatch(readFile(t, p), -1) {
			if m[1] == "toml" {
				blocks++
				check(p+": a toml block", m[2], isWholeConfig(p))
			}
		}
	}
	if blocks < 40 {
		t.Fatalf("only %d toml blocks found", blocks)
	}
}

var envAssign = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=`)

// commandLines are the kjak commands of the shell blocks of a document:
// continuation lines joined, comments dropped, each line cut at its
// pipes, ";", "&&" and "||" (outside quotes), environment assignments
// dropped. A synopsis ("[-o json]", "NAME...") and a command with a shell
// substitution or redirection are left out.
func commandLines(doc string) [][]string {
	var out [][]string
	for _, m := range fence.FindAllStringSubmatch(doc, -1) {
		if m[1] != "sh" && m[1] != "bash" && m[1] != "console" {
			continue
		}
		text := strings.ReplaceAll(m[2], "\\\n", " ")
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "$ "))
			segments, ok := shellSplit(line)
			if !ok {
				continue
			}
			for _, words := range segments {
				for len(words) > 0 && envAssign.MatchString(words[0].text) {
					words = words[1:]
				}
				if len(words) < 2 || words[0].text != "kjak" || isSynopsis(words) {
					continue
				}
				args := make([]string, 0, len(words)-1)
				for _, w := range words[1:] {
					args = append(args, w.text)
				}
				out = append(out, args)
			}
		}
	}
	return out
}

// word is a word of a command line; quoted: some of it was in quotes.
type word struct {
	text   string
	quoted bool
}

// placeholder: the words a synopsis uses for what the reader fills in
// (KJAK_DEV_SLACK_WEBHOOK and the like are real names, not placeholders).
var placeholder = regexp.MustCompile(`^(NAME|FILE|DIR|PATH|VAR|TYPE|T|LEVEL|L|TEXT|BODY|TITLE|URL|KEY|VALUE|KEY=VALUE|SCOPE|FEATHER|GROUP|TEMPLATE|S/A|N)$`)

// isSynopsis: a usage line ("kjak feather list [-o json]", "NAME...",
// "--type T"), not a command; only unquoted words count ("[kjak]" in
// quotes is a value).
func isSynopsis(words []word) bool {
	for _, w := range words {
		if !w.quoted && (strings.HasPrefix(w.text, "[") || strings.Contains(w.text, "...") || strings.Contains(w.text, "|") || placeholder.MatchString(w.text)) {
			return true
		}
	}
	return false
}

// readsStdin: a command that needs what the reader types or pipes.
func readsStdin(args []string) bool {
	for i, a := range args {
		if a == "--stdin" || a == "--secret-stdin" || a == "--body-stdin" || a == "-" && i > 0 && args[i-1] == "--json-input" {
			return true
		}
	}
	return false
}

// shellSplit splits a simple command line ('...' and "..." quotes, # comments)
// into its commands (separated by |, ;, && and ||); false for a line with a
// substitution, a redirection or an unterminated quote.
func shellSplit(s string) ([][]word, bool) {
	var segments [][]word
	var words []word
	var cur strings.Builder
	in, quoted, quote := false, false, byte(0)
	flush := func() {
		if in {
			words = append(words, word{cur.String(), quoted})
			cur.Reset()
			in, quoted = false, false
		}
	}
	endSegment := func() {
		flush()
		if len(words) > 0 {
			segments = append(segments, words)
		}
		words = nil
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if quote == '"' && (c == '$' || c == '`') {
				return nil, false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, in, quoted = c, true, true
		case c == '#' && !in:
			endSegment()
			return segments, true
		case c == ' ' || c == '\t':
			flush()
		case c == '|' || c == ';' || c == '&':
			if c == '&' && (i+1 >= len(s) || s[i+1] != '&') {
				return nil, false // a background job
			}
			if i+1 < len(s) && s[i+1] == c {
				i++
			}
			endSegment()
		case c == '<' || c == '>' || c == '`' || c == '$' && i+1 < len(s) && (s[i+1] == '(' || s[i+1] == '{'):
			return nil, false
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	endSegment()
	return segments, true
}

// sandboxRefusal: what a documented command may say in the sandbox, where
// only what the document's own commands created exists (a feather, group or
// template it shows in a file, a project, a variable, a secret); any other
// refusal (exit 2) is a mistake in the document: an unknown command, flag,
// level, type or a value of the wrong form. ("exists in the ...
// configuration": the getting started guides show the same feather in the
// walkthrough and in its own section.)
var sandboxRefusal = regexp.MustCompile(`no (feather|group|template)( or group)? named|unknown feather or group|no project configuration|no configuration file|environment variable \S+ is not set|no secrets file is available|secrets file \S+ does not exist|is not in the .*secrets file|no such keychain entry|template error: open \S+: |exists in the (global|project) configuration \(--force replaces it\)|routing error`)

// TestDocsCommandLines runs the kjak commands of each document in order in a
// sandbox of its own (a tutorial's later commands use what its earlier ones
// created). None may be refused for its own form. A send runs as a dry run
// (--dry-run added): nothing is ever sent; a command that reads the
// terminal or a pipe is left out.
func TestDocsCommandLines(t *testing.T) {
	n := 0
	for _, p := range docFiles(t) {
		doc := readFile(t, p)
		e := newEnv(t)
		// relative paths of a command (-c ci.toml) are the sandbox's
		t.Chdir(e.project)
		for _, args := range commandLines(doc) {
			if readsStdin(args) {
				continue
			}
			if args[0] == "send" {
				// before any "--": after it, it would be the message
				args = append([]string{"send", "--dry-run"}, args[1:]...)
			}
			n++
			r := e.run(args...)
			if r.code == 2 && !sandboxRefusal.MatchString(r.stderr) {
				t.Errorf("%s: kjak %s: %s", p, strings.Join(args, " "), r.stderr)
			}
		}
	}
	if n < 100 {
		t.Fatalf("only %d command lines found", n)
	}
}

func TestShellSplit(t *testing.T) {
	for line, want := range map[string]string{
		`kjak send "a # b"   # comment`:                    `[[kjak send a # b]]`,
		`kjak send -o json "x" | jq .status`:               `[[kjak send -o json x] [jq .status]]`,
		`make test; kjak send --template done`:             `[[make test] [kjak send --template done]]`,
		`kjak config validate && kjak send --to a "b"`:     `[[kjak config validate] [kjak send --to a b]]`,
		`K=V kjak send x || echo failed`:                   `[[K=V kjak send x] [echo failed]]`,
		`kjak feather add g --subject-prefix "[kjak]" --x`: `[[kjak feather add g --subject-prefix [kjak] --x]]`,
	} {
		segs, ok := shellSplit(line)
		var got [][]string
		for _, s := range segs {
			var ws []string
			for _, w := range s {
				ws = append(ws, w.text)
			}
			got = append(got, ws)
		}
		if !ok || fmt.Sprint(got) != want {
			t.Errorf("%q: %v %v", line, got, ok)
		}
	}
	for _, line := range []string{`kjak send "$(date)"`, `kjak send x > out`, `kjak send x &`, "kjak send `date`", `kjak send "open`} {
		if _, ok := shellSplit(line); ok {
			t.Errorf("%q was taken", line)
		}
	}
	if !isSynopsis([]word{{"kjak", false}, {"[-o", false}}) || isSynopsis([]word{{"--subject-prefix", false}, {"[kjak]", true}}) ||
		!isSynopsis([]word{{"show", false}, {"NAME", false}}) || isSynopsis([]word{{"--secret-env", false}, {"KJAK_DEV_SLACK_WEBHOOK", false}}) {
		t.Fatal("isSynopsis")
	}
}
