# Development Infrastructure Overview

This document provides an overview of the development infrastructure for ContextHelp.

---

## Documentation Created

### 1. Development Guide (`development.md`)

**Comprehensive 400+ line guide covering:**
- Local Go development setup
- Project structure and organization
- Configuration management
- Running and testing the system
- Development workflows
- Docker Compose for optional services
- Troubleshooting guide

**Target Audience:** All developers (new and experienced)

**Key Sections:**
- Quick start for experienced developers
- Step-by-step manual setup
- Database migrations
- Testing strategies
- Code quality standards

---

### 2. Developer Quick Start (`developer-quickstart.md`)

**5-minute onboarding guide:**
- Prerequisites check
- Automated setup with `dev-setup.sh`
- First development session
- Common commands reference
- Quick troubleshooting

**Target Audience:** New developers who want to get started immediately

**Key Features:**
- Minimal time to first contribution
- Clear, actionable steps
- Quick reference cards
- Essential reading list

---

### 3. CI/CD Pipeline Documentation (`ci-cd.md`)

**Comprehensive CI/CD guide covering:**
- Pipeline stages and workflow
- GitHub Actions configuration
- Running CI locally
- Interpreting failures
- Adding new tests
- Release process

**Target Audience:** Contributors and maintainers

**Key Sections:**
- Stage-by-stage pipeline breakdown
- Security scanning and quality gates
- Cross-platform testing
- Release automation
- Performance benchmarks

---

## Configuration Files Created

### 1. Docker Compose (`docker-compose.dev.yml`)

**Purpose:** Optional development services

**Services Included:**
- PostgreSQL 16 (alternative to SQLite)
- Redis 7 (distributed job queue)
- Qdrant 1.7 (vector search)

**Features:**
- Health checks for all services
- Volume persistence
- Proper networking
- Development-optimized configuration

---

### 2. Taskfile (`Taskfile.yml`)

**Purpose:** Unified task runner for all development operations

**Task Categories:**
- **Development:** dev, run, build
- **Testing:** test, test:unit, test:integration, test:e2e, test:coverage
- **Quality:** lint, fmt, vet, security
- **CI:** ci, ci:lint, ci:test, ci:security
- **Database:** db:migrate:up, db:migrate:down, db:reset, db:seed
- **Docker:** services:up, services:down, services:logs
- **Dependencies:** deps, deps:update
- **Release:** release:prepare, release:build

**Benefits:**
- Consistent commands across team
- Matches CI pipeline exactly
- Self-documenting with descriptions
- Cross-platform compatibility

---

### 3. golangci-lint Configuration (`.golangci.yml`)

**Purpose:** Comprehensive Go linting configuration

**Enabled Linters:**
- **Default:** errcheck, gosimple, govet, ineffassign, staticcheck, typecheck, unused
- **Security:** gosec (security-focused)
- **Style:** revive, gocritic
- **Quality:** gocyclo, dupl, goconst
- **Errors:** errorlint, nilerr

**Custom Rules:**
- Test files get relaxed checks
- Generated code excluded
- Main functions allowed complexity
- Import ordering enforced

**Coverage Requirements:**
- dPKMS critical: 95%
- dPKMS general: 85%
- ctxt critical: 90%
- ctxt general: 80%

---

### 4. Environment Configuration (`.env.example`)

**Purpose:** Template for local development configuration

**Sections:**
- Environment settings (dev/prod)
- Storage backends (SQLite, Postgres)
- Optional services (Redis, Qdrant)
- AI providers (OpenAI, Anthropic, Ollama)
- Logging configuration
- API server settings
- Jobs and workers
- Security settings
- Development flags

**Features:**
- Comprehensive comments
- Secure defaults
- Optional sections clearly marked
- Ready to copy to `.env`

---

### 5. YAML Configuration (`config/config.example.yaml`)

**Purpose:** Structured configuration for both dPKMS and ctxt

**dPKMS Configuration:**
- Storage backend settings
- Job queue configuration
- Security and encryption
- Registry subscriptions
- Graph settings
- Query configuration
- API (REST and gRPC)

**ctxt Configuration:**
- Pipeline settings
- AI provider configuration
- Profile management
- Search settings
- Vector search (optional)
- Surfacing configuration
- Composition templates
- CLI preferences

**Additional:**
- Logging configuration
- Monitoring and observability
- Development settings

---

### 6. GitHub Actions Workflows (`.github/workflows/ci.yml`)

**Purpose:** Automated CI/CD pipeline

**Jobs:**

**Lint Job:**
- golangci-lint with timeout
- Format checking (gofmt)
- go vet analysis

**Security Job:**
- govulncheck for vulnerabilities
- Trivy filesystem scanning
- SARIF upload to GitHub Security

**Test Job:**
- Matrix: Ubuntu, macOS, Windows
- Go versions: 1.23
- Race detector enabled
- Coverage reporting to Codecov

**Integration Job:**
- PostgreSQL service
- Redis service
- Integration test suite
- Environment-based configuration

**Build Job:**
- Build both binaries
- Verify executables
- Upload artifacts (7-day retention)

**Security Features:**
- No untrusted input in commands
- Environment variables for safety
- Proper action pinning
- SARIF security scanning

---

## Development Scripts

### Setup Script (`dev-setup.sh`)

**Purpose:** Automated development environment setup

**What it does:**
1. Checks Go installation (1.23+)
2. Creates directory structure
3. Initializes Go modules
4. Installs development tools
5. Sets up SQLite database
6. Creates initial migrations
7. Generates `.env` file
8. Installs Git pre-commit hooks
9. Verifies installation

**Tools Installed:**
- golangci-lint
- Task (task runner)
- golang-migrate
- govulncheck

**Features:**
- OS detection (macOS, Linux, Windows)
- Color-coded output
- Error handling
- Idempotent (safe to re-run)

---

## Git Hooks

### Pre-commit Hook

**Location:** `.git/hooks/pre-commit`

**Actions:**
1. Run golangci-lint
2. Run unit tests (`go test -short`)
3. Report results

**Benefits:**
- Catches issues before push
- Prevents broken commits
- Enforces code quality
- Fast feedback loop

**Installation:** Automatic via `dev-setup.sh`

---

## Key Features of the Infrastructure

### 1. Local-First Development

- **SQLite default:** No external services required
- **Zero-config start:** Run `./dev-setup.sh` and you're ready
- **Offline capable:** All tests work without network
- **Fast iteration:** Hot reload in development mode

### 2. Optional Scaling

- **Postgres:** When multi-user testing needed
- **Redis:** For distributed job queue testing
- **Qdrant:** For vector search experimentation
- **Easy toggle:** Environment variable switches

### 3. Comprehensive Testing

- **Unit tests:** Fast, isolated
- **Integration tests:** Subsystem interactions
- **E2E tests:** Complete workflows
- **Coverage tracking:** Automated reporting
- **Race detection:** Concurrent code verification

### 4. Code Quality Automation

- **Pre-commit hooks:** Lint + test before commit
- **CI pipeline:** Automated on every push
- **Security scanning:** govulncheck + Trivy
- **Cross-platform:** Linux, macOS, Windows
- **Coverage gates:** Minimum thresholds enforced

### 5. Developer Experience

- **Taskfile:** Unified commands (`task <command>`)
- **Documentation:** Step-by-step guides
- **Troubleshooting:** Common issues covered
- **Quick start:** 5-minute onboarding
- **Examples:** Working code samples

---

## Workflow Integration

### Daily Development Flow

```bash
# Morning: Update and verify
git pull origin main
task deps
task test

# Work: Edit code
# (Edit files)
task test:watch  # Auto-run tests

# Commit: Quality checks run automatically
git add .
git commit -m "feat: description"

# Push: CI runs automatically
git push origin feature-branch
```

### Testing Flow

```bash
# Quick check
task test:unit

# Full verification
task test

# Coverage analysis
task test:coverage

# Integration testing
task services:up
task test:integration
task services:down
```

### Quality Assurance Flow

```bash
# Before commit
task lint
task fmt
task security

# Match CI exactly
task ci

# Fix issues
task lint:fix
```

---

## Architecture Alignment

### Matches Project Structure

- **Two-package system:** dPKMS + ctxt clearly separated
- **Internal/pkg split:** Private vs. public APIs
- **Test organization:** Unit, integration, E2E
- **Documentation hierarchy:** Matches code structure

### Follows Design Principles

**dPKMS (Substrate):**
- Deterministic testing
- Crash-safe verification
- Offline-first testing
- Security validation

**ctxt (Brain):**
- Pipeline testing
- AI provider mocking
- Profile testing
- Composition validation

---

## Maintenance

### Updating Dependencies

```bash
# Check for updates
task deps:update

# Verify tests still pass
task test

# Update go.mod
go mod tidy
```

### Updating Tools

```bash
# Reinstall development tools
task install:tools

# Verify installation
task verify
```

### Database Migrations

```bash
# Create new migration
task db:migrate:create -- add_new_table

# Edit migration files in data/migrations/

# Apply migration
task db:migrate:up

# If needed, rollback
task db:migrate:down
```

---

## Security Considerations

### Secrets Management

- `.env` files not committed
- `.env.example` provides template
- API keys via environment variables
- CI secrets in GitHub Settings

### Vulnerability Scanning

- **govulncheck:** Go vulnerability database
- **Trivy:** Filesystem scanning
- **gosec:** Security-focused linting
- **Dependabot:** Automated dependency updates (GitHub)

### Pre-commit Security

- Prevents committing secrets (via hooks)
- Runs security linters
- Validates code patterns
- Fast feedback loop

---

## Performance Considerations

### CI Optimization

- **Caching:** Go modules cached
- **Parallelization:** Matrix builds
- **Selective runs:** Integration tests on PR only
- **Fast feedback:** Lint before expensive tests

### Local Development

- **Incremental builds:** Go build cache
- **Test filtering:** Short mode for quick checks
- **Watch mode:** Auto-run on changes
- **SQLite:** Fast local database

---

## Documentation Standards

All documentation follows:

1. **Markdown format:** Clean, readable
2. **Clear structure:** Table of contents, sections
3. **Code examples:** Working, tested examples
4. **Troubleshooting:** Common issues addressed
5. **Cross-references:** Links to related docs
6. **Update frequency:** Reviewed with major changes

---

## Future Enhancements

Potential improvements:

1. **Air configuration:** For advanced hot reload
2. **Docker development:** Full containerized environment
3. **GitHub Actions enhancements:** Matrix testing, nightly builds
4. **Integration tests:** More service combinations
5. **Performance benchmarks:** Automated tracking
6. **E2E test suite:** Expanded scenarios

---

## Summary

The development infrastructure provides:

- **Fast onboarding:** 5-minute setup to first contribution
- **Comprehensive testing:** Unit, integration, E2E with coverage
- **Quality automation:** Linting, formatting, security scanning
- **Flexible deployment:** SQLite or Postgres, local or Docker
- **Excellent documentation:** Step-by-step guides with troubleshooting
- **CI/CD pipeline:** Automated testing and releases
- **Security-first:** Vulnerability scanning and safe practices

All aligned with ContextHelp's two-package architecture and local-first principles.

---

## Getting Started

For developers new to the project:

1. **Read:** [developer-quickstart.md](developer-quickstart.md)
2. **Setup:** Run `./dev-setup.sh`
3. **Explore:** Run `task --list`
4. **Contribute:** Follow [development.md](development.md)

For questions or issues, see [development.md](development.md#troubleshooting) or open a GitHub Issue.
