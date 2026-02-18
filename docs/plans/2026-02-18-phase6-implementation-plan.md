# Phase 6: Business Logic — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace all stub implementations with working storage, jobs, pipelines, search, and HTTP/gRPC servers so `dpkms serve` + `ctxt analyze/find/list` work end-to-end.

**Architecture:** Six subsystems built bottom-up. Storage first (no deps), then jobs + pipeline + search (depend on storage, independent of each other), then HTTP + gRPC (depend on everything). See `docs/plans/2026-02-18-phase6-business-logic-design.md` for full design.

**Tech Stack:** Go 1.25, SQLite (go-sqlite3 or modernc.org/sqlite), chi v5, gRPC, errgroup, uuid.

---

## Parallel Tracks

Four agents can work simultaneously on tracks A–D. Dependencies shown below.

```
Track A: Storage          Track B: Pipeline        Track C: Search         Track D: gRPC Proto
  Task 1: types             Task 6: interfaces       Task 9: AST             Task 13: proto files
  Task 2: interfaces        Task 7: built-in steps   Task 10: lexer+parser   Task 14: codegen
  Task 3: SQLite driver     Task 8: registry         Task 11: compiler
  Task 4: migrations                                  Task 12: engine
  Task 5: factory + test helper

─── SYNC POINT: Storage + Pipeline + Search + Proto all done ───

Track E: Service Layer (any agent)      Track F: Jobs (any agent)
  Task 15: service layer                  Task 16: job queue
                                          Task 17: worker pool

─── SYNC POINT: Service + Jobs done ───

Track G: HTTP Server (any agent)        Track H: gRPC Server (any agent)
  Task 18: router + middleware            Task 21: server + interceptors
  Task 19: handlers (objects, analyze)    Task 22: service implementations
  Task 20: handlers (jobs, search, entities)

─── SYNC POINT: HTTP + gRPC done ───

Track I: Wiring (single agent)
  Task 23: wire dpkms serve
  Task 24: wire ctxt analyze
  Task 25: integration tests
```

---

## Track A: Storage Layer

### Task 1: Domain Types

**Files:** Create `internal/storage/types.go`

**Red:** Write `internal/storage/types_test.go` — test JSON round-trip for KnowledgeObject, Entity, Edge, Tag, Section, Decision, Task. Test that Draft is a type alias for KnowledgeObject. Test ObjectFilter and EntityFilter zero values.

**Green:** Define all structs per design doc section "Domain Types". Include `Draft = KnowledgeObject` alias (ADR-053).

**Commit:** `feat(storage): add domain types`

---

### Task 2: Storage Interfaces

**Files:** Create `internal/storage/storage.go`

**Red:** Write `internal/storage/storage_test.go` — verify interface satisfaction: a mock struct that implements StorageDriver compiles. Same for ObjectStore, EntityStore, EdgeStore, JobStore.

**Green:** Define StorageDriver, ObjectStore, EntityStore, EdgeStore interfaces per design doc. Add `NewDriver` factory signature (implementation in Task 5). Add `JobStore` interface here too (jobs package will use it).

**Commit:** `feat(storage): add storage interfaces`

---

### Task 3: SQLite Driver

**Files:** Create `internal/storage/sqlite/driver.go`, `objects.go`, `entities.go`, `edges.go`, `jobs.go`

**Red:** Write `internal/storage/sqlite/driver_test.go`:
- `TestInit` — driver initializes without error on temp dir
- `TestHealth` — returns nil after init
- `TestClose` — no error after init

Write `internal/storage/sqlite/objects_test.go`:
- `TestCreateAndGet` — create object, get by ID, fields match
- `TestList` — create 3 objects, list with limit=2 returns 2 + total=3
- `TestListFilterByType` — create mixed types, filter returns correct subset
- `TestUpdate` — create, update tags, get returns new tags
- `TestDelete` — create, delete, get returns not-found error
- `TestDeleteNotFound` — delete nonexistent ID returns error

Write `internal/storage/sqlite/entities_test.go`:
- `TestUpsertAndGet` — upsert entity, get by slug
- `TestUpsertUpdate` — upsert twice, second updates fields
- `TestResolve` — upsert with aliases, resolve by alias returns entity
- `TestList` — upsert 3, list with namespace filter

Write `internal/storage/sqlite/edges_test.go`:
- `TestCreateAndListFrom` — create edge, list by from_id
- `TestListTo` — create edges, list by to_id (backlinks)
- `TestDeleteByObject` — create edges for object, delete all, verify empty

Write `internal/storage/sqlite/jobs_test.go`:
- `TestEnqueueAndGet` — enqueue job, get by ID
- `TestAcquireNext` — enqueue 2 jobs, acquire returns oldest pending, sets status=running
- `TestAcquireNextEmpty` — no pending jobs, returns nil
- `TestComplete` — acquire, complete with resultID, verify status+resultID
- `TestFail` — acquire, fail with error message, verify status+error
- `TestRetry` — fail a job, retry resets to pending with incremented retry_count
- `TestList` — enqueue 3, list with status filter
- `TestRecoverStale` — acquire job, call RecoverStale with 0 timeout, verify reset to pending

**Green:** Implement each file. SQLite driver opens connection with WAL pragma, foreign_keys=ON, cache_size=-64000. Use `database/sql` with `go-sqlite3` (or `modernc.org/sqlite`). JSON fields stored as TEXT, marshaled/unmarshaled in Go. Timestamps stored as RFC3339 strings.

**Commit:** `feat(storage): implement SQLite driver` (or split into sub-commits per file)

---

### Task 4: Migrations

**Files:** Create `internal/storage/sqlite/migrations.go`, `internal/storage/sqlite/migrations/001_initial.sql`

**Red:** Write `internal/storage/sqlite/migrations_test.go`:
- `TestMigrateFromScratch` — new DB, migrate, verify all tables exist via `sqlite_master`
- `TestMigrateIdempotent` — migrate twice, no error
- `TestSchemaVersion` — after migration, schema_version table has version=1
- `TestFTSTableExists` — verify objects_fts virtual table exists after migration

**Green:** Embed SQL file via `//go:embed`. Migration runner reads schema_version, applies pending migrations in order. The 001_initial.sql is the full schema from the design doc.

**Commit:** `feat(storage): add migration system`

---

### Task 5: Factory + Test Helper

**Files:** Update `internal/storage/storage.go` (factory body), create `internal/storage/testutil.go`

**Red:** Write `internal/storage/factory_test.go`:
- `TestNewDriverSQLite` — factory with type="sqlite" returns working driver
- `TestNewDriverPostgres` — factory with type="postgres" returns "not yet implemented" error
- `TestNewDriverUnknown` — factory with type="foo" returns "unknown storage type" error

Write `internal/storage/testutil_test.go`:
- `TestNewTestDriver` — helper returns initialized driver that can Create+Get an object

**Green:** Implement `NewDriver` factory. Implement `NewTestDriver(t)` helper that creates temp dir, initializes SQLite driver, registers cleanup.

**Commit:** `feat(storage): add driver factory and test helper`

---

## Track B: Pipeline Runtime

### Task 6: Pipeline Interfaces

**Files:** Create `internal/pipeline/pipeline.go`

**Red:** Write `internal/pipeline/pipeline_test.go`:
- `TestPipelineStepInterface` — a noop step satisfies PipelineStep interface
- `TestRegistryInterface` — a registry satisfies Registry interface

**Green:** Define PipelineStep interface (Name, Run), Pipeline struct, Registry interface (Register, Get, List, SelectPipeline) per design doc.

**Commit:** `feat(pipeline): add pipeline interfaces`

---

### Task 7: Built-in Steps

**Files:** Create `internal/pipeline/steps/noop.go`, `typedetect.go`, `sectioner.go`, `tagger.go`

**Red:** Write `internal/pipeline/steps/noop_test.go`:
- `TestNoopPassthrough` — draft in equals draft out, no fields changed

Write `internal/pipeline/steps/typedetect_test.go`:
- `TestDetectText` — raw content with no URL → type="text"
- `TestDetectURL` — raw content starting with "http" → type="url"
- `TestDetectShortText` — content < 500 chars → subtype="short"
- `TestDetectLongText` — content >= 500 chars → subtype="long"

Write `internal/pipeline/steps/sectioner_test.go`:
- `TestSectionByHeadings` — markdown with `## H1\n\n## H2` → 2 sections
- `TestSectionSingleBlock` — no headings → 1 section with full content
- `TestSectionPreservesOrder` — sections have sequential Order values

Write `internal/pipeline/steps/tagger_test.go`:
- `TestExtractTags` — text mentioning "design" 5 times → Tag{Label:"design"} in output
- `TestNoTags` — very short text → empty tags slice
- `TestTagWeights` — more frequent words get higher weight

**Green:** Implement each step. Type detection uses simple heuristics (URL regex, length check). Sectioner splits on markdown headings or double newlines. Tagger does word frequency analysis, filters stop words, returns top N as tags.

**Commit:** `feat(pipeline): add built-in steps`

---

### Task 8: Registry

**Files:** Create `internal/pipeline/registry.go`

**Red:** Write `internal/pipeline/registry_test.go`:
- `TestRegisterAndGet` — register pipeline, get by name
- `TestGetNotFound` — get unregistered name returns error
- `TestList` — register 2 pipelines, list returns both names
- `TestSelectPipelineShort` — content < 500 chars → "text.short"
- `TestSelectPipelineLong` — content >= 500 chars → "text.long"
- `TestDefaultRegistry` — DefaultRegistry() has "text.short" and "text.long"

**Green:** Implement in-memory registry (map[string]*Pipeline). `SelectPipeline` checks content length. `DefaultRegistry` wires text.short and text.long with the built-in steps.

**Commit:** `feat(pipeline): add registry with default pipelines`

---

## Track C: Search Engine

### Task 9: AST Types

**Files:** Create `internal/search/ast.go`

**Red:** Write `internal/search/ast_test.go`:
- `TestComparisonNode` — create node, verify fields
- `TestAndNode` — create with 2 children
- `TestOrNode` — create with 2 children
- `TestOperatorString` — each operator has correct string representation ("==", "!=", etc.)

**Green:** Define Node interface, ComparisonNode, AndNode, OrNode structs. Operator enum with String() method. Per design doc AST section.

**Commit:** `feat(search): add AST types`

---

### Task 10: Lexer + Parser

**Files:** Create `internal/search/lexer.go`, `internal/search/parser.go`

**Red:** Write `internal/search/lexer_test.go`:
- `TestLexSimple` — `type==article` → [FIELD("type"), OP("=="), VALUE("article")]
- `TestLexAnd` — `a==1;b==2` → tokens with AND separator
- `TestLexOr` — `a==1,b==2` → tokens with OR separator
- `TestLexIn` — `tag=in=(a,b,c)` → FIELD, OP("=in="), VALUES(["a","b","c"])
- `TestLexGrouping` — `(a==1)` → LPAREN, tokens, RPAREN
- `TestLexQuoted` — `name=="hello world"` → VALUE("hello world")
- `TestLexError` — `==` alone → error

Write `internal/search/parser_test.go`:
- `TestParseSimpleComparison` — `type==article` → ComparisonNode
- `TestParseAnd` — `a==1;b==2` → AndNode with 2 ComparisonNode children
- `TestParseOr` — `a==1,b==2` → OrNode with 2 children
- `TestParsePrecedence` — `a==1,b==2;c==3` → OrNode(ComparisonNode, AndNode)
- `TestParseGrouping` — `(a==1,b==2);c==3` → AndNode(OrNode, ComparisonNode)
- `TestParseIn` — `tag=in=(a,b)` → ComparisonNode with OpIn and []string value
- `TestParseOut` — `tag=out=(a,b)` → ComparisonNode with OpOut
- `TestParseAllOperators` — one test per operator (==, !=, >, >=, <, <=, =in=, =out=)
- `TestParseError` — malformed input returns parse error with position

**Green:** Lexer tokenizes RSQL input into token stream. Parser is recursive descent following the grammar in the design doc. `,` (OR) binds weaker than `;` (AND).

**Commit:** `feat(search): add RSQL lexer and parser`

---

### Task 11: Compiler

**Files:** Create `internal/search/compiler.go`

**Red:** Write `internal/search/compiler_test.go`:
- `TestCompileEq` — `type==article` → SQL `WHERE type = ?` with arg "article"
- `TestCompileNeq` — `type!=draft` → `WHERE type != ?`
- `TestCompileGte` — `created_at>=2025-01-01` → `WHERE created_at >= ?`
- `TestCompileIn` — `tag=in=(a,b)` → JSON subquery with IN clause
- `TestCompileAnd` — `type==article;tag=in=(ui)` → `(type = ?) AND (json subquery)`
- `TestCompileOr` — `type==article,type==note` → `(type = ?) OR (type = ?)`
- `TestCompileMention` — `mention==@ui.best-practice` → edge JOIN query
- `TestCompileNested` — `(a==1,b==2);c==3` → correct SQL with parens
- `TestCompileUnknownField` — unknown field returns error

**Green:** Walk AST, emit SQL WHERE clause + args slice. Direct columns (type, subtype, pipeline, source) map to column comparisons. `tag` maps to `json_each` subquery. `mention` maps to edge JOIN (ADR-049). Date fields map to string comparison (RFC3339). `similar` maps to FTS5 MATCH as fallback.

**Commit:** `feat(search): add AST-to-SQL compiler`

---

### Task 12: Search Engine

**Files:** Create `internal/search/engine.go`

Depends on: Task 5 (test helper), Task 10 (parser), Task 11 (compiler)

**Red:** Write `internal/search/engine_test.go`:
- `TestSearchByType` — insert 2 objects (article + note), search `type==article` → returns 1
- `TestSearchByTag` — insert object with tags, search `tag==design` → returns it
- `TestSearchAnd` — `type==article;tag==design` → correct intersection
- `TestSearchOr` — `type==article,type==note` → returns both
- `TestSearchByMention` — insert object + edge, search `mention==@ui.layout` → returns object
- `TestSearchPagination` — insert 5 objects, search with limit=2, offset=2 → returns 2, total=5
- `TestSearchEmpty` — search with no matches → empty results, total=0
- `TestSearchInvalidQuery` — malformed RSQL → error

**Green:** Engine wires parser → compiler → storage. Add `ListBySQL(ctx, sql, args, limit, offset)` method to ObjectStore interface and SQLite implementation.

**Commit:** `feat(search): add query engine`

---

## Track D: gRPC Proto Files

### Task 13: Proto Definitions

**Files:** Create `api/proto/dpkms/v1/analyze.proto`, `objects.proto`, `jobs.proto`, `entities.proto`, `common.proto`

**Red:** Proto files compile without error via `protoc` or `buf`.

**Green:** Define messages and service RPCs matching the design doc gRPC section. Use `google.protobuf.Timestamp` for time fields. Keep messages minimal — just enough for Phase 6 endpoints.

**Commit:** `feat(proto): add gRPC service definitions`

---

### Task 14: Proto Codegen

**Files:** Generate `api/proto/dpkms/v1/*.pb.go`, `*_grpc.pb.go`. Add `buf.gen.yaml` or Makefile target.

**Red:** Generated Go code compiles. Import from `internal/` packages works.

**Green:** Add `make proto` target (or buf generate). Verify generated code compiles with `go build ./api/...`.

**Commit:** `feat(proto): add code generation`

---

## Track E: Service Layer

Depends on: Track A (storage), Track B (pipeline), Track C (search)

### Task 15: Service Layer

**Files:** Create `internal/service/service.go`, `internal/service/types.go`

**Red:** Write `internal/service/service_test.go`:
- `TestAnalyze` — call Analyze, verify job enqueued in storage
- `TestGetObject` — insert object in storage, call GetObject, verify fields
- `TestListObjects` — insert 3, call ListObjects with filter, verify count
- `TestUpdateObject` — insert, update via service, verify changes persisted
- `TestDeleteObject` — insert, delete via service, verify gone
- `TestSearchObjects` — insert objects, call SearchObjects with RSQL, verify results
- `TestGetJob` — enqueue job, call GetJob, verify
- `TestListJobs` — enqueue 3, call ListJobs, verify
- `TestRetryJob` — fail a job, retry via service, verify status reset
- `TestGetEntity` — upsert entity, call GetEntity, verify
- `TestEntityBacklinks` — create object + edge, call EntityBacklinks, verify

**Green:** Service struct holds StorageDriver, JobQueue, Registry, Engine. Each method delegates to the appropriate subsystem. AnalyzeRequest/Response types in types.go.

**Commit:** `feat(service): add service layer`

---

## Track F: Job System

Depends on: Track A (storage), Track B (pipeline)

### Task 16: Job Queue

**Files:** Create `internal/jobs/queue.go`, `internal/jobs/types.go`

**Red:** Write `internal/jobs/queue_test.go`:
- `TestEnqueueAndGet` — enqueue, get by ID
- `TestAcquireNext` — enqueue 2, acquire returns oldest, status=running
- `TestAcquireNextEmpty` — empty queue returns nil, nil
- `TestComplete` — acquire, complete, verify status + result_id
- `TestFail` — acquire, fail, verify status + error
- `TestRetry` — fail, retry, verify status=pending + retry_count incremented
- `TestRetryMaxExceeded` — fail with retry_count >= max_retries, retry returns error
- `TestRecoverStale` — acquire, recover with 0 timeout, verify reset
- `TestListByStatus` — enqueue 3 with mixed statuses, filter by status

**Green:** Queue wraps storage.JobStore. `AcquireNext` uses atomic UPDATE...RETURNING. Types moved from storage to jobs package if cleaner, or re-exported.

**Commit:** `feat(jobs): add job queue`

---

### Task 17: Worker Pool

**Files:** Create `internal/jobs/worker.go`

Depends on: Task 16 (queue), Task 8 (registry)

**Red:** Write `internal/jobs/worker_test.go`:
- `TestProcessJob` — enqueue job with "text.short" pipeline, start pool, wait, verify object created in storage
- `TestProcessJobCreatesEdges` — enqueue job, pipeline step adds mentions, verify edges created (ADR-049)
- `TestProcessJobFailure` — enqueue job with unknown pipeline, verify job marked failed
- `TestWorkerPoolShutdown` — start pool, cancel context, verify clean exit
- `TestStaleRecovery` — acquire job (simulating stale), run recovery, verify reset to pending

**Green:** WorkerPool starts N goroutines + 1 recovery goroutine via errgroup. Each worker polls `AcquireNext`, runs pipeline steps on a Draft (ADR-053), persists object + edges, marks job complete. On error, marks job failed.

**Commit:** `feat(jobs): add worker pool`

---

## Track G: HTTP Server

Depends on: Track E (service layer)

### Task 18: Router + Middleware + Error Envelope

**Files:** Create `internal/server/http/server.go`, `middleware.go`, `errors.go`

**Red:** Write `internal/server/http/server_test.go`:
- `TestHealthEndpoint` — GET /health returns 200 with `{"status":"ok"}`
- `TestNotFound` — GET /nonexistent returns 404 with error envelope
- `TestRequestID` — any request has X-Request-ID in response headers

Write `internal/server/http/errors_test.go`:
- `TestErrorEnvelope` — WriteError produces `{"error":{"code":"...","message":"...","details":{}}}`
- `TestNotFoundError` — 404 with NOT_FOUND code
- `TestBadRequestError` — 400 with INVALID_REQUEST code

**Green:** chi router with middleware stack (RequestID, RealIP, Logger, Recoverer). Error helper that writes JSON error envelope. Health handler.

**Commit:** `feat(http): add router, middleware, error handling`

---

### Task 19: Handlers — Objects + Analyze

**Files:** Create `internal/server/http/handlers/objects.go`, `analyze.go`

**Red:** Write `internal/server/http/handlers/objects_test.go`:
- `TestGetObject` — GET /api/v1/objects/{id} returns object JSON
- `TestGetObjectNotFound` — returns 404 error envelope
- `TestListObjects` — GET /api/v1/objects returns array + pagination
- `TestListObjectsWithFilter` — ?type=article filters correctly
- `TestUpdateObject` — PATCH /api/v1/objects/{id} with JSON body updates fields
- `TestDeleteObject` — DELETE /api/v1/objects/{id} returns 204

Write `internal/server/http/handlers/analyze_test.go`:
- `TestAnalyze` — POST /api/v1/analyze with JSON body returns job_id
- `TestAnalyzeMissingContent` — returns 400 error envelope
- `TestAnalyzeInvalidType` — returns 400 error envelope

Use `httptest.NewServer` with a real service backed by test storage.

**Green:** Each handler function takes `*service.Service`, returns `http.HandlerFunc`. Parse request, call service, write JSON response or error envelope.

**Commit:** `feat(http): add object and analyze handlers`

---

### Task 20: Handlers — Jobs, Search, Entities

**Files:** Create `internal/server/http/handlers/jobs.go`, `search.go`, `entities.go`

**Red:** Write `internal/server/http/handlers/jobs_test.go`:
- `TestListJobs` — GET /api/v1/jobs returns list
- `TestGetJob` — GET /api/v1/jobs/{id} returns job
- `TestRetryJob` — POST /api/v1/jobs/{id}/retry returns updated job

Write `internal/server/http/handlers/search_test.go`:
- `TestSearch` — GET /api/v1/search?q=type==article returns results
- `TestSearchInvalidQuery` — returns 400 with parse error
- `TestSearchPagination` — ?limit=2&offset=1 works

Write `internal/server/http/handlers/entities_test.go`:
- `TestListEntities` — GET /api/v1/entities returns list
- `TestGetEntity` — GET /api/v1/entities/{slug} returns entity
- `TestEntityBacklinks` — GET /api/v1/entities/{slug}/backlinks returns object list

**Green:** Same pattern as Task 19.

**Commit:** `feat(http): add job, search, and entity handlers`

---

## Track H: gRPC Server

Depends on: Track D (proto), Track E (service layer)

### Task 21: Server + Interceptors

**Files:** Create `internal/server/grpc/server.go`, `interceptors.go`

**Red:** Write `internal/server/grpc/server_test.go`:
- `TestServerStarts` — server starts on a free port without error
- `TestRecoveryInterceptor` — panicking handler returns gRPC Internal error, not crash

**Green:** `NewServer(svc)` returns `*grpc.Server` with recovery + logging interceptors. Register all service servers.

**Commit:** `feat(grpc): add server with interceptors`

---

### Task 22: gRPC Service Implementations

**Files:** Create `internal/server/grpc/services/analyze.go`, `objects.go`, `jobs.go`, `entities.go`

**Red:** Write tests using `google.golang.org/grpc/test/bufconn`:
- `TestGRPCAnalyze` — call Analyze RPC, verify job created
- `TestGRPCGetObject` — call GetObject RPC, verify fields
- `TestGRPCListJobs` — call ListJobs RPC, verify response
- `TestGRPCGetEntity` — call GetEntity RPC, verify fields

**Green:** Each service implementation wraps `*service.Service`. Convert between protobuf messages and domain types. Return gRPC status codes for errors.

**Commit:** `feat(grpc): add service implementations`

---

## Track I: Wiring

Depends on: All tracks above

### Task 23: Wire `dpkms serve`

**Files:** Modify `cmd/dpkms/cmd/serve.go`

**Red:** Write `cmd/dpkms/cmd/serve_integration_test.go`:
- `TestServeStartsAndStops` — start serve in goroutine, verify /health returns 200, send SIGINT, verify clean exit

**Green:** Replace stub in `runServe` with real wiring per design doc section 7: init storage → init queue → init registry → init search engine → init service → start HTTP + gRPC + workers via errgroup → graceful shutdown.

**Commit:** `feat(dpkms): wire serve command`

---

### Task 24: Wire `ctxt analyze`

**Files:** Modify `cmd/ctxt/cmd/analyze.go`

**Red:** Write `cmd/ctxt/cmd/analyze_integration_test.go`:
- `TestAnalyzeCallsDPKMS` — start test HTTP server, run analyze command, verify POST /api/v1/analyze received

**Green:** Replace stub in `runAnalyze` with HTTP client that POSTs to dpkms. Read dpkms URL from config (default `http://localhost:8080`). Add `server.url` to config.

**Commit:** `feat(ctxt): wire analyze command to dpkms API`

---

### Task 25: End-to-End Integration Tests

**Files:** Create `test/integration/e2e_test.go`

**Red/Green:** Write and make pass:
- `TestFullWritePath` — start dpkms (in-process), POST /analyze with text, wait for job completion, GET /objects returns the ingested object
- `TestFullReadPath` — seed objects, GET /search?q=type==article returns correct results
- `TestJobRetry` — POST /analyze with content that triggers step failure, verify job status=failed, POST retry, verify re-processing
- `TestEdgesCreated` — analyze content with mentions, verify edges in storage, verify /entities/{slug}/backlinks returns the object

**Commit:** `test: add end-to-end integration tests`

---

## Dependency Summary

```
go get github.com/go-chi/chi/v5
go get github.com/mattn/go-sqlite3        # or: modernc.org/sqlite
go get google.golang.org/grpc
go get google.golang.org/protobuf
go get github.com/google/uuid
go get golang.org/x/sync
go get github.com/stretchr/testify         # for require/assert in tests
```

Run `go mod tidy` after adding dependencies.

---

## Notes for Agents

1. **Read the design doc first:** `docs/plans/2026-02-18-phase6-business-logic-design.md` has all interfaces, schemas, and struct definitions.
2. **Read the ADRs:** ADR-049 through ADR-053 in `docs/decisions/` explain why decisions were made.
3. **Test helper:** After Task 5, use `storage.NewTestDriver(t)` in all tests that need a database.
4. **No AI steps in Phase 6:** All pipeline steps are deterministic heuristics. No model calls.
5. **Error wrapping:** Use `fmt.Errorf("context: %w", err)` consistently.
6. **JSON in SQLite:** Store JSON fields as TEXT columns. Marshal/unmarshal in Go code.
7. **Timestamps:** Store as RFC3339 strings in SQLite (`time.Now().Format(time.RFC3339)`).
8. **CGo:** If `go-sqlite3` requires CGo and the build environment doesn't have it, use `modernc.org/sqlite` instead. Same `database/sql` interface.
