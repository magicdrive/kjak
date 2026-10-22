package core

import (
	"testing"
	"unicode"
)

// A message Validate accepts has a body, a known level and a one-line title
// (it becomes an e-mail header).
func FuzzMessageValidate(f *testing.F) {
	f.Add("t", "body", "info", "https://e.example/x")
	f.Add("a\r\nBcc: x", "b", "error", "")
	f.Add("", "\x00", "loud", "javascript:x")
	f.Fuzz(func(t *testing.T, title, body, level, url string) {
		m := Message{Title: title, Body: body, Level: Level(level), URL: url}
		if m.Validate() != nil {
			return
		}
		for _, r := range m.Title {
			if unicode.IsControl(r) {
				t.Fatalf("accepted title with control %U", r)
			}
		}
		if _, err := ParseLevel(level); err != nil {
			t.Fatal("accepted unknown level")
		}
	})
}
