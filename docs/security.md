# Credentials, trust and security

## Credentials

A feather's secret — a Slack or Discord webhook URL (the URL is the
credential), an SMTP password — is a reference resolved when a send runs:

| source | example | resolved from |
|---|---|---|
| `env` | `{ env = "KJAK_DEV_SLACK_WEBHOOK" }` | the environment variable (must be set and non-empty) |
| `keychain` | `{ keychain = { service = "kjak", account = "dev_slack" } }` | the OS keychain (below) |
| `secret` | `{ secret = "dev_slack.webhook_url" }` | a secrets file (below) |
| `literal` | `{ literal = "https://hooks.slack.com/..." }` | the file itself (deprecated) |

Exactly one source per credential; two are an error. Prefer `env`,
`keychain` or `secret`: a `literal` puts the secret in the configuration
file (kjak writes its files with mode 0600, but backups and repositories may
not). `kjak send` warns about literals, `kjak config validate --strict`
refuses them, and `kjak config migrate-secrets` moves them to a secrets file
([migration.md](migration.md)).

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

## Secrets files

Secrets live apart from the configuration, in TOML files of their own
([configuration.md](configuration.md#77-secrets-files)):

| file | chosen | serves |
|---|---|---|
| personal | `--secrets FILE`, else `$KJAK_SECRETS` (explicit), else `<config dir>/secrets.toml` (user) | the global configuration; the project configuration for the names its trust allows |
| project | `./.kjak.secrets.toml` (current) | the project configuration only, trusted with it |

What kjak checks, and why:

- **Private files (Unix).** A secrets file must be a regular file owned by
  the effective user, with no permission bit for group or others (any of
  `0o077`); otherwise it is refused before it is parsed (exit 2). The file is
  opened first and the checks are made on the open descriptor (`fstat`), so
  a file swapped between a check and the read is not read: there is no
  separate check of the path. FIFOs and devices are refused without waiting
  (`O_NONBLOCK`). The size is limited to 1 MiB.
- **Links.** The project file is opened with `O_NOFOLLOW`: a repository
  could ship `.kjak.secrets.toml` as a symbolic link to your personal file,
  which would make your secrets look like the project's. For the same
  reason it must have a single name (no hard link: another user of the
  machine could make one in a directory you work in) and must not be the
  personal file itself (checked with the file's identity). The personal
  file may be a symbolic link (dotfiles); the file it leads to is checked. kjak does
  not check the directories above a secrets file: keep them yours.
- **Windows.** The access control list is not read (not implemented in this
  version): only "a regular file" and the size are checked, and a project
  file that is a link or reparse point is refused by a check of the path
  made before the open (a link created in between is not seen). Keep the
  personal file under `%AppData%`, whose default ACL is private to the user.
  `kjak config path` says when permissions are not checked.
- **Nothing is expanded.** A secret is used as it is: `${VAR}`,
  `{ env = ... }`, `{{...}}` in a value, in a message or in a template are
  text. A secret is never substituted into a string.
- **Errors name keys, not values.** A broken secrets file is reported by
  line, column or key; values are registered with the redactor when they are
  resolved, like any other secret.

### The trust boundary of a project

A cloned repository controls `./.kjak.toml` and `./.kjak.secrets.toml`. The
rules that keep your secrets from it:

1. The project configuration is used only once trusted, as before.
2. The project secrets file is part of that trust (its canonical path and
   SHA-256; `config trust` shows the names it defines, never the values). A
   project secrets file that appears, changes or disappears after the trust
   withdraws it, so it cannot be swapped after your review.
3. The global configuration never reads the project secrets file: a
   repository cannot redirect your own feathers by defining the names they
   use.
4. The project configuration reads your personal secrets file only for the
   names you allowed: `config trust` lists every name the project would read
   from your file and asks a separate question; `--yes` needs
   `--allow-user-secrets` for them. The allowed names are stored in the trust
   entry (`user_secrets`). Any other name is an error when it is resolved,
   even in a trusted project. kjak's own edits of a trusted project file
   (`feather add --scope project`) keep the allowed names the new content
   still uses and allow no new one.
5. `env` and `keychain` references of a project's slack, discord and email
   feathers are, as before, shown by `config trust` and allowed by trusting
   the file: reviewing them is your part. Those of a project's **http**
   feathers — which send to a URL the project chooses — are allowed by name
   like the personal secrets above (`env:NAME`, `keychain:SERVICE/ACCOUNT`
   in `user_secrets`); any other is refused when it is resolved.

An allowed name means "this project may send this secret to the
destinations it configures": review those destinations before you allow it.

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
  (`trusted`, `untrusted`, `untrusted (changed since it was trusted)`, or
  untrusted because of the project secrets file).
- The project secrets file and the personal secrets the project may read
  are part of the trust: see [The trust boundary of a project](#the-trust-boundary-of-a-project).
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
- HTTP feathers: see [HTTP feathers and SSRF](#http-feathers-and-ssrf).
- Message content: Slack text is escaped and Discord mentions are disabled,
  so a message cannot ping `@channel`/`@everyone`; a title cannot contain a
  line break (it becomes an e-mail header).

## HTTP feathers and SSRF

An `http` feather sends to a URL of the configuration, a trusted project's
included. Without limits, that would let a configuration reach what only
this machine can reach: its own services, the cloud metadata endpoint
(which hands out credentials), the private network. kjak's rules
([http.md](http.md#the-address-policy-ssrf)):

- `https` only, unless `allow_http = true`; no user information in a URL
  (refused, never sent); TLS 1.2 or later, certificates verified, no option
  to skip that.
- Every connection — redirects included — goes through one dialer: the host
  is resolved once, every address is classified, and the connection is made
  to a checked address (no second lookup: DNS rebinding cannot swap the
  address after the check); the socket's address is checked again before
  `connect(2)`; only TCP (no Unix socket).
- Refused by default: loopback, link-local, private networks (RFC 1918,
  CGNAT, ULA, ...), IPv6 transition addresses (NAT64, 6to4, Teredo,
  classified by the IPv4 address they carry), IPv4-mapped forms of all of
  them. `allow_networks` (CIDR) allows them explicitly; `allow_hosts` allows
  private addresses for named hosts. **Never** allowed: the cloud metadata
  addresses (169.254.169.254, fd00:ec2::254, 169.254.170.2, 100.100.100.200,
  192.0.0.192), unspecified, multicast, broadcast and reserved addresses.
- Proxy environment variables are ignored by http feathers (the guard would
  see the proxy's address, not the destination's); an http feather cannot
  use a proxy.
- Redirects are not followed unless `max_redirects` allows them; then each
  is checked like the URL, https never goes to http, and `Authorization`,
  `Cookie` and the `[auth]` header are dropped as soon as the scheme, host
  or port changes; no `Referer` is ever sent.
- Secrets go only where `[auth]` puts them: never in a query, never in a
  static header, never in a template. Tokens, passwords, the encoded basic
  credentials, header values and a secret URL (with its path and query) are
  redacted from errors and reports, including a response that echoes them.
- The request body of a JSON template is built by encoding: a message
  cannot add keys or break out of a string (template injection). A message
  is never parsed as a template.
- `config trust` shows a project's http feathers with their method, URL or
  its source, authentication source and every `allow_*`/redirect setting.

Limits: a host name allowed with `allow_hosts` is trusted for every private
address its DNS returns. The guard cannot see through an HTTP proxy, which
is why none is used. Responses are read up to `max_response_bytes`.

## Reporting a vulnerability

Please report security issues privately through GitHub's "Report a
vulnerability" (Security advisories) of magicdrive/kjak, not in a public
issue.
