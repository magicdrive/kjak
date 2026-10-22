package slack_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather/slack"
	"github.com/magicdrive/kjak/internal/feather/webhook"
)

func TestDeliverPostsPayload(t *testing.T) {
	var got map[string]any
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f := &slack.Feather{
		WebhookURL: credential.NewSecret(srv.URL + "/services/T/B/X"),
		Poster:     &webhook.Poster{Client: webhook.NewClient(), Clock: clock.NewFake(time.Now())},
	}
	o := f.Deliver(context.Background(), core.Message{Title: "Deploy", Body: "api <@U123> & <!channel>\nline 2", Level: core.LevelError, URL: "https://example.com/run/1"})
	if o.Acceptance != core.AcceptanceConfirmed {
		t.Fatalf("outcome = %+v", o)
	}
	if contentType != "application/json" {
		t.Fatalf("content type = %q", contentType)
	}
	text := got["text"].(string)
	if text != "[ERROR] Deploy: api &lt;@U123&gt; &amp; &lt;!channel&gt;" {
		t.Fatalf("text = %q", text)
	}
	a := got["attachments"].([]any)[0].(map[string]any)
	if a["color"] != "#E01E5A" || a["title_link"] != "https://example.com/run/1" || a["title"] != "Deploy" {
		t.Fatalf("attachment = %v", a)
	}
	if strings.Contains(a["text"].(string), "<!channel>") {
		t.Fatal("a mention was not escaped")
	}
}

func TestPayloadWithoutTitle(t *testing.T) {
	b, _ := json.Marshal(slack.Payload(core.Message{Body: "only body", Level: core.LevelInfo, URL: "https://e.example"}))
	s := string(b)
	if !strings.Contains(s, `"text":"[INFO] only body"`) || !strings.Contains(s, `"title":"https://e.example"`) {
		t.Fatalf("payload = %s", s)
	}
}
