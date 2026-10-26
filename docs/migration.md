# Migrating literal secrets to a secrets file

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
