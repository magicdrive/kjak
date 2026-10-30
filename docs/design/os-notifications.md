# Design: OS notifications (v1.1.0)

[Reference](../os.md) · [Tutorial](../tutorials/os.md)

The `os` feather shows a notification of the machine kjak runs on. kjak is
one statically linked, unsigned Go executable without cgo, released as an
archive, by Homebrew and by `install.sh`. This page records how each OS was
chosen and what was left out.

## Requirements

1. A user who installed kjak by any of its ways gets a notification, with
   nothing else to install where possible.
2. No change to the distribution: one executable per platform, no app
   bundle, no installer, no cgo.
3. The text is data: no shell, AppleScript or PowerShell code is built from
   it.
4. No persistent change to the user's system (no registration, shortcut or
   permission) unless the user runs a setup for it.
5. Honest results: what kjak can tell (no session, notifications off) is
   reported as such; what it cannot (whether anyone saw it) is not
   claimed.

## macOS

| approach | identity | needs | result |
|---|---|---|---|
| UserNotifications (`UNUserNotificationCenter`) | the app's bundle identifier | an app bundle (Info.plist, bundle id), code signing for the permission prompt, cgo or a helper app | **not used**: kjak is not a bundle; a helper app would change the distribution (requirement 2) and need signing and notarization |
| `NSUserNotification` | as above | deprecated since macOS 11 | not used |
| A bundled helper (as terminal-notifier does) | the helper's | a signed app bundle installed with kjak | not used (requirement 2); a later option if notarized releases come |
| `osascript` (`display notification`), JavaScript for Automation | Script Editor's, usually | `/usr/bin/osascript`, present on every macOS; a GUI (Aqua) session | **chosen** |

`osascript -l JavaScript` runs a constant script that reads the notification
as JSON on standard input (`NSFileHandle`, UTF-8) and calls
`displayNotification`: the text is never in the script. AppleScript's
`do shell script` and string concatenation are never involved.

Known limits: the notification is shown under the identity macOS gives
`osascript` (Script Editor on recent versions), whose notification setting
the user controls; macOS does not tell kjak whether it is allowed or
shown. A process outside the GUI session (SSH, a launchd daemon, CI not in
Aqua) cannot show one: kjak checks `launchctl managername` and reports
`unavailable` instead of a `confirmed` that would mean nothing. Gatekeeper
and notarization do not apply: kjak runs a program of the OS.

## Windows

| approach | identity | needs | result |
|---|---|---|---|
| WinRT toast with kjak's own AppUserModelID | kjak | a Start menu shortcut carrying the AUMID (an installer or a `kjak` setup command), WinRT from Go (COM projections) | **not used for 1.1**: a persistent change to the user's system (requirement 4); a candidate for a later `kjak` setup command |
| Windows App SDK notifications | the packaged app | MSIX packaging or the Windows App SDK runtime | not used (requirement 2) |
| `Shell_NotifyIcon` balloon (Win32) | kjak's executable | a hidden window and a tray icon kept long enough; no way to tell whether notifications are allowed | not used: its lifetime and its display are hard to make reliable from a process that exits at once |
| WinRT toast through Windows PowerShell 5.1, with the AppUserModelID of Windows PowerShell | Windows PowerShell | `powershell.exe`, present on every Windows 10 and 11 | **chosen** |

kjak runs `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass
-EncodedCommand <script>`: the script is a constant (UTF-16LE base64), and
the notification reaches it base64-encoded in `KJAK_NOTIFICATION`; each text
is put in the toast's XML with `CreateTextNode`, never as markup. Before
`Show` it reads the notifier's `Setting` and reports a disabled one
(`DisabledForUser`, `DisabledByGroupPolicy`, ...) as `rejected`.

Known limits: the toast is shown as from Windows PowerShell, whose
notifications the user may have turned off; PowerShell 7 (`pwsh`) cannot
be used (no WinRT projection); a session without a desktop (a service, SSH,
a scheduled task not run interactively) cannot show one; Focus Assist /
Do not disturb is not reported. Borrowing Windows PowerShell's identity is
an owner decision recorded here: it needs no registration, and kjak's own
identity can be added later without changing the configuration.

## Linux

`notify-send` (libnotify) is the freedesktop way every desktop supports. It
is found on `PATH`, run with `--app-name=kjak`, the urgency of the level and
the text as arguments after `--`. Without a D-Bus session bus
(`DBUS_SESSION_BUS_ADDRESS`, `$XDG_RUNTIME_DIR/bus`) there is no desktop
to show it, and kjak reports `unavailable` before running anything. A
direct D-Bus client in Go would need a new dependency for no gain.

## Results and the delivery contract

The feather is a feather like the others: one attempt, its own result, never
retried. Two delivery error codes are added (a minor change: "error codes
of a new kind of failure", [stability](../stability.md#versioning)):
`unsupported` (no backend for this OS) and `unavailable` (no session, a
missing or failing program). A disabled notification setting is `rejected`.
The send's deadline and Ctrl-C kill the program; the result is then
`unknown` (it may have been posted).

## Testing

- Unit tests run every backend on every OS with a fake runner: the program,
  its arguments, the data it gets (and that hostile text — quotes,
  `do shell script`, `$(...)`, PowerShell code, escape sequences — reaches
  it only as data), and the mapping of every failure.
- `TestNativeBackend` (`KJAK_OS_NOTIFY_NATIVE=1`) posts one real
  notification on the CI runners of macOS, Windows and Linux: the OS
  program must accept the script, or refuse for a reason it states (no
  session, notifications off). A syntax or a reference error of the script
  fails it.
- Nothing in CI can tell whether a notification appeared on a screen. That
  is checked by hand on a desktop (macOS amd64 and arm64, Windows 11) before
  a release.
