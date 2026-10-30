# E-mail through any SMTP server

[日本語](smtp_ja.md) · [All tutorials](README.md) · [Configuration](../configuration.md#email)

This tutorial sends kjak's messages as e-mail through an SMTP server of your
choice: your provider's submission server, your company's relay, or a local
test server. It covers the server settings, keeping the password secret, the
first message and the options. For a Gmail account, follow the
[Gmail tutorial](gmail.md) instead: it has the Google-specific steps (an app
password).

## 1. Overview

An `email` feather sends each message as one e-mail to a fixed list of
recipients, through one SMTP server:

```toml
[feathers.mail]
type = "email"
host = "smtp.example.com"
user = "kjak@example.com"
password = { secret = "mail.password" }
from = "kjak@example.com"
to = ["you@example.com"]
```

- kjak speaks SMTP itself, one SMTP session per attempt, so that it knows
  how far a failed session got: an error before the end of the message
  means the server certainly did not take it (and a temporary one is
  retried); a connection lost **after** the message was sent means it
  may have arrived (`acceptance: unknown`, never retried).
- The connection is protected by TLS unless you say otherwise:
  `security = "starttls"` (the default, port 587), `"tls"` (implicit TLS,
  port 465) or `"none"` (no TLS, and then no password). Certificates are
  always verified; there is no option to skip that.
- The subject is the message's title, or `[LEVEL] first line of the body`,
  after `subject_prefix`. The body is plain UTF-8 text, followed by the
  message's URL. The header `X-Kjak-Level` carries the level, for mail
  filters.
- One recipient refused by the server refuses the whole message: nothing is
  sent to the others either.
- **The password is a secret.** The configuration never holds it: it names
  where kjak reads it (a secrets file, an environment variable or the OS
  keychain).

## 2. Prerequisites

- kjak installed (`kjak version`): [getting started](../getting-started.md#2-install).
- An SMTP server and its settings: host name, port and TLS mode. Your
  provider or administrator documents them, usually as "SMTP submission"
  or "outgoing mail server".
- An account on it (user name and password), unless the server is a relay
  that accepts you without authentication (a local relay, a test server).
- A sender address (`from`) the server lets that account use. Many servers
  refuse any other sender, or rewrite it.
- Network access from the machine that runs kjak to the server's port.
  Port 25 is often blocked by home and cloud networks; submission uses 587
  or 465.
- For a test without a real server, any local test SMTP server works, for
  example [Mailpit](https://mailpit.axllent.org/), which listens on
  `127.0.0.1:1025` without TLS (section 7).

## 3. Setup

Find out three settings of your server. The usual combinations:

| the server offers | `port` | `security` |
|---|---|---|
| submission with STARTTLS (the common case) | 587 | `"starttls"` (the default: both may be left out) |
| submission with implicit TLS ("SSL/TLS") | 465 | `"tls"` |
| a local relay or test server without TLS | 25, 1025, ... | `"none"` (no `user` or `password`) |

`starttls` never falls back to clear text: a server that does not offer
STARTTLS is an error, not a silent downgrade.

Put the password in your **secrets file**, created private (mode 0600)
before anything is written to it:

```sh
mkdir -p ~/.config/kjak
install -m 600 /dev/null ~/.config/kjak/secrets.toml   # only if it does not exist yet
"${EDITOR:-vi}" ~/.config/kjak/secrets.toml
```

and add, with your password:

```toml
# ~/.config/kjak/secrets.toml (mode 0600; replace the dummy value)
[mail]
password = "replace-me"
```

On Windows the file is `%AppData%\kjak\secrets.toml` (private by the
profile's default permissions). Other places for the password: section 8.

## 4. Minimal configuration

The complete `~/.config/kjak/config.toml` (on Windows
`%AppData%\kjak\config.toml`) for a server with STARTTLS on port 587;
replace the host and the addresses with yours:

```toml
# ~/.config/kjak/config.toml
[feathers.mail]
type = "email"
host = "smtp.example.com"
port = 587
security = "starttls"
user = "kjak@example.com"
password = { secret = "mail.password" }
from = "kjak@example.com"
to = ["you@example.com"]
```

| key | meaning |
|---|---|
| `host` | the SMTP server (required) |
| `port` | default 587 (`starttls`), 465 (`tls`), 25 (`none`) |
| `security` | `starttls` (default), `tls` or `none` |
| `user`, `password` | the account; they go together. `password` is a credential reference |
| `from` | the sender, a bare address (`name@example.com`, no display name) |
| `to` | the recipients, bare addresses (required) |

Check it:

```sh
kjak config validate
kjak feather show mail
```

`feather show` prints `password secret:mail.password`, never the password.

## 5. CLI configuration

`kjak feather add` writes the same feather for you. The password is never a
command-line argument: you name where kjak reads it.

```sh
kjak feather add mail --type email --host smtp.example.com --port 587 --security starttls --user kjak@example.com --secret-ref mail.password --from kjak@example.com --mail-to you@example.com
```

| option | key |
|---|---|
| `--host`, `--port`, `--security` | `host`, `port`, `security` |
| `--user` | `user` |
| `--secret-ref NAME` / `--secret-env VAR` / `--secret-keychain SERVICE/ACCOUNT` | `password` (a secrets file entry, an environment variable, the keychain) |
| `--from` | `from` |
| `--mail-to ADDRESS` (repeatable, or comma-separated) | `to` |
| `--subject-prefix TEXT` | `subject_prefix` |
| `--levels L[,L]` | `levels` |
| `--disabled` | `enabled = false` |

It adds this to the global file:

```toml
[feathers.mail]
type = 'email'
host = 'smtp.example.com'
port = 587
security = 'starttls'
user = 'kjak@example.com'
password = {secret = 'mail.password'}
from = 'kjak@example.com'
to = ['you@example.com']
```

There is no command that changes one key: run `feather add` again with
every option and `--force` (it replaces the whole feather), or edit the
file and run `kjak config validate`. Other commands:

```sh
kjak feather list
kjak feather disable mail
kjak feather enable mail
kjak feather remove mail
```

## 6. First notification

Try the routing and the password first; nothing is sent:

```sh
kjak send --to mail --dry-run "test"
```

```
plan  mail (email)
dry run: 1 target(s), 0 skipped; nothing was sent
```

Then send:

```sh
kjak send --to mail --title "Deploy" --level success "api 1.4.2 is live"
```

```
ok    mail (email) delivered in 812ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

The mail arrives with the subject `Deploy`; without `--title`, the subject
is `[SUCCESS] api 1.4.2 is live`. `delivered` means the server accepted
the message (a 2xx reply to it); where it goes from there (spam
filters, forwarding) is the mail system's business. If nothing arrives,
look in the spam folder first.

The exit status is 0 when it was delivered, 1 when a delivery failed, 2
when nothing was sent (a configuration or credential error). For scripts,
the JSON report has every attempt:

```sh
kjak send --to mail -o json "test"
```

Its `results[0].status` is `delivered` and `acceptance` `confirmed`; on a
failure, `error.code` and `error.message` say why, and `history` lists each
attempt.

## 7. Advanced configuration

A complete configuration with the options most setups use: an implicit-TLS
server for alerts only, a local relay for everything, both in a group:

```toml
# ~/.config/kjak/config.toml
[defaults]
timeout = "30s"       # a whole send, retries included (SMTP servers can be slow)
max_retries = 2       # temporary failures (4xx replies, network errors)
targets = ["mail"]    # kjak send without --to

[feathers.oncall_mail]
type = "email"
host = "smtp.example.com"
port = 465
security = "tls"
user = "alerts@example.com"
password = { env = "KJAK_SMTP_PASSWORD" }
from = "alerts@example.com"
to = ["oncall@example.com", "sre@example.com"]
subject_prefix = "[prod]"
levels = ["error", "critical"]

[feathers.relay_mail]
type = "email"
host = "127.0.0.1"
port = 25
security = "none"
from = "kjak@build-01.example.com"
to = ["builds@example.com"]
subject_prefix = "[kjak]"

[groups.mail]
feathers = ["oncall_mail", "relay_mail"]
```

- **Implicit TLS** (`security = "tls"`, port 465): TLS from the first byte.
  Use it when the server documents "SSL/TLS" on 465, or when STARTTLS on 587
  is not offered.
- **Levels**: `levels` limits a feather to some levels. In a group, a
  feather that does not take the message's level is skipped; named directly
  with `--to`, it is an error (`does not accept level info`).
- **Several recipients** get one e-mail with all of them in `To:`.
- **`security = "none"`** is for a relay on this machine or a trusted local
  network: no TLS, so `user` and `password` are refused (the password would
  travel in clear text).
- **Timeouts and retries** are set in `[defaults]` (or `--timeout`,
  `--max-retries`); an email feather has no `timeout` or `retry` of its own.
  A wrong password (`535`) is never retried: retrying could lock the
  account.

A **local test server** — Mailpit, or any SMTP server listening on this
machine — receives everything without delivering it anywhere:

```toml
# ~/.config/kjak/config.toml
[feathers.test_mail]
type = "email"
host = "127.0.0.1"
port = 1025
security = "none"
from = "kjak@example.com"
to = ["you@example.com"]
```

```sh
kjak send --to test_mail --title "hello" "first test"
```

**In a project file** (`.kjak.toml`), an email feather may connect only to
public addresses: a server on this machine or the private network must be
allowed with `allow_networks` (CIDR) or `allow_hosts` (host names), and
`kjak config trust` shows it. Feathers of your global file are not
restricted. See
[the address policy](../configuration.md#address-policy-of-a-projects-slack-discord-and-email-feathers).

```toml
# .kjak.toml (a project file: used once trusted)
[feathers.team_mail]
type = "email"
host = "smtp.corp.example"
user = "ci@corp.example"
password = { env = "TEAM_SMTP_PASSWORD" }
from = "ci@corp.example"
to = ["team@corp.example"]
allow_hosts = ["smtp.corp.example"]
```

## 8. Secrets

`password` is a reference with exactly one source:

| source | TOML | CLI | for |
|---|---|---|---|
| secrets file | `{ secret = "mail.password" }` | `--secret-ref mail.password` | your own machine; the file is private (mode 0600) |
| environment | `{ env = "KJAK_SMTP_PASSWORD" }` | `--secret-env KJAK_SMTP_PASSWORD` | CI, servers, containers, cron, Windows |
| OS keychain | `{ keychain = { service = "kjak", account = "mail" } }` | `--secret-keychain kjak/mail` | your macOS or Linux desktop |

The secrets file of section 3 holds the value of `mail.password`:

```toml
# ~/.config/kjak/secrets.toml (mode 0600; dummy values)
[mail]
password = "replace-me"

[oncall_mail]
password = "another-dummy"
```

kjak only reads the keychain. Create the entry with the OS tool, which
prompts for the password so it stays out of your shell history:

```sh
security add-generic-password -s kjak -a mail -w                     # macOS
secret-tool store --label "kjak mail" service kjak account mail      # Linux (libsecret-tools)
```

For `env`, set the variable from your CI's secret store, or read it without
echo in an interactive bash or zsh (`read -rs KJAK_SMTP_PASSWORD; export
KJAK_SMTP_PASSWORD`); never type `KJAK_SMTP_PASSWORD=...` on a command line
that your history keeps.

- `kjak config validate --strict` refuses a password written in the file
  (`{ literal = "..." }`, deprecated); `kjak config migrate-secrets` moves
  such literals to a secrets file.
- A password that cannot be resolved stops the send before anything is
  sent (exit 2).
- The password is redacted from every error and report; the text of an
  AUTH refusal is never shown (a server may repeat the encoded password).

## 9. Troubleshooting

| message | cause | fix |
|---|---|---|
| `the server does not offer STARTTLS (set security = "tls" for implicit TLS)` | the port speaks implicit TLS, or the server has no STARTTLS | `security = "tls"` with `port = 465`, if the server supports it |
| `TLS handshake: connection error` | `security = "tls"` on a port that expects STARTTLS or clear text (587, 25) | `security = "starttls"` with port 587 |
| `delivery deadline exceeded during greeting` | `security = "starttls"` (or `none`) on an implicit-TLS port (465): both sides wait until the deadline | `security = "tls"` |
| `TLS certificate verification failed` | the certificate is not trusted, or its name does not match `host` | use the host name of the certificate; for a private CA see [certificate authorities](../security.md#certificate-authorities-ssl_cert_file-ssl_cert_dir) |
| `AUTH: SMTP 535 authentication failed` | wrong user or password, or the provider wants an app password | check both; not retried |
| `AUTH: the server offers neither PLAIN nor LOGIN` | the server offers only other mechanisms (OAuth 2.0, CRAM-MD5, ...) | kjak supports AUTH PLAIN and LOGIN: use an account or a relay that offers one of them |
| `connect: dial: connect: connection refused`, or a timeout at `connect` | wrong host or port, a firewall, port 25 blocked | check the host and port from that machine |
| `MAIL FROM: SMTP 5xx ...` / `RCPT TO <address>: SMTP 5xx ...` | the server refuses the sender or a recipient | use a `from` the account may send as; check the addresses |
| `user and password go together` | only one of them is set | set both, or neither (a relay) |
| `security = "none" sends the password in clear text; ...` | `none` with a `user` | use `starttls` or `tls`, or drop the account for a relay |
| `from: "Kjak <kjak@example.com>" is not a bare address` | a display name | `from = "kjak@example.com"` |
| `routing error: feather "mail" does not accept level info` | `levels` excludes the message's level | `--level error`, or send to a group |
| `environment variable KJAK_SMTP_PASSWORD is not set` | an `env` password | export it in kjak's environment |
| `the secrets file is not private: ...` | the secrets file can be read by others | `chmod 600` the file |
| `????` / `the reply to the message was lost (...); the server may have accepted it` | the connection ended after the whole message was sent | check the mailbox before sending again; kjak does not retry it |
| `the request was not sent: address 127.0.0.1 is loopback: allow it with allow_networks` | an email feather of `.kjak.toml` to a local server | `allow_networks = ["127.0.0.1/32"]` in that feather, then `kjak config trust` |

More: [troubleshooting](../troubleshooting.md) and the
[SMTP acceptance rules](../delivery.md#e-mail-smtp).

```sh
kjak config path
kjak config validate
kjak send --to mail --dry-run "test"
kjak send --to mail -o json "test"
```

## 10. Security notes

- **TLS**: `starttls` and `tls` verify the server's certificate (TLS 1.2 or
  later) and never fall back to clear text. Clear-text data sent with the
  reply to STARTTLS (an injection) ends the session.
- **`security = "none"`** sends the message in clear text and never a
  password: keep it for a relay on this machine or a network you trust.
- **The password** is never stored by kjak in the configuration, never
  printed, never a command-line argument, and redacted from errors.
- **Message content**: a title cannot contain a line break (it becomes the
  `Subject:` header), so a message cannot add headers.
- **Project files** choose the SMTP host like a URL: their email feathers
  reach public addresses only unless `allow_networks` / `allow_hosts` say
  otherwise, and their `env` and `keychain` references must be allowed by
  name when you trust the project.
- **`SSL_CERT_FILE` / `SSL_CERT_DIR`** in kjak's environment decide which
  certificate authorities it trusts ([security](../security.md#certificate-authorities-ssl_cert_file-ssl_cert_dir)).

## 11. Related references

- [Configuration: email](../configuration.md#email) — every key
- [Gmail tutorial](gmail.md) — the same feather with Gmail's settings
- [Delivery: e-mail (SMTP)](../delivery.md#e-mail-smtp) — acceptance and retries by SMTP stage
- [Security: transport security](../security.md#transport-security)
- [Secrets files](../configuration.md#77-secrets-files) and [credential references](../configuration.md#71-credential-references)
- [CLI: kjak feather](../cli.md#kjak-feather)
- [Example: smtp-only.toml](../examples/smtp-only.toml)
- [Troubleshooting](../troubleshooting.md)
