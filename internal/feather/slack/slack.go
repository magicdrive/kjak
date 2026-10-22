// Package slack is the Slack feather: a Slack incoming webhook.
package slack

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/webhook"
)

// Feather posts to one incoming webhook.
type Feather struct {
	WebhookURL credential.Secret
	Poster     *webhook.Poster
}

func (f *Feather) Deliver(ctx context.Context, msg core.Message) feather.Outcome {
	payload, err := json.Marshal(Payload(msg))
	if err != nil {
		return feather.Rejected(core.ErrCodeInternal, 0, false, err)
	}
	return f.Poster.Post(ctx, f.WebhookURL, payload)
}

// Colors of the attachment bar, by level.
var colors = map[core.Level]string{
	core.LevelInfo:     "#439FE0",
	core.LevelSuccess:  "#2EB67D",
	core.LevelWarning:  "#ECB22E",
	core.LevelError:    "#E01E5A",
	core.LevelCritical: "#8B0000",
}

type attachment struct {
	Color     string `json:"color"`
	Title     string `json:"title,omitempty"`
	TitleLink string `json:"title_link,omitempty"`
	Text      string `json:"text"`
	Footer    string `json:"footer"`
}

type payload struct {
	Text        string       `json:"text"`
	Attachments []attachment `json:"attachments"`
}

// Payload is the webhook payload of msg: a plain text line (used by
// notifications and clients without attachments) and an attachment colored
// by level. Text is escaped: a message cannot mention @channel or users.
func Payload(msg core.Message) any {
	title := escape(msg.Title)
	body := escape(msg.Body)
	fallback := "[" + strings.ToUpper(string(msg.Level)) + "] "
	if title != "" {
		fallback += title + ": "
	}
	fallback += firstLine(body)
	a := attachment{
		Color:  colors[msg.Level],
		Title:  title,
		Text:   body,
		Footer: "kjak · " + string(msg.Level),
	}
	if msg.URL != "" {
		if a.Title == "" {
			a.Title = msg.URL
		}
		a.TitleLink = msg.URL
	}
	return payload{Text: fallback, Attachments: []attachment{a}}
}

// escape applies Slack's control character escaping (&, <, >), which also
// disables <!channel> and <@user> mentions.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
