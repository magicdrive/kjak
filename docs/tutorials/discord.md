# Discord

[日本語](discord_ja.md) · [All tutorials](README.md) · [Configuration](../configuration.md#discord)

This tutorial sends kjak's messages to a Discord channel through a channel
webhook: creating the webhook, keeping its URL secret, the first message and
the options.

## 1. Overview

A `discord` feather posts each message to one Discord webhook, which belongs
to one channel of one server:

```toml
[feathers.discord]
type = "discord"
webhook_url = { secret = "discord.webhook_url" }
```

- The message is one **embed** colored by level, with the title, the body,
  the URL as the title's link, and the footer `kjak · <level>`.
- kjak executes the webhook with `?wait=true`, so Discord answers with the
  created message: `acceptance: confirmed` means Discord created it.
- **Mentions are disabled** (`allowed_mentions.parse = []`): `@everyone`,
  `@here` or a user mention in a message stays text and pings nobody.
- **The webhook URL is the credential.** Anyone who has it can post to the
  channel. kjak never takes it as a value in the configuration: the
  configuration names where to read it (a secrets file, an environment
  variable, the OS keychain).

## 2. Prerequisites

- kjak installed (`kjak version`): [getting started](../getting-started.md#2-install).
- A Discord server where you have the **Manage Webhooks** permission (the
  owner and administrators have it), and a text channel for the messages.
  For a first test, a channel only you read is a good choice.
- Network access to `https://discord.com` from the machine that runs kjak.

## 3. Setup

Create the webhook in Discord. The screens change from time to time; the
names below may differ slightly in your Discord version or language.

1. Open the server's menu → **Server Settings** → **Integrations**.
   (Or the channel's **Edit Channel** → **Integrations**.)
2. **Webhooks** → **New Webhook**.
3. Click the new webhook: give it a name (shown as the author of the
   messages), optionally an avatar, and choose the **channel**.
4. **Save Changes**, then **Copy Webhook URL**.

The URL looks like `https://discord.com/api/webhooks/<id>/<token>`. Do not
paste it into chat, issues or a file you commit. Keep it in the clipboard
for the next step only.

Put it in your **secrets file** (recommended), created private (mode 0600)
before anything is written to it:

```sh
mkdir -p ~/.config/kjak
[ -e ~/.config/kjak/secrets.toml ] || install -m 600 /dev/null ~/.config/kjak/secrets.toml
"${EDITOR:-vi}" ~/.config/kjak/secrets.toml
```

and add, with your URL:

```toml
# ~/.config/kjak/secrets.toml (mode 0600; replace the dummy value)
[discord]
webhook_url = "https://discord.com/api/webhooks/000000000000000000/replace-me"
```

On Windows the file is `%AppData%\kjak\secrets.toml`; kjak checks that only
you (and `SYSTEM`, `Administrators`) can read it, which a file under
`%AppData%` is by default. Other places for the URL: section 8.

## 4. Minimal configuration

The configuration file is `~/.config/kjak/config.toml` (on Windows
`%AppData%\kjak\config.toml`; `kjak config path` shows it). Copy this whole
file:

```toml
# ~/.config/kjak/config.toml
[feathers.discord]
type = "discord"
webhook_url = { secret = "discord.webhook_url" }
```

`{ secret = "discord.webhook_url" }` reads the key `webhook_url` of the table
`[discord]` of the secrets file. Check both files:

```sh
kjak config validate
kjak config path
kjak feather show discord
```

`feather show` prints `webhook_url  secret:discord.webhook_url`, never the
URL. A plain string (`webhook_url = "https://..."`) is a configuration error.

## 5. CLI configuration

The same feather, written by kjak:

```sh
kjak config init
kjak feather add discord --type discord --secret-ref discord.webhook_url
```

With a display name for the messages (it replaces the webhook's name; at
most 80 characters):

```sh
kjak feather add discord --type discord --secret-ref discord.webhook_url --username kjak --force
```

The secret is always given by reference, never as an argument:

| option | stored as |
|---|---|
| `--secret-ref NAME` | `{ secret = "NAME" }`, an entry of the secrets file |
| `--secret-env VAR` | `{ env = "VAR" }`, an environment variable |
| `--secret-keychain SERVICE/ACCOUNT` | `{ keychain = { service, account } }` (macOS, Linux) |

Other options: `--levels L[,L]`, `--disabled`, `--scope project`, `--force`
(replace a feather of the same name).

## 6. First notification

A dry run resolves the target and the URL and sends nothing:

```sh
kjak send --to discord --dry-run "Hello from kjak"
```

```
plan  discord (discord)
dry run: 1 target(s), 0 skipped; nothing was sent
```

Then the message:

```sh
kjak send --to discord "Hello from kjak"
kjak send --to discord --title "Deployed" --level success --url https://example.com/releases/1.4.2 "api 1.4.2 is live"
```

```
ok    discord (discord) delivered in 380ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

`ok` means Discord created the message; kjak exits 0. The embed is colored
by level (info blue, success green, warning yellow, error red, critical dark
red).

### Read the result as JSON

```sh
kjak send --to discord -o json "Hello from kjak"
kjak send --to discord -o json "Hello from kjak" | jq '.results[0] | {status, acceptance, attempts, error}'
```

```json
{"status": "delivered", "acceptance": "confirmed", "attempts": 1, "error": null}
```

A deleted webhook (exit 1):

```json
{
  "status": "failed",
  "acceptance": "rejected",
  "attempts": 1,
  "error": {"code": "rejected", "message": "HTTP 404: Unknown Webhook"}
}
```

`history` lists every attempt with its `status_code`; `acceptance:
unknown` (a 502/504 or a lost answer) means the message **may** have been
posted: look at the channel before sending again.

## 7. Advanced configuration

A complete configuration with two channels: deployments go to one, failures
to another, both under one group, with a default level and templates.

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["discord_all"]
level = "info"
timeout = "15s"
max_retries = 2

[feathers.discord_deploys]
type = "discord"
webhook_url = { secret = "discord_deploys.webhook_url" }
username = "deploy bot"
levels = ["info", "success"]

[feathers.discord_alerts]
type = "discord"
webhook_url = { secret = "discord_alerts.webhook_url" }
username = "kjak alerts"
levels = ["warning", "error", "critical"]

[groups.discord_all]
feathers = ["discord_deploys", "discord_alerts"]

[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"
url = "https://example.com/releases/{{.version}}"

[templates.failure]
title = "{{.job}} failed"
body = "{{.job}} failed: {{.reason}}"
level = "error"
```

```toml
# ~/.config/kjak/secrets.toml (mode 0600; dummy values)
[discord_deploys]
webhook_url = "https://discord.com/api/webhooks/000000000000000001/replace-me"

[discord_alerts]
webhook_url = "https://discord.com/api/webhooks/000000000000000002/replace-me"
```

```sh
kjak send --template deploy --var service=api --var version=1.4.2
kjak send --template failure --var job=nightly --var reason=timeout
kjak send --to discord_all --level warning "Disk at 85%"
```

Each send goes to the group `discord_all`; the member whose `levels` exclude
the message's level is **skipped** (`skip  discord_deploys (discord):
level_filter (group discord_all)`), and the send still succeeds.

### A thread

To post into a thread, add `?thread_id=<thread id>` to the webhook URL in the
secrets file. kjak keeps it when it adds `wait=true`:

```toml
# ~/.config/kjak/secrets.toml (mode 0600; dummy value)
[discord_alerts]
webhook_url = "https://discord.com/api/webhooks/000000000000000002/replace-me?thread_id=000000000000000003"
```

### Limits

kjak shortens the title to 256 characters and the body (the embed's
description) to 4096, ending with `…`; the body of a send is at most 16384
characters. Discord's own rate limits answer HTTP 429: kjak waits the
`Retry-After` (or the body's `retry_after`) and retries within the send's
`--timeout`.

## 8. Secrets

| where | configuration | good for |
|---|---|---|
| secrets file (`~/.config/kjak/secrets.toml`, mode 0600) | `{ secret = "discord.webhook_url" }` | your own machine, any OS (recommended) |
| environment variable | `{ env = "KJAK_DISCORD_WEBHOOK" }` | CI, servers, cron (filled from their secret store) |
| OS keychain (macOS, Linux desktop) | `{ keychain = { service = "kjak", account = "discord" } }` | your own machine |

**Environment variable.** Type the URL at a prompt, so it stays out of your
shell history (an `export KJAK_DISCORD_WEBHOOK='https://...'` typed as a
command is kept in the history):

```sh
printf 'Discord webhook URL: '; read -rs KJAK_DISCORD_WEBHOOK; echo
export KJAK_DISCORD_WEBHOOK
kjak feather add discord --type discord --secret-env KJAK_DISCORD_WEBHOOK --force
```

PowerShell (`Read-Host` keeps the value out of the history):

```powershell
$env:KJAK_DISCORD_WEBHOOK = Read-Host "Discord webhook URL"
kjak feather add discord --type discord --secret-env KJAK_DISCORD_WEBHOOK --force
```

The variable lasts for the shell session and must be set wherever kjak runs.

**macOS keychain** (`-w` at the end makes `security` prompt for the URL):

```sh
security add-generic-password -s kjak -a discord -w
kjak feather add discord --type discord --secret-keychain kjak/discord --force
```

On Linux: `secret-tool store --label "kjak discord" service kjak account discord`
(libsecret-tools, a desktop session). Windows has no keychain support in
kjak: use the secrets file or an environment variable.

`kjak config validate --strict` refuses a secret written in the
configuration as a value (`{ literal = ... }`, deprecated).
More: [credential references](../configuration.md#71-credential-references),
[secrets files](../configuration.md#77-secrets-files).

## 9. Troubleshooting

| you see | cause | fix |
|---|---|---|
| `secret:discord.webhook_url is not in the user secrets file ...` (exit 2) | the table or key is missing, or another file is used | `kjak config path` shows the file; add `[discord] webhook_url = "..."` |
| `the secrets file is not private: group or others have access (mode 0644)` | the file is readable by others | `chmod 600 ~/.config/kjak/secrets.toml` |
| `environment variable KJAK_DISCORD_WEBHOOK is not set` (exit 2) | an `env` reference in a shell without it | set it again (section 8) |
| `webhook_url: credential error: the resolved value must be an absolute https URL` / `the resolved value must be https ...` | the value is not the URL Discord gave | copy the URL again (**Copy Webhook URL**) |
| `a credential is a table naming one source: ...` | `webhook_url = "https://..."` in the configuration | `webhook_url = { secret = "discord.webhook_url" }` |
| `discord feathers take no email settings ...` | a key of another type | remove it |
| `HTTP 404: Unknown Webhook` (`rejected`) | the webhook was deleted, or the URL is incomplete | create a new webhook, update the secret |
| `HTTP 401: Invalid Webhook Token` | the token part of the URL is wrong | copy the URL again |
| `HTTP 400: ...` | Discord refused the payload (the text after `HTTP 400:` is Discord's) | check the `username` and the message |
| `HTTP 429` (`rate_limited`) | too many messages | kjak retries after Discord's wait; raise `--timeout` if it gives up |
| `????  discord (discord) failed ...: HTTP 504 from a gateway` (`unknown`) | the answer was lost; the message may exist | look at the channel before sending again |
| `TLS certificate verification failed` | a proxy or a CA file in `SSL_CERT_FILE` | see [troubleshooting](../troubleshooting.md#results) |

Diagnose with:

```sh
kjak config validate
kjak send --to discord --dry-run "test"
kjak send --to discord -o json "test"
```

## 10. Security notes

- **The URL is a password.** Whoever has it can post as the webhook. If it
  was committed, pasted or shared, delete the webhook in **Server Settings →
  Integrations → Webhooks** and create a new one, then update the secret.
- Keep it out of the configuration file, the repository and the shell
  history: a secrets file with mode 0600, an environment variable from a
  secret store, or the keychain.
- kjak never prints the URL: `feather show`, `config show` and every error
  show its source (`secret:discord.webhook_url`), and the path and query of
  a webhook URL are removed from reports.
- The webhook URL must be `https` (`http` only for `localhost`) and must not
  contain user information.
- **Mentions are off**: a message cannot ping `@everyone`, even when its text
  comes from a commit message or a log.
- **Project files.** A `.kjak.toml` of a repository can define a `discord`
  feather that posts wherever it likes; kjak uses it only after
  `kjak config trust`, and a project's feathers reach public addresses only.
  See [project trust](../configuration.md#74-project-configuration-trust).
- What you post is visible to everyone in the channel: no secrets in
  messages.

## 11. Related references

- [Configuration: discord](../configuration.md#discord) — every key
- [Credential references](../configuration.md#71-credential-references) and [secrets files](../configuration.md#77-secrets-files)
- [CLI](../cli.md) — `kjak send`, `kjak feather add`, the JSON report
- [Delivery](../delivery.md#webhooks-slack-discord) — what each HTTP status means, retries
- [Security](../security.md)
- [Troubleshooting](../troubleshooting.md)
- Examples: [discord-only.toml](../examples/discord-only.toml), [discord-and-slack.toml](../examples/discord-and-slack.toml), [multi-channel.toml](../examples/multi-channel.toml)
- Discord's guide: [Intro to Webhooks](https://support.discord.com/hc/en-us/articles/228383668-Intro-to-Webhooks)
- Other tutorials: [Slack](slack.md), [Gmail](gmail.md)
