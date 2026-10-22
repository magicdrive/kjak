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

# Run go vet (also for windows: its own files)
.PHONY: vet
vet:
	@$(GO) vet $(CURDIR)/...
	@GOOS=windows $(GO) vet $(CURDIR)/...

# Run the pinned staticcheck
.PHONY: staticcheck
staticcheck:
	@$(GO) run $(STATICCHECK) ./...

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

# Build and verify the release archives locally; nothing is published
.PHONY: snapshot
snapshot:
	@goreleaser release --snapshot --clean --skip=publish
	@sh $(CURDIR)/scripts/verify-dist.sh dist snapshot

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

# Publish to github.com
.PHONY: publish
publish: test-verbose
	@if [ -z "$(tag)" ]; then \
		echo "Error: version is not set. Please set it and try again."; \
		echo "ex) make publish tag=v0.0.1"; \
		echo ; \
		echo ; \
		exit 1; \
		fi
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
	@echo "  make race              - Run go test -race"
	@echo "  make vet               - Run go vet (host and windows)"
	@echo "  make staticcheck       - Run the pinned staticcheck"
	@echo "  make vulncheck         - Run the pinned govulncheck"
	@echo "  make fuzz              - Run every fuzz target for FUZZTIME (10s)"
	@echo "  make check             - Run the local quality gates (vet, staticcheck, test, race, gofmt, go mod tidy)"
	@echo "  make snapshot          - Build and verify the release archives (nothing is published)"
	@echo "  make clean             - Remove build artifacts"
	@echo "  make dev-tools         - Install dev tools"
	@echo "  make goreg             - Execute goreg -w to entire gofile"
	@echo "  make publish tag=<tag> - Publish to github.com"
	@echo "  make help              - Show this message"
