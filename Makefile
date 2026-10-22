# Makefile for kjak

# Variables
BUILD_DIR = $(CURDIR)/build
CMD_DIRS = $(wildcard cmd/*)
BINARY_NAME = kjak
REPOSITORY = magicdrive/kjak
VERSION = $(shell git describe --tags --always)
LDFLAGS += -X "main.version=$(VERSION)"

PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64
GO := GO111MODULE=on CGO_ENABLED=0 go

# Tools, pinned. CI and the release workflow run these targets, so the
# versions here are the versions of every gate.
#   staticcheck 2026.2.2 (v0.8.2): verified with Go 1.27.3 (go.mod toolchain);
#   2026.2.1 cannot read the export data of Go 1.27.2 and later.
STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.2
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.9.0
GORELEASER_VERSION := v2.18.2

# Critical tests (test/critical-tests.txt)
CRITICAL_COUNT = 20
CRITICAL_RACE_COUNT = 5

# Argments
tag =

# Default target
.PHONY: all
all: help

# Build all artifacts
.PHONY: build
build: clean
	@mkdir -p $(BUILD_DIR)
	@$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME)$(if $(filter windows,$(shell go env GOOS)),.exe) -ldflags "$(LDFLAGS)"
	@chmod 755 $(BUILD_DIR)/$(BINARY_NAME)*

# Build artifacts for all platforms and release
.PHONY: release-build
release-build: clean $(PLATFORMS)
	@echo "Release files are created in the $(BUILD_DIR) directory."

# Build each platform (windows binaries get the .exe suffix)
os = $(word 1,$(subst /, ,$@))
arch = $(word 2,$(subst /, ,$@))
platform_bin = $(BUILD_DIR)/$(os)-$(arch)/$(BINARY_NAME)$(if $(filter windows,$(os)),.exe)
$(PLATFORMS):
	@mkdir -p $(BUILD_DIR)
	GOOS=$(os) GOARCH=$(arch) $(GO) build -trimpath -o $(platform_bin) -ldflags "$(LDFLAGS)" .
	chmod 755 $(platform_bin)

# Run go test for each directory (unit and integration tests, no cache)
.PHONY: test
test:
	@$(GO) test -count=1 $(CURDIR)/...

# Run go test with verbose output and clear test cache
.PHONY: test-verbose
test-verbose:
	@$(GO) clean -testcache
	@$(GO) test -v -count=1 $(CURDIR)/...

# Run go test with the race detector (it needs cgo)
.PHONY: race
race:
	@GO111MODULE=on CGO_ENABLED=1 go test -race -count=1 $(CURDIR)/...

# Measure coverage: coverage.out and coverage.html (no threshold)
.PHONY: coverage
coverage:
	@$(GO) test -count=1 -covermode=atomic -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1
	@go tool cover -html=coverage.out -o coverage.html
	@echo "coverage.out and coverage.html are written."

# Check the critical test manifest, then run every critical test 20 times
.PHONY: critical-test
critical-test:
	@sh $(CURDIR)/scripts/critical-tests.sh run $(CRITICAL_COUNT)

# Run every critical test 5 times with the race detector
.PHONY: critical-race
critical-race:
	@CGO_ENABLED=1 sh $(CURDIR)/scripts/critical-tests.sh run $(CRITICAL_RACE_COUNT) -race

# Run every fuzz target briefly (FUZZTIME each); new crashers land in testdata/fuzz
FUZZTIME = 10s
FUZZ_TARGETS = \
	./internal/core:FuzzMessageValidate \
	./internal/configure:FuzzParse \
	./internal/configure:FuzzCheckWebhookURL \
	./internal/commandline/subcmd:FuzzCheckJSONObject \
	./internal/feather/webhook:FuzzClassify \
	./internal/feather/webhook:FuzzParseRetryAfter \
	./internal/feather/email:FuzzDotStuff \
	./internal/feather/email:FuzzReadReply
.PHONY: fuzz
fuzz:
	@for t in $(FUZZ_TARGETS); do \
		pkg=$${t%%:*}; name=$${t##*:}; \
		echo "fuzz $$pkg $$name"; \
		$(GO) test -run '^$$' -fuzz "^$$name\$$" -fuzztime=$(FUZZTIME) $$pkg || exit 1; \
	done

# Check the critical test manifest only
.PHONY: critical-check
critical-check:
	@sh $(CURDIR)/scripts/critical-tests.sh check

# Run go vet (also for windows: its own files)
.PHONY: vet
vet:
	@$(GO) vet $(CURDIR)/...
	@GOOS=windows $(GO) vet $(CURDIR)/...

# Check formatting and the module files
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	@go mod verify
	@cp go.mod go.mod.orig && cp go.sum go.sum.orig && go mod tidy && \
		if ! cmp -s go.mod go.mod.orig || ! cmp -s go.sum go.sum.orig; then \
			mv go.mod.orig go.mod; mv go.sum.orig go.sum; echo "go mod tidy changes go.mod or go.sum"; exit 1; \
		fi; rm -f go.mod.orig go.sum.orig

# Run the pinned staticcheck
.PHONY: staticcheck
staticcheck:
	@$(GO) run $(STATICCHECK) ./...

# Run the pinned govulncheck (the standard library of the toolchain included)
.PHONY: vulncheck
vulncheck:
	@$(GO) run $(GOVULNCHECK) ./...

# Run the main local quality gates
.PHONY: check
check: fmt-check vet staticcheck test race critical-check
	@echo "check: OK"

# Run the built binary through a smoke test (bin=<path>, default build/kjak)
.PHONY: smoke
smoke:
	@sh $(CURDIR)/scripts/smoke.sh $(or $(bin),$(BUILD_DIR)/$(BINARY_NAME))

# Check the GoReleaser configuration (the pinned version)
.PHONY: release-check
release-check:
	@goreleaser --version | grep -q "GitVersion: *$(GORELEASER_VERSION:v%=%)" || \
		{ echo "goreleaser $(GORELEASER_VERSION) is required"; exit 1; }
	@goreleaser check

# Build the release archives locally; nothing is published
.PHONY: snapshot
snapshot: release-check
	@goreleaser release --snapshot --clean --skip=publish
	@sh $(CURDIR)/scripts/verify-dist.sh dist
	@sh $(CURDIR)/scripts/formula.sh --snapshot dist > dist/kjak.rb
	@echo "dist/kjak.rb is the Homebrew formula of these archives (not published)."

# Install application. Use `go install`
.PHONY: install
install:
	@echo "Installing kjak..."
	@$(GO) install -ldflags "$(LDFLAGS)"

# Clean build artifacts
.PHONY: clean
clean:
	@rm -rf $(BUILD_DIR)

# Install dev tools
.PHONY: dev-tools
dev-tools:
	go install "github.com/magicdrive/goreg@latest"
	go install "github.com/magicdrive/kirke@latest"
	go install $(STATICCHECK)
	go install $(GOVULNCHECK)
	@echo "goreleaser $(GORELEASER_VERSION): https://goreleaser.com/install/"

# Execute goreg -w to entire gofile.
.PHONY: goreg
goreg:
	git ls-files | grep -e '.go$$' | xargs -I GOFILE goreg -w GOFILE

# Check that tag=vX.Y.Z[-pre] can be published: the checks of the release
# workflow's Tag Validation that can run here (docs/release.md), before
# anything is pushed. The workflow checks again (and that no release exists).
.PHONY: publish-check
publish-check:
	@if [ -z "$(tag)" ]; then \
		echo "Error: version is not set. Please set it and try again."; \
		echo "ex) make publish tag=v0.1.0"; \
		exit 1; \
	fi
	@echo "$(tag)" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$$' || \
		{ echo "Error: not a release tag: $(tag) (want vX.Y.Z or vX.Y.Z-pre)"; exit 1; }
	@[ "$$(git rev-parse --abbrev-ref HEAD)" = main ] || \
		{ echo "Error: publish from main (on $$(git rev-parse --abbrev-ref HEAD))"; exit 1; }
	@[ -z "$$(git status --porcelain)" ] || \
		{ echo "Error: the working tree is not clean"; exit 1; }
	@git fetch --quiet origin main --tags
	@[ "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" ] || \
		{ echo "Error: HEAD is not origin/main (git pull --ff-only, or push through a pull request)"; exit 1; }
	@if git rev-parse -q --verify "refs/tags/$(tag)" >/dev/null || \
		[ -n "$$(git ls-remote --tags origin "refs/tags/$(tag)")" ]; then \
		echo "Error: the tag $(tag) exists already (recover forward with a new version)"; exit 1; \
	fi
	@# the release workflow's GITHUB_TOKEN can read neither of these (HTTP 403):
	@# checked here, with the maintainer's gh, before anything is published
	@[ "$$(gh api repos/$(REPOSITORY)/immutable-releases --jq .enabled)" = true ] || \
		{ echo "Error: immutable releases are not enabled for $(REPOSITORY) (Settings > General > Releases; docs/release.md)"; exit 1; }
	@case "$(tag)" in *-*) ;; *) \
		gh secret list --repo $(REPOSITORY) --json name --jq '.[].name' | grep -qx TAP_GITHUB_TOKEN || \
		{ echo "Error: the secret TAP_GITHUB_TOKEN is not set for $(REPOSITORY): a stable release would stop before the tap (docs/release.md)"; exit 1; } ;; \
	esac
	@echo "publish-check: $(tag) on $$(git rev-parse --short HEAD) OK"

# Publish to github.com: push the annotated tag; the release workflow does the rest
.PHONY: publish
publish: publish-check check
	git tag -a $(tag) -m "kjak $(tag)"
	git push origin refs/tags/$(tag)

# Show help
.PHONY: help
help:
	@echo "Makefile commands:"
	@echo "  make build             - Build all artifacts"
	@echo "  make release-build     - Build artifacts for multiple platforms with version info"
	@echo "  make install           - Install application with go install"
	@echo "  make test              - Run go test (unit and integration, no cache)"
	@echo "  make test-verbose      - Run go test -v with go clean -testcache"
	@echo "  make race              - Run go test -race"
	@echo "  make coverage          - Write coverage.out and coverage.html"
	@echo "  make critical-test     - Run the critical tests $(CRITICAL_COUNT) times (manifest checked)"
	@echo "  make critical-race     - Run the critical tests $(CRITICAL_RACE_COUNT) times with -race"
	@echo "  make critical-check    - Check test/critical-tests.txt"
	@echo "  make fuzz              - Run every fuzz target for FUZZTIME (10s)"
	@echo "  make vet               - Run go vet (host and windows)"
	@echo "  make fmt-check         - Check gofmt, go mod verify and go mod tidy"
	@echo "  make staticcheck       - Run the pinned staticcheck"
	@echo "  make vulncheck         - Run the pinned govulncheck"
	@echo "  make check             - Run the local quality gates (fmt, vet, staticcheck, test, race, manifest)"
	@echo "  make smoke [bin=PATH]  - Smoke-test a built binary"
	@echo "  make release-check     - Check .goreleaser.yaml with the pinned GoReleaser"
	@echo "  make snapshot          - Build and verify release archives locally (nothing is published)"
	@echo "  make clean             - Remove build artifacts"
	@echo "  make dev-tools         - Install dev tools"
	@echo "  make goreg             - Execute goreg -w to entire gofile"
	@echo "  make publish-check tag=<tag> - Check that <tag> can be published (nothing is pushed)"
	@echo "  make publish tag=<tag> - Publish to github.com (annotated tag on main; the release workflow runs)"
	@echo "  make help             - Show this message"
