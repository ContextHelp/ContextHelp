# ADR-005 – Adopt Decorator Pattern for AI Providers (Caching & Extensibility)

> **Status:** Accepted
> **Date:** 2025-10-05
> **Author:** @jadb
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp relies on multiple AI-powered components—including LLMs, embedding generators, OCR engines, and classification models—to analyze content during ingestion pipelines. These calls are:

- **expensive** (financially, if using commercial APIs)
- **slow** (LLM latency, network variability)
- **not guaranteed deterministic**
- **not inherently cache-aware**

In the early prototype, caching logic was implemented *inside* specific AI client implementations (e.g., embedding provider, LLM wrapper). This led to:

- duplicated caching code across clients
- tight coupling between AI logic and storage/cache layers
- difficulty instrumenting or testing caching behavior independently
- inability for plugins to wrap custom providers
- challenges replacing AI providers without rewriting caching logic
- lack of observability hooks around cache hits/misses

Further, ContextHelp’s pipeline architecture requires:

- retry-safe behavior for ingestion jobs
- stable, deterministic results on reprocessing
- efficient batch AI operations
- optional offline/local AI integrations (LLama.cpp, Tesseract, etc.)
- plugin-defined AI providers

To satisfy these needs, caching must be **transparent**, **consistent**, and **orthogonal** to business logic. A structural separation is required.

Subsystems impacted:

- `pipelines` (all AI-consuming steps)
- `integrations/ai` (LLM, embedding, OCR client interfaces)
- caching layer (SQLite, memory, file-system-backed)
- plugin system (provider registration & wrapping)

The goal is to improve performance, reliability, testability, and extensibility across the engine.

---

## Decision

We will adopt the **Decorator Pattern** for all AI providers, implementing caching, batching, logging, metrics, and retries as *decorators* that wrap underlying provider implementations.

All pipeline and application code will depend on **interfaces**, and decorators will extend behavior without modifying the core providers.

---

## Rationale

### Alternatives Considered

#### 1. **Caching directly inside AI clients**
Rejected because:
- leads to duplicated logic
- violates single responsibility
- makes plugins difficult to integrate
- prevents layering additional concerns (analytics, tracing, batching)

#### 2. **Global caching middleware**
Rejected because:
- too coarse-grained
- cannot differentiate providers or operations
- complicates plugin injection
- harder to test specific provider behaviors

#### 3. **Pipeline-level caching**
Rejected because:
- pipelines shouldn’t know how or when AI computations are cached
- caching logic would leak into business logic
- prevents consistent cross-pipeline caching (e.g., embedding reuse)

### Benefits of the Decorator Pattern

- **Clean separation of responsibilities**
  Providers generate results; decorators modify behavior.

- **Uniform caching across all AI providers**
  Embeddings, LLM calls, OCR, classification—all follow the same pattern.

- **Better testability**
  Mocking core providers is trivial; caching can be tested independently.

- **Plugin extensibility**
  Plugins can wrap:
  - custom LLMs
  - vector engines
  - OCR pipelines
  without touching core logic.

- **Observability / Metrics**
  Decorators can emit:
  - latency metrics
  - cache hit/miss events
  - retry counts

- **Recoverability**
  Pipeline retries automatically benefit from cached results.

### Risks / Drawbacks

- Slight increase in implementation complexity
- Must define consistent cache keys across providers
- A misconfigured decorator stack may unintentionally double-wrap providers

---

## Consequences

### Positive
- Faster ingestion with predictable latency
- Lower operating costs when using remote AI APIs
- Clear separation between AI logic, caching, and pipeline steps
- Simplified testing and plugin development
- Deterministic reprocessing across job retries
- Improved observability and debugging

### Negative
- Additional wrapper layers increase call depth
- Requires well-defined provider interfaces
- Cache invalidation rules must be carefully maintained
- Increased need for good documentation for plugin authors

### Neutral / Considerations
- Caching should be optional and configurable
- Decorators must support polymorphic config loading (ties into ADR-051)
- Must be careful about memory usage when using in-memory caches

---

## Implementation Notes

- Introduce provider interfaces:
  - `EmbeddingClient`
  - `LLMClient`
  - `OCRClient`
  - `ClassifierClient`

- Implement decorators:
  - `CachingEmbeddingClient`
  - `CachingLLMClient`
  - `RetryingLLMClient`
  - `TracingLLMClient` (optional)

- Caches may use:
  - SQLite tables
  - in-memory cache
  - local file hashes

- The dependency injection graph will look like:

```
[ Pipeline Step ]
        ↓
[ LLM Interface ]
        ↓
[ CachingDecorator ]
        ↓
[ RetryDecorator ]
        ↓
[ Provider Implementation (e.g. OpenAI) ]
```

- Add integration tests ensuring decorators are composable in any order.
- Cache key derivation must use:
  - provider name
  - model name
  - input parameters
  - normalized prompts

- Ensure providers are registered in a global provider registry so decorators can wrap them transparently.

---

## References

- ADR-004 (Pipeline Architecture)
- ADR-003 (Separation of Write/Read Paths)
- ADR-012 (Plugin Extensibility)
- Decorator Pattern – GoF Design Patterns
- Microsoft Kernel Memory’s `CachedEmbeddingGenerator` design
- https://refactoring.guru/design-patterns/decorator
- https://github.com/openai/openai-cookbook (LLM abstraction patterns)

---