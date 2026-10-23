package commandline_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// reply is one scripted answer of a webhook provider; hijack closes the
// connection once the request was read, without an answer.
type reply struct {
	status int
	header map[string]string
	body   string
	hijack bool
}

// scripted answers each request with the next reply (the last one again
// when they run out) and keeps what it received.
type scripted struct {
	*httptest.Server
	mu       sync.Mutex
	replies  []reply
	requests []*http.Request
	bodies   []string
}

func newScripted(t *testing.T, replies ...reply) *scripted {
	s := &scripted{replies: replies}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		rep := s.replies[min(len(s.requests), len(s.replies)-1)]
		s.requests = append(s.requests, r)
		s.bodies = append(s.bodies, string(b))
		s.mu.Unlock()
		if rep.hijack {
			c, _, _ := w.(http.Hijacker).Hijack()
			_ = c.(*net.TCPConn).Close()
			return
		}
		for k, v := range rep.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(rep.status)
		_, _ = w.Write([]byte(rep.body))
	}))
	t.Cleanup(s.Close)
	return s
}

type protocolReport struct {
	Outcome string `json:"outcome"`
	Summary struct {
		Unknown int `json:"unknown"`
	} `json:"summary"`
	Results []struct {
		Status     string `json:"status"`
		Acceptance string `json:"acceptance"`
		Attempts   int    `json:"attempts"`
		Error      *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"results"`
}

// The protocol of each webhook provider, through the whole send: what kjak
// reports, its exit status, and how many requests the provider got. A
// refusal is retried only when transient; an answer that may hide an
// accepted message (502, 504, a connection closed after the request) is
// unknown and never retried. Every attempt posts the same payload, and the
// URL (the secret) is never in the output.
func TestCritical_WebhookProtocolMatrix(t *testing.T) {
	const discordRateLimited = `{"message": "You are being rate limited.", "retry_after": 0.05, "global": false}`
	for _, tc := range []struct {
		name, typ  string
		replies    []reply
		retries    string
		code       int
		acceptance string
		errCode    string
		detail     string // in the error message
		requests   int
	}{
		{"slack ok", "slack", []reply{{status: 200, body: "ok"}}, "3", 0, "confirmed", "", "", 1},
		{"slack invalid_payload", "slack", []reply{{status: 400, body: "invalid_payload"}}, "3", 1, "rejected", "rejected", "invalid_payload", 1},
		{"slack action_prohibited", "slack", []reply{{status: 403, body: "action_prohibited"}}, "3", 1, "rejected", "rejected", "action_prohibited", 1},
		{"slack no_service", "slack", []reply{{status: 404, body: "no_service"}}, "3", 1, "rejected", "rejected", "no_service", 1},
		{"slack channel_is_archived", "slack", []reply{{status: 410, body: "channel_is_archived"}}, "3", 1, "rejected", "rejected", "channel_is_archived", 1},
		{"slack 429 then ok", "slack", []reply{{status: 429, header: map[string]string{"Retry-After": "1"}, body: "rate_limited"}, {status: 200, body: "ok"}}, "3", 0, "confirmed", "", "", 2},
		{"slack 500 rollup_error", "slack", []reply{{status: 500, body: "rollup_error"}}, "1", 1, "rejected", "server_error", "rollup_error", 2},
		{"slack 502", "slack", []reply{{status: 502, body: "bad gateway"}}, "3", 1, "unknown", "ambiguous", "HTTP 502", 1},
		{"slack 504", "slack", []reply{{status: 504}}, "3", 1, "unknown", "ambiguous", "HTTP 504", 1},
		{"slack closed after the request", "slack", []reply{{hijack: true}}, "3", 1, "unknown", "ambiguous", "may have accepted", 1},
		{"discord ok", "discord", []reply{{status: 200, header: map[string]string{"Content-Type": "application/json"}, body: `{"id": "1300000000000000000", "channel_id": "1", "type": 0}`}}, "3", 0, "confirmed", "", "", 1},
		{"discord 429 body then ok", "discord", []reply{{status: 429, body: discordRateLimited}, {status: 200, body: `{"id": "1"}`}}, "3", 0, "confirmed", "", "", 2},
		{"discord 429 header then ok", "discord", []reply{{status: 429, header: map[string]string{"Retry-After": "1"}, body: `{"retry_after": 0.5}`}, {status: 200, body: `{"id": "1"}`}}, "3", 0, "confirmed", "", "", 2},
		{"discord unknown webhook", "discord", []reply{{status: 404, body: `{"message": "Unknown Webhook", "code": 10015}`}}, "3", 1, "rejected", "rejected", "Unknown Webhook", 1},
		{"discord invalid form body", "discord", []reply{{status: 400, body: `{"message": "Invalid Form Body", "code": 50035}`}}, "3", 1, "rejected", "rejected", "Invalid Form Body", 1},
		{"discord 401", "discord", []reply{{status: 401, body: `{"message": "401: Unauthorized", "code": 0}`}}, "3", 1, "rejected", "rejected", "Unauthorized", 1},
		{"discord 500", "discord", []reply{{status: 500, body: `{"message": "Internal Server Error"}`}}, "1", 1, "rejected", "server_error", "Internal Server Error", 2},
		{"discord 502", "discord", []reply{{status: 502}}, "3", 1, "unknown", "ambiguous", "HTTP 502", 1},
		{"discord closed after the request", "discord", []reply{{hijack: true}}, "3", 1, "unknown", "ambiguous", "may have accepted", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newScripted(t, tc.replies...)
			hook := s.URL + "/api/webhooks/123/s3cret-token"
			e := newEnv(t)
			e.writeGlobal(featherConfig("a", tc.typ, hook))
			r := e.run("send", "--to", "a", "--max-retries", tc.retries, "-o", "json", "--title", "Deploy", "build finished")
			var rep protocolReport
			if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil || len(rep.Results) != 1 {
				t.Fatalf("exit %d: %v\n%s%s", r.code, err, r.stdout, r.stderr)
			}
			res := rep.Results[0]
			s.mu.Lock()
			requests, bodies, reqs := len(s.requests), s.bodies, s.requests
			s.mu.Unlock()
			if r.code != tc.code || res.Acceptance != tc.acceptance || requests != tc.requests || res.Attempts != tc.requests {
				t.Fatalf("exit %d, %s after %d attempts, %d requests: %s", r.code, res.Acceptance, res.Attempts, requests, r.stdout)
			}
			if tc.errCode != "" && (res.Error == nil || res.Error.Code != tc.errCode || !strings.Contains(res.Error.Message, tc.detail)) {
				t.Fatalf("error %+v, want %s with %q", res.Error, tc.errCode, tc.detail)
			}
			if (tc.acceptance == "unknown") != (rep.Summary.Unknown == 1) {
				t.Fatalf("summary.unknown = %d", rep.Summary.Unknown)
			}
			if strings.Contains(r.stdout+r.stderr, "s3cret-token") {
				t.Fatal("the webhook URL is in the output")
			}
			for i, req := range reqs {
				if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json" || bodies[i] != bodies[0] {
					t.Fatalf("request %d: %s %q %q", i, req.Method, req.Header.Get("Content-Type"), bodies[i])
				}
				if tc.typ == "discord" && req.URL.Query().Get("wait") != "true" {
					t.Fatalf("request %d without wait=true: %s", i, req.URL.RawQuery)
				}
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(bodies[0]), &payload); err != nil {
				t.Fatalf("payload %q: %v", bodies[0], err)
			}
			if tc.typ == "slack" && !strings.Contains(payload["text"].(string), "Deploy: build finished") {
				t.Fatalf("slack payload %v", payload)
			}
			if tc.typ == "discord" {
				if em := payload["embeds"].([]any)[0].(map[string]any); em["title"] != "Deploy" || em["description"] != "build finished" {
					t.Fatalf("discord payload %v", payload)
				}
			}
		})
	}
}
