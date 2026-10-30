# Slack

[日本語](slack_ja.md) · [All tutorials](README.md) · [Configuration](../configuration.md#slack)

This tutorial sends kjak's messages to a Slack channel through an incoming
webhook of a Slack app: creating the app and the webhook, keeping the URL
secret, the first message and the options.

## 1. Overview

A `slack` feather posts each message to one incoming webhook. An incoming
webhook belongs to one Slack app and posts to the one channel it was created
for, as that app:

```toml
[feathers.slack]
type = "slack"
webhook_url = { secret = "slack.webhook_url" }
```

- The message is a text line (`[LEVEL] title: first line of the body`, used
  by notifications and clients without attachments) and an **attachment**
  colored by level with the title, the body, the URL as the title's link
  and the footer `kjak · <level>`.
- Slack answers HTTP 200 when it accepted the message: `acceptance:
  confirmed`.
- `&`, `<` and `>` are escaped, so a message **cannot mention** `@channel`,
  `@here` or a user, whatever its text.
- **The webhook URL is the credential.** Anyone who has it can post to the
  channel. kjak never takes it as a value in the configuration: the
  configuration names where to read it.
- There is no `username` for Slack: a webhook posts as its app (the app's
  name and icon). A `username` key is a configuration error.

## 2. Prerequisites

- kjak installed (`kjak version`): [getting started](../getting-started.md#2-install).
- A Slack workspace where you may create an app and install it. Some
  workspaces require an administrator to approve apps.
- The channel that should receive the messages. For a first test, a channel
  only you read is a good choice.
- Network access to `https://hooks.slack.com` from the machine that runs
  kjak.

## 3. Setup

Create the app and its webhook. Slack's screens change from time to time;
the names below may differ slightly. Slack's own guide:
[Sending messages using incoming webhooks](https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks/).

1. Open <https://api.slack.com/apps> → **Create New App** → **From scratch**;
   give it a name (the author of the messages) and choose the workspace.
2. In the app's settings, open **Incoming Webhooks** and turn **Activate
   Incoming Webhooks** on.
3. **Add New Webhook to Workspace** (on newer screens: **Add New Webhook**),
   choose the channel, and **Allow**.
4. Copy the **Webhook URL** of the new row.

The URL looks like `https://hooks.slack.com/services/T…/B…/…`. Do not paste
it into chat, issues or a file you commit.

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
[slack]
webhook_url = "https://hooks.slack.com/services/T00000000/B00000000/replace-me"
```

On Windows the file is `%AppData%\kjak\secrets.toml` (private by the
profile's default permissions). Other places for the URL: section 8.

## 4. Minimal configuration

The configuration file is `~/.config/kjak/config.toml` (on Windows
`%AppData%\kjak\config.toml`; `kjak config path` shows it). Copy this whole
file:

```toml
# ~/.config/kjak/config.toml
[feathers.slack]
type = "slack"
webhook_url = { secret = "slack.webhook_url" }
```

`{ secret = "slack.webhook_url" }` reads the key `webhook_url` of the table
`[slack]` of the secrets file. Check both files:

```sh
kjak config validate
kjak config path
kjak feather show slack
```

`feather show` prints `webhook_url  secret:slack.webhook_url`, never the URL.

## 5. CLI configuration

The same feather, written by kjak:

```sh
kjak config init
kjak feather add slack --type slack --secret-ref slack.webhook_url
```

Only some levels (here: warnings and failures):

```sh
kjak feather add slack --type slack --secret-ref slack.webhook_url --levels warning,error,critical --force
```

The secret is always given by reference, never as an argument:

| option | stored as |
|---|---|
| `--secret-ref NAME` | `{ secret = "NAME" }`, an entry of the secrets file |
| `--secret-env VAR` | `{ env = "VAR" }`, an environment variable |
| `--secret-keychain SERVICE/ACCOUNT` | `{ keychain = { service, account } }` (macOS, Linux) |

Other options: `--disabled`, `--scope project`, `--force` (replace a
feather of the same name). `--username` is for Discord only.

## 6. First notification

A dry run resolves the target and the URL and sends nothing:

```sh
kjak send --to slack --dry-run "Hello from kjak"
```

```
plan  slack (slack)
dry run: 1 target(s), 0 skipped; nothing was sent
```

Then the message:

```sh
kjak send --to slack "Hello from kjak"
kjak send --to slack --title "kjak" --level success "Slack works"
```

```
ok    slack (slack) delivered in 312ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

The channel shows `[SUCCESS] kjak: Slack works` and a green attachment. kjak
exits 0.

### Read the result as JSON

```sh
kjak send --to slack -o json "Hello from kjak"
kjak send --to slack -o json "Hello from kjak" | jq '.results[0] | {status, acceptance, attempts, error}'
```

```json
{"status": "delivered", "acceptance": "confirmed", "attempts": 1, "error": null}
```

A removed webhook (exit 1; the text after the status is Slack's):

```json
{
  "status": "failed",
  "acceptance": "rejected",
  "attempts": 1,
  "error": {"code": "rejected", "message": "HTTP 404: no_service"}
}
```

`history` lists every attempt with its `status_code`. `acceptance: unknown`
(a 502/504 or a lost answer) means the message **may** have been posted:
look at the channel before sending again.

## 7. Advanced configuration

One webhook posts to one channel, so several channels are several feathers.
A complete configuration: everything to `#deploys`, failures also to
`#alerts`, a group for both, defaults and templates.

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["slack_all"]
level = "info"
timeout = "15s"
max_retries = 2

[feathers.slack_deploys]
type = "slack"
webhook_url = { secret = "slack_deploys.webhook_url" }

[feathers.slack_alerts]
type = "slack"
webhook_url = { secret = "slack_alerts.webhook_url" }
levels = ["warning", "error", "critical"]

[groups.slack_all]
feathers = ["slack_deploys", "slack_alerts"]

[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"
url = "https://example.com/releases/{{.version}}"

[templates.failure]
title = "{{.job}} failed"
body = "{{.job}} failed on {{.host}}: {{.reason}}"
level = "error"
```

```toml
# ~/.config/kjak/secrets.toml (mode 0600; dummy values)
[slack_deploys]
webhook_url = "https://hooks.slack.com/services/T00000000/B00000001/replace-me"

[slack_alerts]
webhook_url = "https://hooks.slack.com/services/T00000000/B00000002/replace-me"
```

```sh
kjak send --template deploy --var service=api --var version=1.4.2
kjak send --template failure --var job=nightly --var host=ci-1 --var reason=timeout
kjak template render failure --var job=nightly --var host=ci-1 --var reason=timeout
```

The deploy message reaches `slack_deploys` only: `slack_alerts` does not
accept `success` and is **skipped** (`skip  slack_alerts (slack):
level_filter (group slack_all)`). A skipped member is not an error: the
send succeeds.

### Output of a failed command

```sh
make test 2>&1 | tail -n 20 | kjak send --to slack_alerts --stdin --level error --title "tests failed"
```

The body is at most 16384 characters; `--stdin` reads at most 1 MiB, so cut
long output first (`tail`).

### Rate limits

Slack limits incoming webhooks (about one message per second per webhook,
by Slack's documentation). An HTTP 429 is retried after Slack's
`Retry-After`, within the send's `--timeout`.

## 8. Secrets

| where | configuration | good for |
|---|---|---|
| secrets file (`~/.config/kjak/secrets.toml`, mode 0600) | `{ secret = "slack.webhook_url" }` | your own machine, any OS (recommended) |
| environment variable | `{ env = "KJAK_SLACK_WEBHOOK" }` | CI, servers, cron (filled from their secret store) |
| OS keychain (macOS, Linux desktop) | `{ keychain = { service = "kjak", account = "slack" } }` | your own machine |

**Environment variable.** Type the URL at a prompt, so it stays out of your
shell history (an `export KJAK_SLACK_WEBHOOK='https://...'` typed as a
command is kept in the history):

```sh
printf 'Slack webhook URL: '; read -rs KJAK_SLACK_WEBHOOK; echo
export KJAK_SLACK_WEBHOOK
kjak feather add slack --type slack --secret-env KJAK_SLACK_WEBHOOK --force
```

PowerShell:

```powershell
$env:KJAK_SLACK_WEBHOOK = Read-Host "Slack webhook URL"
kjak feather add slack --type slack --secret-env KJAK_SLACK_WEBHOOK --force
```

The variable lasts for the shell session and must be set wherever kjak runs.
In CI, map the CI's secret to the variable (GitHub Actions, in the step or
the job):

```yaml
env:
  KJAK_SLACK_WEBHOOK: ${{ secrets.SLACK_WEBHOOK }}
```

**macOS keychain** (`-w` at the end makes `security` prompt for the URL):

```sh
security add-generic-password -s kjak -a slack -w
kjak feather add slack --type slack --secret-keychain kjak/slack --force
```

On Linux: `secret-tool store --label "kjak slack" service kjak account slack`
(libsecret-tools, a desktop session). Windows has no keychain support in
kjak: use the secrets file or an environment variable.

More: [credential references](../configuration.md#71-credential-references),
[secrets files](../configuration.md#77-secrets-files).

## 9. Troubleshooting

| you see | cause | fix |
|---|---|---|
| `secret:slack.webhook_url is not in the user secrets file ...` (exit 2) | the table or key is missing, or another file is used | `kjak config path` shows the file; add `[slack] webhook_url = "..."` |
| `the secrets file is not private: ...` | the file is readable by others | `chmod 600 ~/.config/kjak/secrets.toml` |
| `environment variable KJAK_SLACK_WEBHOOK is not set` (exit 2) | an `env` reference in a shell without it | set it again (section 8) |
| `webhook_url: credential error: the resolved value must be an absolute https URL` / `the resolved value must be https ...` | the value is not the URL Slack gave | copy the Webhook URL again |
| `slack feathers take no username ...` | `username` (Discord only) | remove it; rename the app in Slack instead |
| `HTTP 404: no_service` (`rejected`) | the webhook was removed or the app uninstalled | add a new webhook, update the secret |
| `HTTP 404: channel_not_found` / `HTTP 410: channel_is_archived` | the channel was deleted or archived | add a webhook for another channel |
| `HTTP 403: action_prohibited` (or another reason) | the workspace or an administrator blocks the post | ask the workspace administrator |
| `HTTP 400: invalid_payload` | Slack refused the payload | check the message (its length, its text); if it persists, report it to kjak with the command line |
| `HTTP 429` (`rate_limited`) | too many messages | kjak retries after Slack's wait; send less often |
| `????  slack (slack) failed ...: HTTP 504 from a gateway` (`unknown`) | the answer was lost; the message may exist | look at the channel before sending again |
| `feather "slack" does not accept level info` (exit 2) | `levels` and a feather named directly | a fitting `--level`, or send to a group |

The reasons after `HTTP 4xx:` are Slack's own texts; the list of reasons is
in Slack's guide (section 3).

```sh
kjak config validate
kjak send --to slack --dry-run "test"
kjak send --to slack -o json "test"
```

## 10. Security notes

- **The URL is a password.** Whoever has it can post to the channel as your
  app. If it was committed, pasted or shared, remove the webhook in the app's
  **Incoming Webhooks** page (or uninstall the app) and add a new one, then
  update the secret.
- Keep it out of the configuration file, the repository and the shell
  history: a secrets file with mode 0600, an environment variable from a
  secret store, or the keychain.
- kjak never prints the URL: `feather show`, `config show` and every error
  show its source (`secret:slack.webhook_url`), and the path and query of a
  webhook URL are removed from reports.
- The webhook URL must be `https` (`http` only for `localhost`) and must not
  contain user information.
- **Mentions are escaped**: a message cannot ping `@channel`, even when its
  text comes from a commit message or a log.
- **Project files.** A `.kjak.toml` of a repository can define a `slack`
  feather that posts wherever it likes; kjak uses it only after
  `kjak config trust`, and a project's `env` and `keychain` references need
  your grant by name. See [project trust](../configuration.md#74-project-configuration-trust).
- What you post is visible to everyone in the channel: no secrets in
  messages.

## 11. Related references

- [Configuration: slack](../configuration.md#slack) — every key
- [Credential references](../configuration.md#71-credential-references) and [secrets files](../configuration.md#77-secrets-files)
- [CLI](../cli.md) — `kjak send`, `kjak feather add`, the JSON report
- [Delivery](../delivery.md#webhooks-slack-discord) — what each HTTP status means, retries
- [Security](../security.md)
- [Troubleshooting](../troubleshooting.md)
- Examples: [slack-only.toml](../examples/slack-only.toml), [groups.toml](../examples/groups.toml), [multi-channel.toml](../examples/multi-channel.toml)
- Getting started with Slack: [getting-started.md](../getting-started.md#4-slack)
- Other tutorials: [Discord](discord.md), [Gmail](gmail.md)
