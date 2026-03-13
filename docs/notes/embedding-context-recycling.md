# Design Note: Embedding Context Recycling

> **Date:** 2026-03-13
> **Status:** Under consideration
> **Source:** Observed in [qmd](https://github.com/tobi/qmd)
> **Applies to:** dPKMS (local inference path)

---

## Observation

qmd keeps loaded embedding model contexts resident in memory and auto-recycles them after 5 minutes of idle. On next request, the context reloads (~1s penalty). This avoids the heavy cost of loading model weights on every request, while also not holding GPU/CPU memory permanently when the system is idle.

## Relevance

When dPKMS runs with a local embedding model (self-hosted path, ADR-001), the model must be loaded to compute embeddings during ingestion and during `balanced`/`best` tier searches (ADR-061). Loading a local GGUF or ONNX model on every request is prohibitively slow (seconds); keeping it resident forever wastes RAM when the system is not actively being used.

This is a narrow but real operational concern for users running dPKMS on a laptop or low-memory machine.

## Proposed Pattern

- Track last-used timestamp per loaded model context
- Background goroutine checks idle time on a configurable interval (default: 1 minute check, 5 minute idle threshold)
- If idle threshold exceeded: unload model, free memory
- On next request that requires the model: reload transparently, log a debug message indicating reload latency
- Configurable via `DPKMS_EMBEDDING_IDLE_TIMEOUT` (default: `5m`, `0` = never unload)

```go
type EmbeddingProvider interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Unload() error  // free model weights from memory
}

// ModelManager wraps EmbeddingProvider with idle-based lifecycle
type ModelManager struct {
    provider    EmbeddingProvider
    lastUsed    time.Time
    idleTimeout time.Duration
    mu          sync.Mutex
}
```

## Trade-offs

| | Keep resident | Idle recycling | Load per request |
|--|--|--|--|
| Memory use | High (always) | Low when idle | Low always |
| Request latency | Lowest | ~1s after idle | High always |
| Complexity | Lowest | Low | Low |

Idle recycling is the right default for a local-first system used intermittently throughout the day.

## Open Questions

- Should recycling apply to reranker models as well, or only embedding models?
- Should the idle timeout be per-model or global?
- Does the reload penalty need to be surfaced to the caller (e.g., `SearchResponse.latency_ms` will spike)?

## Related

- ADR-001 – Local-First and Decentralized
- ADR-061 – Search Quality Tiers (balanced/best tiers require local embeddings)
- ADR-022 – Vector Semantic Search
