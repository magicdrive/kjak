package configure

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/credential"
)

const commented = `# my kjak configuration
# (written by hand)

[defaults]
timeout = "20s" # a long deadline

# the team's chat
[feathers.chat]
type = "slack"
webhook_url = { env = "CHAT_HOOK" }

# [feathers.later]
# type = "discord"
`

func updateIn(t *testing.T, content string, fn func(*File) error) (string, UpdateResult) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	res, err := UpdateFile(Paths{Global: path, TrustStore: filepath.Join(dir, "trust.toml")}, ScopeGlobal, time.Now(), fn)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), res
}

// discord is a discord feather reading its webhook from $HOOK.
func discord(levels ...string) FeatherConfig {
	env := "HOOK"
	return FeatherConfig{Type: TypeDiscord, WebhookURL: &credential.Ref{Env: &env}, Levels: levels}
}

func addFeather(name string, fc FeatherConfig) func(*File) error {
	return func(f *File) error {
		if f.Feathers == nil {
			f.Feathers = map[string]FeatherConfig{}
		}
		f.Feathers[name] = fc
		return nil
	}
}

// Adding a feather, a group or a template keeps the file as it was (its
// comments too) and appends the new tables; the result reads back as the
// rewrite would.
func TestUpdateKeepsCommentsWhenItAdds(t *testing.T) {
	got, res := updateIn(t, commented, func(f *File) error {
		if err := addFeather("desk", discord(`error`))(f); err != nil {
			return err
		}
		f.Groups = map[string]GroupConfig{"all": {Feathers: []string{"chat", "desk"}}}
		f.Templates = map[string]TemplateConfig{"done": {Body: "{{.what}} done"}}
		return nil
	})
	if res.CommentsDropped || !strings.HasPrefix(got, commented) {
		t.Fatalf("the file was not kept:\n%s", got)
	}
	added := strings.TrimPrefix(got, commented)
	for _, want := range []string{"[feathers.desk]", "[groups.all]", "[templates.done]"} {
		if !strings.Contains(added, want) {
			t.Errorf("no %s in what was appended:\n%s", want, added)
		}
	}
	if strings.Contains(added, "[feathers]\n") {
		t.Fatalf("a parent table was appended:\n%s", added)
	}
	f, err := Parse([]byte(got))
	if err != nil || f.Feathers["desk"].Type != TypeDiscord || f.Defaults.Timeout == nil {
		t.Fatalf("%v %+v", err, f)
	}
}

// A file kjak wrote ([feathers] defined explicitly) takes appended tables
// too; without comments nothing is reported.
func TestUpdateAppendsToItsOwnLayout(t *testing.T) {
	first, _ := updateIn(t, "", addFeather("a", discord()))
	got, res := updateIn(t, first, addFeather("b", discord()))
	if res.CommentsDropped || !strings.HasPrefix(got, first) || !strings.Contains(got, "[feathers.b]") {
		t.Fatalf("%v\n%s", res, got)
	}
}

// Any other change (a replaced or removed entry, the defaults) rewrites the
// file as before, and reports the comments it dropped.
func TestUpdateRewritesOtherChanges(t *testing.T) {
	for name, fn := range map[string]func(*File) error{
		"replace": addFeather("chat", discord()),
		"remove":  func(f *File) error { delete(f.Feathers, "chat"); return nil },
		"defaults": func(f *File) error {
			s := "error"
			f.Defaults.Level = &s
			return nil
		},
	} {
		got, res := updateIn(t, commented, fn)
		if !res.CommentsDropped || strings.Contains(got, "#") {
			t.Errorf("%s: %+v\n%s", name, res, got)
		}
	}
	// without comments: rewritten, nothing to report
	if _, res := updateIn(t, "[feathers.chat]\ntype = \"discord\"\nwebhook_url = { env = \"HOOK\" }\n", func(f *File) error { delete(f.Feathers, "chat"); return nil }); res.CommentsDropped {
		t.Fatal("reported comments a file did not have")
	}
}

// A file the appended tables would not fit is rewritten (never written so
// that it reads differently).
func TestUpdateAppendFallsBack(t *testing.T) {
	inline := "# inline tables\nfeathers = { chat = { type = \"discord\", webhook_url = { env = \"HOOK\" } } }\n"
	got, res := updateIn(t, inline, addFeather("desk", discord()))
	f, err := Parse([]byte(got))
	if err != nil || len(f.Feathers) != 2 || !res.CommentsDropped {
		t.Fatalf("%v %+v %+v\n%s", err, res, f, got)
	}
}

// A file with CRLF line ends gets CRLF line ends appended.
func TestUpdateAppendKeepsCRLF(t *testing.T) {
	crlf := "# keep me\r\n[feathers.chat]\r\ntype = \"discord\"\r\nwebhook_url = { env = \"HOOK\" }\r\n"
	got, res := updateIn(t, crlf, addFeather("desk", discord()))
	if res.CommentsDropped || !strings.HasPrefix(got, crlf) || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("%+v %q", res, got)
	}
}

func TestHasComments(t *testing.T) {
	for s, want := range map[string]bool{
		"a = 1\n":               false,
		"# x\na = 1\n":          true,
		"a = 1 # x\n":           true,
		"  # indented\n":        true,
		"a = \"x\"#note\n":      true,
		"a = \"x#y\"\n":         true, // one too many, never one too few
		"url = \"https://x\"\n": false,
	} {
		if got := hasComments([]byte(s)); got != want {
			t.Errorf("%q: %v", s, got)
		}
	}
	if !bytes.Contains([]byte(commented), []byte("#")) {
		t.Fatal("fixture")
	}
}
