# Makefile for kjak

# Variables
BUILD_DIR = $(CURDIR)/build
CMD_DIRS = $(wildcard cmd/*)
BINARY_NAME = kjak
VERSION = $(shell git describe --tags --always)
LDFLAGS += -X "main.version=$(VERSION)"

PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64
GO := GO111MODULE=on CGO_ENABLED=0 go

# Tools, pinned (CI runs these targets too).
#   staticcheck 2026.2.2 (v0.8.2): verified with Go 1.27.3 (go.mod toolchain).
STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.2
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.9.0
ACTIONLINT := github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
RENOVATE_VERSION := 44.164.1

# Release tooling: Hephaestus builds, archives, checksums and publishes the
# release (hephaestus.yaml). The version is kept in step with the commit SHA
# of the magicdrive/hephaestus action in .github/workflows (Renovate updates
# both).
HEPHAESTUS_VERSION ?= v1.1.1-0.20261025105032-4414722a804d
# (no VAR=value prefix: scripts/homebrew-e2e.sh runs it as a plain command)
HEPHAESTUS ?= go run github.com/magicdrive/hephaestus@$(HEPHAESTUS_VERSION)

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

# Run go test for each directory (no cached results)
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

# Same as `race` (the name the other magicdrive repositories use)
.PHONY: test-race
test-race: race

# Run the CLI smoke test against a binary built like a release
# (-trimpath -s -w, main.version=v$(SMOKE_VERSION)); see scripts/smoke.sh
SMOKE_VERSION = 0.0.0-smoke
SMOKE_BIN = $(BUILD_DIR)/smoke/$(BINARY_NAME)$(if $(filter windows,$(shell go env GOOS)),.exe)
.PHONY: smoke
smoke:
	@mkdir -p $(BUILD_DIR)/smoke
	@$(GO) build -trimpath -ldflags "-s -w -X main.version=v$(SMOKE_VERSION)" -o $(SMOKE_BIN) ./main.go
	@sh $(CURDIR)/scripts/smoke.sh $(SMOKE_BIN) "kjak version v$(SMOKE_VERSION)"

# Format every Go file / fail when one is not formatted
.PHONY: fmt
fmt:
	@gofmt -w $$(git ls-files '*.go')

.PHONY: fmt-check
fmt-check:
	@out=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$out" ]; then echo "not gofmt-ed:"; echo "$$out"; exit 1; fi

# Run go vet (also for windows: its own files)
.PHONY: vet
vet:
	@$(GO) vet $(CURDIR)/...
	@GOOS=windows $(GO) vet $(CURDIR)/...

# Run the pinned staticcheck
.PHONY: staticcheck
staticcheck:
	@$(GO) run $(STATICCHECK) ./...

# The static analysis of CI: staticcheck, plus shellcheck of the scripts
.PHONY: lint
lint: staticcheck
	@shellcheck -s sh scripts/*.sh

# Lint the GitHub Actions workflows with the pinned actionlint
.PHONY: actionlint
actionlint:
	@$(GO) run $(ACTIONLINT)

# Validate renovate.json with the pinned Renovate (needs Node.js / npx)
.PHONY: renovate-check
renovate-check:
	@npx --yes --package renovate@$(RENOVATE_VERSION) -- renovate-config-validator --strict renovate.json

# Run the pinned govulncheck (the standard library of the toolchain included)
.PHONY: vulncheck
vulncheck:
	@$(GO) run $(GOVULNCHECK) ./...

# Run every fuzz target for FUZZTIME; a failing input lands in testdata/fuzz
FUZZTIME = 10s
.PHONY: fuzz
fuzz:
	@n=0; for pkg in $$($(GO) list ./...); do \
		for target in $$($(GO) test -list '^Fuzz' $$pkg | grep '^Fuzz'); do \
			echo "== $$pkg $$target"; \
			$(GO) test -run '^$$' -fuzz "^$$target\$$" -fuzztime $(FUZZTIME) $$pkg || exit 1; \
			n=$$((n + 1)); \
		done; \
	done; echo "fuzzed $$n targets"; [ $$n -gt 0 ]

# Run the local quality gates (as CI)
.PHONY: check
check: vet staticcheck test race
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	@go mod tidy -diff
	@echo "check: OK"

# Validate hephaestus.yaml (runs nothing)
.PHONY: hephaestus-check
hephaestus-check:
	@$(HEPHAESTUS) check

# Build and verify the release archives locally (version 0.0.0-SNAPSHOT);
# nothing is published
.PHONY: snapshot
snapshot:
	@rm -rf $(CURDIR)/dist  # as goreleaser --clean: only this run's files
	@$(HEPHAESTUS) run --snapshot --stages build,archive,checksum --fresh
	@sh $(CURDIR)/scripts/verify-dist.sh dist SNAPSHOT

# The release archives of tag=vX.Y.Z at HEAD, verified, and the formula
# (dist/homebrew/kjak.rb), without publishing anything
.PHONY: release-check
release-check:
	@if [ -z "$(tag)" ]; then echo "usage: make release-check tag=vX.Y.Z"; exit 1; fi
	@$(HEPHAESTUS) check
	@rm -rf $(CURDIR)/dist
	@$(HEPHAESTUS) run --tag $(tag) --stages build,archive,checksum,formula --fresh
	@sh $(CURDIR)/scripts/verify-dist.sh dist "$(tag)"

# Read-only release safety checks before tag=vX.Y.Z is pushed; WARN and
# UNKNOWN fail too (docs/release.md). Needs GITHUB_TOKEN (e.g. $$(gh auth token)).
.PHONY: preflight
preflight:
	@if [ -z "$(tag)" ]; then echo "usage: make preflight tag=vX.Y.Z"; exit 1; fi
	@$(HEPHAESTUS) preflight --tag $(tag) --strict

# Archives -> formula with file:// URLs -> brew style, audit --strict,
# install, test, uninstall in a throw-away tap (scripts/homebrew-e2e.sh).
# It installs into this machine's Homebrew prefix and removes what it made:
# meant for CI runners and containers.
.PHONY: homebrew-e2e
homebrew-e2e:
	@HEPHAESTUS="$(HEPHAESTUS)" sh $(CURDIR)/scripts/homebrew-e2e.sh

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

# Execute goreg -w to entire gofile.
.PHONY: goreg
goreg:
	git ls-files | grep -e '.go$$' | xargs -I GOFILE goreg -w GOFILE

# Publish to github.com: the tests, the release preflight (a FAIL stops
# here; run `make preflight tag=...` first for the strict report), then the
# tag is pushed and .github/workflows/release.yml releases it.
.PHONY: publish
publish: test-verbose
	@if [ -z "$(tag)" ]; then \
		echo "Error: version is not set. Please set it and try again."; \
		echo "ex) make publish tag=v0.0.1"; \
		echo ; \
		echo ; \
		exit 1; \
		fi
	$(HEPHAESTUS) preflight --tag $(tag)
	git tag $(tag)
	git push origin $(tag)

# Show help
.PHONY: help
help:
	@echo "Makefile commands:"
	@echo "  make build             - Build all artifacts"
	@echo "  make release-build     - Build artifacts for multiple platforms with version info"
	@echo "  make install           - Install application with go install"
	@echo "  make test              - Run go test"
	@echo "  make test-verbose      - Run go test -v with go clean -testcache"
	@echo "  make race              - Run go test -race (also: make test-race)"
	@echo "  make smoke             - Build like a release and smoke-test the binary (scripts/smoke.sh)"
	@echo "  make fmt               - Format the Go files (fmt-check: fail when one is not formatted)"
	@echo "  make vet               - Run go vet (host and windows)"
	@echo "  make staticcheck       - Run the pinned staticcheck"
	@echo "  make lint              - Run staticcheck and shellcheck"
	@echo "  make vulncheck         - Run the pinned govulncheck"
	@echo "  make actionlint        - Lint the workflows with the pinned actionlint"
	@echo "  make renovate-check    - Validate renovate.json with the pinned Renovate"
	@echo "  make fuzz              - Run every fuzz target for FUZZTIME (10s)"
	@echo "  make check             - Run the local quality gates (vet, staticcheck, test, race, gofmt, go mod tidy)"
	@echo "  make hephaestus-check  - Validate hephaestus.yaml"
	@echo "  make snapshot          - Build and verify the release archives with Hephaestus (nothing is published)"
	@echo "  make release-check tag=<tag> - Archives and formula of <tag> at HEAD, verified (nothing is published)"
	@echo "  make preflight tag=<tag>     - Strict read-only release checks before pushing <tag>"
	@echo "  make homebrew-e2e      - Install the snapshot formula with Homebrew and test it (CI, containers)"
	@echo "  make clean             - Remove build artifacts"
	@echo "  make dev-tools         - Install dev tools"
	@echo "  make goreg             - Execute goreg -w to entire gofile"
	@echo "  make publish tag=<tag> - Test, preflight, then push <tag> (release.yml releases it)"
	@echo "  make help              - Show this message"
