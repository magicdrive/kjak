# HTTP API feathers (`type = "http"`)

[Configuration](configuration.md#http) · [Security](security.md#http-feathers-and-ssrf) · [Delivery](delivery.md#http-feathers)

An `http` feather sends a message to any HTTP API or webhook: you choose the
method, URL, headers, query, authentication and body. It is not the
`slack` or `discord` type (those know their provider's payload and
answers); kjak's internal `feather/webhook` sender of those two is not
involved.

```toml
[feathers.ops_api]
type = "http"
url = "https://api.example.com/v1/alerts"
method = "POST"                                   # default
headers = { "X-Team" = "ops" }
query = { source = "kjak" }
success_status = ["2xx"]                          # default
timeout = "5s"                                    # of one attempt

[feathers.ops_api.auth]
type = "bearer"
token = { secret = "ops_api.token" }

[feathers.ops_api.body]
format = "json"
template = '{"summary": "[{{.level}}] {{.title}}", "details": "{{.body}}", "link": "{{.url}}"}'
```

```sh
kjak config validate
kjak send --to ops_api --dry-run "test"      # resolves the URL and the secrets, sends nothing
kjak send --to ops_api --title "Deploy" "api 1.4.2 is live"
```

`feather add` does not create http feathers (they have too many keys for
options): write them in the file and run `kjak config validate`. Every key
is listed in [configuration.md](configuration.md#http).

## Methods

`GET`, `POST` (default), `PUT`, `PATCH`, `DELETE`, in upper case. `GET` has
no body; `DELETE` has none unless `body.format` says so.

## URL

- `url = "https://..."`: a plain URL, shown by `feather show` and
  `config trust`.
- `url_secret = { secret = "..." }` (or `env`, `keychain`): a URL that is a
  credential (a webhook URL with a token in its path, like Slack's, Teams'
  or Google Chat's). It is never printed; its path and query are redacted
  from errors.

Exactly one of them. The URL must be `https` (`http` only with `allow_http =
true`), must have a host, and must not contain user information
(`https://user:pass@...` is refused: use `[auth]`) or a fragment.

`query` adds parameters to the URL's own query (keys sorted). Query values
are plain strings: kjak never puts a secret in a query, where proxies and
server logs keep it. An API that wants its key in the query needs
`url_secret`.

## Headers

`headers` are sent as they are, with plain string values (no secret: use
`[auth]`). Refused, because kjak or the transport sets them, or because they
carry credentials: `Host`, `Content-Length`, `Transfer-Encoding`,
`Connection`, `Keep-Alive`, `Upgrade`, `TE`, `Trailer`, `Proxy-*`,
`Authorization` and `Cookie` (use `[auth]`), `Content-Type` (use
`body.content_type`). Names are HTTP tokens; values cannot hold control
characters (no header injection). kjak sends `User-Agent: kjak`.

## Authentication

| `auth.type` | keys | sends |
|---|---|---|
| `none` (or no `[auth]`) | — | nothing |
| `bearer` | `token` (credential) | `Authorization: Bearer <token>` |
| `basic` | `username` (plain), `password` (credential) | `Authorization: Basic <base64>` |
| `header` | `header` (a name), `value` (credential) | `<header>: <value>` (e.g. `X-Api-Key`, or `Authorization` with a custom scheme) |

Every secret is a credential reference: `{ secret = ... }`, `{ env = ... }`,
`{ keychain = ... }` (`{ literal = ... }` works and is deprecated). A
resolved value with a line break is refused before anything is sent. The
token, the password, the encoded basic credentials and the header value are
redacted from every error and report, including what the API echoes back.

## Body

| `body.format` | default for | sends | `Content-Type` |
|---|---|---|---|
| `json` | `POST`, `PUT`, `PATCH` | `{"title", "body", "level", "url"}` (the shape of `--json-input`), or `template` | `application/json` |
| `text` | — | `[LEVEL] title`, the body, the URL; or `template` | `text/plain; charset=utf-8` |
| `none` | `GET`, `DELETE` | nothing | — |

`body.content_type` replaces the media type (`application/vnd.api+json`,
`text/markdown`, ...).

### Templates

`body.template` is Go `text/template`, as for [message templates](configuration.md#6-templates):
only the built-in actions, and a name that is not there is an error. A
template sees the message after `--template`, `--title`, `--level` and so
on were applied:

| name | value |
|---|---|
| `{{.title}}` | the title ("" without one) |
| `{{.body}}` | the body |
| `{{.level}}` | `info`, `success`, `warning`, `error`, `critical` |
| `{{.url}}` | the link ("" without one) |
| `{{.feather}}` | the feather's name |

With `format = "json"`, the template is a **JSON document whose string
values are templates**. kjak parses it when the configuration is read, and
at each send renders every string and encodes the document again. The
message therefore cannot change the structure: a title like
`x", "admin": true` stays inside its string, and quotes, line breaks and
`</script>` are escaped by the JSON encoder. Numbers, booleans and `null`
stay as written. Keys are not rendered.

```toml
[feathers.pager.body]
template = '''
{"routing_key": "service-a",
 "event_action": "trigger",
 "payload": {"summary": "{{.title}}: {{.body}}", "severity": "{{.level}}", "source": "kjak"}}
'''
```

With `format = "text"`, the template is rendered as it is: right for
plain-text or Markdown APIs, **wrong for JSON** (use `json`).

Nothing in a message is run as a template (`{{...}}` in a body is text), and
no secret is ever available to a template. A body is at most 1 MiB.

## Success, errors and the response

- `success_status`: the statuses that mean delivered, as `"200"` or a
  class `"2xx"`; default `["2xx"]`. A 2xx that is not listed is a refusal.
- At most `max_response_bytes` (default 64 KiB, at most 10 MiB) of the
  response body is read; it goes, shortened to 200 characters and cleaned of
  control characters and secrets, into the error of a refusal.
- `timeout` (default 10s) bounds one attempt; the send's deadline
  (`--timeout`) bounds all of them.

| outcome | acceptance | retried (by the retry policy) |
|---|---|---|
| status in `success_status` | confirmed | — |
| 429 | rejected | yes, after `Retry-After` |
| 408, 500, 503, other 5xx | rejected | yes (`Retry-After` honored) |
| 502, 504 | **unknown** | no — a gateway may have forwarded it |
| 3xx not followed | rejected (`protocol`) | no |
| other statuses | rejected | no |
| address or redirect refused by the policy | rejected (`blocked`) | no |
| error before the request was fully written | rejected (`network`) | yes |
| TLS certificate refused | rejected | no |
| attempt timeout, deadline or SIGINT after it was written; connection lost | **unknown** | no |

## Retries and idempotency

The engine retries a retryable refusal up to `max_retries` (default 2),
with the usual backoff and jitter, or after `Retry-After`, within the
deadline ([delivery.md](delivery.md)). An http feather decides whether a
refusal may be retried at all, from its method:

| `retry.policy` | retries |
|---|---|
| `safe` (default) | `GET`, `PUT`, `DELETE` (idempotent methods); `POST`, `PATCH` only with `idempotency_key = true` |
| `never` | nothing |
| `always` | every method — **a `POST` without a key may then act twice** |

- `retry.max_attempts` caps the attempts, the first included, below the
  send's `1 + max_retries` (default: no cap of its own).
- `idempotency_key = true` sends a random key (128 bits, hex) in
  `Idempotency-Key` (or `idempotency_header`), the **same key on every
  attempt of one send**, a new key for the next send. An API that supports
  it ([IETF draft](https://datatracker.ietf.org/doc/draft-ietf-httpapi-idempotency-key-header/),
  Stripe-style) performs the request once.
- A request that may have arrived (acceptance `unknown`) is **never**
  retried, whatever the method or the policy: kjak's rule for every feather.
- A `POST` refused with a 5xx is reported as rejected: the API said it
  failed. An API whose 5xx may still have acted is exactly why `POST` is not
  retried by default.

## The address policy (SSRF)

A configuration — a trusted project's too — could otherwise make kjak
connect to what only your machine can reach. Every connection of an http
feather, redirects included, goes through a guard:

| destination | default | allowed by |
|---|---|---|
| public addresses | allowed | — |
| private: 10/8, 172.16/12, 192.168/16, 100.64/10 (CGNAT), fc00::/7 (ULA), fec0::/10, 192.0.0/24, 198.18/15, documentation ranges | refused | `allow_networks` (CIDR) or `allow_hosts` (that host name) |
| loopback: 127/8, ::1 | refused | `allow_networks` only |
| link-local: 169.254/16, fe80::/10 | refused | `allow_networks` only |
| IPv6 transition: NAT64 (64:ff9b::/96), 6to4 (2002::/16), Teredo | refused | `allow_networks` only; classified by the IPv4 address they carry |
| cloud metadata: 169.254.169.254, 169.254.170.2, fd00:ec2::254, 100.100.100.200, 192.0.0.192 | **never** | — |
| unspecified (0.0.0.0, ::), multicast, broadcast, reserved (0/8, 240/4, IPv4-compatible ::/96) | **never** | — |

- IPv4-mapped IPv6 (`::ffff:127.0.0.1`) is checked as the IPv4 address.
- kjak resolves the host once, checks every address, and connects to a
  checked address: a DNS answer that changes after the check (DNS
  rebinding) is never used. The dialer checks the socket's address again
  before it connects. The certificate is still verified for the host name.
- Only TCP: no Unix socket, whatever the URL.
- `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` are **ignored** by http feathers:
  through a proxy, kjak would check the proxy's address, not the
  destination's. (The slack and discord feathers keep using them.) An http
  feather that must go through a proxy is not supported.
- `allow_http = true` permits `http://` (the request, the token and the
  answer then travel in clear text; for a local relay or a test).
- Redirects are not followed (`max_redirects = 0`): a 3xx is a refusal.
  With `max_redirects = N`, kjak follows up to N, each one through the same
  guard, never from https to http (unless `allow_http`), never to a URL with
  user information; `Authorization`, `Cookie` and the `[auth]` header are
  removed as soon as the scheme, host or port differs from the configured
  URL's (stricter than browsers and `curl`, which keep them for a
  subdomain). A `301`/`302`/`303` turns a `POST` into a `GET` without body
  (as browsers do); `307`/`308` repeat it.

```toml
# an internal service on the private network
[feathers.internal]
type = "http"
url = "https://alerts.corp.example/api"
allow_hosts = ["alerts.corp.example"]          # its private addresses only

# a local relay over http
[feathers.relay]
type = "http"
url = "http://127.0.0.1:8080/notify"
allow_http = true
allow_networks = ["127.0.0.1/32"]
```

`kjak config trust` shows the method, the URL (or where it comes from), the
authentication source and every `allow_*`/redirect setting of a project's
http feathers before you trust them. Their `env` and `keychain` references
(and `secret` references to your own secrets file) must be allowed by name:
`config trust` asks a second question for them (`--allow-user-secrets` with
`--yes`), and a run refuses any other.

## Examples

**A generic JSON webhook with a secret URL** (Teams, Google Chat, Mattermost
style):

```toml
[feathers.chat]
type = "http"
url_secret = { secret = "chat.webhook_url" }

[feathers.chat.body]
template = '{"text": "*{{.title}}*\n{{.body}}"}'
```

**An API key in a header, PUT, retried**:

```toml
[feathers.status]
type = "http"
url = "https://status.example.com/api/components/42"
method = "PUT"
success_status = ["200", "204"]

[feathers.status.auth]
type = "header"
header = "X-Api-Key"
value = { env = "STATUS_API_KEY" }

[feathers.status.body]
template = '{"status": "{{.level}}", "message": "{{.body}}"}'

[feathers.status.retry]
max_attempts = 3
```

**A POST that may be retried, thanks to an idempotency key**:

```toml
[feathers.incidents]
type = "http"
url = "https://api.example.com/v2/incidents"
idempotency_key = true

[feathers.incidents.auth]
type = "basic"
username = "kjak"
password = { keychain = { service = "kjak", account = "incidents" } }
```

**A GET ping** (a heartbeat URL): `method = "GET"`, no body.

## Testing a feather

- `kjak send --to NAME --dry-run test`: the URL, the secrets and the
  configuration, without a request.
- A local endpoint: `allow_http = true`, `allow_networks = ["127.0.0.1/32"]`,
  and a listener (`python3 -m http.server`, `nc -l`).
- `-o json` shows every attempt (`history`), with its status, the wait and
  the error.
