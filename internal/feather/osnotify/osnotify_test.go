package osnotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/magicdrive/kjak/internal/core"
)

// fake records the commands it is asked to run and answers each with the
// function of its program.
type fake struct {
	calls  []Command
	answer map[string]func(Command) ([]byte, []byte, error)
}

func (f *fake) run(ctx context.Context, c Command) ([]byte, []byte, error) {
	f.calls = append(f.calls, c)
	if a := f.answer[c.Path]; a != nil {
		return a(c)
	}
	return nil, nil, nil
}

func env(f *fake, vars map[string]string, files ...string) Env {
	return Env{
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(name string) (string, error) {
			if name == "notify-send" && vars["HAVE_NOTIFY_SEND"] != "" {
				return "/usr/bin/notify-send", nil
			}
			return "", exec.ErrNotFound
		},
		Stat: func(p string) (os.FileInfo, error) {
			for _, f := range files {
				if f == p {
					return nil, nil
				}
			}
			return nil, fs.ErrNotExist
		},
		Run: f.run,
	}
}

func exitError(t *testing.T, code int) error {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "GO_WANT_EXIT="+string(rune('0'+code)))
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("not an exit error: %v", err)
	}
	return err
}

// TestMain lets the test binary stand for a program that exits with a code.
func TestMain(m *testing.M) {
	if c := os.Getenv("GO_WANT_EXIT"); c != "" {
		os.Exit(int(c[0] - '0'))
	}
	os.Exit(m.Run())
}

var hostile = core.Message{
	Title: "\" & do shell script \"touch /tmp/x\" & \"$(id)`id`';'\x1b]0;t\x07",
	Body:  "日本語 ✅ é\r\n'; Remove-Item -Recurse C:\\ #\"; $env:PATH\x00\xff",
	Level: core.LevelError,
	URL:   "https://ci.example/1",
}

func TestCompose(t *testing.T) {
	n := Compose(hostile)
	if strings.ContainsAny(n.Title, "\x1b\x07\x00\n\r") || strings.ContainsAny(n.Body, "\x1b\x00\r") {
		t.Fatalf("control characters kept: %q / %q", n.Title, n.Body)
	}
	if !strings.Contains(n.Body, "日本語 ✅ é\n';") || !strings.HasSuffix(n.Body, "\nhttps://ci.example/1") || !strings.Contains(n.Body, "\uFFFD") {
		t.Fatalf("body = %q", n.Body)
	}
	if n.Subtitle != "error" || n.Level != core.LevelError {
		t.Fatalf("level: %+v", n)
	}
	if n := Compose(core.Message{Body: "x"}); n.Title != "kjak" || n.Subtitle != "info" {
		t.Fatalf("defaults: %+v", n)
	}
	long := Compose(core.Message{Title: strings.Repeat("t", 1000), Body: strings.Repeat("あ", 10000)})
	if len([]rune(long.Title)) != MaxTitle || len([]rune(long.Body)) != MaxBody || !strings.HasSuffix(long.Body, "…") {
		t.Fatalf("not shortened: %d %d", len([]rune(long.Title)), len([]rune(long.Body)))
	}
}

func TestDarwin(t *testing.T) {
	f := &fake{answer: map[string]func(Command) ([]byte, []byte, error){
		"/bin/launchctl": func(Command) ([]byte, []byte, error) { return []byte("Aqua\n"), nil, nil },
	}}
	if err := New("darwin", env(f, nil)).Notify(context.Background(), Compose(hostile)); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || f.calls[1].Path != "/usr/bin/osascript" {
		t.Fatalf("calls: %+v", f.calls)
	}
	c := f.calls[1]
	// the script is the constant; the text arrives as JSON on stdin only
	if len(c.Args) != 4 || c.Args[0] != "-l" || c.Args[1] != "JavaScript" || c.Args[2] != "-e" || c.Args[3] != darwinScript {
		t.Fatalf("args: %q", c.Args)
	}
	var got map[string]string
	if err := json.Unmarshal(c.Stdin, &got); err != nil {
		t.Fatal(err)
	}
	if want := Compose(hostile); got["title"] != want.Title || got["body"] != want.Body || got["subtitle"] != "error" {
		t.Fatalf("stdin: %q", got)
	}
	for _, a := range c.Args {
		if strings.Contains(a, "touch") || strings.Contains(a, "日本語") {
			t.Fatalf("the text reached the arguments: %q", a)
		}
	}
}

func TestDarwinNoGUISession(t *testing.T) {
	for _, mgr := range []string{"Background", "System", ""} {
		f := &fake{answer: map[string]func(Command) ([]byte, []byte, error){
			"/bin/launchctl": func(Command) ([]byte, []byte, error) { return []byte(mgr + "\n"), nil, nil },
		}}
		err := New("darwin", env(f, nil)).Notify(context.Background(), Compose(hostile))
		if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "GUI session") {
			t.Fatalf("%q: %v", mgr, err)
		}
		if len(f.calls) != 1 {
			t.Fatalf("%q: osascript ran: %+v", mgr, f.calls)
		}
	}
}

func TestDarwinOsascriptFails(t *testing.T) {
	f := &fake{answer: map[string]func(Command) ([]byte, []byte, error){
		"/bin/launchctl": func(Command) ([]byte, []byte, error) { return []byte("Aqua"), nil, nil },
		"/usr/bin/osascript": func(Command) ([]byte, []byte, error) {
			return nil, []byte("execution error: x (-1743)\n"), exitError(t, 1)
		},
	}}
	err := New("darwin", env(f, nil)).Notify(context.Background(), Compose(hostile))
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "-1743") {
		t.Fatal(err)
	}
}

func decodeCommand(t *testing.T, s string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b)%2 != 0 {
		t.Fatalf("encoded command: %v", err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

func TestWindows(t *testing.T) {
	ps := `C:\Windows` + string(os.PathSeparator) + "System32" + string(os.PathSeparator) + "WindowsPowerShell" + string(os.PathSeparator) + "v1.0" + string(os.PathSeparator) + "powershell.exe"
	f := &fake{}
	if err := New("windows", env(f, map[string]string{"SystemRoot": `C:\Windows`}, ps)).Notify(context.Background(), Compose(hostile)); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0].Path != ps {
		t.Fatalf("calls: %+v", f.calls)
	}
	c := f.calls[0]
	if got := decodeCommand(t, c.Args[len(c.Args)-1]); got != windowsScript || c.Args[len(c.Args)-2] != "-EncodedCommand" {
		t.Fatalf("script: %q", got)
	}
	if !strings.Contains(strings.Join(c.Args, " "), "-NoProfile") {
		t.Fatalf("args: %q", c.Args)
	}
	var payload string
	for _, e := range c.Env {
		if v, ok := strings.CutPrefix(e, "KJAK_NOTIFICATION="); ok {
			payload = v
		}
		if strings.ContainsAny(e, "\"'`$") && !strings.HasPrefix(e, "KJAK_APP_ID=") {
			t.Fatalf("raw text in the environment: %q", e)
		}
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil || got["body"] != Compose(hostile).Body {
		t.Fatalf("payload: %q %v", got, err)
	}
	if strings.Contains(windowsScript, "touch") || strings.Contains(strings.Join(c.Args, " "), "Remove-Item") {
		t.Fatal("the text reached the script")
	}
}

func TestWindowsErrors(t *testing.T) {
	ps := `C:\Windows` + string(os.PathSeparator) + "System32" + string(os.PathSeparator) + "WindowsPowerShell" + string(os.PathSeparator) + "v1.0" + string(os.PathSeparator) + "powershell.exe"
	vars := map[string]string{"SystemRoot": `C:\Windows`}
	cases := []struct {
		name  string
		env   Env
		check func(error) bool
	}{
		{"no SystemRoot", env(&fake{}, nil, ps), func(err error) bool { return errors.Is(err, ErrUnavailable) }},
		{"no PowerShell", env(&fake{}, vars), func(err error) bool {
			return errors.Is(err, ErrUnavailable) && strings.Contains(err.Error(), "not installed")
		}},
		{"disabled", env(&fake{answer: map[string]func(Command) ([]byte, []byte, error){ps: func(Command) ([]byte, []byte, error) {
			return nil, []byte("notifications are off: DisabledForUser\n"), exitError(t, 3)
		}}}, vars, ps), func(err error) bool {
			return errors.Is(err, ErrDisabled) && strings.Contains(err.Error(), "DisabledForUser")
		}},
		{"script error", env(&fake{answer: map[string]func(Command) ([]byte, []byte, error){ps: func(Command) ([]byte, []byte, error) {
			return nil, []byte("Exception calling \"Show\": the notification platform is unavailable\n"), exitError(t, 1)
		}}}, vars, ps), func(err error) bool {
			return errors.Is(err, ErrUnavailable) && strings.Contains(err.Error(), "platform is unavailable")
		}},
	}
	for _, c := range cases {
		if err := New("windows", c.env).Notify(context.Background(), Compose(hostile)); !c.check(err) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestLinux(t *testing.T) {
	f := &fake{}
	vars := map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1/bus", "HAVE_NOTIFY_SEND": "1"}
	n := Compose(core.Message{Title: "-r 5", Body: "--help", Level: core.LevelCritical})
	if err := New("linux", env(f, vars)).Notify(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	args := f.calls[0].Args
	i := 0
	for i < len(args) && args[i] != "--" {
		i++
	}
	// what starts with "-" comes after "--": it is text
	if i != 2 || args[1] != "--urgency=critical" || args[3] != "-r 5 · critical" || args[4] != "--help" {
		t.Fatalf("args: %q", args)
	}
	// no session bus: refused before anything runs
	for _, vars := range []map[string]string{{"HAVE_NOTIFY_SEND": "1"}, {"HAVE_NOTIFY_SEND": "1", "XDG_RUNTIME_DIR": "/run/user/1"}} {
		f := &fake{}
		if err := New("linux", env(f, vars)).Notify(context.Background(), n); !errors.Is(err, ErrUnavailable) || len(f.calls) != 0 {
			t.Fatalf("%v: %v %+v", vars, err, f.calls)
		}
	}
	// the bus of XDG_RUNTIME_DIR is enough
	f = &fake{}
	if err := New("linux", env(f, map[string]string{"HAVE_NOTIFY_SEND": "1", "XDG_RUNTIME_DIR": "/run/user/1"}, "/run/user/1/bus")).Notify(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	// no notify-send
	if err := New("linux", env(&fake{}, map[string]string{"DBUS_SESSION_BUS_ADDRESS": "x"})).Notify(context.Background(), n); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "notify-send") {
		t.Fatal(err)
	}
}

func TestUnsupported(t *testing.T) {
	for _, goos := range []string{"freebsd", "plan9", "js"} {
		if err := New(goos, env(&fake{}, nil)).Notify(context.Background(), Compose(hostile)); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v", goos, err)
		}
	}
}

type backendFunc func(context.Context, Notification) error

func (b backendFunc) Notify(ctx context.Context, n Notification) error { return b(ctx, n) }

// The outcomes of the feather: confirmed, permanent refusals with their
// codes, never retryable; a run interrupted is unknown (it may have shown).
func TestDeliverOutcomes(t *testing.T) {
	cases := []struct {
		err  error
		acc  core.Acceptance
		code core.ErrorCode
	}{
		{nil, core.AcceptanceConfirmed, ""},
		{ErrUnsupported, core.AcceptanceRejected, core.ErrCodeUnsupported},
		{ErrUnavailable, core.AcceptanceRejected, core.ErrCodeUnavailable},
		{ErrDisabled, core.AcceptanceRejected, core.ErrCodeRejected},
		{errors.New("other"), core.AcceptanceRejected, core.ErrCodeInternal},
	}
	for _, c := range cases {
		o := (&Feather{Name: "d", Backend: backendFunc(func(context.Context, Notification) error { return c.err })}).Deliver(context.Background(), hostile)
		if o.Acceptance != c.acc || o.Code != c.code || o.Retryable {
			t.Errorf("%v: %+v", c.err, o)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &Feather{Name: "d", Backend: backendFunc(func(ctx context.Context, _ Notification) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})}
	if o := f.Deliver(ctx, hostile); o.Acceptance != core.AcceptanceUnknown || o.Code != core.ErrCodeCanceled {
		t.Fatalf("canceled: %+v", o)
	}
}

// Exec: a program that ignores a done context is killed, and its output is
// capped.
func TestExecCancelAndCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := Exec(ctx, Command{Path: "/bin/sh", Args: []string{"-c", "sleep 30"}})
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("not killed: %v after %s", err, time.Since(start))
	}
	out, _, err := Exec(context.Background(), Command{Path: "/bin/sh", Args: []string{"-c", "head -c 1000000 /dev/zero"}})
	if err != nil || len(out) != maxOutput {
		t.Fatalf("output %d bytes: %v", len(out), err)
	}
	out, _, err = Exec(context.Background(), Command{Path: "/bin/sh", Args: []string{"-c", "cat; printf %s \"$KJAK_X\""}, Env: []string{"KJAK_X=y"}, Stdin: []byte("in")})
	if err != nil || string(out) != "iny" {
		t.Fatalf("%q %v", out, err)
	}
}
