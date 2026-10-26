package output

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Shown is a value of a configuration as kjak prints it: as it is when
// every character is printable (a Windows path keeps its backslashes),
// else quoted (Go syntax: %q), so a value cannot write a terminal escape
// sequence, break a line, or hide text with a bidirectional override.
func Shown(s string) string {
	for _, r := range s {
		if !strconv.IsPrint(r) {
			return strconv.Quote(s)
		}
	}
	return s
}

// ShownList is Shown of each value, joined with ", ".
func ShownList(values []string) string {
	shown := make([]string, len(values))
	for i, v := range values {
		shown[i] = Shown(v)
	}
	return strings.Join(shown, ", ")
}

// EscapeControls escapes the control characters of a text kjak prints on
// a terminal (an error message): line breaks and tabs are kept, every other
// C0 character, DEL and C1 become \xHH or \u00HH.
func EscapeControls(s string) string {
	if strings.IndexFunc(s, isEscaped) < 0 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case !isEscaped(r):
			b.WriteRune(r)
		case r < 0x80:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String()
}

func isEscaped(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }
