# Phase 7: Testing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans
> to implement this plan task-by-task.

**Goal:** Raise test coverage from 79.4% to 90%+ with unit gap fills,
E2E expansion, binary smoke tests, and race detection.

**Architecture:** Three-tier testing (unit/E2E/smoke) using testify
for new tests, `-race` on all targets. Existing tests untouched.

**Tech Stack:** Go testing, stretchr/testify, os/exec (smoke),
httptest (E2E)

---

## Task List

- Task 1: Add testify dependency + Makefile targets
- Task 2: Seed helpers in storageutil
- Task 3: Storage/sqlite unit gap fill (72.5% -> 85%+)
- Task 4: Search unit gap fill (79.2% -> 90%+)
- Task 5: Server/http unit gap fill (78.5% -> 90%+)
- Task 6: Service unit gap fill (84.4% -> 90%+)
- Task 7: Config unit gap fill (85.2% -> 90%+)
- Task 8: dpkms/cmd unit gap fill (50.3% -> 85%+)
- Task 9: Jobs concurrency tests with race detection
- Task 10: E2E test expansion
- Task 11: Binary smoke test infrastructure
- Task 12: Binary smoke tests
- Task 13: Coverage gate + checklist update

---

### Task 1: Add testify dependency + Makefile targets

**Files:**
- Modify: `go.mod`
- Modify: `Makefile`

**What to do:**

1. `go get github.com/stretchr/testify`
2. `go mod tidy`
3. Add Makefile targets:
   - `test-unit` — runs `go test -race -count=1 ./cmd/... ./internal/...`
   - `test-integration` — runs `go test -race -count=1 ./test/integration/...`
   - `test-smoke` — runs `go test -tags=smoke ./test/smoke/...`
   - `test-all` — depends on test-unit, test-integration, test-smoke
   - `test-cover` — runs `go test -race -coverprofile=coverage.out ./...`
4. Update existing `test` target to use `-race -count=1`
5. Add `coverage.out` to `.gitignore` if one exists
6. Run `make test-unit` to verify nothing breaks
7. Commit: `build: add testify dep and granular Makefile test targets`

---

### Task 2: Seed helpers in storageutil

**Files:**
- Modify: `internal/storageutil/testutil.go`
- Modify: `internal/storageutil/testutil_test.go`

**What to do:**

1. Add `SeedObjects(t, driver, n)` — creates n KnowledgeObjects
   with unique IDs, type "text", timestamps, minimal content.
   Returns the created objects for assertion.
2. Add `SeedEntities(t, driver, slugs...)` — creates entities
   with given slugs, namespace derived from slug prefix.
3. Add `SeedJobs(t, driver, statuses...)` — creates jobs with
   given statuses (pending/running/completed/failed), unique IDs.
4. Write tests for each seed helper verifying correct counts
   and field values.
5. Run tests, verify pass.
6. Commit: `test: add seed helpers to storageutil`

---

### Task 3: Storage/sqlite unit gap fill (72.5% -> 85%+)

**Files:**
- Modify: `internal/storage/sqlite/objects_test.go`
- Modify: `internal/storage/sqlite/edges_test.go`
- Modify: `internal/storage/sqlite/jobs_test.go`

**What to do:**

**objects — `ListBySQL`:**
1. Test with simple WHERE clause (`type = ?`) and verify correct
   results returned with count.
2. Test with empty result set — verify empty slice and count=0.
3. Test with limit and offset — verify pagination works.
4. Test with complex WHERE using JSON functions
   (e.g. `json_extract(metadata, '$.key') = ?`).

**edges — `Delete`:**
1. Create an edge, delete it by ID, verify it's gone.
2. Delete a non-existent edge ID — verify error message.
3. Delete the same edge twice — second call should error.

**jobs — `marshalJSON`:**
1. Test with nil value — verify returns `"{}"`.
2. Test with a map containing nested values — verify valid JSON.
3. Test round-trip: marshal then unmarshal, verify equality.

Run `go test -race ./internal/storage/sqlite/... -v`, verify pass.
Commit: `test: fill storage/sqlite coverage gaps`

---

### Task 4: Search unit gap fill (79.2% -> 90%+)

**Files:**
- Modify: `internal/search/compiler_test.go`
- Modify: `internal/search/parser_test.go`
- Modify: `internal/search/lexer_test.go`

**What to do:**

**compiler — `compileSimilar`:**
1. Test `similar==keyword` compiles to FTS5 MATCH clause.
2. Test that non-`==` operator on similar field returns error.

**compiler — `sqlOperator` full coverage:**
3. Table-driven test covering all 8 operators (==, !=, >, >=,
   <, <=, =in=, =out=) mapping to correct SQL.
4. Test unknown operator returns error.

**compiler — `compileDirect` with IN/OUT:**
5. Test `type=in=(article,note)` produces correct IN clause.
6. Test `type=out=(article)` produces NOT IN clause.
7. Test IN with non-slice value returns error.

**compiler — `compileTag`:**
8. Test `tag==performance` produces EXISTS with json_each.
9. Test `tag!=performance` produces NOT EXISTS.
10. Test `tag=in=(a,b)` produces EXISTS with IN list.
11. Test unsupported operator on tag returns error.

**compiler — `compileMention`:**
12. Test `mention==@entity.slug` produces edge subquery.
13. Test non-`==` operator on mention returns error.

**parser error paths:**
14. Empty input — verify error.
15. Unterminated quoted string — verify error from lexer.
16. Missing value after operator — verify error.
17. Unbalanced parentheses — verify error.

**lexer edge cases:**
18. Whitespace-only input.
19. Field with dots (e.g. `metadata.key`).

Run `go test -race ./internal/search/... -v`, verify pass.
Commit: `test: fill search package coverage gaps`

---

### Task 5: Server/http unit gap fill (78.5% -> 90%+)

**Files:**
- Modify: `internal/server/http/handlers_jobs_test.go`
- Modify: `internal/server/http/server_test.go`
- Modify: `internal/server/http/middleware_test.go`

**What to do:**

**RetryJob handler:**
1. Test POST retry on a non-existent job — verify 400 response.
2. Test POST retry on a job that exceeds max retries — verify
   400 response with error message.
3. Test successful retry returns the updated job as JSON.

**Health endpoint error path:**
4. Create a service with a mock/broken storage driver where
   `Health()` returns an error. Hit /health, verify 503 with
   UNHEALTHY error code.

**Recoverer middleware:**
5. Create a handler that panics. Wrap with Recoverer. Send a
   request. Verify 500 response with INTERNAL_ERROR, and that
   the server continues serving after the panic.

**NotFound handler:**
6. Hit a non-existent route. Verify 404 with NOT_FOUND code.

Run `go test -race ./internal/server/http/... -v`, verify pass.
Commit: `test: fill server/http coverage gaps`

---

### Task 6: Service unit gap fill (84.4% -> 90%+)

**Files:**
- Modify: `internal/service/service_test.go`

**What to do:**

**ListEntities:**
1. Seed 3 entities via driver. Call `ListEntities` with empty
   filter. Verify all 3 returned.
2. Call with `Namespace` filter. Verify only matching returned.
3. Call with `Limit=1`. Verify only 1 returned.
4. Call on empty store. Verify empty slice, no error.

Run `go test -race ./internal/service/... -v`, verify pass.
Commit: `test: fill service coverage gaps`

---

### Task 7: Config unit gap fill (85.2% -> 90%+)

**Files:**
- Modify: `internal/config/config_test.go`

**What to do:**

**EnsureConfigDir error paths:**
1. Set `CTXT_CONFIG` env to a path under a read-only directory
   (create a temp dir, chmod 0444). Call `EnsureConfigDir()`.
   Verify error returned. Clean up env and perms after.
2. Test `EnsureConfigDir` with valid path — verify directory
   created.

**GetConfigPath:**
3. Test with `CTXT_CONFIG` set — verify returns env value.
4. Test with `CTXT_CONFIG` unset — verify returns default path.

Run `go test -race ./internal/config/... -v`, verify pass.
Commit: `test: fill config coverage gaps`

---

### Task 8: dpkms/cmd unit gap fill (50.3% -> 85%+)

**Files:**
- Modify: `cmd/dpkms/cmd/housekeeping_test.go`

**What to do:**

**`runPrune`:**
1. Test that prune command requires --before flag (run without
   it, verify error about required flag).
2. Test prune with --before flag set — verify it runs
   (currently outputs placeholder text; just verify no error
   and output contains the date string).
3. Note: `runPrune` reads from stdin (fmt.Scanln), so pipe "n"
   to stdin for the test to avoid hanging.

**`runServe` is already covered by `serve_integration_test.go`
(starts full stack). The 0% is because the coverage tool doesn't
cross package boundaries. The existing test validates the same
code path. No additional test needed — document this in the
checklist update.**

Run `go test -race ./cmd/dpkms/cmd/... -v`, verify pass.
Commit: `test: fill dpkms/cmd coverage gaps`

---

### Task 9: Jobs concurrency tests with race detection

**Files:**
- Create: `internal/jobs/race_test.go`

**What to do:**

**Multiple workers dequeuing:**
1. Create 10 jobs, start WorkerPool with 4 workers. Wait for
   all jobs to complete. Verify each job processed exactly once
   (no duplicates, no missed jobs). Use a 5s timeout.

**Concurrent enqueue + dequeue:**
2. Start a worker pool. In parallel, enqueue 20 jobs from a
   separate goroutine. Verify all 20 eventually complete.

**Shutdown with in-flight:**
3. Enqueue 5 jobs. Start pool. Cancel context after first job
   completes. Verify no panics, no deadlocks. Pool returns
   within reasonable timeout (3s).

**Stale recovery under load:**
4. Create a job, manually set its status to "running" with an
   old started_at time (simulating a stale job). Start pool.
   Verify the stale job gets recovered and reprocessed.

All tests MUST pass with `-race` flag.
Commit: `test: add concurrency tests for job system`

---

### Task 10: E2E test expansion

**Files:**
- Modify: `test/integration/e2e_test.go`

**What to do:**

Add tests using the existing `startTestEnv` infrastructure:

**TestIngestThenSearch:**
1. POST analyze with known text content. Wait for completion.
   GET /api/v1/search?q=type==text. Verify the ingested object
   appears in results.

**TestEntityLifecycle:**
2. Create an entity via driver. POST analyze with content.
   GET /api/v1/entities/{slug}. Verify entity exists.
   GET /api/v1/entities/{slug}/backlinks after ingesting
   content that mentions it (using custom pipeline step).

**TestConcurrentIngestion:**
3. Fire 10 parallel POST /api/v1/analyze calls. Wait for all
   10 jobs to complete. Verify 10 distinct objects created.
   GET /api/v1/objects — verify total >= 10.

**TestObjectDeleteCascade:**
4. Ingest content with mentions (creates edges). Delete the
   object via API. Verify edges are also deleted.

Run `go test -race ./test/integration/... -v`, verify pass.
Commit: `test: expand E2E test scenarios`

---

### Task 11: Binary smoke test infrastructure

**Files:**
- Create: `test/smoke/smoke_test.go` (build-tagged `smoke`)
- Create: `test/testutil/binary.go`

**What to do:**

**test/testutil/binary.go:**
1. `BinaryPath(name)` — returns `bin/{name}` absolute path.
2. `EnsureBuilt(t)` — runs `make build` if binaries missing.
   Skips test with `t.Skip` if build fails.
3. `Run(t, name, args...)` — exec.Command the binary, capture
   stdout+stderr, return output+error.
4. `StartServer(t, extraArgs...)` — starts `bin/dpkms serve`
   on a random port, waits for /health 200, returns URL and
   a cleanup function that sends SIGTERM.

**test/smoke/smoke_test.go:**
1. Add `//go:build smoke` build tag at top.
2. Add `TestMain` that calls `testutil.EnsureBuilt`.
3. Verify the setup compiles: `go vet -tags=smoke ./test/smoke/...`

Commit: `test: add smoke test infrastructure`

---

### Task 12: Binary smoke tests

**Files:**
- Modify: `test/smoke/smoke_test.go`

**What to do:**

**TestCtxtVersion:**
1. Run `bin/ctxt version`. Verify exit 0. Verify output
   contains "ctxt" and a version-like string.

**TestCtxtHelp:**
2. Run `bin/ctxt --help`. Verify exit 0. Verify output contains
   all subcommands: analyze, list, find, open, edit, delete,
   make, jobs, profile, config, registry, entities, completion.

**TestCtxtCompletionBash:**
3. Run `bin/ctxt completion bash`. Verify exit 0. Verify output
   starts with `#` or contains "bash completion".

**TestDpkmsVersion:**
4. Run `bin/dpkms version`. Verify exit 0.

**TestDpkmsServeHealthAndShutdown:**
5. Start dpkms serve on random port. Verify /health returns 200.
   Send SIGTERM. Verify process exits cleanly within 5s.

**TestRoundTrip:**
6. Start dpkms serve. Use net/http to POST /api/v1/analyze with
   test content. Poll job status. Verify completion. GET the
   resulting object. Verify it exists. Stop server.

Run `make build && go test -tags=smoke ./test/smoke/... -v`.
Commit: `test: add binary smoke tests`

---

### Task 13: Coverage gate + checklist update

**Files:**
- Modify: `IMPLEMENTATION_CHECKLIST.md`
- Modify: `Makefile`

**What to do:**

1. Run `make test-cover` and `go tool cover -func=coverage.out`.
   Record per-package and total coverage.
2. Verify total >= 90%. If any package is below 80% (excluding
   `cmd/*/main.go`), identify remaining gaps and add targeted
   tests to reach the threshold.
3. Run `make test-all` — all three tiers must pass.
4. Run `go test -race ./...` — zero race conditions.
5. Update `IMPLEMENTATION_CHECKLIST.md`:
   - Check off all Phase 7 items that are now complete.
   - Update the coverage numbers in the summary section.
   - Note that `runServe` coverage is cross-package (covered
     by integration test, not visible in per-package stats).
6. Add a `test-gate` Makefile target that runs test-all and
   prints a coverage summary.
7. Commit: `test: phase 7 complete — 90%+ coverage`
