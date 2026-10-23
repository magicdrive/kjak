# kjak

[日本語](README_ja.md) · [Getting started](docs/getting-started.md) · [Configuration](docs/configuration.md)

kjak delivers one notification to many destinations — Slack, Discord and
e-mail — from the command line, a script or CI. Each destination is a
**feather**; feathers can be gathered in **groups**.

```sh
kjak send --to development --level error --title "nightly build" "tests failed on main"
```

- Explicit subcommands: `send`, `feather`, `group`, `config`, `template`, `version`
- Best-effort delivery: every feather independently, at most 5 at a time, one
  deadline (10s) that covers every retry
- Honest results: a delivery is **confirmed** only when the provider says so; a
  message that may have arrived but got no answer is **unknown** and is never
  retried (no duplicates)
- Secrets from environment variables or the OS keychain, never printed
- A project configuration (`.kjak.toml`) is used only after you trust it
- Text or JSON output (`schema_version: 1`), and a stable exit code contract
- One static Go binary (`CGO_ENABLED=0`) for macOS, Linux and Windows

## Install

```sh
brew install magicdrive/tap/kjak
```

Or download an archive from [GitHub Releases](https://github.com/magicdrive/kjak/releases)
(`.tar.gz` for macOS and Linux, `.zip` for Windows) and check it:

```sh
sha256sum -c kjak_checksums.txt --ignore-missing
gh attestation verify kjak_0.0.2_linux_amd64.tar.gz --repo magicdrive/kjak
```

From source (Go 1.27): `go install github.com/magicdrive/kjak@latest`.

Supported platforms: darwin/arm64, darwin/amd64, linux/arm64, linux/amd64,
windows/amd64.

## Quick start

The [Getting started guide](docs/getting-started.md) walks through each step
for Slack, Discord and e-mail.

```sh
# 1. a configuration file (~/.config/kjak/config.toml)
kjak config init

# 2. a feather; its webhook URL comes from an environment variable
printf 'Slack webhook URL: '; read -rs KJAK_DEV_SLACK_WEBHOOK; echo
export KJAK_DEV_SLACK_WEBHOOK
kjak feather add dev_slack --type slack --secret-env KJAK_DEV_SLACK_WEBHOOK

# 3. a group
kjak group set development dev_slack

# 4. check what would happen, then send
kjak send --to development --dry-run "build finished"
kjak send --to development --title "CI" --level success "build finished"
```

```
ok    dev_slack (slack) delivered in 312ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

## Sending

The message comes from exactly one input:

```sh
kjak send --to dev_slack "deploy started"                     # arguments
make test 2>&1 | tail -n 20 | kjak send --to development --stdin --level error --title "tests failed"
echo '{"title":"Disk","body":"/var at 91%","level":"warning"}' | kjak send --to ops --json-input -
kjak send --to ops --template deploy --var service=api --var version=1.4.2
kjak send --to ops --template-file deploy.toml --var service=api
```

| option | meaning |
|---|---|
| `-t, --to NAME[,NAME]` | a feather or a group (repeatable); default `defaults.targets` |
| `-T, --title` / `-l, --level` / `-u, --url` | title, level (`info`, `success`, `warning`, `error`, `critical`), link |
| `--timeout 10s` / `--max-retries 2` / `--concurrency 5` | delivery limits |
| `--dry-run` | resolve targets and credentials, send nothing |
| `-o json` | the JSON report (`schema_version: 1`) |

Exit status: `0` delivered, `1` a delivery failed (or its acceptance is
unknown), `2` input, configuration or preflight error (nothing was sent),
`130` interrupted.

See [docs/cli.md](docs/cli.md) for every command and the JSON report.

## Feathers, groups, templates

```sh
kjak feather add ops_discord --type discord --secret-keychain kjak/ops_discord --username kjak
kjak feather add oncall_mail --type email --host smtp.example.com --user kjak@example.com \
    --secret-env KJAK_SMTP_PASSWORD --from kjak@example.com --mail-to oncall@example.com \
    --levels error,critical
kjak feather list
kjak feather disable ops_discord
kjak group set ops ops_discord oncall_mail
kjak template set deploy --title 'Deployed {{.service}}' --body '{{.service}} {{.version}} is live' --level success
kjak template render deploy --var service=api --var version=1.4.2
kjak config show
```

A feather named directly must be enabled and accept the level of the message
(otherwise nothing is sent: exit 2). A group member that is disabled or
filtered out by its `levels` is **skipped** and reported as such.

## Configuration

```toml
# ~/.config/kjak/config.toml  ($XDG_CONFIG_HOME/kjak/config.toml)
[defaults]
timeout = "10s"
max_retries = 2
concurrency = 5
targets = ["development"]

[feathers.development_slack]
type = "slack"
enabled = true
webhook_url = { env = "KJAK_DEV_SLACK_WEBHOOK" }

[feathers.oncall_mail]
type = "email"
host = "smtp.example.com"
user = "kjak@example.com"
password = { keychain = { service = "kjak", account = "oncall_mail" } }
from = "kjak@example.com"
to = ["oncall@example.com"]
levels = ["error", "critical"]

[groups.development]
feathers = ["development_slack"]
```

A `.kjak.toml` in the current directory adds to (and overrides) the global
file once it is trusted:

```sh
kjak config trust      # shows the file's path, SHA-256 and what it sends where
```

Any change to the file withdraws the trust. See
[docs/configuration.md](docs/configuration.md) and
[docs/security.md](docs/security.md).

## Known limitations

- Feathers: Slack (incoming webhooks), Discord (webhooks) and e-mail (SMTP).
  **OS-native notifications are not supported** (a future feather); a feather
  of type `os` is a configuration error.
- OS keychain: macOS (`security`) and Linux (`secret-tool`, libsecret) are
  read-only; **Windows has no keychain support** (use `env`).
- Groups do not nest.
- kjak rewrites its configuration files without their comments when a
  command (`feather add`, `group set`, ...) changes them.
- No plugins: every feather is built in.

More in [docs/troubleshooting.md](docs/troubleshooting.md).

## Documentation

- [Getting started](docs/getting-started.md) ([日本語](docs/getting-started_ja.md)): install, Slack, Discord, e-mail, recipes
- [Configuration reference](docs/configuration.md) ([日本語](docs/configuration_ja.md)): every key, CLI management, credentials, trust, delivery behavior
- [CLI usage](docs/cli.md)
- [Credentials, trust and security](docs/security.md)
- [Delivery, retry and acceptance semantics](docs/delivery.md)
- [Development guide](docs/development.md)
- [Release runbook](docs/release.md)
- [Troubleshooting](docs/troubleshooting.md)

## License

MIT
