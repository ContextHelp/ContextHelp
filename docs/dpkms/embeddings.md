# Embeddings Domain

**Version:** 0.1.0

This document describes the **embedding lifecycle management** in dPKMS, covering vector storage, semantic similarity search, staleness detection, and provider abstraction.

---

## Overview

The Embeddings Domain manages the complete lifecycle of vector embeddings for knowledge objects, enabling semantic search and similarity-based retrieval while maintaining data freshness and provider flexibility.

---

## Responsibilities

### Core Functions

**Embedding Lifecycle:**
- Generate embeddings for new knowledge objects
- Store embeddings with metadata (model, version, timestamp)
- Track embedding staleness and trigger regeneration
- Delete embeddings when objects are removed

**Vector Storage:**
- Abstract vector storage backends
- Index embeddings for fast similarity search
- Support multiple embedding dimensions
- Handle batch operations efficiently

**Semantic Search:**
- Cosine similarity search
- Approximate nearest neighbor (ANN) queries
- Hybrid retrieval combining embeddings with other signals
- Configurable similarity thresholds

**Provider Management:**
- Abstract embedding model providers (OpenAI, local models, custom)
- Handle provider authentication and rate limits
- Support multiple embedding models simultaneously
- Cache embedding requests to reduce API costs

---

## Staleness Detection

Embeddings become stale when:

### Content Change
- Knowledge object content modified
- Summary or sections updated
- Tags or mentions changed
- Related objects updated (configurable)

### Time-Based Refresh
- Configurable TTL per embedding type
- Default: 90 days for static content
- Shorter TTL for dynamic content (e.g., 7 days for URLs)

### Model Version Updates
- New embedding model deployed
- Model version tracked in embedding metadata
- Batch regeneration for model upgrades
- Gradual rollout support

### Manual Triggers
- User-initiated refresh
- Bulk regeneration jobs
- Registry sync triggers
- Pipeline re-execution

---

## Staleness Policies

### Detection Strategy

```yaml
staleness:
  content_change:
    enabled: true
    hash_algorithm: sha256
    track_fields: [content, summary, sections, tags, mentions]

  time_based:
    enabled: true
    default_ttl: 90d
    ttl_by_type:
      url: 7d
      text: 180d
      document: 365d

  model_version:
    enabled: true
    auto_upgrade: false  # Manual approval required
    batch_size: 100
```

### Refresh Priority

**High Priority:**
- Recently accessed objects
- Frequently queried objects
- Objects with high importance score

**Medium Priority:**
- Objects within TTL but approaching expiration
- Objects with minor content changes

**Low Priority:**
- Rarely accessed objects
- Objects with stable content
- Historical archives

---

## Embedding Providers

### OpenAI Provider

**Model:** `text-embedding-3-large`
**Dimensions:** 3072 (default), 1536, 256 (configurable)
**Context Window:** 8,191 tokens
**Rate Limits:** Tier-based (Tier 1: 3M tokens/min)

**Configuration:**
```yaml
embedding_provider:
  type: openai
  model: text-embedding-3-large
  dimensions: 1536  # Optional dimension reduction
  api_key_env: OPENAI_API_KEY
  timeout: 30s
  retry:
    max_attempts: 3
    backoff: exponential
```

**Cost Optimization:**
- Dimension reduction (3072 → 1536 = 50% cost reduction)
- Request batching (up to 2048 inputs per request)
- Caching identical inputs
- Incremental updates only

---

### Local Provider (Future)

**Models:**
- sentence-transformers/all-MiniLM-L6-v2
- BAAI/bge-large-en-v1.5
- Custom fine-tuned models

**Benefits:**
- No API costs
- Complete data privacy
- Offline operation
- Customizable models

**Configuration:**
```yaml
embedding_provider:
  type: local
  model_path: ./models/bge-large-en-v1.5
  device: cpu  # or cuda, mps
  batch_size: 32
```

---

### Custom Provider Plugin

**Interface:**
```go
type EmbeddingProvider interface {
    Name() string
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
    ModelVersion() string
}
```

**Plugin Registration:**
```go
func (p *MyProvider) Register(host HostContext) {
    host.RegisterEmbeddingProvider("my-provider", p)
}
```

---

## Storage Backend

### Vector Store Interface

```go
type VectorStore interface {
    // Store embedding
    Put(ctx context.Context, objectID string, embedding Embedding) error

    // Retrieve embedding
    Get(ctx context.Context, objectID string) (Embedding, error)

    // Similarity search
    Search(ctx context.Context, query []float32, limit int, threshold float32) ([]Result, error)

    // Batch operations
    PutBatch(ctx context.Context, embeddings []Embedding) error

    // Delete embedding
    Delete(ctx context.Context, objectID string) error

    // Check staleness
    IsStale(ctx context.Context, objectID string, policy StalenesPolicy) (bool, error)
}
```

### Embedding Schema

```sql
CREATE TABLE embeddings (
    object_id TEXT PRIMARY KEY,
    embedding BLOB NOT NULL,           -- Vector data (float32 array)
    dimensions INTEGER NOT NULL,        -- Vector dimensionality
    model_name TEXT NOT NULL,           -- e.g., "text-embedding-3-large"
    model_version TEXT NOT NULL,        -- e.g., "v3-2024-01"
    content_hash TEXT NOT NULL,         -- SHA256 of embedded content
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    accessed_at TIMESTAMP,              -- Last similarity search hit
    metadata JSON                       -- Provider-specific metadata
);

CREATE INDEX idx_embeddings_model ON embeddings(model_name, model_version);
CREATE INDEX idx_embeddings_updated_at ON embeddings(updated_at);
```

### Storage Backends

**SQLite (Default):**
- Embeddings stored as BLOB
- Similarity search via full scan (acceptable for <100k objects)
- No native vector index (use external for scale)

**PostgreSQL + pgvector:**
- Native vector type
- IVFFlat or HNSW indexing
- Fast approximate nearest neighbor search
- Scales to millions of vectors

**Dedicated Vector Stores (Plugin):**
- Pinecone, Weaviate, Qdrant, Milvus
- Purpose-built for vector search
- Advanced indexing algorithms
- Cloud or self-hosted options

---

## Incremental Embedding Updates

### Change Detection

**Content Hashing:**
```go
func computeContentHash(obj KnowledgeObject) string {
    canonical := fmt.Sprintf("%s|%s|%s",
        obj.Content,
        strings.Join(obj.Tags, ","),
        strings.Join(obj.Mentions, ","),
    )
    hash := sha256.Sum256([]byte(canonical))
    return hex.EncodeToString(hash[:])
}
```

**Update Decision:**
```go
func needsEmbeddingUpdate(obj KnowledgeObject, existing Embedding) bool {
    // Content changed
    if computeContentHash(obj) != existing.ContentHash {
        return true
    }

    // TTL expired
    if time.Since(existing.UpdatedAt) > getTTL(obj.Type) {
        return true
    }

    // Model upgraded
    if existing.ModelVersion != currentModelVersion() {
        return true
    }

    return false
}
```

---

## Semantic Similarity Search

### Query Flow

1. **Embed query text** using same provider/model
2. **Vector search** against embedding store
3. **Apply threshold** (default: 0.7 cosine similarity)
4. **Retrieve objects** by returned IDs
5. **Rerank** with hybrid signals (metadata, recency, graph)

### Similarity Metrics

**Cosine Similarity:**
```
similarity = (A · B) / (||A|| * ||B||)
```

**Euclidean Distance:**
```
distance = √(Σ(A_i - B_i)²)
```

**Dot Product:**
```
score = A · B
```

### Hybrid Retrieval

Combine embeddings with other signals:

```yaml
hybrid_search:
  weights:
    semantic: 0.6      # Embedding similarity
    keyword: 0.2       # FTS match score
    recency: 0.1       # Time decay
    graph: 0.1         # Entity proximity

  normalization: minmax
  aggregation: weighted_sum
```

---

## Caching Strategy

### Embedding Request Cache

**Cache identical embedding requests:**
```go
type EmbeddingCache interface {
    Get(text string, model string) ([]float32, bool)
    Put(text string, model string, embedding []float32)
}
```

**Cache Key:**
```
SHA256(text + "|" + model_name + "|" + model_version)
```

**TTL:** 30 days (configurable)

**Benefits:**
- Reduce API costs for repeated content
- Faster response for common queries
- Handle provider rate limits

---

## Batch Operations

### Batch Generation

**Group objects for batch embedding:**
```go
func generateEmbeddingsBatch(objects []KnowledgeObject) error {
    texts := extractTextsForEmbedding(objects)
    embeddings, err := provider.EmbedBatch(texts)

    for i, emb := range embeddings {
        store.Put(objects[i].ID, Embedding{
            ObjectID: objects[i].ID,
            Vector: emb,
            ModelName: provider.Name(),
            ModelVersion: provider.Version(),
            ContentHash: computeContentHash(objects[i]),
        })
    }
}
```

**Batch Size:**
- OpenAI: 2048 inputs per request (recommended: 100-500)
- Local models: Depends on GPU memory (16-64)

---

## Monitoring & Metrics

### Key Metrics

**Coverage:**
- Percentage of objects with embeddings
- Percentage of stale embeddings
- Embedding generation rate

**Performance:**
- Embedding generation latency (p50, p95, p99)
- Similarity search latency
- Cache hit rate

**Cost:**
- Total embedding API cost
- Cost per object
- Cost savings from caching

**Quality:**
- Retrieval accuracy (user feedback)
- Similarity score distribution
- False positive rate

---

## Configuration

Example configuration:

```yaml
embeddings:
  enabled: true

  provider:
    type: openai
    model: text-embedding-3-large
    dimensions: 1536
    api_key_env: OPENAI_API_KEY

  storage:
    backend: sqlite  # or postgres, pinecone, etc.

  staleness:
    content_change: true
    time_ttl: 90d
    model_upgrade: false

  batch:
    size: 100
    concurrency: 5

  cache:
    enabled: true
    ttl: 30d
    max_size: 10000

  search:
    similarity_threshold: 0.7
    max_results: 100

  async:
    enabled: true
    queue_priority: normal
```

---

## API Reference

### CLI Commands

```bash
# Generate embeddings for all objects
dpkms embeddings generate

# Generate for specific objects
dpkms embeddings generate --ids obj1,obj2,obj3

# Refresh stale embeddings
dpkms embeddings refresh --stale

# Check embedding status
dpkms embeddings status

# Delete embeddings (keep objects)
dpkms embeddings delete --ids obj1,obj2
```

### REST Endpoints

```
POST /embeddings/generate
GET  /embeddings/{objectId}
DELETE /embeddings/{objectId}
POST /embeddings/search
GET  /embeddings/status
```

---

## Testing Strategy

### Unit Tests
- Provider interface mocking
- Staleness detection logic
- Cache behavior
- Batch processing

### Integration Tests
- End-to-end embedding generation
- Similarity search accuracy
- Provider fallback
- Storage backend compatibility

### Performance Tests
- Batch generation throughput
- Search latency at scale
- Cache effectiveness
- Memory usage

---

## Future Enhancements

**Planned:**
- Multi-vector embeddings (separate vectors for title, content, metadata)
- Contextual embeddings (entity-aware)
- Embedding compression techniques
- Federated embedding search across registries
- Custom fine-tuning workflows

---

## See Also

- [storage.md](./storage.md) - Storage backends
- [query-language-spec.md](./query-language-spec.md) - Query syntax including similarity search
- [ranking-and-reranking.md](./ranking-and-reranking.md) - Hybrid retrieval and reranking
- [caching.md](./caching.md) - Caching strategies
- [../design.md](../design.md) - System design patterns
