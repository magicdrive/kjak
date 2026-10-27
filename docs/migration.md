# Migrating from v0.0.4 to v1.0.0

[Stability](stability.md) · [Configuration](configuration.md#77-secrets-files) · [Security](security.md#secrets-files)

v1.0.0 reads the configuration files, the groups, the templates and the
trust store of v0.0.4, and sends the same requests to Slack, Discord and
e-mail (`internal/e2e/compat_v004_test.go` checks this against files
written by v0.0.4). A few changes are intended: they close security issues
or freeze the output contract. Each is refused **before anything is sent**
(exit 2, or `blocked` for an address), with a message that names the fix.

## Checklist

1. **Install v1.0.0** (`brew upgrade kjak`, or the archive of the release)
   and keep a copy of `~/.config/kjak/trust.toml` if you may go back to
   v0.0.4 ([Downgrading](#downgrading-to-v004)).
2. **Validate:** `kjak config validate`. It reports literal secrets
   (deprecated, still working); `kjak config validate --strict` refuses
   them.
3. **Move literal secrets** to a secrets file:
   `kjak config migrate-secrets` ([below](#literal-secrets-deprecated)),
   then rotate every secret that was ever committed or shared.
4. **Re-trust each project** (`.kjak.toml`) whose slack, discord or email
   feathers read `env` or `keychain` references: review it, then
   `kjak config trust` ([details](#project-env-and-keychain-references-need-a-grant)).
5. **Allow local or private destinations** of project slack, discord and
   email feathers with `allow_networks` / `allow_hosts`, and trust again
   ([details](#a-projects-slack-discord-and-email-feathers-reach-public-addresses-only)).
6. **Rewrite looping templates** (`range`, `define`, `template`, `block`)
   ([details](#message-templates-cannot-loop)).
7. **Scripts:** a configuration file that is a FIFO or a device is refused
   ([details](#configuration-files-must-be-regular-files)). The JSON
   documents keep their keys
   ([`config show -o json`](#config-show--o-json-keys)).
8. Check: `kjak send --to NAME --dry-run test`, then a real send.

| change | affects | what you see | fix |
|---|---|---|---|
| literal secrets deprecated | `{ literal = ... }`, `--secret-stdin` | a warning on stderr | `kjak config migrate-secrets` |
| project `env`/`keychain` need a grant | project slack, discord, email feathers | exit 2, `credential`: "the trust of the project does not allow ..." | `kjak config trust` |
| project address policy | project slack, discord, email feathers | `blocked`, nothing sent | `allow_networks`, `allow_hosts` |
| templates cannot loop | message templates | exit 2 at `template set`, render or send | write the text out |
| regular files only | configuration, project file, trust store | exit 2, "not a regular file" | use a regular file |

Not changed: configurations without literals, the `env` and `keychain`
references of the global configuration, groups, the JSON report of `send`
(fields were only added), the exit status, the request bodies.

## Literal secrets (deprecated)

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

Literals keep working in all of v1 ([deprecation policy](stability.md#deprecation-policy)). `kjak send` warns on stderr for each
file that holds one; `kjak config validate --strict` refuses them (use it in
CI to keep new literals out).

When the file is also readable by other users (for example mode 0644, or a
project file in a repository), kjak warns that it "keeps literal secrets and
is not private" ([security](security.md#credentials)). Treat those secrets
as disclosed:

1. `kjak config migrate-secrets` (the new secrets file is created private);
2. make the configuration file private (`chmod 600 FILE`; on Windows remove
   the entries for other users with `icacls`), or replace it with the
   migrated one;
3. rotate each secret at its provider (a new webhook URL, a new password).

### With `kjak config migrate-secrets`

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

#### A project file

```sh
kjak config migrate-secrets --scope project
mv .kjak.toml.migrated .kjak.toml
echo .kjak.secrets.toml >> .gitignore
kjak config trust          # the project file and its new secrets file
```

The project secrets file is trusted with the project file: after it is
created, `kjak config trust` is needed. A committed literal is in the
repository's history: rotate it.

#### Somewhere else (CI)

```sh
kjak config migrate-secrets --config ci.toml --secrets-out ci-secrets.toml --config-out ci.new.toml
kjak send --config ci.toml --secrets ci-secrets.toml ...   # or KJAK_SECRETS=ci-secrets.toml
```

### By hand

1. Create the secrets file private from the start:
   `install -m 600 /dev/null ~/.config/kjak/secrets.toml` (Unix).
2. Add the secrets as `[table] key = "value"` (names: letters, digits,
   `_`, `-`, separated by dots).
3. Replace each literal with `{ secret = "table.key" }`, or re-add the
   feather with `kjak feather add NAME --type ... --secret-ref table.key
   --force`.
4. `kjak config validate --strict`.

## Message templates cannot loop

Since v1.0.0 a message template (`[templates.*]`, `--template-file`) refuses
`range` and `define`/`template`/`block`, like an http body template. Their
variables are strings, which `range` cannot iterate, so the only loops were
over numbers (`{{range 1000000000}}`), which could keep kjak busy without
end. A template that uses one is refused by `kjak template set` and by its
render; write the text out instead.

## Configuration files must be regular files

Since v1.0.0 a configuration file, a `.kjak.toml` or the trust store that is
a FIFO, a device or a directory (or a link to one) is refused (`it is not a
regular file`) instead of being read. A symbolic link to a regular file is
still followed.

## Trust store compatibility

- Nothing changes for configurations without literals; `env` and `keychain`
  references work as before.
- The trust store gets new keys (`secrets_path`, `secrets_sha256`,
  `user_secrets`, `plugins`) only in entries that use them; an earlier kjak refuses a
  trust store with such an entry ("broken trust store"). Entries without
  them are written as before.
- `--secret-stdin` still writes a literal (and warns): prefer `--secret-ref`.

## Project env and keychain references need a grant

Since v1.0.0 the `env` and `keychain` references of a project's **slack,
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

Since v1.0.0 the slack, discord and email feathers of a **project** file
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

## `config show -o json` keys

Unchanged. As in v0.0.4, the entries of `groups` and `templates` use Go
field names, not the names of the configuration file:
`{"Feathers": [...]}` and `{"Title", "Body", "Level", "URL"}` (every one
present, `""` when unset). v1.0.0 keeps them for the whole 1.x line
([stability](stability.md#json-output)), so a script written for
v0.0.4 keeps working. `group show -o json` and `template show -o json` use
lower-case names (`feathers`, `title`, ...) as before.

## Downgrading to v0.0.4

v0.0.4 reads the configuration files of v1.0.0 only if they use nothing new
(no `secret` references, http or external feathers, `allow_*` keys). It
refuses a trust store that has an entry with the new keys
(`secrets_path`, `secrets_sha256`, `user_secrets`, `plugins`): "broken
trust store". Restore the copy of `trust.toml` you kept, or remove those
entries and trust again with v0.0.4.
