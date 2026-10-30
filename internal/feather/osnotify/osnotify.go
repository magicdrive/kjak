// Package osnotify is the os feather: a notification shown by the operating
// system of the machine kjak runs on (docs/os.md).
//
// kjak is a single unsigned executable, not an app with an identity the
// notification services of macOS and Windows would know, so it asks a
// program of the OS itself to post the notification:
//
//   - macOS: /usr/bin/osascript (JavaScript for Automation, display
//     notification), in a GUI (Aqua) session only;
//   - Windows: Windows PowerShell 5.1 (powershell.exe) and the WinRT toast
//     API, under the application identity of Windows PowerShell;
//   - Linux: notify-send (libnotify), with a D-Bus session.
//
// The notification's text is data, never code: the script is a constant,
// and the text reaches it on standard input (macOS) or base64 in an
// environment variable (Windows), or as arguments after "--" (Linux).
//
// "confirmed" means that the OS program accepted the notification, not that
// anyone saw it: Focus or Do Not Disturb, the notification settings of the
// program, or a locked screen can still keep it off the screen.
package osnotify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/feather"
)

// The errors of a backend. They are permanent: a retry would meet the same
// machine.
var (
	// ErrUnsupported: kjak has no backend for this OS.
	ErrUnsupported = errors.New("OS notifications are not supported on this system")
	// ErrUnavailable: the backend cannot post here (no GUI or D-Bus session,
	// the OS program is missing or failed).
	ErrUnavailable = errors.New("OS notifications are not available here")
	// ErrDisabled: the OS reports that notifications of the program are off
	// (Windows only: the other OSes do not tell).
	ErrDisabled = errors.New("notifications are turned off")
)

// Limits of the text kjak passes on (the OS shortens it further).
const (
	MaxTitle = 256
	MaxBody  = 4000
)

// Notification is what a backend shows.
type Notification struct {
	Title    string // one line, never empty
	Subtitle string // one line: the level
	Body     string // may hold line breaks
	Level    core.Level
}

// Backend posts a notification. It returns nil once the OS program accepted
// it, an error wrapping ErrUnsupported, ErrUnavailable or ErrDisabled, or an
// error of a done ctx.
type Backend interface {
	Notify(ctx context.Context, n Notification) error
}

// Feather is the os feather.
type Feather struct {
	Name    string
	Backend Backend
}

// Deliver makes one attempt. Nothing of it is retried: a failure is the
// machine's, and a retry would meet the same machine.
func (f *Feather) Deliver(ctx context.Context, msg core.Message) feather.Outcome {
	err := f.Backend.Notify(ctx, Compose(msg))
	switch {
	case err == nil:
		return feather.Confirmed(0)
	case ctx.Err() != nil:
		// the OS program was running: it may have posted the notification
		return feather.Unknown(feather.ContextCode(ctx), fmt.Errorf("%w (the notification may have been posted)", feather.ContextErr(ctx)))
	case errors.Is(err, ErrUnsupported):
		return feather.Rejected(core.ErrCodeUnsupported, 0, false, err)
	case errors.Is(err, ErrDisabled):
		return feather.Rejected(core.ErrCodeRejected, 0, false, err)
	case errors.Is(err, ErrUnavailable):
		return feather.Rejected(core.ErrCodeUnavailable, 0, false, err)
	}
	return feather.Rejected(core.ErrCodeInternal, 0, false, err)
}

// Compose turns a message into a notification: the title (kjak when the
// message has none), the level as the subtitle, the body and then the URL.
// Text is made valid UTF-8, control characters other than line breaks
// become spaces, and the title and body are shortened to MaxTitle and
// MaxBody characters.
func Compose(msg core.Message) Notification {
	title := oneLine(msg.Title)
	if title == "" {
		title = "kjak"
	}
	body := text(msg.Body)
	if u := oneLine(msg.URL); u != "" {
		if body != "" {
			body += "\n"
		}
		body += u
	}
	level := msg.Level
	if level == "" {
		level = core.LevelInfo
	}
	return Notification{Title: clip(title, MaxTitle), Subtitle: clip(oneLine(string(level)), MaxTitle), Body: clip(body, MaxBody), Level: level}
}

// text keeps line breaks and turns every other control character (and a
// lone CR) into a space.
func text(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s))
}

func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(text(s), "\n", " "))
}

// clip shortens s to at most n characters, ending with "…" when it cut.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
