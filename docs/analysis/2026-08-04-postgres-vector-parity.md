# Postgres Vector & Full-Text Parity — Gap Analysis and Plan

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Companions:** [2026-08-04-driver-parity-conformance.md](2026-08-04-driver-parity-conformance.md), [2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md)

## Scope

The federation addendum promoted Postgres parity from tax to strategic asset and named the missing vector/FTS story its fourth-ranked risk: hosted, team, and public instances will run Postgres, yet semantic and keyword search are SQLite-only. A federating instance that cannot search the content it federates is offering half the product. This document maps the full SQLite search pipeline Postgres must match, assesses pgvector + tsvector as the analog, and defines the parity plan — building on the conformance-suite design in the companion gap report, whose findings this analysis re-verified against source before extending.

Two new headline findings beyond the companion report:

1. **The Postgres driver cannot even initialize against the infrastructure this repo provides.** Its first migration is `CREATE EXTENSION IF NOT EXISTS vector` (`internal/storage/postgres/migrations.go:15`), but both the CI service container and the dev compose service run `postgres:16-alpine` (`.github/workflows/ci.yml:187`, `docker-compose.dev.yml:16`), which does not ship pgvector. The moment the conformance suite's CI wiring lands, every Postgres test will fail at migration 1 — before reaching any of the gaps cataloged so far. The image must move to `pgvector/pgvector:pg16` (or equivalent) as step zero.
2. **The distance metric already disagrees between drivers.** SQLite ANN ranks by L2 (`internal/storage/sqlite/vectors.go:13, 42-48`); the Postgres `ObjectStore.VectorSearch` that does exist ranks by cosine (`<=>`, `internal/storage/postgres/objects.go:467`). The blended-score field each writes into `Metadata["score"]` uses a third and fourth formula respectively. Parity work that ports the missing methods without first pinning a metric contract will produce two "green" drivers that rank differently.

## 1. The SQLite pipeline Postgres must match

### 1.1 Orchestration layer — already driver-agnostic

Hybrid search is two-leg RRF plus a graph-aware reranker, implemented entirely above the driver:

- `Service.HybridSearchExplainFilteredWithDiagnostics` (`internal/service/service.go:1758`) runs an FTS leg and a vector leg concurrently, fuses with reciprocal-rank (k=60 default, per-leg weights), then reranks via `internal/ranking` using edge signals. RRF consumes **rank order only** — it never inspects driver scores — so the fusion is portable by construction.
- Query understanding is pure Go: `DetectQueryMode` (`internal/search/query_mode.go:66`), `DecomposeQuery` (`internal/search/query_decompose.go`), alias expansion (`internal/search/alias_expand.go`).
- `BlendVectors` (`internal/search/vector_blend.go:7-35`) is pure float math with L2 normalization, used for session-context blending at `cmd/ctxt/cmd/find.go:391` (alpha 0.85). No SQL assumptions.
- The filter-DSL compiler is already dialect-aware: `CompileFor(DialectPostgres, …)` rebinds `?`→`$N` (`internal/search/compiler.go:44-56`) and switches `json_each`→`jsonb_array_elements` for tags (`compiler.go:129-135`). Only `similar==` hard-rejects Postgres (`compiler.go:201-210`) — a deliberate stub awaiting an FTS index, with the dialect plumbed from `Driver.SQLDialect()` (`internal/search/dialect.go:29-34`, `internal/storage/postgres/driver.go:118`).

So the portability boundary is clean: everything above `ObjectStore`/`VectorStore` ports for free. The gap is entirely inside the driver.

### 1.2 FTS half — FTS5 external-content table over a projected body

- Index: `objects_fts` FTS5 virtual table indexing a single `projected_fts_body` column, external-content against `objects` (`internal/storage/sqlite/migrations.go:412-418`, migration 023).
- Maintenance: transactional inside `ObjectStore.Create`/`Update` — insert at `sqlite/objects.go:86`, delete+reinsert at `objects.go:349-352`. No triggers; the store owns index consistency.
- Query: `bm25(objects_fts)` relevance, ascending (`sqlite/objects.go:1051-1071`), with type and metadata-facet filters pushed into the JOIN; node-aware variant at `objects.go:1209`.
- Sanitization: `SafeFTSQuery` (`internal/search/safe_fts.go:24`) phrase-quotes each token to neutralize FTS5 operator syntax; injected at every call site (`internal/service/service.go:836, 1773`; `internal/retrieval/retriever.go:93`). This is FTS5-specific by design — its whole job is escaping FTS5 metacharacters.
- Provenance: the FTS index signature hashes tokenizer + projection version + live DDL from `sqlite_master` (`internal/storage/sqlite/index_signatures.go:45-75`) into the `index_signatures` table, driving ADR-070 auto-rebuild semantics.

### 1.3 Vector half — three representations, one live path

SQLite currently carries **three** embedding representations; understanding which is load-bearing matters because Postgres should not mirror the accidents:

| Representation | Created by | Role today |
|---|---|---|
| `object_embeddings` (BLOB, one row/object) | migration 004 | **Live write path** — `upsertEmbedding` (`sqlite/objects.go:790-798`); brute-force scan source (`objects.go:864`) |
| `embeddings` composite key (object_id, model_id, chunk_idx) | migration 032 (`sqlite/migrations/032_embedding_models.sql`) | ADR-071 row-of-record; **backfilled** from legacy by migration 033 (`sqlite/migrations.go:513-…`) but not yet the live write path |
| `vec_objects` vec0 virtual table | migration 020 (`sqlite/migrations/020_vec_objects.sql`) | ANN index, dimension templated via `{DIMENSION}` |

- Dimension is configurable pre-Init via `Driver.SetVectorDimension` (`sqlite/driver.go:92-95`), default 1536 (`driver.go:17`). The vec0 table is created with that dimension baked in.
- Mirror-on-write: `Create`/`Update` upsert into `vec_objects` only when `len(obj.Embeddings) == vecDim` (`sqlite/objects.go:100-101, 366-367`) — a **silent skip** on dimension mismatch, which is itself an undocumented contract the conformance suite should pin.
- Search: `VectorSearch` routes to ANN when the query vector matches `vecDim` (`objects.go:916-919`), else brute-force cosine over `object_embeddings` (`objects.go:972-1029`). The ANN path over-fetches 10× and post-filters type/subtype/pipeline/metadata facets in Go (`objects.go:924-967`); score is mapped `1/(1+L2)` (`objects.go:963-964`), while brute-force reports raw cosine similarity — two score semantics inside one driver already.
- Model registry: `internal/embeddings/registry/registry.go` is thin CRUD over `embedding_models` (Register/List/Get/SetDefault/Deprecate, coverage via `embeddings` counts at `registry.go:171-203`). Two portability notes: (a) it is written with `?` placeholders and takes a raw `*sql.DB`, so **as written it can only run against SQLite** despite the table existing on both backends; (b) `dimension` is a per-model column with a multi-model design intent — plural dimensions are a first-class expectation, which the Postgres `vector(1536)` hardcode directly contradicts.
- Embedding index signature: inputs are (model_id, provider, dimension) (`sqlite/migrations.go:630-641`) — driver-neutral in substance, SQLite-located in code.

## 2. pgvector + tsvector as the analog

### 2.2 Verified current Postgres state

The companion report's claims re-verified, plus color:

- `ObjectStore.VectorSearch` **is** implemented on pgvector: cosine `<=>`, `score = 1 - distance` (`postgres/objects.go:417-489`), against a hardcoded `objects.embedding vector(1536)` column (`migrations.go:29` region). The query vector is **string-interpolated** into the SQL as a `'[...]'::vector` literal (`objects.go:422-432`) rather than bound — safe today because `%g` on float32 cannot emit SQL, but it defeats plan caching and is a fragile pattern to keep.
- `VectorStore` is a stub (`postgres/vectors.go:12-28`), freshly allocated on every `Vectors()` call (`postgres/driver.go:110`), whose comment defers to "docs/plans/P-010" — **a file that does not exist** in `docs/plans/` (stale reference; the plans directory holds P-2xx/P-3xx/P-4xx series).
- `FTSSearch` / `FTSSearchNodeAware` / `VectorSearchNodeAware` are error stubs (`postgres/objects.go:491-503`). There is no tsvector column, no `projected_fts_body` column, nothing to search.
- The ADR-071 mirror created an `embeddings` table with `vector BYTEA` (`migrations.go:274-283`) that nothing reads or writes — yet the seed helper **stamps an `embeddings_<model_id>` index signature** for it (`migrations.go:353-362`), so Postgres records provenance for an index that does not exist. Drift in the provenance system itself.
- No Postgres analog of `ComputeFTSSignature`/`VerifyFTSSignature`; the `index_signatures` table exists write-only.

### 2.2 FTS: tsvector is the right baseline; bm25 engines are not

- **Recommendation: native `tsvector`/`tsquery`** — a `projected_fts_body TEXT` column (closing the §1.4 gap in the companion report) plus a `GENERATED ALWAYS AS (to_tsvector('<config>', projected_fts_body)) STORED` column with a GIN index. Generated-column maintenance replaces SQLite's manual delete+reinsert and cannot drift from the row.
- ParadeDB `pg_search` would give true BM25 and closer score parity, but it is a third-party extension unavailable on most managed Postgres (RDS, Cloud SQL, Azure) — adopting it as baseline would trade the sqlite-vec CGO availability problem for a worse one. Keep it as an optional capability at most.
- Ranking: `ts_rank_cd` ≠ bm25. This is fine **iff** the conformance contract is rank-plausibility (query X ranks doc A above doc B on a fixture corpus), not score equality. RRF upstream already only consumes ranks (§1.1).
- Sanitization: `SafeFTSQuery`'s FTS5 phrase-quoting must not be sent to `to_tsquery`. Postgres gets this nearly free — `websearch_to_tsquery('<config>', raw)` neutralizes user syntax by design and matches SafeFTSQuery's implicit-AND semantics. The clean shape is a dialect-aware sanitizer at the same seam where `CompileFor` already branches, with the driver receiving raw text and applying its own quoting (today callers pre-sanitize with FTS5 rules *before* the driver boundary — `service.go:836` — which bakes the SQLite dialect into the service layer and must move).
- The tsvector regconfig (`'simple'` vs `'english'` stemming) is the tokenizer-analog decision, and — exactly like the FTS5 tokenizer — belongs in the FTS index signature so changing it triggers rebuild.
- `compileSimilar`'s Postgres rejection (`compiler.go:205-207`) becomes `id IN (SELECT id FROM objects WHERE fts @@ websearch_to_tsquery(?, ?))` once the column exists.

### 2.3 Vectors: pgvector fits, but three decisions are forced

**Metric.** sqlite-vec vec0 defaults to L2 (`sqlite/vectors.go:13`); pgvector code uses cosine `<=>` (`postgres/objects.go:467`). Provider embeddings are not uniformly unit-norm (OpenAI yes, Ollama models not guaranteed), and `BlendVectors` normalizes only blended vectors — so L2 and cosine can genuinely rank differently. Pick **cosine as the cross-driver contract**: it is the de-facto semantic-search metric, pgvector already uses it here, and vec0 supports `distance_metric=cosine` in its DDL — one SQLite migration recreating `vec_objects` (index-only data; rebuildable from `object_embeddings`, and ADR-070 signatures exist precisely to drive such rebuilds). Keeping L2 and switching Postgres to `<->` is the smaller diff but enshrines the metric nobody chose deliberately.

**Score mapping.** Today: `1/(1+L2)` (sqlite ANN), raw cosine similarity (sqlite brute-force), `1-cosdist` (postgres). `Metadata["score"]` is API-visible and consumed by dedup (`internal/pipeline/steps/dedup_step.go:48`, `internal/service/duplicates.go:69`) and lint (`internal/lint/checks.go:94`). With cosine as the metric, `1 - cosine_distance` (= cosine similarity for unit vectors) is the natural shared mapping; the conformance suite should assert the field's range and monotonicity, not exact values.

**Dimension and canonical store.** `vector(1536)` hardcoded contradicts both `SetVectorDimension` and the multi-dimension `embedding_models` registry. pgvector constraints that shape the answer: an ANN index (HNSW/IVFFlat) requires a fixed-dimension column (typmod), HNSW indexes cap at 2,000 dims (`halfvec` extends to 4,000), and one column cannot hold mixed dimensions with a typmod. Options:

1. *Parity-first (recommended now):* mirror SQLite exactly — add `SetVectorDimension` to the Postgres driver, make the `objects.embedding` typmod and the ANN index dimension a value applied at migration time (dynamic DDL, exactly like SQLite's `{DIMENSION}` template at `sqlite/migrations/020_vec_objects.sql`). Single active model, single dimension per instance. Smallest diff, matches the live SQLite behavior including its limitations.
2. *Registry-grade (ADR-071 Phase 3+ shape):* the composite `embeddings` table becomes the canonical store with a typmod-less `vector` column (any dimension per row), and per-model ANN indexes are built as **partial expression indexes** — `CREATE INDEX ... USING hnsw ((vector::vector(1536)) vector_cosine_ops) WHERE model_id = '<default>'`. This is where multi-model lands eventually, on both drivers; it should not gate initial parity.

Either way, the dead BYTEA `embeddings.vector` column must be re-typed to pgvector `vector` — a BYTEA blob is useless to every Postgres query path and exists only as a copy-paste of the SQLite BLOB column. And `VectorSearch` should keep the SQLite ANN semantics of over-fetch + post-filter *or* push filters into SQL deliberately: pgvector's index scan applies `WHERE` **after** traversal, so a filtered query can return fewer than `LIMIT` rows even when matches exist (mitigated by iterative index scans in pgvector ≥ 0.8.0). Whichever strategy is chosen, the conformance suite needs a filtered-recall assertion so both drivers exhibit the same behavior under selective filters.

## 3. Parity plan

Ordered to extend the companion report's rollout (§3.5 there); items 1–2 below slot into its step 2, items 3–6 into its step 4 "Vectors/FTS" burn-down.

1. **Unblock the runtime.** Switch CI service and dev compose images to a pgvector-bundled Postgres (`pgvector/pgvector:pg16`). Without this every subsequent step is untestable. (The vestigial `qdrant` service at `docker-compose.dev.yml:57` should be removed or justified in the same pass — a third search engine no code references muddies the story.)
2. **Adopt the versioned migration ledger first, then land vector/FTS schema as its first entries.** Per the companion report §1.6/§3.5.5, mirror SQLite's `schema_version` pattern. The initial versioned steps: `source_key` fix, `profile_id`, `projected_fts_body` + generated tsvector column + GIN index, re-type `embeddings.vector` BYTEA→`vector`, parameterized `objects.embedding` dimension + ANN index. Doing schema through ad-hoc idempotent helpers again would reproduce the exact failure mode that created these gaps.
3. **Pin the metric contract** (cosine, per §2.3) and the score mapping; recreate SQLite `vec_objects` with `distance_metric=cosine`, switch nothing on the Postgres side. Encode as a conformance fixture: small corpus, golden rank order (not scores) required from both drivers.
4. **Implement the missing methods.**
   - `VectorStore` on pgvector (Upsert/Search/Delete/Count) — retiring the stub and the phantom P-010 reference; bind the query vector as a parameter, killing the string-interpolation pattern.
   - `FTSSearch`/`FTSSearchNodeAware` via `websearch_to_tsquery` + `ts_rank_cd`; `VectorSearchNodeAware` mirroring the SQLite post-filter shape (`sqlite/objects.go:1248-…`).
   - Move sanitization behind the driver boundary: replace call-site `SafeFTSQuery(...)` pre-baking with a dialect-dispatched sanitizer next to `CompileFor`, so FTS5 quoting is applied only on the SQLite path.
   - Lift `compileSimilar`'s Postgres rejection.
5. **Port provenance.** Promote signature computation to a driver-neutral package: FTS signature inputs on Postgres = (regconfig, projection version, generated-column expression + index DDL from `pg_get_indexdef`); vector signature adds (index method, ops class, build params: `m`/`ef_construction` or `lists`) to the existing (model_id, provider, dimension). Delete the write-only signature stamping for the nonexistent BYTEA index (`postgres/migrations.go:353-362`) as part of the re-type. Make the registry package dialect-clean (placeholder rebind or driver-mediated queries) so `ctxt embeddings list` works on hosted instances.
6. **Wire capabilities so parity is CI-enforced.** In the `storagetest` suite from the companion report, flip the Postgres wrapper to `FTS: true, Vectors: true` only when steps 2–5 land — until then they render as loud skips. Add the search-specific assertions the companion's list implies but does not spell out:
   - golden rank-order fixture per leg (FTS and vector) — same order both drivers;
   - sanitizer corpus: hyphens, quotes, `NEAR(`, `AND/OR/NOT`, `:` — no driver may error or change semantics on hostile input (regression net for the FTS sanitizer-injection class);
   - filtered-KNN recall: selective filter + limit must return all qualifying neighbors on both drivers;
   - dimension-mismatch contract: define and assert what Create does with wrong-dimension embeddings (SQLite silently skips the ANN mirror today, `sqlite/objects.go:100`) — silent divergence here poisons search coverage invisibly;
   - `Metadata["score"]` present, in-range, rank-consistent.

## 4. Operational deltas for hosted instances

- **Extension availability** is a solved problem on every serious managed target — RDS/Aurora, Cloud SQL, AlloyDB, Azure Flexible Server, Supabase, Neon all ship pgvector — but `CREATE EXTENSION` requires sufficient privilege; some platforms require enabling it via their console/API before `Init` runs. The driver should degrade to a **clear, single** error ("pgvector extension unavailable; enable it or …") rather than a raw migration failure, because this will be the first error every self-hosting operator sees.
- **Index build cost.** HNSW builds are memory-hungry (`maintenance_work_mem`) and slow on large corpora; best practice is bulk-load rows first, create the index after — which interacts with the federation pull worker: a subscriber cold-syncing a large remote corpus should defer ANN index creation until after the initial pull, exactly the rebuild flow ADR-070 signatures already model. IVFFlat is cheaper to build but requires representative data at build time and periodic re-`ANALYZE`/re-list tuning as the corpus grows.
- **Planner hygiene.** Autovacuum/`ANALYZE` on `objects` and `embeddings` matters once corpora federate in at volume; `hnsw.ef_search` is a session-level recall/latency dial worth exposing as instance config rather than hardcoding.
- **Dimension ceilings.** HNSW/IVFFlat index ≤ 2,000 dims (`halfvec` to 4,000). The embedding-model registry should validate registered dimensions against the backend's indexable ceiling — a model that SQLite happily brute-forces would be un-indexable on Postgres, and that asymmetry should fail loudly at `Register` time, not at first query.

## Verdict

The expensive part of this parity gap is already paid: the orchestration layer (RRF, blending, query understanding, reranking) is driver-agnostic, the compiler has a dialect seam, both projection concepts (`projected_fts_body`, embedding-model registry) are schema-portable, and Postgres even has a working pgvector query path. What is missing is not architecture but follow-through — one tsvector column, one honest vector schema in place of a dead BYTEA table, four method implementations, and the discipline of a shared metric contract. The two traps are subtle rather than large: a CI Postgres image that cannot load pgvector, and two drivers that would rank the same corpus differently while both passing method-level tests. Both are exactly the class of drift the conformance suite's capability flags exist to catch — provided the search assertions test rank order and recall, not merely that methods stop returning "not implemented".
