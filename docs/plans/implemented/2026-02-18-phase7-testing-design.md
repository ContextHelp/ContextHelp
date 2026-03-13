# Phase 7: Testing Design

**Author:** $USER
**Date:** 2026-02-18
**Status:** Proposed

## Goal

Raise test coverage from 79.4% to 90%+ by filling unit test gaps,
expanding E2E scenarios, adding binary smoke tests, and enabling
race detection across all test tiers.

## Current State

| Package | Coverage | Key Gaps |
|---------|----------|----------|
| `cmd/ctxt/cmd` | 92.4% | `Execute()`, `runConfigEdit` |
| `cmd/dpkms/cmd` | 50.3% | `runServe`, `runPrune` |
| `internal/config` | 85.2% | `EnsureConfigDir` error |
| `internal/jobs` | 87.7% | Concurrency paths |
| `internal/pipeline` | 95.2% | `Name()` getters |
| `internal/pipeline/steps` | 90.0% | `Name()` getters |
| `internal/search` | 79.2% | `compileSimilar`, `sqlOperator` |
| `internal/server/http` | 78.5% | `RetryJob`, `Health`, `Recoverer` |
| `internal/service` | 84.4% | `ListEntities` |
| `internal/storage/sqlite` | 72.5% | `ListBySQL`, `Delete`, `marshalJSON` |
| `internal/storageutil` | 85.7% | error branch |
| `test/integration` | n/a | 1 E2E scenario |

**Total: 79.4% (54 test files, 5,578 LOC tests)**

## Design

### Three Test Tiers

```
Tier       Location              Build Tag     Runner
--------   --------------------  -----------   -------------------------
Unit       *_test.go (colocated) (none)        go test ./... -race
E2E        test/integration/     integration   go test -tags=integration
Smoke      test/smoke/           smoke         go test -tags=smoke
```

### Dependencies

- Add `stretchr/testify` for `require`/`assert` in new tests
- Existing tests keep stdlib style (no retroactive rewrite)
- New tests use `testify`

### Makefile Targets

```makefile
test-unit:        go test -race -count=1 ./cmd/... ./internal/...
test-integration: go test -race -tags=integration ./test/integration/...
test-smoke:       go test -tags=smoke ./test/smoke/...
test-all:         test-unit test-integration test-smoke
test-cover:       go test -race -coverprofile=coverage.out ./...
```

All targets include `-race` flag.

### Unit Test Gap Plan

#### `cmd/dpkms/cmd` (50.3% -> 85%+)

**`runServe` (0%):**
- Test server startup with mock storage driver
- Test graceful shutdown via context cancellation
- Test flag parsing (port, grpc-port, workers, public)
- Test error on invalid port

**`runPrune` (0%):**
- Test prune with --before flag
- Test prune dry-run output
- Test prune with no matching data

#### `internal/storage/sqlite` (72.5% -> 85%+)

**`ListBySQL` (0%):**
- Test with valid WHERE clause
- Test with empty result set
- Test with SQL injection attempt (parameterized)
- Test with complex JSON field queries

**`edges.Delete` (0%):**
- Test delete existing edge
- Test delete non-existent edge (no error)
- Test delete with invalid ID

**`jobs.marshalJSON` (0%):**
- Test marshaling nil payload
- Test marshaling complex nested payload
- Test round-trip marshal/unmarshal

#### `internal/search` (79.2% -> 90%+)

**`compileSimilar` (0%):**
- Test `=~` operator compilation to LIKE
- Test with special characters in pattern
- Test case sensitivity behavior

**`sqlOperator` branches (40%):**
- Test all operator mappings: `=`, `!=`, `<`, `>`, `<=`, `>=`
- Test `in` operator with list values
- Test `exists` operator

**Parser error paths:**
- Unterminated string literals
- Missing operator
- Empty expression
- Nested parentheses edge cases

#### `internal/server/http` (78.5% -> 90%+)

**`RetryJob` branches (60%):**
- Test retry of completed job (should fail)
- Test retry of non-existent job
- Test retry of already-pending job

**`Health` error path (60%):**
- Test health endpoint when storage is unavailable
- Test health response format

**`Recoverer` panic path (80%):**
- Test middleware catches handler panic
- Test 500 response on panic
- Test panic doesn't crash server

#### `internal/config` (85.2% -> 90%+)

**`EnsureConfigDir` error (80%):**
- Test with read-only parent directory
- Test with missing parent directory

#### `internal/service` (84.4% -> 90%+)

**`ListEntities` (0%):**
- Test list with no entities
- Test list with multiple entities
- Test list respects limit/offset

### Concurrency Tests (`internal/jobs/`)

New file: `internal/jobs/race_test.go`

1. **Multiple workers dequeuing** — 4 workers, 10 jobs, each job
   processed exactly once
2. **Concurrent enqueue + dequeue** — producer and consumer goroutines,
   no lost jobs
3. **Shutdown with in-flight** — cancel context mid-processing, verify
   graceful drain
4. **Stale recovery under load** — recover stale jobs while new jobs
   are being processed

### E2E Test Expansion (`test/integration/`)

New scenarios in `test/integration/e2e_test.go`:

1. **Full ingest + search** — analyze text, wait for completion,
   search via RSQL `type==text`, verify object in results
2. **Entity lifecycle** — create entity, ingest content referencing it,
   verify entity appears in search
3. **Edge creation + query** — create 2 objects, create edge between
   them, query edges from/to
4. **Job error + retry** — submit job that triggers pipeline error,
   verify failed status, retry, verify eventual success
5. **Config override via env** — set DPKMS_WORKERS=1 via env, verify
   worker count in /health or logs
6. **Concurrent ingestion** — 10 parallel POST /api/v1/analyze, all
   complete successfully, no data corruption

### Binary Smoke Tests (`test/smoke/`)

Build tag: `smoke`. Requires `make build` first.

New file: `test/smoke/binary_test.go`

1. `ctxt version` — exit 0, output contains version string
2. `ctxt --help` — exit 0, output contains all subcommands
3. `ctxt completion bash` — exit 0, valid bash script output
4. `ctxt completion zsh` — exit 0, valid zsh script output
5. `dpkms version` — exit 0
6. `dpkms serve` — starts on random port, /health returns 200,
   SIGTERM causes clean exit

New file: `test/smoke/roundtrip_test.go`

7. **Round-trip:** start dpkms serve, ctxt analyze sends content,
   poll job status, ctxt find returns result

### Test Infrastructure

**`test/testutil/binary.go`:**
- `BuildBinaries(t)` — ensure binaries exist in `bin/`
- `RunBinary(t, name, args...)` — exec + capture stdout/stderr
- `StartServer(t, port)` — start dpkms serve, return cleanup func
- `WaitForHealth(t, url)` — poll until /health 200

**`internal/storageutil/` extensions:**
- `SeedObjects(t, driver, n)` — insert n sample objects
- `SeedEntities(t, driver, slugs...)` — insert named entities
- `SeedJobs(t, driver, statuses...)` — insert jobs with given statuses

### Success Criteria

- Overall coverage >= 90%
- All tests pass with `-race`
- No package below 80% (except `cmd/*/main.go` entry points)
- E2E covers full ingest→search round-trip
- Smoke tests validate compiled binaries
- CI-ready: `make test-all` runs all three tiers
