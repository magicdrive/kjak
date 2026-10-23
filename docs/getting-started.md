# Getting started

[日本語](getting-started_ja.md) · [Configuration reference](configuration.md) · [README](../README.md)

This guide takes you from installing kjak to your first notification in
Slack, Discord or e-mail, then shows how to use kjak from scripts.

- [1. What kjak is](#1-what-kjak-is)
- [2. Install](#2-install)
- [3. Your first notification](#3-your-first-notification)
- [4. Slack](#4-slack)
- [5. Discord](#5-discord)
- [6. E-mail (SMTP)](#6-e-mail-smtp)
- [7. Recipes](#7-recipes)
- [8. Next steps](#8-next-steps)

## 1. What kjak is

kjak is a command-line tool that sends **one message to many destinations**:
Slack channels, Discord channels and e-mail addresses. You run it from a
terminal, a shell script, cron or CI.

```sh
kjak send --to development --level error --title "nightly build" "tests failed on main"
```

- A **feather** is one destination: a Slack incoming webhook, a Discord
  webhook, or an SMTP server with its recipients.
- A **group** is a named list of feathers: `--to development` sends to every
  feather of the group `development`.
- kjak talks to the services directly: Slack and Discord through their
  webhook URLs, e-mail through your SMTP server. There is no kjak server.
- A message has a body, and optionally a title, a level (`info`, `success`,
  `warning`, `error`, `critical`) and a link.

kjak is meant for developers and operators who want a script, a long build or
a scheduled job to tell them what happened, without writing code for each
service.

## 2. Install

### Homebrew (macOS, Linux)

```sh
brew install magicdrive/tap/kjak
```

The formula lives in [magicdrive/homebrew-tap](https://github.com/magicdrive/homebrew-tap).

### Go

With Go 1.27 or later (an older `go` downloads the 1.27 toolchain by itself):

```sh
go install github.com/magicdrive/kjak@latest
```

The binary goes to `$(go env GOPATH)/bin` (usually `~/go/bin`).

### GitHub Releases (any platform)

[GitHub Releases](https://github.com/magicdrive/kjak/releases) has an archive
per platform: darwin/arm64, darwin/amd64, linux/arm64, linux/amd64 (`.tar.gz`)
and windows/amd64 (`.zip`). Each holds `kjak` (`kjak.exe`), `LICENSE` and
`README.md`.

```sh
# example: v0.0.2 on Linux x86-64
curl -LO https://github.com/magicdrive/kjak/releases/download/v0.0.2/kjak_0.0.2_linux_amd64.tar.gz
curl -LO https://github.com/magicdrive/kjak/releases/download/v0.0.2/kjak_checksums.txt
sha256sum -c kjak_checksums.txt --ignore-missing     # macOS: shasum -a 256 -c kjak_checksums.txt --ignore-missing
tar -xzf kjak_0.0.2_linux_amd64.tar.gz kjak
sudo install -m 0755 kjak /usr/local/bin/kjak       # or any directory in your PATH
```

Optionally, check that the archive was built by kjak's release workflow
(needs the [GitHub CLI](https://cli.github.com/)):

```sh
gh attestation verify kjak_0.0.2_linux_amd64.tar.gz --repo magicdrive/kjak
```

On Windows, extract `kjak.exe` from the `.zip` into a directory of your
`PATH`.

### Check the installation

```sh
kjak version        # kjak version v0.0.2
command -v kjak     # where the shell finds it (PowerShell: Get-Command kjak)
```

If the shell says `command not found`, the directory of the binary is not in
your `PATH` (for `go install`: add `$(go env GOPATH)/bin`).

## 3. Your first notification

This section uses Slack. For Discord or e-mail, get the webhook URL or SMTP
settings from [section 5](#5-discord) or [section 6](#6-e-mail-smtp) and
follow the same steps.

### Step 1 — prepare the destination

Create a Slack incoming webhook for the channel that should receive the
messages ([section 4.1](#41-create-an-incoming-webhook)). For a first test,
a channel only you read is a good choice.

### Step 2 — get the webhook URL

Slack shows a URL like `https://hooks.slack.com/services/T…/B…/…`. **The URL
is the credential**: anyone who has it can post to the channel. Do not paste
it into chat, issues or files you commit.

### Step 3 — give it to kjak

kjak keeps a reference to the secret in its configuration, not the secret
itself. The simplest reference is an environment variable:

```sh
kjak config init                       # creates ~/.config/kjak/config.toml

# bash / zsh: type the URL at the prompt, so it stays out of your shell history
printf 'Slack webhook URL: '; read -rs KJAK_DEV_SLACK_WEBHOOK; echo
export KJAK_DEV_SLACK_WEBHOOK

kjak feather add dev_slack --type slack --secret-env KJAK_DEV_SLACK_WEBHOOK
```

PowerShell:

```powershell
kjak config init
$env:KJAK_DEV_SLACK_WEBHOOK = Read-Host "Slack webhook URL"
kjak feather add dev_slack --type slack --secret-env KJAK_DEV_SLACK_WEBHOOK
```

`dev_slack` is the feather's name: letters, digits, `_`, `.` and `-`. The
variable must be set wherever kjak runs; [section 4.3](#43-keep-the-url-for-later)
shows how to keep the URL for later sessions.

### Step 4 — send a test notification

First a dry run: kjak resolves the target and the credential and sends
nothing.

```sh
kjak send --to dev_slack --dry-run "hello from kjak"
```

```
plan  dev_slack (slack)
dry run: 1 target(s), 0 skipped; nothing was sent
```

Then the real message:

```sh
kjak send --to dev_slack "hello from kjak"
```

### Step 5 — check that it worked

```
ok    dev_slack (slack) delivered in 312ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

`ok` means Slack confirmed the message, and kjak exits with status `0`. The
message appears in the channel. `echo $?` right after the command shows the
exit status:

| exit status | meaning |
|---|---|
| `0` | every target delivered |
| `1` | a delivery failed (or its result is unknown) |
| `2` | input, configuration or preflight error: nothing was sent |
| `130` | interrupted (Ctrl-C, or SIGTERM) |

### Step 6 — when it does not work

| you see | cause | fix |
|---|---|---|
| `environment variable KJAK_DEV_SLACK_WEBHOOK is not set` (exit 2) | the variable is not exported in this shell | export it again (step 3) |
| `webhook_url: ... must be https ...` (exit 2) | the value is not the URL Slack gave | copy the `https://hooks.slack.com/...` URL again |
| `FAIL  dev_slack (slack) failed (1 attempt): HTTP 404 ...` (exit 1) | the webhook was deleted or the URL is wrong | create a new webhook, set the variable again |
| `HTTP 403` / `HTTP 401` | the webhook or its app was revoked | create a new webhook |
| `the project configuration is not trusted: ...` (exit 2) | a `.kjak.toml` in the current directory | see [Trust](configuration.md#74-project-configuration-trust), or add `--no-project` |
| `routing error: no feather or group named "dev-slack"` (exit 2) | a typo in `--to` | `kjak feather list` shows the names |

`kjak send ... -o json` shows every attempt with its HTTP status. More in
[troubleshooting.md](troubleshooting.md).

## 4. Slack

### 4.1 Create an incoming webhook

Slack's screens change from time to time; the official guide is
[Sending messages using incoming webhooks](https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks/).
In short:

1. Create a Slack app for your workspace at <https://api.slack.com/apps>
   ("Create New App" → "From scratch").
2. In the app, open **Incoming Webhooks** and turn it on.
3. **Add New Webhook** (to Workspace), choose the channel, allow.
4. Copy the webhook URL (`https://hooks.slack.com/services/...`).

Your workspace may require an administrator to approve the app.

### 4.2 Register it

```sh
kjak feather add dev_slack --type slack --secret-env KJAK_DEV_SLACK_WEBHOOK
```

To receive only some levels, add `--levels warning,error,critical`. To
change a feather, run `feather add` again with `--force` (it replaces the
whole feather).

### 4.3 Keep the URL for later

An `export` lasts for one shell session. To keep the secret:

| where | how | good for |
|---|---|---|
| OS keychain (macOS, Linux desktop) | store it once, then `--secret-keychain SERVICE/ACCOUNT` | your own machine |
| environment variable | set by your CI's secret store, a service manager, or a file your shell reads | CI, servers, cron |
| the configuration file | `--secret-stdin` (the file is created with mode 0600) | quick local use; never for a file you share |

macOS keychain (the `-w` at the end makes `security` prompt for the URL):

```sh
security add-generic-password -s kjak -a dev_slack -w
kjak feather add dev_slack --type slack --secret-keychain kjak/dev_slack --force
```

Linux (GNOME Keyring, KWallet; needs `secret-tool` from `libsecret-tools`
and a desktop session; it reads the URL from the prompt):

```sh
secret-tool store --label "kjak dev_slack" service kjak account dev_slack
kjak feather add dev_slack --type slack --secret-keychain kjak/dev_slack --force
```

Windows has no keychain support in kjak: use an environment variable.

From standard input, stored in the file as a literal:

```sh
read -rs URL && printf '%s\n' "$URL" | kjak feather add dev_slack --type slack --secret-stdin --force; unset URL
```

### 4.4 Test it

```sh
kjak send --to dev_slack --title "kjak" --level success "Slack works"
```

The message shows as `[SUCCESS] kjak: Slack works` with a colored
attachment. Mentions such as `@channel` are escaped: a message never pings
anyone.

### 4.5 Common errors

| error | meaning |
|---|---|
| `HTTP 404` | the webhook or its channel no longer exists |
| `HTTP 403` / `HTTP 401` | the app was removed or the webhook revoked |
| `HTTP 400` | Slack refused the payload; check the message length (body up to 16384 characters) |
| `HTTP 429` | rate limited; kjak waits `Retry-After` and retries within the deadline |

## 5. Discord

### 5.1 Create a webhook

You need the **Manage Webhooks** permission on the channel. Discord's help
article is [Intro to Webhooks](https://support.discord.com/hc/en-us/articles/228383668-Intro-to-Webhooks);
the API reference is [Webhook Resource](https://discord.com/developers/docs/resources/webhook).

1. Server Settings → **Integrations** → **Webhooks** → **New Webhook**.
2. Choose the channel (and a name and avatar if you like).
3. **Copy Webhook URL** (`https://discord.com/api/webhooks/<id>/<token>`).

### 5.2 Register it

```sh
printf 'Discord webhook URL: '; read -rs KJAK_DEV_DISCORD_WEBHOOK; echo
export KJAK_DEV_DISCORD_WEBHOOK
kjak feather add dev_discord --type discord --secret-env KJAK_DEV_DISCORD_WEBHOOK --username kjak
```

`--username` sets the display name of the messages (optional). The other
ways to keep the URL are the same as for Slack ([4.3](#43-keep-the-url-for-later)).

### 5.3 Test it

```sh
kjak send --to dev_discord --title "kjak" --level warning "Discord works"
```

The message is an embed colored by level. kjak asks Discord to confirm the
message (`?wait=true`), so `ok` means Discord created it. Mentions are
disabled.

### 5.4 Common errors

| error | meaning |
|---|---|
| `HTTP 401` / `HTTP 404` | the webhook was deleted or the token in the URL is wrong |
| `HTTP 400` | Discord refused the payload |
| `HTTP 429` | rate limited; kjak waits as Discord says and retries within the deadline |

A body longer than 4096 characters is cut to fit a Discord embed (it ends
with `…`).

## 6. E-mail (SMTP)

### 6.1 What you need

From your mail provider or administrator:

| setting | kjak option | notes |
|---|---|---|
| SMTP host | `--host` | required |
| port | `--port` | default 587 for `starttls`, 465 for `tls`, 25 for `none` |
| security | `--security` | `starttls` (default): plain connection upgraded with STARTTLS, required; `tls`: TLS from the start (usually port 465); `none`: no encryption, for a local relay only |
| user and password | `--user`, `--secret-env` / `--secret-keychain` / `--secret-stdin` | AUTH PLAIN, else AUTH LOGIN; refused with `security none` |
| sender | `--from` | a bare address: `kjak@example.com` |
| recipients | `--mail-to` | one or more bare addresses |

Your provider documents its host, port and security mode, and whether SMTP
needs a separate password for applications. kjak always verifies the
server's certificate (TLS 1.2 or later) and never falls back to clear text.

### 6.2 Register it

```sh
printf 'SMTP password: '; read -rs KJAK_SMTP_PASSWORD; echo
export KJAK_SMTP_PASSWORD
kjak feather add oncall_mail --type email \
    --host smtp.example.com --port 587 --security starttls \
    --user kjak@example.com --secret-env KJAK_SMTP_PASSWORD \
    --from kjak@example.com --mail-to oncall@example.com \
    --subject-prefix "[kjak]"
```

Several recipients: `--mail-to a@example.com,b@example.com` (or repeat
`--mail-to`).

### 6.3 Test it

```sh
kjak send --to oncall_mail --dry-run "mail test"     # checks the password reference only
kjak send --to oncall_mail --title "kjak" "mail test"
```

The dry run does not connect to the server. The real send succeeds when the
server accepted the message for delivery (a `2xx` reply after the message data); the
subject is `[kjak] kjak` (prefix and title), or `[LEVEL] first line of the
body` without a title.

### 6.4 Common errors

| error | meaning |
|---|---|
| `AUTH: SMTP 535 ...` | wrong user or password (not retried, to avoid locking the account) |
| `the server does not offer STARTTLS` | the port speaks implicit TLS: use `--security tls --port 465` |
| `TLS certificate verification failed` | the certificate is not trusted by your system; kjak cannot skip the check |
| `SMTP 550 ...` at RCPT | a recipient was refused; nothing is sent to anyone |
| connection refused / timeout | wrong host or port, or a firewall blocks outgoing SMTP |

## 7. Recipes

The examples use a group `development`. Create it from feathers you added:

```sh
kjak group set development dev_slack dev_discord
```

### Send to several destinations

```sh
kjak send --to development "deploy started"
kjak send --to dev_slack,oncall_mail --level error "disk almost full"
```

```
ok    dev_slack (slack) delivered in 280ms (1 attempt)
FAIL  dev_discord (discord) failed (1 attempt): HTTP 404: Unknown Webhook
partial: 1 delivered, 1 failed, 0 skipped
```

Each destination is delivered independently: one failure does not stop the
others, and the exit status is `1` if any failed. A group member that is
disabled or does not accept the level is listed as `skip` and does not count
as a failure.

### Notify from a shell script

Keep the script's own exit status, and do not let a failed notification hide
the result of your work:

```sh
#!/bin/sh
# notify.sh: run a command, report its result, exit with its status
"$@"
status=$?
if [ "$status" -eq 0 ]; then
	kjak send --to development --level success --title "$1" "finished" ||
		echo "notify.sh: the notification failed (kjak exit $?)" >&2
else
	kjak send --to development --level error --title "$1" "failed with exit status $status" ||
		echo "notify.sh: the notification failed (kjak exit $?)" >&2
fi
exit "$status"
```

```sh
sh notify.sh make test
```

### Report the end of a long job

```sh
start=$(date +%s)
./long-job.sh
status=$?
elapsed=$(( $(date +%s) - start ))
if [ "$status" -eq 0 ]; then level=success; else level=error; fi
kjak send --to development --level "$level" --title "long-job" \
    "exit status $status after ${elapsed}s"
```

### Send the output of a failed command

A pipe hides the exit status of the command on its left, so capture the
output first:

```sh
if ! out=$(make test 2>&1); then
	printf '%s\n' "$out" | tail -n 20 |
		kjak send --to development --stdin --level error --title "tests failed"
fi
```

`--stdin` reads the body from standard input (at most 16384 characters).

### Run from cron

cron starts commands with a minimal environment: no shell profile, a short
`PATH`, and none of your exported variables. Put what kjak needs in the
crontab or in a wrapper script:

```sh
#!/bin/sh
# ~/bin/backup-and-notify.sh, run by cron
PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin
. "$HOME/.config/kjak/env"     # export KJAK_DEV_SLACK_WEBHOOK=...; chmod 600 this file
/usr/local/bin/backup.sh
status=$?
if [ "$status" -ne 0 ]; then
	kjak send --to development --level error --title "backup" "backup failed with exit status $status"
fi
exit "$status"
```

```
# crontab -e
0 3 * * * $HOME/bin/backup-and-notify.sh >> $HOME/backup.log 2>&1
```

The `env` file is plain shell, read by your script; keep it private
(`chmod 600`) and out of any repository.

### Use JSON in programs

```sh
kjak send --to development -o json "build finished" > report.json
```

The report (`schema_version: 1`) lists every target with its `status`,
`acceptance`, attempts and errors; see [cli.md](cli.md#json-output--o-json).

## 8. Next steps

- [Configuration reference](configuration.md): every key, the commands that
  manage them, credentials, trust and delivery rules.
- [CLI usage](cli.md): every command and option.
- [Troubleshooting](troubleshooting.md).
