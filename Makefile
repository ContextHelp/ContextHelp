.PHONY: all build build-ctxt build-dpkms clean install deps test test-unit test-integration test-integration-services test-smoke test-e2e test-all test-cover test-gate test-docker toolchain-check lint gosec fmt help docs docs-dev docker-build docker-dev docker-prod docker-down docker-logs docker-ps docker-shell security-scan install-hooks install-gitleaks secret-scan-local vuln-scan trivy-scan eva check ben ben-text-short ben-vector ben-install ben-adapter

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# CGo is required for mattn/go-sqlite3 + sqlite-vec extension.
# FTS5 support requires the fts5 build tag with mattn/go-sqlite3.
export CGO_ENABLED := 1
BUILD_TAGS := -tags fts5

# Pinned tools. scripts/ci/tools.sh runs each command with the version
# mise.toml (or packageManager, for pnpm) pins, and fails loudly when it is
# missing or another version is first on PATH. CI targets and every lint
# path go through these; see docs/ci-cd.md#local-parity.
CI_TOOLS := $(CURDIR)/scripts/ci/tools.sh
GO := $(CI_TOOLS) go -- go
GOLANGCI := $(CI_TOOLS) golangci-lint,go -- golangci-lint
GITLEAKS := $(CI_TOOLS) gitleaks -- gitleaks
PNPM := $(CI_TOOLS) node,pnpm -- pnpm
# Tools mise.toml does not pin; `go run pkg@version` builds exactly this
# release with the pinned Go, here and in CI.
GOSEC_VERSION := v2.29.0
GOVULNCHECK_VERSION := v1.8.0

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

## test: Run every test CI runs, stopping at the first failure
##
## Runs the CI jobs in CI_TEST_JOBS (unit and e2e, viewer JS, Postgres
## integration) through their ci-<job> targets, so the commands, flags and
## order are CI's. Integration needs the Postgres service CI provides; without
## one it fails and prints the docker command. scripts/check-ci-parity.sh
## fails when a ci.yml job that runs tests is missing from CI_TEST_JOBS.
test:
	@for job in $(CI_TEST_JOBS); do $(CI_STEP) ci-$$job || exit 1; done

## test-unit: Run unit tests only
test-unit:
	@echo "Running unit tests..."
	go test -failfast $(BUILD_TAGS) -race -count=1 ./cmd/... ./internal/...

## test-integration: Run integration tests
test-integration:
	@echo "Running integration tests..."
	go test -failfast $(BUILD_TAGS) -race -count=1 ./test/integration/...

## test-integration-services: Run the CI integration job's tests (needs Postgres)
##
## Same as `make ci-integration`, which runs the integration job's commands,
## Postgres env and order. See the CI parity section below.
test-integration-services: ci-integration

## test-smoke: Run binary smoke tests
test-smoke:
	@echo "Running smoke tests..."
	CGO_ENABLED=1 go test -failfast -count=1 -tags 'fts5 smoke' ./test/smoke/...

## test-e2e: Run black-box e2e tests against a freshly built ctxt binary
##
## Suites under test/e2e/ carrying the e2e build tag; each builds its own
## binary and runs in a throwaway HOME. Headless-browser smoke tests run
## when Chrome/Chromium is found (CTXT_CHROME picks one; CTXT_CHROME=off
## skips them). The CI test job's e2e step is `make ci-test-e2e` (verbose).
test-e2e:
	@echo "Running e2e tests..."
	CGO_ENABLED=1 go test -failfast -count=1 -tags 'fts5 e2e' ./test/e2e/...

## test-all: Run all test tiers (Go tiers + search-graph viewer JS unit tests)
test-all: test-unit test-searchgraph-viewer-js test-integration test-smoke test-e2e

## test-cover: Generate coverage report
test-cover:
	@echo "Running tests with coverage..."
	go test -failfast $(BUILD_TAGS) -race -coverprofile=coverage.out ./...

## test-gate: Run all tests and print coverage summary
test-gate: test-all
	@echo ""
	@echo "Coverage summary:"
	@go test -failfast $(BUILD_TAGS) -race -coverprofile=coverage.out ./internal/... ./cmd/... 2>&1 | grep -E 'coverage:|FAIL'
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
		go test -failfast $(BUILD_TAGS) -race -count=1 ./...

## eva: Run hop.top/eva contract tests against the recorded fixture set
##
## Tier-1 deterministic JSON-Schema contracts for operator-facing JSON
## shapes (ADR-070 §6). Contracts live under contracts/*.eva.yaml
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
vuln-scan: govulncheck
	@echo "Running nancy (Sonatype OSS Index)..."
	@if ! command -v nancy >/dev/null 2>&1; then \
		echo "Installing nancy..."; \
		go install github.com/sonatype-nexus-community/nancy@v1.2.0; \
	fi
	go list -json -deps ./... | nancy sleuth --exclude-vulnerability-file .nancy-ignore
	@echo "✓ Vulnerability scan complete"

## govulncheck: Report known vulnerabilities in reachable code (CI security job)
govulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) -tags fts5 ./...

## toolchain-check: Assert mise.toml, Dockerfiles and go.mod agree on versions
toolchain-check:
	@scripts/check-toolchain-pins.sh

## lint: Run linters (golangci-lint full scan + gosec), pinned versions
##
## The full scan is the blocking gate in CI (make ci-lint-full); the tree is
## at zero findings at the pinned version. make lint-new narrows it to your
## branch for a faster loop.
lint: gosec
	@echo "Running linters..."
	$(GOLANGCI) run

## gosec: Run gosec static security analysis (fails on medium+ severity)
##
## -tags fts5 is required, not cosmetic. The storage layer is compiled
## against SQLite's FTS5 module and guards the tag with a build sentinel,
## so without it 34 packages fail to type-check and gosec logs "skipping
## SSA analysis" and moves on: it reports success for code it never looked
## at.
##
## install_launchd.go writes to ~/Library/LaunchAgents and
## ~/.config/systemd/user. launchd and systemd must traverse those
## directories and read the unit file, so 0755/0644 is required there
## rather than an oversight. Scoped to the one file so the rules stay live
## everywhere else.
##
## Prints findings as text and writes gosec-results.sarif, which the CI
## security job uploads to GitHub code scanning.
gosec:
	@echo "Running gosec security scan..."
	$(GO) run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) \
		-tags fts5 -severity medium -confidence medium \
		-exclude G104,G304,G307 \
		-exclude-rules "cmd/dpkms/cmd/install_launchd.go:G301,G306" \
		-exclude-dir vendor -exclude-dir docs -exclude-dir test/fixtures \
		-fmt sarif -out gosec-results.sarif -stdout -verbose text \
		./...

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

# Compose env file (docker/.env.<profile>.example): every compose command
# needs one, since DPKMS_BUS_TOKEN is required. docker-dev uses the dev file.
DOCKER_ENV_FILE ?= docker/.env.prod

## docker-build: Build production runtime image
docker-build:
	@echo "Building ctxt runtime image..."
	docker compose --env-file $(DOCKER_ENV_FILE) build dpkms
	@echo "✓ Image built: ctxt/dpkms:latest"

## docker-dev: Start dev environment (hot-reload API + Vite UI)
docker-dev:
	@echo "Starting ctxt dev environment..."
	docker compose --env-file docker/.env.dev --profile dev up

## docker-prod: Start production stack in background (Caddy + dpkms)
docker-prod:
	@echo "Starting ctxt production stack..."
	docker compose --env-file $(DOCKER_ENV_FILE) --profile prod up -d
	@echo "✓ Running. Check status: make docker-ps"

## docker-down: Stop all ctxt containers
docker-down:
	@echo "Stopping ctxt containers..."
	docker compose --env-file $(DOCKER_ENV_FILE) --profile dev --profile prod down
	@echo "✓ Stopped"

## docker-logs: Tail logs from all running ctxt containers
docker-logs:
	docker compose --env-file $(DOCKER_ENV_FILE) logs -f

## docker-ps: Show status of all ctxt containers
docker-ps:
	docker compose --env-file $(DOCKER_ENV_FILE) ps

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

## build-ui: Rebuild the embedded web UI into internal/ui/dist
##
## Vite's outDir is ../../internal/ui/dist (web/ui/vite.config.ts), with
## emptyOutDir, so the build replaces the embedded bundle in place; there is
## no web/ui/dist. web/ui pins pnpm via packageManager and carries its own
## pnpm-workspace.yaml, so pnpm never resolves an enclosing workspace.
build-ui:
	cd web/ui && $(PNPM) install --frozen-lockfile && $(PNPM) run build

## check-ui-dist: Rebuild the web UI; fail if the committed bundle drifts
##
## Rebuilds internal/ui/dist in place, then compares it with the index:
## unstaged edits or new untracked files mean the committed bundle does not
## match web/ui. Overwrites any unstaged dist edits. Fix a failure with
## `make build-ui` and commit the result. CI's webui job runs this target.
UI_DIST := internal/ui/dist
check-ui-dist: build-ui
	@untracked=$$(git ls-files --others --exclude-standard -- $(UI_DIST)); \
	if ! git diff --quiet -- $(UI_DIST) || [ -n "$$untracked" ]; then \
		git diff --stat -- $(UI_DIST); \
		[ -z "$$untracked" ] || printf 'untracked: %s\n' $$untracked; \
		echo "ERROR: $(UI_DIST) is stale; run 'make build-ui' and commit it" >&2; \
		exit 1; \
	fi
	@echo "✓ $(UI_DIST) matches web/ui sources"

.PHONY: build-ui check-ui-dist

## build-searchgraph-viewer: Rebuild the embedded search-graph viewer into internal/searchgraph/viewer/dist
build-searchgraph-viewer:
	cd web/searchgraph && $(PNPM) install --frozen-lockfile && $(PNPM) run build

## test-searchgraph-viewer-js: Run the search-graph viewer's JS unit tests
##
## Dependency-free `node --test` over web/searchgraph/src/*.test.js, so no
## install step. web/searchgraph pins pnpm via packageManager and carries its
## own pnpm-workspace.yaml, so pnpm never resolves an enclosing workspace.
## CI's viewer job runs this target.
test-searchgraph-viewer-js:
	@echo "Running search-graph viewer JS tests..."
	cd web/searchgraph && $(PNPM) test

## check-searchgraph-viewer-dist: Rebuild the viewer; fail if the committed bundle drifts
##
## Rebuilds internal/searchgraph/viewer/dist in place, then compares it with
## the index: unstaged edits or new untracked files mean the committed bundle
## does not match its sources. Overwrites any unstaged dist edits. Fix a
## failure with `make build-searchgraph-viewer` and commit the result.
## CI's viewer job runs this target.
SEARCHGRAPH_VIEWER_DIST := internal/searchgraph/viewer/dist
check-searchgraph-viewer-dist: build-searchgraph-viewer
	@untracked=$$(git ls-files --others --exclude-standard -- $(SEARCHGRAPH_VIEWER_DIST)); \
	if ! git diff --quiet -- $(SEARCHGRAPH_VIEWER_DIST) || [ -n "$$untracked" ]; then \
		git diff --stat -- $(SEARCHGRAPH_VIEWER_DIST); \
		[ -z "$$untracked" ] || printf 'untracked: %s\n' $$untracked; \
		echo "ERROR: $(SEARCHGRAPH_VIEWER_DIST) is stale; run 'make build-searchgraph-viewer' and commit it" >&2; \
		exit 1; \
	fi
	@echo "✓ $(SEARCHGRAPH_VIEWER_DIST) matches web/searchgraph sources"

.PHONY: build-searchgraph-viewer test-searchgraph-viewer-js check-searchgraph-viewer-dist

## security-scan: Scan git history for secrets not in the baseline
##
## Scans every commit reachable from SCAN_REFS (default HEAD; pass
## SCAN_REFS=--all for every local ref). .gitleaksignore is the baseline:
## reviewed findings in past commits, skipped by fingerprint, so only new
## findings fail the scan. CI's secret-scan job runs this target.
SCAN_REFS ?= HEAD
security-scan: install-gitleaks
	@echo "Scanning git history ($(SCAN_REFS)) for secrets..."
	$(GITLEAKS) git . --config .gitleaks.toml --gitleaks-ignore-path .gitleaksignore \
		--log-opts="$(SCAN_REFS)" --redact --verbose --no-banner
	@echo "✓ No new secrets in history"

## install-hooks: Point git at the repo's .githooks directory
##
## Installs a pre-commit gitleaks scan. The pre-push hook chains to the
## user's global one, since core.hooksPath replaces rather than extends.
install-hooks:
	@echo "Installing git hooks..."
	@git config core.hooksPath .githooks
	@chmod +x .githooks/*
	@echo "✓ hooks installed (core.hooksPath -> .githooks)"
	@echo "  pre-commit: gitleaks secret scan, then the lint gate"
	@echo "              (.pre-commit-config.yaml, scoped to staged files)"
	@echo "  pre-push:   chains to your global pre-push hook, if any"
	@command -v gitleaks >/dev/null 2>&1 || echo "  NOTE: gitleaks not installed; run 'make install-gitleaks'"
	@command -v pre-commit >/dev/null 2>&1 \
		&& pre-commit install-hooks \
		|| echo "  NOTE: pre-commit not installed; lint gate will be skipped. See https://pre-commit.com"

## install-gitleaks: Install the gitleaks version pinned in mise.toml
##
## Installs through mise (the pin's home); brew cannot pin a version. CI
## installs the same release from GitHub (.github/workflows/ci.yml).
GITLEAKS_VERSION := $(shell sed -n 's/^gitleaks = "\(.*\)"$$/\1/p' mise.toml)
install-gitleaks:
	@have=$$(gitleaks version 2>/dev/null || true); have=$${have#v}; \
	if [ "$$have" = "$(GITLEAKS_VERSION)" ]; then \
		echo "✓ gitleaks $$have installed (mise.toml pin)"; \
	elif command -v mise >/dev/null 2>&1; then \
		echo "Installing gitleaks $(GITLEAKS_VERSION) via mise..."; \
		mise install gitleaks@$(GITLEAKS_VERSION); \
	else \
		echo "gitleaks $(GITLEAKS_VERSION) required (found: $${have:-none})."; \
		echo "Install mise (https://mise.jdx.dev), then rerun, or fetch the"; \
		echo "v$(GITLEAKS_VERSION) release: https://github.com/gitleaks/gitleaks/releases"; \
		exit 1; \
	fi

## secret-scan-local: Scan the working tree for secrets with gitleaks
##
## Tracked files plus untracked, non-ignored ones; history is `security-scan`.
secret-scan-local: install-gitleaks
	@echo "Scanning working tree for secrets..."
	scripts/secret-scan-tree.sh
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
## is intentionally low until the real candidate
## model; raise it when the embedding leg lights up.
BEN_VECTOR_FLOOR := 0.40
ben-vector: ben-install ben-adapter
	@mkdir -p $(BEN_RUN_DIR)
	@echo "Running ben suite: recall-vector.ben.yaml (floor=$(BEN_VECTOR_FLOOR))"
	@PATH="$(abspath $(BUILD_DIR)):$$PATH" $(BEN_BINARY) run \
		--suite suites/recall-vector.ben.yaml \
		--format json > $(BEN_RUN_DIR)/recall-vector.json
	@bash scripts/ben-floor.sh $(BEN_RUN_DIR)/recall-vector.json $(BEN_VECTOR_FLOOR)

# ─── CI parity ───────────────────────────────────────────────────────────────
#
# Every check .github/workflows/ci.yml runs is a target here: each CI step
# is `make <target>`, and ci-<job> runs the same targets in the same order,
# so a local run executes CI's commands with CI's flags, tags, env and tool
# versions. scripts/check-ci-parity.sh (make ci-parity-check, run by CI's
# toolchain job) fails when the two drift apart. To change what CI runs,
# change the target; add a CI step by adding a target, then a
# `$(CI_STEP) <target>` line to ci-<job>.
#
# What a laptop cannot reproduce (Linux and macOS runners, the Postgres
# service, mingw-w64, Chrome, CI-only uploads) is listed in docs/ci-cd.md
# (#local-parity); scripts/ci/prereq.sh reports each missing one instead
# of letting a step pass without it.

# CI's jobs, in workflow order.
CI_JOBS := toolchain secret-scan lint windows security test viewer webui integration build
# The CI_JOBS that run tests, in workflow order; `make test` runs exactly these.
CI_TEST_JOBS := test viewer integration
CI_STEP := $(MAKE) --no-print-directory
CI_PREREQ := $(CURDIR)/scripts/ci/prereq.sh
CI_WORKFLOW := .github/workflows/ci.yml

## ci: Run every CI job locally, then print a per-job verdict
##
## Jobs run in CI_JOBS order and a failure does not stop the rest, as in
## CI. A job whose runner-provided prerequisite is missing (Postgres,
## mingw-w64) is SKIPPED with how to run it, and the run ends INCOMPLETE;
## CI_STRICT=1 makes that exit non-zero. For one job, make ci-<job>.
ci:
	@scripts/ci/run-all.sh $(CI_JOBS)

## ci-<job>: Run one CI job locally (ci-toolchain, ci-lint, ci-test, ...; see CI_JOBS)

## check-operator-impact: The release-notes check CI runs on a PR, over this branch
##
## Every commit since the merge base with LINT_BASE_REF needs a valid
## Operator-Impact trailer, and a non-none bucket a release-notes row naming
## its short SHA (ADR-070). Same script as the Release Notes Check workflow.
check-operator-impact:
	@scripts/ci/check-operator-impact.sh "$$(git merge-base HEAD $(LINT_BASE_REF))" HEAD

## ci-parity-check: Fail when ci.yml and the ci-<job> targets disagree
ci-parity-check:
	@scripts/check-ci-parity.sh

# go.mod may carry local-path replace directives for these sibling modules
# (developer convenience); CI drops them so the published versions build.
CI_DROP_REPLACES := hop.top/kit hop.top/c12n

## ci-drop-replaces: CI drops local-path replaces; a local run refuses them
##
## On GitHub Actions this edits go.mod. A local run must not, so it fails
## instead when the edit would change go.mod: with such a replace in place
## the local build is not the one CI tests.
ci-drop-replaces:
	@args="$(foreach m,$(CI_DROP_REPLACES),-dropreplace=$(m))"; \
	if [ "$$GITHUB_ACTIONS" = true ]; then \
		$(GO) mod edit $$args; \
	elif [ "$$($(GO) mod edit -print)" != "$$($(GO) mod edit $$args -print)" ]; then \
		echo "ERROR: go.mod replaces $(CI_DROP_REPLACES) with a local path; CI drops that" >&2; \
		echo "and builds the published version. Remove it (go mod edit $$args)" >&2; \
		echo "or run from a clean clone." >&2; \
		exit 1; \
	fi

ci-toolchain:
	$(CI_STEP) toolchain-check
	$(CI_STEP) ci-parity-check

ci-secret-scan:
	$(CI_STEP) security-scan

# Findings new since LINT_BASE fail. CI passes the PR's base commit (HEAD~1
# on a push); locally it defaults to the merge base with LINT_BASE_REF, the
# branch a PR would target.
LINT_BASE ?=
LINT_BASE_REF ?= origin/next

ci-lint:
	$(CI_STEP) ci-drop-replaces
	$(CI_STEP) ci-lint-full
	$(CI_STEP) ci-gofmt
	$(CI_STEP) ci-vet

# Local only: findings your branch introduces, for a faster loop. CI runs
# the full scan (ci-lint-full), which is a superset of this.
lint-new:
	@base="$(LINT_BASE)"; \
	if [ -z "$$base" ]; then \
		base=$$(git merge-base HEAD $(LINT_BASE_REF)) || { \
			echo "ERROR: no merge base with $(LINT_BASE_REF); pass LINT_BASE=<commit> (CI uses the PR base)" >&2; \
			exit 1; \
		}; \
		echo "lint base: $$base (merge base with $(LINT_BASE_REF))"; \
	fi; \
	echo "$(GOLANGCI) run --timeout=10m --new-from-rev=$$base"; \
	$(GOLANGCI) run --timeout=10m --new-from-rev="$$base"

# Blocking in CI and in ci-lint: the whole repo is at zero findings at the
# pinned version, so every finding is new.
ci-lint-full:
	$(GOLANGCI) run --timeout=10m

ci-gofmt:
	$(CI_TOOLS) go -- sh -c 'if [ -n "$$(gofmt -l .)" ]; then echo "Go code is not formatted. Run: gofmt -w ."; gofmt -d .; exit 1; fi'

ci-vet:
	$(GO) vet -tags fts5 ./...

# GoReleaser ships windows/amd64 binaries, cross-compiled with CGO and
# mingw-w64. Type-check every package, tests included, for that target,
# then build the two binaries as GoReleaser does.
#
# mingw-w64 ships no sqlite3.h, which sqlite-vec's cgo package includes;
# scripts/ci/sqlite3-include.sh puts the header of the amalgamation
# mattn/go-sqlite3 compiles there, and CGO_CFLAGS adds it (on top of Go's
# default -O2 -g). .goreleaser.yaml's windows builds do the same.
CI_WINDOWS_INCLUDE := $(CURDIR)/build/cgo-include/windows
CI_WINDOWS_ENV := GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
	CGO_CFLAGS='-O2 -g -I$(CI_WINDOWS_INCLUDE)'
CI_WINDOWS_PREP := $(CI_PREREQ) mingw && $(CI_TOOLS) go -- scripts/ci/sqlite3-include.sh $(CI_WINDOWS_INCLUDE)

ci-windows:
	$(CI_STEP) ci-drop-replaces
	$(CI_STEP) ci-windows-vet
	$(CI_STEP) ci-windows-build

ci-windows-vet:
	@$(CI_WINDOWS_PREP)
	$(CI_WINDOWS_ENV) $(GO) vet -tags fts5 ./...

ci-windows-build:
	@$(CI_WINDOWS_PREP)
	$(CI_WINDOWS_ENV) $(GO) build -tags fts5 ./cmd/ctxt ./cmd/dpkms

ci-security:
	$(CI_STEP) ci-drop-replaces
	$(CI_STEP) gosec
	$(CI_STEP) govulncheck

ci-test:
	$(CI_STEP) ci-drop-replaces
	$(CI_STEP) ci-test-unit
	$(CI_STEP) ci-test-e2e

# -timeout 30m: cmd/ctxt/cmd alone runs ~5 min under -race on a laptop and
# runners are slower; the default 10 min per package is too close.

ci-test-unit:
	$(GO) test -failfast -v -short -race -timeout 30m -tags fts5 -coverprofile=coverage.out -covermode=atomic ./...

# Black-box suites under test/e2e/ tagged e2e: each builds ctxt, seeds a
# throwaway store through the binary and asserts exit codes, output and
# files. Headless-browser checks run when Chrome is found and skip
# otherwise. Other test/e2e suites carry their own tags.
ci-test-e2e:
	@$(CI_PREREQ) chrome
	$(GO) test -failfast -v -count=1 -tags 'fts5 e2e' ./test/e2e/...

ci-viewer:
	$(CI_STEP) test-searchgraph-viewer-js
	$(CI_STEP) check-searchgraph-viewer-dist

ci-webui:
	$(CI_STEP) check-ui-dist

# Postgres the integration job's service runs. Credentials default to the
# service's env block in ci.yml; export POSTGRES_* to point elsewhere.
CI_INTEGRATION_STEPS := ci-integration-suites ci-integration-jobs ci-integration-http ci-integration-journey ci-integration-searchgraph
ci_service_env = $(shell sed -n 's/^ *$(1): *//p' $(CI_WORKFLOW) | head -1)
$(CI_INTEGRATION_STEPS): export POSTGRES_HOST ?= localhost
$(CI_INTEGRATION_STEPS): export POSTGRES_PORT ?= 5432
$(CI_INTEGRATION_STEPS): export POSTGRES_USER ?= $(call ci_service_env,POSTGRES_USER)
$(CI_INTEGRATION_STEPS): export POSTGRES_PASSWORD ?= $(call ci_service_env,POSTGRES_PASSWORD)
$(CI_INTEGRATION_STEPS): export POSTGRES_DB ?= $(call ci_service_env,POSTGRES_DB)

ci-integration:
	$(CI_STEP) ci-drop-replaces
	$(CI_STEP) ci-integration-suites
	$(CI_STEP) ci-integration-jobs
	$(CI_STEP) ci-integration-http
	$(CI_STEP) ci-integration-journey
	$(CI_STEP) ci-integration-searchgraph

# The build-tagged suites under test/integration/ plus the Postgres driver,
# storage factory and embeddings registry suites, which live next to their
# code; all read the POSTGRES_* env, so each tree must be listed or its
# Postgres coverage never runs.
ci-integration-suites:
	@$(CI_PREREQ) postgres
	$(GO) test -failfast -v -tags=integration,fts5 \
		./test/integration/... \
		./internal/storage/postgres/... \
		./internal/storageutil/... \
		./internal/embeddings/registry/...

# Task-job claim and re-projection on Postgres (the SQLite variants run in
# the test job).
ci-integration-jobs:
	@$(CI_PREREQ) postgres
	$(GO) test -failfast -v -tags=integration,fts5 -count=1 \
		-run 'Test(Claim|Reproject)_.*_Postgres' ./internal/jobs/

# HTTP handlers against a Postgres store; the package's other tests run in
# the test job.
ci-integration-http:
	@$(CI_PREREQ) postgres
	$(GO) test -failfast -v -tags=integration,fts5 -count=1 \
		-run '_Postgres$$' ./internal/server/http/

# The embeddings journey drives the built ctxt and dpkms binaries.
ci-integration-journey:
	@$(CI_PREREQ) postgres
	$(GO) test -failfast -v -tags=integration,fts5 -count=1 \
		-run TestEmbeddingsJourney_Postgres ./cmd/ctxt/cmd/

# The search-graph endpoint's Postgres run needs the e2e and integration
# tags (its SQLite run is part of ci-test-e2e).
ci-integration-searchgraph:
	@$(CI_PREREQ) postgres
	$(GO) test -failfast -v -count=1 -tags 'fts5 e2e integration' \
		-run TestDpkmsGraph_Postgres ./test/e2e/searchgraph/

ci-build:
	$(CI_STEP) ci-drop-replaces
	$(CI_STEP) ci-build-dpkms
	$(CI_STEP) ci-build-ctxt
	$(CI_STEP) ci-build-verify

ci-build-dpkms:
	$(GO) build -v -tags fts5 -o bin/dpkms ./cmd/dpkms

ci-build-ctxt:
	$(GO) build -v -tags fts5 -o bin/ctxt ./cmd/ctxt

ci-build-verify:
	./bin/dpkms version || echo "dpkms built successfully"
	./bin/ctxt version || echo "ctxt built successfully"

.PHONY: ci ci-parity-check check-operator-impact ci-drop-replaces govulncheck \
	$(addprefix ci-,$(CI_JOBS)) $(CI_INTEGRATION_STEPS) \
	lint-new ci-lint-full ci-gofmt ci-vet ci-windows-vet ci-windows-build \
	ci-test-unit ci-test-e2e ci-build-dpkms ci-build-ctxt ci-build-verify

## help: Show this help message
help:
	@echo "ContextHelp Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
