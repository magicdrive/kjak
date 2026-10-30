# OS notifications (`type = "os"`)

[Tutorial](tutorials/os.md) · [Configuration](configuration.md) · [Design](design/os-notifications.md)

An `os` feather (since v1.1.0) shows the message as a notification of the
operating system kjak runs on. It needs no account and no secret.

```toml
[feathers.desktop]
type = "os"
```

```sh
kjak feather add desktop --type os
kjak send --to desktop --title "Build complete" --level success "All tests passed"
```

## Keys

An `os` feather takes the keys every feather takes and no other:

| key | meaning |
|---|---|
| `type` | `"os"` |
| `enabled` | `false` leaves it out of every send (default `true`) |
| `levels` | the levels it accepts (default: all) |

A webhook, e-mail, http or plugin key, `allow_networks` or `allow_hosts` is a
configuration error, and so are `timeout` and `retry` (the send's
`--timeout` applies). `kjak feather add NAME --type os` takes only
`--levels`, `--disabled`, `--scope` and `--force`. The names `native`,
`macos`, `linux` and `windows` are not types: they are refused with a
message that names `os`.

## What is shown

| | macOS | Windows | Linux |
|---|---|---|---|
| title | the message's title, else `kjak` | the same | the title and the level (`Deployed · success`) |
| second line | the level (`success`, `error`, ...) | the body | the body |
| third line | the body and the URL | the level | |
| level | as text | as text | `error` and `critical` are `critical` urgency, the others `normal` |
| shown as from | Script Editor (usually) | Windows PowerShell | kjak (`--app-name=kjak`) |

The body is followed by the message's URL on its own line. Control
characters other than line breaks become spaces, text that is not UTF-8 is
replaced (U+FFFD), the title is shortened to 256 characters and the body to
4000 (the OS shortens them further). There is no sound and no action: a
click opens nothing.

## How kjak shows it

kjak is a single unsigned executable, not an app that the notification
services of macOS and Windows know, so it asks a program of the OS itself
to post the notification. The text is data, never code: the script is a
constant of kjak, and the text reaches it as JSON on standard input
(macOS), as base64 in an environment variable (Windows) or as arguments
after `--` (Linux). [The design](design/os-notifications.md) compares the
alternatives.

| OS | program | needs |
|---|---|---|
| macOS (amd64, arm64) | `/usr/bin/osascript -l JavaScript` (`display notification`) | a GUI session: `launchctl managername` must say `Aqua` |
| Windows (amd64) | `%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe` (Windows PowerShell 5.1) and the WinRT toast API, under the application identity (AppUserModelID) of Windows PowerShell | an interactive desktop session; notifications of Windows PowerShell allowed |
| Linux | `notify-send` (libnotify) found on `PATH` | a D-Bus session (`DBUS_SESSION_BUS_ADDRESS`, or `$XDG_RUNTIME_DIR/bus`) and a notification server (every desktop has one) |

On Windows kjak does not check the session itself: without an interactive
desktop (SSH, a service, a scheduled task run whether or not you are logged
on), what happens is what Windows PowerShell reports — an error
(`unavailable`), or an accepted toast that nobody sees. Run kjak in your
desktop session there.

kjak changes nothing on the system: no shortcut, no registry key, no
permission. Each send runs the program once, under the send's deadline;
Ctrl-C or the deadline kills it.

## Results

| the OS | status | acceptance | `error.code` | exit |
|---|---|---|---|---|
| accepted the notification | `delivered` | `confirmed` | | 0 |
| kjak has no OS notifications for this system (FreeBSD, ...) | `failed` | `rejected` | `unsupported` | 1 |
| cannot show one here: no GUI session (macOS) or D-Bus session (Linux) — SSH, a service, CI —, the program missing (`notify-send`, Windows PowerShell), or failing | `failed` | `rejected` | `unavailable` | 1 |
| reports the notifications of the program off (Windows: the setting, e.g. `DisabledForUser`) | `failed` | `rejected` | `rejected` | 1 |
| the send was interrupted or its deadline passed while the program ran | `failed` | `unknown` | `canceled` / `deadline` | 130 / 1 |

An `os` feather is never retried: a failure is the machine's, and a retry
would meet the same machine. Like every feather, it is delivered on its
own: one that fails does not stop the others of the send (outcome
`partial`). A dry run (`--dry-run`) does not run the OS program.

**`confirmed` means that the OS program accepted the notification, not
that anyone saw it.** It can still stay off the screen: Focus or Do Not
Disturb, the notification settings of the program it is shown as from
(Script Editor, Windows PowerShell), a locked screen, or a notification
server that drops it. macOS and Linux do not tell kjak about their
settings; Windows does (the `rejected` row above).

## Platforms

| platform | support | checked by |
|---|---|---|
| macOS amd64, arm64 | yes, in a GUI session | unit tests on every OS; CI (macos-latest, macos-15-intel) runs osascript once |
| Windows amd64 | yes, in a desktop session | unit tests; CI (windows-latest) runs Windows PowerShell once |
| Linux amd64, arm64 | yes, with a D-Bus session and `notify-send` | unit tests; CI runners have no desktop session (`unavailable` is checked) |
| other systems | no: `unsupported` | |

CI checks that the OS program accepts the script, not that a notification
appeared on a screen: no CI runner has someone looking at it.

## Security

The title and the body may come from anywhere (a commit message, a log):
they are never part of a script or a command line that a shell or an
interpreter parses. kjak runs the OS program by its absolute path on macOS
and Windows (`SystemRoot` is read from the environment, which kjak trusts
like its configuration), and `notify-send` from `PATH` on Linux. An `os`
feather of a project file (`.kjak.toml`) can only show notifications on your
screen: it reads no secret and makes no connection.
