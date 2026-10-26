package configure

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"text/template/parse"
	"time"

	"github.com/magicdrive/kjak/internal/credential"
)

// Limits of an http feather.
const (
	DefaultHTTPTimeout          = 10 * time.Second
	DefaultMaxResponseBytes     = 64 << 10
	MaxMaxResponseBytes         = 10 << 20
	MaxHTTPRedirects            = 10
	MaxHTTPAttempts             = MaxMaxRetries + 1
	MaxHTTPHeaders              = 64
	MaxHTTPBodyTemplateBytes    = 64 << 10
	DefaultIdempotencyHeaderKey = "Idempotency-Key"
)

// HTTPMethods are the methods of an http feather.
var HTTPMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// IdempotentMethod: a method whose repetition has the effect of one
// request (RFC 9110 9.2.2); an http feather retries it under policy safe.
func IdempotentMethod(m string) bool { return m == "GET" || m == "PUT" || m == "DELETE" }

// reservedHeaders are set by kjak or by the transport, or carry
// credentials: a static header cannot set them.
var reservedHeaders = map[string]string{
	"host":                "set from the URL",
	"content-length":      "set by kjak",
	"transfer-encoding":   "set by the transport",
	"connection":          "set by the transport",
	"keep-alive":          "set by the transport",
	"upgrade":             "not supported",
	"te":                  "set by the transport",
	"trailer":             "not supported",
	"proxy-authorization": "a credential: kjak does not use proxies",
	"proxy-connection":    "set by the transport",
	"authorization":       "a credential: use [auth]",
	"content-type":        "use body.content_type",
	"cookie":              "a credential: use [auth] (type header)",
}

var headerToken = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$")

func checkHeaderName(name string) error {
	if !headerToken.MatchString(name) {
		return fmt.Errorf("%q is not a valid header name", name)
	}
	return nil
}

func checkHeaderValue(v string) error {
	if len(v) > 8<<10 {
		return errors.New("is longer than 8 KiB")
	}
	for _, r := range v {
		if r == '\r' || r == '\n' || r == 0 || (r < 0x20 && r != '\t') || r == 0x7f {
			return errors.New("contains a control character")
		}
	}
	return nil
}

// CheckHTTPURL checks the URL of an http feather: absolute https (http only
// with allowHTTP), a host, no user information, no fragment. Its errors
// never quote the URL (it may be a secret).
func CheckHTTPURL(s string, allowHTTP bool) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Opaque != "" || u.Hostname() == "" {
		return nil, errors.New("must be an absolute https URL")
	}
	if u.User != nil {
		return nil, errors.New("must not contain user information (credentials go in [auth])")
	}
	if u.Fragment != "" || strings.Contains(s, "#") {
		return nil, errors.New("must not contain a fragment")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !allowHTTP {
			return nil, errors.New("must be https (http needs allow_http = true)")
		}
	default:
		return nil, errors.New("must be an absolute https URL")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return nil, errors.New("has an invalid port")
		}
	}
	for _, r := range s {
		if r <= 0x20 || r == 0x7f {
			return nil, errors.New("must not contain spaces or control characters")
		}
	}
	return u, nil
}

// SuccessRange is one entry of success_status: a status, or a class
// ("2xx": 200-299).
type SuccessRange struct{ Lo, Hi int }

// ParseSuccessStatus parses success_status ("2xx" when empty).
func ParseSuccessStatus(list []string) ([]SuccessRange, error) {
	if len(list) == 0 {
		return []SuccessRange{{200, 299}}, nil
	}
	var out []SuccessRange
	for _, s := range list {
		switch {
		case len(s) == 3 && strings.HasSuffix(s, "xx") && s[0] >= '1' && s[0] <= '5':
			lo := int(s[0]-'0') * 100
			out = append(out, SuccessRange{lo, lo + 99})
		default:
			n, err := strconv.Atoi(s)
			if err != nil || len(s) != 3 || n < 100 || n > 599 {
				return nil, fmt.Errorf("success_status: %q is not a status (200) or a class (2xx)", s)
			}
			out = append(out, SuccessRange{n, n})
		}
	}
	return out, nil
}

// ParseAllowNetworks parses allow_networks: CIDR prefixes or addresses.
func ParseAllowNetworks(list []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range list {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil || a.Zone() != "" {
			return nil, fmt.Errorf("allow_networks: %q is not a CIDR prefix or an address", s)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

var hostName = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// TemplateData are the names an http body template can use.
var TemplateData = []string{"title", "body", "level", "url", "feather"}

func checkBodyTemplate(format, src string) error {
	if len(src) > MaxHTTPBodyTemplateBytes {
		return fmt.Errorf("is longer than %d bytes", MaxHTTPBodyTemplateBytes)
	}
	parse := func(s string) error {
		t, err := template.New("body").Option("missingkey=error").Parse(s)
		if err != nil {
			return err
		}
		return BodyTemplateProblem(t)
	}
	if format != BodyJSON {
		return parse(src)
	}
	// a JSON template is a JSON document whose strings are templates: the
	// structure is fixed here, a value can never add keys or elements
	var doc any
	if err := json.Unmarshal([]byte(src), &doc); err != nil {
		return fmt.Errorf("is not a JSON document: %v", err)
	}
	var walk func(any) error
	walk = func(v any) error {
		switch t := v.(type) {
		case string:
			return parse(t)
		case []any:
			for _, e := range t {
				if err := walk(e); err != nil {
					return err
				}
			}
		case map[string]any:
			for _, e := range t {
				if err := walk(e); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(doc)
}

// BodyTemplateProblem refuses what a body template cannot use: range (its
// data are strings; a range over a number could run without end), and
// template/define/block.
func BodyTemplateProblem(t *template.Template) error {
	var walk func(n parse.Node) error
	walk = func(n parse.Node) error {
		switch x := n.(type) {
		case *parse.ListNode:
			if x == nil {
				return nil
			}
			for _, c := range x.Nodes {
				if err := walk(c); err != nil {
					return err
				}
			}
		case *parse.RangeNode:
			return errors.New("range is not available in a body template")
		case *parse.TemplateNode:
			return errors.New("template is not available in a body template")
		case *parse.IfNode:
			if err := walk(x.List); err != nil {
				return err
			}
			return walk(x.ElseList)
		case *parse.WithNode:
			if err := walk(x.List); err != nil {
				return err
			}
			return walk(x.ElseList)
		}
		return nil
	}
	for _, tt := range t.Templates() {
		if tt.Name() != t.Name() {
			return errors.New("define and block are not available in a body template")
		}
		if tt.Tree != nil {
			if err := walk(tt.Root); err != nil {
				return err
			}
		}
	}
	return nil
}

// httpFieldsSet reports whether fc sets a key of the http type only
// (timeout and retry belong to external feathers too).
func httpFieldsSet(fc FeatherConfig) bool {
	return fc.URL != "" || fc.URLSecret != nil || fc.Method != "" || len(fc.Headers) > 0 || len(fc.Query) > 0 ||
		fc.Auth != nil || fc.Body != nil || len(fc.SuccessStatus) > 0 || fc.MaxResponseBytes != 0 ||
		fc.AllowHTTP || len(fc.AllowNetworks) > 0 || len(fc.AllowHosts) > 0 || fc.MaxRedirects != 0 ||
		fc.IdempotencyKey || fc.IdempotencyHeader != ""
}

// HTTPMethod is the method of an http feather (POST when unset).
func (f FeatherConfig) HTTPMethod() string {
	if f.Method == "" {
		return "POST"
	}
	return f.Method
}

// HTTPBodyFormat is the body format of an http feather: json for POST, PUT
// and PATCH, none for GET and DELETE, unless set.
func (f FeatherConfig) HTTPBodyFormat() string {
	if f.Body != nil && f.Body.Format != "" {
		return f.Body.Format
	}
	switch f.HTTPMethod() {
	case "GET", "DELETE":
		return BodyNone
	}
	return BodyJSON
}

func httpProblems(fc FeatherConfig) []string {
	var problems []string
	add := func(format string, a ...any) {
		problems = append(problems, fmt.Sprintf(format, a...))
	}
	switch {
	case fc.URL == "" && fc.URLSecret == nil:
		add("url (or url_secret, a credential) is required")
	case fc.URL != "" && fc.URLSecret != nil:
		add("give url or url_secret, not both")
	case fc.URL != "":
		if _, err := CheckHTTPURL(fc.URL, fc.AllowHTTP); err != nil {
			add("url: %v", err)
		}
	default:
		if err := fc.URLSecret.Validate(); err != nil {
			add("url_secret: %v", err)
		} else if fc.URLSecret.Literal != nil {
			if _, err := CheckHTTPURL(*fc.URLSecret.Literal, fc.AllowHTTP); err != nil {
				add("url_secret: %v", err)
			}
		}
	}
	method := fc.HTTPMethod()
	if !contains(HTTPMethods, method) {
		add("method must be one of %s (upper case)", strings.Join(HTTPMethods, ", "))
	}
	if len(fc.Headers) > MaxHTTPHeaders {
		add("headers: at most %d", MaxHTTPHeaders)
	}
	for _, name := range sortedKeys(fc.Headers) {
		if err := checkHeaderName(name); err != nil {
			add("headers: %v", err)
			continue
		}
		if why, ok := reservedHeaders[strings.ToLower(name)]; ok {
			add("headers: %s is %s", name, why)
		}
		if err := checkHeaderValue(fc.Headers[name]); err != nil {
			add("headers.%s %v", name, err)
		}
	}
	for _, k := range sortedKeys(fc.Query) {
		if k == "" || len(k) > 256 {
			add("query: a key is empty or too long")
		}
	}
	if a := fc.Auth; a != nil {
		ref := func(field string, r *credential.Ref) {
			if r == nil {
				add("auth.%s is required for type %q", field, a.Type)
			} else if err := r.Validate(); err != nil {
				add("auth.%s: %v", field, err)
			}
		}
		other := func(fields ...string) {
			set := map[string]bool{"token": a.Token != nil, "username": a.Username != "", "password": a.Password != nil, "header": a.Header != "", "value": a.Value != nil}
			for k, v := range set {
				if v && !contains(fields, k) {
					add("auth.%s does not belong to type %q", k, a.Type)
				}
			}
		}
		switch a.Type {
		case AuthNone:
			other()
		case AuthBearer:
			ref("token", a.Token)
			other("token")
		case AuthBasic:
			ref("password", a.Password)
			if a.Username == "" || strings.ContainsAny(a.Username, ":\r\n") {
				add("auth.username is required (without ':' or a line break)")
			}
			other("username", "password")
		case AuthHeader:
			ref("value", a.Value)
			if err := checkHeaderName(a.Header); err != nil {
				add("auth.header: %v", err)
			} else if why, ok := reservedHeaders[strings.ToLower(a.Header)]; ok && !strings.EqualFold(a.Header, "Authorization") && !strings.EqualFold(a.Header, "Cookie") {
				add("auth.header: %s is %s", a.Header, why)
			} else if _, dup := headerKeyFold(fc.Headers, a.Header); dup {
				add("auth.header: %s is also a static header", a.Header)
			}
			other("header", "value")
		default:
			add("auth.type must be none, bearer, basic or header")
		}
	}
	format := fc.HTTPBodyFormat()
	if b := fc.Body; b != nil {
		switch b.Format {
		case "", BodyJSON, BodyText, BodyNone:
		default:
			add("body.format must be json, text or none")
		}
		if b.Template != "" {
			if format == BodyNone {
				add("body.template needs body.format json or text")
			} else if err := checkBodyTemplate(format, b.Template); err != nil {
				add("body.template: %v", err)
			}
		}
		if b.ContentType != "" {
			if format == BodyNone {
				add("body.content_type needs a body")
			} else if _, _, err := mime.ParseMediaType(b.ContentType); err != nil || checkHeaderValue(b.ContentType) != nil {
				add("body.content_type: %q is not a media type", b.ContentType)
			}
		}
	}
	if method == "GET" && format != BodyNone {
		add("a GET request has no body (body.format = \"none\")")
	}
	if fc.Timeout != nil && (fc.Timeout.Duration <= 0 || fc.Timeout.Duration > MaxTimeout) {
		add("timeout must be between 0s (exclusive) and %s", MaxTimeout)
	}
	if _, err := ParseSuccessStatus(fc.SuccessStatus); err != nil {
		add("%v", err)
	}
	if fc.MaxResponseBytes < 0 || fc.MaxResponseBytes > MaxMaxResponseBytes {
		add("max_response_bytes must be between 1 and %d (0: %d)", MaxMaxResponseBytes, DefaultMaxResponseBytes)
	}
	if _, err := ParseAllowNetworks(fc.AllowNetworks); err != nil {
		add("%v", err)
	}
	for _, h := range fc.AllowHosts {
		if len(h) > 253 || !hostName.MatchString(strings.TrimSuffix(h, ".")) {
			add("allow_hosts: %q is not a host name", h)
		}
	}
	if fc.MaxRedirects < 0 || fc.MaxRedirects > MaxHTTPRedirects {
		add("max_redirects must be between 0 and %d", MaxHTTPRedirects)
	}
	if fc.IdempotencyHeader != "" {
		if !fc.IdempotencyKey {
			add("idempotency_header needs idempotency_key = true")
		} else if err := checkHeaderName(fc.IdempotencyHeader); err != nil {
			add("idempotency_header: %v", err)
		} else if _, reserved := reservedHeaders[strings.ToLower(fc.IdempotencyHeader)]; reserved {
			add("idempotency_header: %s is reserved", fc.IdempotencyHeader)
		}
	}
	if fc.IdempotencyKey {
		h := fc.IdempotencyHeader
		if h == "" {
			h = DefaultIdempotencyHeaderKey
		}
		if _, dup := headerKeyFold(fc.Headers, h); dup {
			add("headers: %s is the idempotency key's header", h)
		}
		if fc.Auth != nil && strings.EqualFold(fc.Auth.Header, h) {
			add("auth.header: %s is the idempotency key's header", h)
		}
	}
	if r := fc.Retry; r != nil {
		switch r.Policy {
		case "", RetrySafe, RetryNever, RetryAlways:
		default:
			add("retry.policy must be safe, never or always")
		}
		if r.MaxAttempts < 0 || r.MaxAttempts > MaxHTTPAttempts {
			add("retry.max_attempts must be between 1 and %d (0: no cap of its own)", MaxHTTPAttempts)
		}
	}
	return problems
}

func headerKeyFold(m map[string]string, name string) (string, bool) {
	for k := range m {
		if strings.EqualFold(k, name) {
			return k, true
		}
	}
	return "", false
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
