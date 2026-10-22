# Configuration reference

kjak reads TOML from two files:

| file | location | used |
|---|---|---|
| global | `--config`, else `$KJAK_CONFIG`, else `$XDG_CONFIG_HOME/kjak/config.toml` (`~/.config/kjak/config.toml`; on Windows without `XDG_CONFIG_HOME`: `%AppData%\kjak\config.toml`) | always, when it exists |
| project | `./.kjak.toml` (the current directory only) | only when trusted ([security.md](security.md)); `--no-project` ignores it |

Both files have the same format, at most 1 MiB each. Unknown keys and keys
given twice are errors (a typo is never ignored). `kjak config validate` checks both; `kjak config show` prints the
merged result.

## Merge rules

| part | rule |
|---|---|
| `[defaults]` | field by field: a field the project file sets wins, the others stay |
| `[feathers.NAME]` | by name, replaced **whole**: no field of the global entry remains |
| `[groups.NAME]` | by name, replaced whole |
| `[templates.NAME]` | by name, replaced whole |

A project group may name global feathers. References are checked on the
merged result.

## `[defaults]`

| key | type | default | |
|---|---|---|---|
| `timeout` | duration string | `"10s"` | the deadline of a whole send, every retry and wait included (max `10m`) |
| `max_retries` | integer | `2` | retries of a retryable failure, after the first attempt (0–10) |
| `concurrency` | integer | `5` | feathers delivered at the same time (1–64) |
| `level` | string | `"info"` | the level of a message that names none |
| `targets` | list of names | — | the feathers and groups of a send without `--to` |

## `[feathers.NAME]`

Names: letters, digits, `_`, `.`, `-`; at most 64; starting with a letter or
a digit. A name cannot be both a feather and a group.

Common keys:

| key | type | default | |
|---|---|---|---|
| `type` | string | required | `slack`, `discord`, `email` (`os` and other OS-native types are refused: not supported in v0.1.0) |
| `enabled` | bool | `true` | |
| `levels` | list | all levels | the levels the feather accepts |

A key that does not belong to the type is an error.

### Credentials

A secret is a reference with exactly one source:

```toml
webhook_url = { env = "KJAK_DEV_SLACK_WEBHOOK" }
webhook_url = { keychain = { service = "kjak", account = "dev_slack" } }
webhook_url = { literal = "https://hooks.slack.com/services/T000/B000/XXXX" }
```

Two sources at once, or none, is an error. A plain string
(`webhook_url = "https://..."`) is an error too: a literal secret is always
explicit. A `webhook_url` must be `https` (`http` only to localhost). See
[security.md](security.md).

### slack

```toml
[feathers.dev_slack]
type = "slack"
webhook_url = { env = "KJAK_DEV_SLACK_WEBHOOK" }   # an incoming webhook URL
levels = ["warning", "error", "critical"]
```

The message is posted as a text line (`[LEVEL] title: first line`) and an
attachment colored by level with the title, the body and the URL as the
title link. `&`, `<` and `>` are escaped, so a message cannot mention
`@channel` or users. An incoming webhook posts to the channel and as the app it
was created for.

### discord

```toml
[feathers.ops_discord]
type = "discord"
webhook_url = { keychain = { service = "kjak", account = "ops_discord" } }
username = "kjak"        # optional display name
```

The message is one embed colored by level. kjak executes the webhook with
`?wait=true` (keeping a `thread_id` of the URL) so that Discord confirms the
created message. Mentions are disabled (`allowed_mentions.parse = []`). Discord
limits an embed description to 4096 characters: a longer body is cut and ends
with `…`.

### email

```toml
[feathers.oncall_mail]
type = "email"
host = "smtp.example.com"
port = 587                       # default: 587 starttls, 465 tls, 25 none
security = "starttls"            # starttls (default), tls (implicit TLS), none
user = "kjak@example.com"
password = { env = "KJAK_SMTP_PASSWORD" }
from = "kjak@example.com"
to = ["oncall@example.com", "sre@example.com"]
subject_prefix = "[kjak]"
levels = ["error", "critical"]
```

- `starttls` requires the server to offer STARTTLS: kjak never falls back to
  clear text. Certificates are verified (TLS 1.2 or later).
- `user` and `password` go together; with `security = "none"` they are refused
  (the password would travel in clear text). `none` suits a local relay.
- AUTH PLAIN, else AUTH LOGIN.
- `from` and `to` are bare addresses (`name@example.com`, no display name).
- The subject is the title, or `[LEVEL] first line of the body`, after
  `subject_prefix`. The body is UTF-8 quoted-printable text, followed by the URL.

## `[groups.NAME]`

```toml
[groups.development]
feathers = ["dev_slack", "dev_discord"]
```

At least one member, each a feather, each once. Groups do not nest in
v0.1.0.

## `[templates.NAME]`

```toml
[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"              # may be a template too: "{{.level}}"
url = "https://ci.example.com/runs/{{.run}}"
```

`body` is required. Variables are given with `--var KEY=VALUE`; a missing one
is an error. Only `text/template`'s built-in actions are available.

A template file (`--template-file`) holds the same keys at the top level.

## A complete example

```toml
[defaults]
timeout = "15s"
targets = ["development"]

[feathers.dev_slack]
type = "slack"
webhook_url = { env = "KJAK_DEV_SLACK_WEBHOOK" }

[feathers.dev_discord]
type = "discord"
webhook_url = { env = "KJAK_DEV_DISCORD_WEBHOOK" }
levels = ["error", "critical"]

[feathers.oncall_mail]
type = "email"
host = "smtp.example.com"
user = "kjak@example.com"
password = { keychain = { service = "kjak", account = "oncall_mail" } }
from = "kjak@example.com"
to = ["oncall@example.com"]
levels = ["critical"]

[groups.development]
feathers = ["dev_slack", "dev_discord"]

[groups.incident]
feathers = ["dev_slack", "dev_discord", "oncall_mail"]

[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"
```

## Changes made by kjak

`feather add`, `group set`, `template set` and the other changing commands
parse the file, change it, validate the result (alone and merged with the
other file) and write it back:

- atomically: a temporary file in the same directory, synced, renamed over
  the file; the file keeps its mode, minus write permission for group and
  others (a new file is 0600);
- through symbolic links: a file that is a link (a dotfiles repository) stays
  a link and its target is written; a link to nothing is refused;
- under a lock: an exclusive OS lock (flock on macOS/Linux, LockFileEx on
  Windows) on `<file>.lock`; a second kjak fails instead of waiting (`the
  configuration is being changed by another kjak process`). The OS releases
  the lock when its process ends, so a crashed or killed kjak blocks nobody;
  the `.lock` file itself stays (it is never followed if it is a symbolic
  link). `kjak send` only reads and takes no lock;
- checked: if the file changed while kjak wrote it (an editor that ignores
  the lock), kjak stops and leaves that change alone.

kjak writes its own layout: **comments and key order are not kept**. Edit by
hand if you want to keep them.

A project file is changed only when it is trusted (or does not exist yet);
the trust then moves to the content kjak wrote.
