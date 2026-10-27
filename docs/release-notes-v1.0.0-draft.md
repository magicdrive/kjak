<!--
DRAFT of the notes of the v1.0.0 release. Not used by the release workflow
yet: hephaestus.yaml sets `release.github.generate_notes: true` and no
`notes_file`, so a release gets GitHub's generated notes.

Plan for v1.0.0: the release preparation PR copies this text, without this
comment, to a file that names the release (for example
docs/release-notes/v1.0.0.md), sets `release.github.notes_file` to it and
removes `generate_notes` (Hephaestus ignores it when `notes_file` is set,
and `notes_file` is mutually exclusive with `notes`). `make preflight
tag=v1.0.0` then fails unless that file names the tag
(config.release-notes), so a later release cannot reuse it unchanged.
-->

# kjak v1.0.0

kjak sends one notification to Slack, Discord, e-mail, any HTTP API or a
plugin of your own, from the command line, a script or CI. v1.0.0 is the
first stable release: the command line, the configuration, the JSON output,
the exit status, the error codes and the plugin protocol are now a contract
kept for all of 1.x ([docs/stability.md](https://github.com/magicdrive/kjak/blob/v1.0.0/docs/stability.md)).

## Highlights

- **Stable contract.** [docs/stability.md](https://github.com/magicdrive/kjak/blob/v1.0.0/docs/stability.md) lists what 1.x
  guarantees (commands and flags, configuration and secrets files, trust
  store, JSON schema version 1, error codes, exit status, retry semantics,
  plugin protocol v1), what it does not (text output, messages, provider
  payloads), and the deprecation policy. Tests check it against the code.
- **Secrets files.** Secrets live in a private TOML file of their own
  (`{ secret = "dev_slack.webhook_url" }`), personal (`--secrets`,
  `$KJAK_SECRETS`, `~/.config/kjak/secrets.toml`) or per project
  (`.kjak.secrets.toml`, trusted with the project). kjak refuses a secrets
  file anyone else can read, checking the mode bits and the ACLs on Linux,
  macOS and Windows. `kjak config migrate-secrets` moves literal secrets out
  of a configuration file without changing it.
- **`http` feathers.** Any HTTP API: method, headers, query, bearer/basic/
  header authentication by reference, a JSON or text body or a template,
  success statuses, idempotency keys and method-aware retries, behind an
  address policy (https only, no redirects, no loopback, private,
  link-local or cloud-metadata address unless allowed; checked where the
  connection is made).
- **External feather plugins.** `kjak-feather-<name>` executables speaking
  a documented JSON protocol (v1) on stdin/stdout; kjak checks which
  executable runs, gives it only its own secrets and a minimal environment,
  and ends its whole process tree (process group on Unix, Job Object on
  Windows) at its timeout, the deadline and Ctrl-C. `kjak feather plugins`
  lists them without running any.
- **Project trust, tightened.** A trusted `.kjak.toml` reads your
  environment variables, keychain entries and personal secrets only by the
  names you allowed when you trusted it, and its slack, discord and email
  feathers reach public addresses only unless the file allows more.
- **Quality.** golangci-lint, fuzzing, mutation testing, end-to-end tests of
  the built binary (and of v0.0.4 configurations), native CI on every
  release platform.

## Breaking changes

Compared to v0.0.4. Each is a security fix or a fix of the output contract;
the [migration guide](https://github.com/magicdrive/kjak/blob/v1.0.0/docs/migration.md) has the steps.

- **Project feathers need a new trust for `env` and `keychain`
  references.** A project trusted by v0.0.4 stays trusted, but its slack,
  discord and email feathers that read an environment variable or a
  keychain entry are refused (exit 2, nothing sent) until you trust the
  project again and allow those names (`kjak config trust`, or
  `--yes --allow-user-secrets` in CI).
- **A project's slack, discord and email feathers reach public addresses
  only, without a proxy.** A project feather that sends to `localhost`, a
  private network or a private SMTP server fails with `blocked` until the
  project allows it (`allow_networks`, `allow_hosts`). Global feathers are
  not affected.
- **Message templates cannot loop.** `range`, `define`, `template` and
  `block` are refused in a message template (`template set`, and when a
  template is rendered).
- **Configuration files must be regular files.** A configuration file, a
  project file or the trust store that is a FIFO, a device or a directory is
  refused (a symbolic link to a regular file still works).
- **`config validate -o json` prints a JSON document on success**
  (`"valid": true`, the files, the literal secrets); v0.0.4 printed the
  `PATH: OK` text lines there. Failures print the same error document as
  before, and the exit status is unchanged.
- **Downgrade:** once a trust entry uses the new keys (a project secrets
  file, allowed names, plugins), v0.0.4 cannot read the trust store
  ("broken trust store"). Keep a copy of `trust.toml` if you may go back.

Unchanged: configurations without literals, `env` and `keychain`
references of the global configuration, groups, templates without loops,
the JSON report (fields were only added), the exit status, the request
bodies sent to Slack, Discord and e-mail.

## Deprecated

- `{ literal = "..." }` credentials and `feather add --secret-stdin`. They
  keep working in 1.x; every command that loads one warns on stderr, and
  `kjak config validate --strict` refuses them. Move them with
  `kjak config migrate-secrets`.

## Migration

1. Install v1.0.0 (`brew upgrade kjak`, or the archive of the release).
2. `kjak config validate` (and `--strict` to find literals);
   `kjak config migrate-secrets` if you have any, then rotate the secrets
   that were ever committed or shared.
3. In each repository with a `.kjak.toml`: review it, then
   `kjak config trust` again if a send says the trust does not allow a
   reference; add `allow_networks`/`allow_hosts` to project feathers that
   send to a local or private address.
4. Replace `range`/`define`/`template`/`block` in message templates.

Details: [docs/migration.md](https://github.com/magicdrive/kjak/blob/v1.0.0/docs/migration.md).

## Security

- Secrets: separate private files, checked on the opened file (owner, mode,
  ACLs; fail closed), never printed; redaction of every error and report
  now also covers fragments of a secret cut by a truncation and multi-line
  secrets.
- Projects: every `env`/`keychain`/personal secret reference and plugin
  environment variable is allowed by name; an address policy for every
  network feather of a project; plugins pinned by the trust.
- Untrusted text: terminal escape sequences in a project file are refused
  or escaped before they reach your terminal (`config trust` shows what you
  approve).
- SSRF: metadata services (AWS, GCP, Azure WireServer, Tencent, ...),
  IPv4-in-IPv6 and NAT64 forms are refused; DNS rebinding is covered by
  checking the address where the connection is made.
- Denial of service: a FIFO as a configuration file and looping templates
  are refused.

Report a vulnerability privately: [SECURITY.md](https://github.com/magicdrive/kjak/blob/v1.0.0/SECURITY.md).

## Platform notes

| platform | archive | notes |
|---|---|---|
| macOS arm64, amd64 | `tar.gz`, Homebrew | keychain (`security`); secrets files: mode bits and ACLs (an allow entry, even your own, is refused: `chmod -N`) |
| Linux amd64, arm64 | `tar.gz` | keychain through `secret-tool` (a Secret Service must run); secrets files: mode bits and POSIX ACLs (NFSv4 ACLs are not read) |
| Windows amd64 | `zip` | no keychain (use `env` or a secrets file); secrets files: owner and DACL (a file under a folder made in `C:\` inherits `Authenticated Users` and is refused); plugins run in a Job Object |

windows/arm64 is built and vetted in CI but not released. Every archive is
attested (`gh attestation verify`) and listed in `kjak_checksums.txt`.

## Known issues

- OS-native notifications are not supported (the type names `os`, `native`,
  `macos`, `linux`, `windows` are reserved and refused).
- Windows has no keychain support.
- Commands that change a configuration file (`feather add`, `group set`,
  ...) rewrite it without its comments, and keep its mode (a file you made
  readable by others stays so: a literal secret in it is only warned
  about).
- `config validate -o json` prints JSON only for an error; a success is
  text.
- Plugins are not sandboxed: they run with your rights. A descendant that
  leaves the process group (`setsid`) or the Job Object (through a service)
  is not ended by kjak.
- The Discord end-to-end check against a real webhook is manual
  ([docs/discord-e2e.md](https://github.com/magicdrive/kjak/blob/v1.0.0/docs/discord-e2e.md)); CI uses local test servers.
