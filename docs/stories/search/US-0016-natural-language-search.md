# Story: Natural Language Search

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a user, I want to search my knowledge base using natural language questions without learning query syntax.

---

## Context

Knowledge workers think in natural language ("What are insights on error handling?", "How does the system handle user errors?"). Forcing them to learn RSQL syntax creates friction. The system should understand intent and automatically select optimal search strategies.

---

## Acceptance Criteria

- [ ] User can type natural language queries in any form (declarative, interrogative)
- [ ] System auto-detects intent (semantic summary, relationship pattern, temporal, similarity, metadata filter)
- [ ] System selects optimal strategy mix based on intent (graph, vector, FTS, metadata, registries)
- [ ] Results return within 2 seconds (local) or 5 seconds (with federated registries)
- [ ] Each result includes `rank.explain` (why this matched, contributing strategies)
- [ ] Results can be filtered by focus profile (role/project lens)
- [ ] System handles ambiguous queries gracefully (returns best guess + offers clarification)
- [ ] Query can be refined iteratively ("show only engineering decisions", "from last month")

---

## Implementation Notes

### CLI Interface
```bash
# Most basic natural language
ctxt find "what are insights on error handling"

# Interrogative form
ctxt search "how does the system handle user errors"

# Declarative
ctxt find "recent decisions about mobile"

# With profile filter
ctxt find "insights on authentication" --profile security

# With time filter (natural)
ctxt find "recent decisions" --days 7

# Returns results with explanations
```

### NLQ Normalization Pipeline

```
User Input: "how does the system handle user errors"
     ↓
Intent Classifier (AI): Detect intent type
  → Intent: "relationship_pattern"
  → Confidence: 0.92
     ↓
Entity Extractor (AI): Identify entities
  → Entities: [
      "@system.system",
      "@concept.user-errors"
    ]
  → Confidence: 0.88
     ↓
Strategy Selector: Choose optimal execution strategies
  → Primary: graph (relationship traversal)
  → Secondary: vector (pattern matching)
  → Metadata: filter if needed
     ↓
Query Plan: Multi-strategy parallel execution
  [
    { strategy: "graph", intent: "traverse mentions of user-errors" },
    { strategy: "vector", intent: "semantically similar to error handling patterns" },
    { strategy: "metadata", filter: { type: { in: ["decision", "concept"] } } }
  ]
     ↓
Merge Results: Combine from all strategies
     ↓
Rerank Results: Combine scores (RRF, weighted sum)
     ↓
Apply Profile Filter: Boost/filter by focus profile
     ↓
Return Results with Explanation
```

### Intent Detection

| Natural Language | Intent Type | Primary Strategy | Secondary |
|---|---|---|---|
| "what are insights on X" | Semantic summary | Vector (similarity) | Metadata (type=summary) |
| "how does A handle B" | Relationship pattern | Graph (traversal) | Vector (pattern matching) |
| "find recent decisions about X" | Temporal entity | Metadata (date filter) | FTS (keyword) |
| "show related work on X" | Similarity discovery | Vector (embeddings) | Graph (relationships) |
| "list all X" | Metadata filter | Metadata (type filter) | — |

### LMQL Constrained Normalization (Local Models)

```lmql
argmax
    "User query: " + query + "\n"
    "Intent (relationship_pattern, semantic_summary, temporal_entity, similarity_discovery, metadata_filter): "
    intent in ["relationship_pattern", "semantic_summary", "temporal_entity", "similarity_discovery", "metadata_filter"]
    "\n"
    "Entities: ["
    for i in range(3):  // max 3 entities
        '"@' + in(["person", "project", "system", "concept"]) + '.' + gen(slug_pattern) + '"'
        if i < 2: ","
    "]\n"
    "Strategies (y/n): graph="
    graph in ["yes", "no"]
    " vector="
    vector in ["yes", "no"]
    " fts="
    fts in ["yes", "no"]
    " metadata="
    metadata in ["yes", "no"]
from
    openai("gpt-4")
where
  len(STOP) < 300
```

### Fallback for API Models (instructor)

```python
from pydantic import BaseModel
from typing import List, Literal

class NLQNormalization(BaseModel):
    intent: Literal[
        "relationship_pattern",
        "semantic_summary",
        "temporal_entity",
        "similarity_discovery",
        "metadata_filter"
    ]
    entities: List[str] = Field(..., description="@type.slug format")
    graph_search: bool = Field(..., description="include graph traversal?")
    vector_search: bool = Field(..., description="include vector search?")
    fts_search: bool = Field(..., description="include full-text search?")
    metadata_search: bool = Field(..., description="include metadata filters?")

# Use instructor to validate
response = client.messages.create(
    model="gpt-4",
    messages=[{"role": "user", "content": prompt}],
    response_model=NLQNormalization,
)
```

### REST API

```
GET /search?q=how+does+system+handle+errors&query_mode=nlq
Content-Type: application/json

→ 200 OK
{
  "query": "how does the system handle user errors",
  "intent": "relationship_pattern",
  "entities": ["@system.system", "@concept.user-errors"],
  "strategies": ["graph", "vector"],
  "results": [
    {
      "id": "o-abc123",
      "type": "decision",
      "summary": "Implement centralized error logging middleware",
      "rank": {
        "score": 0.92,
        "explain": {
          "graph_match": 0.88,
          "vector_similarity": 0.95,
          "mention_boost": 0.05
        }
      }
    },
    ...
  ],
  "total": 42,
  "took_ms": 567
}
```

### gRPC

```protobuf
message SearchRequest {
  string query = 1;
  string query_mode = 2;  // "nlq", "rsql", "auto"
  string profile = 3;      // optional focus profile
  int32 limit = 4;
  int32 offset = 5;
}

message SearchResult {
  string id = 1;
  string type = 2;
  string summary = 3;
  double rank_score = 4;
  RankExplanation rank_explain = 5;
}

message RankExplanation {
  map<string, double> strategy_scores = 1;  // graph → 0.88, vector → 0.95
  string primary_match_reason = 2;
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt find "what are insights on X"` → results in <2s
- [ ] Intent: Query intent correctly classified for each example type
- [ ] Entities: Extracted entities formatted as `@type.slug`
- [ ] Strategy: Relationship queries primarily use graph, semantic queries primarily use vector
- [ ] Results: All results ranked, each result includes explanation
- [ ] Ranking: Results ordered by relevance score descending
- [ ] Profile Filter: With `--profile engineering`, non-relevant results deprioritized
- [ ] Ambiguity: Ambiguous query returns results + asks for clarification
- [ ] Latency: Local search <2s, with registries <5s (P99)
- [ ] Iteration: Refined query (`"from last month"`) reuses context from previous query
- [ ] LMQL: Local model normalizer produces valid structured output (hard constraints)
- [ ] Instructor: API model normalizer succeeds with retry on schema failure
- [ ] Multi-modal: Same query via CLI, REST, gRPC → identical results

---

## Related Stories

- [structured-rsql-query](./structured-rsql-query.md) — Deterministic structured queries
- [multi-strategy-search-execution](./multi-strategy-search-execution.md) — Strategy selection logic
- [apply-focus-profile-to-search](./apply-focus-profile-to-search.md) — Profile-based filtering
- [federated-registry-search](./federated-registry-search.md) — External knowledge sources
- [text-capture-minimal-friction](../ingestion/text-capture-minimal-friction.md) — Content to search
