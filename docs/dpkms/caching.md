# Caching

There is no general caching layer: no `cache:` config key, no `cache` command,
and no cache for LLM, embedding or OCR results. What exists is a set of small,
purpose-specific mechanisms:

- **Ingest dedup.** Workers hash incoming content (`ContentHash`); a repeat
  capture reinforces the existing object instead of reprocessing it
  (`internal/jobs/worker.go`).
- **Registry manifests.** Fetched manifests are stored in the database with
  their ETag and last-fetched time, and refetched with `If-None-Match`
  (`internal/storage` `RegistryCache`, `internal/steps/discovery.go`). See
  [registries.md](registries.md).
- **In-process caches.** Ambient capture dedup (LRU, 60 s window), lateral
  page-shape recipes and JIT proposals, and browser-extension cookies (1 h);
  all in memory, lost on restart.

Related: [embeddings.md](embeddings.md), [storage.md](storage.md).
