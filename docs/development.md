# Development guide

## Requirements

- Go 1.27 (go.mod pins the toolchain `go1.27.3`; with `GOTOOLCHAIN=auto`, the
  default, an older `go` downloads it)
- GNU make, a POSIX shell
- For the race detector: a C compiler (`-race` needs cgo; release binaries do
  not)
- Nothing else for `make snapshot`: Hephaestus (`HEPHAESTUS_VERSION` in the
  Makefile) runs with `go run`
- For `make homebrew-e2e`: Homebrew (it installs into the machine's prefix;
  meant for CI runners and containers); for `make renovate-check`: Node.js 24

```sh
make help          # every target
make check         # the local quality gates (before a pull request)
```

## Layout

The layout follows magicdrive/ark and magicdrive/enma:

```
main.go                      version from -ldflags or the build info; calls cmd.Execute
cmd/kjak.go                  signals (SIGINT, SIGTERM -> context; SIGPIPE caught), exit status
internal/
  commandline/               dispatch of `kjak <command>` (no default command)
    option/                  flag parsing per command; input exclusivity
    subcmd/                  each command: options -> domain calls -> output, exit code
  textbank/                  help texts and the default config (go:embed)
  core/                      Message, Level, Result, Report, aggregation (standard library only)
  clock/                     real and fake clocks (the engine's only time source)
  configure/                 TOML schema, merge, validation, trust store, atomic writes,
                             secrets files (private-file checks, scopes of a reference)
  credential/                env / keychain / secret / literal references, Secret, Redactor
  template/                  text/template rendering of message templates
  routing/                   targets -> ordered plan (direct, group, skipped)
  delivery/                  worker pool, deadline, retry, backoff, cancellation
  feather/                   the Feather interface and Outcome (the engine's view)
    webhook/                 HTTP POST and its classification (shared by slack, discord)
    slack/ discord/ email/   the adapters: payloads, SMTP
    builtin/                 builds a feather from its configuration (credentials resolved)
  output/                    text and JSON (schema_version 1) reports
  e2e/                       the built binary, run as a user runs it
```

Dependency rules (`architecture_test.go` enforces them):

- nothing outside `internal/commandline` (and `cmd`) imports
  `internal/commandline`: the domain does not know the command line, and no
  adapter sees an option type;
- `internal/core` imports the standard library only;
- the feathers do not import `delivery` or `routing`;
- `cmd` imports `internal/commandline` only.

Provider-independent HTTP handling is in `feather/webhook`; what differs per
provider (payload, Discord's `?wait=true` and `retry_after` body) is in the
adapter. There is no DI framework: `subcmd.Runtime` carries the outside world
(stdio, environment, keychain, clock, HTTP client), and tests replace it.

## Dependencies

kjak depends on `github.com/pelletier/go-toml/v2` (TOML) and
`golang.org/x/sys` (the Windows file lock). The transports use the standard
library only. Libraries were evaluated in October 2026 against what
[delivery.md](delivery.md) promises, and not adopted:

| library | why not |
|---|---|
| `github.com/slack-go/slack` v0.30.1 | the webhook call discards the response body (Slack's error text), reads `Retry-After` as seconds only (an HTTP date becomes a parse error that hides the status), and wraps the transport error that quotes the URL, which is the secret; a 30k-line module with a WebSocket dependency for one POST |
| `github.com/bwmarrin/discordgo` v0.29.0 | retries HTTP 502 up to 3 times by default (a duplicate: kjak reports 502 as unknown) and sleeps on 429 without a context; a bot framework |
| `github.com/disgoorg/disgo` v0.19.6 | retries rate-limited requests itself; a 41k-line framework |
| `github.com/wneessen/go-mail` v0.8.1 | its data writer ignores the error of writing the terminator, so a failure before the end of the message cannot be told from a lost reply after it; standard `textproto` replies (a bare `250` is refused); a context for the dial only; `x/crypto`, `x/text` and 2.6 MB more in a minimal program |
| `github.com/emersion/go-smtp` v0.25.0 | no context; it resets the connection deadline at every command, which would undo kjak's cancellation; the same terminator and reply limits |
| `github.com/go-mail/mail` | archived (last release 2018) |

Re-evaluate when one of these can report how far a request got.

## Conventions inherited from Ark and Enma

- `main.go` with `var version` set by `-ldflags "-X main.version=..."`, falling
  back to `debug.ReadBuildInfo`.
- `cmd` hands over to `internal/commandline`; `option/` parses with the
  standard `flag` package; `subcmd/` runs; help texts are embedded from
  `internal/textbank`.
- Makefile variables (`BUILD_DIR`, `BINARY_NAME`, `VERSION`, `LDFLAGS`,
  `PLATFORMS`, `GO := GO111MODULE=on CGO_ENABLED=0 go`), per-platform targets
  (`make linux/amd64`), `help` as the default, pinned tools run with `go run
  tool@version`.
- Tests next to the code (`_test.go` in the package), end-to-end tests of the
  built binary in `internal/e2e`.
- GitHub Actions pinned to commit SHAs; Hephaestus for the archives and the
  release (`hephaestus.yaml`, [release.md](release.md));
  `goreg.toml` import order (`make goreg`).

Deliberate differences: kjak's entry point is `cmd/kjak.go` (package `cmd`)
as its specification requires, instead of `cmd/<name>/mod.go`; the
subcommands return an exit status instead of calling `os.Exit`, so that the
whole command line is testable in-process; CI tests Windows on every change
(kjak has Windows code of its own: the configuration lock, paths).

## Tests

```sh
make test            # unit + integration + e2e, no cache (go test -count=1 ./...)
make race            # the same with -race
make vet staticcheck vulncheck
make fuzz            # every fuzz target for FUZZTIME (10s)
make check           # vet, staticcheck, test, race, gofmt, go mod tidy
make smoke           # a binary built like a release, run by scripts/smoke.sh
make snapshot        # the release archives (Hephaestus), verified
```

Tests are deterministic: no test waits for real time to pass.

- The engine runs on `clock.Fake`; a test advances it to each pending wait
  (`BlockUntil`, `NextIn`, `Advance`). Backoff jitter is injected
  (`Backoff.Rand`).
- Concurrency is checked with barriers (channels the test releases one at a
  time), not with sleeps.
- Providers are `httptest` servers (a handler that hijacks and closes the
  connection after reading the body is the "lost response") and a fake SMTP
  server (`internal/feather/email/fakesmtp_test.go`) with a self-signed
  certificate generated per test.
- The CLI is tested in-process with a fake `Runtime`
  (`internal/commandline/commandline_test.go`) and as a binary
  (`internal/e2e`, including SIGINT, SIGTERM and a closed stdout on Unix).
- No test sends anything outside the machine.

A flaky test is a bug: fix it, never skip it.

Tests of the rules that must never regress (worker pool limit, result order,
retry limit, Retry-After, deadline, cancellation, ambiguous acceptance,
concurrent config modification, aggregation, trust, secret redaction) are
named `TestCritical_<Name>`; they run with every other test.
`architecture_test.go` checks the dependency rules.

The fuzz targets cover message validation, TOML parsing (round trip), secrets
files (round trip, no value in an error), webhook
URL checks, the JSON input object, the webhook classification and
`Retry-After`, SMTP dot stuffing and reply parsing. `make fuzz` finds them
with `go test -list`; a failing input is written to the package's
`testdata/fuzz` and then runs as a regular test.

## CI

`.github/workflows/ci.yml` (as enma's, plus ark's fuzz smoke), on every push
to main and every pull request:

| job | runs |
|---|---|
| test (ubuntu, macos, windows) | gofmt (Linux), `go vet`, `go test -count=1 ./...`, `-race` (not Windows: no cgo there), CLI smoke test of a release-built binary (`scripts/smoke.sh`) |
| lint | `go mod verify`, `go mod tidy -diff`, `make staticcheck`, `GOOS=windows go vet`, shellcheck, `make actionlint` |
| renovate | `make renovate-check` (`renovate-config-validator --strict`) |
| fuzz | `make fuzz` |
| dist | `make vulncheck`, `hephaestus check`, Hephaestus snapshot (build, archive, checksum), `scripts/verify-dist.sh` |
| homebrew-e2e (ubuntu, macos) | `make homebrew-e2e`: the generated formula with file:// URLs, `brew style`, `brew audit --strict`, install, smoke test, `brew test`, uninstall |

macOS runs on `macos-latest` (arm64); the darwin/amd64 binary is cross-built
and checked by `scripts/verify-dist.sh` (architecture, static linking), not
run. Tool versions are pinned in the Makefile (staticcheck, govulncheck,
actionlint, Renovate, Hephaestus) and the workflows (actions by commit SHA,
`magicdrive/hephaestus` included); Go comes from `go.mod`. Renovate
(`renovate.json`) updates all of them.
