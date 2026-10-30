# Stability and compatibility (v1)

This page is the public contract of kjak 1.x: what scripts, CI pipelines,
configuration files and plugins may rely on, what may change in a minor
release, and how a change is deprecated. The detailed behavior is in the
guides it links to; this page names the parts that are stable.

The tables marked `contract` below are checked against the code by
`stability_test.go` and `internal/commandline/option/contract_test.go`: a
change of a command, a flag, a key or a code fails the tests until this page
(and so the contract) is changed with it.

## Versioning

kjak follows [Semantic Versioning](https://semver.org/) from v1.0.0:

| release | may |
|---|---|
| patch (1.0.x) | fix bugs and security issues; never remove or rename anything below |
| minor (1.x.0) | add commands, flags, configuration keys, feather types, JSON fields, error codes of a new kind of failure, plugin request fields; deprecate something (it keeps working) |
| major (2.0.0) | remove or rename anything below, change a default, a meaning or an exit status |

**A security fix is the exception**: when keeping a behavior would keep a
vulnerability (a secret that can leak, a request that should be refused), a
patch or minor release may refuse what was accepted. It says so in the
release notes, under *Breaking changes*, with the migration. v1.0.0 itself
has such changes compared to v0.0.4: see [migration.md](migration.md).

## What is guaranteed

| part | guaranteed in 1.x | details |
|---|---|---|
| Commands and flags | the commands, actions and flags of the [table below](#command-line), their meaning and their defaults | [cli.md](cli.md) |
| Exit status | 0, 1, 2, 130 and what each means | [cli.md](cli.md#exit-status) |
| JSON output | `schema_version` 1 and the keys and values below; fields are only added | [cli.md](cli.md#json-output--o-json) |
| Error codes | the delivery `error.code` and the command `error.code` values below | [cli.md](cli.md#json-errors) |
| Configuration | every key below, its type and its default; a file valid in 1.0 stays valid in 1.x (except a security fix) | [configuration.md](configuration.md) |
| Secrets files | the format (TOML tables of strings, dot-separated names), the lookup rules, the permission requirements per OS | [configuration.md](configuration.md#77-secrets-files), [security.md](security.md) |
| Trust | a project file is used only once trusted; the trust store format; an entry written by 1.0 stays valid in 1.x | [security.md](security.md#project-configuration-trust) |
| Feather types | `slack`, `discord`, `email`, `http`, `external` and their keys | [configuration.md](configuration.md#4-feathers) |
| Plugin protocol | protocol version 1: the request, the response, the process environment, the exit status rules | [plugins.md](plugins.md#protocol-v1) |
| Delivery semantics | acceptance (`confirmed`, `rejected`, `unknown`), "unknown is never retried", the retry bounds, the deadline | [delivery.md](delivery.md) |
| Environment | `KJAK_CONFIG`, `KJAK_SECRETS`, `XDG_CONFIG_HOME`; for plugins `KJAK_PLUGIN_PROTOCOL` | [cli.md](cli.md#environment) |
| Version line | the first line of `kjak --version` (and `kjak version`) is `kjak version vX.Y.Z` (installers and the Homebrew formula read it) | [cli.md](cli.md) |

## What is not guaranteed

- **Text output.** The text of a command (`ok    dev_slack (slack) ...`, the
  columns of `feather list`, `config path`, `config trust`) and every
  diagnostic on stderr is for people: wording, spacing and order may change
  in any release. Scripts use `-o json` and the exit status.
- **Error messages.** `error.message` (in the JSON report and the error
  document) is text for people; scripts use `error.code`. Messages never
  contain a secret in the forms kjak knows: the value, its URL encodings,
  the path and query of a secret URL, and the base64 forms it sends (HTTP
  basic authentication, SMTP `AUTH`). A provider that echoes another
  transformation of a secret (changed case, a fragment) is not covered.
- **Values for people inside JSON documents.** `project_state`,
  `project_secrets_state`, `secrets_state` and `secrets_permission_model`
  of `config path` and `config validate`, and `auth` and `policy` of
  `feather show`, are descriptions: compare them by prefix (`trusted`,
  `untrusted`, `invalid`, `none`) at most. The keys of the entries of
  `feather plugins` (`plugins[]`) may grow.
- **What a dry run checks.** `send --dry-run` resolves the routing, the
  credentials and the trust, and builds every feather; it does not resolve
  names or check the address policy (a refused address shows as `blocked`,
  exit 1, in a real send).
- **Provider payloads.** The JSON sent to Slack and Discord and the e-mail
  headers kjak writes may change to follow the providers (the message's
  title, body, level and URL are always in it). An `http` feather's body is
  yours: `format` and `template` are stable.
- **Timing.** Backoff values and jitter, how long a request takes, the
  order in which concurrent deliveries happen (not the order of the results,
  which follows the targets).
- **Undocumented behavior**, and behavior that contradicts the
  documentation (a bug: it is fixed).
- **`config show -o json` beyond its top-level keys** (`schema_version`,
  `sources`, `defaults`, `feathers`, `groups`, `templates`): the entries
  of `feathers` follow the configuration and may gain keys. The keys of a
  group and a template entry are stable ([below](#other-commands)).
- **Platform support** follows the [platform table](#platforms): a
  platform may be added in a minor release.
- **Go packages.** kjak is a command; everything under `internal/` is
  private and may change in any release.

## Deprecation policy

1. A deprecated feature keeps working for the rest of the major version.
2. Using it prints a warning on **stderr** (never on stdout, never in the
   JSON document), naming what to use instead, and the documentation marks
   it *deprecated*. A strict mode refuses it where one exists
   (`config validate --strict`).
3. It is announced in the release notes of the release that deprecates it,
   and removed no earlier than the next major release.
4. A security fix may shorten this (see [Versioning](#versioning)).

Deprecated in 1.0:

| feature | instead | since |
|---|---|---|
| `{ literal = "..." }` credentials, `feather add --secret-stdin` | a secrets file (`{ secret = "..." }`), `env` or `keychain`; `kjak config migrate-secrets` | v1.0.0 |

## Command line

Every command and action of kjak 1.x, with its flags (a short and a long
spelling are the same flag). Every command also takes `-h`/`--help`.
Options and arguments may come in any order; `--` ends the options.

<!-- contract: commands -->
| command | flags |
|---|---|
| `kjak send` | `-t`/`--to`, `-T`/`--title`, `-l`/`--level`, `-u`/`--url`, `--stdin`, `--json-input`, `--template`, `--template-file`, `--var`, `--timeout`, `--max-retries`, `--concurrency`, `--dry-run`, `-q`/`--quiet`, `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak feather list` | `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak feather show` | `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak feather add` | `--type`, `-s`/`--scope`, `--force`, `--disabled`, `--levels`, `--secret-env`, `--secret-keychain`, `--secret-ref`, `--secret-stdin`, `--username`, `--host`, `--port`, `--security`, `--user`, `--from`, `--mail-to`, `--subject-prefix`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak feather remove` | `-s`/`--scope`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak feather enable` | `-s`/`--scope`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak feather disable` | `-s`/`--scope`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak feather types` | |
| `kjak feather plugins` | `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak group list` | `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak group show` | `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak group set` | `-s`/`--scope`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak group remove` | `-s`/`--scope`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak template list` | `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak template show` | `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak template set` | `--title`, `--body`, `--body-stdin`, `--level`, `--url`, `-s`/`--scope`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak template remove` | `-s`/`--scope`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak template render` | `--var`, `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak config path` | `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak config show` | `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak config validate` | `--strict`, `-o`/`--output`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak config init` | `--force`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak config trust` | `-y`/`--yes`, `--allow-user-secrets`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak config untrust` | `-y`/`--yes`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak config migrate-secrets` | `-s`/`--scope`, `--secrets-out`, `--config-out`, `--dry-run`, `-c`/`--config`, `--secrets`, `--no-project` |
| `kjak version` | |

`kjak --version`, `kjak -v` (the same as `kjak version`) and `kjak --help`,
`kjak -h`, `kjak help` are stable too. `kjak` alone prints a short help and
exits 2: there is no default command.

The bounds of the delivery flags (`--timeout` at most 10m, `--max-retries`
0–10, `--concurrency` 1–64) are stable; a value outside them is a usage
error (exit 2).

## Exit status

<!-- contract: exit-codes -->
| status | meaning |
|---|---|
| `0` | success: every attempted target was delivered (skipped targets do not count) |
| `1` | at least one delivery failed, including one whose acceptance is unknown |
| `2` | input, usage, configuration or preflight error: nothing was sent |
| `130` | interrupted by SIGINT, SIGTERM or SIGHUP (the report of what happened is still printed) |

A command other than `send` whose output cannot be written fails with 2; a
`send` keeps the status of its deliveries ([cli.md](cli.md#exit-status)).

## JSON output

Every `-o json` document has `"schema_version": 1`. Within schema version 1
fields are only **added**: a key below is never removed, renamed or given
another type, and a value of an enumeration below keeps its meaning. A
reader ignores keys it does not know. A change that cannot be made so is a
new schema version, in a major release.

### The report of `kjak send`

Key paths (`[]`: each element of an array):

<!-- contract: report-keys -->
| key | type |
|---|---|
| `schema_version` | number: 1 |
| `outcome` | string: an [outcome](#enumerations) |
| `canceled` | boolean |
| `dry_run` | boolean |
| `duration_ms` | number |
| `level` | string (a level) or null |
| `summary` | object |
| `summary.total` | number |
| `summary.delivered` | number |
| `summary.failed` | number (failed results, unknown acceptance included) |
| `summary.unknown` | number (failed results with acceptance `unknown`) |
| `summary.skipped` | number |
| `results` | array, one per target, in the order of the targets |
| `results[].feather` | string |
| `results[].type` | string: a feather type |
| `results[].groups` | array of strings (empty when named directly) |
| `results[].status` | string: a [status](#enumerations) |
| `results[].acceptance` | string: an [acceptance](#enumerations) |
| `results[].attempts` | number |
| `results[].duration_ms` | number |
| `results[].error` | object or null |
| `results[].error.code` | string: a [delivery error code](#delivery-error-codes) |
| `results[].error.message` | string (for people) |
| `results[].skipped` | object or null |
| `results[].skipped.reason` | string: a [skip reason](#enumerations) |
| `results[].skipped.group` | string |
| `results[].history` | array, one per attempt |
| `results[].history[].number` | number, from 1 |
| `results[].history[].started_at` | string, RFC 3339 UTC |
| `results[].history[].duration_ms` | number |
| `results[].history[].acceptance` | string: an acceptance |
| `results[].history[].retryable` | boolean |
| `results[].history[].status_code` | number: the HTTP status or SMTP reply code (absent without one) |
| `results[].history[].retry_after_ms` | number (absent when the provider asked for no wait) |
| `results[].history[].wait_ms` | number (absent when no wait followed the attempt) |
| `results[].history[].error` | object (absent without an error) |
| `results[].history[].error.code` | string: a delivery error code |
| `results[].history[].error.message` | string |
| `error` | null in a report; the [error](#the-error-document) of a send that failed before delivering anything |
| `error.code` | string: a [command error code](#command-error-codes) |
| `error.message` | string (for people) |

### The error document

With `-o json`, a command that fails before delivering anything (exit 2, or
130 when interrupted) prints one JSON document on stdout and its diagnostic
on stderr. `kjak send` prints the shape of its report (empty `results`,
`summary` of zeros, `level` null) with `error`; every other command prints:

<!-- contract: error-document-keys -->
| key | type |
|---|---|
| `schema_version` | number: 1 |
| `outcome` | string: `error` |
| `error` | object |
| `error.code` | string: a [command error code](#command-error-codes) |
| `error.message` | string (for people) |

A command line whose flags cannot be parsed (an unknown flag, a value of the
wrong type, an unknown `--output`) is not a JSON request yet: text on stderr,
nothing on stdout, exit 2.

### Other commands

The documents of `feather list|plugins`, `group list`, `template list` and
`config path|show|validate` carry `schema_version` 1 and are additive in the
same way. The documents of `feather show`, `group show`, `template show` and
`template render` are the object itself, without `schema_version`, as
v0.0.4 printed them (the same keys and values for the slack, discord and
email feathers, checked against the v0.0.4 binary); they are additive in
the same way. Their top-level keys, and the keys a document has only when
they are set:

`config validate -o json` prints its document only when the files are valid
(exit 0): `valid` is `true`, `global_exists` says whether the global file
exists, `project` and `project_state` are those of `config path`, and
`literal_secrets` lists every literal secret as `{"source": "global" |
"project", "key": "feathers.<name>.<field>"}` (deprecated; empty when
none). An invalid file, or a literal secret with `--strict`, is the
[error document](#the-error-document) (exit 2), as before.

In `config show -o json`, an entry of `groups` has the key `Feathers` and
an entry of `templates` the keys `Title`, `Body`, `Level` and `URL` (each
present, `""` when unset): the Go field names v0.0.4 printed, unlike the
lower-case names of the configuration file and of `group list|show` and
`template list|show`. They are kept for compatibility
(`TestContractConfigShowKeys`, `internal/e2e/compat_v004_test.go`); a
lower-case name may only be added beside them, never replace them, in 1.x.

<!-- contract: command-document-keys -->
| command | top-level keys | only when set |
|---|---|---|
| `kjak feather list` | `schema_version`, `feathers` | |
| `kjak feather show NAME` | `name`, `origin`, `type`, `enabled`, `levels` | `webhook_url`, `username`, `host`, `port`, `security`, `user`, `password`, `from`, `to`, `subject_prefix`, `url`, `method`, `auth`, `policy`, `plugin`, `path`, `sha256`, `secrets`, `env` |
| `kjak feather plugins` | `schema_version`, `plugins`, `skipped_path_elements` | |
| `kjak group list` | `schema_version`, `groups` | |
| `kjak group show NAME` | `name`, `origin`, `feathers` | |
| `kjak template list` | `schema_version`, `templates` | |
| `kjak template show NAME` | `name`, `origin`, `title`, `body`, `level`, `url` | |
| `kjak template render NAME` | `body`, `level` | `title`, `url` |
| `kjak config path` | `schema_version`, `global`, `global_source`, `project`, `project_state`, `secrets`, `secrets_source`, `secrets_state`, `project_secrets`, `project_secrets_state`, `secrets_permissions_checked`, `secrets_permission_model`, `trust_store` | |
| `kjak config show` | `schema_version`, `sources`, `defaults`, `feathers`, `groups`, `templates` | |
| `kjak config validate` | `schema_version`, `valid`, `global`, `global_exists`, `project`, `project_state`, `literal_secrets` | |

`feather show` and the entries of `feather list` are the same object: a
credential is shown by its source only (`env:NAME`, `secret:FILE.KEY`, …),
never its value. The keys only when set follow the feather's type: slack
and discord `webhook_url` (and discord `username`); email `host`, `port`,
`security`, `user`, `password`, `from`, `to`, `subject_prefix`; http `url`,
`method`, `auth` (`none`, or the scheme and the source of its secret) and
`policy` (what its address policy allows beyond public addresses);
external
`plugin`, `path`, `sha256`, `secrets` (`NAME from SOURCE`), `env`. `levels`
is `[]` when every level is sent. `template show` has every key, `""` when
unset; `template render` prints the rendered message, its `level` `info`
when the template sets none (`TestContractCommandDocuments`,
`TestContractShowDocuments`).

### Enumerations

<!-- contract: outcomes -->
| outcome | |
|---|---|
| `success` | no failed target (skipped targets allowed) |
| `partial` | some delivered, some failed |
| `failure` | none delivered, at least one failed |
| `canceled` | interrupted (SIGINT, SIGTERM, SIGHUP) |
| `error` | the command failed before delivering anything (the error document) |

<!-- contract: statuses -->
| status | |
|---|---|
| `delivered` | the provider confirmed it accepted the message |
| `failed` | not confirmed: see the acceptance |
| `skipped` | a group member left out before delivery (see `skipped`) |
| `planned` | a dry run: it would be delivered to |

<!-- contract: acceptances -->
| acceptance | |
|---|---|
| `confirmed` | the provider answered that it accepted the message |
| `rejected` | certainly not accepted: refused, or never fully sent |
| `unknown` | fully sent, no answer: it may have arrived; never retried |
| `none` | no attempt was made |

<!-- contract: skip-reasons -->
| skip reason | |
|---|---|
| `disabled` | the group member is disabled |
| `level_filter` | the group member's `levels` exclude the level |

<!-- contract: levels -->
| level | |
|---|---|
| `info` | the default |
| `success` | |
| `warning` | |
| `error` | |
| `critical` | |

## Error codes

### Delivery error codes

`results[].error.code` and `results[].history[].error.code`: why an attempt
or a delivery failed.

<!-- contract: delivery-error-codes -->
| code | meaning |
|---|---|
| `rejected` | the provider refused the message (permanent) |
| `rate_limited` | HTTP 429 |
| `server_error` | HTTP 5xx, SMTP 4xx |
| `network` | the request did not fully reach the provider |
| `ambiguous` | the request reached the provider, its answer was lost |
| `deadline` | the deadline of the send passed |
| `canceled` | interrupted (SIGINT, SIGTERM, SIGHUP; a SIGHUP ignored when kjak started, as with `nohup`, stays ignored) |
| `protocol` | an unexpected answer (a redirect, a malformed reply, a plugin's broken response) |
| `blocked` | kjak's own address policy refused the connection or a redirect: nothing was sent |
| `internal` | a failure inside kjak (a bug) |

An attempt interrupted or ended by the deadline **after its request was
sent** has acceptance `unknown`: its code is `ambiguous` for slack, discord,
email and http feathers, `canceled` or `deadline` for an external feather.
Before its request was sent (or for a delivery that never started) the code
is `canceled` or `deadline`. A script tells an interrupted send by the
report's `canceled`, not by these codes.

### Command error codes

`error.code` of the error document.

<!-- contract: command-error-codes -->
| code | meaning |
|---|---|
| `usage` | the command line, after its flags were parsed |
| `config` | a configuration file: syntax, validation, lock, concurrent change |
| `untrusted_project` | `./.kjak.toml` is not trusted |
| `routing` | a target: unknown, disabled, filtered out by its levels |
| `credential` | a secret that does not resolve or is not acceptable |
| `input` | the message or its input |
| `not_found` | a named feather, group or template |
| `deadline` | the deadline of the send passed while the credentials were resolved |
| `interrupted` | SIGINT or SIGTERM before any delivery (exit 130) |
| `error` | anything else |

A new code may be added in a minor release for a new kind of failure: a
reader treats an unknown code like `error` (command) or `internal`
(delivery).

## Configuration

The global file (`--config`, `$KJAK_CONFIG`, else
`$XDG_CONFIG_HOME/kjak/config.toml`) and the project file (`./.kjak.toml`)
have the same format: TOML, at most 1 MiB, regular files. Unknown and
duplicate keys are errors. Every key (`<name>`: a name you choose):

<!-- contract: config-keys -->
| key | type |
|---|---|
| `defaults` | table |
| `defaults.timeout` | duration string (`"10s"`), at most 10m |
| `defaults.max_retries` | integer 0–10 (default 2) |
| `defaults.concurrency` | integer 1–64 (default 5) |
| `defaults.level` | a level (default `info`) |
| `defaults.targets` | list of feather and group names |
| `feathers.<name>.type` | a [feather type](#feather-types) |
| `feathers.<name>.enabled` | boolean (default true) |
| `feathers.<name>.levels` | list of levels (default: all) |
| `feathers.<name>.webhook_url` | credential (slack, discord) |
| `feathers.<name>.username` | string (discord) |
| `feathers.<name>.host` | string (email) |
| `feathers.<name>.port` | integer (email) |
| `feathers.<name>.security` | `starttls`, `tls`, `none` (email) |
| `feathers.<name>.user` | string (email) |
| `feathers.<name>.password` | credential (email) |
| `feathers.<name>.from` | address (email) |
| `feathers.<name>.to` | list of addresses (email) |
| `feathers.<name>.subject_prefix` | string (email) |
| `feathers.<name>.url` | URL (http) |
| `feathers.<name>.url_secret` | credential (http) |
| `feathers.<name>.method` | `POST`, `GET`, `PUT`, `PATCH`, `DELETE` (http) |
| `feathers.<name>.headers` | table of strings (http) |
| `feathers.<name>.query` | table of strings (http) |
| `feathers.<name>.auth` | table (http) |
| `feathers.<name>.auth.type` | `none`, `bearer`, `basic`, `header` (http) |
| `feathers.<name>.auth.token` | credential (http, bearer) |
| `feathers.<name>.auth.username` | string (http, basic) |
| `feathers.<name>.auth.password` | credential (http, basic) |
| `feathers.<name>.auth.header` | string (http, header) |
| `feathers.<name>.auth.value` | credential (http, header) |
| `feathers.<name>.body` | table (http) |
| `feathers.<name>.body.format` | `json`, `text`, `none` (http) |
| `feathers.<name>.body.template` | template string (http) |
| `feathers.<name>.body.content_type` | string (http) |
| `feathers.<name>.timeout` | duration string: one attempt (http, external) |
| `feathers.<name>.success_status` | list of `"2xx"`, `"200"`, ... (http) |
| `feathers.<name>.max_response_bytes` | integer (http) |
| `feathers.<name>.allow_http` | boolean (http) |
| `feathers.<name>.allow_networks` | list of CIDR prefixes (http; slack, discord, email of a project) |
| `feathers.<name>.allow_hosts` | list of host names (http; slack, discord, email of a project) |
| `feathers.<name>.max_redirects` | integer (http) |
| `feathers.<name>.idempotency_key` | boolean (http) |
| `feathers.<name>.idempotency_header` | string (http) |
| `feathers.<name>.retry` | table (http, external) |
| `feathers.<name>.retry.policy` | `safe`, `never`, `always` (http); `never`, `plugin` (external) |
| `feathers.<name>.retry.max_attempts` | integer (http, external) |
| `feathers.<name>.plugin` | plugin name (external) |
| `feathers.<name>.path` | absolute path of the executable (external) |
| `feathers.<name>.sha256` | SHA-256 of the executable (external) |
| `feathers.<name>.secrets` | table of credentials (external) |
| `feathers.<name>.settings` | table of strings (external) |
| `feathers.<name>.env` | list of variable names (external) |
| `groups.<name>.feathers` | list of feather names (no nesting) |
| `templates.<name>.title` | template string |
| `templates.<name>.body` | template string (required) |
| `templates.<name>.level` | a level |
| `templates.<name>.url` | template string |

Which keys each type accepts, their defaults and their checks:
[configuration.md](configuration.md), [http.md](http.md),
[plugins.md](plugins.md). A message template is Go `text/template` with
string variables; `range`, `define`, `template` and `block` are refused (a
template is text with variables, not a program).

### Credentials

A credential is an inline table with exactly one source:

<!-- contract: credential-sources -->
| source | TOML |
|---|---|
| `env` | `{ env = "VAR" }` |
| `keychain` | `{ keychain = { service = "S", account = "A" } }` (macOS, Linux) |
| `secret` | `{ secret = "name.key" }`: an entry of a secrets file |
| `literal` | `{ literal = "..." }`: **deprecated** (warned about; `config validate --strict` refuses it) |

### Feather types

<!-- contract: feather-types -->
| type | sends to |
|---|---|
| `slack` | a Slack incoming webhook |
| `discord` | a Discord webhook |
| `email` | an SMTP server |
| `http` | any HTTP API ([http.md](http.md)) |
| `external` | a plugin, `kjak-feather-<plugin>` ([plugins.md](plugins.md)) |

These names are refused with an explanation (a configuration error, never a
silent no-op), and reserved for an OS-native notification feather:

<!-- contract: reserved-feather-types -->
| type |
|---|
| `os` |
| `native` |
| `macos` |
| `linux` |
| `windows` |

## Secrets files

The personal file (`--secrets`, `$KJAK_SECRETS`, else
`$XDG_CONFIG_HOME/kjak/secrets.toml`) and the project file
(`./.kjak.secrets.toml`) are TOML tables of strings; a `secret` reference
names a value by its dot-separated key path (`dev_slack.webhook_url`). Stable
in 1.x ([configuration.md](configuration.md#77-secrets-files),
[security.md](security.md#secrets-files)):

- names: segments of a letter or digit then letters, digits, `_`, `-` (at
  most 64), at most 8 segments and 255 bytes; values: strings, used as they
  are (nothing is expanded);
- at most 1 MiB, a regular file owned by the user that nobody else can read
  or change (mode bits, ACLs on Linux, macOS and Windows), checked on the
  opened file; a file that does not pass is refused, never read;
- the global configuration reads the personal file only; a project reads its
  own file, and the personal file only for the names its trust allows.

## Trust

- A project file (`./.kjak.toml`) is used only after `kjak config trust`;
  an untrusted one is an error (exit 2, `untrusted_project`) for every
  command that would use it, never ignored silently (`--no-project`
  ignores it on purpose).
- The trust is the canonical path and the SHA-256 of the project file and
  of its secrets file: any change withdraws it.
- A project reads `env` and `keychain` references, the personal secrets
  file and the environment of its plugins only for the names its trust
  allows (`user_secrets`); its plugins only as the executables trusted
  (`plugins`).
- A project's slack, discord and email feathers connect to public addresses
  only unless `allow_networks` / `allow_hosts` allow more; an http feather
  always has that policy.

The trust store (`$XDG_CONFIG_HOME/kjak/trust.toml`) is written by kjak and
read by every 1.x release; an entry of v0.0.4 is read too (it trusts the
same file, and a new trust adds the grants it lacks):

<!-- contract: trust-store-keys -->
| key | |
|---|---|
| `projects` | array of tables, one per trusted project file |
| `projects.path` | canonical path of the project file |
| `projects.sha256` | SHA-256 of its content |
| `projects.trusted_at` | when it was trusted |
| `projects.secrets_path` | canonical path of the project secrets file trusted with it |
| `projects.secrets_sha256` | SHA-256 of its content |
| `projects.user_secrets` | the names allowed: secrets (`name.key`), `env:NAME`, `keychain:SERVICE/ACCOUNT` |
| `projects.plugins` | array of tables: the executables of its external feathers |
| `projects.plugins.name` | plugin name |
| `projects.plugins.path` | the executable |
| `projects.plugins.sha256` | its SHA-256 |

## Plugin protocol v1

An external feather runs `kjak-feather-<plugin>` once per attempt: one JSON
request on standard input, one JSON response on standard output
([plugins.md](plugins.md#protocol-v1)). Version 1 is stable for all of kjak
1.x: kjak may **add** request fields (a plugin ignores what it does not
know) and accepts unknown response fields. A new protocol version would be
announced by `protocol_version` and `KJAK_PLUGIN_PROTOCOL`.

<!-- contract: plugin-request-keys -->
| request key | |
|---|---|
| `protocol_version` | `1` |
| `feather` | the feather's name |
| `plugin` | the plugin's name |
| `attempt` | 1, 2, ... within one send |
| `deadline` | RFC 3339 UTC: when kjak stops the plugin |
| `timeout_ms` | the feather's `timeout` |
| `idempotency_key` | the same for every attempt of one send |
| `message` | object |
| `message.title` | string (empty when not set) |
| `message.body` | string |
| `message.level` | a level |
| `message.url` | string (empty when not set) |
| `settings` | the feather's `settings` |
| `secrets` | only the feather's `secrets`, resolved |

<!-- contract: plugin-response-keys -->
| response key | |
|---|---|
| `protocol_version` | required: `1` |
| `status` | required: `delivered` or `failed` |
| `acceptance` | `confirmed`; `rejected` or `unknown` (the default of `failed`) |
| `retryable` | with `rejected`: may be retried (`retry.policy = "plugin"`) |
| `retry_after_ms` | the wait the provider asked for (capped at 24h) |
| `status_code` | the provider's status |
| `error` | object |
| `error.code` | `rejected`, `rate_limited`, `server_error`, `network`, `protocol`, `internal` |
| `error.message` | text for the report (cleaned, cut, redacted) |
| `message` | the same as `error.message` |

The process gets no argument, the temporary directory as its working
directory, `KJAK_PLUGIN_PROTOCOL=1` and only these variables of kjak's
environment (those that are set), besides the names of the feather's `env`:

<!-- contract: plugin-environment -->
| variable |
|---|
| `PATH` |
| `HOME` |
| `TMPDIR` |
| `TEMP` |
| `TMP` |
| `LANG` |
| `LC_ALL` |
| `LC_CTYPE` |
| `TZ` |
| `SYSTEMROOT` |
| `USERPROFILE` |

The plugin runs with no argument, in a new empty directory that only you
can write to, made for the attempt and removed after it. The `PATH` it gets
has only the absolute directories of kjak's `PATH` (as for finding the
plugin: an empty or relative element is left out). The response's keys are
the lower-case names of the table above; other spellings are not part of
the protocol.

Exit status 0: the response decides. Any other exit status, a crash, a kill
or a broken response is a failure with acceptance `unknown` (the plugin ran:
it may have delivered); a plugin that cannot be started is `rejected`.

## Retry semantics

Stable in 1.x ([delivery.md](delivery.md)):

- Only an attempt whose acceptance is `rejected` **and** that is retryable
  is retried. An `unknown` acceptance is **never** retried, whatever a
  feather or a plugin says: a retry could deliver it twice.
- At most `max_retries` retries after the first attempt (`--max-retries`,
  `defaults.max_retries`); `retry.max_attempts` of an http or external
  feather caps it lower.
- An http feather retries `POST` and `PATCH` only with
  `idempotency_key = true` (`retry.policy = "safe"`, the default) or
  `retry.policy = "always"`; an external feather only with
  `retry.policy = "plugin"`. Slack, Discord and e-mail retry their transient
  refusals (429, 5xx except 502/504, SMTP 4xx, a connection that failed
  before the request was sent).
- A provider's `Retry-After` (Discord: `retry_after`) is honored, capped at
  24h. Without one, the wait is an exponential backoff with jitter.
- `timeout` is the deadline of the **whole send** (credentials, attempts,
  waits): a wait that does not fit is not started, and no delivery starts
  after it.
- Every feather is delivered on its own, at most `concurrency` at a time;
  the results follow the order of the targets.

<!-- contract: delivery-bounds -->
| setting | default | bound |
|---|---|---|
| `timeout` | `10s` | `10m` |
| `max_retries` | `2` | `10` |
| `concurrency` | `5` | `64` |
| `retry_after` | | `24h` |

## Platforms

| OS | architectures | release archive | notes |
|---|---|---|---|
| Linux | amd64, arm64 | `tar.gz` | keychain: a Secret Service (GNOME Keyring, KWallet); secrets files: mode bits and POSIX ACLs |
| macOS | amd64, arm64 | `tar.gz`, Homebrew | keychain: the login keychain; secrets files: mode bits and ACLs |
| Windows | amd64 | `zip` | no keychain (use `env` or a secrets file); secrets files: owner and DACL; plugins in a Job Object |

The tests run natively on each of them in CI (windows/arm64 is
cross-compiled only and not released; the race detector runs on Linux and
macOS). The keychain support is tested with the commands kjak runs
(`security`, `secret-tool`) replaced by test doubles, not against a real
keychain; the Homebrew formula installs on macOS and Linux.
