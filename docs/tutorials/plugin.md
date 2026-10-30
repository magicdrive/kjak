# Plugins (external feathers)

[日本語](plugin_ja.md) · [All tutorials](README.md) · [Reference: plugins](../plugins.md)

This tutorial installs a small plugin, `kjak-feather-log`, registers it as
an `external` feather and sends a first notification through it; then it
explains what kjak gives a plugin and expects back (protocol v1), how kjak
stops it, how to pass it secrets, where the trust boundary is, and what
changes on Windows. The full protocol is in the reference,
[plugins.md](../plugins.md).

## 1. Overview

A plugin is an executable named `kjak-feather-<name>` that delivers a
message somewhere kjak does not know. A feather of type `external` names
it:

```toml
[feathers.log]
type = "external"
plugin = "log"                                   # runs kjak-feather-log
settings = { file = "/home/me/kjak-notifications.log" }
```

- kjak runs the plugin **once per attempt**, with no arguments. It writes
  one JSON **request** on the plugin's standard input (the message, the
  feather's `settings`, the secrets you give it) and reads one JSON
  **response** on its standard output (`delivered` or `failed`). That is
  protocol v1.
- kjak decides *which* executable runs (absolute `PATH` directories only,
  file permission checks, optional pinning) and *what it gets* (a minimal
  environment, a fresh working directory, only its own secrets), and it
  stops the plugin and everything it started at the timeout.
- **A plugin is not sandboxed.** It runs with your rights: it can read your
  files, your keys and use your network as you can. Install a plugin as you
  would install any other program.

The plugin of this tutorial, [kjak-feather-log](../examples/plugins/kjak-feather-log),
is a POSIX shell script: it appends each message as one JSON line to the
file named by its `file` setting.

## 2. Prerequisites

- kjak installed (`kjak version`): [getting started](../getting-started.md#2-install).
- macOS, Linux or another Unix with `/bin/sh` and the standard `cat`, `tr`,
  `grep`, `sed` and `date`. The example plugin does not need `jq`.
- A directory for your own programs that is in your `PATH` **as an
  absolute path**, owned by you and not writable by group or others —
  `~/.local/bin` or `~/bin` usually are. kjak never searches an empty or
  relative `PATH` element (`bin`, `.`), so a repository you `cd` into cannot
  provide a plugin.
- **Windows**: a plugin must be an `.exe` file; scripts (`.ps1`, `.cmd`,
  `.bat`, `.sh`) are not run. The shell plugin of this tutorial does not
  work there: build a Go plugin instead (section 7, *Windows*).

## 3. Setup

Install the plugin from a checkout of the kjak repository (the file is
`docs/examples/plugins/kjak-feather-log`):

```sh
mkdir -p ~/.local/bin
install -m 755 docs/examples/plugins/kjak-feather-log ~/.local/bin/kjak-feather-log
```

If `~/.local/bin` is not in your `PATH` yet, add it in your shell's profile
(`~/.profile`, `~/.zshrc`, ...), expanded to an absolute path:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

(A quoted `"~/.local/bin"` stays a literal `~`, which is a relative element
that kjak does not search.)

Check that kjak sees it. `kjak feather plugins` lists the `kjak-feather-*`
files of the `PATH` directories and runs none of them:

```sh
kjak feather plugins
```

```
PLUGIN  PATH                                   STATE  FEATHERS
log     /home/me/.local/bin/kjak-feather-log   ok     -
A plugin runs only for a feather of type "external" that names it (docs/plugins.md).
```

`ok` means the file passed the checks: a regular executable file owned by
you (or root), not writable by group or others, in directories that nobody
else can change. A file that fails them is listed as `refused: <why>`, and
`not searched:` lines name the `PATH` elements kjak skips. Finding a plugin
does nothing by itself: it runs only for a feather that names it.

Plugin names are lower-case ASCII letters, digits, `_` and `-` (at most 48),
and never the name of a built-in type (`slack`, `email`, `http`, `os`, ...).

## 4. Minimal configuration

The complete `~/.config/kjak/config.toml`; put your own absolute path in
`file` (TOML does not expand `~`, and a relative path would be relative to
the plugin's working directory, which is new and removed after each run):

```toml
# ~/.config/kjak/config.toml
[feathers.log]
type = "external"
plugin = "log"
settings = { file = "/home/me/kjak-notifications.log" }
timeout = "5s"
```

| key | meaning |
|---|---|
| `plugin` | the name: kjak runs `kjak-feather-log` from the absolute `PATH` directories |
| `settings` | plain strings passed to the plugin in the request (keys: lower-case letters, digits, `_`) |
| `timeout` | how long one run may take (default `10s`, at most `10m`); then kjak kills it |

## 5. CLI configuration

`kjak feather add` does not create external feathers; it says so:

```
kjak: feather add: an external feather is configured in the file ([feathers.log] type = "external", plugin = "NAME", docs/plugins.md); kjak feather plugins lists what PATH has
```

Write the feather in the file (section 4), then check it with the CLI:

```sh
kjak config validate
kjak feather plugins
kjak feather show log
kjak feather plugins -o json
```

- `config validate` checks the keys (a reserved plugin name, a bad
  `settings` key, `path must be absolute`, ...).
- `feather plugins` now shows `[log]` in the `FEATHERS` column; with `-o
  json` it gives each plugin's path and SHA-256 (for pinning, section 7).
- `feather show log` prints the plugin's name and where its secrets come
  from, never their values.

`kjak feather disable log`, `enable` and `remove` work as for every type.

## 6. First notification

Resolve everything without running the plugin:

```sh
kjak send --to log --dry-run "test"
```

```
plan  log (external)
dry run: 1 target(s), 0 skipped; nothing was sent
```

The dry run finds and checks the executable and resolves the secrets; it
does not start the plugin. Then send:

```sh
kjak send --to log --title "Deploy" --level success "api 1.4.2 is live"
```

```
ok    log (external) delivered in 37ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

The file now has one line per notification:

```
{"time":"2026-10-30T12:16:14Z","idempotency_key":"38e2a504ce9431dd8eaf9620c500b156","message":{"title":"Deploy","body":"api 1.4.2 is live","level":"success","url":""}}
```

`-o json` shows the result as kjak understood the response (`status`,
`acceptance`, the error and its code):

```sh
kjak send --to log -o json "second"
```

### Run the plugin by hand

A plugin is an ordinary program: give it a request on standard input and
read its answer. `env -i` gives it an environment as small as kjak's:

```sh
printf '%s' '{"protocol_version":1,"feather":"log","plugin":"log","attempt":1,"idempotency_key":"k1","message":{"title":"","body":"hello","level":"info","url":""},"settings":{"file":"/tmp/kjak-plugin-test.log"},"secrets":{}}' | env -i PATH=/usr/bin:/bin KJAK_PLUGIN_PROTOCOL=1 ~/.local/bin/kjak-feather-log; echo "exit $?"
```

```
{"protocol_version":1,"status":"delivered"}
exit 0
```

With `"file":"relative.log"` it answers `failed` / `rejected` (`the setting
file must be an absolute path`), and kjak reports that as a failure that
certainly delivered nothing.

## 7. Advanced configuration

### The request (protocol v1)

kjak writes one JSON object on the plugin's standard input, then closes it
(compact, on one line; shown indented here):

```json
{
  "protocol_version": 1,
  "feather": "log",
  "plugin": "log",
  "attempt": 1,
  "deadline": "2026-10-30T12:28:35.742961Z",
  "timeout_ms": 5000,
  "idempotency_key": "1d14879dc6d597eb7a2fdb25d6ce2856",
  "message": {"title": "Deploy", "body": "api 1.4.2 is live", "level": "success", "url": "https://ci.example/7"},
  "settings": {"file": "/home/me/kjak-notifications.log"},
  "secrets": {"token": "dummy-token"}
}
```

- `attempt` counts the attempts of one send; `idempotency_key` is the same
  for every attempt of a send and new for the next send (use it so that a
  retried attempt does not deliver twice).
- `deadline` is when kjak stops the plugin (its `timeout` or the send's
  deadline, whichever comes first); finish before it.
- `message` always has the four strings (empty when not set); `level` is
  `info`, `success`, `warning`, `error` or `critical`.
- `secrets` holds **only** the secrets of the feather's `secrets` table
  (section 8). New fields may be added in v1: ignore what you do not know.

### The response and the exit status

One JSON object on standard output, then exit 0:

```json
{"protocol_version": 1, "status": "delivered"}
```

```json
{"protocol_version": 1, "status": "failed", "acceptance": "rejected", "retryable": true,
 "retry_after_ms": 2000, "status_code": 429,
 "error": {"code": "rate_limited", "message": "too many requests"}}
```

| the plugin | kjak reports |
|---|---|
| exits 0 with `"status": "delivered"` | delivered (`acceptance: confirmed`) |
| exits 0 with `"failed"` and `"acceptance": "rejected"` | failed, certainly not delivered; retried only with `retry.policy = "plugin"` and `"retryable": true` |
| exits 0 with `"failed"` and `"acceptance": "unknown"` (or none) | failed, **may have been delivered** (`????`); never retried |
| exits with any other status, crashes or is killed | failed, may have been delivered, whatever it printed; the end of its stderr is in the error (`the plugin exited with status 3; it may have delivered; stderr: ...`) |
| prints no JSON, two objects, more than 1 MiB, or no/another `protocol_version` | a protocol error, may have been delivered |
| cannot be started | failed, not delivered (nothing ran) |

Say `rejected` only when the message certainly did not arrive; when in
doubt, `unknown`. Standard error is for diagnostics (kjak keeps 64 KiB and
shows its last 300 characters, cleaned and redacted). The `error.code`
values and every field: [plugins.md](../plugins.md#response-standard-output).

### Timeout, cancel and cleanup

| | |
|---|---|
| timeout | the feather's `timeout` (default 10s) and the send's deadline (`--timeout`): kjak kills the plugin, and the result is `the plugin did not finish within its timeout of 5s; it was stopped and may have delivered` |
| cancel | Ctrl-C (SIGINT), SIGTERM or SIGHUP to kjak kills the plugin too; kjak exits 130 and reports it (`interrupted while the plugin ran; ...`) |
| cleanup | on Unix the plugin runs in its own process group and kjak kills the whole group, at a timeout, a cancel and also after the plugin exited: nothing it started stays behind. On Windows the plugin runs in its own Job Object, terminated in the same cases |
| not covered | if kjak itself is killed with SIGKILL (or crashes) on Unix, nobody kills the group; a child that leaves the group (`setsid`) or a job started through another service (`launchd`, `systemd --user`, `at`) survives |
| working directory | a new empty directory for each attempt, writable only by you, removed after it: never the project, never `/tmp` itself |
| environment | only `PATH` (its absolute directories only), `HOME`, `TMPDIR`, `TEMP`, `TMP`, `LANG`, `LC_ALL`, `LC_CTYPE`, `TZ`, `SYSTEMROOT`, `USERPROFILE` (those that are set), the names in the feather's `env`, and `KJAK_PLUGIN_PROTOCOL=1` |
| concurrency | different feathers run at the same time, so a plugin may run several times at once; one feather's attempts never overlap |

A child that you start in the background must not keep the plugin's
standard output or error open (`cmd >/dev/null 2>&1 &`): kjak reads them
until they close, at most 2 seconds after the plugin exits.

### A complete configuration

```toml
# ~/.config/kjak/config.toml
[feathers.log]
type = "external"
plugin = "log"
# pin the executable: this file only, with this content (kjak feather plugins -o json shows both)
path = "/home/me/.local/bin/kjak-feather-log"
sha256 = "85abe0775af1f9fb009b8743c73b1dcab8e600c937abc7d3ccc91bc9c77700c8"
settings = { file = "/home/me/kjak-notifications.log" }
env = ["TZ"]                       # variables it may see besides the minimal set
timeout = "5s"
levels = ["warning", "error", "critical"]

[feathers.log.retry]
policy = "plugin"                  # retry a response "rejected" + "retryable"
max_attempts = 3

[feathers.pager]
type = "external"
plugin = "pagerduty"
settings = { service = "api", severity_map = "error=high" }
secrets = { routing_key = { secret = "pagerduty.routing_key" } }
timeout = "5s"

[groups.alerts]
feathers = ["log", "pager"]
```

- **Pinning**: with `path`, kjak does not search `PATH`; with `sha256`, it
  hashes the file before every run and refuses one whose content changed.
  Without them, kjak still records the file's identity when the feather is
  built and refuses a file replaced before the run. After an upgrade of a
  pinned plugin, update `sha256` (`shasum -a 256 FILE`, `sha256sum FILE`).
- **Retries**: by default (`retry.policy = "never"`) a plugin runs once per
  send, because starting it may already have effects. With `"plugin"`, a
  `rejected` + `retryable` response is retried by the engine (backoff,
  `retry_after_ms`, the deadline, `max_retries`), with `attempt` incremented
  and the same `idempotency_key`.
- **`env`** cannot name `KJAK_*`, `LD_*`, `DYLD_*`, `GODEBUG`,
  `GOTRACEBACK`, nor a variable that is the source of one of the feather's
  secrets (the secret comes in the request instead).

### Writing your own

Read [kjak-feather-log](../examples/plugins/kjak-feather-log) as a
template. It reads the whole request before acting, checks
`protocol_version`, validates its setting (an absolute path, no JSON
escapes), opens the file — the last step that cannot have written anything,
so a failure there is `rejected` — and only then writes; a failed write is
`unknown`. It never writes the request itself, so the secrets of the request
never reach the log. Its JSON parsing is done with `sed` patterns that
respect JSON strings, enough for the values it needs; a plugin that needs
more should use a real JSON parser (a Go program, Python, or `jq`).
More guidelines: [writing a plugin](../plugins.md#writing-a-plugin).

### Windows

On Windows a plugin is `kjak-feather-<name>.exe`: kjak refuses any other
file (`not an .exe file`) and does not run scripts. Build the Go plugin of
[plugins.md](../plugins.md#writing-a-plugin) (standard library only; it
also writes the message to the file of the setting `file`) under the name
the feather uses:

```powershell
go build -o kjak-feather-log.exe .
```

Put it in a directory of your `PATH` under your profile (for example
`%LOCALAPPDATA%\Programs\kjak-plugins`) or under `Program Files`. A folder
made directly under `C:\` inherits `Authenticated Users: Modify`, and kjak
refuses a plugin there. The file and its directories must be owned by you,
SYSTEM, Administrators or TrustedInstaller, with no other principal allowed
to change them. The configuration is the same, with a Windows path in
`settings` (`file = 'C:\Users\me\kjak-notifications.log'`, a TOML literal
string so that the backslashes stay).

## 8. Secrets

A plugin gets a secret only when its feather names it in `secrets`, under
the name the plugin knows it by:

```toml
# ~/.config/kjak/config.toml
[feathers.pager]
type = "external"
plugin = "pagerduty"
settings = { service = "api" }
secrets = { routing_key = { secret = "pagerduty.routing_key" }, api_token = { env = "PAGERDUTY_TOKEN" } }
```

```toml
# ~/.config/kjak/secrets.toml (mode 0600; dummy value)
[pagerduty]
routing_key = "R0ut1ngK3y-replace-me"
```

- Each value is a credential reference (`secret`, `env` or `keychain`),
  resolved when the send runs and put in the request's `secrets` object:
  `{"routing_key": "...", "api_token": "..."}`.
- kjak never puts a secret in the plugin's environment or arguments, and
  never gives it anything else of your secrets files, environment or
  keychain.
- kjak redacts the secrets it passed from the plugin's messages and stderr
  before they reach a report — but not what the plugin derives from them
  (an encoded form, a part). **A plugin must never print a secret.**
- A secret that does not resolve stops the send before the plugin runs
  (exit 2).
- The example `kjak-feather-log` takes no secret; if you give it one, it
  ignores it and never writes it.

## 9. Troubleshooting

| message | cause | fix |
|---|---|---|
| `plugin error: kjak-feather-log is not on PATH (absolute directories only)` (exit 2) | not installed, or only in a relative or empty `PATH` element | install it in an absolute `PATH` directory (section 3), or set `path =` |
| `not searched: PATH element "bin" (a relative directory)` (`feather plugins`) | a relative `PATH` element | use absolute directories in `PATH` |
| `plugin error: ...kjak-feather-log: writable by group or others (mode 0775)` | the file (or a directory) could be changed by someone else | `chmod 755` the file, `chmod go-w` the directory |
| `... is found in several PATH directories as different files` | two installations | remove one, or pin `path =` |
| `plugin name "slack" is reserved` | a plugin named like a built-in type | rename the plugin |
| `feather add: an external feather is configured in the file` | `feather add --type external` | write `[feathers.NAME]` in the file (section 4) |
| `plugin: the setting file must be an absolute path` | the log plugin refused a relative `file` | give an absolute path |
| `the plugin did not finish within its timeout of 5s; it was stopped and may have delivered` (`????`) | the plugin is too slow, or waits for input | raise `timeout`; check the destination before sending again |
| `the plugin exited with status 3; it may have delivered; stderr: ...` | the plugin failed or crashed | read its stderr in the message; fix the plugin |
| `protocol error: no response on stdout; the plugin may have delivered` | it printed nothing, or not one JSON object | answer one JSON object (section 7); keep diagnostics on stderr |
| `plugin error: ... has SHA-256 ..., the configuration pins ...` (exit 2) | the file does not match `sha256` (an upgrade, or another file) | check the file; update `sha256` after an upgrade |
| `the plugin was not run: ... changed since it was checked` (`blocked`) | the file was replaced between the check and the run | find out what replaced it; run again |
| `its plugin log is not the executable that was trusted` (`untrusted_project`) | a project's plugin was upgraded or replaced | review it, then `kjak config trust` |
| `not an .exe file` (Windows) | a script or a file without `.exe` | build an `.exe` (section 7, *Windows*) |

More: [troubleshooting](../troubleshooting.md).

```sh
kjak feather plugins
kjak config validate
kjak send --to log --dry-run "test"
kjak send --to log -o json "test"
```

## 10. Security notes

- **No sandbox.** A plugin runs with your rights and can do anything you
  can. kjak controls which file runs and what it is given, and it stops the
  plugin's processes at the end of the run; it does not limit what the
  plugin does meanwhile.
- **Discovery is not authorization.** A `kjak-feather-*` file on `PATH` runs
  only for a feather that names it; `kjak feather plugins` runs nothing.
  `type = "slack"` (or any built-in type) always runs the built-in feather.
- **PATH hijack.** Only absolute `PATH` directories are searched; a name
  found twice as different files is an error; a refused candidate is an
  error, never skipped for a later one.
- **Projects are pinned by trust.** A `.kjak.toml` may register external
  feathers, but `kjak config trust` resolves each plugin, shows its path,
  SHA-256, settings, secrets and environment variables, and records the
  path and SHA-256. A run then refuses the feather (`untrusted_project`) if
  the plugin resolves to another file or its content changed; an upgrade
  needs a new `kjak config trust`. The plugin must be installed when you
  trust. The `env` and `keychain` secrets of a project's plugin, and its
  `env` variables, must be allowed by name.
- **Settings are input.** A project's `settings` come from the repository:
  a plugin must validate them (the example refuses a relative or escaped
  path).
- **TOCTOU limit.** kjak checks the file and then executes it by path; a
  process that could replace it in that instant would need write access
  that the checks keep to you and root.
- Details: [security: external plugins](../security.md#external-plugins) and
  [per OS](../security.md#plugin-processes-and-files-per-os).

## 11. Related references

- [Plugins](../plugins.md) — protocol v1, naming, discovery, the process (the reference of this tutorial)
- [Protocol v1](../plugins.md#protocol-v1) and [the process](../plugins.md#the-process)
- [Projects and trust](../plugins.md#projects)
- [Configuration: external](../configuration.md#external) — the table of keys
- [Security: external plugins](../security.md#external-plugins)
- [Example: plugin-only.toml](../examples/plugin-only.toml) and [kjak-feather-log](../examples/plugins/kjak-feather-log)
- [Troubleshooting](../troubleshooting.md)
