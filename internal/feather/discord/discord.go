// Package discord is the Discord feather: a Discord webhook, executed with
// ?wait=true so that Discord answers with the created message (a confirmed
// acceptance) instead of an empty 204.
package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/webhook"
)

// Limits of an embed (Discord API).
const (
	maxTitle       = 256
	maxDescription = 4096
	maxUsername    = 80
)

// Feather executes one webhook.
type Feather struct {
	WebhookURL credential.Secret
	Username   string
	Poster     *webhook.Poster
}

func (f *Feather) Deliver(ctx context.Context, msg core.Message) feather.Outcome {
	target, err := WaitURL(f.WebhookURL)
	if err != nil {
		return feather.Rejected(core.ErrCodeInternal, 0, false, err)
	}
	payload, err := json.Marshal(Payload(msg, f.Username))
	if err != nil {
		return feather.Rejected(core.ErrCodeInternal, 0, false, err)
	}
	return f.Poster.Post(ctx, target, payload)
}

// WaitURL adds wait=true to the webhook URL (keeping thread_id and the rest).
func WaitURL(s credential.Secret) (credential.Secret, error) {
	u, err := url.Parse(s.Reveal())
	if err != nil || u.Host == "" {
		return credential.Secret{}, errors.New("invalid webhook URL")
	}
	q := u.Query()
	q.Set("wait", "true")
	u.RawQuery = q.Encode()
	return credential.NewSecret(u.String()), nil
}

var colors = map[core.Level]int{
	core.LevelInfo:     0x439FE0,
	core.LevelSuccess:  0x2EB67D,
	core.LevelWarning:  0xECB22E,
	core.LevelError:    0xE01E5A,
	core.LevelCritical: 0x8B0000,
}

type embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description"`
	URL         string       `json:"url,omitempty"`
	Color       int          `json:"color"`
	Footer      *embedFooter `json:"footer,omitempty"`
}

type embedFooter struct {
	Text string `json:"text"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}

type payload struct {
	Username        string          `json:"username,omitempty"`
	Embeds          []embed         `json:"embeds"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

// Payload is the webhook payload of msg: one embed colored by level. No
// mention is allowed (allowed_mentions.parse = []): @everyone in a message
// stays text.
func Payload(msg core.Message, username string) any {
	return payload{
		Username: truncate(username, maxUsername),
		Embeds: []embed{{
			Title:       truncate(msg.Title, maxTitle),
			Description: truncate(msg.Body, maxDescription),
			URL:         msg.URL,
			Color:       colors[msg.Level],
			Footer:      &embedFooter{Text: "kjak · " + string(msg.Level)},
		}},
		AllowedMentions: allowedMentions{Parse: []string{}},
	}
}

// truncate shortens s to max characters, the last one an ellipsis.
func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// RetryAfterBody reads Discord's 429 body: {"retry_after": seconds}.
func RetryAfterBody(body []byte) (time.Duration, bool) {
	var v struct {
		RetryAfter *float64 `json:"retry_after"`
	}
	if json.Unmarshal(body, &v) != nil || v.RetryAfter == nil || *v.RetryAfter < 0 {
		return 0, false
	}
	return time.Duration(*v.RetryAfter * float64(time.Second)), true
}

// Detail reads Discord's error body: {"message": "...", "code": n}.
func Detail(body []byte) string {
	var v struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &v) == nil && v.Message != "" {
		return v.Message
	}
	return string(body)
}
