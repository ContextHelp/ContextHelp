# Ingestion Job Pipeline Offload-ability Map

**Date:** 2026-11-22
**Scope:** `internal/jobs/` (`queue.go`, `worker.go`, `tasks.go`, `topics.go`, `reprocess.go`, `reproject.go`, `release_watcher.go`, `race_test.go`) and `internal/pipeline/builtins/`

## 1. Summary

Queued work in `dpkms` splits into two families inside `WorkerPool` (`internal/jobs/worker.go`, `internal/jobs/tasks.go`):

1. **Ingest pipeline jobs** (`ingest:*`, plus `:nofanout` variants): execute in two cleanly separable stages inside `WorkerPool.process`:
   - **Stage 1 — Draft computation (`processWithHops`)**: decodes `job.Payload` (`""` or `base64`), builds an in-memory `*storage.KnowledgeObject` draft, runs `pipe.Steps` (and any `metadata["next_pipeline"]` hops up to `maxHops`), applies ambient metadata, and computes `ContentHash`.
   - **Stage 2 — Store persistence (`process` post-draft)**: handles source-key updates, profile-scoped content-hash dedup/reinforcement, non-text refusal, `Objects().Create` (with `ErrAlreadyExists` race settlement), `blob.Ensure`, `putVectors`, `EnsureMentionEntities`, `MentionEdges`, `fanOutItems`, job completion, and post-ingest `FanOutFunc`.
2. **Task jobs** (`handlers` map in `tasks.go`): lease-backed long-running maintenance tasks (`object:reprocess`, `projection:reproject`, `embeddings:migrate`) plus the `ReleaseWatcher` poller.

**Core finding:** A containerized or remote pod cannot open the host's local SQLite database directly, and does not need to. By placing the offload boundary right between **Stage 1** and **Stage 2** (`PipelineExecutor` returning `*storage.KnowledgeObject`), Stage 1 runs statelessly on an ephemeral pod (`Job` JSON in → `KnowledgeObject` draft JSON out) while the host `dpkms` remains the single authoritative writer to SQLite/Postgres, blob store, vector index, mention graph, and job queue.

## 2. Job I/O Contract and Classification

| Job type / component | Reads | Writes | Classification | Notes |
|---|---|---|---|---|
| `ingest:text` (and `:nofanout`) | `job.Payload`, user hints/mentions/profile/metadata | Draft `KnowledgeObject` → Stage 2 DB/blobs/vectors/edges | **offloadable** | Pure payload-in / draft-out in Stage 1; host persists Stage 2. Break-even depends on step latency (fast heuristic text is cheaper locally). |
| `ingest:url` (and `:nofanout`) | URL in `job.Payload`, outbound HTTP fetch (`url_fetcher`, `html_cleaner`, `github_repo_fetcher`) | Draft `KnowledgeObject` → Stage 2 DB | **offloadable** | Offloadable when pipeline uses HTTP fetchers. Pipelines invoking local `ibr_fetcher` stay **local-only**. |
| `ingest:document` | Document bytes (`PayloadBase64`) or text (`pdf_extractor`, `office_extractor`, `table_extractor`) | Draft `KnowledgeObject` → Stage 2 DB + binary blob | **offloadable-with-changes** | Offloadable when payload carries document bytes (not a host filesystem path via `filereader`) and host runs `steps.ExternalizeBinary` after draft returns. |
| `ingest:image` | Image bytes (`PayloadBase64`), OCR / vision provider | Draft `KnowledgeObject` → Stage 2 DB + binary blob | **offloadable-with-changes** | High compute / provider latency makes images strong offload candidates once host runs `ExternalizeBinary` on returned draft. |
| `ingest:audio` | Audio bytes (`PayloadBase64`), transcription / diarization | Draft `KnowledgeObject` → Stage 2 DB + binary blob | **offloadable-with-changes** | Same as `ingest:image`; long wall-clock duration easily amortizes pod startup. |
| `ingest:video` | Video bytes (`PayloadBase64`), `audio_extractor`, `frame_sampler`, `frame_ocr`, `scene_detector` | Draft `KnowledgeObject` → Stage 2 DB + binary blob | **offloadable-with-changes** | Highest CPU/media payoff; requires host-side binary externalization or shared S3/Garage blob store. |
| `ingest:code` | Source payload (`language_detector`, `function_extractor`, `comment_extractor`, `code_block_extractor`) | Draft `KnowledgeObject` → Stage 2 DB | **offloadable** | Pure CPU parsing in Stage 1. |
| `ingest:feed` | Feed URL (`feed_fetcher`, `feed_parser`, `item_enqueuer`) | Draft container object + staged items (`metadata["enqueue_items"]`) → host `fanOutItems` | **offloadable-with-changes** | `item_deduplicator` checks `FeedItemStore.ExistsByGUID` (passthrough when store is nil); host handles `fanOutItems` and dedup on Stage 2. |
| `ingest:feed_item` | Feed item payload / URL | Draft `KnowledgeObject` → Stage 2 DB | **offloadable** | Natural parallel fan-out target when a feed enqueues dozens of items at once. |
| `ingest:github_release` | Release URL / metadata payload | Draft `KnowledgeObject` → Stage 2 DB | **offloadable** | Enqueued by `ReleaseWatcher`; Stage 1 is stateless fetch/parse. |
| `object:reprocess` (`Reprocessor`) | Reads stored object via `ReprocessObjects.GetObject`; calls LLM (`structured_metadata`, `entity_extractor`, `tagger`) | `UpdateObject` + `PersistMentionEdges` | **offloadable-with-changes** | Currently coupled to `ReprocessObjects` inside `Handle`. Could split load/save on host + LLM step on pod later; kept local in v1. |
| `projection:reproject` (`Reprojector`) | Bulk scans stale rows from `ProjectionStore` + `Blobs` | Per-row FTS re-projection + `indexsig.StampFTS` | **local-only** | Pure database/index maintenance tightly coupled to SQLite/Postgres transactions. |
| `embeddings:migrate` | Bulk scans objects lacking target model vectors; calls embedding provider | Bulk vector table inserts/updates | **local-only** | Lease-tracked DB migration cursor; vector writes belong on the DB host. |
| Post-ingest `FanOutFunc` | Stored object ID + graph/object store | Cross-reference edges + audit log | **local-only** | Runs on host after Stage 2 `markCompleted`. |
| `ReleaseWatcher` (`release_watcher.go`) | Local `StateFile` JSON + GitHub REST poll | `Queue.EnqueueIngestJob` + `StateFile` | **local-only** | Lightweight timer loop; the enqueued `ingest:github_release` jobs are offloadable. |

### Pipeline Step Store-Coupling Audit (`internal/pipeline/builtins/builtins.go`)

- **Pure draft transforms (safe in pod):** `typedetector`, `sectioner`, `tagger`, `formatdetector`, `textcleaner`, `html_cleaner`, `entity_extractor`, `timestamp_aligner`, `noop`, `url_fetcher`, `content_type_router`, `feed_fetcher`, `feed_parser`, `item_enqueuer`, `jsonl_parser`, `csv_parser`, `record_validator`, `batch_enqueuer`, `scene_detector`, `timeline_assembler`, `slack_parser`, `discord_parser`, `markdown_parser`, `heading_splitter`, `code_block_extractor`, `language_detector`, `function_extractor`, `comment_extractor`, `table_extractor`, `twitter_archive_parser`, `linkedin_*`, `email_*`, `dependency_enricher`, `graph_extractor`, `structured_metadata`, `c12n_classify`, `github_repo_fetcher`, `github_repo_mapper`, `content_classes`, `ocr_extractor`, `vision_analyzer`, `audio_transcriber`, `speaker_diarizer`, `audio_extractor`, `frame_sampler`, `frame_ocr`, `pdf_extractor`, `office_extractor`.
- **Stateless-with-external-HTTP (`embedding`):** Calls the embedding HTTP provider and populates `draft.Vectors` in memory without touching the database; Stage 2 host persists `draft.Vectors` via `putVectors`.
- **Store-coupled steps handled on host:**
  - `externalize_content`: requires `BlobStore`; in a stateless pod without shared S3/Garage, binary externalization runs on the host (`steps.ExternalizeBinary`) immediately after Stage 1 returns.
  - `dedup`: queries `EmbeddingStore.Search` and writes `AuditStore`; must execute on the host once `draft.Vectors` return from the pod.
  - `item_deduplicator`, `entity_resolver`, `alternative_detector`: already pass through cleanly when their store dependencies are `nil` (as in `builtins.Registry()`).
- **Host-only steps:** `filereader` (reads local host filesystem paths) and `ibr_fetcher` (connects to local IBR browser daemon).

## 3. Idempotency and Retry Semantics

| Mechanism | Current behavior (`internal/jobs/`) | Implication for pod execution |
|---|---|---|
| **Claim & retry** | `AcquireNext` assigns a unique `job.Claim`. Transient errors call `retryOrFail` → `Queue.Requeue(ctx, id, claim)` until `RetryCount >= MaxRetries`. `storage.ErrClaimLost` aborts cleanly without double-settling. | A pod failure (non-zero exit, timeout, lost container, invalid JSON) maps to a transient error and triggers normal `Requeue`. |
| **Permanent errors** | `*pipeline.PermanentError` calls `markFailed` immediately with zero retries. | Pod worker distinguishes permanent pipeline errors (e.g., invalid payload encoding, unknown pipeline, malformed input) in its result envelope so `WorkerPool` does not waste pods retrying unrecoverable jobs. |
| **Crash / lost pod recovery** | `recoverStaleLoop` calls `RecoverStale` every minute using `StaleTimeout` (default `30m`). | Every pod exec and pod lifetime must carry a hard deadline shorter than `StaleTimeout`, and `defer pod destroy` runs on every exit path so a dead pod becomes an immediate retryable failure rather than a stuck job. |
| **Stage 2 idempotency** | `updateBySourceKey`, `GetByContentHash` (profile-scoped), `ErrAlreadyExists` → `settleCreateRace`, and `blob.Ensure` make Stage 2 safe under duplicate execution. | Even if a pod completed Stage 1 right before a host crash and the job is retried later, Stage 2 converges to one stored object with no duplicates. |
| **Task leases (`tasks.go`)** | `defaultTaskLeaseTTL = 30s`, renewed every 10s; `ErrJobCancelled` / `ErrPoolStopped` / `ErrLeaseLost`. | Task jobs stay local in v1; ingest jobs retain their existing `Claim` + `drainCtx` model. |

## 4. Ordering and Race Constraints

From `internal/jobs/race_test.go` and `internal/jobs/worker.go`:

- **SQLite single-writer contention:** Multiple workers sharing SQLite rely on `busy_timeout` and short transactions. Offloading Stage 1 moves the slow CPU/network/media/LLM phase out of the host process while Stage 2 DB commits remain brief and serialized by SQLite's writer lock. High pod fan-out can still burst Stage 2 commits, so pod concurrency must be capped (`max_inflight`) and SQLite WAL + `busy_timeout` remain required.
- **Duplicate content races:** Concurrent workers processing identical content hit `storage.ErrAlreadyExists` on `Objects().Create`, which `settleCreateRace` resolves by reinforcing the winning row. This works identically whether Stage 1 ran locally or in a pod.
- **Parent/child ordering (`fanOutItems`):** Container jobs (`ingest:feed`, batch archives) must complete Stage 2 object creation and enqueue all child jobs before `markCompleted`; failing any child enqueue fails the parent job. Keeping Stage 2 on the host preserves this invariant untouched.
- **Archived pipeline guard:** `pipelineArchived(ctx, job.Pipeline)` runs on the host *before* dispatching Stage 1, so archived pipelines never spawn a pod.

## 5. Natural Offload Granularity

| Granularity | Fit | Trade-offs |
|---|---|---|
| **Per-job Stage 1 (`processWithHops`)** | **Primary unit (v1 default)** | Exact 1:1 mapping to `WorkerPool.process`, `Claim`, `PermanentError`, retries, and cancellation. Zero cross-job failure blast radius. Simple JSON stdin/stdout contract. Pays one pod lifecycle per job unless pod startup is amortized. |
| **Per-batch (micro-batch of N jobs in one pod)** | **Optional future optimization** | Amortizes pod `create`/`destroy` overhead across fast jobs, but requires partial-failure accounting, multi-claim lease management, and batch-formation latency that delays single interactive captures. |
| **Per-run (long-lived worker pod)** | **Already covered by pod daemon runner** | Running `dpkms serve --runner pod` already provides a long-lived remote worker pool; the ephemeral job executor complements it for burst fan-out. |

**Recommendation:** Design the v1 pod job executor around **per-job Stage 1 execution** behind a `PipelineExecutor` interface on `WorkerPool`, with explicit topic allowlisting and bounded fan-out concurrency, and measure exact per-job startup overhead vs break-even job duration in the benchmark phase.
