.PHONY: all build build-ctxt build-dpkms clean install deps test test-unit test-integration test-smoke test-all test-cover test-gate lint fmt help docs docs-dev docker-build docker-dev docker-prod docker-down docker-logs docker-ps docker-shell security-scan install-hooks

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Build flags
LDFLAGS := -ldflags "\
	-X main.Version=$(VERSION) \
	-X main.BuildTime=$(BUILD_TIME) \
	-X main.GitCommit=$(GIT_COMMIT)"

# Build directories
BUILD_DIR := bin
CTXT_BINARY := $(BUILD_DIR)/ctxt
DPKMS_BINARY := $(BUILD_DIR)/dpkms

# Go files
CTXT_MAIN := cmd/ctxt/main.go
DPKMS_MAIN := cmd/dpkms/main.go

# Default target
all: build

## deps: Install media processing dependencies (pdftotext, whisper, pyannote)
deps: build-dpkms
	$(DPKMS_BINARY) install-deps

## build: Build both ctxt and dpkms binaries
build: build-ctxt build-dpkms

## build-ctxt: Build the ctxt binary
build-ctxt:
	@echo "Building ctxt..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(CTXT_BINARY) $(CTXT_MAIN)
	@echo "✓ Built: $(CTXT_BINARY)"

## build-dpkms: Build the dpkms binary
build-dpkms:
	@echo "Building dpkms..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(DPKMS_BINARY) $(DPKMS_MAIN)
	@echo "✓ Built: $(DPKMS_BINARY)"

## install: Install both binaries to $GOPATH/bin
install: build
	@echo "Installing binaries..."
	go install $(LDFLAGS) ./cmd/ctxt
	go install $(LDFLAGS) ./cmd/dpkms
	@echo "✓ Installed to $(shell go env GOPATH)/bin"

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	@echo "✓ Cleaned"

## test: Run tests
test:
	@echo "Running tests..."
	go test -race -count=1 -cover ./...

## test-unit: Run unit tests only
test-unit:
	@echo "Running unit tests..."
	go test -race -count=1 ./cmd/... ./internal/...

## test-integration: Run integration tests
test-integration:
	@echo "Running integration tests..."
	go test -race -count=1 ./test/integration/...

## test-smoke: Run binary smoke tests
test-smoke:
	@echo "Running smoke tests..."
	go test -tags=smoke ./test/smoke/...

## test-all: Run all test tiers
test-all: test-unit test-integration test-smoke

## test-cover: Generate coverage report
test-cover:
	@echo "Running tests with coverage..."
	go test -race -coverprofile=coverage.out ./...

## test-gate: Run all tests and print coverage summary
test-gate: test-all
	@echo ""
	@echo "Coverage summary:"
	@go test -race -coverprofile=coverage.out ./internal/... ./cmd/... 2>&1 | grep -E 'coverage:|FAIL'
	@echo ""
	@go tool cover -func=coverage.out | grep total:
	@echo ""
	@echo "✓ Test gate passed"

## lint: Run linters
lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Install with:"; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

## fmt: Format Go code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "✓ Formatted"

## tidy: Tidy Go modules
tidy:
	@echo "Tidying Go modules..."
	go mod tidy
	@echo "✓ Tidied"

## run-ctxt: Build and run ctxt
run-ctxt: build-ctxt
	$(CTXT_BINARY)

## run-dpkms: Build and run dpkms
run-dpkms: build-dpkms
	$(DPKMS_BINARY)

## version: Show version information
version:
	@echo "Version:    $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Git Commit: $(GIT_COMMIT)"

## docs: Build documentation site
docs:
	@echo "Building docs..."
	@cd docs/public && pnpm build
	@echo "✓ Docs built to docs/public/dist"

## docs-dev: Start documentation dev server
docs-dev:
	@echo "Starting docs dev server..."
	@cd docs/public && pnpm dev

## docker-build: Build production runtime image
docker-build:
	@echo "Building ctxt runtime image..."
	docker compose build dpkms
	@echo "✓ Image built: ctxt/dpkms:latest"

## docker-dev: Start dev environment (hot-reload API + Vite UI)
docker-dev:
	@echo "Starting ctxt dev environment..."
	docker compose --profile dev up

## docker-prod: Start production stack in background (Caddy + dpkms)
docker-prod:
	@echo "Starting ctxt production stack..."
	docker compose --profile prod up -d
	@echo "✓ Running. Check status: make docker-ps"

## docker-down: Stop all ctxt containers
docker-down:
	@echo "Stopping ctxt containers..."
	docker compose --profile dev --profile prod down
	@echo "✓ Stopped"

## docker-logs: Tail logs from all running ctxt containers
docker-logs:
	docker compose logs -f

## docker-ps: Show status of all ctxt containers
docker-ps:
	docker compose ps

## docker-shell: Open a shell in the running dpkms container
docker-shell:
	docker exec -it ctxt-dpkms sh

## build-ui: Build web UI assets and copy into internal/ui/dist
build-ui:
	cd web/ui && npm ci && npm run build
	rm -rf internal/ui/dist
	cp -r web/ui/dist internal/ui/dist

.PHONY: build-ui

## security-scan: Run gitleaks secret scanning on entire repo history
security-scan:
	@echo "Running gitleaks secret scan..."
	@if command -v gitleaks >/dev/null 2>&1; then \
		gitleaks detect --source . --config .gitleaks.toml --verbose; \
	else \
		echo "gitleaks not installed. Install with:"; \
		echo "  brew install gitleaks"; \
		echo "  or: https://github.com/gitleaks/gitleaks#installing"; \
		exit 1; \
	fi

## install-hooks: Install git pre-commit hook running gitleaks protect --staged
install-hooks:
	@echo "Installing git hooks..."
	@mkdir -p .git/hooks
	@printf '#!/bin/sh\n# Pre-commit hook: secret scanning via gitleaks\nif ! command -v gitleaks >/dev/null 2>&1; then\n  echo "WARNING: gitleaks not found; skipping secret scan."\n  echo "Install: brew install gitleaks"\n  exit 0\nfi\ngitleaks protect --staged --config .gitleaks.toml --verbose\n' > .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "✓ pre-commit hook installed (.git/hooks/pre-commit)"

## help: Show this help message
help:
	@echo "ContextHelp Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
