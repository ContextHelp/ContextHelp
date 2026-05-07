# Development Guide

Comprehensive guide for developing **dPKMS + `ctxt`**.

---

## Table of Contents

- [Development Environment](#development-environment)
- [Architecture Overview](#architecture-overview)
- [Development Workflow](#development-workflow)
- [Testing Strategy](#testing-strategy)
- [Code Quality](#code-quality)
- [Database & Migrations](#database--migrations)
- [Plugin Development](#plugin-development)
- [Debugging](#debugging)
- [Performance Optimization](#performance-optimization)
- [Release Process](#release-process)

---

## Development Environment

### System Requirements

**Minimum:**
- Go 1.23+
- 4GB RAM
- 10GB disk space

**Recommended:**
- Go 1.23+
- 8GB+ RAM
- 20GB+ disk space
- macOS, Linux, or Windows with WSL2

### IDE Setup

#### VS Code

Recommended extensions:
```json
{
  "recommendations": [
    "golang.go",
    "github.copilot",
    "eamodio.gitlens",
    "streetsidesoftware.code-spell-checker",
    "tamasfe.even-better-toml"
  ]
}
```

Settings (`.vscode/settings.json`):
```json
{
  "go.toolsManagement.autoUpdate": true,
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "workspace",
  "editor.formatOnSave": true,
  "[go]": {
    "editor.defaultFormatter": "golang.go"
  }
}
```

#### GoLand / IntelliJ

1. Enable **Go modules** support
2. Configure **golangci-lint** as external linter
3. Set up **File Watchers** for auto-formatting

### Environment Variables

Create `.env` from `.env.example`:

```bash
cp .env.example .env
```

Key variables for development:
```bash
ENV=development
LOG_LEVEL=debug
STORAGE_BACKEND=sqlite
SQLITE_PATH=./data/sqlite/contexthelp_dev.db
WORKER_COUNT=2
API_PORT=8080
```

---

## Architecture Overview

### Two-Package Design

```
┌─────────────────────────────────────┐
│           ctxt (Brain)              │
│  • Capture, Enrichment, Surfacing  │
│  • CLI, TUI, Composition            │
│  • Focus Profiles, Hints            │
└──────────────┬──────────────────────┘
               │ Uses
               ▼
┌─────────────────────────────────────┐
│          dPKMS (Substrate)          │
│  • Storage, Jobs, Pipeline Runtime  │
│  • Query Engine, Graph, Registries  │
│  • Security, Privacy, Federation    │
└─────────────────────────────────────┘
```

**Key Principle:**
- **dPKMS runs work correctly** — Mechanics
- **`ctxt` decides which work is valuable** — Meaning

### Package Structure

```
cmd/
├── dpkms/         # dPKMS server entry point
│   ├── cmd/       # Cobra commands (serve, housekeeping, etc.)
│   └── main.go
└── ctxt/          # ctxt CLI entry point
    ├── cmd/       # Cobra commands (analyze, find, make, etc.)
    └── main.go

internal/
├── config/        # Shared configuration
├── dpkms/         # dPKMS implementation (private)
│   ├── storage/   # Storage backends
│   ├── jobs/      # Job queue
│   ├── graph/     # Knowledge graph
│   ├── query/     # Query engine
│   └── ...
└── ctxt/          # ctxt implementation (private)
    ├── pipelines/ # Enrichment recipes
    ├── profiles/  # Focus profiles
    ├── capture/   # Capture interfaces
    └── ...

pkg/               # Public libraries (if any)
test/
├── integration/   # Integration tests
├── e2e/           # End-to-end tests
└── fixtures/      # Test data
```

---

## Development Workflow

### 1. Branch Strategy

```
main          # Production-ready code
  ├─ develop  # Integration branch
       ├─ feature/add-tui     # Feature branches
       ├─ fix/job-retry-bug   # Bug fix branches
       └─ docs/update-readme  # Documentation branches
```

**Branch naming:**
- `feature/` — New features
- `fix/` — Bug fixes
- `docs/` — Documentation
- `refactor/` — Code refactoring
- `test/` — Test additions

### 2. Development Cycle

```bash
# 1. Create feature branch
git checkout -b feature/my-feature develop

# 2. Make changes and test
task test
task lint

# 3. Commit with conventional commits
git commit -m "feat(pipelines): add video transcription pipeline"

# 4. Push and create PR
git push origin feature/my-feature
```

### 3. Conventional Commits

Format: `<type>(<scope>): <description>`

**Types:**
- `feat:` — New feature
- `fix:` — Bug fix
- `docs:` — Documentation
- `refactor:` — Code refactoring
- `test:` — Test additions
- `chore:` — Maintenance tasks
- `perf:` — Performance improvements

**Examples:**
```
feat(storage): add PostgreSQL backend support
fix(jobs): prevent job retry loop on permanent errors
docs(api): update REST API documentation
refactor(graph): optimize entity resolution algorithm
test(pipelines): add unit tests for text.long pipeline
```

### 4. Code Review Process

1. **Self-review first** — Review your own diff
2. **Run checks locally** — `task ci`
3. **Write good PR description** — What, why, how
4. **Respond to feedback** — Address comments promptly
5. **Update as needed** — Keep commits clean

**PR Template:**
```markdown
## Summary
Brief description of changes

## Changes
- List of key changes
- Bullet points preferred

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests pass
- [ ] Manual testing completed

## Breaking Changes
Any breaking changes and migration path

## Related Issues
Fixes #123
```

---

## Testing Strategy

### Test Organization

```
test/
├── unit/          # Fast, isolated tests
├── integration/   # Tests with real storage/jobs
├── e2e/           # Full system tests
└── fixtures/      # Shared test data
```

### Operator-contract tests (eva)

Operator-facing JSON shapes — `ctxt upgrade status` output, the `/healthz`
envelope, the `staleness_warning` field on search responses, `ctxt
embeddings list` output — are locked by **Tier-1 deterministic contracts**
authored against [`hop.top/eva`](https://github.com/hop-top/eva). The
contract files live at:

```
contracts/
├── upgrade-status.eva.yaml      # ctxt upgrade status JSON shape (ADR-070 §5)
├── healthz.eva.yaml             # /healthz envelope (ADR-070 §5)
├── search-staleness.eva.yaml    # staleness_warning field on search responses
└── embeddings-list.eva.yaml     # ctxt embeddings list output (ADR-071)
```

Each contract is paired with one or more JSON fixtures under
`test/integration/testdata/eva-fixtures/`. The naming convention is
`<contract-base>*.json` so a single contract can lock multiple states
(e.g. `upgrade-status-idle.json` + `upgrade-status-in-progress.json`).

**Rationale** — see ADR-070 §6 and the "Why xrr + eva + ben specifically"
rationale section. Short version: the same JSON is consumed by humans, by
`osascript` for notifications, and by web dashboards. eva contracts catch
shape regressions before they reach an operator's terminal — a missing
key, a renamed field, an enum value that flipped from `in_progress` to
`inProgress` all fail the build.

**Running locally**:

```bash
# Run all contracts against their fixtures
make eva

# Or run the script directly (useful when iterating)
./scripts/run-eva-contracts.sh

# Run as part of the pre-merge gate (test + eva)
make check
```

Requires the `eva` CLI on PATH. Install via `pipx install eva` (once a
version with `eva run --contract` ships) or from source per the pinned
ref in `.github/workflows/eva-contracts.yml`. Override the binary with
`EVA_BIN=/path/to/eva make eva` when debugging against a local build.

**Adding a new contract**:

1. Author `contracts/<name>.eva.yaml`. Use `json_schema_valid` for Tier-1
   deterministic shape locks; use additional evaluators (regex, contains,
   …) when the shape encodes free-text fields with a known invariant.
2. Record one or more fixtures at
   `test/integration/testdata/eva-fixtures/<name>*.json`. Capture against
   the live CLI when the surface exists; synthesize from the spec when it
   doesn't (and tighten on land).
3. The CI workflow (`.github/workflows/eva-contracts.yml`) and `make eva`
   pick the new contract up automatically — no further wiring required.

**Why eva specifically, not a hand-rolled JSON-Schema test?** eva ships a
single-source-of-truth dispatcher that's shared with the eva gateway, so a
contract that passes locally is the same contract a future eva-served API
gateway would enforce at request time. The marginal cost is one YAML file
per shape; the marginal reward is shape-stability across CLI consumers,
docs, and gateway middleware in lockstep.



### Running Tests

```bash
# All tests
task test

# Unit tests only (fast)
task test:unit

# Integration tests
task test:integration

# E2E tests
task test:e2e

# With coverage
task test:coverage

# Watch mode (auto-rerun on changes)
task test:watch

# Specific package
go test -v ./internal/dpkms/storage/...

# Specific test
go test -v -run TestJobRetry ./internal/dpkms/jobs/...
```

### Writing Tests

#### Unit Test Example

```go
// internal/dpkms/storage/sqlite_test.go
package storage

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSQLiteStore_CreateObject(t *testing.T) {
    // Arrange
    store, cleanup := setupTestStore(t)
    defer cleanup()
    
    obj := &Object{
        Type:    "text",
        Content: "test content",
    }
    
    // Act
    err := store.Create(obj)
    
    // Assert
    require.NoError(t, err)
    assert.NotEmpty(t, obj.ID)
    
    // Verify persistence
    retrieved, err := store.Get(obj.ID)
    require.NoError(t, err)
    assert.Equal(t, obj.Content, retrieved.Content)
}
```

#### Integration Test Example

```go
// test/integration/pipeline_test.go
// +build integration

package integration

import (
    "testing"
    "context"
)

func TestTextPipeline_EndToEnd(t *testing.T) {
    // Setup test database
    db := setupTestDB(t)
    defer db.Close()
    
    // Create pipeline
    pipeline := pipelines.NewTextLong(db)
    
    // Execute
    ctx := context.Background()
    result, err := pipeline.Execute(ctx, "Long text input...")
    
    // Verify
    require.NoError(t, err)
    assert.NotEmpty(t, result.Summary)
    assert.NotEmpty(t, result.Tags)
}
```

### Test Helpers

Create reusable test utilities:

```go
// test/helpers/storage.go
package helpers

func SetupTestStore(t *testing.T) (*storage.Store, func()) {
    t.Helper()
    
    db := setupTempDB(t)
    store := storage.New(db)
    
    cleanup := func() {
        db.Close()
        os.Remove(db.Path())
    }
    
    return store, cleanup
}
```

---

## Code Quality

### Linting

```bash
# Run all linters
task lint

# Auto-fix issues
task lint:fix

# Specific linters
golangci-lint run --disable-all --enable=gofmt
golangci-lint run --disable-all --enable=govet
```

### Formatting

```bash
# Format all Go code
task fmt

# Check formatting without changing
gofmt -l .

# Format with imports
goimports -w -local github.com/ideacrafterslabs/ctxt .
```

### Pre-commit Hooks

Install pre-commit hooks:

```bash
# Create .git/hooks/pre-commit
cat > .git/hooks/pre-commit << 'HOOK'
#!/bin/bash
set -e

echo "Running pre-commit checks..."

# Format
task fmt

# Lint
task lint

# Test
task test:unit

echo "✓ All checks passed"
HOOK

chmod +x .git/hooks/pre-commit
```

---

## Database & Migrations

### Creating Migrations

```bash
# Create new migration
task db:migrate:create -- add_embeddings_table

# This creates:
# data/migrations/000001_add_embeddings_table.up.sql
# data/migrations/000001_add_embeddings_table.down.sql
```

### Writing Migrations

**Up migration** (`000001_add_embeddings_table.up.sql`):
```sql
CREATE TABLE IF NOT EXISTS embeddings (
    id TEXT PRIMARY KEY,
    object_id TEXT NOT NULL,
    model TEXT NOT NULL,
    vector BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (object_id) REFERENCES objects(id) ON DELETE CASCADE
);

CREATE INDEX idx_embeddings_object_id ON embeddings(object_id);
```

**Down migration** (`000001_add_embeddings_table.down.sql`):
```sql
DROP INDEX IF EXISTS idx_embeddings_object_id;
DROP TABLE IF EXISTS embeddings;
```

### Running Migrations

```bash
# Apply all pending migrations
task db:migrate:up

# Rollback last migration
task db:migrate:down

# Reset database (drops all, re-runs migrations)
task db:reset
```

---

## Plugin Development

See [docs/plugins/plugins-api.md](plugins/plugins-api.md) for complete plugin development guide.

### Quick Start

1. **Define plugin interface:**
```go
type PipelinePlugin interface {
    Name() string
    Execute(ctx context.Context, input any) (any, error)
}
```

2. **Implement plugin:**
```go
type MyPlugin struct{}

func (p *MyPlugin) Name() string {
    return "my-plugin"
}

func (p *MyPlugin) Execute(ctx context.Context, input any) (any, error) {
    // Plugin logic
    return result, nil
}
```

3. **Register plugin:**
```go
func init() {
    plugins.Register(&MyPlugin{})
}
```

---

## Debugging

### Logging

```bash
# Set debug logging
export LOG_LEVEL=debug

# Debug specific components
export DEBUG_SQL=true
export DEBUG_JOBS=true
export DEBUG_PIPELINES=true
```

### Profiling

```bash
# Enable pprof
export ENABLE_PROFILING=true
export PPROF_PORT=6060

# Start server
./bin/dpkms serve

# Access profiler
go tool pprof http://localhost:6060/debug/pprof/profile
```

### Delve Debugger

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug binary
dlv exec ./bin/dpkms -- serve

# Debug tests
dlv test ./internal/dpkms/storage
```

---

## Performance Optimization

### Benchmarking

```bash
# Run benchmarks
task test:bench

# Specific benchmark
go test -bench=BenchmarkStorageCreate -benchmem ./internal/dpkms/storage/

# With profiling
go test -bench=. -cpuprofile=cpu.prof -memprofile=mem.prof
go tool pprof cpu.prof
```

### Performance Tips

1. **Use connection pooling** for database
2. **Batch operations** where possible
3. **Cache frequently accessed data**
4. **Profile before optimizing**
5. **Avoid premature optimization**

---

## Release Process

See [docs/release-process.md](release-process.md) for complete release guide.

### Quick Release

```bash
# 1. Update version
git tag -a v0.2.0 -m "Release v0.2.0"

# 2. Build release binaries
task release:build

# 3. Push tag
git push origin v0.2.0

# 4. GitHub Actions will create release
```

---

## Development Best Practices

### Code Style

1. **Follow Go conventions** — `gofmt`, idiomatic Go
2. **Keep functions small** — Single responsibility
3. **Use meaningful names** — `getUserByID` not `get`
4. **Document public APIs** — Godoc comments
5. **Handle errors explicitly** — No silent failures

### Error Handling

```go
// Good
result, err := doSomething()
if err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}

// Bad
result, _ := doSomething()
```

### Context Usage

```go
// Good
func ProcessItem(ctx context.Context, id string) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        // Continue processing
    }
}

// Pass context through call chain
func caller(ctx context.Context) {
    callee(ctx)
}
```

---

## Getting Help

**Documentation:**
- [Architecture](architecture.md)
- [Domain Index](domains.md)
- [API Reference](api/)

**Community:**
- GitHub Issues
- Discussions
- Slack (coming soon)

---

For quick setup, see [developer-quickstart.md](developer-quickstart.md).
