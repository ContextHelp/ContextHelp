# Storage Driver Parity — Gap Report & Conformance Suite Design

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Companion:** [2026-08-04-storage-architecture-pov.md](2026-08-04-storage-architecture-pov.md)

## Scope

The architecture assessment flagged Postgres parity drift as the top standing risk: ~24 store interfaces in `internal/storage/storage.go`, implemented twice (`internal/storage/sqlite/`, `internal/storage/postgres/`), with no shared conformance suite. This document inventories the actual drift with file-level evidence, surveys what each driver's tests really exercise, and designs a cross-driver conformance suite that runs against both backends in CI.

The headline: the drift is not hypothetical. **The Postgres driver is broken today for its most basic operation** — every object read and write references a column no migration creates — and the tests that would have caught it exist but are never executed by CI.

## 1. Gap inventory

### 1.1 Latent runtime break: `source_key` column does not exist

`postgres/objects.go` inserts and selects `source_key` on every object operation:

- INSERT column list — `internal/storage/postgres/objects.go:46`
- `GetBySourceKey` — `internal/storage/postgres/objects.go:95`
- `objectSelectCols` (shared by Get/List/Reinforce/VectorSearch/ListBySQL) — `internal/storage/postgres/objects.go:507-513`

No statement in `internal/storage/postgres/migrations.go` creates that column: the `objects` DDL ends at `reminded_at` (`migrations.go:16-45`), and the only object-table ALTER is `graph_json` (`migrations.go:491`). On a fresh Postgres database, `ObjectStore.Create` and every read path fail with `column "source_key" does not exist`. The SQLite side added the column via its versioned migration chain (`internal/storage/sqlite/migrations.go:262` area, ADD COLUMN loop) and was never mirrored.

This is the strongest possible evidence for the drift thesis: a change landed on the SQLite driver, was partially ported (queries, not schema), and nothing red-flagged it — because nothing runs the Postgres driver.

### 1.2 Whole stores stubbed on Postgres

Six of the ~24 interfaces are wired to stubs that return errors on every call:

| Store | Evidence | Failure mode |
|---|---|---|
| `VectorStore` | `postgres/vectors.go:12-27` (`vectorStoreStub`) | every method errors "not yet implemented" |
| `MeteringStore` | `postgres/metering.go:12` | all methods error |
| `AttachmentStore` | `postgres/attachments.go:11-31` (`ErrNotImplemented`) | all methods error |
| `ResurfacingQueueStore` | `postgres/resurfacing_queue.go:13` | all methods error |
| `SavedSearchStore` | `postgres/saved_searches.go:12` | all methods error |
| `SearchHistoryStore` | `postgres/search_history.go:12` | all methods error |

The stubs are wired unconditionally in the driver constructor (`postgres/driver.go:69-74, 110`). Because Go interface satisfaction is structural, the compiler enforces *method-set* parity and says nothing about behavior — the type system actively hides these gaps. Error strings also drift per file (`ErrNotImplemented` sentinel vs. six distinct `fmt.Errorf` texts), so callers cannot even detect "unsupported" uniformly.

### 1.3 ObjectStore method gaps

Three `ObjectStore` methods are error stubs on Postgres:

- `FTSSearch` — `postgres/objects.go:491-493`
- `FTSSearchNodeAware` — `postgres/objects.go:496-498`
- `VectorSearchNodeAware` — `postgres/objects.go:501-503`

SQLite implements all three (`sqlite/objects.go:1034`, `1209`, `1248`) over the `objects_fts` FTS5 table it maintains transactionally inside `Create`/`Update` (`sqlite/objects.go:86, 352`). Postgres has no full-text story at all — no tsvector column, no trigger, nothing to search. The driver exposes `SQLDialect()` (`postgres/driver.go:118-119`) so the search layer can switch SQL flavor, but keyword and hybrid search are structurally unavailable on the Postgres path.

### 1.4 Silently dropped semantics: profiles

SQLite objects carry `profile_id` and filter on it (`sqlite/objects.go:223-225`; column present in every INSERT/SELECT list, e.g. `sqlite/objects.go:68, 115`). The Postgres schema has no `profile_id` column and `postgres/objects.go` neither writes `KnowledgeObject.ProfileID` nor honors `ObjectFilter.ProfileID` (`List`, `postgres/objects.go:103-209`, has no profile condition). This is worse than an error stub: profile-scoped data is **silently flattened into the global namespace** — a correctness and privacy-adjacent bug, invisible to callers.

Same pattern for `projected_fts_body`: an SQLite column feeding the FTS index (`sqlite/migrations.go:402`) with no Postgres counterpart.

### 1.5 Two incompatible vector designs inside one driver

- SQLite: embeddings live in a composite-key `embeddings` table plus a `vec_objects` sqlite-vec ANN index; `VectorSearch` picks ANN vs. brute-force at runtime (`sqlite/objects.go:911-919`); dimension is configurable before Init via `Driver.SetVectorDimension` (`sqlite/driver.go:92-95`).
- Postgres: `ObjectStore.VectorSearch` is implemented against a pgvector `objects.embedding vector(1536)` column (`postgres/objects.go:417`, DDL at `migrations.go:29`, `CREATE EXTENSION vector` at `migrations.go:15`) — dimension hardcoded to 1536, no configurability. Meanwhile the ADR-071 mirror created a *second* representation, an `embeddings` table storing `BYTEA` (`migrations.go:275-283`), which nothing queries, and the standalone `VectorStore` is a stub whose comment defers "pgvector integration" as future work — while pgvector is already in use one file over.

So Postgres simultaneously: requires the pgvector extension for its migrations to succeed, uses it in `ObjectStore.VectorSearch`, stubs it in `VectorStore`, and carries a dead byte-array embeddings table. That is drift compounding on drift.

### 1.6 Migration frameworks have diverged structurally

- SQLite: versioned chain 1–33 with a `schema_version` ledger and mixed SQL/function migrations (`sqlite/migrations.go:117-189`, ledger at `274-311`). New schema changes are appended as numbered steps.
- Postgres: an *unversioned* list of idempotent `CREATE ... IF NOT EXISTS` statements re-executed on every Init, plus five hand-written idempotent helper functions (`postgres/migrations.go:13-330`). No ledger, no ordering guarantees beyond slice position, and — critically — **column adds must be hand-mirrored as bespoke information-schema checks** (`migrations.go:480-495`). The `source_key` break in §1.1 is the direct product of this asymmetry: on SQLite a column add is one appended migration; on Postgres it requires remembering to write a matching helper, and nobody did.

Additional schema-semantics drift: Postgres mixes naive `TIMESTAMP` (objects, jobs, edges) with `TIMESTAMPTZ` (watermarks, embeddings, index_signatures — `migrations.go:258, 270-291`), while SQLite stores RFC3339 text throughout. Cross-driver timestamp comparison semantics are therefore untested assumptions.

### 1.7 SQLite-only capability surfaces

- **Index signatures**: SQLite ships compute/verify/upsert helpers (`sqlite/index_signatures.go:45-180`) using `?` placeholders — SQLite-dialect SQL that cannot run on Postgres. Postgres creates the `index_signatures` table (`migrations.go:289-294`) but has no read/verify path, so index-invalidation provenance — called out in the architecture POV as a maturity signal — exists on one backend only.
- **Registry cache naming**: same store, different filenames (`sqlite/registry_cache.go` vs. `postgres/registries.go`); cosmetic, but it defeats side-by-side diffing, which is currently the only parity tool available.

### 1.8 Where parity is actually good

Credit where due — the following mirror faithfully and deserve conformance locking so they stay that way:

- **Jobs queue**: Postgres `AcquireNext` uses `FOR UPDATE SKIP LOCKED ... RETURNING` (`postgres/jobs.go:125-145`); SQLite uses a single atomic `UPDATE ... RETURNING` under the write lock (`sqlite/jobs.go:135-165`). Different mechanisms, same claim-once contract.
- **Graph canonical storage**: `graph_json` + `object_nodes` maintained transactionally in both (`postgres/objects.go:875-890`, `sqlite/objects.go:52` area), with matching indexes.
- Entities (thin-sync semantics), edges, feeds/feed-items, pipelines, steps, detectors, proximity, watches, aliases, audit log, entitlements, watermarks (epoch-default semantics stated in the interface, `storage.go:59-68`) — all substantively implemented on both sides.

## 2. Test coverage survey

| Surface | SQLite | Postgres |
|---|---|---|
| Test files | ~30 | 2 |
| Test functions | 164 | 10 |
| Build gating | `fts5` tag (unit lane passes `-tags fts5`) | `//go:build integration` on **both** files |
| Runs in CI? | Yes — unit lane `go test -short -race -tags fts5 ./...` (`.github/workflows/ci.yml:167-171`) | **No** |

The Postgres tests (`postgres/graph_rw_test.go` — five graph/migration round-trip tests; `postgres/integration_dsn_test.go` — five DSN-builder tests) are integration-tagged, but the CI integration job runs only `go test -tags=integration,fts5 ./test/integration/...` (`.github/workflows/ci.yml:228`). The path filter excludes `internal/storage/postgres`, so **zero Postgres driver tests execute anywhere in CI** — despite the job provisioning a `postgres:16-alpine` service container with the full env block (`ci.yml:186-191, 231-235`) that those very tests are written to consume. `TestPostgres_MigrationAppliesCleanly` and `TestPostgres_GraphRoundtrip_Create` would fail today on the §1.1 column bug; they simply never run.

Supporting infrastructure that already exists and should be reused rather than rebuilt:

- `integrationDSN()` / `newIntegrationDriver()` — env-block DSN builder with skip-if-unset semantics (`postgres/graph_rw_test.go:31-80`, covered by `integration_dsn_test.go`).
- `docker-compose.dev.yml:14-35` — local `postgres` service (`ctxt`/`ctxt`/`ctxt` on 5432) giving developers the same target CI uses.
- `internal/storage/storage_test.go` — interface-satisfaction mocks only; verifies shape, not behavior.

## 3. Conformance suite design

### 3.1 Shape

One new package: `internal/storage/storagetest`. Exported entry point:

```go
package storagetest

// Capabilities declares which optional surfaces a driver claims.
// A claimed capability is tested; an unclaimed one is skipped LOUDLY.
type Capabilities struct {
    FTS           bool // FTSSearch / FTSSearchNodeAware
    Vectors       bool // VectorStore + ObjectStore.VectorSearch*
    Attachments   bool
    Metering      bool
    Resurfacing   bool
    SavedSearches bool
    SearchHistory bool
    Profiles      bool // ObjectFilter.ProfileID honored
}

// Factory returns a fresh, migrated driver. Cleanup via t.Cleanup.
type Factory func(t *testing.T) storage.StorageDriver

// Run executes the full cross-driver conformance suite.
func Run(t *testing.T, name string, f Factory, caps Capabilities)
```

`Run` fans out one `t.Run` subtree per store interface (`Objects`, `Jobs`, `Edges`, …), each subtree a table of behavioral assertions against the interface only — no driver imports, no raw SQL. Drivers plug in with thin wrappers:

- `internal/storage/sqlite/conformance_test.go` — factory: `sqlite.New(filepath.Join(t.TempDir(), "test.db"))` + `Init`; caps: everything true. Runs in the existing unit lane (`fts5` tag already set).
- `internal/storage/postgres/conformance_test.go` — `//go:build integration`; factory: promote `integrationDSN`/`newIntegrationDriver` from `graph_rw_test.go` into a shared test helper; caps: current honest state (FTS false, Vectors false, Attachments false, …). Skips when no DSN env is present, exactly like today.

### 3.2 What the assertions must cover

Method-doesn't-error is not conformance. The suite should lock the *documented semantics* in `storage.go`, because those are what consumers rely on and where silent drift hides:

1. **Contract semantics stated in interface comments**: watermark missing-row → epoch (`storage.go:63-65`); audit log append-only, `created_at` ascending order (`storage.go:117-124`); reminder lifecycle (`SetReminder`/`ListDueReminders`/`MarkReminded`); `UpsertThin` never overwriting a `full` entity (`storage.go:177-179`).
2. **Concurrency contracts**: N goroutines calling `Jobs().AcquireNext` claim each pending job exactly once; `RecoverStale` requeues; `Complete`/`Fail`/`Retry` state machine transitions.
3. **Filter fidelity**: every `ObjectFilter`/`JobFilter`/… field either filters or the driver must not claim the capability. This is the test that catches §1.4 — a `Profiles: true` driver must prove ProfileID isolation.
4. **Round-trip fidelity**: create → get → deep-equal for every entity type, including graph nodes, embeddings, metadata maps, and timestamp round-tripping within tolerance (catches TEXT-RFC3339 vs. TIMESTAMP vs. TIMESTAMPTZ drift, §1.6).
5. **Error taxonomy**: not-found behavior per store (nil-nil vs. error) asserted identically for both drivers — today this is unspecified and almost certainly divergent.
6. **Schema parity probe**: one test introspects the live schema (`sqlite_master` / `information_schema.columns`) and diffs each shared table's column set against a checked-in manifest. This is the cheap structural tripwire that catches the next `source_key` the day it lands, without waiting for a behavioral test to trip over it.

### 3.3 Isolation between Postgres tests

SQLite gets a fresh temp file per test; Postgres needs an equivalent. Cheapest robust option against the existing single CI database: the factory creates a uniquely named schema per test (`CREATE SCHEMA conf_<rand>; SET search_path`) and drops it in cleanup — or, simpler given the migrations are `IF NOT EXISTS`-idempotent, `DROP ... CASCADE`+re-migrate once per `Run` invocation and `TRUNCATE` the table list between subtests. Start with truncate-between-subtests; it needs no migration changes.

### 3.4 CI wiring

Two-line change to the integration job (`.github/workflows/ci.yml:228`):

```yaml
- name: Run integration tests
  run: go test -v -tags=integration,fts5 ./test/integration/... ./internal/storage/...
```

The service container, env block, and DSN builder already exist; this closes the §2 gap and turns the ten existing Postgres tests plus the new conformance run live. The unit lane needs no change — the SQLite conformance wrapper is picked up by `./...` automatically.

Local parity: `docker compose -f docker-compose.dev.yml up postgres`, then `POSTGRES_DSN=postgres://ctxt:ctxt@localhost:5432/ctxt?sslmode=disable go test -tags=integration,fts5 ./internal/storage/postgres/`.

### 3.5 Rollout order

1. **Land the harness with SQLite green.** All caps true; fix any latent SQLite issues the semantics assertions surface.
2. **Wire the CI path change** so the existing (pre-conformance) Postgres tests run at all. Expect immediate red from §1.1 — fix by adding `source_key` (and deciding `profile_id`/`projected_fts_body`) to the Postgres migration set.
3. **Add the Postgres conformance wrapper** with honest capability flags. Every `false` renders as an explicit skip in test output — the parity gap becomes a visible, countable burn-down list instead of tribal knowledge.
4. **Burn down capabilities** in value order: Profiles (correctness bug), Vectors (pgvector is already half-wired; also resolve the dual embeddings representation, §1.5), FTS (tsvector or accept and document keyword-search-is-SQLite-only), then the four CRUD stubs (attachments, saved searches, search history, resurfacing, metering — mechanical ports).
5. **Adopt a versioned migration ledger for Postgres** mirroring the SQLite `schema_version` pattern, so future schema changes are one appended step on both sides rather than an idempotent-helper ritual on one.

## Verdict

The compiler guarantees the two drivers agree on method signatures and nothing else — and the codebase demonstrates every failure mode that leaves open: whole-store stubs hidden behind interfaces, silently dropped filter semantics, a schema column referenced but never created, and a test suite that exists but is unreachable from CI. None of this required new infrastructure to detect; the DSN builder, the service container, and the compose service were all already in place. The conformance suite is mostly a matter of connecting them, then converting the parity gap from an unknown into a skip-count that trends to zero.
