#!/usr/bin/env bash
set -euo pipefail

# dev-setup.sh - Development environment setup for ContextHelp
# This script sets up a local-first development environment with all necessary tools

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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
    error "Go is not installed. Please install Go 1.23+ from https://go.dev/dl/"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
REQUIRED_VERSION="1.23"

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
mkdir -p data/sqlite data/migrations
mkdir -p configs
mkdir -p test/integration test/e2e test/fixtures
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
        brew install golangci-lint || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
    else
        curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
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

# golang-migrate (database migrations)
if ! command_exists migrate; then
    info "Installing golang-migrate..."
    if [ "$OS" = "macos" ]; then
        brew install golang-migrate || go install -tags 'sqlite3' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
    else
        go install -tags 'sqlite3' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
    fi
    success "golang-migrate installed"
else
    success "migrate already installed"
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

# Step 6: Setup SQLite database
info "Step 6: Setting up local SQLite database..."
mkdir -p data/sqlite

# Create initial migration files if they don't exist
if [ ! -f "data/migrations/000001_initial_schema.up.sql" ]; then
    info "Creating initial migration files..."

    cat > data/migrations/000001_initial_schema.up.sql << 'EOF'
-- Initial schema for dPKMS storage
-- Phase 0: Shared Kernel

-- Jobs table (transactional outbox pattern)
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending', -- pending, running, completed, failed
    payload TEXT NOT NULL,
    error TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    started_at INTEGER,
    completed_at INTEGER
);

CREATE INDEX idx_jobs_status ON jobs(status, created_at);
CREATE INDEX idx_jobs_type ON jobs(type);

-- Knowledge objects table (simplified for Phase 0)
CREATE TABLE IF NOT EXISTS objects (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    source TEXT NOT NULL,
    content TEXT NOT NULL,
    metadata TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_objects_type ON objects(type);
CREATE INDEX idx_objects_created_at ON objects(created_at);

-- Configuration table
CREATE TABLE IF NOT EXISTS config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);
EOF

    cat > data/migrations/000001_initial_schema.down.sql << 'EOF'
-- Rollback initial schema
DROP TABLE IF EXISTS config;
DROP TABLE IF EXISTS objects;
DROP TABLE IF EXISTS jobs;
EOF

    success "Initial migration files created"
fi

# Run migrations
if command_exists migrate; then
    info "Running database migrations..."
    migrate -path ./data/migrations -database "sqlite3://./data/sqlite/contexthelp.db" up || warn "Migrations failed or already applied"
    success "Database initialized at data/sqlite/contexthelp.db"
else
    warn "Skipping migrations (migrate tool not found)"
fi

# Step 7: Create local configuration
info "Step 7: Creating local configuration..."
if [ ! -f ".env" ]; then
    cat > .env << 'EOF'
# ContextHelp Development Environment Configuration
# Copy this to .env and customize as needed

# Environment
ENV=development

# Storage
STORAGE_BACKEND=sqlite
SQLITE_PATH=./data/sqlite/contexthelp.db

# Optional: Postgres (when using Docker Compose)
# STORAGE_BACKEND=postgres
# POSTGRES_HOST=localhost
# POSTGRES_PORT=5432
# POSTGRES_USER=contexthelp
# POSTGRES_PASSWORD=dev_password
# POSTGRES_DB=contexthelp

# Redis (optional, for distributed jobs)
# REDIS_HOST=localhost
# REDIS_PORT=6379

# Qdrant (optional, for vector search)
# QDRANT_HOST=localhost
# QDRANT_PORT=6333

# AI Providers (add your keys)
# OPENAI_API_KEY=
# ANTHROPIC_API_KEY=

# Logging
LOG_LEVEL=debug
LOG_FORMAT=console

# Server
API_HOST=localhost
API_PORT=8080
GRPC_PORT=9090

# Development
DEV_MODE=true
ENABLE_PROFILING=false
EOF
    success ".env file created"
else
    success ".env file already exists"
fi

# Step 8: Setup pre-commit hooks
info "Step 8: Setting up Git pre-commit hooks..."
mkdir -p .git/hooks

cat > .git/hooks/pre-commit << 'EOF'
#!/usr/bin/env bash
set -e

# Run linter
echo "Running golangci-lint..."
golangci-lint run

# Run tests
echo "Running tests..."
go test -short ./...

echo "Pre-commit checks passed!"
EOF

chmod +x .git/hooks/pre-commit
success "Git pre-commit hook installed"

# Step 9: Verify installation
info "Step 9: Verifying installation..."

echo ""
echo "=== Environment Check ==="
echo "Go version:      $(go version | awk '{print $3}')"
echo "OS:              $OS"
echo "Project root:    $PROJECT_ROOT"
echo "Database:        ./data/sqlite/contexthelp.db"
echo ""

echo "=== Tools Check ==="
command_exists golangci-lint && echo "golangci-lint:   $(golangci-lint --version | head -n1)" || echo "golangci-lint:   NOT FOUND"
command_exists task && echo "task:            $(task --version)" || echo "task:            NOT FOUND"
command_exists migrate && echo "migrate:         $(migrate -version 2>&1 | head -n1)" || echo "migrate:         NOT FOUND"
command_exists govulncheck && echo "govulncheck:     installed" || echo "govulncheck:     NOT FOUND"
echo ""

# Step 10: Next steps
success "Development environment setup complete!"
echo ""
echo "=== Next Steps ==="
echo "1. Review and update .env with your API keys"
echo "2. Run 'task --list' to see available commands"
echo "3. Run 'task test' to verify everything works"
echo "4. Run 'task build' to build binaries"
echo "5. Check docs/development.md for detailed workflows"
echo ""
echo "=== Optional Services ==="
echo "To start Postgres, Redis, and Qdrant:"
echo "  docker-compose -f docker-compose.dev.yml up -d"
echo ""
echo "Happy coding!"
