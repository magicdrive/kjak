# Development guide

## Requirements

- Go 1.27 (go.mod pins the toolchain `go1.27.3`; with `GOTOOLCHAIN=auto`, the
  default, an older `go` downloads it)
- GNU make, a POSIX shell
- For the race detector: a C compiler (`-race` needs cgo; release binaries do
  not)
- For `make snapshot`: GoReleaser v2.18.2

```sh
make help          # every target
make check         # the local quality gates (before a pull request)
```

## Layout

The layout follows magicdrive/ark and magicdrive/enma:

```
main.go                      version from -ldflags or the build info; calls cmd.Execute
cmd/kjak.go                  signals (SIGINT -> context), exit status; nothing else
internal/
  commandline/               dispatch of `kjak <command>` (no default command)
    option/                  flag parsing per command; input exclusivity
    subcmd/                  each command: options -> domain calls -> output, exit code
  textbank/                  help texts and the default config (go:embed)
  core/                      Message, Level, Result, Report, aggregation (standard library only)
  clock/                     real and fake clocks (the engine's only time source)
  configure/                 TOML schema, merge, validation, trust store, atomic writes
  credential/                env / keychain / literal references, Secret, Redactor
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
- GitHub Actions pinned to commit SHAs; GoReleaser v2 for the archives;
  `goreg.toml` import order (`make goreg`).

Deliberate differences: kjak's entry point is `cmd/kjak.go` (package `cmd`)
as its specification requires, instead of `cmd/<name>/mod.go`; the
subcommands return an exit status instead of calling `os.Exit`, so that the
whole command line is testable in-process; there is no `make publish` (a
lightweight tag pushed from a laptop): a release is an annotated tag and the
release workflow ([release.md](release.md)).

## Tests

```sh
make test            # unit + integration + e2e, no cache (go test -count=1 ./...)
make race            # the same with -race
make coverage        # coverage.out + coverage.html (-covermode=atomic; no threshold)
make critical-test   # the critical tests 20 times
make critical-race   # the critical tests 5 times with -race
make vet staticcheck vulncheck
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
  (`internal/e2e`, including SIGINT on Unix).
- No test sends anything outside the machine.

A flaky test is a bug: fix it, never skip it.

### Critical tests

Tests of the rules that must never regress — worker pool limit, result order,
retry limit, Retry-After, deadline, cancellation, ambiguous acceptance,
concurrent config modification, aggregation, trust, secret redaction — are
named `TestCritical_<Name>` and listed in `test/critical-tests.txt`
(`<package> <test>` per line).

`scripts/critical-tests.sh` (`make critical-check`, and before every critical
run) fails when the manifest is empty, malformed, has duplicates, names a test
that does not exist in its package, or misses a `TestCritical_` test of the
code. The run then requires every listed test to have passed exactly COUNT
times — `go test -run` with a pattern that matches nothing passes, so the
count is checked.

Adding one: name it `TestCritical_...`, add its line to the manifest, run
`make critical-check`.

### Tests of the gates themselves

- `scripts_test.go` runs `critical-tests.sh` against a fixture module (a
  self-skipping critical test must fail the run), `ci-gate.sh` with failed,
  skipped, cancelled, missing and extra jobs, `formula.sh` (pre-release,
  mismatched tag, malformed checksum) and `verify-dist.sh`.
- `release_workflow_test.go` checks the job order of `release.yml`, build
  once (only the build job builds or signs), the pre-release and stale-draft
  rules, and that the CI Gate needs every job.
- `architecture_test.go` checks the dependency rules.

### Fuzzing

`make fuzz` (CI: Fuzz Smoke) runs the fuzz targets — message validation, TOML
parsing (round trip), webhook URL checks, the JSON input object, the webhook
classification and `Retry-After`, SMTP dot stuffing and reply parsing — for
`FUZZTIME` (10s) each. A crasher is written to the package's `testdata/fuzz`
and then runs as a regular test.

## CI

`.github/workflows/ci.yml`, on every push to main and every pull request:

| job | runs |
|---|---|
| Quality / Format / Module Integrity | `make fmt-check vet critical-check`, shellcheck |
| Staticcheck | `make staticcheck` (v0.8.2) |
| govulncheck | `make vulncheck` (v1.9.0) |
| Unit / Integration | `make test` on Linux and macOS; its `go test -count=1 ./...` on Windows |
| Race (Linux) | `make race` |
| Coverage (Linux) | `make coverage`, uploads the profile and the report |
| Critical Repeat 20x / Critical Race 5x (Linux) | `make critical-test` / `make critical-race` |
| Cross Build | `make <os>/<arch>` for the five targets, `CGO_ENABLED=0` |
| Native Smoke | the cross-built binary run by `scripts/smoke.sh` on Linux, macOS, Windows |
| Fuzz Smoke (Linux) | `make fuzz`: every fuzz target for 10s |
| Release Dry Run | `make snapshot`: GoReleaser check, archives, verify-dist, formula |
| **CI Gate** | `scripts/ci-gate.sh`: succeeds only if all 12 jobs above succeeded (failed, skipped, cancelled, missing or extra fail it) |

The main branch ruleset requires the check `CI Gate`. Tool versions are pinned
in the Makefile (and Go / GoReleaser in the workflow `env`); change them
there.
