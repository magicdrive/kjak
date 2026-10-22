package core

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits of a message. They are kjak's own; an adapter whose provider allows
// less (Discord: 4096 characters of embed description) shortens the text it
// sends and marks the cut.
const (
	MaxTitleLength = 256   // runes
	MaxBodyLength  = 16384 // runes
	MaxURLLength   = 2048  // bytes
)

// Message is one notification, delivered unchanged to every target. The
// differences between providers are the adapters' (internal/feather/...).
type Message struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body"`
	Level Level  `json:"level"`
	URL   string `json:"url,omitempty"`
}

// ErrInvalidMessage wraps every validation error of a message.
var ErrInvalidMessage = errors.New("invalid message")

// Normalize applies the defaults of an unset field: the level info.
func (m *Message) Normalize() {
	if m.Level == "" {
		m.Level = LevelInfo
	}
}

// Validate reports the first problem of the message: an empty body, an
// unknown level, a title with a line break or a control character, a URL
// that is not absolute http(s), or a field over its limit.
func (m Message) Validate() error {
	invalid := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidMessage, fmt.Sprintf(format, a...))
	}
	if !utf8.ValidString(m.Title) || !utf8.ValidString(m.Body) || !utf8.ValidString(m.URL) {
		return invalid("not valid UTF-8")
	}
	if strings.TrimSpace(m.Body) == "" {
		return invalid("body is required")
	}
	if n := utf8.RuneCountInString(m.Body); n > MaxBodyLength {
		return invalid("body is %d characters (limit %d)", n, MaxBodyLength)
	}
	if n := utf8.RuneCountInString(m.Title); n > MaxTitleLength {
		return invalid("title is %d characters (limit %d)", n, MaxTitleLength)
	}
	// A title is one line: it becomes an e-mail Subject header.
	for _, r := range m.Title {
		if unicode.IsControl(r) {
			return invalid("title must be a single line without control characters")
		}
	}
	for _, r := range m.Body {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return invalid("body contains a control character (U+%04X)", r)
		}
	}
	if _, err := ParseLevel(string(m.Level)); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMessage, err)
	}
	if m.URL != "" {
		if len(m.URL) > MaxURLLength {
			return invalid("url is %d bytes (limit %d)", len(m.URL), MaxURLLength)
		}
		u, err := url.Parse(m.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("url must be an absolute http or https URL")
		}
		for _, r := range m.URL {
			if unicode.IsControl(r) || unicode.IsSpace(r) {
				return invalid("url must not contain spaces or control characters")
			}
		}
	}
	return nil
}
