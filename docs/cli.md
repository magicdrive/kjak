# CLI usage

```
kjak send ...
kjak feather list|show|add|remove|enable|disable|types
kjak group list|show|set|remove
kjak config path|show|validate|init|trust|untrust
kjak template list|show|set|remove|render
kjak version            (also --version, -v)
kjak --help             (also -h, help; every command takes --help)
```

`kjak` alone prints a short help and exits 2: there is no default command,
and `kjak hello` is an unknown command, not a send.

Options and arguments may come in any order (`kjak send hello --to x`);
everything after `--` is an argument.

Options of every command that reads the configuration:

| option | |
|---|---|
| `-c, --config FILE` | the global configuration file (default `$KJAK_CONFIG`, else `$XDG_CONFIG_HOME/kjak/config.toml`) |
| `--no-project` | ignore `./.kjak.toml` |
| `-o, --output text\|json` | output format (commands that print data) |

## kjak send

```
kjak send [options] BODY...
kjak send [options] --stdin
kjak send [options] --json-input FILE|-
kjak send [options] --template NAME [--var KEY=VALUE]...
kjak send [options] --template-file FILE [--var KEY=VALUE]...
```

Exactly one input gives the message; two of them are a usage error (exit 2).

| input | message |
|---|---|
| `BODY...` | the arguments joined by spaces |
| `--stdin` | standard input (trailing newlines removed), at most 1 MiB |
| `--json-input FILE` | one JSON object `{"title", "body", "level", "url"}` (unknown keys, a key given twice, or anything after the object are errors); `-` reads standard input, at most 1 MiB. FILE may be a FIFO (Unix): kjak waits for its writer to close it, and SIGINT or SIGTERM ends the wait. It is read through the descriptor kjak opened, so removing or replacing the path meanwhile changes nothing. A writer that closes it without writing ends it too: an empty input (exit 2). On macOS, when kjak's descriptors are 1024 or more, kjak sees a writer's close within 0.1s, and a writer that opens and closes the FIFO without writing within that time is not seen: kjak keeps waiting for one that writes |
| `--template NAME` | a template of the configuration, rendered with `--var` |
| `--template-file FILE` | a TOML file with the keys of a template (`title`, `body`, `level`, `url`) |

`--title`, `--level` and `--url` override what a template or the body input
gives; they cannot be combined with `--json-input`. `--var` is only for the
template inputs. The level defaults to `defaults.level`, else `info`.

Validation (exit 2 when it fails): the body is required and at most 16384
characters; the title is one line (no control characters) of at most 256; the
level is one of `info`, `success`, `warning`, `error`, `critical`; the URL is
an absolute `http(s)` URL.

Targets: `-t, --to NAME` (repeatable, comma-separated) names feathers and
groups; without `--to`, `defaults.targets` is used. Each feather is delivered
to once, in the order it is first named (see [delivery.md](delivery.md)).

Delivery options: `--timeout DURATION` (the deadline of the send once the
message is read: credentials, deliveries, retries and waits; at most 10m), `--max-retries N` (0–10), `--concurrency N` (1–64) — the bounds of the
configuration's `[defaults]`. `--dry-run` resolves the targets and the
credentials (the preflight) and sends nothing. `-q, --quiet` prints nothing
when every target was delivered.

Before anything is sent, kjak checks every target: an unknown name, a feather
named directly that is disabled or does not accept the level, a credential
that does not resolve, an untrusted project file — each is a preflight error:
exit 2, nothing sent.

### Text output

```
ok    dev_slack (slack) delivered in 312ms (1 attempt)
FAIL  ops_mail (email) failed (3 attempts): RCPT TO <x@example.com>: SMTP 450 mailbox busy (gave up after 3 attempts)
????  ops_discord (discord) failed (1 attempt): HTTP 504 from a gateway: the provider may have accepted it [acceptance unknown: it may have been delivered; not retried]
skip  dev_discord (discord): level_filter (group development)
partial: 1 delivered, 2 failed (1 with unknown acceptance), 1 skipped
```

### JSON output (`-o json`)

```json
{
  "schema_version": 1,
  "outcome": "partial",
  "canceled": false,
  "dry_run": false,
  "duration_ms": 1234,
  "level": "error",
  "summary": {"total": 3, "delivered": 1, "failed": 1, "unknown": 0, "skipped": 1},
  "results": [
    {
      "feather": "dev_slack",
      "type": "slack",
      "groups": ["development"],
      "status": "delivered",
      "acceptance": "confirmed",
      "attempts": 2,
      "duration_ms": 812,
      "error": null,
      "skipped": null,
      "history": [
        {"number": 1, "started_at": "2026-10-01T12:00:00Z", "duration_ms": 120,
         "acceptance": "rejected", "retryable": true, "status_code": 429,
         "retry_after_ms": 500, "wait_ms": 500,
         "error": {"code": "rate_limited", "message": "HTTP 429"}},
        {"number": 2, "started_at": "2026-10-01T12:00:00.62Z", "duration_ms": 190,
         "acceptance": "confirmed", "retryable": false, "status_code": 200}
      ]
    },
    {
      "feather": "dev_discord", "type": "discord", "groups": ["development"],
      "status": "skipped", "acceptance": "none", "attempts": 0, "duration_ms": 0,
      "error": null, "skipped": {"reason": "level_filter", "group": "development"},
      "history": []
    }
  ]
}
```

| field | values |
|---|---|
| `outcome` | `success` (no failure; skipped targets allowed), `partial`, `failure`, `canceled` |
| `results[]` | one per target, in the order of the targets |
| `status` | `delivered`, `failed`, `skipped`, `planned` (dry run) |
| `acceptance` | `confirmed`, `rejected`, `unknown`, `none` (no attempt) |
| `error.code` | `rejected`, `rate_limited`, `server_error`, `network`, `ambiguous`, `deadline`, `canceled`, `protocol`, `internal` |
| `skipped.reason` | `disabled`, `level_filter` |

Within schema version 1, fields are only added, never removed or renamed.
`error` is `null` in a report; `level` is `null` when no message was built.

### JSON errors

With `-o json`, a command that fails before delivering anything (exit 2, or
130 when interrupted) prints a JSON document on **stdout** and its diagnostic
on **stderr**:

```json
{
  "schema_version": 1,
  "outcome": "error",
  "canceled": false,
  "dry_run": false,
  "duration_ms": 0,
  "level": null,
  "summary": {"total": 0, "delivered": 0, "failed": 0, "unknown": 0, "skipped": 0},
  "results": [],
  "error": {"code": "credential", "message": "feather \"dev\": webhook_url: credential error: environment variable KJAK_HOOK is not set"}
}
```

`kjak send` keeps the shape of its report (empty); the other commands with
`-o json` print `schema_version`, `outcome` and `error` only.

| `error.code` | |
|---|---|
| `usage` | the command line, after its flags were parsed (two inputs, a bound, a missing argument) |
| `config` | a configuration file: syntax, validation, lock, concurrent change |
| `untrusted_project` | `./.kjak.toml` is not trusted |
| `routing` | a target: unknown, disabled, filtered out by its levels |
| `credential` | a secret that does not resolve, or a resolved webhook URL that is not acceptable |
| `input` | the message or its input (empty body, invalid level, JSON input, template) |
| `not_found` | a named feather, group or template |
| `deadline` | the deadline of the send (`--timeout`) passed before anything was sent, while the credentials were resolved |
| `interrupted` | SIGINT or SIGTERM before any delivery (exit 130) |
| `error` | anything else |

Messages never contain a secret (they are redacted like a report's).

A command line whose **flags cannot be parsed** (an unknown flag, a value of
the wrong type, an unknown `--output`) is not a JSON request yet: the error
is text on stderr only, stdout stays empty, exit 2.

### Exit status

| status | meaning |
|---|---|
| 0 | every attempted target was delivered (skipped targets do not count) |
| 1 | at least one delivery failed — including an **unknown** acceptance |
| 2 | input, configuration or preflight error: nothing was sent |
| 130 | interrupted by SIGINT (Ctrl-C) or SIGTERM (a canceled CI job, a stopped service); the report of what happened is still printed |

The output of `send` is a report of deliveries already made: when it cannot
be written (a full disk, a reader that went away), kjak says so on stderr
and the exit status is still the deliveries'. The output of every other
command is its result: when it cannot be written, the command fails with
exit 2 (a changing command has made its change by then); a closed pipe
(`| head`) is not reported on stderr, on Unix (EPIPE) as on Windows
(`ERROR_NO_DATA`, `ERROR_BROKEN_PIPE`).

So a JSON report on stdout is whole unless kjak said `cannot write the
result` on stderr; the exit status tells the deliveries either way, and a
delivery whose acceptance is `unknown` may have arrived: check before
sending again. With `set -o pipefail`, `kjak send ... | head` fails only
when the deliveries did. An unwritable stderr changes no exit status.

## kjak feather

```sh
kjak feather list [-o json]
kjak feather show NAME [-o json]
kjak feather add NAME --type slack|discord|email [options]
kjak feather remove NAME [--scope global|project]
kjak feather enable NAME [--scope global|project]
kjak feather disable NAME [--scope global|project]
kjak feather types
```

`add` writes the global file unless `--scope project`; `--force` replaces a
feather of the same name. The secret of the type (Slack and Discord:
`webhook_url`; e-mail: `password`) is given by reference:

| option | stored as |
|---|---|
| `--secret-env VAR` | `{ env = "VAR" }` |
| `--secret-keychain SERVICE/ACCOUNT` | `{ keychain = { service, account } }` |
| `--secret-stdin` | `{ literal = "..." }`, read from standard input |

The secret value is never a command-line argument (other processes and the
shell history would see it).

Type options: `--username` (discord); `--host`, `--port`, `--security
starttls|tls|none`, `--user`, `--from`, `--mail-to` (repeatable),
`--subject-prefix` (email). Common: `--levels L[,L]`, `--disabled`.

Without `--scope`, `remove`, `enable` and `disable` change the file that
defines the feather. `remove` refuses a feather that a group names.

## kjak group

```sh
kjak group list [-o json]
kjak group show NAME [-o json]
kjak group set NAME FEATHER... [--scope global|project]
kjak group remove NAME [--scope global|project]
```

`set` creates or replaces the whole group. Members are feathers; a group
naming a group is an error (no nesting in v0.1.0).

## kjak template

```sh
kjak template list [-o json]
kjak template show NAME [-o json]
kjak template set NAME --body TEXT|--body-stdin [--title T] [--level L] [--url U] [--scope S]
kjak template remove NAME [--scope global|project]
kjak template render NAME [--var KEY=VALUE]... [-o json]
```

Fields are Go `text/template` strings with string variables
(`{{.service}}`); a variable the template uses must be given. `render` prints
the message without sending it.

## kjak config

```sh
kjak config path [-o json]      # the files and the trust state of ./.kjak.toml
kjak config show [-o json]      # the effective configuration, literal secrets redacted
kjak config validate            # both files, whether or not the project file is trusted
kjak config init [--force]      # create the global file
kjak config trust [--yes]       # trust ./.kjak.toml (asks on a terminal; --yes otherwise)
kjak config untrust
```

## Environment

| variable | |
|---|---|
| `XDG_CONFIG_HOME` | base of the configuration directory (absolute; default `~/.config`; on Windows `%AppData%` when unset) |
| `KJAK_CONFIG` | the global configuration file |
| any `env` reference | read when a send resolves its credentials |
