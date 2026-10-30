package osnotify

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/magicdrive/kjak/internal/core"
)

// Env is how a backend reads the environment and finds programs; tests
// replace it.
type Env struct {
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Stat     func(string) (os.FileInfo, error)
	Run      Runner
}

// SystemEnv is the environment of the process.
func SystemEnv() Env {
	return Env{Getenv: os.Getenv, LookPath: exec.LookPath, Stat: os.Stat, Run: Exec}
}

// New returns the backend of goos (runtime.GOOS).
func New(goos string, env Env) Backend {
	switch goos {
	case "darwin":
		return &darwin{env: env}
	case "windows":
		return &windows{env: env}
	case "linux":
		return &linux{env: env}
	}
	return unsupported{goos: goos}
}

type unsupported struct{ goos string }

func (u unsupported) Notify(context.Context, Notification) error {
	return fmt.Errorf("%w (%s)", ErrUnsupported, u.goos)
}

// --- macOS ----------------------------------------------------------------

// darwinScript is run by osascript as JavaScript for Automation. It reads
// the notification as JSON on standard input: its text is never part of
// the script.
const darwinScript = `ObjC.import("Foundation");
function run() {
  var data = $.NSFileHandle.fileHandleWithStandardInput.readDataToEndOfFile;
  var n = JSON.parse(ObjC.unwrap($.NSString.alloc.initWithDataEncoding(data, $.NSUTF8StringEncoding)));
  var app = Application.currentApplication();
  app.includeStandardAdditions = true;
  app.displayNotification(n.body, {withTitle: n.title, subtitle: n.subtitle});
  return "ok";
}`

type darwin struct{ env Env }

func (d *darwin) Notify(ctx context.Context, n Notification) error {
	// A notification belongs to the GUI (Aqua) session of the user logged
	// in on the screen: over SSH, from a launchd daemon or a CI job outside
	// that session it cannot be shown, and osascript would not say so.
	out, _, err := d.env.Run(ctx, Command{Path: "/bin/launchctl", Args: []string{"managername"}})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("%w: cannot tell the session (launchctl managername: %v)", ErrUnavailable, err)
	}
	if s := strings.TrimSpace(string(out)); s != "Aqua" {
		return fmt.Errorf("%w: kjak does not run in a GUI session (launchctl managername: %s; over SSH or in a background job, a notification cannot be shown)", ErrUnavailable, oneLine(s))
	}
	in, err := json.Marshal(map[string]string{"title": n.Title, "subtitle": n.Subtitle, "body": n.Body})
	if err != nil {
		return err
	}
	_, stderr, err := d.env.Run(ctx, Command{Path: "/usr/bin/osascript", Args: []string{"-l", "JavaScript", "-e", darwinScript}, Stdin: in})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failure("osascript", stderr, err)
	}
	return nil
}

// --- Windows --------------------------------------------------------------

// powerShellAppID is the application identity of Windows PowerShell (the
// AppUserModelID of its Start menu shortcut). An unpackaged executable has
// none of its own, and kjak does not register one (no shortcut, no
// registry key): the toast is shown as from Windows PowerShell.
const powerShellAppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

// windowsScript is run by Windows PowerShell 5.1 (WinRT is not available in
// PowerShell 7). It reads the notification from KJAK_NOTIFICATION (base64 of
// UTF-8 JSON) and puts each text in the toast's XML as a text node: its text
// is never part of the script or of the XML markup. Exit status 3: the
// notifications of the identity are off (the setting names why).
const windowsScript = `$ErrorActionPreference = 'Stop'
$n = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:KJAK_NOTIFICATION)) | ConvertFrom-Json
$null = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($env:KJAK_APP_ID)
$setting = $notifier.Setting
if ($setting -ne [Windows.UI.Notifications.NotificationSetting]::Enabled) {
  [Console]::Error.WriteLine("notifications are off: " + $setting)
  exit 3
}
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText04)
$texts = $xml.GetElementsByTagName('text')
$null = $texts.Item(0).AppendChild($xml.CreateTextNode($n.title))
$null = $texts.Item(1).AppendChild($xml.CreateTextNode($n.body))
$null = $texts.Item(2).AppendChild($xml.CreateTextNode($n.subtitle))
$notifier.Show([Windows.UI.Notifications.ToastNotification]::new($xml))
`

type windows struct{ env Env }

func (w *windows) Notify(ctx context.Context, n Notification) error {
	root := w.env.Getenv("SystemRoot")
	if root == "" {
		return fmt.Errorf("%w: SystemRoot is not set, Windows PowerShell cannot be found", ErrUnavailable)
	}
	ps := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := w.env.Stat(ps); err != nil {
		return fmt.Errorf("%w: Windows PowerShell (%s) is not installed", ErrUnavailable, ps)
	}
	in, err := json.Marshal(map[string]string{"title": n.Title, "subtitle": n.Subtitle, "body": n.Body})
	if err != nil {
		return err
	}
	_, stderr, err := w.env.Run(ctx, Command{
		Path: ps,
		Args: []string{"-NoProfile", "-NonInteractive", "-NoLogo", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeCommand(windowsScript)},
		Env:  []string{"KJAK_NOTIFICATION=" + base64.StdEncoding.EncodeToString(in), "KJAK_APP_ID=" + powerShellAppID},
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 3 {
			return fmt.Errorf("%w for Windows PowerShell (Settings, System, Notifications): %s", ErrDisabled, lastLine(stderr))
		}
		return failure("Windows PowerShell", stderr, err)
	}
	return nil
}

// encodeCommand is a script as -EncodedCommand takes it: base64 of UTF-16LE.
func encodeCommand(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// --- Linux ----------------------------------------------------------------

type linux struct{ env Env }

func (l *linux) Notify(ctx context.Context, n Notification) error {
	// notify-send talks to the notification server of the desktop session
	// over D-Bus: without a session bus (SSH, cron, CI) nothing can show it.
	if l.env.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		bus := ""
		if dir := l.env.Getenv("XDG_RUNTIME_DIR"); dir != "" {
			bus = filepath.Join(dir, "bus")
		}
		if bus == "" {
			return fmt.Errorf("%w: no D-Bus session (DBUS_SESSION_BUS_ADDRESS and XDG_RUNTIME_DIR are not set): kjak does not run in a desktop session", ErrUnavailable)
		}
		if _, err := l.env.Stat(bus); err != nil {
			return fmt.Errorf("%w: no D-Bus session (DBUS_SESSION_BUS_ADDRESS is not set, %s does not exist): kjak does not run in a desktop session", ErrUnavailable, bus)
		}
	}
	path, err := l.env.LookPath("notify-send")
	if err != nil {
		return fmt.Errorf("%w: notify-send is not installed (the libnotify-bin or libnotify package)", ErrUnavailable)
	}
	_, stderr, err := l.env.Run(ctx, Command{
		Path: path,
		// "--": a title or body that starts with "-" is text, not an option
		Args: []string{"--app-name=kjak", "--urgency=" + urgency(n.Level), "--", n.Title + " · " + n.Subtitle, n.Body},
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failure("notify-send", stderr, err)
	}
	return nil
}

// urgency is the freedesktop urgency of a level.
func urgency(l core.Level) string {
	switch l {
	case core.LevelError, core.LevelCritical:
		return "critical"
	}
	return "normal"
}
