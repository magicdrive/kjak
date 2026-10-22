# Credentials, trust and security

## Credentials

A feather's secret — a Slack or Discord webhook URL (the URL is the
credential), an SMTP password — is a reference resolved when a send runs:

| source | example | resolved from |
|---|---|---|
| `env` | `{ env = "KJAK_DEV_SLACK_WEBHOOK" }` | the environment variable (must be set and non-empty) |
| `keychain` | `{ keychain = { service = "kjak", account = "dev_slack" } }` | the OS keychain (below) |
| `literal` | `{ literal = "https://hooks.slack.com/..." }` | the file itself |

Exactly one source per credential; two are an error. Prefer `env` or
`keychain`: a `literal` puts the secret in the file (kjak writes its files with
mode 0600, but backups and repositories may not).

A credential that does not resolve is a preflight error (exit 2): nothing is
sent to any target.

### Where secrets never appear

- Output: `feather show`, `feather list`, `config show` and `config trust`
  print the source (`env:NAME`, `keychain:service/account`,
  `literal:[REDACTED]`), never the value.
- Errors and reports: transport errors are described without the URL (Go's
  `*url.Error` quotes the URL, which is the webhook's secret), and every
  resolved secret — and its URL path and query — is removed from every error
  and result message before it is printed, including text a provider echoes
  back.
- Configuration files are read up to 1 MiB, so an untrusted project file
  cannot exhaust memory before its trust is checked; TOML errors give the
  position, never the value.
- Command lines: `feather add` takes the secret by reference, or from
  standard input (`--secret-stdin`), never as an argument.

## OS keychain

kjak **reads** the keychain; it never writes it. The tools that would write
an entry without cgo take the secret as a command-line argument, visible to
other processes, so creating the entry is left to you.

| OS | backend | create an entry |
|---|---|---|
| macOS | `/usr/bin/security` (login keychain) | `security add-generic-password -s kjak -a dev_slack -w` (prompts for the secret; do not put it on the command line) |
| Linux | `secret-tool` (libsecret: GNOME Keyring, KWallet via Secret Service) | `secret-tool store --label "kjak dev_slack" service kjak account dev_slack` (reads the secret from standard input) |
| Windows | **not supported in v0.1.0** | use `env` |

On Windows a `keychain` reference is a preflight error that says so: it is
never treated as an empty secret. (The Windows Credential Manager API needs
either cgo or a syscall binding kjak does not ship yet.) On Linux,
`secret-tool` must be installed (`libsecret-tools`) and a Secret Service must
run (a desktop session); headless servers should use `env`. On macOS the
first read may show a dialog asking to allow `security` access.

## Project configuration trust

A `.kjak.toml` can name environment variables, keychain entries and the URLs
they are sent to. A cloned repository could therefore make `kjak send` send
your secrets anywhere. kjak uses a project file only after you trust that
exact file:

```sh
kjak config trust
```

```
project configuration /home/me/src/app/.kjak.toml
sha256 3f1c...e9
  feather app_slack: slack, webhook URL from env:APP_SLACK_WEBHOOK
  group app: app_slack
Trust this exact content? [y/N]
```

- The trust is the file's **canonical path** (absolute, symbolic links
  resolved) and the **SHA-256 of its content**, stored in
  `$XDG_CONFIG_HOME/kjak/trust.toml`. The hash trusted is the hash of the
  content that was shown.
- Any change of the content withdraws the trust; so does a different path.
- An untrusted project file is an error for every command that would use it
  (exit 2) — never used silently, never ignored silently. Pass `--no-project`
  to run without it.
- Without a terminal, `kjak config trust` requires `--yes`: review the file
  first.
- `kjak config untrust` removes the entry; `kjak config path` shows the state
  (`trusted`, `untrusted`, `untrusted (changed since it was trusted)`).
- kjak edits a project file only when it is trusted (or new), and then trusts
  the content it wrote itself.

## Transport security

- Webhooks: the URL is the credential, so it must be `https` (TLS 1.2+);
  `http` is accepted for `localhost` / loopback addresses only, and a URL with
  user information is refused — for literals when the file is read, for `env`
  and `keychain` values when they are resolved. Redirects are never followed
  (a redirected POST would carry the message elsewhere).
- E-mail: `starttls` and `tls` verify the server certificate; `starttls`
  fails if STARTTLS is not offered; a password is never sent with
  `security = "none"`. Clear-text data that arrives with the reply to
  STARTTLS (a response injection) ends the session. The text of an AUTH
  refusal is never shown (a server may repeat the encoded password); at most
  1 MiB of replies is read per session.
- Message content: Slack text is escaped and Discord mentions are disabled,
  so a message cannot ping `@channel`/`@everyone`; a title cannot contain a
  line break (it becomes an e-mail header).

## Reporting a vulnerability

Please report security issues privately through GitHub's "Report a
vulnerability" (Security advisories) of magicdrive/kjak, not in a public
issue.
