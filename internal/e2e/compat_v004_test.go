package e2e

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Compatibility with v0.0.4, the last release before v1.0.0.
//
// testdata/compat_v004 holds files written by the v0.0.4 binary itself:
//
//   - config.toml: `kjak config init`, then `feather add` (slack with a
//     literal webhook URL from --secret-stdin, discord with an env reference,
//     a username and levels, email without authentication, a keychain
//     reference, a disabled feather), `group set` and `template set`. Only the
//     literal URL and the SMTP port are placeholders (@SLACK_URL@,
//     @SMTP_PORT@): the test servers choose them.
//   - config_init.toml: what `kjak config init` of v0.0.4 wrote.
//   - trust.toml: the trust store `kjak config trust --yes` of v0.0.4 wrote
//     (@PATH@ and @SHA256@ are the project file of the test).
//   - golden/: the requests v0.0.4 made for the sends below (the slack and
//     discord JSON bodies, the e-mail as the SMTP server read it with its Date
//     and Message-ID removed).
//
// The tests run the binary built from this tree. KJAK_COMPAT_BINARY=<path
// of a v0.0.4 binary> runs them against v0.0.4 instead: every check then
// expects what v0.0.4 did (the intended changes are marked with
// `if v004`), and KJAK_COMPAT_UPDATE=1 rewrites golden/ from it. So the
// golden requests are v0.0.4's, and the current binary must make the same.
//
// The intended changes since v0.0.4 (docs/migration.md):
//   - a literal secret is deprecated: `config validate` and every command
//     that loads it warn on stderr; `config validate --strict` refuses it;
//   - a project feather's env or keychain reference needs a new trust that
//     allows it (#19): the trust of v0.0.4 does not;
//   - a project's slack, discord and email feathers connect to public
//     addresses only unless allow_networks / allow_hosts allow more (#24);
//   - a message template may not use range, define, template or block (#25).

func compatBinary() (string, bool) {
	if b := os.Getenv("KJAK_COMPAT_BINARY"); b != "" {
		return b, true
	}
	return binary, false
}

// compatRecorder is a local Slack- or Discord-like webhook: it answers 200
// (Discord: 204) and records the bodies.
type compatRecorder struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newCompatRecorder(t *testing.T, status int) *compatRecorder {
	r := &compatRecorder{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.bodies = append(r.bodies, req.Method+" "+req.URL.Path+" "+req.Header.Get("Content-Type")+"\n"+string(b))
		r.mu.Unlock()
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte("ok"))
		}
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *compatRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.bodies)
}

// compatSMTP is a local SMTP server without TLS or authentication (the
// v0.0.4 fixture's security = "none"): it accepts every message and
// records the envelope and the data.
type compatSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	msgs []string
}

func newCompatSMTP(t *testing.T) *compatSMTP {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &compatSMTP{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *compatSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *compatSMTP) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	say := func(line string) { _, _ = io.WriteString(c, line+"\r\n") }
	say("220 compat.test ESMTP")
	var env []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250-compat.test")
			say("250 8BITMIME")
		case strings.HasPrefix(cmd, "MAIL FROM"), strings.HasPrefix(cmd, "RCPT TO"):
			env = append(env, strings.TrimSpace(line))
			say("250 ok")
		case cmd == "DATA":
			say("354 go ahead")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			s.mu.Lock()
			s.msgs = append(s.msgs, strings.Join(env, "\n")+"\n\n"+data.String())
			s.mu.Unlock()
			env = nil
			say("250 queued")
		case cmd == "RSET", cmd == "NOOP":
			env = nil
			say("250 ok")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("502 not implemented")
		}
	}
}

func (s *compatSMTP) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.msgs)
}

// The fields of a message that change on every send.
var volatileMailHeader = regexp.MustCompile(`(?m)^(Date|Message-ID): .*\r?\n`)

type compatEnv struct {
	*sandbox
	v004    bool
	slack   *compatRecorder
	discord *compatRecorder
	smtp    *compatSMTP
	xdg     string
}

func newCompatEnv(t *testing.T) *compatEnv {
	exe, v004 := compatBinary()
	s := newSandbox(t)
	s.exe = exe
	e := &compatEnv{sandbox: s, v004: v004,
		slack: newCompatRecorder(t, http.StatusOK), discord: newCompatRecorder(t, http.StatusNoContent), smtp: newCompatSMTP(t),
		xdg: filepath.Join(filepath.Dir(s.dir), "xdg", "kjak")}
	config := strings.NewReplacer(
		"@SLACK_URL@", e.slack.URL+"/services/T000/B000/LITERALSECRET",
		"@SMTP_PORT@", fmt.Sprint(e.smtp.port()),
	).Replace(compatFixture(t, "config.toml"))
	if err := os.MkdirAll(e.xdg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.xdg, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	s.env = append(s.env, "KJAK_DISCORD_WEBHOOK="+e.discord.URL+"/api/webhooks/1/DISCORDSECRET")
	return e
}

func compatFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "compat_v004", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// golden compares got with golden/<name>, the request v0.0.4 made
// (KJAK_COMPAT_UPDATE=1 with KJAK_COMPAT_BINARY writes it).
func (e *compatEnv) golden(name, got string) {
	e.t.Helper()
	// the servers' ports are not part of a request's content
	got = strings.ReplaceAll(got, e.slack.URL, "@SLACK@")
	got = strings.ReplaceAll(got, e.discord.URL, "@DISCORD@")
	got = strings.ReplaceAll(got, fmt.Sprintf(`"port": %d,`, e.smtp.port()), `"port": "@SMTP_PORT@",`)
	got = strings.ReplaceAll(got, "\r\n", "\n")
	path := filepath.Join("testdata", "compat_v004", "golden", name)
	if os.Getenv("KJAK_COMPAT_UPDATE") == "1" {
		if !e.v004 {
			e.t.Fatal("KJAK_COMPAT_UPDATE needs KJAK_COMPAT_BINARY: the golden requests are v0.0.4's")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			e.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			e.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatal(err)
	}
	if strings.HasSuffix(name, "_keys.txt") {
		// JSON documents: within schema version 1 fields are only added
		have := map[string]bool{}
		for _, k := range strings.Fields(got) {
			have[k] = true
		}
		for _, k := range strings.Fields(string(want)) {
			if !have[k] {
				e.t.Errorf("%s: %s of v0.0.4 is gone\n%s", name, k, got)
			}
		}
		return
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		e.t.Errorf("%s: the request differs from v0.0.4's\n--- v0.0.4\n%s\n--- now\n%s", name, want, got)
	}
}

// literalWarning checks the deprecation warning of the fixture's literal
// secret: none from v0.0.4, on stderr only from the current binary.
func (e *compatEnv) literalWarning(what string, r run) {
	e.t.Helper()
	warned := strings.Contains(r.stderr, "literal (deprecated): feathers.dev_slack.webhook_url")
	if warned == e.v004 {
		e.t.Errorf("%s: literal warning %v (v0.0.4: %v)\nstderr: %s", what, warned, e.v004, r.stderr)
	}
	if strings.Contains(r.stdout+r.stderr, "LITERALSECRET") {
		e.t.Errorf("%s: the literal secret is printed", what)
	}
}

// The global configuration v0.0.4 wrote: it validates, sends the same
// requests to Slack, Discord and e-mail through its groups and templates,
// reports the same results and keeps its permissions.
func TestCompatV004GlobalConfig(t *testing.T) {
	e := newCompatEnv(t)

	r := e.kjak("", "config", "validate")
	if r.code != 0 {
		t.Fatalf("config validate: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if strings.Contains(r.stdout, "1 literal secret(s): deprecated") == e.v004 {
		t.Errorf("config validate does not report the literal: %s", r.stdout)
	}
	// --strict is new: v0.0.4 does not know it; now it refuses the literal
	if r := e.kjak("", "config", "validate", "--strict"); r.code != 2 {
		t.Errorf("config validate --strict: exit %d", r.code)
	} else if !e.v004 && !strings.Contains(r.stderr, "feathers.dev_slack.webhook_url") {
		t.Errorf("--strict does not name the literal: %s", r.stderr)
	}

	// groups (with a level filter), every built-in type, one send
	r = e.kjak("", "send", "--to", "development,ops", "--level", "error", "--title", "CI", "tests failed")
	if r.code != 0 {
		t.Fatalf("send: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	e.literalWarning("send", r)
	for _, want := range []string{
		"ok    dev_slack (slack) delivered",
		"ok    ops_discord (discord) delivered",
		"ok    ops_mail (email) delivered",
		"success: 3 delivered, 0 failed, 0 skipped",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("send: no %q in\n%s", want, r.stdout)
		}
	}
	// a level the discord feather filters out: skipped through its group
	r = e.kjak("", "send", "--to", "development", "--level", "info", "-q", "hello")
	if r.code != 0 || r.stdout != "" {
		t.Fatalf("send -q: exit %d %q %s", r.code, r.stdout, r.stderr)
	}
	// the template of the file, with variables
	r = e.kjak("", "send", "--to", "dev_slack,ops_mail", "--template", "deploy", "--var", "service=api", "--var", "version=1.4.2")
	if r.code != 0 {
		t.Fatalf("send --template: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	slack, discord, mails := e.slack.all(), e.discord.all(), e.smtp.all()
	if len(slack) != 3 || len(discord) != 1 || len(mails) != 2 {
		t.Fatalf("requests: slack %d, discord %d, mail %d", len(slack), len(discord), len(mails))
	}
	e.golden("slack_error.txt", slack[0])
	e.golden("slack_info.txt", slack[1])
	e.golden("slack_template.txt", slack[2])
	e.golden("discord_error.txt", discord[0])
	e.golden("mail_error.txt", volatileMailHeader.ReplaceAllString(mails[0], ""))
	e.golden("mail_template.txt", volatileMailHeader.ReplaceAllString(mails[1], ""))

	// template render prints the same message
	r = e.kjak("", "template", "render", "deploy", "--var", "service=api", "--var", "version=1.4.2")
	if r.code != 0 {
		t.Fatalf("template render: exit %d %s", r.code, r.stderr)
	}
	e.golden("template_render.txt", r.stdout)
	// a variable the template uses must be given (both versions)
	if r := e.kjak("", "template", "render", "deploy", "--var", "service=api"); r.code != 2 {
		t.Errorf("render without a variable: exit %d", r.code)
	}

	// the JSON report: v0.0.4's keys are all still there (fields are only
	// added within schema version 1)
	r = e.kjak("", "send", "--to", "ops", "--level", "warning", "-o", "json", "disk")
	if r.code != 0 {
		t.Fatalf("send -o json: exit %d %s", r.code, r.stderr)
	}
	e.literalWarning("send -o json", r)
	e.golden("report_keys.txt", jsonKeyPaths(t, r.stdout))

	// the feathers and the effective configuration, the literal redacted
	r = e.kjak("", "feather", "list", "-o", "json")
	if r.code != 0 {
		t.Fatalf("feather list: exit %d %s", r.code, r.stderr)
	}
	e.golden("feather_list_keys.txt", jsonKeyPaths(t, r.stdout))
	r = e.kjak("", "config", "path", "-o", "json")
	if r.code != 0 {
		t.Fatalf("config path: exit %d %s", r.code, r.stderr)
	}
	e.golden("config_path_keys.txt", jsonKeyPaths(t, r.stdout))
	r = e.kjak("", "feather", "show", "dev_slack")
	if r.code != 0 || !strings.Contains(r.stdout, "literal:[REDACTED]") {
		t.Fatalf("feather show: exit %d %s%s", r.code, r.stdout, r.stderr)
	}
	r = e.kjak("", "config", "show")
	if r.code != 0 || strings.Contains(r.stdout, "LITERALSECRET") {
		t.Fatalf("config show: exit %d %s", r.code, r.stderr)
	}
	// the keys of config show -o json, the Go field names of a group and a
	// template ("Feathers", "Title", ...) included, are v0.0.4's
	r = e.kjak("", "config", "show", "-o", "json")
	if r.code != 0 || strings.Contains(r.stdout, "LITERALSECRET") {
		t.Fatalf("config show -o json: exit %d %s", r.code, r.stderr)
	}
	e.golden("config_show_keys.txt", jsonKeyPaths(t, r.stdout))

	// routing and preflight errors keep their exit status
	before := len(e.slack.all()) + len(e.discord.all()) + len(e.smtp.all())
	for _, args := range [][]string{
		{"send", "--to", "keep_off", "x"},                  // a disabled feather named directly
		{"send", "--to", "ops_discord", "-l", "info", "x"}, // a level the feather refuses
		{"send", "--to", "nobody", "x"},                    // unknown
		{"send", "x"},                                      // no target, no defaults.targets
	} {
		if r := e.kjak("", args...); r.code != 2 {
			t.Errorf("%v: exit %d\n%s", args, r.code, r.stderr)
		}
	}
	if n := len(e.slack.all()) + len(e.discord.all()) + len(e.smtp.all()); n != before {
		t.Errorf("a preflight error sent something: %d requests", n)
	}

	// a change through the CLI keeps the file private and its content
	if r := e.kjak("", "group", "set", "late", "dev_slack"); r.code != 0 {
		t.Fatalf("group set: exit %d %s", r.code, r.stderr)
	}
	b, err := os.ReadFile(filepath.Join(e.xdg, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[groups.late]", "[templates.deploy]", "username = 'kjak-bot'", "subject_prefix = '[kjak]'"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("after group set the file lacks %q:\n%s", want, b)
		}
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(filepath.Join(e.xdg, "config.toml")); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("config.toml mode %v (%v), want 0600", fi.Mode().Perm(), err)
		}
		// the mode the user gave the file is kept, as v0.0.4 kept it (a
		// literal in it is warned about as deprecated, not refused)
		cfg := filepath.Join(e.xdg, "config.toml")
		if err := os.Chmod(cfg, 0o644); err != nil {
			t.Fatal(err)
		}
		if r := e.kjak("", "group", "set", "later", "dev_slack"); r.code != 0 {
			t.Fatalf("group set on a 0644 file: exit %d %s", r.code, r.stderr)
		}
		if fi, err := os.Stat(cfg); err != nil || fi.Mode().Perm() != 0o644 {
			t.Errorf("config.toml mode %v (%v), want 0644 kept", fi.Mode().Perm(), err)
		}
	}
}

// jsonKeyPaths lists the key paths of a JSON document ("results[].history"),
// sorted, one per line.
func jsonKeyPaths(t *testing.T, doc string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, doc)
	}
	set := map[string]bool{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				set[p] = true
				walk(p, c)
			}
		case []any:
			for _, c := range x {
				walk(prefix+"[]", c)
			}
		}
	}
	walk("", v)
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n") + "\n"
}

// The trust store v0.0.4 wrote keeps trusting the project file it names:
// its groups and templates are used without a new trust. Its feather with
// an env reference needs a new trust that allows the reference (#19).
func TestCompatV004ProjectTrust(t *testing.T) {
	e := newCompatEnv(t)
	project := "[templates.release]\ntitle = \"Release {{.v}}\"\nbody = \"released {{.v}}\"\n\n" +
		"[groups.team]\nfeathers = [\"dev_slack\"]\n\n" +
		"[feathers.proj_slack]\ntype = \"slack\"\nwebhook_url = { env = \"PROJ_HOOK\" }\n"
	projectPath := filepath.Join(e.dir, ".kjak.toml")
	if err := os.WriteFile(projectPath, []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	canon, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(project))
	// TOML literal string: a Windows path keeps its backslashes
	trust := strings.NewReplacer("@PATH@", canon, "@SHA256@", hex.EncodeToString(sum[:])).Replace(compatFixture(t, "trust.toml"))
	if err := os.WriteFile(filepath.Join(e.xdg, "trust.toml"), []byte(trust), 0o600); err != nil {
		t.Fatal(err)
	}
	e.env = append(e.env, "PROJ_HOOK="+e.slack.URL+"/project")

	// groups and templates of the trusted project
	r := e.kjak("", "send", "--to", "team", "--template", "release", "--var", "v=2")
	if r.code != 0 {
		t.Fatalf("project template: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if got := e.slack.all(); len(got) != 1 || !strings.Contains(got[0], "Release 2: released 2") {
		t.Fatalf("project template sent %q", got)
	}

	// the project feather's env reference, to a loopback webhook
	r = e.kjak("", "send", "--to", "proj_slack", "--dry-run", "x")
	if e.v004 {
		if r.code != 0 {
			t.Fatalf("v0.0.4: project env reference: exit %d %s", r.code, r.stderr)
		}
		if r := e.kjak("", "send", "--to", "proj_slack", "x"); r.code != 0 {
			t.Fatalf("v0.0.4: project webhook on loopback: exit %d %s", r.code, r.stderr)
		}
		return
	}
	if r.code != 2 || !strings.Contains(r.stderr, "the trust of the project does not allow") {
		t.Fatalf("project env reference under the v0.0.4 trust: exit %d %s", r.code, r.stderr)
	}
	// a new trust that allows it
	if r := e.kjak("", "config", "trust", "--yes", "--allow-user-secrets"); r.code != 0 {
		t.Fatalf("config trust: exit %d %s", r.code, r.stderr)
	}
	if r := e.kjak("", "send", "--to", "proj_slack", "--dry-run", "x"); r.code != 0 {
		t.Fatalf("after the new trust: exit %d %s", r.code, r.stderr)
	}
	// a project's webhook reaches public addresses only (#24): loopback is
	// refused before anything is sent, until the project allows it
	before := len(e.slack.all())
	r = e.kjak("", "send", "--to", "proj_slack", "-o", "json", "x")
	if r.code != 1 || !strings.Contains(r.stdout, `"code": "blocked"`) || len(e.slack.all()) != before {
		t.Fatalf("project webhook on loopback: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	project = strings.Replace(project, "type = \"slack\"\n", "type = \"slack\"\nallow_networks = [\"127.0.0.1/32\", \"::1/128\"]\n", 1)
	if err := os.WriteFile(projectPath, []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := e.kjak("", "config", "trust", "--yes", "--allow-user-secrets"); r.code != 0 {
		t.Fatalf("config trust: exit %d %s", r.code, r.stderr)
	}
	if r := e.kjak("", "send", "--to", "proj_slack", "x"); r.code != 0 || len(e.slack.all()) != before+1 {
		t.Fatalf("project webhook allowed on loopback: exit %d %s%s", r.code, r.stdout, r.stderr)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(filepath.Join(e.xdg, "trust.toml")); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("trust.toml mode %v (%v), want 0600", fi.Mode().Perm(), err)
		}
	}
}

// What v0.0.4 refused is still refused: an OS notification feather (not
// implemented), a webhook URL in clear text to a remote host, an e-mail
// password in clear text.
func TestCompatV004StillRefused(t *testing.T) {
	e := newCompatEnv(t)
	for name, feather := range map[string]string{
		"os notification": "[feathers.desk]\ntype = \"os\"\n",
		"macos":           "[feathers.desk]\ntype = \"macos\"\n",
		"http webhook":    "[feathers.desk]\ntype = \"slack\"\nwebhook_url = { literal = \"http://hooks.example.com/x\" }\n",
		"clear password":  "[feathers.desk]\ntype = \"email\"\nhost = \"smtp.example.com\"\nsecurity = \"none\"\nuser = \"u\"\npassword = { env = \"P\" }\nfrom = \"a@example.com\"\nto = [\"b@example.com\"]\n",
	} {
		if err := os.WriteFile(filepath.Join(e.xdg, "config.toml"), []byte(feather), 0o600); err != nil {
			t.Fatal(err)
		}
		r := e.kjak("", "config", "validate")
		if r.code != 2 {
			t.Errorf("%s: config validate: exit %d %s", name, r.code, r.stdout)
		}
	}
}

// The configuration `kjak config init` of v0.0.4 wrote is valid.
func TestCompatV004InitFile(t *testing.T) {
	e := newCompatEnv(t)
	if err := os.WriteFile(filepath.Join(e.xdg, "config.toml"), []byte(compatFixture(t, "config_init.toml")), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := e.kjak("", "config", "validate"); r.code != 0 {
		t.Fatalf("config validate: exit %d %s", r.code, r.stderr)
	}
}

// A message template of v0.0.4 that loops (range, define/template) is
// refused now (#25): `template set` and every render refuse it, before
// anything is sent. A template without them renders as before.
func TestCompatV004TemplateLoops(t *testing.T) {
	e := newCompatEnv(t)
	config := "[feathers.dev_slack]\ntype = 'slack'\nwebhook_url = {literal = '" + e.slack.URL + "/services/T000/B000/LITERALSECRET'}\n\n" +
		"[templates.loop]\nbody = \"{{range 3}}x{{end}} {{.v}}\"\n\n" +
		"[templates.def]\nbody = '{{define \"a\"}}A{{end}}{{template \"a\"}}'\n\n" +
		"[templates.plain]\nbody = \"{{.v}} {{if .v}}set{{end}}\"\n"
	if err := os.WriteFile(filepath.Join(e.xdg, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := e.kjak("", "template", "render", "plain", "--var", "v=1"); r.code != 0 || !strings.Contains(r.stdout, "1 set") {
		t.Fatalf("plain template: exit %d %s%s", r.code, r.stdout, r.stderr)
	}
	want := 2
	if e.v004 {
		want = 0
	}
	for _, args := range [][]string{
		{"template", "render", "loop", "--var", "v=1"},
		{"template", "render", "def"},
		{"send", "--to", "dev_slack", "--template", "loop", "--var", "v=1"},
		{"template", "set", "again", "--body", "{{range 2}}y{{end}}"},
	} {
		if r := e.kjak("", args...); r.code != want {
			t.Errorf("%v: exit %d, want %d\n%s%s", args, r.code, want, r.stdout, r.stderr)
		}
	}
	if !e.v004 && len(e.slack.all()) != 0 {
		t.Error("a refused template sent something")
	}
}
