package discord_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather/discord"
	"github.com/magicdrive/kjak/internal/feather/webhook"
)

func TestDeliverWaitsForTheMessage(t *testing.T) {
	var query string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()
	f := &discord.Feather{
		WebhookURL: credential.NewSecret(srv.URL + "/api/webhooks/1/tok?thread_id=42"),
		Username:   "kjak-bot",
		Poster:     &webhook.Poster{Client: webhook.NewClient(), Clock: clock.NewFake(time.Now())},
	}
	o := f.Deliver(context.Background(), core.Message{Title: "T", Body: "@everyone hi", Level: core.LevelWarning})
	if o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("outcome = %+v", o)
	}
	if !strings.Contains(query, "wait=true") || !strings.Contains(query, "thread_id=42") {
		t.Fatalf("query = %q", query)
	}
	if got["username"] != "kjak-bot" {
		t.Fatalf("payload = %v", got)
	}
	am := got["allowed_mentions"].(map[string]any)
	if parse := am["parse"].([]any); len(parse) != 0 {
		t.Fatalf("mentions allowed: %v", am)
	}
	e := got["embeds"].([]any)[0].(map[string]any)
	if e["color"].(float64) != 0xECB22E || e["title"] != "T" {
		t.Fatalf("embed = %v", e)
	}
}

func TestPayloadTruncatesToDiscordLimits(t *testing.T) {
	long := strings.Repeat("あ", 5000)
	b, _ := json.Marshal(discord.Payload(core.Message{Title: strings.Repeat("t", 256), Body: long, Level: core.LevelInfo}, ""))
	var p struct {
		Embeds []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"embeds"`
	}
	_ = json.Unmarshal(b, &p)
	d := p.Embeds[0].Description
	if n := utf8.RuneCountInString(d); n != 4096 || !strings.HasSuffix(d, "…") {
		t.Fatalf("description: %d characters", n)
	}
	if utf8.RuneCountInString(p.Embeds[0].Title) != 256 {
		t.Fatal("a title at the limit was cut")
	}
}

func TestDetail(t *testing.T) {
	if d := discord.Detail([]byte(`{"message": "Unknown Webhook", "code": 10015}`)); d != "Unknown Webhook" {
		t.Fatalf("detail = %q", d)
	}
	if d := discord.Detail([]byte("plain")); d != "plain" {
		t.Fatalf("detail = %q", d)
	}
}

func TestWaitURLRejectsGarbage(t *testing.T) {
	if _, err := discord.WaitURL(credential.NewSecret("not a url")); err == nil {
		t.Fatal("accepted")
	}
}

// The retry_after of a 429 body is capped like a Retry-After header (24h):
// a value beyond what a duration holds is the cap, not a negative wait that
// would retry at once.
func TestRetryAfterBodyCapped(t *testing.T) {
	for _, tc := range []struct {
		body string
		want time.Duration
	}{
		{`{"retry_after": 1.5}`, 1500 * time.Millisecond},
		{`{"retry_after": 100000}`, webhook.MaxRetryAfter},
		{`{"retry_after": 1e20}`, webhook.MaxRetryAfter},
		{`{"retry_after": 1e300}`, webhook.MaxRetryAfter},
	} {
		if d, ok := discord.RetryAfterBody([]byte(tc.body)); !ok || d != tc.want {
			t.Errorf("%s: %s %v, want %s", tc.body, d, ok, tc.want)
		}
	}
}
