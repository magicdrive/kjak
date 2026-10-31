# External feather plugins

[Configuration](configuration.md#external) · [Security](security.md#external-plugins)

A plugin is an executable, `kjak-feather-<name>`, that delivers a message
somewhere kjak does not know. kjak runs it once per attempt, writes a JSON
request on its standard input and reads a JSON response on its standard
output (protocol v1, below).

> **A plugin runs with your rights.** It is arbitrary code: it can read your
> files, your keys and your network as you can. kjak decides *which*
> executable runs and *what it is given*; it does not sandbox what the
> plugin does. Install plugins you would install as any other program.

## Using a plugin

1. Install the executable in a directory of your `PATH` (an absolute
   directory: see [Discovery](#discovery)).
2. Register a feather for it — finding it on `PATH` does nothing by itself:

   ```toml
   [feathers.pager]
   type = "external"
   plugin = "pagerduty"                 # runs kjak-feather-pagerduty
   settings = { service = "api", severity_map = "error=high" }
   secrets = { routing_key = { secret = "pagerduty.routing_key" } }
   timeout = "5s"
   ```

3. `kjak feather plugins` (what `PATH` has, nothing run), `kjak config
   validate`, `kjak send --to pager --dry-run test` (the executable is found
   and checked, the secrets resolved, nothing run), then a real send.

Pin the executable when you want no doubt about which one runs:

```toml
path = "/opt/homebrew/bin/kjak-feather-pagerduty"     # no PATH search
sha256 = "9f2c...e1"                                  # its content
```

## Naming

| rule | why |
|---|---|
| `kjak-feather-<name>`, `.exe` on Windows | one prefix: discovery never mistakes another program for a plugin |
| `<name>`: lower-case ASCII letters, digits, `_`, `-`; at most 48; starting with a letter or digit | the same name on every file system: no case folding (`PagerDuty` vs `pagerduty` on macOS/Windows), no Unicode look-alikes or normalization forms |
| not `slack`, `discord`, `email`, `http`, `external`, `os` (and the other OS names), `kjak`, `builtin`, `plugin`, `plugins` | a plugin is never mistaken for, and never replaces, a built-in type: `type = "slack"` always runs the built-in feather, whatever `PATH` has |

A file with a refused name is listed by `kjak feather plugins` as refused,
and never runs.

## Discovery

`kjak feather plugins [-o json]` lists the `kjak-feather-*` files of the
`PATH` directories, with their state, the feathers that use them and the
`PATH` elements that are not searched. Nothing is executed.

When a feather runs, its plugin is found like this:

- `path = ...` set: that file only (it must be absolute and named
  `kjak-feather-<plugin>`).
- Otherwise, the **absolute** `PATH` directories, in order. An empty element
  (which means the current directory to a shell) and a relative one
  (`bin`, `.`) are **not searched**: a repository you `cd` into cannot
  provide a plugin.
- The same name in two directories as two different files is an error
  (which one runs is not left to the order of `PATH`): pin `path`. The same
  file twice (a directory listed twice, a link) is fine.
- A candidate that is refused is an error, never skipped for a later one.

The file — symbolic links resolved (a Homebrew link runs its Cellar file) —
must be:

| check (Unix) | |
|---|---|
| a regular file, executable | |
| owned by root or by the user running kjak | |
| not writable by group or others | someone else could replace its content |
| the `PATH` directory that holds the name, and the directory of the file a link leads to: owned by root or the user, not writable by group or others (sticky or not) | someone else could put a file, or a link to any program, there |
| every directory above those: owned by root or the user, writable by group or others only when sticky (`/tmp`) | someone else could rename the directory away |
| a `PATH` entry that is a symbolic link: the link owned by root or the user | |
| a FIFO or a device named like a plugin is refused without waiting | |

**Homebrew (macOS).** Homebrew's `bin`, `Cellar` and `opt` are writable by
the `admin` group (mode 775), so the checks above refuse a plugin installed
with `brew` (`kjak feather plugins` shows `Homebrew: runs only for a feather
with sha256 = "..."`). Pin its SHA-256 and it runs:

```toml
[feathers.mac]
type = "external"
plugin = "notify-macos"
sha256 = "3276acaa..."      # kjak feather plugins shows it; update it after brew upgrade
```

For a feather of a project, `kjak config trust` records the path and the
SHA-256 instead. Only the standard prefixes (`/opt/homebrew`, `/usr/local`)
and a plugin in a keg (`<prefix>/Cellar/<formula>/<version>/bin/kjak-feather-<name>`)
get this exception, and only from the group-write rule; the plugin must be a
Mach-O that loads system libraries only (not a script, not a program linked
to a library of Homebrew). Check the SHA-256 against what you installed
before you pin it. The trust boundary this opens to other admin users and
the TOCTOU risk that remains are in [security.md](security.md#external-plugins).

The ACLs are checked too (internal/fileacl): on Linux the POSIX ACL and on
macOS the ACL of the file and of those directories must not let anyone else
change them (macOS: no allow entry with a write right, whoever it names),
nor remove an entry of a directory above them.

On Windows: a regular `.exe` file owned by you, SYSTEM, Administrators or
TrustedInstaller, that no other principal may change (its DACL); the
directories that hold it likewise (no other principal may add, change or
delete an entry); every directory above them owned the same way, and no
other principal may delete an entry or change its DACL or owner (adding an
entry is allowed: `Users` may create folders in `C:\`). A plugin in a
folder made directly under `C:\` inherits `Authenticated Users: Modify` and
is refused: install it under your profile or `Program Files`. A distribution whose `/usr/local/bin` is group-writable
(the old Debian `staff` setup) needs `path =` to a plugin elsewhere, or a
directory without group write.

kjak records the file's identity (device and inode, size, modification
time) and SHA-256 when the feather is built, and checks the identity again
just before every run (the SHA-256 too when it is pinned, by `sha256` or by
a project's trust). A changed file is not run (`error.code: blocked`).
**Limit:** the file is executed by its path after that check; a process
that can replace it in that instant (it would need write access to the file
or its directory, which the checks above keep to root and you, and for a
plugin installed by Homebrew also to the other admin users: below) could
still win the race. kjak does not copy or `fexecve` the executable.

## Projects

A project file (`.kjak.toml`) may register external feathers. `kjak config
trust` resolves each plugin with your `PATH`, shows its path and SHA-256 and
records them in the trust entry; the plugin must be installed to trust the
project. Afterwards a run refuses the feather (`untrusted_project`, exit 2)
if the plugin resolves to another file or its content changed — an upgrade
needs a new `kjak config trust`. The trust also shows the secrets, the
environment variables and the settings the project gives the plugin; the
`env` and `keychain` sources of its secrets and every variable of its
`env` list must be allowed by name (a second question, or
`--allow-user-secrets` with `--yes`): a project could otherwise hand your
`AWS_SECRET_ACCESS_KEY` to a plugin that sends it wherever its settings
say.

## Protocol v1

### Request (standard input)

One JSON object, then standard input is closed.

```json
{
  "protocol_version": 1,
  "feather": "pager",
  "plugin": "pagerduty",
  "attempt": 1,
  "deadline": "2026-10-27T09:00:05.25Z",
  "timeout_ms": 5000,
  "idempotency_key": "4f1c0e9d2b7a4c35a8e1f0b2c3d4e5f6",
  "message": {"title": "Deploy", "body": "api 1.4.2 is live", "level": "success", "url": "https://ci.example/7"},
  "settings": {"service": "api", "severity_map": "error=high"},
  "secrets": {"routing_key": "R0ut1ngK3y..."}
}
```

| field | |
|---|---|
| `protocol_version` | `1` |
| `feather`, `plugin` | the feather's name and the plugin's |
| `attempt` | 1, 2, ... within one send |
| `deadline` | UTC: when kjak stops the plugin (the feather's `timeout` or the send's deadline, whichever comes first) |
| `timeout_ms` | the feather's `timeout` |
| `idempotency_key` | random, **the same for every attempt of one send**, new for the next send: use it to deliver once |
| `message` | `title`, `body`, `level` (`info`, `success`, `warning`, `error`, `critical`), `url`; empty strings when not set |
| `settings` | the feather's `settings` (plain strings) |
| `secrets` | **only** the secrets of the feather's `secrets` table, resolved, by the names given there. Never anything else of the secrets files, the environment or the keychain |

New fields may be added to the request in v1: ignore what you do not know.

### Response (standard output)

One JSON object, then exit with status 0.

```json
{"protocol_version": 1, "status": "delivered", "acceptance": "confirmed", "status_code": 202}
```

```json
{"protocol_version": 1, "status": "failed", "acceptance": "rejected", "retryable": true,
 "retry_after_ms": 2000, "status_code": 429,
 "error": {"code": "rate_limited", "message": "too many requests"}}
```

| field | required | meaning |
|---|---|---|
| `protocol_version` | yes | `1`; any other value is a protocol error ("unsupported protocol_version") |
| `status` | yes | `delivered` or `failed` |
| `acceptance` | no | with `delivered`: `confirmed` (the default). With `failed`: `rejected` (certainly not delivered: nothing was sent, or the provider refused) or `unknown` (the default: it may have arrived) |
| `retryable` | no | with `rejected`: a later attempt may succeed. kjak retries only with `retry.policy = "plugin"` |
| `retry_after_ms` | no | the wait the provider asked for (capped at 24h) |
| `status_code` | no | the provider's status, shown in the report |
| `error.code` | no | `rejected`, `rate_limited`, `server_error`, `network`, `protocol`, `internal`; any other becomes `rejected` |
| `error.message` / `message` | no | a short text for the report (control characters removed, cut at 300 characters, secrets redacted) |

Unknown fields are ignored. Be honest about `acceptance`: say `rejected` only
when the message certainly did not arrive. When in doubt, `unknown`: kjak
never retries it, so nothing is delivered twice.

### Standard error, exit status

- Standard error is for diagnostics. kjak keeps the first 64 KiB, and puts
  its last 300 characters — cleaned and redacted — in the error of a failed
  attempt.
- Exit status 0: the response decides. **Any other exit status** (or a
  crash, a signal) is a failure with acceptance `unknown`, whatever was
  written on stdout: the plugin ran and may have delivered.
- No response, a response that is not one JSON object, more than 1 MiB on
  stdout, a missing or unsupported `protocol_version`: a protocol error,
  acceptance `unknown`.
- A plugin that cannot be started (gone, not executable): `rejected`, not
  retried (nothing ran).

### The process

| | |
|---|---|
| arguments | none (only the program's path as `argv[0]`) |
| environment | only `PATH`, `HOME`, `TMPDIR`, `TEMP`, `TMP`, `LANG`, `LC_ALL`, `LC_CTYPE`, `TZ`, `SYSTEMROOT`, `USERPROFILE` (those that are set), the names in the feather's `env`, and `KJAK_PLUGIN_PROTOCOL=1`. kjak puts no secret there. `env` cannot name `KJAK_*`, `LD_*`, `DYLD_*`, `GODEBUG`, `GOTRACEBACK`, nor a variable that is the source of one of the feather's secrets (it is in the request). `PATH` keeps only its absolute directories (an empty or relative element would find commands in the working directory) |
| working directory | a new empty directory that only you can write to, made for the attempt (in the temporary directory) and removed after it — not the project, not a shared directory such as `/tmp` |
| standard input | the request, then closed |
| timeout | the feather's `timeout` (default 10s) and the send's deadline; then kjak kills the plugin. Unix: the plugin runs in its own process group and the whole group is killed — at a timeout, at the deadline, at SIGINT, SIGTERM or SIGHUP, and after the plugin exits (nothing it started stays). If kjak itself is killed with SIGKILL (or crashes), nobody kills the group: the plugin and its children run on until they end by themselves (on Windows the Job Object ends them). Windows: the plugin runs in a Job Object of its own (created suspended, put in the job, then resumed; no breakaway) and the whole job is terminated in the same cases ([security](security.md#plugin-processes-and-files-per-os)) |
| after a kill | a plugin killed after it started has acceptance `unknown` |
| concurrency | feathers run at the same time (`--concurrency`), so a plugin may run several times at once (for several feathers); one feather's attempts never overlap |

### Retries

By default (`retry.policy = "never"`) a plugin runs once per send: starting a
process may itself have effects kjak cannot see. With `retry.policy =
"plugin"`, a response `rejected` + `retryable` is retried by the engine
(backoff, `Retry-After`, the deadline, `max_retries`), with `attempt`
incremented and the same `idempotency_key`; `retry.max_attempts` caps it.
`unknown` is never retried.

## Writing a plugin

A minimal plugin in Go, standard library only:

```go
// kjak-feather-echo: writes the message to a file named by the setting "file".
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	ProtocolVersion int               `json:"protocol_version"`
	IdempotencyKey  string            `json:"idempotency_key"`
	Message         map[string]string `json:"message"`
	Settings        map[string]string `json:"settings"`
	Secrets         map[string]string `json:"secrets"`
}

func reply(v map[string]any) {
	v["protocol_version"] = 1
	json.NewEncoder(os.Stdout).Encode(v)
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil || req.ProtocolVersion != 1 {
		// nothing was sent: say so
		reply(map[string]any{"status": "failed", "acceptance": "rejected",
			"error": map[string]any{"code": "protocol", "message": "unexpected request"}})
		return
	}
	f, err := os.OpenFile(req.Settings["file"], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		reply(map[string]any{"status": "failed", "acceptance": "rejected",
			"error": map[string]any{"code": "rejected", "message": "cannot open the file"}})
		return
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s [%s] %s\n", req.IdempotencyKey, req.Message["level"], req.Message["body"]); err != nil {
		// part of it may be there
		reply(map[string]any{"status": "failed", "acceptance": "unknown",
			"error": map[string]any{"message": "write failed"}})
		return
	}
	reply(map[string]any{"status": "delivered"})
}
```

```sh
go build -o ~/bin/kjak-feather-echo .     # ~/bin in PATH (absolute)
kjak feather plugins
```

```toml
[feathers.log]
type = "external"
plugin = "echo"
settings = { file = "/tmp/kjak-notifications.log" }
```

A shell plugin works as well (`#!/bin/sh`, reading stdin with `jq`), as
long as it answers one JSON object and exits 0.

Guidelines:

- Read the whole request before acting; finish before `deadline`.
- Use `idempotency_key` with providers that support one, so a retried
  attempt does not deliver twice.
- Never print a secret, on stdout or stderr. kjak redacts the secrets it
  passed, but not what you derive from them.
- Do not leave processes behind: kjak kills the process group on Unix and
  the Job Object on Windows. (kjak kills the group once more after the plugin exited; if
  the group is gone by then, the kill finds nothing — the identifier of a
  group that ended could in theory have been reused in that instant.)
- A child you start in the background must not keep your standard output
  or error open (`cmd >/dev/null 2>&1 &`): kjak reads them until they close
  (at most 2 seconds after you exit), and a pipe still open then makes the
  response unreadable (`failed`, acceptance `unknown`, a protocol error).
- Write the response keys in lower case, as in the table above.
- Do not rely on files in the working directory: it is new and empty for
  each attempt, and removed after it.
- Treat `settings` as input you validate: on a project, they come from the
  repository.

## Testing a plugin

```sh
echo '{"protocol_version":1,"feather":"t","plugin":"echo","attempt":1,"idempotency_key":"k",
  "message":{"title":"","body":"hello","level":"info","url":""},"settings":{"file":"/tmp/x"},"secrets":{}}' \
  | env -i PATH="$PATH" KJAK_PLUGIN_PROTOCOL=1 ./kjak-feather-echo; echo "exit $?"
kjak send --to log --dry-run test       # found, checked, nothing run
kjak send --to log -o json test         # the report: status, acceptance, error
```

kjak's own tests build a plugin with `go build`
(`internal/feather/external/testdata/plugin`) and run it through every case
above: responses, malformed output, huge output, timeouts, cancellation,
crashes, exit statuses, process groups, the environment and the secrets it
receives.
