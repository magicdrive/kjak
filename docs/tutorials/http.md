# HTTP APIs and webhooks

[日本語](http_ja.md) · [All tutorials](README.md) · [Reference: HTTP feathers](../http.md)

This tutorial sends kjak's messages to any HTTP API or webhook with an
`http` feather: the request (method, URL, headers, authentication, body),
a first test against a local mock server, then a real API, and the options
for retries, templates and private networks. Every rule is in the
reference, [http.md](../http.md); this page shows how to get it working.

## 1. Overview

An `http` feather makes one HTTP request per attempt, and you decide what
it looks like:

```toml
[feathers.api]
type = "http"
url = "https://api.example.com/v1/notifications"

[feathers.api.auth]
type = "bearer"
token = { secret = "api.token" }
```

- **The request**: `method` (`POST` by default), `url`, `headers`, `query`,
  `[auth]` (bearer, basic or a header) and `[body]` — by default the
  message as JSON, `{"title", "body", "level", "url"}`, or your own JSON or
  text template.
- **The answer**: a status in `success_status` (default `2xx`) is
  `delivered`. 429, 408 and 5xx are temporary refusals; 502 and 504 mean
  the request may have arrived (`acceptance: unknown`, never retried).
- **Retries** follow the method: `GET`, `PUT` and `DELETE` are retried;
  `POST` and `PATCH` only with `idempotency_key = true`, which sends the same
  `Idempotency-Key` on every attempt of a send.
- **The address policy** (SSRF guard): `https` only, and only public
  addresses, unless the feather allows more (`allow_http`,
  `allow_networks`, `allow_hosts`). Redirects are not followed, and proxy
  environment variables are not used.
- It is not the `slack` or `discord` type: those know their provider's
  payload. Use `http` for everything else (Teams, Google Chat, Mattermost,
  PagerDuty, your own service, ...).

## 2. Prerequisites

- kjak installed (`kjak version`): [getting started](../getting-started.md#2-install).
- The documentation of the API you call: its URL, the method, how it
  authenticates, the body it expects, the statuses it answers on success,
  and whether it supports an idempotency key.
- Its credential (a token, a password, an API key) if it needs one.
- `python3`, for the local mock server of section 6 (optional, but the
  safest way to see what kjak sends).
- Network access to the API over `https`, without a proxy: an http feather
  connects directly.

## 3. Setup

1. From the API's documentation, note:

   | you need | goes to |
   |---|---|
   | the URL | `url` (or `url_secret` when the URL itself is the credential, like a webhook URL with a token in its path) |
   | the method | `method` (`POST` if not said otherwise) |
   | the authentication | `[auth]`: `bearer` (`Authorization: Bearer ...`), `basic`, or `header` (`X-Api-Key: ...`) |
   | the body | `[body]`: the default JSON, a JSON template, or text |
   | the success statuses | `success_status` (default `["2xx"]`) |

2. Put the credential in your **secrets file**, created private (mode 0600)
   before anything is written to it:

   ```sh
   mkdir -p ~/.config/kjak
   [ -e ~/.config/kjak/secrets.toml ] || install -m 600 /dev/null ~/.config/kjak/secrets.toml
   "${EDITOR:-vi}" ~/.config/kjak/secrets.toml
   ```

   ```toml
   # ~/.config/kjak/secrets.toml (mode 0600; replace the dummy value)
   [api]
   token = "replace-me"
   ```

   On Windows the file is `%AppData%\kjak\secrets.toml`. Other places for
   the credential: section 8.

## 4. Minimal configuration

The complete `~/.config/kjak/config.toml` for a JSON API with a bearer
token:

```toml
# ~/.config/kjak/config.toml
[feathers.api]
type = "http"
url = "https://api.example.com/v1/notifications"

[feathers.api.auth]
type = "bearer"
token = { secret = "api.token" }
```

kjak sends:

```
POST /v1/notifications HTTP/1.1
Host: api.example.com
User-Agent: kjak
Authorization: Bearer <the token>
Content-Type: application/json

{"title":"Deploy","body":"api 1.4.2 is live","level":"success"}
```

(`title` and `url` are left out when the message has none.) An API that
wants another shape needs a template: section 7.

## 5. CLI configuration

An http feather has too many keys for command-line options:
`kjak feather add NAME --type http` does not create one, and says so:

```
kjak: feather add: an http feather is configured in the file ([feathers.api] type = "http", docs/http.md); then run kjak config validate
```

Write it in the file (`kjak config path` shows which file), then check it
with the CLI:

```sh
kjak config path
kjak config validate
kjak feather show api
kjak feather list
```

`kjak config validate` names every wrong key (`url: must be https (http
needs allow_http = true)`, an unknown key with its line). `feather show`
prints the method, the URL, where the credential comes from and the
address policy; never the token:

```
name     api
origin   global
type     http
enabled  true
levels   all
method   POST
url      https://api.example.com/v1/notifications
auth     bearer from secret:api.token
```

The other feather commands work as for every type:

```sh
kjak feather disable api
kjak feather enable api
kjak feather remove api
```

## 6. First notification

### Against a local mock server

Before calling the real API, see exactly what kjak sends. `python3 -m
http.server` answers only `GET`; this mock server answers every request
with 200 and prints it:

```python
# mock_server.py: answers every request with 200 and prints it
from http.server import BaseHTTPRequestHandler, HTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        print(self.command, self.path)
        print(self.headers, end="")
        print(body.decode("utf-8", "replace"), flush=True)
        self.send_response(200)
        self.end_headers()

    do_PUT = do_PATCH = do_DELETE = do_GET = do_POST

HTTPServer(("127.0.0.1", 8080), Handler).serve_forever()
```

```sh
python3 mock_server.py      # in a second terminal; Ctrl-C stops it
```

The address policy refuses `http://` and loopback addresses by default, so
the test feather allows exactly this one: `allow_http = true` for the
scheme, `allow_networks = ["127.0.0.1/32"]` for the address (loopback is
allowed only by `allow_networks`, not by `allow_hosts`):

```toml
# ~/.config/kjak/config.toml
[feathers.mock]
type = "http"
url = "http://127.0.0.1:8080/notify"
allow_http = true
allow_networks = ["127.0.0.1/32"]
```

```sh
kjak send --to mock --title "Deploy" "api 1.4.2 is live"
```

```
ok    mock (http) delivered in 31ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

and the mock server prints:

```
POST /notify
Host: 127.0.0.1:8080
User-Agent: kjak
Content-Length: 60
Content-Type: application/json
Connection: close

{"title":"Deploy","body":"api 1.4.2 is live","level":"info"}
```

Without `allow_networks`, the same send fails before connecting:
`the request was not sent: address 127.0.0.1 is loopback: allow it with
allow_networks`. Use `127.0.0.1` rather than `localhost` in the URL: a
name that also resolves to `::1` needs that address allowed too.

Keep `allow_http` and `allow_networks` for the test feather only; remove
the `mock` feather when you are done.

### Against the API

With the configuration of section 4:

```sh
kjak send --to api --dry-run "test"
kjak send --to api --title "Deploy" --level success "api 1.4.2 is live"
```

`--dry-run` resolves the URL and the token and sends nothing. The real send
prints `ok    api (http) delivered ...` when the API answered a status of
`success_status`. The exit status is 0 when it was delivered, 1 when a
delivery failed, 2 when nothing was sent. The JSON report shows each
attempt with its status code, wait and error:

```sh
kjak send --to api -o json "test"
```

## 7. Advanced configuration

A complete configuration for an API that wants its own JSON shape, may be
retried thanks to an idempotency key, and answers 201 or 202:

```toml
# ~/.config/kjak/config.toml
[defaults]
timeout = "30s"        # the whole send: every attempt and wait
max_retries = 3

[feathers.incidents]
type = "http"
url = "https://api.example.com/v2/incidents"
method = "POST"
headers = { "X-Team" = "ops" }
query = { source = "kjak" }
success_status = ["201", "202"]
timeout = "5s"                  # one attempt
idempotency_key = true          # POST may be retried: same key on every attempt
levels = ["warning", "error", "critical"]

[feathers.incidents.auth]
type = "header"
header = "X-Api-Key"
value = { secret = "incidents.api_key" }

[feathers.incidents.body]
format = "json"
template = '''
{"summary": "[{{.level}}] {{.title}}",
 "details": "{{.body}}",
 "link": "{{.url}}",
 "source": {"tool": "kjak", "feather": "{{.feather}}"},
 "page": true}
'''

[feathers.retry_cap]
type = "http"
url = "https://status.example.com/api/components/42"
method = "PUT"
success_status = ["200", "204"]

[feathers.retry_cap.auth]
type = "basic"
username = "kjak"
password = { env = "STATUS_API_PASSWORD" }

[feathers.retry_cap.body]
template = '{"status": "{{.level}}", "message": "{{.body}}"}'

[feathers.retry_cap.retry]
max_attempts = 2
```

- **JSON templates** are a JSON document whose string values are
  templates (`{{.title}}`, `{{.body}}`, `{{.level}}`, `{{.url}}`,
  `{{.feather}}`). kjak parses the document when it reads the
  configuration and encodes it again at each send, so a message cannot
  change its structure (a quote in a title stays in its string). Numbers,
  booleans and `null` stay as written; the keys come out in sorted order.
  For a plain-text API use `format = "text"` (never for JSON).
- **Methods and retries**: the default policy `safe` retries `GET`, `PUT`
  and `DELETE`, and `POST` / `PATCH` only with `idempotency_key = true`
  (header `Idempotency-Key`, or `idempotency_header`). `retry.policy =
  "never"` sends once; `"always"` retries every method and **may deliver a
  `POST` twice**. `retry.max_attempts` caps the attempts below `1 +
  max_retries`.
- **`timeout`** bounds one attempt; `[defaults] timeout` (or `--timeout`)
  bounds the whole send.
- **`success_status`** lists statuses (`"200"`) or classes (`"2xx"`); a
  2xx that is not listed is a refusal.
- **`url_secret`** replaces `url` when the URL is the credential (a webhook
  URL with a token in it): it is never printed.

```toml
# ~/.config/kjak/config.toml
[feathers.chat]
type = "http"
url_secret = { secret = "chat.webhook_url" }

[feathers.chat.body]
template = '{"text": "*{{.title}}*\n{{.body}}"}'
```

- **A service on your private network** is refused until you allow it:
  `allow_hosts` for a host name (its private addresses only),
  `allow_networks` for CIDR ranges. Cloud metadata addresses
  (`169.254.169.254`, ...) are never allowed.

```toml
# ~/.config/kjak/config.toml
[feathers.internal]
type = "http"
url = "https://alerts.corp.example/api"
allow_hosts = ["alerts.corp.example"]
max_redirects = 1          # follow one redirect, checked like the URL
```

- **Redirects** are refused (`HTTP 302: a redirect that is not followed`)
  unless `max_redirects` allows them; credentials are dropped when a
  redirect changes the scheme, host or port.
- **`GET`** (a heartbeat URL) has no body: `method = "GET"` is enough.

## 8. Secrets

Every credential of an http feather is a reference with exactly one source:
`{ secret = "..." }` (your secrets file), `{ env = "..." }` or
`{ keychain = { service = "...", account = "..." } }` (macOS, Linux).

| key | sends |
|---|---|
| `url_secret` | the whole URL (never printed; its path and query are redacted from errors) |
| `auth.token` (`bearer`) | `Authorization: Bearer <token>` |
| `auth.password` (`basic`, with a plain `auth.username`) | `Authorization: Basic <base64>` |
| `auth.value` (`header`, with `auth.header` naming it) | `<header>: <value>` |

The secrets file of the examples above:

```toml
# ~/.config/kjak/secrets.toml (mode 0600; dummy values)
[api]
token = "replace-me"

[incidents]
api_key = "replace-me-too"

[chat]
webhook_url = "https://chat.example.com/hooks/replace-me"
```

- Secrets go only where `[auth]` and `url_secret` put them: `headers`,
  `query` and templates hold plain text, and no secret is available to a
  template. An API that wants its key in the query needs `url_secret`.
- A resolved value with a line break is refused before anything is sent.
- The token, the password, the encoded basic credentials, the header value
  and a secret URL are redacted from every error and report, including what
  the API echoes back.
- For `env` in CI, fill the variable from the CI's secret store; do not
  type `VAR=value kjak ...` where your shell history keeps it.
- `kjak config validate --strict` refuses `{ literal = "..." }` (deprecated).

## 9. Troubleshooting

| message | cause | fix |
|---|---|---|
| `url: must be https (http needs allow_http = true)` | an `http://` URL | use `https`; `allow_http = true` only for a local relay or a test |
| `the request was not sent: address 127.0.0.1 is loopback: allow it with allow_networks` (`blocked`) | a local URL | `allow_networks = ["127.0.0.1/32"]` (section 6) |
| `the request was not sent: address 10.0.0.5 is private: allow it with allow_networks or allow_hosts` | a private service | `allow_hosts = ["the.host"]` or `allow_networks = ["10.0.0.0/24"]` |
| `... is cloud metadata: never allowed` | the URL (or a redirect) points at an instance metadata endpoint | not possible, on purpose |
| `HTTP 302: a redirect that is not followed (max_redirects)` | the API redirects | use the final URL, or `max_redirects = N` |
| `HTTP 401: ...` / `HTTP 403: ...` | a wrong or revoked credential, or the wrong `[auth]` type | check the token and the API's authentication scheme; not retried |
| `HTTP 200 is not in success_status` | `success_status` lists other statuses | add it |
| `(not retried: POST is not repeated without an idempotency key; retry.policy)` | a POST refused with a temporary status | `idempotency_key = true` if the API supports it; `retry.policy = "always"` only if a duplicate is acceptable |
| `HTTP 502 from a gateway: the API may have accepted it` (`????`) | a gateway answered for the API | check the API before sending again; kjak does not retry it |
| `no response within the attempt timeout of 5s after the request was sent; the API may have accepted it` | the API is slow | raise `timeout`; check before sending again |
| `auth.token: credential error: environment variable KJAK_API_TOKEN is not set` | an `env` reference | export the variable in kjak's environment |
| `headers: Authorization is ...` | a credential in `headers` | use `[auth]` |
| `feather add: an http feather is configured in the file` | `feather add --type http` | write `[feathers.NAME]` in the file (section 5) |
| `TLS certificate verification failed` | a private CA, or the wrong host name | see [certificate authorities](../security.md#certificate-authorities-ssl_cert_file-ssl_cert_dir) |

More: [troubleshooting](../troubleshooting.md) and the
[acceptance table](../http.md#success-errors-and-the-response).

```sh
kjak config validate
kjak send --to api --dry-run "test"
kjak send --to api -o json "test"
```

## 10. Security notes

- **The address policy** stops a configuration from reaching what only your
  machine can reach: loopback, link-local and private addresses need an
  explicit `allow_networks` / `allow_hosts`, and cloud metadata, unspecified,
  multicast and reserved addresses are never allowed. kjak resolves the host
  once and connects to the address it checked (no DNS rebinding); every
  redirect goes through the same check.
- **No proxy**: `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` are ignored by
  http feathers (the guard would see the proxy, not the destination). An
  API reachable only through a proxy is not supported.
- **`allow_http = true`** sends the request, the token and the answer in
  clear text: keep it for a test or a relay on this machine.
- **`retry.policy = "always"`** can deliver a `POST` twice. Prefer an
  idempotency key.
- **Certificates** are always verified (TLS 1.2 or later); there is no
  option to skip that.
- **Project files** (`.kjak.toml`): `kjak config trust` shows each http
  feather's method, URL (or its source), authentication source and every
  `allow_*` and redirect setting before you trust it, and its `env` and
  `keychain` references must be allowed by name.
- Details: [security: HTTP feathers and SSRF](../security.md#http-feathers-and-ssrf).

## 11. Related references

- [HTTP feathers](../http.md) — every key and rule (the reference of this tutorial)
- [The address policy (SSRF)](../http.md#the-address-policy-ssrf)
- [Retries and idempotency](../http.md#retries-and-idempotency)
- [Templates](../http.md#templates)
- [Configuration: http](../configuration.md#http) — the table of keys
- [Delivery: HTTP feathers](../delivery.md#http-feathers)
- [Security: HTTP feathers and SSRF](../security.md#http-feathers-and-ssrf)
- [Example: http-only.toml](../examples/http-only.toml)
- [Troubleshooting](../troubleshooting.md)
