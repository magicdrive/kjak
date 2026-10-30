// Package httpapi is the http feather: one request to any HTTP API — its
// method, headers, query, authentication and body configured — classified
// by how far it got, like the webhooks of slack and discord
// (feather/webhook), under an SSRF policy (guard.go) and a retry policy
// that knows which methods may be repeated.
//
// Classification (docs/http.md):
//
//	status in success_status       confirmed
//	502, 504                       unknown: a gateway may have forwarded it
//	429, 408, other 5xx            rejected, retryable by the retry policy
//	                                 (Retry-After honored)
//	3xx (not followed)             rejected, permanent
//	any other status               rejected, permanent
//	refused by the address policy  rejected, permanent (blocked)
//	error before the request was   rejected, retryable by the retry policy
//	  fully written
//	error after it was written     unknown, never retried
package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/webhook"
	"github.com/magicdrive/kjak/internal/template"
)

// MaxBodyBytes bounds a rendered request body.
const MaxBodyBytes = 1 << 20

// Auth is the resolved authentication of a feather.
type Auth struct {
	Header string // the header that carries it ("" for none)
	Value  credential.Secret
}

// Feather makes one request per attempt.
type Feather struct {
	Name   string
	Method string
	// URL is the URL with the configured query added; it may be a secret
	// (url_secret): it is never put in an error.
	URL         *url.URL
	Header      http.Header // static headers
	Auth        Auth
	BodyFormat  string // json, text, none
	Template    string
	ContentType string
	Success     []configure.SuccessRange
	Timeout     time.Duration // of one attempt
	MaxResponse int64
	// MaxAttempts caps the attempts (0: none of its own); Retry tells
	// whether a retryable refusal may be retried at all.
	MaxAttempts       int
	Retry             bool
	IdempotencyHeader string
	IdempotencyKey    string
	Client            *http.Client
	Clock             clock.Clock
	// Redactor: the secrets redacted from a response body before it is
	// shortened (nil: none).
	Redactor *credential.Redactor

	attempts atomic.Int32
}

// Options are what New takes besides the configuration.
type Options struct {
	Resolver Resolver    // DNS (net.DefaultResolver when nil)
	TLS      *tls.Config // root CAs of tests; nil: the system's
	Clock    clock.Clock
	// Redactor redacts the secrets of the feather from a response body
	// before it is shortened for an error (an API may echo them).
	Redactor *credential.Redactor
	// dial replaces the connection to a checked address (tests).
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// New builds the feather of fc with its resolved URL and authentication.
// fc must be valid (configure.ValidateFeather).
func New(name string, fc configure.FeatherConfig, rawURL string, auth Auth, opt Options) (*Feather, error) {
	u, err := configure.CheckHTTPURL(rawURL, fc.AllowHTTP)
	if err != nil {
		return nil, err
	}
	if len(fc.Query) > 0 {
		q := u.Query()
		keys := make([]string, 0, len(fc.Query))
		for k := range fc.Query {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			q.Add(k, fc.Query[k])
		}
		u.RawQuery = q.Encode()
	}
	success, err := configure.ParseSuccessStatus(fc.SuccessStatus)
	if err != nil {
		return nil, err
	}
	networks, err := configure.ParseAllowNetworks(fc.AllowNetworks)
	if err != nil {
		return nil, err
	}
	f := &Feather{
		Name:        name,
		Method:      fc.HTTPMethod(),
		URL:         u,
		Header:      http.Header{},
		Auth:        auth,
		BodyFormat:  fc.HTTPBodyFormat(),
		Success:     success,
		Timeout:     configure.DefaultHTTPTimeout,
		MaxResponse: configure.DefaultMaxResponseBytes,
		Clock:       opt.Clock,
		Redactor:    opt.Redactor,
	}
	for k, v := range fc.Headers {
		f.Header.Set(k, v)
	}
	if fc.Body != nil {
		f.Template, f.ContentType = fc.Body.Template, fc.Body.ContentType
	}
	if f.ContentType == "" {
		switch f.BodyFormat {
		case configure.BodyJSON:
			f.ContentType = "application/json"
		case configure.BodyText:
			f.ContentType = "text/plain; charset=utf-8"
		}
	}
	if fc.Timeout != nil {
		f.Timeout = fc.Timeout.Duration
	}
	if fc.MaxResponseBytes > 0 {
		f.MaxResponse = fc.MaxResponseBytes
	}
	policy := configure.RetrySafe
	if fc.Retry != nil {
		if fc.Retry.Policy != "" {
			policy = fc.Retry.Policy
		}
		f.MaxAttempts = fc.Retry.MaxAttempts
	}
	switch policy {
	case configure.RetryNever:
		f.Retry = false
	case configure.RetryAlways:
		f.Retry = true
	default:
		// a repeated POST or PATCH may act twice, unless the API
		// recognizes the repetition by its idempotency key
		f.Retry = configure.IdempotentMethod(f.Method) || fc.IdempotencyKey
	}
	if fc.IdempotencyKey {
		f.IdempotencyHeader = fc.IdempotencyHeader
		if f.IdempotencyHeader == "" {
			f.IdempotencyHeader = configure.DefaultIdempotencyHeaderKey
		}
		// one key per send: every attempt of this message carries it
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		f.IdempotencyKey = hex.EncodeToString(b[:])
	}
	guard := &Guard{AllowNetworks: networks, AllowHosts: fc.AllowHosts, Resolver: opt.Resolver, dial: opt.dial}
	f.Client = newClient(guard, opt.TLS, fc, auth)
	return f, nil
}

// newClient is the HTTP client of one feather: HTTP/1.1, a connection per
// request (the classification relies on when the request was written, as
// for the webhooks), TLS 1.2 or later, every connection through the guard,
// no proxy, redirects only as configured.
func newClient(g *Guard, tlsConfig *tls.Config, fc configure.FeatherConfig, auth Auth) *http.Client {
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if tlsConfig != nil {
		tc = tlsConfig.Clone()
		if tc.MinVersion < tls.VersionTLS12 {
			tc.MinVersion = tls.VersionTLS12
		}
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	tr := &http.Transport{
		Proxy:                  nil, // environment proxies are ignored (guard.go)
		DialContext:            g.DialContext,
		TLSClientConfig:        tc,
		TLSHandshakeTimeout:    10 * time.Second,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      false,
		Protocols:              protocols,
		MaxResponseHeaderBytes: 64 << 10,
		DisableCompression:     true,
	}
	maxRedirects := fc.MaxRedirects
	allowHTTP := fc.AllowHTTP
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return http.ErrUseLastResponse
			}
			if req.URL.User != nil {
				return &BlockedError{Reason: "a redirect to a URL with user information"}
			}
			if req.URL.Scheme != "https" && (req.URL.Scheme != "http" || !allowHTTP) {
				return &BlockedError{Reason: "a redirect to a URL that is not https"}
			}
			// net/http sets Referer to the URL before: it may be a secret
			// (url_secret) and never goes anywhere
			req.Header.Del("Referer")
			first := via[0].URL
			// credentials go to the origin they were configured for only:
			// the same scheme, host and port (stricter than net/http, which
			// keeps them for a subdomain, and knows nothing of a custom
			// header)
			if req.URL.Scheme != first.Scheme || !strings.EqualFold(hostPort(req.URL), hostPort(first)) {
				req.Header.Del("Authorization")
				req.Header.Del("Cookie")
				if auth.Header != "" {
					req.Header.Del(auth.Header)
				}
			}
			// 307 and 308 ask for the same request elsewhere: the hop before
			// did not accept it, and whether this one is written is decided
			// again. After a 301, 302 or 303 the first hop may have acted on
			// the request: it stays written (an error is then unknown).
			if r := req.Response; r != nil && (r.StatusCode == http.StatusTemporaryRedirect || r.StatusCode == http.StatusPermanentRedirect) {
				if w, ok := req.Context().Value(wroteKey{}).(*atomic.Bool); ok {
					w.Store(false)
				}
			}
			return nil
		},
	}
}

// wroteKey is the context key of the attempt's "request written" flag, which
// CheckRedirect resets for each hop.
type wroteKey struct{}

func hostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}
	return strings.ToLower(u.Hostname()) + ":" + port
}

// errAttemptTimeout ends one attempt (timeout); the send goes on.
var errAttemptTimeout = errors.New("the attempt timeout passed")

// Deliver makes one request.
func (f *Feather) Deliver(ctx context.Context, msg core.Message) feather.Outcome {
	n := int(f.attempts.Add(1))
	o := f.attempt(ctx, msg)
	if o.Retryable {
		switch {
		case !f.Retry:
			o.Retryable = false
			o.Err = fmt.Errorf("%w (not retried: %s is not repeated without an idempotency key; retry.policy)", o.Err, f.Method)
		case f.MaxAttempts > 0 && n >= f.MaxAttempts:
			o.Retryable = false
			o.Err = fmt.Errorf("%w (retry.max_attempts %d reached)", o.Err, f.MaxAttempts)
		}
	}
	return o
}

func (f *Feather) attempt(ctx context.Context, msg core.Message) feather.Outcome {
	body, err := f.body(msg)
	if err != nil {
		return feather.Rejected(core.ErrCodeInternal, 0, false, err)
	}
	actx, cancel := context.WithTimeoutCause(ctx, f.Timeout, errAttemptTimeout)
	defer cancel()
	var wrote atomic.Bool
	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				wrote.Store(true)
			}
		},
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	rctx := context.WithValue(httptrace.WithClientTrace(actx, trace), wroteKey{}, &wrote)
	req, err := http.NewRequestWithContext(rctx, f.Method, f.URL.String(), rd)
	if err != nil {
		// the error of NewRequest quotes the URL: never pass it on
		return feather.Rejected(core.ErrCodeInternal, 0, false, errors.New("invalid request URL"))
	}
	req.Header = f.Header.Clone()
	req.Header.Set("User-Agent", "kjak")
	if body != nil {
		req.Header.Set("Content-Type", f.ContentType)
	}
	if f.Auth.Header != "" {
		req.Header.Set(f.Auth.Header, f.Auth.Value.Reveal())
	}
	if f.IdempotencyHeader != "" {
		req.Header.Set(f.IdempotencyHeader, f.IdempotencyKey)
	}

	resp, err := f.Client.Do(req)
	if err != nil {
		return f.transportOutcome(ctx, actx, err, wrote.Load())
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, f.MaxResponse+1))
	truncated := int64(len(data)) > f.MaxResponse
	if truncated {
		data = data[:f.MaxResponse]
	}
	return f.classify(resp.StatusCode, resp.Header, data, truncated)
}

func (f *Feather) transportOutcome(ctx, actx context.Context, err error, wrote bool) feather.Outcome {
	var blocked *BlockedError
	if errors.As(err, &blocked) {
		return feather.Rejected(core.ErrCodeBlocked, 0, false, fmt.Errorf("the request was not sent: %s", blocked.Reason))
	}
	if ctx.Err() == nil && errors.Is(context.Cause(actx), errAttemptTimeout) {
		if wrote {
			return feather.Unknown(core.ErrCodeAmbiguous, fmt.Errorf("no response within the attempt timeout of %s after the request was sent; the API may have accepted it", f.Timeout))
		}
		return feather.Rejected(core.ErrCodeNetwork, 0, true, fmt.Errorf("the request was not sent within the attempt timeout of %s", f.Timeout))
	}
	return webhook.TransportOutcome(ctx, err, wrote)
}

func (f *Feather) classify(status int, h http.Header, body []byte, truncated bool) feather.Outcome {
	for _, r := range f.Success {
		if status >= r.Lo && status <= r.Hi {
			return feather.Confirmed(status)
		}
	}
	detail := webhook.Sanitize(f.Redactor, string(body))
	if truncated {
		detail += " (response cut at max_response_bytes)"
	}
	statusErr := func() error {
		if strings.TrimSpace(detail) == "" {
			return fmt.Errorf("HTTP %d", status)
		}
		return fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(detail))
	}
	switch {
	case status == http.StatusBadGateway || status == http.StatusGatewayTimeout:
		return feather.Unknown(core.ErrCodeAmbiguous, fmt.Errorf("HTTP %d from a gateway: the API may have accepted it", status))
	case status == http.StatusTooManyRequests:
		o := feather.Rejected(core.ErrCodeRateLimited, status, true, statusErr())
		o.RetryAfter = f.retryAfter(h)
		return o
	case status == http.StatusRequestTimeout || status >= 500:
		o := feather.Rejected(core.ErrCodeServer, status, true, statusErr())
		o.RetryAfter = f.retryAfter(h)
		return o
	case status >= 300 && status < 400:
		return feather.Rejected(core.ErrCodeProtocol, status, false, fmt.Errorf("HTTP %d: a redirect that is not followed (max_redirects)", status))
	case status >= 200 && status < 300:
		return feather.Rejected(core.ErrCodeRejected, status, false, fmt.Errorf("HTTP %d is not in success_status", status))
	}
	return feather.Rejected(core.ErrCodeRejected, status, false, statusErr())
}

func (f *Feather) retryAfter(h http.Header) time.Duration {
	c := f.Clock
	if c == nil {
		c = clock.Real{}
	}
	d, _ := webhook.ParseRetryAfter(h.Get("Retry-After"), c.Now())
	return d
}

// body renders the request body of msg (nil: none).
func (f *Feather) body(msg core.Message) ([]byte, error) {
	data := map[string]string{
		"title": msg.Title, "body": msg.Body, "level": string(msg.Level), "url": msg.URL, "feather": f.Name,
	}
	var out []byte
	switch {
	case f.BodyFormat == configure.BodyNone:
		return nil, nil
	case f.BodyFormat == configure.BodyJSON && f.Template != "":
		b, err := template.JSON("feathers."+f.Name+".body", f.Template, data)
		if err != nil {
			return nil, err
		}
		out = b
	case f.BodyFormat == configure.BodyJSON:
		b, err := json.Marshal(struct {
			Title string `json:"title,omitempty"`
			Body  string `json:"body"`
			Level string `json:"level"`
			URL   string `json:"url,omitempty"`
		}{msg.Title, msg.Body, string(msg.Level), msg.URL})
		if err != nil {
			return nil, err
		}
		out = b
	case f.Template != "":
		s, err := template.Text("feathers."+f.Name+".body", f.Template, data)
		if err != nil {
			return nil, err
		}
		out = []byte(s)
	default:
		s := "[" + strings.ToUpper(string(msg.Level)) + "] "
		if msg.Title != "" {
			s += msg.Title + "\n\n"
		}
		s += msg.Body
		if msg.URL != "" {
			s += "\n\n" + msg.URL
		}
		out = []byte(s)
	}
	if len(out) > MaxBodyBytes {
		return nil, fmt.Errorf("the request body is %d bytes (limit %d)", len(out), MaxBodyBytes)
	}
	return out, nil
}

// BasicAuth is the header value of basic authentication, as a secret (its
// base64 form is registered for redaction too: an API may echo the header).
func BasicAuth(user string, password credential.Secret) credential.Secret {
	return credential.NewSecret("Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password.Reveal())))
}
