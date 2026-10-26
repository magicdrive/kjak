# Configuration reference

[日本語](configuration_ja.md) · [Getting started](getting-started.md) · [README](../README.md)

- [1. Configuration files](#1-configuration-files)
- [2. Managing the configuration with the CLI](#2-managing-the-configuration-with-the-cli)
- [3. `[defaults]`](#3-defaults)
- [4. Feathers](#4-feathers)
- [5. Groups](#5-groups)
- [6. Templates](#6-templates)
- [7. Credentials and security](#7-credentials-and-security) ([secrets files](#77-secrets-files))
- [8. Delivery behavior](#8-delivery-behavior)
- [9. A complete example](#9-a-complete-example)
- [10. Changes made by kjak](#10-changes-made-by-kjak)

## 1. Configuration files

kjak reads TOML from two configuration files and two secrets files:

| file | location | chosen as | used |
|---|---|---|---|
| global | `--config FILE`, else `$KJAK_CONFIG`, else `$XDG_CONFIG_HOME/kjak/config.toml`, else `~/.config/kjak/config.toml` (also on macOS); on Windows without `XDG_CONFIG_HOME`: `%AppData%\kjak\config.toml` | explicit (`--config`, `$KJAK_CONFIG`), else user | always, when it exists |
| project | `./.kjak.toml` (the current directory only; parent directories are not searched) | current | only when trusted ([7.4](#74-project-configuration-trust)); `--no-project` ignores it |
| personal secrets | `--secrets FILE`, else `$KJAK_SECRETS`, else `secrets.toml` in the directory of the default global file | explicit (`--secrets`, `$KJAK_SECRETS`), else user | for the `secret` references of the global file, and of the project file for the names its trust allows ([7.7](#77-secrets-files)) |
| project secrets | `./.kjak.secrets.toml` | current | for the `secret` references of the project file only, trusted with it; `--no-project` ignores it |

The three kinds of location are **explicit** (a command-line option or an
environment variable: used as given, instead of the user file), **user**
(your configuration directory) and **current** (the current directory: a
repository you may not control, used only once trusted). Each file is chosen
on its own: `--config` does not move the secrets file, and `--secrets` does
not move the configuration. `kjak config path` shows which file was chosen
for each, and how, and their state; never a secret.

The trust store is `trust.toml` next to the default global file
(`$XDG_CONFIG_HOME/kjak/trust.toml`, ...); `--config` and `KJAK_CONFIG` do not
move it.

```sh
kjak config path        # which files kjak uses, and the state of ./.kjak.toml
kjak config init        # creates the global file (--force overwrites it)
```

`config init` writes a commented starting file. A missing global file is not
an error: kjak runs with the built-in defaults.

**Precedence.** The project file is merged over the global file:

| part | rule |
|---|---|
| `[defaults]` | field by field: a field the project file sets wins, the others stay |
| `[feathers.NAME]` | by name, replaced **whole**: no field of the global entry remains |
| `[groups.NAME]` | by name, replaced whole |
| `[templates.NAME]` | by name, replaced whole |

A project group may name global feathers. References are checked on the
merged result. Command-line options (`--timeout`, `--level`, ...) win over
both files.

**Format.** Both files have the same format, at most 1 MiB each. Unknown keys
and keys given twice are errors, so a typo is never ignored. `kjak config
validate` checks both files (the project file whether trusted or not);
`kjak config show` prints the merged result with literal secrets redacted.
`kjak config validate --strict` also refuses literal secrets.

## 2. Managing the configuration with the CLI

| task | feathers | groups | templates |
|---|---|---|---|
| list | `kjak feather list` | `kjak group list` | `kjak template list` |
| show one | `kjak feather show NAME` | `kjak group show NAME` | `kjak template show NAME` |
| add | `kjak feather add NAME --type T ...` | `kjak group set NAME FEATHER...` | `kjak template set NAME --body ...` |
| change | `kjak feather add NAME ... --force` (replaces the whole feather), `enable` / `disable` | `kjak group set` again (replaces the member list) | `kjak template set` again |
| remove | `kjak feather remove NAME` | `kjak group remove NAME` | `kjak template remove NAME` |
| try | `kjak send --to NAME --dry-run "test"`, then without `--dry-run` | same | `kjak template render NAME --var K=V` |

`list` and `show` accept `-o json`. There is no command that changes a
single key of a feather (its host, for example); run `feather add --force`
with every option, or edit the file and run `kjak config validate`.

Other settings — `[defaults]`, and keys that have no option — are edited in
the file.

**Scope.** `add`/`set` write the global file unless `--scope project` is
given. Without `--scope`, `remove`, `enable` and `disable` change the file
that defines the entry. A project file is changed only when it is trusted
(or does not exist yet).

**Checking.**

```sh
kjak config validate          # syntax, keys, references, of both files
kjak config show              # the merged configuration
kjak send --to NAME --dry-run "test"   # routing and credentials, nothing sent
```

## 3. `[defaults]`

| key | type | default | meaning |
|---|---|---|---|
| `timeout` | duration string | `"10s"` | the deadline of a whole send once its message is read: credentials, every retry and wait included (max `10m`) |
| `max_retries` | integer | `2` | retries of a retryable failure, after the first attempt (0–10) |
| `concurrency` | integer | `5` | feathers delivered at the same time (1–64) |
| `level` | string | `"info"` | the level of a message that names none |
| `targets` | list of names | — | the feathers and groups of a send without `--to` |

```toml
[defaults]
timeout = "15s"
level = "info"
targets = ["development"]
```

The options `--timeout`, `--max-retries`, `--concurrency`, `--level` and
`--to` of `kjak send` override them for one send.

## 4. Feathers

A feather is one destination: a Slack incoming webhook, a Discord webhook,
an SMTP server with its recipients, or any HTTP API. `kjak feather types` lists the types.

**Names** (feathers, groups, templates): letters, digits, `_`, `.`, `-`; at
most 64; starting with a letter or a digit. A name cannot be both a feather
and a group.

**Using one:** `kjak send --to NAME`. A feather named directly must be
enabled and accept the message's level, otherwise the send stops before
sending anything (exit 2).

### Common keys

| key | type | default | CLI (`feather add`) | meaning |
|---|---|---|---|---|
| `type` | string | required | `--type` | `slack`, `discord`, `email` or `http` (OS-native notifications are not available) |
| `enabled` | bool | `true` | `--disabled`; `feather enable` / `disable` | a disabled feather is not sent to |
| `levels` | list | all levels | `--levels L[,L]` | the levels the feather accepts |

A key that does not belong to the type is an error.

### slack

| key | CLI | meaning |
|---|---|---|
| `webhook_url` | `--secret-env`, `--secret-keychain`, `--secret-stdin` | the incoming webhook URL (a [credential](#71-credential-references)) |

```toml
[feathers.dev_slack]
type = "slack"
webhook_url = { env = "KJAK_DEV_SLACK_WEBHOOK" }
levels = ["warning", "error", "critical"]
```

The message is posted as a text line (`[LEVEL] title: first line`) and an
attachment colored by level with the title, the body, and the URL as the
title link. `&`, `<` and `>` are escaped, so a message cannot mention
`@channel` or users. An incoming webhook posts to the channel, and as the app,
it was created for.

### discord

| key | CLI | meaning |
|---|---|---|
| `webhook_url` | `--secret-env`, `--secret-keychain`, `--secret-stdin` | the webhook URL |
| `username` | `--username` | display name of the messages (optional) |

```toml
[feathers.ops_discord]
type = "discord"
webhook_url = { keychain = { service = "kjak", account = "ops_discord" } }
username = "kjak"
```

The message is one embed colored by level. kjak executes the webhook with
`?wait=true` (keeping a `thread_id` of the URL) so that Discord confirms the
created message. Mentions are disabled (`allowed_mentions.parse = []`). A body
longer than 4096 characters is cut and ends with `…`.

### email

| key | CLI | default | meaning |
|---|---|---|---|
| `host` | `--host` | required | SMTP server |
| `port` | `--port` | 587 / 465 / 25 | by `security`: `starttls` / `tls` / `none` |
| `security` | `--security` | `"starttls"` | `starttls` (required, never falls back), `tls` (implicit TLS), `none` |
| `user` | `--user` | — | SMTP user; goes with `password` |
| `password` | `--secret-env`, `--secret-keychain`, `--secret-stdin` | — | a credential |
| `from` | `--from` | required | sender, a bare address |
| `to` | `--mail-to` | required | recipients, bare addresses |
| `subject_prefix` | `--subject-prefix` | — | put before every subject |

```toml
[feathers.oncall_mail]
type = "email"
host = "smtp.example.com"
port = 587
security = "starttls"
user = "kjak@example.com"
password = { env = "KJAK_SMTP_PASSWORD" }
from = "kjak@example.com"
to = ["oncall@example.com", "sre@example.com"]
subject_prefix = "[kjak]"
levels = ["error", "critical"]
```

- Certificates are verified (TLS 1.2 or later); there is no option to skip
  the check.
- `user` and `password` go together; with `security = "none"` they are
  refused (the password would travel in clear text). `none` suits a local
  relay.
- AUTH PLAIN, else AUTH LOGIN.
- `from` and `to` are bare addresses (`name@example.com`, no display name).
- The subject is the title, or `[LEVEL] first line of the body`, after
  `subject_prefix`. The body is UTF-8 quoted-printable text, followed by the
  URL.
- One refused recipient refuses the whole message: nothing is sent.

### http

Any HTTP API or webhook: the guide is [http.md](http.md). Configured in the
file (`feather add` does not create one).

| key | type | default | required | validation | security | example |
|---|---|---|---|---|---|---|
| `url` | string | — | one of `url`, `url_secret` | absolute `https` (`http` with `allow_http`), a host, no user information, no fragment | shown by `feather show` and `config trust`: not for secrets | `"https://api.example.com/v1/alerts"` |
| `url_secret` | credential | — | one of `url`, `url_secret` | as `url`, checked when resolved | never printed; path and query redacted | `{ secret = "chat.webhook_url" }` |
| `method` | string | `"POST"` | no | `GET`, `POST`, `PUT`, `PATCH`, `DELETE` (upper case) | decides the retry policy | `"PUT"` |
| `headers` | table of strings | — | no | header names (tokens), values without control characters, at most 64; not `Host`, `Content-Length`, `Transfer-Encoding`, `Connection`, `Keep-Alive`, `Upgrade`, `TE`, `Trailer`, `Proxy-*`, `Authorization`, `Cookie`, `Content-Type` | plain values: no secret (use `[auth]`) | `{ "X-Team" = "ops" }` |
| `query` | table of strings | — | no | non-empty keys | plain values: kjak never puts a secret in a query | `{ source = "kjak" }` |
| `auth.type` | string | `"none"` | with `[auth]` | `none`, `bearer`, `basic`, `header`; only the keys of the type | — | `"bearer"` |
| `auth.token` | credential | — | `bearer` | a credential reference | sent as `Authorization: Bearer ...`; redacted | `{ secret = "api.token" }` |
| `auth.username` | string | — | `basic` | no `:` or line break | shown | `"kjak"` |
| `auth.password` | credential | — | `basic` | a credential reference | the encoded header is redacted too | `{ env = "API_PASSWORD" }` |
| `auth.header` | string | — | `header` | a header name, not a reserved one (`Authorization` and `Cookie` allowed here), not also in `headers` | — | `"X-Api-Key"` |
| `auth.value` | credential | — | `header` | a credential without line breaks | redacted | `{ keychain = { service = "kjak", account = "api" } }` |
| `body.format` | string | `json` (`POST`, `PUT`, `PATCH`), `none` (`GET`, `DELETE`) | no | `json`, `text`, `none`; a `GET` has no body | — | `"text"` |
| `body.template` | string | — | no | `text/template`; for `json` a JSON document whose strings are templates; at most 64 KiB | a message cannot change a JSON template's structure; no secret is available to it | `'{"text": "{{.title}}: {{.body}}"}'` |
| `body.content_type` | string | `application/json` / `text/plain; charset=utf-8` | no | a media type | — | `"text/markdown"` |
| `timeout` | duration | `"10s"` | no | 0 < t <= 10m | — | `"5s"` |
| `success_status` | list of strings | `["2xx"]` | no | `"NNN"` (100-599) or `"Nxx"` | — | `["200", "204"]` |
| `max_response_bytes` | integer | 65536 | no | 1 - 10485760 | bounds the memory a response takes | `4096` |
| `allow_http` | bool | `false` | no | — | `http://` sends everything in clear text | `true` |
| `allow_networks` | list of strings | — | no | CIDR prefixes or addresses | allows loopback, link-local, private and IPv6-transition addresses inside them; never metadata, unspecified, multicast or reserved ones | `["10.1.0.0/16"]` |
| `allow_hosts` | list of strings | — | no | host names | allows the private addresses of these names (not loopback or link-local) | `["alerts.corp.example"]` |
| `max_redirects` | integer | 0 | no | 0 - 10 | each redirect is checked like the URL; credentials are dropped when the origin changes | `2` |
| `idempotency_key` | bool | `false` | no | — | lets `POST` and `PATCH` be retried under policy `safe` | `true` |
| `idempotency_header` | string | `"Idempotency-Key"` | no | with `idempotency_key`; a header name, not reserved | — | `"X-Request-Id"` |
| `retry.policy` | string | `"safe"` | no | `safe`, `never`, `always` | `always` may deliver a `POST` twice | `"never"` |
| `retry.max_attempts` | integer | 0 (no cap) | no | 0 - 11 | — | `3` |

```toml
[feathers.ops_api]
type = "http"
url = "https://api.example.com/v1/alerts"
headers = { "X-Team" = "ops" }

[feathers.ops_api.auth]
type = "bearer"
token = { secret = "ops_api.token" }

[feathers.ops_api.body]
template = '{"summary": "[{{.level}}] {{.title}}", "details": "{{.body}}"}'
```

Proxies from the environment are not used by http feathers, and every
connection is checked by the address policy: [http.md](http.md#the-address-policy-ssrf).

## 5. Groups

A group is a named list of feathers, so that one `--to` reaches several
destinations.

```sh
kjak group set development dev_slack dev_discord      # create or replace
kjak group set incident dev_slack dev_discord oncall_mail
kjak group show development
kjak group remove development
kjak send --to development "deploy started"
```

```toml
[groups.development]
feathers = ["dev_slack", "dev_discord"]
```

- At least one member, each a feather (groups do not nest), each once.
- `group set` replaces the whole member list.
- Several targets at once: `--to development,oncall_mail` or `--to A --to B`.
  A feather named twice is sent to once.
- A member that is **disabled** or whose `levels` exclude the message's level
  is **skipped**: shown as `skip`, not counted as a failure.
- Every member is delivered independently; one failure does not stop the
  others.

Partial failure:

```
ok    dev_slack (slack) delivered in 280ms (1 attempt)
FAIL  dev_discord (discord) failed (1 attempt): HTTP 404: Unknown Webhook
skip  oncall_mail (email): level_filter (group development)
partial: 1 delivered, 1 failed, 1 skipped
```

The outcome is `partial` and the exit status `1`.

## 6. Templates

```sh
kjak template set deploy --title 'Deployed {{.service}}' \
    --body '{{.service}} {{.version}} is live' --level success
kjak template render deploy --var service=api --var version=1.4.2
kjak send --to development --template deploy --var service=api --var version=1.4.2
```

```toml
[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"              # may be a template too: "{{.level}}"
url = "https://ci.example.com/runs/{{.run}}"
```

`body` is required. Variables are given with `--var KEY=VALUE`; a missing one
is an error. Only `text/template`'s built-in actions are available. A
template file (`--template-file FILE`) holds the same keys at the top level.

## 7. Credentials and security

### 7.1 Credential references

A feather's secret — a Slack or Discord webhook URL (the URL is the
credential) or an SMTP password — is a reference with exactly one source,
resolved when a send runs:

| source | TOML | CLI | resolved from |
|---|---|---|---|
| `env` | `{ env = "KJAK_DEV_SLACK_WEBHOOK" }` | `--secret-env VAR` | the environment variable (set and non-empty) |
| `keychain` | `{ keychain = { service = "kjak", account = "dev_slack" } }` | `--secret-keychain SERVICE/ACCOUNT` | the OS keychain (macOS, Linux) |
| `secret` | `{ secret = "dev_slack.webhook_url" }` | `--secret-ref NAME` | an entry of a secrets file ([7.7](#77-secrets-files)) |
| `literal` | `{ literal = "https://hooks.slack.com/..." }` | `--secret-stdin` | the file itself (**deprecated**, [7.8](#78-literal-secrets-deprecated)) |

- Two sources, or none, is an error. A plain string (`webhook_url =
  "https://..."`) is an error too.
- A webhook URL must be `https` (`http` only for `localhost`), without user
  information.
- A credential that does not resolve stops the send before anything is sent
  (exit 2).
- `feather add` never takes the secret as an argument: by reference, or on
  standard input with `--secret-stdin`.

**Which source to use**

| situation | use |
|---|---|
| your own macOS or Linux desktop | `keychain`, or `secret` with a private secrets file |
| CI, servers, containers, cron, Windows | `env`, filled from the platform's secret store; or `secret` with `--secrets FILE` |
| a quick local test | `secret`, in your own secrets file |

kjak only reads the keychain. Create the entry with the OS tool, which
prompts for the secret so it stays out of your shell history:

```sh
security add-generic-password -s kjak -a dev_slack -w                  # macOS
secret-tool store --label "kjak dev_slack" service kjak account dev_slack   # Linux (libsecret-tools)
```

On Linux a Secret Service (a desktop session with GNOME Keyring or KWallet)
must run; headless machines should use `env`. Windows has no keychain support
in kjak.

### 7.2 What kjak shows

Secrets are never printed. `feather show`, `feather list`, `config show` and
`config trust` print the source instead:

```
webhook_url  env:KJAK_DEV_SLACK_WEBHOOK
webhook_url  keychain:kjak/dev_slack
webhook_url  secret:dev_slack.webhook_url
webhook_url  literal:[REDACTED]
```

Errors and reports remove every resolved secret (and a webhook URL's path and
query) before printing, including text a provider echoes back.

### 7.3 Files and permissions

kjak creates its files with mode `0600` (owner only) and keeps an existing
file's mode minus write permission for group and others. A `literal` secret
sits in the file in clear text, so prefer `env` or `keychain`, and keep files
with literals out of backups you share and out of repositories.

### 7.4 Project configuration trust

A `.kjak.toml` in a repository can name environment variables and keychain
entries and the URLs they are sent to. A repository you clone could therefore
make `kjak send` send your secrets elsewhere. kjak uses a project file only
after you trust that exact file:

```sh
kjak config trust
```

```
project configuration /home/me/src/app/.kjak.toml
sha256 3f1c...e9
  feather app_slack: slack, webhook URL from env:APP_SLACK_WEBHOOK
  group app: app_slack, dev_slack
Trust this exact content? [y/N]
```

- The trust is the file's canonical path and the SHA-256 of its content,
  stored in the trust store. **Any change** of the content, or another path,
  needs a new trust.
- A `./.kjak.secrets.toml` next to it is trusted with it (its path and
  SHA-256; its values are never shown). A secrets file that appears, changes
  or goes after the trust withdraws it.
- The `secret` references of the project file that the project secrets file
  does not define are read from **your** secrets file only when you allow
  each name: `config trust` lists them and asks a second question (with
  `--yes`, `--allow-user-secrets` is required). Any other name is refused
  when a send resolves it, so a trusted repository cannot reach the rest of
  your secrets.
- An untrusted project file is an error for every command that would use it
  (exit 2), never used and never ignored silently:

  ```
  kjak: the project configuration is not trusted: /home/me/src/app/.kjak.toml
    review it, then run `kjak config trust` (or pass --no-project to ignore it)
  ```

- In an untrusted repository, read `.kjak.toml` first, or run kjak with
  `--no-project`.
- Without a terminal, `config trust` requires `--yes` (and
  `--allow-user-secrets` for the names above): review the files first. It
  never waits for an answer there.
- `kjak config untrust` withdraws the trust; `kjak config path` shows the
  state (`trusted`, `untrusted`, `untrusted (changed since it was trusted)`,
  or untrusted because of its secrets file).

### 7.5 Keeping secrets out of Git

- Commit a `.kjak.toml` with `env` or `keychain` references only, never a
  `literal`. Each user then provides the secret in their own environment.
- Webhook URLs leak easily: check `git diff` before committing, and revoke
  (delete and recreate) a webhook whose URL was committed or shared.
- Secrets belong in a secrets file, never in the repository: keep
  `.kjak.secrets.toml` in `.gitignore`.

### 7.6 In CI

- Give secrets as environment variables from the CI's secret store
  (`env` references); the keychain is not available there.
- Trusting `.kjak.toml` in CI with `kjak config trust --yes` trusts whatever
  the checked-out commit contains, including a change in a pull request. Use
  `--no-project` with a configuration the pipeline controls (`--config FILE`
  or `KJAK_CONFIG`), or trust only in pipelines that run reviewed code.
- Use `-o json` to keep a machine-readable record; it contains no secret.
- A secrets file written by the pipeline (mode 0600, owned by the job's
  user) can be given with `--secrets FILE` or `KJAK_SECRETS`. Container
  secret mounts (Kubernetes, Docker) are usually readable by others or owned
  by root and are then refused: use `env` there.

### 7.7 Secrets files

A secrets file is TOML of string values in tables; a `secret` reference
names one by its key path:

```toml
# ~/.config/kjak/secrets.toml   (mode 0600)
[dev_slack]
webhook_url = "https://hooks.slack.com/services/..."

[oncall_mail]
password = "..."
```

```toml
# ~/.config/kjak/config.toml
[feathers.dev_slack]
type = "slack"
webhook_url = { secret = "dev_slack.webhook_url" }
```

| | |
|---|---|
| names | dot-separated segments, each a letter or digit then letters, digits, `_`, `-` (at most 64); at most 8 segments and 255 bytes. A key that contains a dot is an error |
| values | strings only; an empty value is an error when it is used. Values are used as they are: nothing in them is expanded (`${...}`, `{ env = ... }` and `{{...}}` stay text) |
| size | at most 1 MiB; unknown or non-string entries are errors that name the key, never the value |
| permissions (Unix) | a regular file of the user running kjak, with no permission for group or others (`chmod 600`); otherwise refused before it is parsed. The check is made on the opened file (`fstat`), not on the path |
| symbolic links | the personal file may be one (a dotfiles link): the file it leads to is checked. The project file must not be one (a repository could link it to your personal file): it is opened with `O_NOFOLLOW` |
| FIFOs, devices | refused without waiting (`--secrets <(...)` does not work: use `env`) |
| Windows | the file must be a regular file; its access control list is **not checked** (kjak does not read ACLs yet). A file under `%AppData%` is private by the profile's default ACL; keep it there. A project secrets file that is a reparse point is refused (checked on the path, before the open) |

**Which file a reference reads.** Two files are read, each on its own:

| reference in | reads |
|---|---|
| the global file (`--config`, `$KJAK_CONFIG` or the user file) | the personal secrets file only (`--secrets`, else `$KJAK_SECRETS`, else the user file). The project secrets file is never read for it: a repository cannot redirect your feathers |
| the project file | the project secrets file; else the personal secrets file, **only for the names its trust allows** ([7.4](#74-project-configuration-trust)); any other name is an error |

The personal file is read once per run, when a send first needs a secret
(`kjak config validate` checks it too). A missing secret, a missing file or a
refused file stops the send before anything is sent (exit 2, `error.code:
credential`).

### 7.8 Literal secrets (deprecated)

`{ literal = "..." }` keeps a secret in the configuration file, which is
easily shared, committed or backed up. It still works; `kjak send` warns about
it on stderr, and `kjak config validate --strict` refuses it.
`kjak config migrate-secrets` moves the literals of a file to a new secrets
file without changing the file: see [migration.md](migration.md).

More: [security.md](security.md).

## 8. Delivery behavior

### Deadline and timeout

`timeout` (`--timeout`, default 10s, at most 10m) is the deadline of the
**whole send**, from the moment its message is read: resolving the
credentials (a keychain lookup, which may wait for a confirmation dialog),
every attempt and every wait between attempts, of every feather, end by
then. A deadline that passes while the credentials are resolved ends the
send with exit 2 (`error.code: deadline`), nothing sent; raise `--timeout`
if a keychain dialog needs longer. A feather still waiting for a free slot
when it passes fails without an attempt. Reading the message (standard
input, a FIFO) is not under the deadline: it waits for its writer, and
SIGINT or SIGTERM ends that wait.

### Retries

- At most `max_retries` (default 2) retries after the first attempt, only of
  failures known to be transient: HTTP 429, 408 and 5xx other than 502/504,
  network errors before the request was fully sent, SMTP 4xx.
- Waits: `Retry-After` when the provider gives one, else 0.5s, 1s, 2s, …
  (at most 5s, with jitter). A wait that does not fit the deadline is not
  started.
- Never retried: refusals such as HTTP 400/401/403/404, SMTP 5xx, a wrong
  SMTP password, a failed certificate check.

### Unknown acceptance

When the whole request reached the provider but no answer came back (the
connection broke, the deadline passed, HTTP 502/504), the message **may have
been delivered**. kjak does not retry it, to avoid a duplicate, and reports it
as failed with unknown acceptance:

```
????  dev_slack (slack) failed (1 attempt): ... [acceptance unknown: it may have been delivered; not retried]
```

Check the destination before sending again.

### Cancellation

Ctrl-C (SIGINT) or SIGTERM cancels the send: attempts in progress end as soon as
possible, queued feathers are not attempted, the report is printed with the
outcome `canceled`, and kjak exits 130. A second signal ends kjak at once.

### Results and exit status

| outcome | when | exit |
|---|---|---|
| `success` | nothing failed (skipped targets allowed) | 0 |
| `partial` | some delivered, some failed | 1 |
| `failure` | none delivered, at least one failed | 1 |
| `canceled` | SIGINT or SIGTERM | 130 |
| (error) | input, configuration, trust, routing or credential error before sending, or the deadline passing while the credentials were resolved: nothing was sent | 2 |

The deliveries and the report are two facts. When the report of `kjak send`
cannot be written (a full disk, a reader that closed the pipe), kjak says
`cannot write the result` on stderr and the exit status is still the
deliveries'; without that message, the JSON on stdout is whole. A delivery
whose acceptance is `unknown` may have arrived: check before sending again.
The output of the other commands is their result: when it cannot be
written, they fail with exit 2; a reader that closed the pipe (`| head`) is
not reported on stderr, on Unix as on Windows.

Text output marks each target: `ok` delivered, `FAIL` failed, `????` failed
with unknown acceptance, `skip` skipped, `plan` (dry run). `-q` prints
nothing when everything was delivered.

### JSON output

`-o json` prints one document with `schema_version: 1`: `outcome`,
`summary` (`total`, `delivered`, `failed`, `unknown`, `skipped`) and one entry
per target in `results` (`status`, `acceptance`, `attempts`, `error`,
`history` of every attempt). A command that fails before sending prints the
same shape with `outcome: "error"` and `error: {code, message}` on stdout,
and the diagnostic on stderr. Field by field: [cli.md](cli.md#json-output--o-json).

### Reading errors

- `kjak: ...` on stderr with exit 2: nothing was sent; the message names the
  file, key, feather or variable.
- A `FAIL` line: the provider refused (`HTTP 404: ...`, `SMTP 535 ...`) or the
  network failed; `-o json` shows every attempt.
- [troubleshooting.md](troubleshooting.md) lists the messages and their fixes.

### Differences between operating systems

| | macOS | Linux | Windows |
|---|---|---|---|
| global file without `XDG_CONFIG_HOME` | `~/.config/kjak/config.toml` | `~/.config/kjak/config.toml` | `%AppData%\kjak\config.toml` |
| `keychain` credentials | yes (`security`) | yes (`secret-tool`, needs a Secret Service) | no: use `env` |
| configuration lock | flock | flock | LockFileEx |

Details of delivery: [delivery.md](delivery.md).

## 9. A complete example

```toml
[defaults]
timeout = "15s"
targets = ["development"]

[feathers.dev_slack]
type = "slack"
webhook_url = { env = "KJAK_DEV_SLACK_WEBHOOK" }

[feathers.dev_discord]
type = "discord"
webhook_url = { env = "KJAK_DEV_DISCORD_WEBHOOK" }
levels = ["error", "critical"]

[feathers.oncall_mail]
type = "email"
host = "smtp.example.com"
user = "kjak@example.com"
password = { keychain = { service = "kjak", account = "oncall_mail" } }
from = "kjak@example.com"
to = ["oncall@example.com"]
levels = ["critical"]

[groups.development]
feathers = ["dev_slack", "dev_discord"]

[groups.incident]
feathers = ["dev_slack", "dev_discord", "oncall_mail"]

[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"
```

## 10. Changes made by kjak

`feather add`, `group set`, `template set` and the other changing commands
parse the file, change it, validate the result (alone and merged with the
other file) and write it back:

- atomically: a temporary file in the same directory, synced, renamed over
  the file;
- through symbolic links: a file that is a link (a dotfiles repository) stays
  a link and its target is written; a link to nothing is refused;
- under a lock: an exclusive OS lock on `<file>.lock`; a second kjak fails
  instead of waiting (`the configuration is being changed by another kjak
  process`). The OS releases the lock when its process ends, so a crashed
  kjak blocks nobody; the `.lock` file itself stays and is harmless. `kjak
  send` only reads and takes no lock;
- checked: if the file changed while kjak wrote it (an editor that ignores
  the lock), kjak stops and leaves that change alone.

kjak writes its own layout: **comments and key order are not kept**. Edit by
hand if you want to keep them.
