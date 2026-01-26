# ADR-022 – Vector Indexing and Hybrid Semantic Search

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Traditional keyword and full-text search fails when users cannot formulate precise queries or remember exact terms. Users need semantic understanding:

**Query Formulation Challenges:**
- Users forget exact keywords used in notes
- Synonyms and related concepts not matched
- Paraphrased queries miss relevant content
- Cross-language knowledge not discoverable
- Conceptual similarity not captured

**FTS Limitations:**
- Exact term matching only
- Stemming helps but limited
- No understanding of meaning
- Cannot handle typos well
- Language-specific (hard for polyglot knowledge)
- No semantic similarity ranking

**Graph-Only Limitations:**
- Requires explicit mentions to connect
- Cannot discover implicit relationships
- Limited to predefined entity connections
- Misses subtle semantic similarities
- No ranking by conceptual relevance

**Multi-Modal Needs:**
- Text semantics (documents, notes, articles)
- Code semantics (functions, APIs, snippets)
- Visual semantics (diagrams, screenshots, UI patterns)
- Mixed-mode queries (find image similar to text description)

**Performance and Scale:**
- Vector similarity expensive (million-scale comparisons)
- Embedding generation costly (time + API calls)
- Storage overhead (1536 dimensions × 4 bytes × N objects)
- Index building time significant
- Query latency must remain < 100ms

**Constraints:**
- Must work offline (local embedding models)
- Must preserve privacy (no required external APIs)
- Must be optional (not all users need semantic search)
- Must integrate with existing FTS and graph search
- Must support pluggable vector backends
- Must handle embedding model evolution (re-embedding)
- Must remain fast at scale (100K+ objects)

**Affected Subsystems:**
- Storage layer (vector column/table)
- Query engine (hybrid query execution)
- Embedding pipeline (model integration)
- Ranking system (multi-source score merging)
- Export/import (vector portability)
- Plugin system (custom embedding providers)

**Goals:**
- Enable semantic search ("find notes about authentication")
- Support conceptual similarity ("similar to this article")
- Handle paraphrased queries naturally
- Merge results from FTS + vectors + graph
- Maintain < 100ms query latency
- Preserve privacy (local-first embeddings)
- Support embedding model evolution

---

## Decision

**dPKMS will implement hybrid semantic search combining full-text search (FTS5), vector similarity (embeddings), and graph traversal (entities/mentions) through a unified query execution engine with pluggable vector backends (in-memory, pgvector, LEANN), optional embedding generation (local models preferred), and Reciprocal Rank Fusion (RRF) for result merging, enabling forgiving search under uncertainty while maintaining privacy and performance.**

The vector search system provides:

1. **Embedding Strategy**

   **Embedding Models:**
   ```yaml
   embeddings:
     provider: openai | ollama | local | custom
     enabled: true
     auto_embed: true  # Embed on ingestion

     # OpenAI (high quality, API cost)
     openai:
       model: text-embedding-3-large
       dimensions: 1536
       api_key: ${OPENAI_API_KEY}
       batch_size: 100
       retry_policy:
         max_retries: 3
         backoff: exponential

     # Ollama (local, free, good quality)
     ollama:
       model: nomic-embed-text
       dimensions: 768
       endpoint: http://localhost:11434
       timeout: 30s

     # Local embedding (ONNX runtime)
     local:
       model: all-MiniLM-L6-v2
       dimensions: 384
       device: cpu  # or cuda
       cache_dir: ~/.cache/ctxt/models

     # Custom embedding provider
     custom:
       plugin: my-embedding-provider
       config:
         # Plugin-specific configuration
   ```

   **Embedding Generation:**
   ```go
   type EmbeddingProvider interface {
       Name() string
       Dimensions() int
       Embed(ctx context.Context, texts []string) ([][]float32, error)
       BatchSize() int
       Cost() EmbeddingCost  // tokens/call, $/token
   }

   type EmbeddingCost struct {
       TokensPerText int
       CostPerToken  float64
       LocalOnly     bool
   }
   ```

   **Embedding Pipeline:**
   ```
   Object Creation → Text Extraction → Chunking (if large) →
   Embed via Provider → Store Vector → Update Index
   ```

   **What to Embed:**
   - Primary: Summary (if exists) or first 500 words
   - Alternative: Full content (chunked if > 8K tokens)
   - Code: Function signatures + docstrings
   - Multimodal: Text description + OCR text

2. **Vector Storage Backends**

   **Backend Options:**

   **In-Memory (Default, Small Scale):**
   ```yaml
   vector_backend:
     type: memory
     options:
       persist: true
       persist_path: ~/.local/share/ctxt/vectors.bin
       index_type: flat  # or hnsw
       metric: cosine
   ```

   - Pros: Fast, simple, no dependencies
   - Cons: Limited scale (< 10K objects), RAM overhead
   - Use case: Solo users, small knowledge bases

   **pgvector (PostgreSQL Extension):**
   ```yaml
   storage:
     type: postgres
     # ... postgres config

   vector_backend:
     type: pgvector
     options:
       table: object_embeddings
       index_type: ivfflat  # or hnsw
       lists: 100  # for ivfflat
       metric: cosine
       probes: 10  # search accuracy
   ```

   - Pros: Integrated with Postgres, proven, good performance
   - Cons: Requires PostgreSQL, extension install
   - Use case: Teams using PostgreSQL backend

   **LEANN (Graph-Based, Highly Efficient):**
   ```yaml
   vector_backend:
     type: leann
     options:
       path: ~/.local/share/ctxt/leann
       recompute: true  # On-demand embedding generation
       graph_degree: 32
       build_complexity: 64
       backend: hnsw
       metric: cosine
   ```

   - Pros: 97% storage savings, fast queries, local
   - Cons: Slower index build, less mature
   - Use case: Large knowledge bases, storage-constrained

   **Custom Plugin:**
   ```yaml
   vector_backend:
     type: plugin
     plugin: qdrant-backend
     config:
       url: http://localhost:6333
       collection: ctxt_knowledge
   ```

   - Pros: Specialized vector DB (Qdrant, Weaviate, Pinecone)
   - Cons: External dependency, additional setup
   - Use case: Advanced needs, existing infrastructure

3. **Hybrid Query Execution**

   **Query Types:**

   **Type 1: Pure Semantic Query**
   ```
   User: "authentication patterns"

   Execution:
   1. Embed query → [vector]
   2. Vector similarity search → top 100 candidates
   3. Graph expansion (if query mentions entities)
   4. Rank and return top 10
   ```

   **Type 2: Hybrid FTS + Vector**
   ```
   User: "React hooks patterns" + filters

   Execution:
   1. FTS query: "React hooks patterns" → candidates_fts
   2. Embed query → [vector]
   3. Vector search → candidates_vec
   4. Merge via RRF → unified_results
   5. Apply filters (type, date, profile)
   6. Return top 10
   ```

   **Type 3: Graph-Aware Semantic**
   ```
   User: "similar to" + object_id + graph depth

   Execution:
   1. Get object embedding → [vector]
   2. Vector similarity → candidates
   3. Graph traversal from object → related_entities
   4. Boost candidates mentioning related_entities
   5. Return top 10
   ```

   **Type 4: Multi-Modal**
   ```
   User: "find images similar to this article"

   Execution:
   1. Embed article text → [text_vector]
   2. Cross-modal search (text → image)
   3. Similarity in shared semantic space
   4. Return top 10 images
   ```

4. **Reciprocal Rank Fusion (RRF)**

   **Algorithm:**
   ```
   For each result source (FTS, Vector, Graph):
     For each result at rank r:
       score(result) += 1 / (k + r)

   Where k = 60 (constant, tunable)

   Final ranking: Sort by combined score descending
   ```

   **Example:**
   ```
   FTS results:     [A, B, C, D, E]
   Vector results:  [C, A, F, G, H]
   Graph results:   [A, I, C, J]

   RRF scores:
   A: 1/61 + 1/62 + 1/61 = 0.0164 + 0.0161 + 0.0164 = 0.0489
   C: 1/63 + 1/61 + 1/63 = 0.0159 + 0.0164 + 0.0159 = 0.0482
   B: 1/62 + 0 + 0 = 0.0161
   ...

   Final ranking: [A, C, B, F, I, D, G, J, H, E]
   ```

   **RRF Benefits:**
   - No score normalization needed
   - Rank-based (robust to score scale differences)
   - Simple implementation
   - Proven effective in practice
   - Handles missing sources gracefully

   **Alternative: Weighted Sum (Optional)**
   ```
   final_score =
     w_fts × normalize(fts_score) +
     w_vec × normalize(vec_score) +
     w_graph × normalize(graph_score)

   Where weights sum to 1.0
   Profile can override weights
   ```

5. **Query Language Extensions**

   **Semantic Operators:**
   ```
   # Semantic similarity
   similar=="authentication patterns"
   similar==@object_id

   # Minimum similarity threshold
   similar=="API design" AND similarity>=0.7

   # Combined with filters
   similar=="React hooks" AND tag==frontend AND created_at>=2024-01-01

   # Graph-aware semantic
   similar==@object_id AND graph_depth==2

   # Multimodal
   similar=="architecture diagram" AND type==image
   ```

   **AST Extensions:**
   ```go
   type SimilarityNode struct {
       Query      string   // Text query
       ObjectID   string   // Reference object
       Threshold  float64  // Minimum similarity
       GraphDepth int      // Graph expansion depth
   }
   ```

6. **Embedding Lifecycle Management**

   **Re-Embedding Triggers:**
   - Model upgrade (better embedding model available)
   - Dimension change (upgrade 384 → 768 → 1536)
   - Provider change (OpenAI → local for privacy)
   - Corrupted embeddings detected
   - User request (manual re-embedding)

   **Re-Embedding Process:**
   ```bash
   # Check embedding status
   ctxt embeddings status

   # Re-embed all objects
   ctxt embeddings rebuild --provider local

   # Re-embed subset
   ctxt embeddings rebuild --query "type==article AND created_at>=2024-01-01"

   # Estimate cost
   ctxt embeddings estimate --provider openai

   # Migrate embeddings
   ctxt embeddings migrate --from openai --to local
   ```

   **Versioning:**
   ```json
   {
     "embedding_metadata": {
       "provider": "openai",
       "model": "text-embedding-3-large",
       "dimensions": 1536,
       "generated_at": "2024-01-26T10:00:00Z",
       "version": 1
     }
   }
   ```

7. **Performance Optimization**

   **Indexing Strategies:**

   **HNSW (Hierarchical Navigable Small World):**
   - Fast approximate search
   - Excellent recall (> 95%)
   - Moderate memory overhead
   - Good for < 100K objects

   **IVFFlat (Inverted File with Flat Compression):**
   - Faster build time
   - Lower memory
   - Configurable accuracy (probes parameter)
   - Good for > 100K objects

   **Flat (Brute Force):**
   - Exact search (100% recall)
   - Slow at scale (O(n))
   - No index build time
   - Good for < 10K objects

   **Query Optimization:**
   ```go
   type QueryOptimizer struct {
       objectCount int
       indexType   string
   }

   func (qo *QueryOptimizer) SelectStrategy(query *Query) SearchStrategy {
       if query.Filters != nil && qo.selectivity(query.Filters) < 0.1 {
           // Filters highly selective, apply first
           return &FilterThenVector{}
       }
       if query.HasSemanticOnly() {
           // Pure vector search
           return &VectorOnlyStrategy{}
       }
       // Hybrid search
       return &HybridStrategy{merger: NewRRFMerger()}
   }
   ```

   **Caching:**
   ```go
   type EmbeddingCache struct {
       cache *lru.Cache
       ttl   time.Duration
   }

   // Cache query embeddings (common queries)
   func (ec *EmbeddingCache) GetOrEmbed(query string) ([]float32, error)

   // Cache result sets (short TTL)
   type ResultCache struct {
       cache *lru.Cache
       ttl   time.Duration
   }
   ```

8. **Privacy and Cost Control**

   **Local-First Embedding:**
   ```yaml
   # Preferred: Local model (no API calls)
   embeddings:
     provider: local
     model: all-MiniLM-L6-v2
     dimensions: 384

   # Or Ollama (local server, no external calls)
   embeddings:
     provider: ollama
     model: nomic-embed-text
     endpoint: http://localhost:11434
   ```

   **Cost Estimation:**
   ```bash
   $ ctxt embeddings estimate --provider openai

   Objects to embed: 5,432
   Tokens per object: ~500 (average)
   Total tokens: 2,716,000
   Cost estimate: $0.27 (at $0.0001/1K tokens)
   Time estimate: ~5 minutes (batch size 100)
   ```

   **Selective Embedding:**
   ```yaml
   embeddings:
     auto_embed: false  # Manual control
     rules:
       - match:
           type: article
           tag: technical
         embed: true
       - match:
           type: note
           length: <500
         embed: false  # Skip short notes
   ```

---

## Rationale

### Alternatives Considered

#### 1. **FTS Only, No Vectors (Rejected)**
Rely solely on full-text search improvements.

**Rejected because:**
- Cannot handle semantic similarity
- Synonym problem unsolved
- Paraphrased queries miss content
- No conceptual understanding
- Competition has semantic search

#### 2. **Mandatory External Embedding API (Rejected)**
Require OpenAI or similar API for embeddings.

**Rejected because:**
- Violates local-first principle
- Privacy concerns (sends content externally)
- Ongoing cost for users
- Network dependency breaks offline
- Vendor lock-in

#### 3. **Graph-Only Semantic (Rejected)**
Rely on entity mentions and graph for semantics.

**Rejected because:**
- Requires explicit mentions (user burden)
- Cannot discover implicit relationships
- Limited to predefined connections
- No fuzzy conceptual matching
- Graph sparsity limits effectiveness

#### 4. **Vector Search Only (Rejected)**
Pure vector similarity, no FTS or graph.

**Rejected because:**
- Exact keyword matches missed
- Entity-based filtering lost
- Graph relationships ignored
- Slower for exact matches
- Users expect keyword search

#### 5. **Normalize Scores, Not RRF (Rejected)**
Normalize each source's scores, then weight and sum.

**Rejected because:**
- Score scales differ wildly
- Normalization assumptions fragile
- Weights hard to tune
- RRF simpler and robust
- RRF proven in literature

### Benefits of Chosen Approach

**Best of All Worlds:**
- FTS for exact keyword matching
- Vectors for semantic similarity
- Graph for entity-based discovery
- RRF merges intelligently

**Privacy Preserved:**
- Local models default (Ollama, ONNX)
- External API optional
- No required external calls
- User controls embedding provider

**Performance Optimized:**
- Pluggable backends (scale per need)
- HNSW for speed
- Caching for common queries
- Query optimization strategies

**Future-Proof:**
- Embedding model evolution supported
- Re-embedding workflows built-in
- Pluggable providers (new models)
- Multimodal ready (vision embeddings)

**User Control:**
- Optional (can disable)
- Cost estimation before embedding
- Selective embedding rules
- Provider choice (local vs API)

### Drawbacks / Risks

**Complexity:**
- Three search systems to maintain
- Merging logic sophisticated
- Query optimization non-trivial
- Multiple failure modes

**Storage Overhead:**
- 384-1536 dimensions per object
- 1.5KB - 6KB per embedding
- 10K objects = 15MB - 60MB
- 100K objects = 150MB - 600MB

**Embedding Cost:**
- Initial embedding expensive (time/money)
- Re-embedding on model upgrade
- Incremental cost on ingestion
- API costs for external providers

**Quality Variance:**
- Local models less accurate than API
- Embedding quality impacts results
- Model selection matters
- User confusion about quality differences

---

## Consequences

### Positive

**Forgiving Search:**
- Users find content without exact keywords
- Semantic understanding of queries
- Synonym and paraphrase handling
- Conceptual similarity matching

**Competitive Feature:**
- Modern search experience
- Matches user expectations
- Comparable to commercial tools
- Differentiator for product

**Privacy-Friendly:**
- Local models default
- No required external calls
- User controls data sharing
- Sovereignty preserved

**Scalable:**
- Pluggable backends for growth
- LEANN for storage efficiency
- Optimizations per backend
- Handles 100K+ objects

### Negative

**Implementation Burden:**
- Vector backend integration complex
- Three search systems to coordinate
- RRF merger implementation
- Query optimization logic

**Storage Growth:**
- Embeddings add 30-40% storage
- Index structures add overhead
- Memory requirements increase
- Cache adds RAM pressure

**Operational Complexity:**
- Embedding pipeline management
- Re-embedding workflows
- Provider configuration
- Performance tuning needed

**User Education:**
- Semantic search concept unfamiliar
- Provider selection confusing
- Cost implications unclear
- Quality expectations management

### Neutral / Considerations

**Embedding Model Evolution:**
- New models improve over time
- Re-embedding recommended periodically
- Migration tools essential
- Version tracking important

**Multimodal Future:**
- Image embeddings (CLIP)
- Code embeddings (specialized)
- Audio embeddings (Whisper)
- Cross-modal search (text → image)

**Online Learning:**
- User interactions inform relevance
- Click-through improves ranking
- Personalized embedding space (future)
- Privacy-preserving learning

**Specialized Embeddings:**
- Domain-specific models (medical, legal)
- Fine-tuned for user's domain
- Plugin-provided embeddings
- Community model sharing

---

## Implementation Notes

### Core Components

**Embedding Manager:**
```go
type EmbeddingManager struct {
    provider EmbeddingProvider
    cache    EmbeddingCache
    queue    EmbeddingQueue
}

func (em *EmbeddingManager) EmbedObject(obj *Object) ([]float32, error)
func (em *EmbeddingManager) EmbedQuery(query string) ([]float32, error)
func (em *EmbeddingManager) BatchEmbed(objects []*Object) error
```

**Vector Backend Interface:**
```go
type VectorStore interface {
    Name() string
    Insert(id string, vector []float32) error
    Delete(id string) error
    Search(vector []float32, limit int) ([]ScoredResult, error)
    SearchWithFilter(vector []float32, filter Filter, limit int) ([]ScoredResult, error)
    Rebuild() error
    Stats() (*VectorStats, error)
}

type ScoredResult struct {
    ID         string
    Similarity float64
}
```

**Hybrid Query Executor:**
```go
type HybridExecutor struct {
    fts          FTSEngine
    vector       VectorStore
    graph        GraphStore
    merger       ResultMerger
    optimizer    QueryOptimizer
}

func (he *HybridExecutor) Execute(ctx context.Context, query *Query) (*SearchResults, error) {
    strategy := he.optimizer.SelectStrategy(query)
    return strategy.Execute(ctx, query)
}
```

**RRF Merger:**
```go
type RRFMerger struct {
    k float64  // Constant, typically 60
}

func (rrf *RRFMerger) Merge(
    ftsResults []Result,
    vecResults []Result,
    graphResults []Result,
) []Result {
    scores := make(map[string]float64)

    // Accumulate RRF scores from each source
    for rank, result := range ftsResults {
        scores[result.ID] += 1.0 / (rrf.k + float64(rank+1))
    }
    for rank, result := range vecResults {
        scores[result.ID] += 1.0 / (rrf.k + float64(rank+1))
    }
    for rank, result := range graphResults {
        scores[result.ID] += 1.0 / (rrf.k + float64(rank+1))
    }

    // Sort by combined score
    return sortByScore(scores)
}
```

### CLI Commands

```bash
# Embedding configuration
ctxt config set embeddings.provider local
ctxt config set embeddings.model all-MiniLM-L6-v2

# Embedding management
ctxt embeddings status
ctxt embeddings rebuild [--provider local] [--query "filter"]
ctxt embeddings estimate --provider openai
ctxt embeddings migrate --from openai --to local

# Vector backend configuration
ctxt config set vector_backend.type hnsw
ctxt config set vector_backend.metric cosine

# Search with semantic
ctxt find similar=="authentication patterns"
ctxt find similar==@550e8400 --graph-depth 2
ctxt find "React hooks" --semantic  # Enable semantic mode

# Explain ranking
ctxt find similar=="API design" --explain
```

### Storage Schema

**Objects table extensions:**
```sql
ALTER TABLE objects ADD COLUMN embedding BLOB;
ALTER TABLE objects ADD COLUMN embedding_metadata JSON;

-- Example embedding_metadata:
{
  "provider": "openai",
  "model": "text-embedding-3-large",
  "dimensions": 1536,
  "generated_at": "2024-01-26T10:00:00Z",
  "version": 1
}
```

**Vector index table (for backends without native support):**
```sql
CREATE TABLE vector_index (
    object_id UUID PRIMARY KEY,
    embedding BLOB NOT NULL,
    dimensions INT NOT NULL,
    metadata JSON,
    indexed_at TIMESTAMP NOT NULL,
    FOREIGN KEY(object_id) REFERENCES objects(id)
);
```

### Integration Points

**With Storage Layer:**
1. Store embeddings in objects table or separate table
2. Backend-specific indexes (pgvector, HNSW)
3. Bulk insert for re-embedding
4. Transaction support for atomicity

**With Query Engine:**
1. Parse semantic operators from query AST
2. Generate embedding for query
3. Execute vector search
4. Merge with FTS and graph results
5. Return unified results

**With Pipeline System:**
1. Embedding generation as pipeline step
2. Post-enrichment embedding
3. Async embedding for large batches
4. Retry on embedding failure

**With Profile System:**
1. Profile-specific embedding rules
2. Profile-weighted RRF merger
3. Profile-scoped vector search
4. Profile boost for semantic results

### Migration Strategy

**Phase 1: Foundation (Skeleton 6)**
- Embedding provider interface
- In-memory vector backend
- Basic semantic search operator
- Simple RRF merger

**Phase 2: Hybrid Search (Skeleton 6)**
- Query executor with FTS+Vector+Graph
- RRF merger implementation
- Query optimization strategies
- Result explanation

**Phase 3: Advanced Backends (Skeleton 7)**
- pgvector integration
- LEANN integration
- HNSW indexing
- Performance optimization

**Phase 4: Production Features (Skeleton 8+)**
- Re-embedding workflows
- Embedding migration tools
- Cost estimation
- Selective embedding rules
- Multimodal embeddings (image, code)

### Testing Requirements

**Unit Tests:**
- Embedding generation
- Vector similarity calculation
- RRF merger correctness
- Query parsing (semantic operators)

**Integration Tests:**
- End-to-end semantic search
- Hybrid query execution
- Backend switching (memory → pgvector)
- Re-embedding workflows

**Performance Tests:**
- Vector search latency (1K, 10K, 100K objects)
- Hybrid query performance
- Index build time
- Memory usage

**Quality Tests:**
- Semantic search recall (known relevant items)
- RRF vs individual sources
- Embedding quality (cosine similarity distributions)
- Cross-lingual search (if supported)

---

## References

- **architecture.md:111-120** – Hybrid Retrieval specification
- **ROADMAP.md** – Skeleton 6: Hybrid Retrieval (Discoverability Upgrades)
- **dpkms/ranking-and-reranking.md** – RRF and merging strategies
- ADR-021 – Multi-Backend Storage (vector backend integration)
- ADR-010 – Extended RSQL (semantic query operators)
- ADR-011 – Multi-Source Reranker (RRF implementation)

**External References:**
- RRF Paper: https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf
- pgvector: https://github.com/pgvector/pgvector
- LEANN: https://github.com/yichuan-w/LEANN
- HNSW: https://arxiv.org/abs/1603.09320
- Sentence Transformers: https://www.sbert.net/
- OpenAI Embeddings: https://platform.openai.com/docs/guides/embeddings
- Ollama: https://ollama.ai/

---
