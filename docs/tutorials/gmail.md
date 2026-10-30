# Gmail

[日本語](gmail_ja.md) · [All tutorials](README.md) · [Configuration](../configuration.md#email) · [Any SMTP server](smtp.md)

This tutorial sends kjak's messages as e-mail through Gmail's SMTP server,
with an `email` feather and a Google **app password**: who can use it,
creating the app password, keeping it secret, the first message and the
options.

## 1. Overview

An `email` feather speaks SMTP to one server and sends each message to a
fixed list of recipients. For Gmail:

```toml
[feathers.gmail]
type = "email"
host = "smtp.gmail.com"
port = 587
security = "starttls"
user = "you@gmail.com"
password = { secret = "gmail.password" }
from = "you@gmail.com"
to = ["you@gmail.com"]
```

- The password is an **app password**: a 16-character passcode that Google
  creates for one app. **Never use your Google account password** with kjak,
  and never write it in a file.
- kjak authenticates with user name and password (SMTP AUTH PLAIN or LOGIN)
  over TLS. **kjak does not implement OAuth.**
- The subject is the message's title (else `[LEVEL] first line of the
  body`), after `subject_prefix`; the body is plain text (UTF-8) followed by
  the message's URL. The header `X-Kjak-Level` carries the level.
- `acceptance: confirmed` means Gmail's server accepted the message (a 2xx
  reply after the data), not that it reached an inbox.

### Who can use this tutorial

App passwords are not available to every Google account. Check this
before you start:

| account | app password |
|---|---|
| a personal Google account (`@gmail.com`) with 2-Step Verification | available — this tutorial |
| 2-Step Verification not turned on | not available until you turn it on |
| 2-Step Verification with **security keys only** | the app password option does not appear |
| a work or school account, or another account of an organization | the option does not appear |
| **Advanced Protection** turned on | the option does not appear |
| **Google Workspace** | since 2025-05-01 Workspace accounts no longer sign in with a user name and their password, and kjak does not support OAuth. An app password may still work if the administrator allows it; this tutorial does not cover or test that |

If your account is not in the first row, use another SMTP server that
accepts a password over TLS: see the [SMTP tutorial](smtp.md). Google may
change these rules; its help page on app passwords is the reference
(section 11).

## 2. Prerequisites

- kjak installed (`kjak version`): [getting started](../getting-started.md#2-install).
- A personal Google account with **2-Step Verification** turned on, not
  limited to security keys and without Advanced Protection (section 1).
- Network access from the machine that runs kjak to `smtp.gmail.com` on
  port 587 (or 465). Some networks block outgoing SMTP ports.

## 3. Setup

### 3.1 Create an app password

1. Sign in to the Google account that will send the messages.
2. If 2-Step Verification is off, turn it on in your Google Account's
   **Security** page.
3. Open <https://myaccount.google.com/apppasswords>.
4. Enter a name for the app, such as `kjak`, and create it.
5. Google shows the 16-character app password once. Copy it into the
   secrets file right away (3.2); you cannot see it again later (you can
   create a new one).

If the page says the setting is not available, your account is one of the
cases of section 1.

### 3.2 Put it in the secrets file

Create the secrets file private (mode 0600) before anything is written to
it, then open it in an editor:

```sh
mkdir -p ~/.config/kjak
[ -e ~/.config/kjak/secrets.toml ] || install -m 600 /dev/null ~/.config/kjak/secrets.toml
"${EDITOR:-vi}" ~/.config/kjak/secrets.toml
```

and add the app password. Google shows it in four groups of four
characters; write the 16 characters without the spaces:

```toml
# ~/.config/kjak/secrets.toml (mode 0600; replace the dummy value)
[gmail]
password = "abcdefghijklmnop"
```

On Windows the file is `%AppData%\kjak\secrets.toml` (private by the
profile's default permissions). Other places for the password: section 8.

## 4. Minimal configuration

The configuration file is `~/.config/kjak/config.toml` (on Windows
`%AppData%\kjak\config.toml`; `kjak config path` shows it). Copy this whole
file and replace `you@gmail.com` with your address (three times):

```toml
# ~/.config/kjak/config.toml
[feathers.gmail]
type = "email"
host = "smtp.gmail.com"
port = 587
security = "starttls"
user = "you@gmail.com"
password = { secret = "gmail.password" }
from = "you@gmail.com"
to = ["you@gmail.com"]
subject_prefix = "[kjak]"
```

| key | value for Gmail |
|---|---|
| `host` | `smtp.gmail.com` |
| `port`, `security` | `587` with `"starttls"` (this tutorial), or `465` with `"tls"` (implicit TLS) |
| `user` | your full Gmail address |
| `password` | a reference to the app password |
| `from` | your Gmail address (a bare address; no display name) |
| `to` | the recipients: one or more bare addresses |

With `starttls`, kjak requires the server to offer STARTTLS and never falls
back to clear text. Check both files:

```sh
kjak config validate
kjak feather show gmail
```

`feather show` prints `password  secret:gmail.password`, never the password.

## 5. CLI configuration

The same feather, written by kjak:

```sh
kjak config init
kjak feather add gmail --type email --host smtp.gmail.com --port 587 --security starttls --user you@gmail.com --secret-ref gmail.password --from you@gmail.com --mail-to you@gmail.com --subject-prefix "[kjak]"
```

Several recipients: `--mail-to a@example.com,b@example.com`, or the option
several times. Implicit TLS on port 465:

```sh
kjak feather add gmail --type email --host smtp.gmail.com --port 465 --security tls --user you@gmail.com --secret-ref gmail.password --from you@gmail.com --mail-to you@gmail.com --force
```

The password is always given by reference, never as an argument:
`--secret-ref NAME` (the secrets file), `--secret-env VAR`, or
`--secret-keychain SERVICE/ACCOUNT` (macOS, Linux). `--user` and a password
reference go together.

## 6. First notification

A dry run resolves the target and reads the app password from the secrets
file, and sends nothing (it does not connect to Gmail):

```sh
kjak send --to gmail --dry-run "Hello from kjak"
```

```
plan  gmail (email)
dry run: 1 target(s), 0 skipped; nothing was sent
```

Then the e-mail:

```sh
kjak send --to gmail --title "Hello" "Hello from kjak"
kjak send --to gmail --level error --title "Backup failed" --url https://example.com/runs/42 "The nightly backup stopped"
```

```
ok    gmail (email) delivered in 1.9s (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

The first e-mail arrives with the subject `[kjak] Hello`. Without
`--title`, the subject is `[kjak] [INFO] Hello from kjak`.

### Read the result as JSON

```sh
kjak send --to gmail -o json --title "Hello" "Hello from kjak"
kjak send --to gmail -o json "Hello from kjak" | jq '.results[0] | {status, acceptance, attempts, error}'
```

```json
{"status": "delivered", "acceptance": "confirmed", "attempts": 1, "error": null}
```

A refused app password (exit 1; kjak does not retry it, so as not to lock
the account):

```json
{
  "status": "failed",
  "acceptance": "rejected",
  "attempts": 1,
  "error": {"code": "rejected", "message": "AUTH: SMTP 535 authentication failed"}
}
```

`history` lists every attempt with its SMTP code (`status_code`). An
`acceptance: unknown` (the connection was lost after the data was sent)
means the e-mail **may** have been sent: look in the mailbox (and in
**Sent**) before sending again.

## 7. Advanced configuration

A complete configuration: failures by e-mail to two people, everything to a
Slack channel, one group for both, a longer deadline for SMTP and templates.

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["notify"]
level = "info"
timeout = "30s"
max_retries = 2

[feathers.chat]
type = "slack"
webhook_url = { env = "KJAK_SLACK_WEBHOOK" }

[feathers.gmail_alerts]
type = "email"
host = "smtp.gmail.com"
port = 465
security = "tls"
user = "you@gmail.com"
password = { secret = "gmail.password" }
from = "you@gmail.com"
to = ["you@gmail.com", "teammate@example.com"]
subject_prefix = "[kjak alert]"
levels = ["error", "critical"]

[groups.notify]
feathers = ["chat", "gmail_alerts"]

[templates.failure]
title = "{{.job}} failed on {{.host}}"
body = "{{.job}} failed on {{.host}}: {{.reason}}"
level = "error"
url = "https://example.com/jobs/{{.job}}"
```

```sh
kjak send "Nightly job done"
kjak send --template failure --var job=nightly --var host=build-1 --var reason=timeout
kjak send --to gmail_alerts --level critical --title "Disk full" "/var is at 100%"
```

The first send goes only to Slack: `gmail_alerts` does not accept
`info` and is skipped (`skip  gmail_alerts (email): level_filter (group
notify)`), which is not an error. The template sends an e-mail and a Slack
message.

### Notes

- **The deadline.** An SMTP session takes longer than a webhook (connect,
  TLS, authentication): `timeout = "30s"` gives the send room for a retry.
- **Retries.** A temporary refusal (SMTP 4xx, a network error before the
  data) is retried; a 5xx and a refused login are not.
- **From.** Use your Gmail address. Gmail may replace another `from` with
  the account's address unless it is an address you added to the account
  ("Send mail as").
- **Sorting.** A Gmail filter on the subject (`[kjak alert]`) can label or
  star the messages.
- **Google's limits.** Google limits how many messages an account sends per
  day (see Google's help on Gmail sending limits). These are Google's
  limits, not kjak's: kjak does not count, and Gmail refuses the messages
  beyond them (`rejected`). For many notifications, prefer a chat webhook
  ([Slack](slack.md), [Discord](discord.md)).

## 8. Secrets

| where | configuration | good for |
|---|---|---|
| secrets file (`~/.config/kjak/secrets.toml`, mode 0600) | `{ secret = "gmail.password" }` | your own machine, any OS (recommended) |
| environment variable | `{ env = "KJAK_GMAIL_PASSWORD" }` | CI, servers, cron (filled from their secret store) |
| OS keychain (macOS, Linux desktop) | `{ keychain = { service = "kjak", account = "gmail" } }` | your own machine |

**Environment variable.** Type the app password at a prompt, so it stays
out of your shell history (an `export KJAK_GMAIL_PASSWORD=...` typed as a
command is kept in the history):

```sh
printf 'Gmail app password: '; read -rs KJAK_GMAIL_PASSWORD; echo
export KJAK_GMAIL_PASSWORD
kjak feather add gmail --type email --host smtp.gmail.com --user you@gmail.com --secret-env KJAK_GMAIL_PASSWORD --from you@gmail.com --mail-to you@gmail.com --force
```

PowerShell:

```powershell
$env:KJAK_GMAIL_PASSWORD = Read-Host "Gmail app password"
kjak feather add gmail --type email --host smtp.gmail.com --user you@gmail.com --secret-env KJAK_GMAIL_PASSWORD --from you@gmail.com --mail-to you@gmail.com --force
```

**macOS keychain** (`-w` at the end makes `security` prompt for it):

```sh
security add-generic-password -s kjak -a gmail -w
kjak feather add gmail --type email --host smtp.gmail.com --user you@gmail.com --secret-keychain kjak/gmail --from you@gmail.com --mail-to you@gmail.com --force
```

On Linux: `secret-tool store --label "kjak gmail" service kjak account gmail`
(libsecret-tools, a desktop session). Windows has no keychain support in
kjak: use the secrets file or an environment variable.

More: [credential references](../configuration.md#71-credential-references),
[secrets files](../configuration.md#77-secrets-files).

## 9. Troubleshooting

| you see | cause | fix |
|---|---|---|
| no **App passwords** page, or a message that the setting is not available for your account | 2-Step Verification off, security keys only, an organization's account, Advanced Protection, a Google Workspace account whose administrator does not allow app passwords | section 1; use the [SMTP tutorial](smtp.md) with another server |
| `AUTH: SMTP 535 authentication failed` (`rejected`, not retried) | the user is not the full address, the app password is wrong (spaces, a typo, revoked), or it is not an app password | check `user`; create a new app password and replace it in the secrets file |
| `AUTH: SMTP 534 authentication failed` | Gmail wants an app password (for example, the account password was used) | create an app password (section 3) |
| `secret:gmail.password is not in the user secrets file ...` (exit 2) | the table or key is missing, or another file is used | `kjak config path`; add `[gmail] password = "..."` |
| `the secrets file is not private: ...` | the file is readable by others | `chmod 600 ~/.config/kjak/secrets.toml` |
| `user and password go together` | `user` without `password`, or the reverse | set both |
| `security = "none" sends the password in clear text ...` | `security = "none"` with a password | `starttls` (587) or `tls` (465) |
| `the server does not offer STARTTLS (set security = "tls" for implicit TLS)` | `starttls` on a port that does not offer it | 587 with `starttls`, 465 with `tls` |
| `connect` or `TLS handshake` network errors, timeouts | the network blocks the port, or port and security do not match (465 with `starttls`, 587 with `tls`) | try the other pair; check a firewall or proxy |
| `TLS certificate verification failed` | a TLS-inspecting proxy, or `SSL_CERT_FILE` | see [troubleshooting](../troubleshooting.md#results) |
| SMTP 5xx after the data (`rejected`), for example a sending limit | Gmail refused the message | read the reply in the error; wait for Google's limit to reset |
| `ok` but no e-mail | filtered or in **Spam**; a recipient's server delayed it | search the mailbox (also **Sent** of the sender) |

Google revokes the app passwords of an account when its password is
changed (Google's help): after a password change, create a new app
password.

```sh
kjak config validate
kjak send --to gmail --dry-run "test"
kjak send --to gmail -o json "test"
```

## 10. Security notes

- **Never your Google password.** kjak needs an app password; your account
  password must not be in a configuration file, a secrets file, an
  environment variable or a command line.
- **An app password is a password to your mailbox.** Whoever has it can use
  your account's mail. Keep it in a secrets file with mode 0600 (or a secret
  store), out of repositories, backups you share and the shell history.
  Revoke it at <https://myaccount.google.com/apppasswords> when you stop
  using kjak on that machine, or if it may have leaked; create one app
  password per machine, so you can revoke one alone.
- kjak never prints the password: `feather show`, `config show` and every
  error show its source (`secret:gmail.password`); the text of a refused
  login is left out of errors (a server may repeat the encoded password).
- **TLS only.** `starttls` never falls back to clear text, `tls` is TLS from
  the first byte, and the server's certificate is always verified. With
  `security = "none"` a password is refused.
- **Recipients see what you send.** No secrets in messages. Mail to other
  people goes from your Gmail address.
- **Project files.** A `.kjak.toml` of a repository can define an `email`
  feather that sends to any address; kjak uses it only after
  `kjak config trust`, and a project reads your secrets only for the names
  you allow. See [project trust](../configuration.md#74-project-configuration-trust).

## 11. Related references

- [Configuration: email](../configuration.md#email) — every key
- [SMTP tutorial](smtp.md) — any other SMTP server
- [Credential references](../configuration.md#71-credential-references) and [secrets files](../configuration.md#77-secrets-files)
- [CLI](../cli.md) — `kjak send`, `kjak feather add`, the JSON report
- [Delivery: e-mail](../delivery.md#e-mail-smtp) — what each SMTP stage and reply means, retries
- [Security](../security.md)
- [Troubleshooting](../troubleshooting.md)
- Example: [gmail-only.toml](../examples/gmail-only.toml)
- Google: [Sign in with app passwords](https://support.google.com/accounts/answer/185833), [2-Step Verification](https://support.google.com/accounts/answer/185839)
- Other tutorials: [Discord](discord.md), [Slack](slack.md)
