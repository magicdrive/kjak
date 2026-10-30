# Design: OS notifications (deferred to v1.2.0)

> **Status (2026-10-30): not releasable.** The macOS backend below
> (osascript) was checked on a real Mac and its notifications are **not
> shown** when kjak runs from a terminal; the `launchctl managername ==
> Aqua` check is wrong as well. The owner deferred the os feather (macOS,
> Windows, Linux) to v1.2.0; see "Hands-on results" and "Re-evaluation"
> at the end. Everything else on this page describes the v1.1.0 candidate
> as it was written.

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

## Hands-on results (2026-10-30, owner, macOS)

Run from iTerm2 directly, no SSH:

| check | result |
|---|---|
| `launchctl managername` | `Background` (not `Aqua`) |
| `launchctl print gui/501` | succeeds |
| `/usr/bin/osascript` AppleScript `display notification` | exits, **nothing shown** |
| `osascript -l JavaScript` (the JXA of this backend) | exits, **nothing shown** |
| through `launchctl asuser` | **nothing shown** |
| the same script run in Script Editor (GUI) | **shown** |
| the script exported by Script Editor as an `.app`, started from iTerm2 with `open ~/Desktop/KjakNotificationTest.app` | **shown** |

Checked on the same Mac without posting anything (macOS 15.7.9, x86_64):
`launchctl managername` says `Background`, `launchctl print gui/501` exits
0, the console belongs to the user. A minimal Swift bundle
(`prototype/probe.swift`, bundle id `io.github.magicdrive.kjak.probe`,
ad-hoc signed, `LSUIElement`), its executable run directly from the
terminal (not through `open`), gets an answer from
`UNUserNotificationCenter.getNotificationSettings`: `authorizationStatus`
0 (not determined). So a bundle run directly is known to the notification
center by its own identifier; whether its notifications are shown is not
checked yet.

Conclusions:

1. The osascript backend is not a working implementation: the OS accepts
   the script and shows nothing, and kjak would report `confirmed`.
2. `managername == Aqua` refuses an ordinary terminal session (it says
   `Background` in iTerm2). A GUI session of the user is better told by
   `launchctl print gui/<uid>` (or the owner of `/dev/console`), and in the
   end by the result of what posts the notification.
3. Only notifications posted by an app bundle were shown.

## Re-evaluation (for v1.2.0)

Required: dynamic title and body passed as data; results classified
(accepted, not allowed, no session, failed, interrupted); the delivery
contract kept; works for Homebrew, the GitHub Release archives and
`install.sh` alike.

| approach | shown | identity | data | results | distribution | verdict |
|---|---|---|---|---|---|---|
| A. osascript (the backend above) | no (owner's Mac) | Script Editor | stdin | none: always looks accepted | none | rejected |
| B. osascript, after the user allows Script Editor | unknown | Script Editor | stdin | none | none | rejected: manual step, borrowed permission |
| C. an `osacompile` applet generated on the user's Mac, started with `open` | the same kind of `.app` was shown | the applet (kjak's id after editing Info.plist and re-signing ad hoc) | a private file and `open --args` | weak: `open` returns at launch (a result file must be awaited); no permission state | no release change; generated locally, so the same for every channel | fallback |
| D. a Swift helper bundle (`UNUserNotificationCenter`) run directly | not checked yet; known to the notification center when run directly | kjak's own (`io.github.magicdrive.kjak.notifier`, shown as kjak) | JSON on stdin; exit status | good: not allowed (denied, alerts off), refused, timeout, accepted | a macOS build in the release | **first candidate** |
| E. kjak copied into a bundle, Objective-C through purego (no cgo) | unknown | kjak | in process | good | no release change; blocks and run loops through purego are fragile | rejected |
| F. depend on terminal-notifier or the like | depends | that tool | arguments | weak | a dependency; `NSUserNotification` is deprecated | rejected |

D, as proposed (to be decided after the proof of concept):

- `kjak-notifier.app`: bundle id `io.github.magicdrive.kjak.notifier`,
  name kjak, `LSUIElement`, ad-hoc signed. kjak runs its executable
  directly with the notification as JSON on stdin; exit status 0 accepted,
  3 not allowed (`rejected`), 4 refused by the notification center
  (`unavailable`), 2 bad input (`internal`), 5 no answer (`unknown`); a done
  context is `unknown` as for the other backends. The first use asks for
  permission once.
- Distribution: the helper (universal, arm64 + x86_64) is embedded in the
  darwin kjak binaries (go:embed) and written on first use to
  `~/Library/Application Support/kjak/<version>/kjak-notifier.app`,
  checked by its hash. Files kjak writes are not quarantined, and the
  Homebrew formula, the archives and `install.sh` stay as they are. The
  release gets a macOS job that builds the helper for the ubuntu build job
  to embed; CI builds and tests it on macOS.
- Side effects: one helper per kjak version in Application Support (an
  older one removed by the next), one "kjak" entry in the notification
  settings. Upgrade: a new directory, the same bundle id (the permission is
  expected to carry over; to check). Uninstall: remove
  `~/Library/Application Support/kjak`.
- Signing: ad hoc for now; Developer ID signing and notarization need the
  owner's Apple Developer account (a later option). Whether an ad-hoc
  signed helper may post is part of the proof of concept.
- The session check becomes `launchctl print gui/<uid>`; the rest is the
  helper's result.

Proof of concept (owner, on a real Mac; `prototype/notifier.swift`,
`prototype/Info.plist`):

```sh
mkdir -p /tmp/kjak-poc/KjakNotifier.app/Contents/MacOS
cp docs/design/prototype/Info.plist /tmp/kjak-poc/KjakNotifier.app/Contents/
swiftc -O -o /tmp/kjak-poc/KjakNotifier.app/Contents/MacOS/kjak-notifier docs/design/prototype/notifier.swift
codesign -s - --force /tmp/kjak-poc/KjakNotifier.app
printf '%s' '{"title":"kjak test","subtitle":"info","body":"Hello from the prototype"}' \
  | /tmp/kjak-poc/KjakNotifier.app/Contents/MacOS/kjak-notifier; echo "exit=$?"
```

To check: the notification appears from iTerm2; it is shown as kjak; the
permission prompt appears once; denying gives exit 3; dynamic text arrives
as given; Intel and Apple silicon; it can be embedded and placed as above;
signing, bundle id, placement and upgrade behave as described. If it is
not shown, re-evaluate C.

## Open findings of the v1.1.0 audits (to fix in v1.2.0)

- Linux: the body is markup for most notification servers (`<a href>`,
  `<img>`, a plain `a < b`): escape `&`, `<`, `>` in the body.
- Windows: errors of a script started with `-EncodedCommand` may come as
  CLIXML; wrap the script in `try`/`catch` and print one plain line.
- `TestNativeBackend` passes without reaching the script (no session):
  require acceptance on the macOS and Windows runners, and check the
  scripts' syntax without posting (`osacompile`, the PowerShell parser).
- `KJAK_NOTIFICATION` can exceed the 32767-character environment limit
  (`json.Marshal` escapes `<`, `>`, `&`): `SetEscapeHTML(false)` or stdin.
- A context done before the OS program started is reported `unknown`
  ("may have been posted"); only a started program should be.
- Windows: a hidden window (`CREATE_NO_WINDOW`) when kjak has no console.
- Format characters (Cf: bidi overrides, zero width) are kept: replace them.
- Tests: the deadline case, the backends' own context checks, the
  Windows payload check that compares a constant with itself.
- `scripts/mutants.tsv`: the `osnotify-deliver-unknown` row is split over
  three lines (literal tab and newline): `make mutation-check` fails.
- The CI step runs on every test runner and is required: make it a job of
  its own with a short timeout, outside the release's CI call.
- `config trust` describes a project's os feather as a webhook.
- `feather add --type os --secret-stdin` reads stdin before refusing.
