# OS notifications (macOS, Windows, Linux)

[日本語](os_ja.md) · [All tutorials](README.md) · [Reference: os.md](../os.md) · [Configuration](../configuration.md)

This tutorial sets up an `os` feather (new in v1.1.0): kjak shows the
message as a notification of the operating system it runs on. It covers
macOS, Windows and Linux, the OS settings that decide whether you actually
see the notification, and what to check when nothing appears.

## 1. Overview

An `os` feather needs no account, no webhook and no secret:

```toml
[feathers.desktop]
type = "os"
```

kjak is a single unsigned executable, not an app the notification services
of macOS and Windows know, so it asks a program of the OS itself to post the
notification:

| OS | program kjak runs | shown as from |
|---|---|---|
| macOS | `/usr/bin/osascript` (JavaScript for Automation, `display notification`) | usually **Script Editor** |
| Windows | Windows PowerShell 5.1 (`powershell.exe`) and the WinRT toast API, with the application identity (AppUserModelID) of Windows PowerShell | **Windows PowerShell** |
| Linux | `notify-send` (libnotify) over the D-Bus session | kjak (`--app-name=kjak`) |

The text of the message is data, never code: it is not part of a script or a
command line that a shell parses. kjak changes nothing on the system (no
shortcut, no registry key, no permission).

**What "delivered" means here.** kjak reports `acceptance: confirmed` when
the OS program **accepted** the notification. It does not mean that the
notification appeared on the screen or that anyone saw it: Focus or Do Not
Disturb, the notification settings of Script Editor or Windows PowerShell, a
locked screen or the notification server can still keep it out of sight.
macOS and Linux do not tell kjak about those settings; Windows does when the
notifications of Windows PowerShell are turned off (section 9).

## 2. Prerequisites

- kjak v1.1.0 or later (`kjak version`). Installation: [getting started](../getting-started.md#2-install).
- A **desktop session** on the machine that runs kjak — you are logged in at
  its screen, and kjak runs in that session (a terminal of the desktop, a
  script started from it):

| OS | requirement | not possible |
|---|---|---|
| macOS (amd64, arm64) | a GUI (Aqua) session: `launchctl managername` prints `Aqua` | over SSH, from a launchd daemon, a CI job outside the logged-in session |
| Windows (amd64) | an interactive desktop session; Windows PowerShell 5.1 at `%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe` (part of Windows 10 and 11) | a service, a scheduled task that runs whether or not you are logged on, a session without a desktop |
| Linux | a D-Bus session (`DBUS_SESSION_BUS_ADDRESS`, or `$XDG_RUNTIME_DIR/bus`), `notify-send` on `PATH`, and a notification server (every desktop environment has one) | SSH without the desktop's session bus, cron, containers, CI |

PowerShell 7 (`pwsh`) is not used: WinRT is not available there. kjak runs
Windows PowerShell 5.1 by its absolute path.

On Linux, install `notify-send` if it is missing: the package is
`libnotify-bin` on Debian and Ubuntu, `libnotify` on Fedora and Arch.

## 3. Setup

There is nothing to create on a service. Check the OS side once, so that an
accepted notification is also shown.

### macOS

1. Check the session in the terminal you will use:

   ```sh
   launchctl managername        # Aqua: notifications can be shown
   ```

   `Background` or another value (an SSH session, for example) means
   kjak's notifications cannot be shown from there.
2. Open **System Settings → Notifications**. Notifications that kjak posts
   through `osascript` are usually listed under **Script Editor** (kjak
   cannot choose the name; it is not verified on every macOS version). The
   entry may appear only after the first notification: send one (section 6),
   then come back.
3. In the Script Editor entry, turn on **Allow notifications** and choose
   the style: **Banners** (disappear by themselves) or **Alerts** (stay until
   dismissed). With **None**, the notification goes to Notification Center
   only.
4. Check **Focus** (Control Center, or System Settings → Focus): a Focus mode
   such as Do Not Disturb silences notifications unless Script Editor is an
   allowed app.

### Windows

1. Open **Settings → System → Notifications** and check that
   **Notifications** is on.
2. In the list of apps, find **Windows PowerShell** and turn it on. kjak's
   notifications are shown as from Windows PowerShell, because kjak borrows
   its application identity. The entry may appear only after the first
   notification. Turning it off also turns off the notifications of your
   own PowerShell scripts that use the same identity.
3. Check **Do not disturb** (Windows 11) or **Focus assist** (Windows 10):
   while it is on, notifications go to the notification center without a
   banner.
4. An administrator may turn notifications off with Group Policy; kjak then
   reports the setting (section 9).

### Linux

1. Check the session and the program in the terminal you will use:

   ```sh
   echo "$DBUS_SESSION_BUS_ADDRESS"   # set in a desktop session
   command -v notify-send              # the libnotify client
   ```

2. Check the desktop's notification settings (GNOME: Settings →
   Notifications, with **Do Not Disturb**; KDE Plasma: System Settings →
   Notifications). Without a desktop environment, run a notification server
   (dunst, mako, ...).

## 4. Minimal configuration

The configuration file is `~/.config/kjak/config.toml` (`$XDG_CONFIG_HOME/kjak/config.toml`;
on Windows `%AppData%\kjak\config.toml`). `kjak config path` shows the file
kjak uses. Copy this whole file:

```toml
# ~/.config/kjak/config.toml
[feathers.desktop]
type = "os"
```

To send without `--to`, make it the default target:

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["desktop"]

[feathers.desktop]
type = "os"
```

Check it:

```sh
kjak config validate
kjak feather show desktop
```

An `os` feather takes `type`, `enabled` and `levels` only. A webhook, e-mail,
http or plugin key, `allow_networks`, `allow_hosts`, `timeout` or `retry` is
a configuration error. The types `native`, `macos`, `linux` and `windows` do
not exist: they are refused with a message that names `os`.

## 5. CLI configuration

Instead of editing the file, let kjak write the feather:

```sh
kjak config init                      # creates the file (refuses to replace an existing one)
kjak feather add desktop --type os
kjak feather list
```

`feather add --type os` takes `--levels`, `--disabled`, `--scope` and
`--force`; no secret option (an `os` feather has no secret). Examples:

```sh
kjak feather add desktop_alerts --type os --levels warning,error,critical
kjak feather add desktop --type os --force          # replace an existing one
kjak feather add desktop --type os --scope project  # write ./.kjak.toml instead
kjak feather disable desktop
kjak feather enable desktop
```

## 6. First notification

First a dry run: kjak resolves the target and sends nothing (it does not run
the OS program):

```sh
kjak send --to desktop --dry-run "Hello from kjak"
```

Then the notification:

```sh
kjak send --to desktop "Hello from kjak"
kjak send --to desktop --title "Build complete" --level success "All tests passed"
```

```
ok    desktop (os) delivered in 420ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

PowerShell:

```powershell
kjak send --to desktop --title "Build complete" --level success "All tests passed"
```

What is shown:

| | macOS | Windows | Linux |
|---|---|---|---|
| title | the message's title, else `kjak` | the same | the title and the level (`Build complete · success`) |
| then | the level as a subtitle, then the body | the body, then the level | the body |

The message's URL (`--url`) is added to the body on its own line. There is
no sound and no action: clicking the notification opens nothing.

### Read the result as JSON

`-o json` prints the report: `status`, `acceptance` and, on a failure,
`error.code`:

```sh
kjak send --to desktop -o json "Hello from kjak"
kjak send --to desktop -o json "Hello from kjak" | jq '.results[0] | {status, acceptance, error}'
```

A notification that the OS program accepted:

```json
{"status": "delivered", "acceptance": "confirmed", "error": null}
```

The same command over SSH on a Mac fails with exit status 1 (the session
name after `managername:` depends on how kjak was started; `Background` is
an example):

```json
{
  "status": "failed",
  "acceptance": "rejected",
  "error": {
    "code": "unavailable",
    "message": "OS notifications are not available here: kjak does not run in a GUI session (launchctl managername: Background; over SSH or in a background job, a notification cannot be shown)"
  }
}
```

| the OS | `status` | `acceptance` | `error.code` | exit |
|---|---|---|---|---|
| accepted the notification | `delivered` | `confirmed` | — | 0 |
| kjak has no OS notifications for this system (FreeBSD, ...) | `failed` | `rejected` | `unsupported` | 1 |
| cannot show one here: no GUI or D-Bus session, the program missing or failing | `failed` | `rejected` | `unavailable` | 1 |
| Windows: the notifications of Windows PowerShell are off | `failed` | `rejected` | `rejected` | 1 |
| the deadline passed or Ctrl-C while the program ran | `failed` | `unknown` | `deadline` / `canceled` | 1 / 130 |

An `os` feather is never retried: a retry would meet the same machine.

## 7. Advanced configuration

A complete configuration for a developer machine: every message on the
desktop, with a default level, a group to grow later, and a template for the
end of a long command.

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["local"]
level = "info"
timeout = "15s"

[feathers.desktop]
type = "os"

[groups.local]
feathers = ["desktop"]

[templates.done]
title = "{{.task}} finished"
body = "{{.task}} exited with status {{.status}}"
level = "info"
```

```sh
kjak send "Backup finished"
kjak send --template done --var task=backup --var status=0
kjak template render done --var task=backup --var status=0
```

At the end of a long command (the shell fills in the exit status):

```sh
make test; kjak send --template done --var task="make test" --var status=$?
```

### Only some levels

`levels` limits a feather to some levels. A feather named directly with
`--to` that does not accept the level is an error (exit 2, nothing sent); a
**group member** that does not accept it is skipped:

```toml
# ~/.config/kjak/config.toml
[feathers.desktop]
type = "os"
levels = ["warning", "error", "critical"]

[groups.alerts]
feathers = ["desktop"]
```

```sh
kjak send --to alerts --level error "Disk almost full"
kjak send --to alerts "Nightly job done"
```

The second send skips `desktop` (`skip  desktop (os): level_filter (group alerts)`)
and still succeeds.

### The desktop and a remote destination

A notification on the screen is lost when you are away. Send to a remote
feather at the same time; each feather is delivered on its own, so an
`unavailable` desktop (over SSH) does not stop the other one (outcome
`partial`, exit 1):

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["notify"]

[feathers.desktop]
type = "os"

[feathers.discord]
type = "discord"
webhook_url = { secret = "discord.webhook_url" }

[groups.notify]
feathers = ["desktop", "discord"]
```

See the [Discord tutorial](discord.md) for the webhook and
[desktop-and-discord.toml](../examples/desktop-and-discord.toml).

## 8. Secrets

An `os` feather has no secret: nothing to put in a secrets file, an
environment variable or the keychain. `kjak feather add --type os` takes no
`--secret-*` option.

When an `os` feather shares a configuration with other feathers (the
example above), their secrets go in the secrets file as usual:

```toml
# ~/.config/kjak/secrets.toml (mode 0600; dummy value)
[discord]
webhook_url = "https://discord.com/api/webhooks/000000000000000000/replace-me"
```

The text of your notifications is shown on the screen: do not send secrets
in a message (section 10).

## 9. Troubleshooting

### `error.code: unavailable`

| message contains | cause | fix |
|---|---|---|
| `kjak does not run in a GUI session (launchctl managername: ...)` (macOS) | SSH, a launchd daemon or agent outside the GUI session, a CI job | run kjak in the logged-in desktop session; from SSH or CI use a remote feather |
| `cannot tell the session (launchctl managername: ...)` (macOS) | `/bin/launchctl` could not be run | run kjak in a normal user session |
| `osascript failed: ...` (macOS) | the script was refused (the message is osascript's) | try `osascript -e 'display notification "test"'` in the same terminal |
| `SystemRoot is not set` / `Windows PowerShell (...) is not installed` (Windows) | a stripped environment, or Windows PowerShell 5.1 removed | run kjak from a normal desktop session; restore Windows PowerShell |
| `Windows PowerShell failed: ...` (Windows) | PowerShell or WinRT refused the toast (the message is PowerShell's) | run kjak in your interactive desktop session, not as a service |
| `no D-Bus session (...)` (Linux) | SSH, cron, a container, CI | run in the desktop session; for cron, see below |
| `notify-send is not installed` (Linux) | libnotify's client is missing | install `libnotify-bin` (Debian, Ubuntu) or `libnotify` |
| `notify-send failed: ...` (Linux) | no notification server answered | start the desktop's server, or dunst / mako |

On Linux, a cron job of your desktop user can reach the session bus when
you give it the bus address: `DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u)/bus`
in the job (only while you are logged in to the desktop).

### `error.code: rejected` on Windows

```
FAIL  desktop (os) failed (1 attempt): notifications are turned off for Windows PowerShell (Settings, System, Notifications): notifications are off: DisabledForApplication
```

Windows reported that the notifications of Windows PowerShell are off. The
word at the end is the Windows setting: `DisabledForApplication` (the
Windows PowerShell entry), `DisabledForUser` (notifications off for you),
`DisabledByGroupPolicy` (an administrator's policy). Turn them on in
**Settings → System → Notifications** (section 3), or ask your administrator.

### `error.code: unsupported`

kjak has no OS notifications for this system (BSD and other systems). Use a
remote feather.

### "ok" but nothing appeared

`confirmed` means the OS program accepted the notification. Check, in this
order:

1. **The session.** The notification goes to the session kjak runs in. With
   two users logged in, or Remote Desktop / screen sharing, it is shown in
   kjak's session.
2. **The notification center.** macOS: click the clock in the menu bar.
   Windows: Win+N (Windows 11) or Win+A (Windows 10). A notification may be
   there without a banner.
3. **Focus / Do Not Disturb.** macOS Focus, Windows Do not disturb or Focus
   assist, GNOME Do Not Disturb.
4. **The app's settings.** macOS: Script Editor in System Settings →
   Notifications (Allow notifications, style not None). Windows: Windows
   PowerShell in Settings → System → Notifications (kjak reports this one as
   `rejected` when Windows tells it). Linux: the desktop's settings for kjak.
5. **The locked screen or a screen saver**: notifications may be held or
   hidden until you unlock.
6. **Test the OS program alone**, in the same terminal:

   ```sh
   osascript -e 'display notification "test" with title "test"'     # macOS
   notify-send "test" "from the terminal"                            # Linux
   ```

   If this does not show anything either, the cause is the OS settings, not
   kjak.

### Errors before anything is sent (exit 2)

| message | fix |
|---|---|
| `type "native": OS notifications are type = "os"` (or `macos`, `linux`, `windows`) | write `type = "os"` |
| `os feathers take no webhook_url, username or email settings ...` | remove those keys |
| `os feathers take no timeout or retry of their own (use [defaults])` | move `timeout` to `[defaults]`, or use `--timeout` |
| `feather "desktop" does not accept level info` | a fitting `--level`, or send to a group |

More: [os.md](../os.md), [troubleshooting.md](../troubleshooting.md).

## 10. Security notes

- **The screen is visible.** A notification may be shown on a locked screen,
  during screen sharing or a presentation, and stays in the notification
  center. Do not put secrets or personal data in the title or the body, and
  check the OS options that hide previews on a locked screen.
- **The text is data.** The title and the body may come from anywhere (a
  commit message, a log): kjak never puts them into a script or a command
  line that a shell or an interpreter parses (JSON on standard input on
  macOS, base64 in an environment variable on Windows, arguments after `--`
  on Linux).
- **The programs.** kjak runs `/usr/bin/osascript` and Windows PowerShell by
  their absolute paths (`SystemRoot` is read from the environment, which kjak
  trusts like its configuration), and `notify-send` from `PATH` on Linux:
  keep your `PATH` free of directories others can write.
- **A project's `os` feather** (`.kjak.toml`, used once trusted) can only
  show notifications on your screen: it reads no secret and makes no
  connection.
- **Borrowed identity on Windows.** The notification is shown as from
  Windows PowerShell; anyone who sees it cannot tell it came from kjak
  rather than from another PowerShell script.

## 11. Related references

- [os.md](../os.md) — the reference of the `os` feather (keys, what is shown, results, platforms)
- [Design of OS notifications](../design/os-notifications.md)
- [Configuration](../configuration.md) — feathers, groups, templates, defaults
- [CLI](../cli.md) — `kjak send`, `kjak feather add`, the JSON report
- [Delivery](../delivery.md) — acceptance, retries, outcomes
- [Troubleshooting](../troubleshooting.md)
- Examples: [desktop-only.toml](../examples/desktop-only.toml), [desktop-and-discord.toml](../examples/desktop-and-discord.toml), [development.toml](../examples/development.toml)
- Other tutorials: [Discord](discord.md), [Slack](slack.md), [Gmail](gmail.md)
