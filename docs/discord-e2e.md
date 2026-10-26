# Discord: manual end-to-end check

The automated tests never reach Discord: they run kjak against local test
servers (`internal/e2e`). This is the procedure for a person to check, by
hand, that a build of kjak really posts to Discord — before a release, or
after a change to the discord feather or to secrets handling.

It sends a handful of messages to **one channel you own**, through a webhook
you create for the check and delete afterwards. It changes nothing in your
own kjak configuration.

**Never paste the webhook URL** into an issue, a pull request, a chat, a
command line or a file other than the secrets file below: the URL is the
credential (anyone who has it can post to the channel). If it leaks, delete
the webhook in Discord. Every example here uses the placeholder
`https://discord.com/api/webhooks/<id>/<token>`.

- [1. Prepare](#1-prepare)
- [2. Configure (an isolated directory)](#2-configure-an-isolated-directory)
- [3. Validate (nothing is sent)](#3-validate-nothing-is-sent)
- [4. Send](#4-send)
- [5. Check that the secret is never printed](#5-check-that-the-secret-is-never-printed)
- [6. Clean up (and the failure case)](#6-clean-up-and-the-failure-case)
- [7. Troubleshooting](#7-troubleshooting)
- [Record](#record)

## 1. Prepare

- The kjak to check: `make build` (then `build/kjak`), or the installed
  release (`kjak --version`). Below, `kjak` is that binary.
- A Discord server where you have **Manage Webhooks**, and a test channel in
  it that nobody relies on.
- Create the webhook: *Server Settings → Integrations → Webhooks → New
  Webhook*, choose the test channel, name it `kjak e2e`, *Copy Webhook URL*.
  Keep it on the clipboard only until step 2.

## 2. Configure (an isolated directory)

A throw-away directory holds the configuration and the secrets file, chosen
with `$KJAK_CONFIG` and `$KJAK_SECRETS`: your own files are not read or
changed. Run every following step from this directory, in this shell.

```sh
umask 077
e2e=$(mktemp -d)
cd "$e2e"
export KJAK_CONFIG="$e2e/config.toml" KJAK_SECRETS="$e2e/secrets.toml"
```

The configuration names the secret with `{ secret = ... }`; the URL itself
is only in the secrets file:

```sh
cat > config.toml <<'EOF'
[feathers.discord_e2e]
type = "discord"
webhook_url = { secret = "discord_e2e.webhook_url" }
username = "kjak e2e"

[groups.e2e]
feathers = ["discord_e2e"]
EOF
```

Write the secrets file without the URL appearing on the screen or in the
shell history: paste it at the prompt (nothing is echoed), then press Enter.

```sh
printf 'Discord webhook URL: '; IFS= read -rs url; printf '\n'
printf '[discord_e2e]\nwebhook_url = "%s"\n' "$url" > secrets.toml
unset url
chmod 600 secrets.toml
```

The file then reads (with your values):

```toml
[discord_e2e]
webhook_url = "https://discord.com/api/webhooks/<id>/<token>"
```

(Windows: create the two files with an editor in a new folder, and set
`$env:KJAK_CONFIG` and `$env:KJAK_SECRETS` to their full paths in
PowerShell. The access control list of the secrets file is not checked
there: keep the folder under your profile.)

## 3. Validate (nothing is sent)

```sh
kjak config path
kjak config validate
kjak config validate --strict
kjak config show
kjak send --to e2e --dry-run "kjak e2e dry run"
```

Expected:

| command | expected |
|---|---|
| `config path` | `global (explicit ($KJAK_CONFIG))` and `secrets (explicit ($KJAK_SECRETS)) ... (1 secret(s))`; exit 0 |
| `config validate`, `--strict` | `.../config.toml: OK`; exit 0 (`--strict` refuses literal secrets: there are none) |
| `config show` | `webhook_url = {secret = 'discord_e2e.webhook_url'}`: the reference, never the URL |
| `send --dry-run` | `plan  discord_e2e (discord)` and `dry run: 1 target(s), 0 skipped; nothing was sent`; exit 0; nothing in the channel |

## 4. Send

Each command posts one message. After each, look at the channel and check
the exit status (`echo $?`).

```sh
kjak send --to discord_e2e "kjak e2e 1/6: plain message"
kjak send --to e2e --level success --title "kjak e2e 2/6" "via the group, level success"
kjak send --to e2e --level critical --title "kjak e2e 3/6" --url "https://example.com/run/1" "level critical, with a link"
kjak send --to e2e --title "kjak e2e 4/6" "mentions are not pinged: @everyone @here"
printf 'line 1\nline 2\n' | kjak send --to e2e --stdin --level warning --title "kjak e2e 5/6"
kjak send --to e2e --level error --title "kjak e2e 6/6" -o json "JSON report" > report.json
```

Expected:

| # | in the channel | output | exit |
|---|---|---|---|
| 1 | an embed with the body, posted as `kjak e2e` | `ok    discord_e2e (discord) delivered in ...ms (1 attempt)` | 0 |
| 2 | the title, the success color | the same line | 0 |
| 3 | the critical color, the title linked to the URL | the same line | 0 |
| 4 | `@everyone @here` shown as text: **nobody is notified** | the same line | 0 |
| 5 | the two lines of the body, the warning color | the same line | 0 |
| 6 | the error color | `report.json`: `"outcome": "success"`, `"status": "delivered"`, `"acceptance": "confirmed"`, `"attempts": 1`, `"status_code": 200` in `history` | 0 |

`--level error` with a delivered message still exits 0: the exit status is
about the delivery, not the level.

## 5. Check that the secret is never printed

Run a send and the commands that print the configuration, keeping all of
their output, and look for the webhook path in it. The count must be 0.

```sh
{ kjak config path; kjak config show; kjak feather show discord_e2e;
  kjak send --to e2e -o json "kjak e2e: redaction check"; } > out.txt 2>&1
grep -c 'api/webhooks/' out.txt; grep -c 'api/webhooks/' report.json
```

Expected: `0` and `0` (one more message in the channel).

## 6. Clean up (and the failure case)

1. In Discord, **delete the webhook** (*Integrations → Webhooks → kjak e2e →
   Delete Webhook*).
2. The failure case, with the deleted webhook:

   ```sh
   kjak send --to e2e "kjak e2e: after the webhook was deleted"; echo "exit $?"
   kjak send --to e2e -o json "kjak e2e: deleted" > failed.json; grep -c 'api/webhooks/' failed.json
   ```

   Expected: `FAIL  discord_e2e (discord) failed (1 attempt): HTTP 404: Unknown Webhook`,
   exit 1; in `failed.json` `"outcome": "failure"`, `"acceptance": "rejected"`,
   `"code": "rejected"`; the `grep` prints 0. Nothing is posted.
3. Remove the directory and the variables:

   ```sh
   cd / && rm -rf "$e2e"
   unset KJAK_CONFIG KJAK_SECRETS e2e
   ```

## 7. Troubleshooting

| symptom | cause, fix |
|---|---|
| `the secrets file is not private: group or others have access (mode 0644)`, exit 2 | `chmod 600 "$KJAK_SECRETS"` (step 2 sets it) |
| `the explicit secrets file ... does not exist`, exit 2 | `$KJAK_SECRETS` is unset or wrong in this shell: re-export it (step 2) |
| `secret:discord_e2e.webhook_url ...` not found | the secrets file has another table or key: it must be `[discord_e2e]` / `webhook_url` |
| `the secret is empty` or `the resolved value must be an absolute https URL` (`credential`), exit 2 | the paste was empty or not a whole `https://` URL: write the file again (step 2) |
| `the project configuration is not trusted` | a `.kjak.toml` is in the current directory: run from the directory of step 2, or add `--no-project` |
| `HTTP 404: Unknown Webhook` | the webhook was deleted, or the URL was copied incompletely |
| `HTTP 401` / `HTTP 403` | the token part of the URL is wrong, or the webhook was revoked |
| `HTTP 429` and `history` with a `retry_after_ms` | Discord's rate limit: kjak waits and retries within `--timeout` |
| `????` / `acceptance: unknown` | the answer was lost after the request was sent: **check the channel** before sending again (kjak does not retry it) |
| `network` / TLS errors | `https://discord.com` is not reachable from this machine; the discord feather uses `HTTPS_PROXY` / `NO_PROXY` from the environment when the network needs a proxy |

More: [troubleshooting.md](troubleshooting.md), [security.md](security.md).

## Record

For a release check, note: the kjak version (`kjak --version`), the OS and
architecture, the date, the six messages seen (with their colors and the
unpinged mentions), the two `grep` counts of 0, and the 404 after the
deletion. Do not record the webhook URL, a screenshot that shows it, or the
contents of `secrets.toml`.
