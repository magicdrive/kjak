package commandline_test

import (
	"strings"
	"testing"
)

// noEscapes fails when out holds a terminal control character (ESC, BEL,
// a C1 control): text from an untrusted project file must reach the
// terminal escaped.
func noEscapes(t *testing.T, what, out string) {
	t.Helper()
	for _, r := range out {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("%s holds the control character %U:\n%q", what, r, out)
		}
	}
}

// An untrusted .kjak.toml cannot write terminal escapes through kjak: an
// unknown key, a value the trust shows, an error.
func TestCritical_UntrustedProjectCannotWriteTerminalEscapes(t *testing.T) {
	// TOML escapes: the file holds the control characters once parsed
	const esc = `\u001b]0;owned\u0007\u001b[2J\u009b31m`
	for _, c := range []struct {
		name, project string
		refused       bool
	}{
		{"unknown key", "[feathers.x]\ntype = \"slack\"\n\"k" + esc + "\" = 1\n", true},
		{"email host", "[feathers.m]\ntype = \"email\"\nhost = \"smtp" + esc + ".example\"\nfrom = \"a@example.com\"\nto = [\"b@example.com\"]\n", true},
		{"subject prefix", "[feathers.m]\ntype = \"email\"\nhost = \"smtp.example\"\nsubject_prefix = \"[x]" + esc + "\"\nfrom = \"a@example.com\"\nto = [\"b@example.com\"]\n", true},
		{"discord username", "[feathers.d]\ntype = \"discord\"\nusername = \"bot" + esc + "\"\nwebhook_url = { env = \"HOOK\" }\n", true},
		{"plugin setting", "[feathers.p]\ntype = \"external\"\nplugin = \"pager\"\nsettings = { mode = \"ok" + esc + "\" }\n", true},
		{"keychain account", "[feathers.s]\ntype = \"slack\"\nwebhook_url = { keychain = { service = \"kjak\", account = \"a" + esc + "\" } }\n", true},
		{"http query key", "[feathers.h]\ntype = \"http\"\nurl = \"https://api.example/x\"\nquery = { \"k" + esc + "\" = \"v\" }\n", true},
		{"group member", "[feathers.s]\ntype = \"slack\"\nwebhook_url = { env = \"HOOK\" }\n[groups.g]\nfeathers = [\"s\", \"t" + esc + "\"]\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.writeProject(c.project)
			for _, args := range [][]string{
				{"config", "trust", "--yes", "--allow-user-secrets"},
				{"config", "validate"},
				{"config", "path"},
				{"send", "--to", "s", "hi"},
			} {
				r := e.run(args...)
				noEscapes(t, strings.Join(args, " ")+" stdout", r.stdout)
				noEscapes(t, strings.Join(args, " ")+" stderr", r.stderr)
				if args[1] == "trust" && c.refused && r.code == 0 {
					t.Fatalf("trusted: %+v", r)
				}
			}
		})
	}
}

// A template keeps its line breaks, but the commands that print it escape
// every other control character.
func TestTemplateTextIsPrintedEscaped(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal("[templates.t]\ntitle = \"T\\u001b[31m\"\nbody = \"line one\\nline \\u001b]0;x\\u0007two\"\n")
	for _, args := range [][]string{
		{"template", "show", "t", "--no-project"},
		{"template", "list", "--no-project"},
	} {
		r := e.run(args...)
		if r.code != 0 {
			t.Fatalf("%v: %+v", args, r)
		}
		noEscapes(t, strings.Join(args, " "), r.stdout+r.stderr)
	}
	if r := e.must("template", "show", "t", "--no-project"); !strings.Contains(r.stdout, "line one\n") || !strings.Contains(r.stdout, `line \x1b]0;x\x07two`) {
		t.Fatal(r.stdout)
	}
}
