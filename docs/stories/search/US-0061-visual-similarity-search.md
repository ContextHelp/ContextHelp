---
status: shipped
---

# US-0061: Visual Similarity Search

**System Types:** ctxt, dpkms
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Agents & LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker, I want to use an image as a search query — and get back all related knowledge (text notes, decisions, entities, code snippets, other images) — so I can find context connected to what I'm looking at without having to describe it in words.

---

## Context

Knowledge workers constantly work with visual artifacts: architecture diagrams, whiteboard photos, UI screenshots, flowcharts, error messages, presentation slides. The natural instinct is "I have this image — what do I know about this?"

Today, searching requires the user to translate what they see into text: "authentication flow diagram" or "microservice architecture." This loses information — the user may not know the right words, or the visual content may convey relationships and structure that text queries can't express.

This story enables **image-as-query search** across the entire knowledge base. The image is the input; the results span all knowledge object types:

| Input | Possible Results |
|-------|-----------------|
| Whiteboard photo of auth flow | Decisions about auth architecture, meeting notes discussing auth, related code snippets, other auth diagrams |
| Screenshot of error message | Bug reports mentioning that error, troubleshooting notes, stack overflow captures about it |
| Photo of system diagram | Architecture ADRs, entity relationships shown in diagram, design docs, similar diagrams |
| UI mockup screenshot | Related design decisions, feature specs, user stories, other mockup iterations |

This works through two complementary mechanisms:

1. **Cross-modal matching** — The image's VLM-generated description is used as a text query against all knowledge objects (text, decisions, entities, etc.)
2. **Visual similarity** — The image's visual embedding is compared against other image objects' visual embeddings for image-to-image matches

Both result sets merge via RRF, returning a unified ranked list spanning all knowledge types.

**Prerequisite:** US-0003 (Image OCR and Analysis) must be implemented first.

---

## Acceptance Criteria

### Cross-Modal Search (Image → All Knowledge)

- [ ] User can search entire knowledge base using an image: `ctxt search --image photo.png`
- [ ] Results include ALL knowledge object types (text, decisions, entities, code, images, URLs)
- [ ] System generates a text description of the query image (via VLM) and searches against all text embeddings
- [ ] OCR text extracted from query image is also used as search terms
- [ ] Results are ranked by relevance across modalities

### Visual Similarity (Image → Similar Images)

- [ ] Image knowledge objects store both text and visual embeddings (dual embedding)
- [ ] Visual embeddings are generated during image ingestion pipeline
- [ ] When searching with `--image`, visual similarity results are merged with cross-modal results
- [ ] User can restrict to image-only results: `ctxt search --image photo.png --type image`
- [ ] Visual embedding provider is pluggable (same backend interface as text embeddings)

### Combined and Filtered

- [ ] User can combine image query with text: `ctxt search --image diagram.png "authentication"`
- [ ] User can combine with filters: `ctxt search --image photo.png --tag architecture --profile engineering`
- [ ] User can reference an existing object's image: `ctxt search --image obj-123`
- [ ] Graceful degradation: without visual embedding provider, falls back to cross-modal text search only (with info message)
- [ ] Performance: returns results within 2 seconds for 10K+ objects

---

## Implementation Notes

### How Image-as-Query Works

```
User: ctxt search --image whiteboard.png

Step 1 — Understand the image:
  ├─ VLM API call → "Architecture diagram showing API Gateway,
  │                  Auth Service, User DB, and Message Queue
  │                  connected with arrows. Handwritten labels."
  ├─ OCR extraction → "API GW", "Auth Svc", "UserDB", "MQ", "Redis"
  └─ Visual embedding → [0.56, 0.78, ...] (vision encoder)

Step 2 — Multi-strategy search using image understanding:
  ├─ FTS: search("API Gateway" OR "Auth Service" OR "UserDB" OR "MQ")
  │   → [meeting notes mentioning API Gateway, ADR about auth service]
  │
  ├─ Vector (text): embed(VLM description) → cosine similarity
  │   → [architecture decision docs, system design notes]
  │
  ├─ Graph: resolve entities(@api-gateway, @auth-service, @user-db)
  │   → [all objects mentioning these entities, their relationships]
  │
  ├─ Vector (visual): cosine similarity on visual embeddings
  │   → [other architecture diagrams, similar whiteboard photos]
  │
  └─ Metadata: type filters, profile filters (if specified)

Step 3 — Merge via RRF:
  ├─ All results from all strategies ranked together
  ├─ Duplicates collapsed (same object from multiple strategies boosts rank)
  └─ Return unified list with match explanations

Result:
  1. "ADR-018: Auth Service Architecture" (text match + entity match)
  2. "Meeting notes: API Gateway discussion, Jan 15" (FTS + entity)
  3. "Architecture diagram v2" (visual similarity: 0.91)
  4. "Auth service code review notes" (vector text similarity)
  5. "Redis caching decision" (entity match: @redis)
```

### Dual Embedding Architecture

The existing embeddings schema supports multiple embeddings per object:

```sql
CREATE TABLE embeddings (
    object_id TEXT NOT NULL,
    model TEXT NOT NULL,      -- distinguishes text vs. visual
    vector BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (object_id) REFERENCES objects(id) ON DELETE CASCADE
);
```

An image knowledge object carries:

| object_id | model | purpose |
|-----------|-------|---------|
| img-001 | `text-embedding-3-small` | Cross-modal search (text description) |
| img-001 | `dinov2-vitl14` | Visual similarity (image-to-image) |

No schema changes required.

### Image Ingestion Pipeline (Enhanced)

```
Image captured (ctxt add image.png)
  │
  ├─ Existing (US-0003):
  │   ├─ VLM API call → text description + OCR
  │   ├─ Entity/mention extraction from description
  │   ├─ Text embedding of description
  │   └─ Knowledge object created
  │
  └─ New (US-0061):
      ├─ Visual embedding API call (vision encoder)
      ├─ Store as additional embedding row (model = vision provider)
      └─ Index in vector store for visual similarity
```

### CLI Interface

```bash
# Core: search with image → get all related knowledge
ctxt search --image ~/photos/whiteboard.png

# Returns mixed results (text, decisions, images, code, etc.)
[
  {
    "object_id": "obj-aaa111",
    "type": "text",
    "title": "ADR-018: Auth Service Architecture",
    "snippet": "We decided to use API Gateway pattern with...",
    "relevance": 0.94,
    "match_sources": ["entity:@api-gateway", "vector:text"]
  },
  {
    "object_id": "obj-bbb222",
    "type": "text",
    "title": "Meeting notes: API Gateway discussion",
    "snippet": "Agreed on Redis for caching layer between...",
    "relevance": 0.87,
    "match_sources": ["fts:API Gateway", "entity:@redis"]
  },
  {
    "object_id": "obj-ccc333",
    "type": "image",
    "title": "Architecture diagram v2",
    "snippet": "Similar architecture diagram from last sprint",
    "relevance": 0.85,
    "match_sources": ["visual:0.91", "entity:@auth-service"]
  }
]

# Combine with text query (narrow the context)
ctxt search --image diagram.png "authentication decisions"

# Filter to specific types
ctxt search --image screenshot.png --type decision
ctxt search --image mockup.png --type image   # image-to-image only

# Use existing object as image query
ctxt search --image obj-abc123

# With profile and tag filters
ctxt search --image whiteboard.jpg --profile engineering --tag architecture

# Limit results
ctxt search --image photo.png --limit 5
```

### REST API

```
POST /search/by-image
Content-Type: multipart/form-data

image: <binary>
query: "authentication"           (optional text query)
filters: {"tags": ["architecture"], "type": "decision"}
limit: 10

→ 200 OK
{
  "query_understanding": {
    "description": "Architecture diagram showing API Gateway...",
    "ocr_text": ["API GW", "Auth Svc", "UserDB"],
    "entities_detected": ["@api-gateway", "@auth-service"],
    "visual_embedding_model": "dinov2-vitl14"
  },
  "results": [...],
  "strategies_used": ["fts", "vector_text", "vector_visual", "graph"],
  "query_time_ms": 342
}
```

```
POST /search/by-image
Content-Type: application/json

{
  "object_id": "obj-abc123",
  "query": "related decisions",
  "limit": 10
}
```

### gRPC Interface

```protobuf
service Search {
  rpc SearchByImage(ImageSearchRequest) returns (SearchResponse);
}

message ImageSearchRequest {
  oneof image_source {
    bytes image = 1;           // raw image bytes
    string object_id = 2;      // existing object reference
  }
  string text_query = 3;       // optional text to combine
  SearchFilters filters = 4;   // type, tag, profile filters
  int32 limit = 5;
}

message SearchResponse {
  ImageQueryUnderstanding understanding = 1;
  repeated SearchResult results = 2;
  repeated string strategies_used = 3;
  int32 query_time_ms = 4;
}

message ImageQueryUnderstanding {
  string description = 1;
  repeated string ocr_text = 2;
  repeated string entities_detected = 3;
}
```

### Search Strategy Integration

Image-as-query activates multiple strategies simultaneously:

```
ctxt search --image photo.png
  │
  ├─ VLM describes image → text description + OCR
  │
  ├─ Strategy: FTS
  │   └─ Search OCR text + key terms from description
  │
  ├─ Strategy: Vector (text)
  │   └─ Embed description → cosine similarity against ALL objects' text embeddings
  │   └─ This is what returns text notes, decisions, code related to image content
  │
  ├─ Strategy: Graph
  │   └─ Resolve entities mentioned in description → traverse graph
  │   └─ Returns objects connected to same entities shown in image
  │
  ├─ Strategy: Visual (new)
  │   └─ Visual embedding → cosine similarity against image objects' visual embeddings
  │   └─ Returns visually similar images only
  │
  └─ RRF Merge
      └─ All results ranked together, deduplicated, match sources annotated
```

The key insight: **text strategies (FTS, vector, graph) search against ALL knowledge objects**, not just images. The image is translated into text understanding, then that understanding queries the full knowledge base. Visual strategy adds image-to-image on top.

### Visual Embedding Provider Interface

```go
// Extends the existing EmbeddingProvider interface
type VisualEmbeddingProvider interface {
    // Embed an image into a vector
    EmbedImage(ctx context.Context, image []byte) ([]float32, error)

    // Model identifier (stored in embeddings.model column)
    ModelID() string

    // Vector dimensions
    Dimensions() int
}
```

Pluggable implementations:
- API-based: OpenAI (if/when available), Replicate, HuggingFace Inference
- Local: DINOv2 via ONNX runtime, CLIP via ONNX
- Plugin: custom providers via plugin system (ADR-012)

### Configuration

```yaml
# In configuration.yaml
visualEmbedding:
  enabled: true
  provider: replicate  # or: openai, local, plugin
  model: dinov2-vitl14
  replicate:
    apiKey: ${CH_REPLICATE_API_KEY}
  local:
    modelPath: ./models/dinov2-vitl14.onnx

imageSearch:
  # VLM used to describe query images
  vlmProvider: ${aiProvider}
  # Whether to include OCR text in search
  ocrEnabled: true
  # Whether to resolve entities from image description
  entityResolution: true
```

### Backfill

When visual embedding is first enabled, existing image objects need backfilling:

```bash
# Backfill visual embeddings for all existing image objects
ctxt admin backfill --type visual-embeddings

# Status
ctxt admin backfill --status
# → 847/1203 image objects embedded (70%), ETA: 3 minutes
```

---

## E2E Test Checklist

### CLI → Server Payload

- [ ] `ctxt search --image diagram.png` sends image bytes (or temp path) in multipart/form-data
      to `POST /search/by-image`; server receives non-empty `image` field
- [ ] `ctxt search --image obj-123` sends `object_id=obj-123` in JSON body; server receives and
      resolves to stored visual embedding (no re-encoding)
- [ ] `ctxt search --image photo.png "auth flow"` sends both `image` and `query="auth flow"` in
      request; server receives both fields
- [ ] `ctxt search --image photo.png --type decision` sends `filters.type=decision` in request
      payload; server receives and applies type filter
- [ ] `ctxt search --image photo.png --tag architecture` sends `filters.tags=["architecture"]` in
      request payload; server receives and applies tag filter
- [ ] `ctxt search --image photo.png --profile engineering` sends `filters.profile=engineering` in
      request payload; server receives and applies profile filter
- [ ] `ctxt search --image photo.png --limit 5` sends `limit=5` in request payload; server
      receives it and returns ≤5 results

### Server-Side Receipt and Storage

- [ ] Server stores visual embedding in `embeddings` table with correct `model` identifier upon
      `ctxt add image.png`; row verifiable via DB query
- [ ] Server stores text embedding (from VLM description) as separate `embeddings` row for same
      `object_id`; both rows present after ingestion
- [ ] `query_understanding.description` in response is populated server-side (non-empty string)
- [ ] `query_understanding.ocr_text` in response reflects OCR extracted from query image
- [ ] `query_understanding.entities_detected` in response reflects entities resolved server-side
- [ ] `strategies_used` field in response lists all strategies server actually executed
- [ ] `query_time_ms` field in response is populated with server-measured duration

### Flags Coverage

- [ ] `--image <path>` — image bytes present in request payload; server processes image
- [ ] `--image <obj-id>` — `object_id` present in request payload; no re-upload
- [ ] `--type <type>` — `filters.type` present in request payload; results match type
- [ ] `--tag <tag>` — `filters.tags` present in request payload; results match tag
- [ ] `--profile <name>` — `filters.profile` present in request payload; results filtered
- [ ] `--limit <n>` — `limit` present in request payload; result count ≤ n
- [ ] No undocumented flags silently ignored: unknown flag returns error

### Cross-Modal (Image → All Knowledge)

- [ ] `ctxt search --image diagram.png` returns text notes related to diagram content
- [ ] `ctxt search --image diagram.png` returns decisions related to diagram content
- [ ] `ctxt search --image diagram.png` returns entities shown in diagram
- [ ] Results include `match_sources` annotations (fts, vector_text, graph, visual)
- [ ] VLM-generated description is used for text-based strategies (vector_text, graph)
- [ ] OCR-extracted text is used for FTS strategy
- [ ] Entities detected in image description are resolved and used for graph strategy

### Visual Similarity (Image → Similar Images)

- [ ] `ctxt add image.png` stores both text and visual embeddings (both rows in `embeddings` table)
- [ ] Visual embedding stored with correct model identifier (e.g., `dinov2-vitl14`)
- [ ] `ctxt search --image photo.png --type image` returns visually similar images
- [ ] Results ranked by cosine similarity, highest first
- [ ] `ctxt search --image obj-123` uses stored visual embedding (no re-encoding); server
      confirms via `object_id` path in request

### Combined and Filtered

- [ ] `ctxt search --image photo.png "auth flow"` merges visual + text via RRF; response
      `strategies_used` includes both visual and text strategies
- [ ] `ctxt search --image photo.png --tag architecture` applies tag filter; all results carry
      `architecture` tag
- [ ] `ctxt search --image photo.png --type decision` returns only decisions
- [ ] Returns within 2 seconds for 10K objects (P99)

### Fallback

- [ ] Without visual embedding provider: cross-modal text search still works; response
      `strategies_used` omits `visual`; info message returned to client
- [ ] Without VLM provider: error returned — cannot understand image content

### APIs

- [ ] `POST /search/by-image` multipart: server receives `image` field; returns mixed-type results
- [ ] `POST /search/by-image` JSON with `object_id`: server returns results using stored embedding
- [ ] Response `query_understanding` contains `description`, `ocr_text`, `entities_detected`
- [ ] gRPC `SearchByImage` returns equivalent results to REST for same input

### Backfill and Maintenance

- [ ] `ctxt admin backfill --type visual-embeddings` processes existing image objects; DB row
      count of visual embeddings increases
- [ ] `ctxt admin backfill --status` returns progress (processed/total count)
- [ ] Plugin: custom visual embedding provider loads and `EmbedImage` called by server
- [ ] Multi-format ingestion: PNG, JPEG, WebP, TIFF, SVG (rasterized) all produce embeddings

---

## Related Stories

- [US-0003](../ingestion/US-0003-image-ocr-and-analysis.md) — Image OCR and Analysis (prerequisite; creates image knowledge objects)
- [US-0005](../ingestion/US-0005-video-processing-with-scenes.md) — Video Processing (frame-level visual search potential)
- [US-0051](./US-0051-semantic-search-with-embeddings.md) — Semantic Search with Embeddings (text embedding search; extended here to visual)
- [US-0016](./US-0016-natural-language-search.md) — Natural Language Search (complementary; text vs. visual query modalities)
- [US-0018](./US-0018-multi-strategy-search-execution.md) — Multi-Strategy Search (visual becomes new strategy in pipeline)
- [US-0021](./US-0021-search-with-result-explanation.md) — Search with Explanation (match_sources annotation)

## Related ADRs

- [ADR-022](../../decisions/) — Vector Indexing and Hybrid Semantic Search (visual embeddings extend this)
- [ADR-026](../../decisions/) — Multimodal Content Processing (visual search is a multimodal capability)
- [ADR-048](../../decisions/ADR-048-visual-ssl-scaling-not-adopted.md) — Visual SSL Scaling (informative; establishes dual embedding principle)
- [ADR-021](../../decisions/) — Multi-Backend Storage (pluggable vector backends)
- [ADR-011](../../decisions/) — Multi-Source Reranker (RRF merge across modalities)
- [ADR-005](../../decisions/) — Decorator Pattern for AI Providers (VLM as provider)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)

---

## E2E Tests

- `test/integration/us0061_visual_similarity_test.go::TestUS0061_VisuallySimialrImageRanksHigher`
- `test/integration/us0061_visual_similarity_test.go::TestUS0061_DualEmbeddingImageAndTextCoexist`
- `test/integration/us0061_visual_similarity_test.go::TestUS0061_ImageTypeFilterRestrictsVectorResults`
- `test/integration/us0061_visual_similarity_test.go::TestUS0061_NoEmbeddingImageExcludedFromVectorSearch`
