# US-0051: Semantic Search with Embeddings

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a user, I want to search the knowledge base using semantic similarity so that I find conceptually related objects even when they don't share keywords with my query.

---

## Context

Keyword search fails when the query and the document use different words for the same concept. Semantic search encodes the query and all indexed objects as dense vectors using a text embedding model, then ranks results by cosine similarity. Objects are embedded at ingestion time and stored in the `embeddings` table (keyed by `object_id` and `model`). At query time the server embeds the query text with the same model and performs an approximate nearest-neighbor search. Users can specify the embedding model explicitly, set a minimum similarity threshold, and later switch or add models — each model gets its own row, so multiple models co-exist without overwriting.

---

## Acceptance Criteria

- [ ] User can invoke vector search: `ctxt find "query" --strategy vector`
- [ ] User can specify the embedding model: `--embedding-model text-embedding-3-small`
- [ ] User can set a minimum similarity threshold: `--threshold 0.75`; all returned results have `rank.score` ≥ threshold
- [ ] Server generates a query embedding at search time; the embedding is not returned to the client
- [ ] Server stores embeddings at ingestion time in the `embeddings` table with `model` column set to the provider model ID
- [ ] Re-indexing with a new model appends a new row; it does not overwrite existing model rows
- [ ] Semantically similar objects (not keyword-matched) appear in top results
- [ ] Cosine similarity scores in `rank.explain.vector_similarity` are in [-1, 1]
- [ ] Results differ from FTS-only query on the same text (semantic vs. keyword)
- [ ] Unknown embedding model returns 400 with a model-not-found error
- [ ] Embedding provider unavailable returns 503 with a retry-after hint
- [ ] Same vector query via CLI, REST, and gRPC returns identical result sets and similarity scores

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload

- [ ] `ctxt find "query" --strategy vector` sends `strategies=["vector"]` and the query text in
      request payload; server receives both
- [ ] `ctxt find "query" --embedding-model text-embedding-3-small` sends `embedding_model=
      text-embedding-3-small` in payload; server receives and uses specified model
- [ ] `ctxt find "query" --limit 10` sends `limit=10` in payload; server receives it
- [ ] `ctxt find "query" --threshold 0.75` sends `min_score=0.75` in payload; server receives
      and filters results below threshold

### Server-Side Receipt and Storage

- [ ] Server receives query text and generates query embedding using the requested (or default)
      model; embedding not returned to client but used for search
- [ ] Server stores embeddings for ingested objects in `embeddings` table with `model` column
      set to provider model ID; row verifiable via DB query
- [ ] Server receives `min_score`; all returned results have `rank.score` ≥ threshold
- [ ] Server receives `limit`; result count ≤ limit
- [ ] Server response includes `embedding_model` field indicating which model was used

### Flags Coverage

- [ ] `--strategy vector` — `strategies=["vector"]` in payload; server executes only vector
      search (no FTS, no graph)
- [ ] `--embedding-model <id>` — model ID in payload; server uses specified model
- [ ] `--limit <n>` — in payload; result count ≤ n
- [ ] `--threshold <f>` — `min_score` in payload; results all ≥ threshold score
- [ ] Unknown embedding model → server returns 400 with model-not-found error

### Embedding Correctness

- [ ] Semantically similar objects (not keyword-matched) appear in top results
- [ ] Cosine similarity scores in `rank.explain.vector_similarity` are in [-1, 1]
- [ ] Higher similarity score → higher rank position
- [ ] Results differ from FTS-only query on same text (semantic vs. keyword)

### Storage and Backfill

- [ ] `ctxt add <object>` triggers embedding generation; `embeddings` table row created
- [ ] Embedding stored with correct `model` identifier
- [ ] Re-indexing with new model appends new row (does not overwrite existing model rows)

### Error Handling

- [ ] Embedding provider unavailable → 503 with retry-after hint
- [ ] Object with no embedding → excluded from results (not 500)

### Interface Parity

- [ ] Same vector query via CLI, REST, and gRPC → identical result sets and similarity scores

---

## Related Stories

- [US-0018](./US-0018-multi-strategy-search-execution.md) — Multi-Strategy Search Execution (vector is one strategy in the pipeline)
- [US-0021](./US-0021-search-with-result-explanation.md) — Search with Result Explanation (`rank.explain.vector_similarity` populated here)
- [US-0061](./US-0061-visual-similarity-search.md) — Visual Similarity Search (extends text embeddings to visual embeddings)
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) — Extract Entities and Mentions (enrichment that complements semantic search)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)

---

## E2E Tests

> Not yet implemented.
