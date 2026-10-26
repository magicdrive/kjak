# Migrating literal secrets to a secrets file

(For the change of the project trust in 0.0.5, see
[Project env and keychain references need a grant](#project-env-and-keychain-references-need-a-grant).)

[Configuration](configuration.md#77-secrets-files) · [Security](security.md#secrets-files)

A `{ literal = "..." }` credential keeps the secret in the configuration
file. Configuration files are copied, shared, committed and backed up; a
secrets file is a file of its own, private (mode 0600), that a configuration
refers to by name:

```toml
# before: ~/.config/kjak/config.toml
[feathers.dev_slack]
type = "slack"
webhook_url = { literal = "https://hooks.slack.com/services/T000/B000/XXXX" }
```

```toml
# after: ~/.config/kjak/config.toml
[feathers.dev_slack]
type = "slack"
webhook_url = { secret = "dev_slack.webhook_url" }

# ~/.config/kjak/secrets.toml (mode 0600)
[dev_slack]
webhook_url = "https://hooks.slack.com/services/T000/B000/XXXX"
```

Literals keep working in this version. `kjak send` warns on stderr for each
file that holds one; `kjak config validate --strict` refuses them (use it in
CI to keep new literals out).

## With `kjak config migrate-secrets`

```sh
kjak config migrate-secrets --dry-run     # what would move: names only, no file written
kjak config migrate-secrets
```

```
moved feathers.dev_slack.webhook_url -> { secret = "dev_slack.webhook_url" }
created /home/me/.config/kjak/secrets.toml (mode 0600): 1 secret(s)
created /home/me/.config/kjak/config.toml.migrated: the configuration with secret references
kjak: warning: /home/me/.config/kjak/config.toml still holds the 1 literal secret(s); it was not changed.
next:
  1. review /home/me/.config/kjak/config.toml.migrated, then move it over /home/me/.config/kjak/config.toml
  2. check: kjak config validate --strict && kjak send --to NAME --dry-run test
  rotate every secret that was ever committed, shared or backed up in the old file
```

What it does, and does not do:

- Every literal of the file becomes `{ secret = "<feather>.<key>" }` (a dot
  in a feather name becomes `_`: `dev.slack` -> `dev_slack.webhook_url`).
- The secrets go to a **new** file created with mode 0600 and `O_EXCL`:
  `--secrets-out FILE`, else the secrets file of the scope (the personal
  file — `--secrets`, `$KJAK_SECRETS` or `<config dir>/secrets.toml` — for
  the global file; `./.kjak.secrets.toml` with `--scope project`). An
  existing file, or a symbolic link, is never overwritten or merged: the
  command stops (pass `--secrets-out`, or merge by hand).
- The configuration goes to a **new** file, `--config-out FILE` or
  `<file>.migrated` (mode 0600). The original file is **not changed**: it
  still holds the literals until you replace it. kjak says so on stderr.
- If the configuration cannot be written, the secrets file it just created
  is removed: nothing is left half done.
- Values are never printed.

Then:

1. Review `<file>.migrated` and move it over the original (`mv`). Comments
   and key order are not kept (as with every file kjak writes).
2. `kjak config validate --strict` and a `kjak send --dry-run`.
3. Delete the backups of the old file, and **rotate** every secret that was
   committed, shared or synced while it was a literal: moving it does not
   un-leak it.

### A project file

```sh
kjak config migrate-secrets --scope project
mv .kjak.toml.migrated .kjak.toml
echo .kjak.secrets.toml >> .gitignore
kjak config trust          # the project file and its new secrets file
```

The project secrets file is trusted with the project file: after it is
created, `kjak config trust` is needed. A committed literal is in the
repository's history: rotate it.

### Somewhere else (CI)

```sh
kjak config migrate-secrets --config ci.toml --secrets-out ci-secrets.toml --config-out ci.new.toml
kjak send --config ci.toml --secrets ci-secrets.toml ...   # or KJAK_SECRETS=ci-secrets.toml
```

## By hand

1. Create the secrets file private from the start:
   `install -m 600 /dev/null ~/.config/kjak/secrets.toml` (Unix).
2. Add the secrets as `[table] key = "value"` (names: letters, digits,
   `_`, `-`, separated by dots).
3. Replace each literal with `{ secret = "table.key" }`, or re-add the
   feather with `kjak feather add NAME --type ... --secret-ref table.key
   --force`.
4. `kjak config validate --strict`.

## Message templates cannot loop

Since 0.0.5 a message template (`[templates.*]`, `--template-file`) refuses
`range` and `define`/`template`/`block`, like an http body template. Their
variables are strings, which `range` cannot iterate, so the only loops were
over numbers (`{{range 1000000000}}`), which could keep kjak busy without
end. A template that uses one is refused by `kjak template set` and by its
render; write the text out instead.

## Configuration files must be regular files

Since 0.0.5 a configuration file, a `.kjak.toml` or the trust store that is
a FIFO, a device or a directory (or a link to one) is refused (`it is not a
regular file`) instead of being read. A symbolic link to a regular file is
still followed.

## Compatibility

- Nothing changes for configurations without literals; `env` and `keychain`
  references work as before.
- The trust store gets new keys (`secrets_path`, `secrets_sha256`,
  `user_secrets`, `plugins`) only in entries that use them; an earlier kjak refuses a
  trust store with such an entry ("broken trust store"). Entries without
  them are written as before.
- `--secret-stdin` still writes a literal (and warns): prefer `--secret-ref`.

## Project env and keychain references need a grant

Since 0.0.5 the `env` and `keychain` references of a project's **slack,
discord and email** feathers must be allowed by name when the project is
trusted, like those of its http and external feathers (before, trusting the
file allowed them). Each of these feathers sends to a destination the
project chooses — an SMTP host receives the password, a webhook URL
receives the message — so a repository could otherwise hand one of your
environment variables to a server of its choosing.

A project trusted by an earlier kjak keeps its trust, but its grant does not
name these references: `kjak send` refuses such a feather before anything is
sent (exit 2), naming the reference:

```
kjak: feather "mail": password: credential error: env:SMTP_PASSWORD: the trust of the project does not allow its email feather "mail" to send env:SMTP_PASSWORD: review the project, then run `kjak config trust` and allow it (--allow-user-secrets with --yes)
```

To migrate, review the project file — its hosts, webhook URLs and the
references it reads — then trust it again:

```sh
kjak config trust                               # interactive: answer the second question
kjak config trust --yes --allow-user-secrets    # CI, after the review
```

Your global configuration is not affected: its feathers are yours.

## A project's slack, discord and email feathers reach public addresses only

Since 0.0.5 the slack, discord and email feathers of a **project** file
connect through the address policy of the http feathers: public addresses
only, no proxy. A project feather that sends to a local relay, a test
server or a private SMTP server is refused with the code `blocked`
(nothing is sent):

```
FAIL  mail (email) failed (1 attempt): connect: address 10.0.0.25 is private: allow it with allow_networks or allow_hosts
```

Review the destination, then allow it in the project file and trust the
project again:

```toml
[feathers.mail]
type = "email"
host = "smtp.corp.example"
allow_hosts = ["smtp.corp.example"]     # or allow_networks = ["10.0.0.0/24"]
```

A project webhook feather behind a mandatory HTTP proxy cannot use the
proxy any more: define it in your global file. Global feathers are not
affected.
