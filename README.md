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
  ([what v1 guarantees](docs/stability.md))
- One static Go binary (`CGO_ENABLED=0`) for macOS, Linux and Windows

## Install

```sh
brew install magicdrive/tap/kjak
brew upgrade kjak                              # later, to update it
```

Or download an archive from [GitHub Releases](https://github.com/magicdrive/kjak/releases)
(`.tar.gz` for macOS and Linux, `.zip` for Windows) and check it:

```sh
sha256sum -c kjak_checksums.txt --ignore-missing
gh attestation verify kjak_1.0.0_linux_amd64.tar.gz --repo magicdrive/kjak
```

From source (Go 1.27): `git clone https://github.com/magicdrive/kjak.git && cd kjak && make build`
(the binary is `build/kjak`).

Supported platforms: darwin/arm64, darwin/amd64, linux/arm64, linux/amd64,
windows/amd64.

### Install script (macOS and Linux, amd64 and arm64)

Download the script, read it if you like, then run it with `sh`:

```sh
curl -fsSLO https://raw.githubusercontent.com/magicdrive/kjak/main/install.sh
sh install.sh                                  # the latest release, into ~/.local/bin
"$HOME/.local/bin/kjak" --version             # prints: kjak version vX.Y.Z
```

```sh
sh install.sh                                  # install the latest release
sh install.sh --version v0.0.3                 # install a given release (older ones too)
sh install.sh --upgrade                        # switch to the latest stable release
sh install.sh --upgrade --version v0.0.3       # switch to a given release (also a downgrade)
sh install.sh --dir "$HOME/.local/bin"         # choose the directory (this is the default)
sh install.sh --help
```

- **Where**: `$HOME/.local/bin/kjak` by default; `--dir DIR` installs `DIR/kjak` and creates
  `DIR` if needed (spaces in `DIR` are fine). It never uses `sudo` and never changes `PATH` or
  shell start-up files: if the script says the directory is not in `PATH`, add it, e.g.
  `export PATH="$HOME/.local/bin:$PATH"` in `~/.profile`. For a system directory such as
  `/usr/local/bin`, run it as a user who can write there (it does not ask for `sudo` itself).
- **Versions**: without `--version` it installs the latest stable release (pre-releases are not
  picked). `--version` takes a release tag, `v0.0.3` or `0.0.3`, newer or older than the
  installed one; a version that does not exist is an error, never another version. `--upgrade`
  is the same run, named for what you mean. Running it again with the installed version changes
  nothing. Releases from v0.0.2 on can be installed: v0.0.1 has a checksum file named `checksums.txt`, which the script does not use, so it refuses v0.0.1.
- **Updating**: run `sh install.sh --upgrade` again (with the same `--dir` if you used one).
- **Checks**: the archive must match the SHA-256 listed in the release's checksum file
  (`kjak_checksums.txt`); a missing or malformed checksum file stops the install. Only the
  `kjak` binary is taken out of the archive, after its entries have been checked (no absolute
  paths, `..`, links or devices). The new binary must run and report the version asked for
  before it replaces the old one in a single rename: a failed or interrupted run leaves the
  installed `kjak` as it was.
- **Security**: every request starts at github.com, over https, and redirects (GitHub sends the
  downloads to its asset host) are followed only to https. The checksum file comes from the same GitHub release as the archive, so it catches a
  broken or altered download, but not a release that was itself replaced on GitHub. For a
  stronger check, verify the archive's build provenance with the
  [GitHub CLI](https://cli.github.com/) before you trust it:
  `gh attestation verify kjak_<version>_<os>_<arch>.tar.gz -R magicdrive/kjak`
  (releases from v0.0.1 on have these attestations).
  The command above runs the script from the `main` branch; to run a fixed script, download it
  from a release tag instead (`https://raw.githubusercontent.com/magicdrive/kjak/<tag>/install.sh`;
  the tags of the releases after v0.0.4 have it). Piping it straight into `sh` (`curl ... | sh`) also works, but then you
  run what you have not seen.
- **Homebrew**: the script refuses Homebrew's directories (its prefix, e.g. `/opt/homebrew`, or
  `/usr/local` on an Intel Mac) and does not replace a symbolic link such as one Homebrew made.
  Keep one way of installing: with Homebrew, upgrade with `brew upgrade kjak`; the script warns
  if another `kjak` comes first in `PATH`.
- **Uninstall**: delete the file: `rm "$HOME/.local/bin/kjak"` (or `DIR/kjak`).
- **Needs**: a POSIX `sh`, `curl` (or GNU Wget 1.17+; BusyBox wget is refused because it does
  not check certificates, and wget2 is not supported), `tar`, `gzip`, and `sha256sum`, `shasum`
  or `openssl`. A proxy set in `https_proxy` is used. `INSTALLER_TIMEOUT` (seconds, default 60)
  is how long it waits for a connection or more data.
- **Windows**: the script does not run on Windows (Git Bash, MSYS2 and Cygwin are refused). Use
  the `kjak_<version>_windows_amd64.zip` of [GitHub Releases](https://github.com/magicdrive/kjak/releases) (it holds `kjak.exe`). Inside WSL, the Linux instructions apply.
- **Troubleshooting**: *not writable* — choose a directory of your own with `--dir`;
  *not in PATH* — see above; *does not run in DIR (noexec)* — choose a directory on another file
  system; *SHA-256 mismatch* or *rejected* — nothing was installed; try again later and report it
  if it persists; *release not found* — check the version on the
  [Releases page](https://github.com/magicdrive/kjak/releases); *inside the Homebrew prefix* —
  use `brew upgrade kjak`, or another `--dir`.

The [Getting started guide](docs/getting-started.md#2-install) has the details.

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
  where the connection is made), no proxy. A project's slack, discord and
  email feathers get the same address policy.
- Plugins run with your rights: kjak controls which executable runs (absolute
  `PATH` only, checked before every run) and what it gets (a minimal
  environment, only its own secrets), not what it does.

Details: [docs/security.md](docs/security.md). Report a vulnerability
privately: [SECURITY.md](SECURITY.md).

## Known limitations

- Feathers: Slack (incoming webhooks), Discord (webhooks), e-mail (SMTP),
  HTTP APIs (`http`) and plugins (`external`).
  **OS-native notifications are not supported** (a future feather); a feather
  of type `os` is a configuration error.
- OS keychain: macOS (`security`) and Linux (`secret-tool`, libsecret) are
  read-only; **Windows has no keychain support** (use `env`).
- Groups do not nest.
- kjak rewrites its configuration files without their comments when a
  command (`feather add`, `group set`, ...) changes them.
- Plugins are not sandboxed (they run with your rights). kjak ends a plugin
  and every process it started (its process group on Unix, its Job Object
  on Windows) at its timeout, the deadline and Ctrl-C.

More in [docs/troubleshooting.md](docs/troubleshooting.md).

## Documentation

- [Getting started](docs/getting-started.md) ([日本語](docs/getting-started_ja.md)): install, Slack, Discord, e-mail, recipes
- [Configuration reference](docs/configuration.md) ([日本語](docs/configuration_ja.md)): every key, CLI management, credentials, trust, delivery behavior
- [CLI usage](docs/cli.md)
- [Stability and compatibility (v1)](docs/stability.md): what scripts, configurations and plugins may rely on
- [Credentials, trust and security](docs/security.md)
- [Migrating to v1.0.0](docs/migration.md) (from v0.0.4: literal secrets, project trust)
- [HTTP API / webhook feathers](docs/http.md)
- [External feather plugins](docs/plugins.md) (users and plugin authors)
- [Delivery, retry and acceptance semantics](docs/delivery.md)
- [Discord end-to-end check](docs/discord-e2e.md) (manual, with a real webhook)
- [Development guide](docs/development.md)
- [Release runbook](docs/release.md)
- [Troubleshooting](docs/troubleshooting.md)

## License

MIT
