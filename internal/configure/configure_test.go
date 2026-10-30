package configure

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

const globalTOML = `
[defaults]
timeout = "20s"
max_retries = 3
level = "warning"

[feathers.dev_slack]
type = "slack"
webhook_url = { env = "DEV_SLACK" }
levels = ["error"]

[feathers.ops_mail]
type = "email"
host = "smtp.example.com"
user = "kjak"
password = { keychain = { service = "kjak", account = "ops" } }
from = "kjak@example.com"
to = ["ops@example.com"]

[groups.development]
feathers = ["dev_slack"]

[templates.deploy]
body = "{{.service}} deployed"
level = "success"
`

const projectTOML = `
[defaults]
max_retries = 1

[feathers.dev_slack]
type = "discord"
webhook_url = { env = "PROJECT_HOOK" }

[groups.development]
feathers = ["dev_slack", "ops_mail"]
`

func mustParse(t *testing.T, s string) *File {
	t.Helper()
	f, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestMergeDefaultsFieldWiseAndEntriesAtomic(t *testing.T) {
	m := Merge(mustParse(t, globalTOML), mustParse(t, projectTOML))
	s := m.Settings()
	if s.Timeout != 20*time.Second || s.MaxRetries != 1 || s.Level != "warning" || s.Concurrency != DefaultConcurrency {
		t.Fatalf("settings = %+v", s)
	}
	f := m.Feathers["dev_slack"]
	// replaced whole: no "levels" of the global entry survives
	if f.Type != "discord" || len(f.Levels) != 0 || *f.WebhookURL.Env != "PROJECT_HOOK" {
		t.Fatalf("dev_slack = %+v", f)
	}
	if g := m.Groups["development"].Feathers; len(g) != 2 {
		t.Fatalf("group = %v", g)
	}
	if _, ok := m.Feathers["ops_mail"]; !ok {
		t.Fatal("a global feather is lost")
	}
	if _, ok := m.Templates["deploy"]; !ok {
		t.Fatal("a global template is lost")
	}
}

func TestMergeDoesNotAliasInputs(t *testing.T) {
	g := mustParse(t, globalTOML)
	m := Merge(g, nil)
	m.Groups["development"].Feathers[0] = "changed"
	*m.Defaults.MaxRetries = 9
	if g.Groups["development"].Feathers[0] != "dev_slack" || *g.Defaults.MaxRetries != 3 {
		t.Fatal("Merge shares memory with its input")
	}
}

func TestSettingsDefaults(t *testing.T) {
	s := (&File{}).Settings()
	if s.Timeout != 10*time.Second || s.MaxRetries != 2 || s.Concurrency != 5 || s.Level != "info" {
		t.Fatalf("defaults = %+v", s)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"unknown key":       "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"X\" }\nwebhok = 1\n",
		"two sources":       "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"X\", literal = \"https://x\" }\n",
		"no source":         "[feathers.a]\ntype = \"slack\"\nwebhook_url = {}\n",
		"plain string url":  "[feathers.a]\ntype = \"slack\"\nwebhook_url = \"https://hooks\"\n",
		"os feather":        "[feathers.a]\ntype = \"os\"\n",
		"unknown type":      "[feathers.a]\ntype = \"teams\"\n",
		"slack with host":   "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"X\" }\nhost = \"h\"\n",
		"email no to":       "[feathers.a]\ntype = \"email\"\nhost = \"h\"\nfrom = \"a@example.com\"\n",
		"email clear auth":  "[feathers.a]\ntype = \"email\"\nhost = \"h\"\nsecurity = \"none\"\nuser = \"u\"\npassword = { env = \"P\" }\nfrom = \"a@example.com\"\nto = [\"b@example.com\"]\n",
		"email bad address": "[feathers.a]\ntype = \"email\"\nhost = \"h\"\nfrom = \"a@example.com\\r\\nBcc: x@evil\"\nto = [\"b@example.com\"]\n",
		"bad level filter":  "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"X\" }\nlevels = [\"loud\"]\n",
		"bad timeout":       "[defaults]\ntimeout = \"soon\"\n",
		"zero concurrency":  "[defaults]\nconcurrency = 0\n",
		"bad name":          "[feathers.\"a b\"]\ntype = \"slack\"\nwebhook_url = { env = \"X\" }\n",
		"empty group":       "[groups.g]\nfeathers = []\n",
		"bad env name":      "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"1X\" }\n",
		"not toml":          "[feathers\n",
		"template no body":  "[templates.t]\ntitle = \"x\"\n",
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		} else if !errors.Is(err, ErrInvalidConfig) && !strings.Contains(err.Error(), "credential") {
			t.Errorf("%s: %v is not a configuration error", name, err)
		}
	}
}

func TestValidateReferences(t *testing.T) {
	nested := mustParse(t, "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"X\" }\n[groups.inner]\nfeathers = [\"a\"]\n[groups.outer]\nfeathers = [\"inner\"]\n")
	if err := ValidateReferences(nested); err == nil || !strings.Contains(err.Error(), "nested groups are not supported") {
		t.Fatalf("nesting: %v", err)
	}
	missing := mustParse(t, "[groups.g]\nfeathers = [\"nobody\"]\n")
	if err := ValidateReferences(missing); err == nil {
		t.Fatal("a missing member is accepted")
	}
	clash := mustParse(t, "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"X\" }\n[groups.a]\nfeathers = [\"a\"]\n")
	if err := ValidateReferences(clash); err == nil {
		t.Fatal("a name that is a feather and a group is accepted")
	}
	target := mustParse(t, "[defaults]\ntargets = [\"ghost\"]\n")
	if err := ValidateReferences(target); err == nil {
		t.Fatal("an unknown default target is accepted")
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	f := mustParse(t, globalTOML)
	b, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Parse(b)
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	if fmt.Sprint(g.Settings()) != fmt.Sprint(f.Settings()) || len(g.Feathers) != 2 || g.Feathers["ops_mail"].Password.Keychain.Account != "ops" {
		t.Fatalf("round trip lost data:\n%s", b)
	}
}

// --- paths, trust, load ---

type fixture struct {
	dir   string
	paths Paths
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real // macOS: /var -> /private/var
	}
	cfg := filepath.Join(dir, "xdg", "kjak")
	proj := filepath.Join(dir, "project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture{dir: dir, paths: Paths{
		Global:     filepath.Join(cfg, "config.toml"),
		Project:    filepath.Join(proj, ProjectFileName),
		TrustStore: filepath.Join(cfg, "trust.toml"),
	}}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// absPath is an absolute path on this OS ("/xdg" is not absolute on
// Windows, where it would be ignored like a relative XDG_CONFIG_HOME).
func absPath(p string) string {
	if runtime.GOOS == "windows" {
		return `C:` + filepath.FromSlash(p)
	}
	return p
}

func TestDefaultPaths(t *testing.T) {
	xdg, home, work := absPath("/xdg"), absPath("/home/u"), absPath("/work")
	env := Env{
		Getenv: func(k string) string {
			return map[string]string{"XDG_CONFIG_HOME": xdg}[k]
		},
		HomeDir: func() (string, error) { return home, nil },
		Getwd:   func() (string, error) { return work, nil },
		GOOS:    "linux",
	}
	p, err := DefaultPaths(env, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Global != filepath.Join(xdg, "kjak", "config.toml") || p.Project != filepath.Join(work, ".kjak.toml") || p.TrustStore != filepath.Join(xdg, "kjak", "trust.toml") {
		t.Fatalf("paths = %+v", p)
	}
	env.Getenv = func(k string) string { return map[string]string{"KJAK_CONFIG": "/etc/kjak.toml"}[k] }
	p, _ = DefaultPaths(env, "", true)
	if p.Global != "/etc/kjak.toml" || p.Project != "" || p.TrustStore != filepath.Join(home, ".config", "kjak", "trust.toml") {
		t.Fatalf("paths = %+v", p)
	}
	p, _ = DefaultPaths(env, "/flag.toml", false)
	if p.Global != "/flag.toml" {
		t.Fatalf("--config: %+v", p)
	}
	// a relative XDG_CONFIG_HOME is ignored (XDG Base Directory spec)
	env.Getenv = func(k string) string { return map[string]string{"XDG_CONFIG_HOME": "rel"}[k] }
	p, _ = DefaultPaths(env, "", true)
	if !strings.HasPrefix(p.Global, filepath.Join(home, ".config")) {
		t.Fatalf("relative XDG: %+v", p)
	}
}

func TestLoadWithoutFiles(t *testing.T) {
	fx := newFixture(t)
	l, err := Load(fx.paths)
	if err != nil {
		t.Fatal(err)
	}
	if l.Global.Exists || l.Project.Exists || l.ProjectTrusted || len(l.Effective.Feathers) != 0 {
		t.Fatalf("loaded = %+v", l)
	}
}

func TestCritical_UntrustedProjectRefused(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	write(t, fx.paths.Project, projectTOML)
	_, err := Load(fx.paths)
	if !errors.Is(err, ErrUntrustedProject) {
		t.Fatalf("err = %v", err)
	}

	// trusted: used
	src, _ := ReadFile(fx.paths.Project)
	if err := Trust(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0); err != nil {
		t.Fatal(err)
	}
	l, err := Load(fx.paths)
	if err != nil || !l.ProjectTrusted || l.Effective.Feathers["dev_slack"].Type != "discord" {
		t.Fatalf("trusted load: %v", err)
	}
	if l.Origin("feather", "dev_slack") != "project" || l.Origin("feather", "ops_mail") != "global" {
		t.Fatal("origins")
	}

	// one byte changed: refused again
	write(t, fx.paths.Project, projectTOML+"\n")
	if _, err := Load(fx.paths); !errors.Is(err, ErrUntrustedProject) {
		t.Fatalf("changed file: err = %v", err)
	}

	// --no-project: the global file alone
	np := fx.paths
	np.Project = ""
	if l, err := Load(np); err != nil || l.Effective.Feathers["dev_slack"].Type != "slack" {
		t.Fatalf("no project: %v", err)
	}
}

func TestTrustUsesCanonicalPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Log("symbolic links need privileges on Windows; the canonical path is checked without one")
	}
	fx := newFixture(t)
	write(t, fx.paths.Project, projectTOML)
	write(t, fx.paths.Global, globalTOML)
	src, _ := ReadFile(fx.paths.Project)
	if err := Trust(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0); err != nil {
		t.Fatal(err)
	}
	ts, err := ReadTrustStore(fx.paths.TrustStore)
	if err != nil || len(ts.Projects) != 1 || ts.Projects[0].Path != fx.paths.Project || ts.Projects[0].SHA256 != src.Hash {
		t.Fatalf("store = %+v (%v)", ts, err)
	}
	// the same file through a relative path is the same entry
	// t.Chdir restores the directory and fails the test (instead of
	// ignoring the error); it also forbids t.Parallel, as the process-wide
	// working directory requires.
	t.Chdir(filepath.Dir(fx.paths.Project))
	if ok, _ := ts.IsTrusted(ProjectFileName, src.Hash); !ok {
		t.Fatal("a relative path is not canonicalized")
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(fx.dir, "link")
		if err := os.Symlink(filepath.Dir(fx.paths.Project), link); err != nil {
			t.Fatal(err)
		}
		if ok, _ := ts.IsTrusted(filepath.Join(link, ProjectFileName), src.Hash); !ok {
			t.Fatal("a path through a symbolic link is not canonicalized")
		}
	}
	// re-trusting replaces the entry
	if err := Trust(fx.paths.TrustStore, fx.paths.Project, "other", t0); err != nil {
		t.Fatal(err)
	}
	ts, _ = ReadTrustStore(fx.paths.TrustStore)
	if len(ts.Projects) != 1 || ts.Projects[0].SHA256 != "other" {
		t.Fatalf("store = %+v", ts)
	}
	if removed, err := Untrust(fx.paths.TrustStore, fx.paths.Project); err != nil || !removed {
		t.Fatalf("untrust: %v", err)
	}
	ts, _ = ReadTrustStore(fx.paths.TrustStore)
	if len(ts.Projects) != 0 {
		t.Fatal("not removed")
	}
}

// --- atomic writes and concurrent modification ---

func TestUpdateWritesAtomicallyWithPrivateMode(t *testing.T) {
	fx := newFixture(t)
	err := Update(fx.paths, ScopeGlobal, t0, func(f *File) error {
		f.Templates = map[string]TemplateConfig{"t": {Body: "x"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(fx.paths.Global)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(fx.paths.Global))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("left behind: %s", e.Name())
		}
	}
	assertUnlocked(t, fx.paths.Global)
}

// assertUnlocked: the lock of path is free (its file may stay).
func assertUnlocked(t *testing.T, path string) {
	t.Helper()
	unlock, err := lockFile(path)
	if err != nil {
		t.Fatalf("the lock is still held: %v", err)
	}
	unlock()
}

func TestUpdateRejectsInvalidResult(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	before, _ := os.ReadFile(fx.paths.Global)
	err := Update(fx.paths, ScopeGlobal, t0, func(f *File) error {
		f.Groups["development"] = GroupConfig{Feathers: []string{"ghost"}}
		return nil
	})
	if err == nil {
		t.Fatal("an invalid result was written")
	}
	after, _ := os.ReadFile(fx.paths.Global)
	if string(after) != string(before) {
		t.Fatal("the file changed")
	}
}

func TestUpdateProjectNeedsTrustAndKeepsIt(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	write(t, fx.paths.Project, projectTOML)
	add := func(f *File) error {
		f.Templates = map[string]TemplateConfig{"p": {Body: "x"}}
		return nil
	}
	if err := Update(fx.paths, ScopeProject, t0, add); !errors.Is(err, ErrUntrustedProject) {
		t.Fatalf("untrusted project edited: %v", err)
	}
	src, _ := ReadFile(fx.paths.Project)
	_ = Trust(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0)
	if err := Update(fx.paths, ScopeProject, t0, add); err != nil {
		t.Fatal(err)
	}
	if l, err := Load(fx.paths); err != nil || !l.ProjectTrusted {
		t.Fatalf("the trust did not follow kjak's own edit: %v", err)
	}
	// a new project file is created trusted
	fx2 := newFixture(t)
	if err := Update(fx2.paths, ScopeProject, t0, add); err != nil {
		t.Fatal(err)
	}
	if l, err := Load(fx2.paths); err != nil || !l.ProjectTrusted {
		t.Fatalf("new project file: %v", err)
	}
}

// Many writers at once: each either succeeds or is told that another one
// holds the file; no successful change is lost and the file stays valid.
func TestCritical_ConfigConcurrentModification(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	const writers = 16
	var wg sync.WaitGroup
	errs := make([]error, writers)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = Update(fx.paths, ScopeGlobal, t0, func(f *File) error {
				f.Templates[fmt.Sprintf("t%02d", i)] = TemplateConfig{Body: "x"}
				return nil
			})
		}(i)
	}
	close(start)
	wg.Wait()

	src, err := ReadFile(fx.paths.Global)
	if err != nil {
		t.Fatalf("the file is broken: %v", err)
	}
	succeeded := 0
	for i, e := range errs {
		_, present := src.File.Templates[fmt.Sprintf("t%02d", i)]
		switch {
		case e == nil:
			succeeded++
			if !present {
				t.Errorf("writer %d succeeded but its change is lost", i)
			}
		case errors.Is(e, ErrLocked) || errors.Is(e, ErrConcurrentModification):
			if present {
				t.Errorf("writer %d failed but its change is there", i)
			}
		default:
			t.Errorf("writer %d: unexpected error %v", i, e)
		}
	}
	if succeeded == 0 {
		t.Fatal("no writer succeeded")
	}
	if _, ok := src.File.Templates["deploy"]; !ok {
		t.Fatal("the original content is lost")
	}
	assertUnlocked(t, fx.paths.Global)
}

// An editor that ignores the lock changes the file while kjak writes it:
// kjak detects it and does not overwrite the change.
func TestCritical_ConfigExternalModificationDetected(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	external := globalTOML + "\n[templates.external]\nbody = \"from the editor\"\n"
	beforeCommit = func(path string) { write(t, path, external) }
	t.Cleanup(func() { beforeCommit = nil })
	err := Update(fx.paths, ScopeGlobal, t0, func(f *File) error {
		f.Templates["mine"] = TemplateConfig{Body: "x"}
		return nil
	})
	if !errors.Is(err, ErrConcurrentModification) {
		t.Fatalf("err = %v", err)
	}
	b, _ := os.ReadFile(fx.paths.Global)
	if string(b) != external {
		t.Fatal("the external change was overwritten")
	}
}

// A project file changed after kjak checked its trust is not edited.
func TestCritical_ConfigTrustedHashRaced(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	write(t, fx.paths.Project, projectTOML)
	src, _ := ReadFile(fx.paths.Project)
	_ = Trust(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0)
	_, _, err := rewrite(fx.paths.Project, Expect{Check: true, Exists: true, Hash: Hash([]byte("something else"))}, func(b []byte) ([]byte, error) { return b, nil })
	if !errors.Is(err, ErrConcurrentModification) {
		t.Fatalf("err = %v", err)
	}
}

func TestLockHeldIsReported(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	unlock, err := lockFile(fx.paths.Global)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	err = Update(fx.paths, ScopeGlobal, t0, func(*File) error { return nil })
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), ".lock") {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteNew(t *testing.T) {
	fx := newFixture(t)
	if err := WriteNew(fx.paths.Global, []byte("[defaults]\n"), false); err != nil {
		t.Fatal(err)
	}
	if err := WriteNew(fx.paths.Global, []byte("[defaults]\n"), false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("overwrote: %v", err)
	}
	if err := WriteNew(fx.paths.Global, []byte("# new\n"), true); err != nil {
		t.Fatal(err)
	}
}

// trusted_at is read in UTC, so that an entry is written back as it was
// read whatever the local zone; a time outside years 1 to 9999 (which TOML
// cannot write back) breaks the store.
func TestTrustStoreTimesInUTC(t *testing.T) {
	ts, err := parseTrustStore("trust.toml", []byte("[[projects]]\npath = \"/p\"\nsha256 = \"x\"\ntrusted_at = 2026-01-01T09:00:00+09:00\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if at := ts.Projects[0].TrustedAt; at.Location() != time.UTC || !at.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("trusted_at = %v", at)
	}
	for _, at := range []string{"0000-01-01T00:00:00+09:00", "9999-12-31T23:00:00-05:00"} {
		if _, err := parseTrustStore("trust.toml", []byte("[[projects]]\npath = \"/p\"\nsha256 = \"x\"\ntrusted_at = "+at+"\n"), true); err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Errorf("%s: %v", at, err)
		}
	}
}
