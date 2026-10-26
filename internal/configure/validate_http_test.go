package configure

import (
	"strings"
	"testing"
)

const httpBase = "[feathers.api]\ntype = \"http\"\n"

func TestHTTPFeatherValidation(t *testing.T) {
	good := []string{
		`url = "https://api.example.com/v1/alerts"`,
		"url = \"https://api.example.com/x?a=1\"\nmethod = \"PUT\"\nheaders = { \"X-Team\" = \"ops\" }\nquery = { source = \"kjak\" }",
		"url_secret = { secret = \"api.url\" }\n[feathers.api.auth]\ntype = \"bearer\"\ntoken = { env = \"API_TOKEN\" }",
		"url = \"https://api.example.com/\"\nmethod = \"GET\"\n[feathers.api.auth]\ntype = \"basic\"\nusername = \"kjak\"\npassword = { secret = \"api.pw\" }",
		"url = \"https://api.example.com/\"\n[feathers.api.auth]\ntype = \"header\"\nheader = \"X-Api-Key\"\nvalue = { keychain = { service = \"kjak\", account = \"api\" } }",
		"url = \"http://10.0.0.5:8080/hook\"\nallow_http = true\nallow_networks = [\"10.0.0.0/24\", \"fd00::1\"]\nallow_hosts = [\"intranet.example.com\"]\nmax_redirects = 2",
		"url = \"https://api.example.com/\"\ntimeout = \"3s\"\nsuccess_status = [\"2xx\", \"304\"]\nmax_response_bytes = 4096\nidempotency_key = true\nidempotency_header = \"X-Request-Id\"\n[feathers.api.retry]\npolicy = \"always\"\nmax_attempts = 3",
		"url = \"https://api.example.com/\"\n[feathers.api.body]\nformat = \"json\"\ntemplate = '{\"text\": \"{{.title}}: {{.body}}\", \"n\": 1}'",
		"url = \"https://api.example.com/\"\n[feathers.api.body]\nformat = \"text\"\ntemplate = \"{{.level}} {{.body}}\"\ncontent_type = \"text/markdown; charset=utf-8\"",
		"url = \"https://api.example.com/\"\nmethod = \"DELETE\"\n[feathers.api.body]\nformat = \"json\"",
	}
	for _, doc := range good {
		if _, err := Parse([]byte(httpBase + doc + "\n")); err != nil {
			t.Errorf("refused:\n%s\n%v", doc, err)
		}
	}
	bad := map[string]string{
		"no url":               `method = "POST"`,
		"both urls":            "url = \"https://a.example/\"\nurl_secret = { env = \"U\" }",
		"http without opt-in":  `url = "http://api.example.com/"`,
		"user info":            `url = "https://user:SECRETPW@api.example.com/"`,
		"fragment":             `url = "https://api.example.com/#x"`,
		"scheme":               `url = "file:///etc/passwd"`,
		"unix":                 `url = "unix:///var/run/docker.sock"`,
		"literal url userinfo": `url_secret = { literal = "https://u:SECRETPW@h.example/" }`,
		"method":               "url = \"https://a.example/\"\nmethod = \"post\"",
		"method connect":       "url = \"https://a.example/\"\nmethod = \"CONNECT\"",
		"get body":             "url = \"https://a.example/\"\nmethod = \"GET\"\n[feathers.api.body]\nformat = \"json\"",
		"host header":          "url = \"https://a.example/\"\nheaders = { Host = \"evil\" }",
		"authorization header": "url = \"https://a.example/\"\nheaders = { Authorization = \"Bearer x\" }",
		"cookie header":        "url = \"https://a.example/\"\nheaders = { Cookie = \"s=1\" }",
		"header injection":     "url = \"https://a.example/\"\nheaders = { \"X-A\" = \"a\\r\\nX-B: b\" }",
		"header name":          "url = \"https://a.example/\"\nheaders = { \"X A\" = \"a\" }",
		"auth type":            "url = \"https://a.example/\"\n[feathers.api.auth]\ntype = \"digest\"",
		"bearer no token":      "url = \"https://a.example/\"\n[feathers.api.auth]\ntype = \"bearer\"",
		"bearer plain token":   "url = \"https://a.example/\"\n[feathers.api.auth]\ntype = \"bearer\"\ntoken = \"SECRETPW\"",
		"basic colon":          "url = \"https://a.example/\"\n[feathers.api.auth]\ntype = \"basic\"\nusername = \"a:b\"\npassword = { env = \"P\" }",
		"basic extra":          "url = \"https://a.example/\"\n[feathers.api.auth]\ntype = \"basic\"\nusername = \"a\"\npassword = { env = \"P\" }\ntoken = { env = \"T\" }",
		"header dup":           "url = \"https://a.example/\"\nheaders = { \"X-Api-Key\" = \"a\" }\n[feathers.api.auth]\ntype = \"header\"\nheader = \"x-api-key\"\nvalue = { env = \"V\" }",
		"header host":          "url = \"https://a.example/\"\n[feathers.api.auth]\ntype = \"header\"\nheader = \"Host\"\nvalue = { env = \"V\" }",
		"json template":        "url = \"https://a.example/\"\n[feathers.api.body]\ntemplate = '{\"text\": '",
		"template syntax":      "url = \"https://a.example/\"\n[feathers.api.body]\nformat = \"text\"\ntemplate = \"{{.body\"",
		"body format":          "url = \"https://a.example/\"\n[feathers.api.body]\nformat = \"xml\"",
		"content type":         "url = \"https://a.example/\"\n[feathers.api.body]\ncontent_type = \"not a type\"",
		"timeout":              "url = \"https://a.example/\"\ntimeout = \"0s\"",
		"timeout long":         "url = \"https://a.example/\"\ntimeout = \"11m\"",
		"status":               "url = \"https://a.example/\"\nsuccess_status = [\"2x\"]",
		"status range":         "url = \"https://a.example/\"\nsuccess_status = [\"600\"]",
		"response bytes":       "url = \"https://a.example/\"\nmax_response_bytes = 99999999",
		"network":              "url = \"https://a.example/\"\nallow_networks = [\"10.0.0.0/33\"]",
		"network zone":         "url = \"https://a.example/\"\nallow_networks = [\"fe80::1%eth0\"]",
		"host":                 "url = \"https://a.example/\"\nallow_hosts = [\"bad host\"]",
		"redirects":            "url = \"https://a.example/\"\nmax_redirects = 11",
		"idempotency header":   "url = \"https://a.example/\"\nidempotency_header = \"X-Id\"",
		"idempotency dup":      "url = \"https://a.example/\"\nidempotency_key = true\nheaders = { \"idempotency-key\" = \"x\" }",
		"retry policy":         "url = \"https://a.example/\"\n[feathers.api.retry]\npolicy = \"sometimes\"",
		"retry attempts":       "url = \"https://a.example/\"\n[feathers.api.retry]\nmax_attempts = 12",
		"webhook field":        "url = \"https://a.example/\"\nwebhook_url = { env = \"X\" }",
		"email field":          "url = \"https://a.example/\"\nhost = \"smtp.example.com\"",
		"unknown key":          "url = \"https://a.example/\"\nproxy = \"http://p\"",
	}
	for name, doc := range bad {
		_, err := Parse([]byte(httpBase + doc + "\n"))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error(), "SECRETPW") {
			t.Errorf("%s: the error quotes a secret: %v", name, err)
		}
	}
	// http keys on another type
	if _, err := Parse([]byte("[feathers.s]\ntype = \"slack\"\nwebhook_url = { env = \"X\" }\nurl = \"https://a.example/\"\n")); err == nil {
		t.Error("a slack feather took url")
	}
}

func TestHTTPDefaultsAndMerge(t *testing.T) {
	f := mustParse(t, httpBase+"url = \"https://a.example/\"\nheaders = { A = \"1\" }\n[feathers.api.auth]\ntype = \"bearer\"\ntoken = { secret = \"a.t\" }\n")
	fc := f.Feathers["api"]
	if fc.HTTPMethod() != "POST" || fc.HTTPBodyFormat() != BodyJSON {
		t.Fatalf("%s %s", fc.HTTPMethod(), fc.HTTPBodyFormat())
	}
	fc.Method = "DELETE"
	if fc.HTTPBodyFormat() != BodyNone {
		t.Fatal("DELETE has a body by default")
	}
	merged := Merge(f, nil)
	m := merged.Feathers["api"]
	m.Headers["A"] = "changed"
	m.Auth.Type = "none"
	if f.Feathers["api"].Headers["A"] != "1" || f.Feathers["api"].Auth.Type != "bearer" {
		t.Fatal("Merge shares maps or pointers with its input")
	}
	if names := SecretNames(f); len(names) != 1 || names[0] != "a.t" {
		t.Fatalf("secret names %v", names)
	}
	b, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(b); err != nil {
		t.Fatalf("round trip: %v\n%s", err, b)
	}
}

func TestHTTPBodyTemplateRefusesRange(t *testing.T) {
	for _, tpl := range []string{"{{range 9}}x{{end}}", "{{define \"a\"}}x{{end}}"} {
		doc := httpBase + "url = \"https://a.example/\"\n[feathers.api.body]\nformat = \"text\"\ntemplate = '" + tpl + "'\n"
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s accepted", tpl)
		}
	}
}
