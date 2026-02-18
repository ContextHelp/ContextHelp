.PHONY: all build build-ctxt build-dpkms clean install test test-unit test-integration test-smoke test-all test-cover test-gate lint fmt help

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

## help: Show this help message
help:
	@echo "ContextHelp Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
