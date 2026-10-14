#!/usr/bin/env bash
set -euo pipefail

# dev-setup.sh - Development environment setup for ContextHelp
# This script sets up a local-first development environment with all necessary tools

# The script lives in scripts/; the repo root is its parent directory.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"
cd "$PROJECT_ROOT"

# Color output helpers
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info() {
    echo -e "${BLUE}==>${NC} $*"
}

success() {
    echo -e "${GREEN}✓${NC} $*"
}

warn() {
    echo -e "${YELLOW}!${NC} $*"
}

error() {
    echo -e "${RED}✗${NC} $*"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Darwin*)    echo "macos";;
        Linux*)     echo "linux";;
        MINGW*|MSYS*|CYGWIN*) echo "windows";;
        *)          echo "unknown";;
    esac
}

OS=$(detect_os)

# Step 1: Check Go installation
info "Step 1: Checking Go installation..."
if ! command_exists go; then
    error "Go is not installed. Please install a version compatible with go.mod from https://go.dev/dl/"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
REQUIRED_VERSION=$(awk '/^go / {print $2}' go.mod 2>/dev/null)

if [ -z "$REQUIRED_VERSION" ]; then
    error "Unable to determine required Go version from go.mod"
    exit 1
fi

if [ "$(printf '%s\n' "$REQUIRED_VERSION" "$GO_VERSION" | sort -V | head -n1)" != "$REQUIRED_VERSION" ]; then
    error "Go version $GO_VERSION is too old. Minimum required: $REQUIRED_VERSION"
    exit 1
fi

success "Go $GO_VERSION installed"

# Step 2: Create directory structure
info "Step 2: Creating directory structure..."
mkdir -p cmd/dpkms cmd/ctxt
mkdir -p internal/dpkms/storage internal/dpkms/jobs internal/dpkms/graph
mkdir -p internal/ctxt/pipelines internal/ctxt/profiles
mkdir -p pkg/dpkms pkg/ctxt
mkdir -p configs
mkdir -p test/integration test/smoke test/testdata test/testutil
mkdir -p docs/development
success "Directory structure created"

# Step 3: Initialize Go modules if not exists
info "Step 3: Initializing Go modules..."
if [ ! -f "go.mod" ]; then
    go mod init github.com/ideacrafterslabs/contexthelp
    success "go.mod created"
else
    success "go.mod already exists"
fi

# Step 4: Install development tools
info "Step 4: Installing development tools..."

# golangci-lint
if ! command_exists golangci-lint; then
    info "Installing golangci-lint..."
    if [ "$OS" = "macos" ]; then
        brew install golangci-lint || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOPATH)/bin"
    else
        curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOPATH)/bin"
    fi
    success "golangci-lint installed"
else
    success "golangci-lint already installed"
fi

# Task (Go task runner)
if ! command_exists task; then
    info "Installing Task..."
    if [ "$OS" = "macos" ]; then
        brew install go-task/tap/go-task || go install github.com/go-task/task/v3/cmd/task@latest
    else
        go install github.com/go-task/task/v3/cmd/task@latest
    fi
    success "Task installed"
else
    success "Task already installed"
fi

# govulncheck (security scanner)
if ! command_exists govulncheck; then
    info "Installing govulncheck..."
    go install golang.org/x/vuln/cmd/govulncheck@latest
    success "govulncheck installed"
else
    success "govulncheck already installed"
fi

# Step 5: Install Go dependencies
info "Step 5: Installing Go dependencies..."
if [ -f "go.mod" ] && grep -q "require" go.mod; then
    go mod download
    go mod tidy
    success "Go dependencies installed"
else
    info "No dependencies to install yet (go.mod is empty)"
fi

# Step 6: Create local configuration
info "Step 6: Creating local configuration..."
if [ ! -f ".env" ]; then
    cat > .env << 'EOF'
# ContextHelp development environment (subset of .env.example)
#
# Nothing loads this file automatically. Export it into your shell:
#   set -a; . ./.env; set +a
#
# Values shown are the built-in defaults; uncomment to change. Everything
# else is set in the config file.

# Storage backend: sqlite or postgres (config key: storage.type)
# CH_STORAGE_TYPE=sqlite

# dpkms serve listener ports. No bind-host variable: bind address follows
# server.access in the config file.
# CH_SERVER_PORT=8080
# CH_GRPC_PORT=9090
# CH_COOKIE_BRIDGE_PORT=9377

# AI providers (checked after the secrets store)
# OPENAI_API_KEY=
# ANTHROPIC_API_KEY=

# Postgres integration tests only (go test -tags=integration); values match
# docker-compose.dev.yml
# POSTGRES_HOST=localhost
# POSTGRES_PORT=5432
# POSTGRES_USER=ctxt
# POSTGRES_PASSWORD=ctxt
# POSTGRES_DB=ctxt
EOF
    success ".env file created"
else
    success ".env file already exists"
fi

# Step 7: Install git hooks
# Delegates to the canonical installer (make install-hooks), which points
# core.hooksPath at the tracked .githooks directory. That works in linked
# worktrees too, where .git is a file rather than a directory.
info "Step 7: Installing git hooks..."
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    warn "Not a git checkout; skipping hooks"
elif ! command_exists make; then
    warn "make not found; skipping hooks (run 'make install-hooks' later)"
else
    make install-hooks
    success "Git hooks installed ($(git rev-parse --git-path hooks))"
fi

# Step 8: Verify installation
info "Step 8: Verifying installation..."

echo ""
echo "=== Environment Check ==="
echo "Go version:      $(go version | awk '{print $3}')"
echo "OS:              $OS"
echo "Project root:    $PROJECT_ROOT"
echo "Database:        created and migrated on first run (migrations embedded in the binaries)"
echo ""

echo "=== Tools Check ==="
command_exists golangci-lint && echo "golangci-lint:   $(golangci-lint --version | head -n1)" || echo "golangci-lint:   NOT FOUND"
command_exists task && echo "task:            $(task --version)" || echo "task:            NOT FOUND"
command_exists govulncheck && echo "govulncheck:     installed" || echo "govulncheck:     NOT FOUND"
echo ""

# Step 9: Next steps
success "Development environment setup complete!"
echo ""
echo "=== Next Steps ==="
echo "1. Review .env, then export it: set -a; . ./.env; set +a (nothing loads it automatically)"
echo "2. Run 'task --list' to see available commands"
echo "3. Run 'task test' to verify everything works"
echo "4. Run 'task build' to build binaries"
echo "5. Check docs/development.md for detailed workflows"
echo ""
echo "=== Optional Services ==="
echo "To start Postgres:"
echo "  docker-compose -f docker-compose.dev.yml up -d"
echo ""
echo "Happy coding!"
