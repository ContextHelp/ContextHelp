.PHONY: all build build-ctxt build-dpkms clean install deps test test-unit test-integration test-smoke test-all test-cover test-gate test-docker lint gosec fmt help docs docs-dev docker-build docker-dev docker-prod docker-down docker-logs docker-ps docker-shell security-scan install-hooks vuln-scan trivy-scan eva check ben ben-text-short ben-vector ben-install ben-adapter

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# CGo is required for mattn/go-sqlite3 + sqlite-vec extension.
# FTS5 support requires the fts5 build tag with mattn/go-sqlite3.
export CGO_ENABLED := 1
BUILD_TAGS := -tags fts5

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
	go build $(BUILD_TAGS) $(LDFLAGS) -o $(CTXT_BINARY) $(CTXT_MAIN)
	@echo "✓ Built: $(CTXT_BINARY)"

## build-dpkms: Build the dpkms binary
build-dpkms:
	@echo "Building dpkms..."
	@mkdir -p $(BUILD_DIR)
	go build $(BUILD_TAGS) $(LDFLAGS) -o $(DPKMS_BINARY) $(DPKMS_MAIN)
	@echo "✓ Built: $(DPKMS_BINARY)"

## install: Install both binaries to $GOPATH/bin
install: build
	@echo "Installing binaries..."
	go install $(BUILD_TAGS) $(LDFLAGS) ./cmd/ctxt
	go install $(BUILD_TAGS) $(LDFLAGS) ./cmd/dpkms
	@echo "✓ Installed to $(shell go env GOPATH)/bin"

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	@echo "✓ Cleaned"

## test: Run tests
test:
	@echo "Running tests..."
	go test $(BUILD_TAGS) -race -count=1 -cover ./...

## test-unit: Run unit tests only
test-unit:
	@echo "Running unit tests..."
	go test $(BUILD_TAGS) -race -count=1 ./cmd/... ./internal/...

## test-integration: Run integration tests
test-integration:
	@echo "Running integration tests..."
	go test $(BUILD_TAGS) -race -count=1 ./test/integration/...

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

## test-docker: Run the test suite inside the canonical Linux/Go container
##
## Useful when local Go (mise/brew) doesn't match go.mod, or when CGo
## extensions (sqlite3, fts5, sqlite-vec) misbehave on macOS. Builds the
## Dockerfile builder stage once, mounts the repo read-write so the build
## cache and test artifacts persist across runs, and runs the full test
## tree with the canonical $(BUILD_TAGS).
test-docker:
	@echo "Building test image (golang:1.26-bookworm + sqlite-dev)..."
	@docker build --target go-builder -t ctxt/test:latest -f Dockerfile .
	@echo "Running tests inside container..."
	docker run --rm \
		-v "$(PWD)":/app \
		-v ctxt_test_gocache:/root/.cache/go-build \
		-v ctxt_test_gomod:/go/pkg/mod \
		-w /app \
		-e CGO_ENABLED=1 \
		-e GOWORK=off \
		ctxt/test:latest \
		go test $(BUILD_TAGS) -race -count=1 ./...

## eva: Run hop.top/eva contract tests against the recorded fixture set
##
## Tier-1 deterministic JSON-Schema contracts for operator-facing JSON
## shapes (ADR-070 §6, T-0586). Contracts live under contracts/*.eva.yaml
## and are paired with one or more fixtures under
## test/integration/testdata/eva-fixtures/<contract-base>*.json.
##
## Requires the eva CLI on PATH (https://github.com/hop-top/eva). Override
## with EVA_BIN=/path/to/eva when running from a non-standard install. CI
## pins the version in .github/workflows/eva-contracts.yml.
eva:
	@echo "Running eva contract tests..."
	@./scripts/run-eva-contracts.sh

## check: Run the pre-merge gate (test + eva contracts)
##
## Mirrors the CI default lane: every commit must pass tests AND every
## operator-facing JSON shape must conform to its eva contract. New
## contracts added under contracts/*.eva.yaml are picked up automatically.
check: test eva

## vuln-scan: Run govulncheck + nancy dependency vulnerability scans
vuln-scan:
	@echo "Running govulncheck..."
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		echo "Installing govulncheck..."; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
	fi
	govulncheck ./...
	@echo "Running nancy (Sonatype OSS Index)..."
	@if ! command -v nancy >/dev/null 2>&1; then \
		echo "Installing nancy..."; \
		go install github.com/sonatype-nexus-community/nancy@latest; \
	fi
	go list -json -deps ./... | nancy sleuth --exclude-vulnerability-file .nancy-ignore
	@echo "✓ Vulnerability scan complete"

## lint: Run linters (golangci-lint + gosec)
lint: gosec
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Install with:"; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

## gosec: Run gosec static security analysis (fails on medium+ severity)
gosec:
	@echo "Running gosec security scan..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec -conf .gosec.yaml -severity medium -confidence medium \
			-exclude G104,G304,G307 \
			-fmt text ./...; \
	else \
		echo "gosec not installed. Install with:"; \
		echo "  go install github.com/securego/gosec/v2/cmd/gosec@latest"; \
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

## trivy-scan: Scan Dockerfile for misconfigs and image for CVEs (requires trivy)
TRIVY_IMAGE ?= ghcr.io/$(shell git remote get-url origin 2>/dev/null | sed 's|.*github.com[:/]||;s|\.git$$||' | tr '[:upper:]' '[:lower:]')/ctxt-dpkms:latest
trivy-scan:
	@echo "Running trivy config scan on Dockerfile..."
	@if ! command -v trivy >/dev/null 2>&1; then \
		echo "trivy not installed. Install: https://aquasecurity.github.io/trivy/latest/getting-started/installation/"; \
		exit 1; \
	fi
	trivy config --exit-code 1 --severity CRITICAL Dockerfile
	@echo "✓ Dockerfile config scan passed"
	@if docker image inspect $(TRIVY_IMAGE) >/dev/null 2>&1; then \
		echo "Running trivy image scan on $(TRIVY_IMAGE)..."; \
		trivy image --exit-code 1 --severity CRITICAL $(TRIVY_IMAGE); \
		echo "✓ Image CVE scan passed"; \
	else \
		echo "Image $(TRIVY_IMAGE) not found locally; skipping image scan."; \
		echo "Run 'make docker-build' first, or set TRIVY_IMAGE=<image> explicitly."; \
	fi

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

## ben: Run all hop.top/ben recall suites (text-short + vector)
##
## Gates pipeline-version + embedding-model PRs per ADR-070 §6 and
## ADR-071 Phase 3. Set BEN=1 to opt in when the suite is part of an
## aggregate test target; running this target directly always executes.
ben: ben-text-short ben-vector
	@echo "✓ All ben recall suites passed"

## ben-install: Install hop.top/ben into bin/ at the version pinned in tools/ben.ref
##
## Resolution order:
##   1. $$BEN_LOCAL_PATH env var, if set and pointing to a hop.top/ben checkout
##   2. Sibling labspace path ($$HOME/.w/ideacrafterslabs/ben/hops/main) if it
##      exists — matches the dev convention used by xrr / kit / c12n
##   3. `go install hop.top/ben/cmd/ben@$(BEN_VERSION)` from the network
##   4. Fallback: build the local ben-compatible runner cmd/ctxt-ben-run.
##      This fallback exists because hop.top/ben at the pinned SHA does
##      not currently build against the restructured hop.top/kit module
##      layout (kit/hops/main moved its packages under go/<area>/ while
##      ben still imports the flat hop.top/kit/<pkg> paths). When ben/main
##      lands the kit-compat fix, drop this fallback and the cmd/ctxt-ben-run
##      directory; suites are already in ben's native YAML format.
##      See docs/ctxt/testing.md "ben recall harness" for the full story.
##
## The installed binary lives in $(BUILD_DIR)/ (not $GOPATH/bin) so the
## version stays scoped to this checkout.
BEN_REF_FILE := tools/ben.ref
BEN_VERSION := $(shell grep '^BEN_VERSION=' $(BEN_REF_FILE) | cut -d= -f2)
BEN_BIN_PATH := $(shell grep '^BEN_BIN_PATH=' $(BEN_REF_FILE) | cut -d= -f2)
BEN_BINARY := $(BUILD_DIR)/ben
BEN_SIBLING := $(HOME)/.w/ideacrafterslabs/ben/hops/main
ben-install: $(BEN_BINARY)

$(BEN_BINARY): $(BEN_REF_FILE)
	@mkdir -p $(BUILD_DIR)
	@set -e; \
	if [ -n "$$BEN_LOCAL_PATH" ] && [ -d "$$BEN_LOCAL_PATH" ]; then \
		echo "Trying upstream ben from BEN_LOCAL_PATH=$$BEN_LOCAL_PATH..."; \
		if (cd "$$BEN_LOCAL_PATH" && go build -o $(abspath $(BEN_BINARY)) ./cmd/ben) 2>/dev/null; then \
			echo "✓ Installed upstream ben: $(BEN_BINARY)"; exit 0; fi; \
	elif [ -d "$(BEN_SIBLING)" ]; then \
		echo "Trying upstream ben from sibling labspace ($(BEN_SIBLING))..."; \
		if (cd "$(BEN_SIBLING)" && go build -o $(abspath $(BEN_BINARY)) ./cmd/ben) 2>/dev/null; then \
			echo "✓ Installed upstream ben: $(BEN_BINARY)"; exit 0; fi; \
	else \
		echo "Trying upstream ben install $(BEN_BIN_PATH)@$(BEN_VERSION)..."; \
		if GOBIN=$(abspath $(BUILD_DIR)) go install $(BEN_BIN_PATH)@$(BEN_VERSION) 2>/dev/null; then \
			echo "✓ Installed upstream ben: $(BEN_BINARY)"; exit 0; fi; \
	fi; \
	echo "Upstream ben unavailable; falling back to local cmd/ctxt-ben-run (see Makefile comment)..."; \
	go build -buildvcs=false -o $(abspath $(BEN_BINARY)) ./cmd/ctxt-ben-run; \
	echo "✓ Built fallback ctxt-ben-run as: $(BEN_BINARY)"

## ben-adapter: Build the ctxt-recall ben binary plugin into bin/
##
## ben discovers binary plugins on PATH; we prepend $(BUILD_DIR) when we
## invoke ben so the plugin is picked up without polluting the user PATH.
BEN_ADAPTER := $(BUILD_DIR)/ben-adapter-ctxt-recall
ben-adapter: $(BEN_ADAPTER)

$(BEN_ADAPTER):
	@echo "Building ben-adapter-ctxt-recall..."
	@mkdir -p $(BUILD_DIR)
	go build -buildvcs=false -o $(BEN_ADAPTER) ./cmd/ben-adapter-ctxt-recall
	@echo "✓ Built: $(BEN_ADAPTER)"

## ben-text-short: Run the text.short recall suite (ADR-070 §6)
##
## Output:
##   $(BUILD_DIR)/ben-runs/recall-text-short.json — full ben run record
##   stdout — pretty-printed pass/fail summary with the recall floor check.
BEN_RUN_DIR := $(BUILD_DIR)/ben-runs
BEN_TEXT_SHORT_FLOOR := 0.85
ben-text-short: ben-install ben-adapter
	@mkdir -p $(BEN_RUN_DIR)
	@echo "Running ben suite: recall-text-short.ben.yaml (floor=$(BEN_TEXT_SHORT_FLOOR))"
	@PATH="$(abspath $(BUILD_DIR)):$$PATH" $(BEN_BINARY) run \
		--suite suites/recall-text-short.ben.yaml \
		--format json > $(BEN_RUN_DIR)/recall-text-short.json
	@bash scripts/ben-floor.sh $(BEN_RUN_DIR)/recall-text-short.json $(BEN_TEXT_SHORT_FLOOR)

## ben-vector: Run the vector-recall suite (ADR-071 Phase 3 gate)
##
## See suites/recall-vector.ben.yaml for the floor-rationale. The floor
## is intentionally low while T-0584 hasn't wired the real candidate
## model; raise it when the embedding leg lights up.
BEN_VECTOR_FLOOR := 0.40
ben-vector: ben-install ben-adapter
	@mkdir -p $(BEN_RUN_DIR)
	@echo "Running ben suite: recall-vector.ben.yaml (floor=$(BEN_VECTOR_FLOOR))"
	@PATH="$(abspath $(BUILD_DIR)):$$PATH" $(BEN_BINARY) run \
		--suite suites/recall-vector.ben.yaml \
		--format json > $(BEN_RUN_DIR)/recall-vector.json
	@bash scripts/ben-floor.sh $(BEN_RUN_DIR)/recall-vector.json $(BEN_VECTOR_FLOOR)

## help: Show this help message
help:
	@echo "ContextHelp Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
