// Package webhook is the HTTP part shared by the webhook feathers (Slack,
// Discord): one POST of a JSON payload, classified by how far the request
// got.
//
// The classification (docs/delivery.md):
//
//	2xx                         confirmed
//	429                         rejected, retryable (Retry-After, or
//	                              RetryAfterBody, honored)
//	408, 500, 503               rejected, retryable (503: Retry-After
//	                              honored)
//	502, 504                    unknown: a gateway may have forwarded it
//	other 5xx                   rejected, retryable
//	3xx, other 4xx              rejected, permanent
//	error before the request    rejected, retryable (it never fully left)
//	  was fully written
//	TLS certificate refused     rejected, permanent
//	error after it was written  unknown, not retried
package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
)

// Poster posts webhook payloads.
type Poster struct {
	Client *http.Client
	Clock  clock.Clock
	// RetryAfterBody extracts a wait from a 429 body when the header has none
	// (Discord: {"retry_after": 1.5}).
	RetryAfterBody func(body []byte) (time.Duration, bool)
	// Detail extracts a short error text from a refusal body.
	Detail func(body []byte) string
}

// NewClient is the HTTP client of the webhook feathers: HTTP/1.1, no
// redirects (a redirected POST would carry the payload to another URL), TLS
// 1.2 or later, proxies from the environment.
func NewClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	tr.MaxIdleConnsPerHost = 4
	// HTTP/1.1 only: the classification relies on when the request was
	// written (httptrace WroteRequest after the final flush), which is the
	// HTTP/1.1 transport's behavior and is what the tests exercise.
	tr.ForceAttemptHTTP2 = false
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	tr.Protocols = protocols
	// A connection per request: a request never goes out on an idle
	// connection the server closed meanwhile, which would make a request the
	// server never read look written (unknown, not retried). With no reuse,
	// the transport also has nothing to retry by itself.
	tr.DisableKeepAlives = true
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// maxBody bounds what is read of a response.
const maxBody = 64 << 10

// Post makes one POST of payload to url.
func (p *Poster) Post(ctx context.Context, url credential.Secret, payload []byte) feather.Outcome {
	var wrote atomic.Bool
	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				wrote.Store(true)
			}
		},
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodPost, url.Reveal(), bytes.NewReader(payload))
	if err != nil {
		// the error of NewRequest quotes the URL: never pass it on
		return feather.Rejected(core.ErrCodeInternal, 0, false, errors.New("invalid webhook URL"))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "kjak")

	resp, err := p.Client.Do(req)
	if err != nil {
		return transportOutcome(ctx, err, wrote.Load())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return p.classify(resp.StatusCode, resp.Header, body)
}

// transportOutcome classifies an error without a response.
func transportOutcome(ctx context.Context, err error, wrote bool) feather.Outcome {
	reason := describeTransportError(err)
	if ctx.Err() != nil {
		code := feather.ContextCode(ctx)
		if wrote {
			return feather.Unknown(core.ErrCodeAmbiguous, fmt.Errorf("%v after the request was sent; the provider may have accepted it", feather.ContextErr(ctx)))
		}
		return feather.Rejected(code, 0, false, fmt.Errorf("%v before the request was sent", feather.ContextErr(ctx)))
	}
	if wrote {
		return feather.Unknown(core.ErrCodeAmbiguous, fmt.Errorf("the response was lost after the request was sent (%s); the provider may have accepted it", reason))
	}
	var cert *tls.CertificateVerificationError
	if errors.As(err, &cert) {
		// a retry meets the same certificate
		return feather.Rejected(core.ErrCodeProtocol, 0, false, fmt.Errorf("the request was not sent: %s", reason))
	}
	return feather.Rejected(core.ErrCodeNetwork, 0, true, fmt.Errorf("the request was not sent: %s", reason))
}

// describeTransportError describes err without the URL (*url.Error quotes
// it; the URL of a webhook is its secret).
func describeTransportError(err error) string {
	var dns *net.DNSError
	var op *net.OpError
	var cert *tls.CertificateVerificationError
	switch {
	case errors.As(err, &cert):
		return "TLS certificate verification failed"
	case errors.As(err, &dns):
		return "DNS lookup failed"
	case errors.As(err, &op):
		if op.Err != nil {
			return op.Op + ": " + op.Err.Error()
		}
		return op.Op + " failed"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection closed by the server"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "connection error"
}

func (p *Poster) classify(status int, h http.Header, body []byte) feather.Outcome {
	switch {
	case status >= 200 && status < 300:
		return feather.Confirmed(status)
	case status == http.StatusBadGateway || status == http.StatusGatewayTimeout:
		return feather.Unknown(core.ErrCodeAmbiguous, fmt.Errorf("HTTP %d from a gateway: the provider may have accepted it", status))
	case status == http.StatusTooManyRequests:
		o := feather.Rejected(core.ErrCodeRateLimited, status, true, p.statusErr(status, body))
		o.RetryAfter = p.retryAfter(h, body)
		return o
	case status == http.StatusRequestTimeout || status >= 500:
		o := feather.Rejected(core.ErrCodeServer, status, true, p.statusErr(status, body))
		if status == http.StatusServiceUnavailable {
			o.RetryAfter = p.retryAfter(h, nil)
		}
		return o
	case status >= 300 && status < 400:
		return feather.Rejected(core.ErrCodeProtocol, status, false, fmt.Errorf("HTTP %d: redirects are not followed", status))
	default:
		return feather.Rejected(core.ErrCodeRejected, status, false, p.statusErr(status, body))
	}
}

func (p *Poster) statusErr(status int, body []byte) error {
	detail := ""
	if p.Detail != nil {
		detail = p.Detail(body)
	} else {
		detail = string(body)
	}
	detail = sanitize(detail)
	if detail == "" {
		return fmt.Errorf("HTTP %d", status)
	}
	return fmt.Errorf("HTTP %d: %s", status, detail)
}

func (p *Poster) retryAfter(h http.Header, body []byte) time.Duration {
	if d, ok := ParseRetryAfter(h.Get("Retry-After"), p.Clock.Now()); ok {
		return d
	}
	if body != nil && p.RetryAfterBody != nil {
		if d, ok := p.RetryAfterBody(body); ok {
			return d
		}
	}
	return 0
}

// MaxRetryAfter caps the wait a provider asks for (the deadline ends a send
// long before).
const MaxRetryAfter = 24 * time.Hour

// ParseRetryAfter parses a Retry-After value: delay seconds or an HTTP date,
// capped at MaxRetryAfter.
func ParseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		if secs > int64(MaxRetryAfter/time.Second) {
			return MaxRetryAfter, true
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		if d > MaxRetryAfter {
			d = MaxRetryAfter
		}
		return d, true
	}
	return 0, false
}

// sanitize keeps a provider's error text short and printable.
func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "..."
	}
	return s
}
