package output

import "testing"

func TestShownAndEscapeControls(t *testing.T) {
	for in, want := range map[string]string{
		"smtp.example.com":       "smtp.example.com",
		"a b":                    "a b",
		"x\x1b[2Jy":              `"x\x1b[2Jy"`,
		"bel\a":                  `"bel\a"`,
		"c1\u009b31m":            `"c1\u009b31m"`,
		"rtl\u202eexe.txt":       `"rtl\u202eexe.txt"`,
		`C:\Users\me\.kjak.toml`: `C:\Users\me\.kjak.toml`,
		`say "hi"`:               `say "hi"`,
		"line\nbreak":            `"line\nbreak"`,
	} {
		if got := Shown(in); got != want {
			t.Errorf("Shown(%q) = %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"plain\n  next\tcol": "plain\n  next\tcol",
		"a\x1b]0;t\x07b":     `a\x1b]0;t\x07b`,
		"c1\u009bx\x7f":      `c1\u009bx\x7f`,
	} {
		if got := EscapeControls(in); got != want {
			t.Errorf("EscapeControls(%q) = %s, want %s", in, got, want)
		}
	}
}
