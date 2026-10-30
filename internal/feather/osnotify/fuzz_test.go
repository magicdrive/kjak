package osnotify

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/magicdrive/kjak/internal/core"
)

// FuzzCompose: whatever the message, the notification is valid UTF-8, has a
// one-line, non-empty title, no control character but the body's line
// breaks, and stays within MaxTitle and MaxBody.
func FuzzCompose(f *testing.F) {
	f.Add("Build done", "tests passed", "https://ci.example.com/1", "success")
	f.Add("", "", "", "")
	f.Add("\x1b]0;x\x07\r\n", "a\r\nb\x00c\xff", "\n", "error")
	f.Add(strings.Repeat("あ", 300), strings.Repeat("😀", 5000), "", "critical")
	f.Fuzz(func(t *testing.T, title, body, url, level string) {
		n := Compose(core.Message{Title: title, Body: body, URL: url, Level: core.Level(level)})
		for what, s := range map[string]string{"title": n.Title, "subtitle": n.Subtitle, "body": n.Body} {
			if !utf8.ValidString(s) {
				t.Fatalf("%s is not UTF-8: %q", what, s)
			}
			for _, r := range s {
				if unicode.IsControl(r) && (r != '\n' || what != "body") {
					t.Fatalf("%s has a control character %U: %q", what, r, s)
				}
			}
		}
		if n.Title == "" || utf8.RuneCountInString(n.Title) > MaxTitle {
			t.Fatalf("title %q", n.Title)
		}
		if utf8.RuneCountInString(n.Body) > MaxBody {
			t.Fatalf("body of %d characters", utf8.RuneCountInString(n.Body))
		}
	})
}
