package commandline_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// readableByOthers makes path readable by other users: mode 0644 on Unix,
// an ACE for Everyone on Windows (where the mode bits mean nothing). It
// returns what kjak names as the reason.
func readableByOthers(t *testing.T, e *env, path, content string) string {
	t.Helper()
	e.writeFile(path, content, 0o644)
	if runtime.GOOS != "windows" {
		return "group or others have access (mode 0644)"
	}
	if out, err := exec.Command("icacls", path, "/grant", "*S-1-1-0:(R)").CombinedOutput(); err != nil {
		t.Fatalf("icacls: %v %s", err, out)
	}
	return "Everyone"
}

const notPrivate = "keeps literal secrets and is not private"

// A configuration file with a literal secret that other users can read is
// reported (never the secret), and only that: the send still works (a
// literal is deprecated, not refused), a private file or a file without
// literals gets no such warning, and --strict refuses the literal with the
// reason.
func TestCritical_LiteralSecretInReadableFileWarns(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	global := filepath.Join(e.xdg, "kjak", "config.toml")
	literal := "[feathers.a]\ntype = \"slack\"\nwebhook_url = { literal = \"" + p.URL + "/T/B/LITERALSECRET\" }\n"
	noSecret := func(what string, r result) {
		t.Helper()
		if strings.Contains(r.stdout+r.stderr, "LITERALSECRET") {
			t.Fatalf("%s prints the secret:\n%s%s", what, r.stdout, r.stderr)
		}
	}

	// private: the deprecation warning only
	e.writeFile(global, literal, 0o600)
	r := e.must("send", "--to", "a", "hi")
	if !strings.Contains(r.stderr, "literal (deprecated)") || strings.Contains(r.stderr, notPrivate) {
		t.Fatalf("a private file: %s", r.stderr)
	}

	// readable by others: warned, with the reason, and still sent
	why := readableByOthers(t, e, global, literal)
	r = e.must("send", "--to", "a", "hi")
	if !strings.Contains(r.stderr, notPrivate) || !strings.Contains(r.stderr, why) || !strings.Contains(r.stderr, "rotate") || p.count() != 2 {
		t.Fatalf("a readable file: %d sent\n%s", p.count(), r.stderr)
	}
	noSecret("send", r)
	r = e.must("config", "validate")
	if !strings.Contains(r.stderr, notPrivate) || !strings.Contains(r.stderr, why) {
		t.Fatalf("validate: %s", r.stderr)
	}
	noSecret("validate", r)
	r = e.run("config", "validate", "--strict")
	// Windows words the reason "the access control list grants access to
	// others: Everyone (S-1-1-0) ...": the reason, not its first word
	if r.code != 2 || !strings.Contains(r.stderr, "(and the file is not private: ") || !strings.Contains(r.stderr, why) {
		t.Fatalf("validate --strict: exit %d %s", r.code, r.stderr)
	}
	noSecret("validate --strict", r)

	// readable, without a literal: nothing to report
	readableByOthers(t, e, global, "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"HOOK\" }\n")
	e.vars["HOOK"] = p.URL
	if r := e.must("send", "--to", "a", "hi"); strings.Contains(r.stderr, notPrivate) || strings.Contains(r.stderr, "literal") {
		t.Fatalf("no literal: %s", r.stderr)
	}

	// a trusted project file (usually committed, readable by anyone)
	e.writeGlobal("")
	why = readableByOthers(t, e, filepath.Join(e.project, ".kjak.toml"), strings.Replace(literal, "[feathers.a]", "[feathers.a]\nallow_networks = [\"127.0.0.1/32\", \"::1/128\"]", 1))
	e.must("config", "trust", "--yes")
	r = e.must("send", "--to", "a", "hi")
	if !strings.Contains(r.stderr, notPrivate) || !strings.Contains(r.stderr, why) || !strings.Contains(r.stderr, ".kjak.toml") {
		t.Fatalf("a project file: %s", r.stderr)
	}
	noSecret("send (project)", r)
}
