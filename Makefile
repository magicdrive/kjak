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
#   golangci-lint v2.15.0 is built from source with the toolchain (go
#   install): its release binaries are built with an older Go and cannot
#   read Go 1.27 export data (v2.5.0 fails so).
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
TOOLS_DIR = $(BUILD_DIR)/tools

# Release tooling: Hephaestus builds, archives, checksums and publishes the
# release (hephaestus.yaml). The version is kept in step with the commit SHA
# of the magicdrive/hephaestus action in .github/workflows (Renovate updates
# both).
HEPHAESTUS_VERSION ?= v1.3.0
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

# (each command substitution is checked: a failing git or gofmt prints
# nothing, which would read as "every file is formatted")
.PHONY: fmt-check
fmt-check:
	@files=$$(git ls-files '*.go') || exit 1; \
	[ -n "$$files" ] || { echo "fmt-check: no Go file found"; exit 1; }; \
	out=$$(gofmt -l $$files) || { echo "fmt-check: gofmt failed"; exit 1; }; \
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

# Run the pinned golangci-lint (.golangci.yml) for each OS kjak has code of
# its own for: the tool is built for the host once, then GOOS selects the
# files it analyses
.PHONY: golangci-lint
golangci-lint:
	@GOBIN=$(TOOLS_DIR) $(GO) install $(GOLANGCI_LINT)
	@for goos in linux darwin windows; do \
		echo "== golangci-lint GOOS=$$goos"; \
		GOOS=$$goos $(TOOLS_DIR)/golangci-lint run ./... || exit 1; \
	done

# The static analysis of CI: staticcheck, golangci-lint, plus shellcheck of
# the scripts
.PHONY: lint
lint: staticcheck golangci-lint
	@shellcheck -s sh scripts/*.sh

# Lint the GitHub Actions workflows with the pinned actionlint
.PHONY: actionlint
actionlint:
	@$(GO) run $(ACTIONLINT)

# Validate renovate.json (needs Node.js, of .github/renovate-check/.node-version,
# and npm). Renovate and its whole dependency tree are pinned by
# .github/renovate-check/package-lock.json and installed with `npm ci`, which
# Renovate updates (npm manager, lockFileMaintenance).
.PHONY: renovate-check
renovate-check:
	@$(CURDIR)/.github/renovate-check/run.sh

# The failure-injection tests of renovate-check's install (a fake registry:
# lockfile pinning, 404 and integrity failures)
.PHONY: renovate-check-test
renovate-check-test:
	@python3 $(CURDIR)/.github/renovate-check/test_run.py

# Run the pinned govulncheck (the standard library of the toolchain included)
.PHONY: vulncheck
vulncheck:
	@$(GO) run $(GOVULNCHECK) ./...

# Run every fuzz target for FUZZTIME; a failing input lands in testdata/fuzz.
# A package list or a fuzz target list that cannot be read fails (not a
# package silently fuzzed by nothing).
FUZZTIME = 10s
.PHONY: fuzz
fuzz:
	@pkgs=$$($(GO) list ./...) || exit 1; \
	n=0; for pkg in $$pkgs; do \
		list=$$($(GO) test -list '^Fuzz' $$pkg) || { echo "fuzz: cannot list the fuzz targets of $$pkg"; exit 1; }; \
		for target in $$(printf '%s\n' "$$list" | grep '^Fuzz'); do \
			echo "== $$pkg $$target"; \
			$(GO) test -run '^$$' -fuzz "^$$target\$$" -fuzztime $(FUZZTIME) $$pkg || exit 1; \
			n=$$((n + 1)); \
		done; \
	done; echo "fuzzed $$n targets"; [ $$n -gt 0 ]

# Mutation testing of the security-critical paths (scripts/mutation.sh,
# scripts/mutants.tsv): every mutant must be killed. MUTANTS=regex selects
# mutants by id. Slow (minutes): CI runs it weekly (mutation.yml).
MUTANTS =
.PHONY: mutation
mutation:
	@sh $(CURDIR)/scripts/mutation.sh $(MUTANTS)

# Check that every mutant still applies to the code (fast; runs no test)
.PHONY: mutation-check
mutation-check:
	@sh $(CURDIR)/scripts/mutation.sh --check $(MUTANTS)

# Run the local quality gates (as CI)
.PHONY: check
check: fmt-check vet staticcheck golangci-lint test race
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

# Release kjak with Hephaestus's publish (docs/release.md,
# https://github.com/magicdrive/hephaestus/blob/main/docs/publish.md):
#   make publish v1.2.3          (or VERSION=v1.2.3, or tag=v1.2.3 as before)
#   make publish-check v1.2.3    preflight only
#   make publish-status v1.2.3   what exists for a version
#   make publish-watch v1.2.3    follow an already pushed tag
#   make publish-help
# publish checks the version can be released (CI of the commit, rulesets,
# the `release` environment, the workflow conventions), asks, pushes one
# annotated tag, follows the release run and verifies the release; it never
# publishes anything itself. The version given as a goal is a no-op goal
# (.DEFAULT). It reaches hephaestus through the environment, never through a
# shell command line, and hephaestus validates it. YES=1 on the command line
# skips the prompt. PUBLISH can be replaced (makefile_publish_test.go).
PUBLISH ?= $(HEPHAESTUS) publish
PUBLISH_TARGETS = publish publish-check publish-status publish-watch publish-help

ifneq ($(filter $(PUBLISH_TARGETS),$(MAKECMDGOALS)),)
PUBLISH_GOAL := $(firstword $(MAKECMDGOALS))
PUBLISH_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
ifeq ($(filter $(PUBLISH_TARGETS),$(PUBLISH_GOAL)),)
$(error publish targets come first and alone: make publish v1.2.3)
endif
ifneq ($(filter $(PUBLISH_TARGETS),$(PUBLISH_ARGS)),)
$(error give one publish target at a time)
endif
ifneq ($(word 2,$(PUBLISH_ARGS)),)
$(error one version is expected, got: $(PUBLISH_ARGS))
endif
ifeq ($(PUBLISH_GOAL),publish-help)
ifneq ($(PUBLISH_ARGS),)
$(error publish-help takes no version)
endif
endif
export HEPHAESTUS_PUBLISH_ARGS := $(PUBLISH_ARGS)
export HEPHAESTUS_PUBLISH_VERSION := $(if $(filter command line,$(origin VERSION)),$(VERSION))
export HEPHAESTUS_PUBLISH_TAG := $(if $(filter command line,$(origin tag)),$(tag))
PUBLISH_YES := $(if $(and $(filter command line,$(origin YES)),$(filter 1,$(YES))),-yes)
ifneq ($(PUBLISH_ARGS),)
ifeq ($(filter v0% v1% v2% v3% v4% v5% v6% v7% v8% v9% 0% 1% 2% 3% 4% 5% 6% 7% 8% 9%,$(PUBLISH_ARGS)),)
$(error "$(PUBLISH_ARGS)" is not a version: make $(PUBLISH_GOAL) v1.2.3)
endif
# The version goal has no rule of its own: it is the publish target's
# argument. (No other target starts with v<digit>, so none is shadowed.)
.DEFAULT:
	@:
endif
endif

# Check, tag, push and follow a release (the only target that pushes a tag)
.PHONY: publish
publish:
	@$(PUBLISH) publish -from-make $(PUBLISH_YES)

# Release preflight only (no tag, no push, no GitHub writes)
.PHONY: publish-check
publish-check:
	@$(PUBLISH) check -from-make

# Show what exists for a version (read-only)
.PHONY: publish-status
publish-status:
	@$(PUBLISH) status -from-make

# Follow the release run of an already pushed tag and verify the release
.PHONY: publish-watch
publish-watch:
	@$(PUBLISH) watch -from-make

# Show how releasing works and its safety rules
.PHONY: publish-help
publish-help:
	@$(PUBLISH) help

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
	@echo "  make golangci-lint     - Run the pinned golangci-lint for linux, darwin and windows"
	@echo "  make lint              - Run staticcheck, golangci-lint and shellcheck"
	@echo "  make vulncheck         - Run the pinned govulncheck"
	@echo "  make actionlint        - Lint the workflows with the pinned actionlint"
	@echo "  make renovate-check    - Validate renovate.json with the pinned Renovate"
	@echo "  make renovate-check-test - Test renovate-check's pinned install (fake registry)"
	@echo "  make fuzz              - Run every fuzz target for FUZZTIME (10s)"
	@echo "  make mutation          - Mutation testing of the security-critical paths (MUTANTS=regex)"
	@echo "  make mutation-check    - Check that every mutant still applies (no test run)"
	@echo "  make check             - Run the local quality gates (vet, staticcheck, golangci-lint, test, race, gofmt, go mod tidy)"
	@echo "  make hephaestus-check  - Validate hephaestus.yaml"
	@echo "  make snapshot          - Build and verify the release archives with Hephaestus (nothing is published)"
	@echo "  make release-check tag=<tag> - Archives and formula of <tag> at HEAD, verified (nothing is published)"
	@echo "  make preflight tag=<tag>     - Strict read-only release checks before pushing <tag>"
	@echo "  make homebrew-e2e      - Install the snapshot formula with Homebrew and test it (CI, containers)"
	@echo "  make clean             - Remove build artifacts"
	@echo "  make dev-tools         - Install dev tools"
	@echo "  make goreg             - Execute goreg -w to entire gofile"
	@echo "  make publish vX.Y.Z        - Release: preflight, confirm, tag, push, follow, verify (also tag=vX.Y.Z)"
	@echo "  make publish-check vX.Y.Z  - Release preflight only"
	@echo "  make publish-status vX.Y.Z - Show the release state of a version"
	@echo "  make publish-watch vX.Y.Z  - Follow an already pushed release tag"
	@echo "  make publish-help          - How releasing works"
	@echo "  make help              - Show this message"
