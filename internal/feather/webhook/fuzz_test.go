package webhook

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
)

// Whatever a server answers: an unknown acceptance is never retryable, and
// only 2xx confirms.
func FuzzClassify(f *testing.F) {
	f.Add(200, "", []byte("ok"))
	f.Add(429, "3", []byte(`{"retry_after":1}`))
	f.Add(504, "", []byte(""))
	p := &Poster{Clock: clock.NewFake(time.Unix(0, 0))}
	f.Fuzz(func(t *testing.T, status int, retryAfter string, body []byte) {
		if status < 100 || status > 999 {
			return
		}
		h := http.Header{}
		h.Set("Retry-After", retryAfter)
		o := p.classify(status, h, body)
		if o.Acceptance == core.AcceptanceUnknown && o.Retryable {
			t.Fatal("unknown and retryable")
		}
		if (o.Acceptance == core.AcceptanceConfirmed) != (status >= 200 && status < 300) {
			t.Fatalf("HTTP %d: %s", status, o.Acceptance)
		}
		if o.RetryAfter < 0 || o.RetryAfter > 24*time.Hour {
			t.Fatalf("retry after %s", o.RetryAfter)
		}
	})
}

func FuzzParseRetryAfter(f *testing.F) {
	f.Add("120")
	f.Add("Wed, 21 Oct 2026 07:28:00 GMT")
	f.Add(strconv.Itoa(-1))
	f.Fuzz(func(t *testing.T, v string) {
		d, ok := ParseRetryAfter(v, time.Unix(1_800_000_000, 0))
		if d < 0 || d > MaxRetryAfter || (!ok && d != 0) {
			t.Fatalf("%q: %s %t", v, d, ok)
		}
	})
}
