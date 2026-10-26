# kjak

[日本語](README_ja.md) · [Getting started](docs/getting-started.md) · [Configuration](docs/configuration.md)

kjak delivers one notification to many destinations — Slack, Discord,
e-mail, any HTTP API, or a plugin of your own — from the command line, a
script or CI. Each destination is a
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
- Secrets from environment variables, the OS keychain or a private secrets
  file, never printed and never kept in the configuration
- A project configuration (`.kjak.toml`) is used only after you trust it, and
  reads your own secrets only by the names you allowed
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
webhook_url = { secret = "development_slack.webhook_url" }

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

### Secrets

Secrets go in a secrets file of their own, private to you (mode 0600:
kjak refuses one that group or others can read), and the configuration
names them:

```toml
# ~/.config/kjak/secrets.toml
[development_slack]
webhook_url = "https://hooks.slack.com/services/..."
```

`{ env = "VAR" }` and `{ keychain = { service = "...", account = "..." } }`
work as well. A `{ literal = "..." }` secret in the configuration still works
but is deprecated: `kjak config migrate-secrets` moves literals to a new
secrets file ([migration guide](docs/migration.md)).

### User, current and explicit files

| | configuration | secrets |
|---|---|---|
| explicit | `--config FILE`, `$KJAK_CONFIG` | `--secrets FILE`, `$KJAK_SECRETS` |
| user | `~/.config/kjak/config.toml` | `~/.config/kjak/secrets.toml` |
| current | `./.kjak.toml` (once trusted) | `./.kjak.secrets.toml` (trusted with `.kjak.toml`, for it only) |

An explicit file replaces the user file of its kind; each kind is chosen on
its own. `kjak config path` shows which files are used, how each was chosen
and its state.

A `.kjak.toml` in the current directory adds to (and overrides) the global
file once it is trusted:

```sh
kjak config trust      # shows the file's path, SHA-256 and what it sends where
```

Any change to the file (or to its `.kjak.secrets.toml`) withdraws the
trust. The project's secret references read its own secrets file; a name it
wants from **your** secrets file must be allowed, name by name, when you
trust it. See [docs/configuration.md](docs/configuration.md) and
[docs/security.md](docs/security.md).

### HTTP API / webhook feathers

An `http` feather sends to any HTTP API: method, headers, query,
authentication (bearer, basic, a custom header; secrets by reference) and a
JSON or text body, from a template if you want one.

```toml
[feathers.ops_api]
type = "http"
url = "https://api.example.com/v1/alerts"

[feathers.ops_api.auth]
type = "bearer"
token = { secret = "ops_api.token" }

[feathers.ops_api.body]
template = '{"summary": "[{{.level}}] {{.title}}", "details": "{{.body}}"}'
```

`https` only (unless `allow_http`), no redirects (unless `max_redirects`),
and no connection to loopback, link-local, private or cloud-metadata
addresses unless the configuration allows the network: the address is
checked where the connection is made, so DNS rebinding does not get around
it. `POST` and `PATCH` are not retried unless an idempotency key (or the
policy) allows it. See the [HTTP feather guide](docs/http.md).

### External plugins

A plugin is an executable named `kjak-feather-<name>` on your `PATH`. kjak
runs it for a feather that names it, and only then:

```toml
[feathers.pager]
type = "external"
plugin = "pagerduty"                     # runs kjak-feather-pagerduty
secrets = { routing_key = { secret = "pagerduty.routing_key" } }
```

It reads one JSON request on stdin (the message, its settings, and only the
secrets you give it) and answers one JSON response on stdout (protocol v1).
**A plugin runs with your rights**: install only plugins you trust. kjak
searches absolute `PATH` directories only, refuses ambiguous or writable
executables, re-checks the file before every run, passes a minimal
environment, and kills the plugin at its timeout. `kjak feather plugins`
lists what `PATH` has without running anything. See the
[plugin guide](docs/plugins.md), which also covers writing one.

## Security

- Secrets are references (secrets file, environment, keychain), resolved
  when a send runs, never printed, and removed from every error and report.
- A repository's `.kjak.toml` (and its `.kjak.secrets.toml`) is used only
  after `kjak config trust`; any change withdraws the trust. It reads your
  own secrets, and gives your environment variables to an http feather or a
  plugin, only by the names you allowed.
- http feathers: https only, no redirects, no connection to loopback,
  private, link-local or cloud-metadata addresses unless allowed (checked
  where the connection is made), no proxy.
- Plugins run with your rights: kjak controls which executable runs (absolute
  `PATH` only, checked before every run) and what it gets (a minimal
  environment, only its own secrets), not what it does.

Details: [docs/security.md](docs/security.md). Report a vulnerability
privately: [SECURITY.md](SECURITY.md).

## Known limitations

- Feathers: Slack (incoming webhooks), Discord (webhooks), e-mail (SMTP) and
  HTTP APIs (`http`).
  **OS-native notifications are not supported** (a future feather); a feather
  of type `os` is a configuration error.
- OS keychain: macOS (`security`) and Linux (`secret-tool`, libsecret) are
  read-only; **Windows has no keychain support** (use `env`).
- Groups do not nest.
- kjak rewrites its configuration files without their comments when a
  command (`feather add`, `group set`, ...) changes them.
- Plugins are not sandboxed (they run with your rights); on Windows a
  plugin's child processes are not killed at its timeout.

More in [docs/troubleshooting.md](docs/troubleshooting.md).

## Documentation

- [Getting started](docs/getting-started.md) ([日本語](docs/getting-started_ja.md)): install, Slack, Discord, e-mail, recipes
- [Configuration reference](docs/configuration.md) ([日本語](docs/configuration_ja.md)): every key, CLI management, credentials, trust, delivery behavior
- [CLI usage](docs/cli.md)
- [Credentials, trust and security](docs/security.md)
- [Migrating literal secrets](docs/migration.md)
- [HTTP API / webhook feathers](docs/http.md)
- [External feather plugins](docs/plugins.md) (users and plugin authors)
- [Delivery, retry and acceptance semantics](docs/delivery.md)
- [Development guide](docs/development.md)
- [Release runbook](docs/release.md)
- [Troubleshooting](docs/troubleshooting.md)

## License

MIT
