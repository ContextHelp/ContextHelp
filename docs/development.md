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
- [Adding a Pipeline Step](#adding-a-pipeline-step)
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

None are needed for local development. Nothing loads a `.env` file
automatically; to keep overrides in one, export it into your shell:

```bash
cp .env.example .env
set -a; . ./.env; set +a
```

Variables useful during development:
```bash
CH_STORAGE_TYPE=sqlite   # storage.type
DPKMS_WORKERS=2          # server.workers
CH_SERVER_PORT=8080      # server.port
```

Logging has no env var: pass `--verbose` to `ctxt` or `dpkms` for debug
output. The database location is the `storage.path` config key. Full
list: [environment-variables/README.md](environment-variables/README.md).

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

The schema lives in migrations embedded in the binaries. There is no
separate migrate tool, migration directory or Taskfile target: `dpkms` and
`ctxt` apply every pending migration whenever they open the database
(`Driver.Init` -> `Migrate`). Migrations are forward-only; there are no
down steps.

| Backend | Where | Ledger |
|---------|-------|--------|
| SQLite (default) | `internal/storage/sqlite/migrations/NNN_name.sql`, each `//go:embed`ed and listed in the `migrations` slice in `internal/storage/sqlite/migrations.go` | `schema_version` table |
| Postgres | the `pgMigrations` slice in `internal/storage/postgres/migrations.go` | versioned chain |

### Adding a migration

1. Add the next-numbered `internal/storage/sqlite/migrations/NNN_name.sql`.
2. Embed it in `migrations.go` (`//go:embed migrations/NNN_name.sql`) and
   append `{Version: NNN, SQL: migrationNNN}` to `migrations`. Use the
   entry's `fn` instead when SQL alone cannot do it (for example an
   idempotent `ALTER TABLE` on upgraded databases).
3. Mirror the change as the next `pgMigrations` entry.
4. Cover it with a test next to the existing ones
   (`internal/storage/sqlite/migrations_test.go`,
   `internal/storage/postgres/migrations_unit_test.go`).

### Starting from an empty database

Point the binary at a throwaway data dir; the database
(`<data dir>/db.sqlite`) is created and migrated on first open:

```bash
CTXT_DATA_DIR="$(mktemp -d)" ./bin/dpkms serve
```

To reset your real database instead, stop dpkms (`dpkms shutdown`), back
it up (`dpkms backup`), then move the file at `storage.path` aside.

---

## Adding a Pipeline Step

There is no plugin system ([ADR-078](decisions/ADR-078-no-plugin-system.md)).
A built-in step implements `pipeline.PipelineStep` (`internal/pipeline/pipeline.go`):

```go
type PipelineStep interface {
    Name() string
    Contract() StepContract
    Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
}
```

1. Add the step under `internal/pipeline/steps/`, embedding
   `pipeline.NewBaseContract(pipeline.StepContract{Requires: ..., Produces: ...})`.
2. Add its constructor to the step tables in
   `internal/pipeline/builtins/builtins.go` so pipeline definitions can name it.
3. Reference it from a pipeline definition, or from
   `pipelines.overrides.<pipeline>.insert_steps`.

A step that ships outside ctxt is an external step: a directory with a
`SKILL.md` and an executable that reads the knowledge object on stdin and
writes `{"object": ...}` (the fields it changed) on stdout, installed with
`dpkms pipeline step install` (ADR-058; example in `examples/steps/pii-mask`).

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

ctxt has no published release yet. The release path is a `v*.*.*` tag:
pushing one runs `.github/workflows/release.yml`, which runs all of
`.github/workflows/ci.yml` on the tag, then a blocking Trivy scan, then
GoReleaser per `.goreleaser.yaml` (see [ci-cd.md](ci-cd.md)).
Release notes for operators live in [release-notes/](release-notes/); the
`Operator-Impact:` commit trailer feeds them (see
[release-notes/template.md](release-notes/template.md)).

`task release:build` packs `bin/` into `dist/contexthelp-<tag>.tar.gz` for
a local check; it does not publish anything.

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
