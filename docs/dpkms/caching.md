# Caching

This document describes the caching architecture within ContextHelp. Caching is optional but strongly recommended for reducing latency, minimizing API costs, and ensuring predictable pipeline performance. Caching is implemented using **decorator patterns**, ensuring that pipelines, LLM clients, embedding providers, and registry resolvers remain unaware of caching logic.

---

## Goals

Caching exists to:

- Reduce repeated calls to expensive operations (LLMs, embeddings, OCR).
- Improve throughput for pipelines that reprocess similar or identical content.
- Support offline or low-connectivity operation where cached results substitute for upstream providers.
- Enable efficient multi-pass pipeline execution (e.g., section splitting → summary → tags → reranking).
- Allow registry-based or plugin-provided caching strategies without coupling them to pipeline internals.

Caching must never:

- Mutate pipeline behavior in ways that alter correctness.
- Prevent re-execution when data changes.
- Override user preferences.

---

## Caching Targets

Caching may apply to the following components:

### 1. Embedding Providers
Embedding generation is one of the most expensive and frequently repeated operations.

Cache key:
```
embedding:<model>:<content-hash>
```

### 2. LLM Generations
Summaries, tags, extraction steps, and reasoning layers can reuse results when inputs do not change.

Cache key:
```
llm:<model>:<prompt-hash>
```

### 3. OCR / Audio Transcription
Multimodal pipelines often revisit the same image or audio frames.

Cache key:
```
ocr:<tool>:<image-hash>
asr:<tool>:<audio-hash>
```

### 4. Registry Data
Registries may supply labels, translations, expansions, weights, or taxonomies.

Cache key:
```
registry:<registry-id>:<resource>:<version>
```

Registry cache invalidates when:
- The registry publishes a new version.
- The user explicitly clears cache.
- The registry endpoint returns a `version` or `etag` mismatch.

### 5. Plugin-Defined Caches
Plugins may introduce:
- Translation caches
- Localization caches
- Custom model inference caches
- Rendering or analysis caches

Plugins must follow:
- Namespaced cache keys (`plugin:<name>:<key>`)
- Declarative invalidation rules (TTL, manual purge, version stamps)

---

## Cache Placement in the Architecture

Caching is implemented as a **decorator layer** around provider interfaces.

Example:

```go
type EmbeddingClient interface {
    Embed(ctx context.Context, input string) ([]float32, error)
}

type CachingEmbeddingClient struct {
    inner EmbeddingClient
    cache Cache
}
```

Pipeline code calls:

```go
emb, _ := embeddingClient.Embed(ctx, text)
```

It does **not** know or care that caching exists.

This preserves the Single Responsibility Principle:
- Providers provide results.
- Decorators provide caching.
- Pipelines orchestrate calls.

---

## Cache Storage Backends

Cache backends are pluggable and configured in `config.yaml`.

Recommended default:
- **SQLite** or **BadgerDB** local key-value store.

Supported backends (initially):
- Memory (ephemeral)
- File-based (JSON or binary)
- SQLite
- Plugin-defined stores (Redis, Postgres, external cache services)

### Example configuration

```yaml
cache:
  type: "sqlite"
  path: "./data/cache.db"
  ttl: "30d"
  maxSizeMB: 500
```

---

## Cache Keys

Cache keys must be:
- Deterministic
- Canonical
- Order-independent when relevant
- Version-stamped (include versions of models, plugins, registry sources)

Example hash components:
- Pipeline version
- Model version
- Registry version
- Normalized input

---

## Cache Invalidation

Caching requires explicit, predictable invalidation.

### Automatic invalidation
- Version mismatch (model, pipeline, registry)
- TTL expiration
- Size-based eviction (LRU recommended)

### Manual invalidation
CLI:

```bash
ch cache clear          # Clear all cache
ch cache clear llm      # Clear only LLM cache
ch cache clear embedding
```

API:

- `DELETE /cache`
- `DELETE /cache/{namespace}`

Plugin hooks may also trigger invalidation events.

---

## Cache Safety

To maintain correctness:

1. **Never cache errors.**
   Only successful results should be cached.

2. **Never cache partial pipeline output.**
   Cached data must represent a complete step or complete call.

3. **Cache is a performance layer, not a persistence layer.**
   Bookmarks should never rely on cached data as their source of truth.

4. **Cache may be disabled entirely.**

```bash
ch analyze --no-cache
```

---

## Pipeline Interaction with Caching

Pipelines do not perform caching directly.

Pipeline steps may be cached indirectly when:
- The underlying provider (LLM, OCR, embedding) is wrapped.
- A registry lookup is cached.
- A plugin intercepts step input/output to memoize it.

Steps in a pipeline should be idempotent and safe to repeat.
Cache layers should ensure:
- consistent keys,
- well-scoped TTL,
- plugin-defined cache layering.

---

## Plugin Integration

Plugins may:

- Add new cache namespaces
- Register custom cache decorators
- Introduce alternative caching stores
- Add TTL or eviction strategies
- Override or disable caching per pipeline or per provider

Example plugin config:

```yaml
plugins:
  - name: "i18n"
    config:
      caching:
        translations:
          ttl: "90d"
          namespace: "translation"
```

Plugins must:

- Use namespaced keys
- Handle invalidation responsibly
- Avoid storing sensitive user input outside local disk unless user opts in

---

## Future Extensions

- Multi-level caching (RAM → local disk → remote)
- Fingerprinting large multimedia objects efficiently
- Agent-level caching policies (agents may require different TTLs or models)
- Cross-device caching via encrypted sync

---

## Summary

ContextHelp uses caching as a **transparent optimization layer** built with decorator patterns, allowing pipelines and providers to remain pure and stateless. Caching improves performance, reduces costs, and enables offline resilience without altering the correctness or determinism of pipeline logic.

Caching is always:

- Optional
- Configurable
- Replaceable
- Extendable by plugins

And strictly **non-destructive** to user data or final bookmark outputs.