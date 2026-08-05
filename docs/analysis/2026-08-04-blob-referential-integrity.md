# Blob/DB Referential Integrity — Failure Modes and Reconciliation Design

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Companion:** [2026-08-04-storage-architecture-pov.md](2026-08-04-storage-architecture-pov.md) (risk item: "Blob/DB referential integrity")

The architecture POV flagged that `blob://` refs cross the transaction boundary. This document traces the actual write and delete paths, enumerates the crash-point outcomes, and specifies a mark-and-sweep reconciliation job. The headline findings, in decreasing order of surprise:

1. **The only "orphan blob" cleanup in the tree is a no-op against a phantom table.** `dpkms housekeeping` runs `DELETE FROM blobs WHERE object_id NOT IN (SELECT id FROM objects)` (`cmd/dpkms/cmd/housekeeping.go:336`) — no `blobs` table exists in any migration (`internal/storage/sqlite/migrations/`). The statement fails, the error is swallowed as a warning (`housekeeping.go:338`), and the real blob store (local FS or S3) is never touched.
2. **Object deletion never deletes blobs.** `storage.BlobStore.Delete` (`internal/storage/storage.go:130`) has zero callers outside the backend implementations and their tests. Every deleted externalized object orphans its blob, deterministically.
3. **Nothing dereferences `blob://` on the read path.** `blob.Resolve` (`internal/storage/blob/resolve.go:29`) also has zero callers outside its package. Consumers receive the literal ref string, so dangling refs are currently asymptomatic — they will surface only when a resolver ships, at which point today's integrity debt becomes user-visible.

The good news: content-addressed keys make the write path largely self-healing under retry, and a 1:1 row↔blob cardinality makes reconciliation simple and safe.

## Write path

Ingestion is queue-mediated. The full raw content travels in the job row (`jobs.payload`, `internal/storage/sqlite/jobs.go:33`), which matters for recovery: an orphaned blob's content survives in the jobs table until history pruning (30 days, `housekeeping.go:355`).

1. `Service.Analyze` enqueues an `ingest:<type>` job (`internal/service/service.go:233-238`). The raw-mode path (`service.go:154-188`) bypasses the pipeline entirely and stores content inline — oversized raw objects are never externalized (an inverse, non-integrity concern).
2. A worker acquires the job via atomic `UPDATE … RETURNING` (`internal/storage/sqlite/jobs.go:135`) and runs pipeline steps (`internal/jobs/worker.go:157-170`).
3. The `externalize_content` step fires when `len(RawContent)` exceeds the threshold (default 65536, `internal/config/config.go:790`). It computes `key = ContentHash(RawContent, Source)` (`internal/pipeline/steps/externalize.go:43`; SHA-256 over normalized content + source, `internal/storageutil/content_hash.go:12`), calls `store.Put` (`externalize.go:45`), records `blob_key` in metadata (`externalize.go:57`), and rewrites `RawContent = "blob://" + hash` (`externalize.go:61`).
4. Back in `process`, `ContentHash` is **recomputed over the ref string** (`worker.go:253`) — the DB dedup hash is the hash of `blob://<key>`, not of the content. Because the blob key is itself deterministic from (content, source), dedup still converges: same content + source → same key → same ref → same row hash.
5. The object row commits via `Objects().Create` (`worker.go:272`) or `Reinforce` (`worker.go:259`, dedup hit). Only then does `queue.Complete` run (`worker.go:349`).

The blob `Put` and the row `INSERT` are separated by whatever pipeline steps follow externalization — potentially LLM calls taking seconds to minutes. There is no transaction spanning them, and there cannot be one across two stores; the design question is which side wins each race.

Two mitigating properties fall out of content addressing:

- **Retry is idempotent.** A re-run re-`Put`s the same bytes under the same key — an overwrite, not a duplicate. "Double-write" is a cost blip (one extra S3 PUT), never a correctness issue.
- **Cardinality is 1:1.** `objects.content_hash` is unique (dedup via `GetByContentHash`/`Reinforce`, `worker.go:257-268`), and one hash maps to one blob key. A blob is referenced by at most one live row, so deleting a row's blob can never break another row. This is what makes both eager deletion and sweeping safe.

Recovery machinery already in place: jobs stuck `running` after a crash are requeued by `recoverStaleLoop` (`worker.go:471-479`); transient step failures go through `queue.Retry` with a `MaxRetries` cap (`internal/jobs/queue.go:65-75`); `PermanentError` fails the job immediately (`worker.go:238-243`).

## Delete path

- `Service.DeleteObject` (`internal/service/service.go:409-421`): deletes edges, then hard-deletes the row (`internal/storage/sqlite/objects.go:374-384`). No blob deletion.
- `dpkms housekeeping prune` (`cmd/dpkms/cmd/housekeeping.go:241`): same `Objects().Delete`, same gap.
- The full-maintenance "orphaned blobs" step (`housekeeping.go:333-347`) targets the nonexistent `blobs` table, as noted above.

So the deletion *ordering* risk from the POV does not materialize as dangling refs — the code never gets that far. The delete path produces **orphans, always**. Dangling refs have three real sources today:

1. **Non-atomic local `Put`.** `local.Store.Put` does `os.Create` + `io.Copy` directly at the final path with no temp-file rename and no fsync (`internal/storage/blob/local/store.go:44-52`). A crash mid-copy leaves a truncated file *under the final key*. If the job then survives (or the row already committed on a prior attempt), the ref points at corrupt bytes — worse than dangling, because `Exists` returns true (`local/store.go:89-98`) and a sweep sees a healthy blob.
2. **Backup/restore asymmetry.** Backup tars local blobs only, optionally (`internal/service/backup.go:145-176`); S3 blobs are never captured. Restore writes blobs next to the DB (`internal/service/restore.go:110-123`). A DB-only restore, an archive made with blobs excluded, or a restore under an S3 config yields rows whose refs resolve nowhere.
3. **Content replacement on reinforce.** `Reinforce` overwrites `raw_content` when the incoming draft carries content (`objects.go:427-434`). If the blob threshold is raised (or the externalize step is removed from a pipeline) and the same content is re-ingested, the inline body replaces the `blob://` ref — silently orphaning the blob. The reverse (inline → ref) is benign.

One more sharp edge for any future sweeper: `s3.Store.Exists` swallows all errors and reports `false` (`internal/storage/blob/s3/store.go:165-175`). A transient network failure is indistinguishable from a missing blob. Reconciliation must never treat a single `Exists` probe as proof of absence.

## Failure-mode matrix

| # | Fault point | Blob state | DB row | Outcome | Durable? |
|---|-------------|-----------|--------|---------|----------|
| 1 | Crash mid-`Put`, local backend (`local/store.go:50`) | Truncated file at final key | none yet | Stale-job recovery re-runs, overwrite repairs. If retries exhaust: **corrupt orphan**. If row committed on an earlier attempt: **ref to corrupt blob**, undetectable by `Exists` | on retry exhaustion |
| 2 | Crash between blob `Put` and row `Create` (`externalize.go:45` → `worker.go:272`) | written | none | Job stuck `running` → `RecoverStale` (`worker.go:471`) requeues → idempotent re-`Put` → row commits. Converges | no |
| 3 | Permanent step failure after externalization (`worker.go:238-243`) | written | none | Job `failed`; **orphan blob**. Content recoverable from `jobs.payload` for 30 days (`housekeeping.go:355`) | **yes** |
| 4 | Transient step failure after externalization → retry (`worker.go:245`) | written twice (same key) | committed on success | Benign double-write; extra S3 PUT cost only | no |
| 5 | Retry exhaustion after externalization (`queue.go:71-73` → `worker.go:246`) | written | none | **Orphan blob**, job `failed` | **yes** |
| 6 | `DeleteObject` / `prune` on externalized object (`service.go:414`, `housekeeping.go:241`) | untouched | deleted | **Orphan blob** — guaranteed, no crash needed | **yes** |
| 7 | Threshold raised, same content re-ingested (`objects.go:427-434`) | untouched | ref replaced by inline body | **Orphan blob**, silent | **yes** |
| 8 | Blob dir loss / partial restore / external S3 lifecycle (`restore.go:110-123`, `backup.go:145`) | missing | ref intact | **Dangling ref** — asymptomatic today (no resolver), poisons future reads; `text_content` retains extracted text as partial fallback | **yes** |
| 9 | Crash between data `Put` and meta `Put` (both backends write two objects: `local/store.go:58`, `s3/store.go:109`) | data without `.meta.json` | any | Benign — `Get` treats meta as optional (`local/store.go:73-76`, `s3/store.go:139-142`) | no |

Rows 2 and 4 are the crash windows the POV worried about, and content addressing already neutralizes them. The durable leaks are rows 3, 5, 6, 7 (orphans — pure storage waste, unbounded over time) and rows 1 and 8 (corrupt/dangling refs — data loss the moment a resolver ships). Orphans need a sweeper; dangling refs need detection plus the local-`Put` atomicity fix at the source.

## Reconciliation: mark-and-sweep design

### Invariant

A blob key `k` is **live** iff some `objects` row has `raw_content = 'blob://' || k`. Because of the 1:1 cardinality argument above, this is exact — no refcounting, no multi-owner ambiguity. `metadata.blob_key` (`externalize.go:57`) is a secondary witness; the sweep should honor the union of both to be conservative against future writers.

### Algorithm

Single pass per cycle, both directions:

1. **Mark.** Stream `SELECT id, substr(raw_content, 8) FROM objects WHERE raw_content LIKE 'blob://%'` into an in-memory set. At PKMS scale (10⁴–10⁵ externalized objects × 64-byte hex keys) this is a few MB. Also collect keys from `metadata` JSON where present.
2. **Sweep blobs → orphans.** `BlobStore.List(ctx, "")` (`storage.go:132`), diff against the live set. For each candidate orphan, apply the guards below, then `BlobStore.Delete` (removes data + `.meta.json`, `local/store.go:81-87`, `s3/store.go:147-163`).
3. **Sweep refs → dangling.** For each live key, `BlobStore.Exists`. Missing → **report, never repair**: append an `AuditStore` entry (`storage.go:118`) against the object, emit a bus event (4-segment topic per the kit convention, e.g. `maintenance.blob.ref.orphaned` / `maintenance.blob.ref.dangling`), and count it in the run summary. The operator decides between re-ingest (content may still be in `jobs.payload` or `text_content`) and deletion.

### Safety guards (in order of importance)

- **Age grace.** Never delete a blob younger than a grace window (default 24h, config-driven), using `BlobInfo.UpdatedAt` from `List`. This is the crash-window guard: a blob `Put` by an in-flight pipeline whose row hasn't committed yet (matrix rows 2–5) is always younger than the stale-recovery + retry horizon (minutes). 24h gives three orders of magnitude of margin.
- **Pending-work cross-check.** Skip deletion while any `pending`/`running` job exists (`Jobs().List` with status filter, pattern as in `internal/service/service_inbox.go:158`). Cheap, and closes the theoretical race where a long-running LLM step outlives the grace window.
- **Absence needs two witnesses.** For dangling-ref detection on S3, retry `Exists` with backoff before reporting, since `s3.Store.Exists` folds transport errors into `false` (`s3/store.go:171-173`). Better: fix `Exists` to return the error, and have the sweep treat `error ≠ absent`.
- **Quarantine before hard delete.** First N cycles (or a `--quarantine` mode) move orphans under a `quarantine/` prefix instead of deleting (`Put` copy + `Delete`, or rename on local FS). Matches the data-safety posture of the rest of the codebase (backup-before-destroy).
- **Dry-run default in the CLI.** Mirror the `housekeeping` `--dry-run` convention (`housekeeping.go:335-346`).

### Fitting the existing job machinery

The queue is pipeline-shaped: `WorkerPool.process` resolves `job.Pipeline` unconditionally (`worker.go:174-177`) and the enqueue-time validator rejects jobs without a registered pipeline (`queue.go:38-46`). Shoehorning a maintenance job through it would mean either a fake pipeline or a job-type dispatch branch in the worker. Neither is warranted: the codebase already has a precedent for exactly this shape of work — the resurfacing job, a ticker-driven loop inside `dpkms serve` with a `RunOnce` escape hatch for tests and one-shot invocation (`internal/resurfacing/job.go:38-66`). Recommended structure:

- `internal/maintenance/blobreconcile.go` — `Job` with `Run(ctx)` (ticker, default interval 24h, disabled when `Blobs()` is the stub backend) and `RunOnce(ctx) (Report, error)`.
- Wire into `serve.go` next to the resurfacing job, so the sweep runs inside the single-writer process — no second writer on the SQLite file, consistent with the behavioral write-ownership contract the POV calls the system's most fragile.
- Replace the phantom-table step in `runFull` (`housekeeping.go:333-347`) with a call to `RunOnce`, giving operators the manual `dpkms housekeeping` entry point with real numbers in `housekeepingStats.OrphanBlobs` (`housekeeping.go:259`).

### Backend cost profile

- **Local FS:** `List` is a full `filepath.Walk` (`local/store.go:100-121`) — O(blob count) stats, no I/O amplification worth worrying about at this scale. Fine as-is.
- **S3-compatible (R2/B2/MinIO/Garage):** two blockers before the sweep is trustworthy:
  1. `List` is **unpaginated** — a single `ListObjectsV2` call returns at most 1000 keys (`s3/store.go:187-190`) with no continuation-token loop. Beyond ~500 blobs (data + `.meta.json` pairs double the key count) the sweep would silently see a partial listing and could mislabel unlisted-but-live... nothing (it diffs blob→row, so a partial listing only *misses orphans*, it cannot cause a wrong delete — but dangling detection via `List` would false-positive; using per-key `Exists` for direction 3 avoids that). Pagination is still required for completeness.
  2. Cost: LIST is ~$0.005 per 1000 requests on S3 (free on R2/B2). 100k blobs ≈ 200k keys ≈ 200 LIST pages per cycle ≈ $0.001/day at daily cadence — negligible. Per-key `HeadObject` for dangling detection is the larger number (one per live ref), still ~$0.0004 per 100k on S3 and free-tier territory elsewhere. No cost argument against daily runs at PKMS scale; at fleet scale, direction 3 can drop to weekly.

### Fix at the source, not just the sweep

The sweeper is the backstop; two point fixes shrink what it has to catch:

1. **Eager blob delete on object delete.** `Service.DeleteObject` and the prune path should best-effort `Blobs().Delete(key)` *after* the row delete succeeds (row first, blob second: a crash between the two yields an orphan the sweep collects; the reverse order would manufacture dangling refs). Safe because of the 1:1 cardinality. Converts matrix row 6 from "guaranteed leak" to "leak only on crash."
2. **Atomic local `Put`.** Write to `<path>.tmp`, fsync, rename (`local/store.go:38-63`). Eliminates matrix row 1's corrupt-blob-under-final-key state, which no sweep can detect without re-hashing content. (A `--verify` sweep mode that re-hashes each blob against its key is a cheap optional deep check precisely because keys are content hashes.)

## Verdict

The transaction-boundary crash the POV flagged is the part the system already survives — content-addressed keys plus stale-job recovery make the write path convergent. What actually leaks is the mundane half: deletes that never had a blob-side counterpart, a housekeeping step that cleans a table that doesn't exist, and a local `Put` that isn't atomic. The reconciliation job is worth building as specified — 1:1 cardinality makes it exact and safe — but the eager-delete and atomic-`Put` fixes are smaller, land first, and remove the two failure modes a sweep handles worst.
