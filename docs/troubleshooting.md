# Troubleshooting

| message | cause | fix |
|---|---|---|
| `the project configuration is not trusted: ./.kjak.toml` | a `.kjak.toml` in the current directory that you have not trusted, or that changed since | review it, `kjak config trust`; or `--no-project` |
| `config trust: not a terminal; review the file, then pass --yes` | a non-interactive trust | `kjak config trust --yes` after reviewing the file |
| `environment variable X is not set` | an `env` credential | export it in the environment of kjak |
| `the OS keychain is not supported on windows in kjak v0.1.0` | a `keychain` credential on Windows | use `env` |
| `secret-tool (libsecret-tools) is not installed` | Linux keychain | install `libsecret-tools`, or use `env` |
| `no such keychain entry` | the service/account does not exist (or, on Linux, no Secret Service runs) | create it ([security.md](security.md)); check `--secret-keychain SERVICE/ACCOUNT` |
| `feather "x" is disabled` / `does not accept level info` | a feather named directly with `--to` | `kjak feather enable x`, a fitting `--level`, or send to a group (members are then skipped) |
| `... exclude each other: give only one message input` | two inputs (a body and `--stdin`, ...) | give one |
| `--title, --level and --url cannot be combined with --json-input` | | put them in the JSON |
| `nested groups are not supported in v0.1.0` | a group names a group | list the feathers |
| `the configuration is being changed by another kjak process (lock file ...lock)` | another kjak is changing the file right now (the lock is an OS lock: it ends with its process, a crashed kjak does not hold it) | retry; the `.lock` file itself stays and is harmless |
| `the configuration file was modified concurrently` | the file changed while kjak wrote it | check the file, run the command again |
| `unknown key(s): feathers.x.webhok (line 3)` | a typo | fix the key ([configuration.md](configuration.md)) |
| `a credential is a table naming one source: ...` | `webhook_url = "https://..."` (a plain string) | `webhook_url = { env = "..." }` (or `{ literal = "..." }`) |
| `credential error: name only one of env, keychain or literal` | two sources in one credential | keep one |
| `type "os": OS-native notifications are not supported in kjak v0.1.0` | | not available yet |
| `webhook_url: ... must be https (http is accepted for localhost only ...)` | an `http://` webhook URL to another host | use the `https` URL the provider gave |
| `--json-input: key "body" given twice` | a JSON key repeated | give each key once |
| `the file is larger than 1048576 bytes` | a configuration file over 1 MiB | split it, or check that `.kjak.toml` is what you think |
| `STARTTLS: the server sent clear-text data after its reply` | something injected data before TLS (or a broken server) | check the network path to the SMTP server |
| `... is a symbolic link to a file that does not exist` | kjak was asked to write through a dangling link | fix or remove the link |
| `cannot write the result: ...` (send) | the report could not be written (a full disk, a closed pipe); the deliveries were made, the exit status is theirs | check the exit status before sending again; fix the output |
| `cannot write the output: ...` (exit 2) | the output of another command could not be written | fix the output (disk, pipe) and run the command again |
| `kjak send --stdin` waits | standard input is a terminal or a pipe nobody closes (cron, CI) | give the body as an argument, or end the input (`< /dev/null` is an empty body, refused) |
| `cannot read standard input: the system cannot wait for it` (exit 2) | macOS: standard input is a device it cannot poll, such as `/dev/null` (an empty body either way) | give the body as an argument, or pipe it in |
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
  trusted by the system; kjak has no option to skip verification.

## Diagnose

```sh
kjak config path                # which files, trust state
kjak config validate            # both files, trusted or not
kjak config show                # the merged configuration
kjak send --to x --dry-run "t"  # routing and credentials, nothing sent
kjak send --to x -o json "t"    # every attempt, with codes
```
