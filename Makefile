.PHONY: all build build-ctxt build-dpkms clean install deps test test-unit test-integration test-integration-services test-smoke test-all test-cover test-gate test-docker toolchain-check lint gosec fmt help docs docs-dev docker-build docker-dev docker-prod docker-down docker-logs docker-ps docker-shell security-scan install-hooks install-gitleaks secret-scan-local vuln-scan trivy-scan eva check ben ben-text-short ben-vector ben-install ben-adapter

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

## test-integration-services: Run integration tests needing Postgres/Redis
#
# The build-tagged integration suites are split across three trees: the ones
# under test/integration/ plus the Postgres driver and embeddings registry
# conformance suites, which live next to the code they cover. All three read
# the same POSTGRES_* env block, so all three must be listed or the Postgres
# coverage never runs. Mirrors the integration job in
# .github/workflows/ci.yml; the devcontainer supplies the env block.
test-integration-services:
	@echo "Running integration tests (requires Postgres + Redis)..."
	go test -v -tags=integration,fts5 -count=1 \
		./test/integration/... \
		./internal/storage/postgres/... \
		./internal/embeddings/registry/...

## test-smoke: Run binary smoke tests
test-smoke:
	@echo "Running smoke tests..."
	go test -tags=smoke ./test/smoke/...

## test-all: Run all test tiers
test-all: test-unit test-integration test-smoke

## test-cover: Generate coverage report
test-cover:
	@echo "Running tests with coverage..."
	go test $(BUILD_TAGS) -race -coverprofile=coverage.out ./...

## test-gate: Run all tests and print coverage summary
test-gate: test-all
	@echo ""
	@echo "Coverage summary:"
	@go test $(BUILD_TAGS) -race -coverprofile=coverage.out ./internal/... ./cmd/... 2>&1 | grep -E 'coverage:|FAIL'
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
	@echo "Building test image (golang from Dockerfile, pinned via mise.toml + sqlite-dev)..."
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
	govulncheck -tags fts5 ./...
	@echo "Running nancy (Sonatype OSS Index)..."
	@if ! command -v nancy >/dev/null 2>&1; then \
		echo "Installing nancy..."; \
		go install github.com/sonatype-nexus-community/nancy@latest; \
	fi
	go list -json -deps ./... | nancy sleuth --exclude-vulnerability-file .nancy-ignore
	@echo "✓ Vulnerability scan complete"

## toolchain-check: Assert mise.toml, Dockerfiles and go.mod agree on versions
toolchain-check:
	@scripts/check-toolchain-pins.sh

## lint: Run linters (golangci-lint + gosec)
lint: gosec
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Install the pinned version with:"; \
		echo "  mise install"; \
		echo "(mise.toml pins the version CI uses; go install @latest drifts)"; \
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

## install-hooks: Point git at the repo's .githooks directory
##
## Installs a pre-commit gitleaks scan. The pre-push hook chains to the
## user's global one, since core.hooksPath replaces rather than extends.
install-hooks:
	@echo "Installing git hooks..."
	@git config core.hooksPath .githooks
	@chmod +x .githooks/*
	@echo "✓ hooks installed (core.hooksPath -> .githooks)"
	@echo "  pre-commit: gitleaks secret scan on staged changes"
	@echo "  pre-push:   chains to your global pre-push hook, if any"
	@command -v gitleaks >/dev/null 2>&1 || echo "  NOTE: gitleaks not installed; run 'make install-gitleaks'"

## install-gitleaks: Install the gitleaks secret scanner locally
##
## Secret scanning runs locally rather than in CI: gitleaks-action requires
## a license for organization-owned repos. See .github/workflows/ci.yml.
install-gitleaks:
	@if command -v gitleaks >/dev/null 2>&1; then \
		echo "✓ gitleaks already installed ($$(gitleaks version 2>/dev/null))"; \
	elif command -v brew >/dev/null 2>&1; then \
		echo "Installing gitleaks via brew..."; \
		brew install gitleaks; \
	else \
		echo "Installing gitleaks via go install..."; \
		go install github.com/zricethezav/gitleaks/v8@latest; \
	fi

## secret-scan-local: Scan the working tree for secrets with gitleaks
secret-scan-local: install-gitleaks
	@echo "Scanning working tree for secrets..."
	gitleaks detect --config .gitleaks.toml --redact --verbose
	@echo "✓ No secrets detected"

## ben: Run all hop.top/ben recall suites (text-short + vector)
##
## Gates pipeline-version + embedding-model PRs per ADR-070 §6 and
## ADR-071 Phase 3. Set BEN=1 to opt in when the suite is part of an
## aggregate test target; running this target directly always executes.
ben: ben-text-short ben-vector
	@echo "✓ All ben recall suites passed"

## ben-install: Build hop.top/ben into bin/ from a local checkout.
##
## Requires BEN_LOCAL_PATH to point at a ben checkout; there is no default,
## since the location depends on how you arrange your checkouts.
##
## ben is consumed via local-path replace, not a published version: it has
## no tagged release yet and is in active local development alongside this
## tree. CI must set BEN_LOCAL_PATH for `make ben` to resolve. See
## docs/ctxt/testing.md "Recall Harness (hop.top/ben)" for the full story.
##
## The built binary lives in $(BUILD_DIR)/ (not $GOPATH/bin) so the version
## stays scoped to this checkout.
BEN_BINARY := $(BUILD_DIR)/ben
ben-install: $(BEN_BINARY)

$(BEN_BINARY):
	@mkdir -p $(BUILD_DIR)
	@set -e; \
	if [ -n "$$BEN_LOCAL_PATH" ] && [ -d "$$BEN_LOCAL_PATH" ]; then \
		echo "Building ben from BEN_LOCAL_PATH=$$BEN_LOCAL_PATH..."; \
		(cd "$$BEN_LOCAL_PATH" && go build -buildvcs=false -o $(abspath $(BEN_BINARY)) ./cmd/ben); \
	else \
		echo "ERROR: hop.top/ben not found." >&2; \
		echo "Set BEN_LOCAL_PATH=<path-to-ben-checkout> and re-run." >&2; \
		exit 1; \
	fi; \
	echo "✓ Built ben: $(BEN_BINARY)"

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
