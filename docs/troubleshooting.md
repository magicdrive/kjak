# Troubleshooting

| message | cause | fix |
|---|---|---|
| `the project configuration is not trusted: ./.kjak.toml` | a `.kjak.toml` in the current directory that you have not trusted, or that changed since | review it, `kjak config trust`; or `--no-project` |
| `config trust: not a terminal; review the file, then pass --yes` | a non-interactive trust | `kjak config trust --yes` after reviewing the file |
| `environment variable X is not set` | an `env` credential | export it in the environment of kjak |
| `the OS keychain is not supported on windows` | a `keychain` credential on Windows | use `env` |
| `secret-tool (libsecret-tools) is not installed` | Linux keychain | install `libsecret-tools`, or use `env` |
| `no such keychain entry` | the service/account does not exist (or, on Linux, no Secret Service runs) | create it ([security.md](security.md)); check `--secret-keychain SERVICE/ACCOUNT` |
| `feather "x" is disabled` / `does not accept level info` | a feather named directly with `--to` | `kjak feather enable x`, a fitting `--level`, or send to a group (members are then skipped) |
| `... exclude each other: give only one message input` | two inputs (a body and `--stdin`, ...) | give one |
| `--title, --level and --url cannot be combined with --json-input` | | put them in the JSON |
| `nested groups are not supported` | a group names a group | list the feathers |
| `the configuration is being changed by another kjak process (lock file ...lock)` | another kjak is changing the file right now (the lock is an OS lock: it ends with its process, a crashed kjak does not hold it) | retry; the `.lock` file itself stays and is harmless |
| `the configuration file was modified concurrently` | the file changed while kjak wrote it | check the file, run the command again |
| `unknown key(s): feathers.x.webhok (line 3)` | a typo | fix the key ([configuration.md](configuration.md)) |
| `a credential is a table naming one source: ...` | `webhook_url = "https://..."` (a plain string) | `webhook_url = { env = "..." }` or `{ secret = "..." }` |
| `credential error: name only one of env, keychain, secret or literal` | two sources in one credential | keep one |
| `the secrets file is not private: group or others have access (mode 0644)` | a secrets file others can read or change | `chmod 600 FILE` (and check who could read it meanwhile) |
| `the secrets file is not private: it is owned by uid N` | a secrets file of another user (a container mount, `sudo`) | use a file of your own, or `env` |
| `the secrets file is not private: it is a symbolic link, which is refused here` | `./.kjak.secrets.toml` is a link | make it a regular file (a repository could link it to your own secrets) |
| `secret:x is not in the user secrets file ...` / `... does not exist` | the name, or the file, is missing | add it (`kjak config path` shows which file is used), or pass `--secrets FILE` |
| `secret:x (used by the project configuration) is not in the project secrets file ..., and the trust of the project does not allow it ...` | a project reads one of your secrets you did not allow | review the project; `kjak config trust` lists and asks for each name |
| `the project configuration is not trusted: ...: its secrets file ... changed since it was trusted` (or `is not covered by the trust`, `is gone`) | `./.kjak.secrets.toml` differs from the trusted one | review it, `kjak config trust` |
| `config trust: the project reads N secret(s) from your secrets file ...` | `--yes` without `--allow-user-secrets` | review the names, then add `--allow-user-secrets` |
| `kjak: warning: ... keeps N secret(s) in the file as literal (deprecated)` | `{ literal = "..." }` in a configuration | `kjak config migrate-secrets` ([migration.md](migration.md)) |
| `... exists: kjak does not overwrite it` (migrate-secrets) | the secrets file or `<file>.migrated` exists already | `--secrets-out NEW_FILE` / `--config-out NEW_FILE`, or move the secrets by hand |
| `type "os": OS-native notifications are not supported` | | not available yet |
| `the request was not sent: address 10.0.0.5 is private: allow it with allow_networks or allow_hosts` (`error.code: blocked`) | an http feather's host resolves to a private, loopback or link-local address | if it is meant, add the network (`allow_networks`) or the host (`allow_hosts`) ([http.md](http.md#the-address-policy-ssrf)) |
| `the request was not sent: address 127.0.0.1 is loopback: allow it with allow_networks` (a slack, discord or email feather of `.kjak.toml`) | since v1.0.0 a project's webhook and SMTP feathers reach public addresses only | review the destination, add `allow_networks` / `allow_hosts` to the feather, `kjak config trust` ([migration.md](migration.md#a-projects-slack-discord-and-email-feathers-reach-public-addresses-only)) |
| `the trust of the project does not allow its slack feather "x" to send env:VAR` | a project trusted before v1.0.0, or a new reference | review the project, then `kjak config trust` (`--yes --allow-user-secrets` in CI) ([migration.md](migration.md#project-env-and-keychain-references-need-a-grant)) |
| `template error: x.body: range is not available in a message template` | `range`, `define`, `template` or `block` in a message template | write the text out ([migration.md](migration.md#message-templates-cannot-loop)) |
| `...: it is not a regular file (a FIFO)` | a configuration file, `.kjak.toml` or the trust store is a FIFO, a device or a directory | use a regular file (a symbolic link to one is fine) |
| `... is cloud metadata: never allowed` | an http feather (or a redirect) points at an instance metadata endpoint | not possible on purpose |
| `url: must be https (http needs allow_http = true)` | an `http://` URL | use https, or `allow_http = true` for a local relay |
| `HTTP 302: a redirect that is not followed (max_redirects)` | the API redirects | use the final URL, or `max_redirects = N` |
| `(not retried: POST is not repeated without an idempotency key; retry.policy)` | a POST refused with a transient status | `idempotency_key = true` if the API supports it, or `retry.policy = "always"` if a duplicate is acceptable |
| `feather add: an http feather is configured in the file` | `feather add --type http` | write `[feathers.NAME]` with `type = "http"` ([http.md](http.md)) |
| `plugin error: kjak-feather-x is not on PATH (absolute directories only)` | the plugin is not installed, or only in a relative or empty `PATH` element | install it in an absolute `PATH` directory, or set `path =` ([plugins.md](plugins.md)) |
| `... is found in several PATH directories as different files` | two installations of one plugin | remove one, or pin `path =` |
| `plugin error: ...: writable by group or others` / `not executable` / `owned by uid N` | the plugin file could be changed by someone else, or is not runnable | `chmod 755`, own it, or install it elsewhere |
| `plugin name "slack" is reserved` | a plugin named like a built-in type | rename the plugin |
| `its plugin x is not the executable that was trusted` (`untrusted_project`) | a project's plugin was upgraded or replaced | review it, `kjak config trust` |
| `the plugin was not run: ... changed since it was checked` (`blocked`) | the executable changed between the check and the run | run again; find out what replaced it |
| `protocol error: ...; the plugin may have delivered` / `the plugin exited with status N` (`????`) | a plugin that answered wrong, crashed or exited non-zero | check the destination before sending again; fix the plugin (its stderr is in the error) |
| `webhook_url: ... must be https (http is accepted for localhost only ...)` | an `http://` webhook URL to another host | use the `https` URL the provider gave |
| `--json-input: key "body" given twice` | a JSON key repeated | give each key once |
| `the file is larger than 1048576 bytes` | a configuration file over 1 MiB | split it, or check that `.kjak.toml` is what you think |
| `STARTTLS: the server sent clear-text data after its reply` | something injected data before TLS (or a broken server) | check the network path to the SMTP server |
| `... is a symbolic link to a file that does not exist` | kjak was asked to write through a dangling link | fix or remove the link |
| `cannot write the result: ...` (send) | the report could not be written (a full disk, a closed pipe); the deliveries were made, the exit status is theirs | check the exit status before sending again; fix the output |
| `cannot write the output: ...` (exit 2) | the output of another command could not be written | fix the output (disk, pipe) and run the command again |
| `kjak send --stdin` waits | standard input is a terminal or a pipe nobody closes (cron, CI) | give the body as an argument, or end the input (`< /dev/null` is an empty body, refused) |
| `cannot read standard input: the system cannot wait for it` (exit 2) | macOS: standard input is a device it cannot poll, such as `/dev/null` (an empty body either way) | give the body as an argument, or pipe it in |
| `cannot read standard input: its FIFO is no longer at ...` (macOS) | standard input is a named FIFO whose path was removed or replaced before kjak read it (macOS reads it through its path) | give the FIFO with `--json-input`, or keep its path until kjak reads it |
| `kjak send --json-input FIFO` waits | the FIFO's writer has not closed it (or there is none yet) | close the writer; SIGINT or SIGTERM ends the wait (130) |
| `the deadline of 10s passed before anything was sent: ...` (exit 2) | resolving the credentials took the whole `--timeout` (a keychain waiting for a confirmation dialog) | answer the dialog sooner, or raise `--timeout` |

## Results

- **`????` / `acceptance: unknown`** — the provider received the whole request
  but its answer was lost (connection broken, deadline, HTTP 502/504). The
  message **may have been delivered**; kjak did not retry, to avoid a
  duplicate. Check the destination before sending again.
- **`no retry: the wait of 30s does not fit the remaining ...`** — a provider
  asked to wait (`Retry-After`) longer than the deadline left. Raise
  `--timeout` or send later.
- **`gave up after 3 attempts`** — every attempt was refused with a transient
  error; `history` in `-o json` shows each.
- **`deadline exceeded before the first attempt`** — the deadline passed while
  the feather waited for a free worker; raise `--timeout` or `--concurrency`.
- **`HTTP 404` from Slack or Discord** — the webhook was deleted or the URL is
  wrong. **`HTTP 403`/`401`** — a revoked webhook.
- **`AUTH: SMTP 535 authentication failed`** — wrong user or password; kjak
  does not retry it.
- **`the server does not offer STARTTLS`** — use `security = "tls"` (port 465)
  if the server speaks implicit TLS.
- **`TLS certificate verification failed`** — the server's certificate is not
  trusted by the system; kjak has no option to skip verification. Check
  `SSL_CERT_FILE` and `SSL_CERT_DIR` in kjak's environment
  (`env | grep SSL_CERT`): on macOS and Windows either one replaces the
  Keychain / Windows certificate store, so a file with only a corporate or
  test CA makes every public endpoint fail, and a file that does not exist
  makes every TLS feather fail. Use a full bundle with your CA appended, or
  unset them for kjak (`env -u SSL_CERT_FILE -u SSL_CERT_DIR kjak send …`);
  `GODEBUG=x509sslcertoverrideplatform=0` makes kjak use the platform
  verifier again. To trust a private CA (a TLS-inspecting proxy, an
  internal API), set `SSL_CERT_FILE` to a bundle that holds it. See
  [security.md](security.md#certificate-authorities-ssl_cert_file-ssl_cert_dir).

## Diagnose

```sh
kjak config path                # which files, trust state
kjak config validate            # both files, trusted or not
kjak config show                # the merged configuration
kjak send --to x --dry-run "t"  # routing and credentials, nothing sent
kjak send --to x -o json "t"    # every attempt, with codes
```
