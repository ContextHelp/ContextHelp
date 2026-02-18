# Story: Agent Constructs RSQL Query

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As an autonomous agent, I want to construct deterministic, explicit RSQL queries to reliably retrieve knowledge based on structured criteria.

---

## Context

Agents require reproducible query results for reliable decision-making. RSQL (Relational String Query Language) provides explicit, deterministic query syntax that eliminates ambiguity of natural language. Agents should fetch the query schema, understand available properties/operators, and construct queries for their specific use cases.

---

## Acceptance Criteria

- [ ] Agent can retrieve `/query-schema` endpoint on startup
- [ ] Query schema includes: available properties, operators, examples, constraints
- [ ] Agent constructs valid RSQL queries based on schema
- [ ] Same RSQL query always returns results in same order (deterministic)
- [ ] RSQL queries are cacheable (same query → cache hit)
- [ ] Results include metadata (pipeline, source, timestamp) for auditing
- [ ] Agent handles empty results gracefully (no error, just empty array)
- [ ] Agent can combine multiple criteria with AND (`;`) and OR (`,`) operators
- [ ] Agent can retry failed queries with backoff
- [ ] Query execution is fast (<500ms for local, <2s with registries)

---

## Implementation Notes

### Query Schema Endpoint

```
GET /query-schema

→ 200 OK
{
  "properties": [
    {
      "name": "type",
      "type": "enum",
      "values": ["text", "url", "image", "video", "document", "decision", "task"],
      "description": "object type classification"
    },
    {
      "name": "tags",
      "type": "string_array",
      "indexing": "json",
      "description": "user-defined tags with weights"
    },
    {
      "name": "created_at",
      "type": "timestamp",
      "indexing": "indexed",
      "description": "creation timestamp"
    },
    {
      "name": "mentions",
      "type": "string_array",
      "pattern": "@namespace.slug",
      "description": "extracted entity mentions"
    },
    {
      "name": "pipeline",
      "type": "string",
      "values": [
        "text.short", "text.long",
        "url.article", "url.repository",
        "image.ocr", "image.diagram",
        "audio.transcription",
        "video.transcription",
        "document.pdf", "document.markdown"
      ],
      "description": "processing pipeline used"
    },
    {
      "name": "created_at",
      "type": "timestamp",
      "description": "creation date"
    }
  ],
  "operators": [
    {
      "name": "==",
      "description": "equality match",
      "example": "type==article"
    },
    {
      "name": "!=",
      "description": "inequality",
      "example": "type!=draft"
    },
    {
      "name": "<, >, <=, >=",
      "description": "range comparison (timestamps, numbers)",
      "example": "created_at>2025-01-01"
    },
    {
      "name": "=in=",
      "description": "any of (array membership)",
      "example": "tags=in=recommended,important"
    },
    {
      "name": "=out=",
      "description": "none of (exclusion)",
      "example": "tags=out=draft"
    },
    {
      "name": ";",
      "description": "logical AND",
      "example": "type==article;tags=in=recommended"
    },
    {
      "name": ",",
      "description": "logical OR",
      "example": "type==article,type==decision"
    },
    {
      "name": "similar",
      "description": "semantic similarity",
      "example": "similar==error handling patterns"
    }
  ],
  "examples": [
    {
      "intent": "recent recommended articles",
      "rsql": "type==article;tags=in=recommended;created_at>2025-01-01"
    },
    {
      "intent": "decisions made by engineering",
      "rsql": "type==decision;pipeline==text.long;mentions=in=@person.alice,@person.bob"
    },
    {
      "intent": "content on authentication or security",
      "rsql": "mentions=in=@concept.authentication,@concept.security"
    }
  ]
}
```

### Agent Query Construction

```python
# Pseudo-code: Agent constructs query based on schema

class QueryAgent:
  def __init__(self, api_client):
    self.client = api_client
    self.schema = self.client.get("/query-schema")
    self.operators = {op["name"]: op for op in self.schema["operators"]}
    self.properties = {p["name"]: p for p in self.schema["properties"]}

  def build_query(self, intent: str) -> str:
    """
    Example intent: "Find all articles tagged 'recommended' from the last month"
    """
    # Step 1: Parse intent semantically (or use hardcoded logic)
    criteria = {
      "type": "article",
      "tags": "recommended",
      "created_after": "2025-01-01"
    }

    # Step 2: Validate against schema
    assert self.properties["type"]["name"] in self.schema["properties"]
    assert "article" in self.properties["type"]["values"]

    # Step 3: Construct RSQL
    rsql_parts = []
    rsql_parts.append("type==article")
    rsql_parts.append("tags=in=recommended")
    rsql_parts.append("created_at>2025-01-01")

    rsql = ";".join(rsql_parts)  # AND all conditions
    return rsql

  def execute_query(self, rsql: str) -> dict:
    """Execute with caching and retry logic"""
    cache_key = f"rsql:{rsql}"

    # Check cache first
    cached = self.cache.get(cache_key)
    if cached:
      return cached

    # Execute with backoff retry
    for attempt in range(3):
      try:
        response = self.client.get("/search", params={"q": rsql, "query_mode": "rsql"})
        results = response.json()

        # Cache successful results (1 hour TTL)
        self.cache.set(cache_key, results, ttl=3600)
        return results
      except Exception as e:
        if attempt < 2:
          time.sleep(2 ** attempt)  # Exponential backoff
        else:
          raise

# Usage
agent = QueryAgent(api_client)
rsql = agent.build_query("Find all articles tagged 'recommended' from the last month")
results = agent.execute_query(rsql)

# Example RSQL:
# type==article;tags=in=recommended;created_at>2025-01-01
```

### REST API: Structured Query Execution

```
GET /search?q=type%3D%3Darticle%3Btags%3Din%3Drecommended%3Bcreated_at%3E2025-01-01&query_mode=rsql

→ 200 OK
{
  "query": "type==article;tags=in=recommended;created_at>2025-01-01",
  "query_mode": "rsql",
  "total": 42,
  "results": [
    {
      "id": "o-abc123",
      "type": "article",
      "summary": "Best practices for error handling",
      "tags": ["recommended", "important"],
      "created_at": "2025-01-15T10:30:45Z",
      "pipeline": "url.article",
      "source": "https://example.com/article"
    },
    ...
  ],
  "took_ms": 234
}
```

### gRPC: Structured Query

```protobuf
message SearchRequest {
  string query = 1;  // "type==article;tags=in=recommended"
  string query_mode = 2;  // "rsql"
  int32 limit = 3;
  int32 offset = 4;
}

message SearchResponse {
  string query = 1;
  repeated SearchResult results = 2;
  int32 total = 3;
  int32 took_ms = 4;
}
```

### Query Caching Strategy

- Cache key: `rsql:{query_string}:{profile_context}`
- TTL: 1 hour (configurable)
- Invalidation: On any write to knowledge base (new object, enrichment completion)
- Hit rate tracking: Helps identify most-used queries

### Retry Strategy

```python
def execute_with_retry(query, max_attempts=3):
  for attempt in range(max_attempts):
    try:
      return execute(query)
    except TransientError:
      wait_time = 2 ** attempt  # 1s, 2s, 4s
      sleep(wait_time)
    except PermanentError:
      raise  # Don't retry

  raise QueryFailedError(f"Failed after {max_attempts} attempts")
```

---

## E2E Test Checklist

- [ ] Agent: GET /query-schema returns valid schema with properties, operators, examples
- [ ] Agent: Constructs RSQL for "find recent articles" intent
- [ ] Agent: Constructs RSQL for "find decisions by person X" intent
- [ ] Query: RSQL execution returns results in deterministic order
- [ ] Query: Same RSQL executed twice → identical results (deterministic)
- [ ] Query: Results include all metadata (pipeline, source, created_at)
- [ ] Query: Empty results handled gracefully (no error)
- [ ] Query: Complex query with AND/OR operators works correctly
- [ ] Caching: Second query for same RSQL uses cache (verified by latency)
- [ ] Retry: Failed query retried with exponential backoff
- [ ] Error Handling: Invalid RSQL returns 400 Bad Request with error message
- [ ] Performance: Query latency <500ms (P99) for local storage
- [ ] Performance: Query latency <2s (P99) with federated registries

---

## Related Stories

- [agent-discovers-query-schema](./agent-discovers-query-schema.md) — Fetch query schema on startup
- [natural-language-search](../search/natural-language-search.md) — NLQ alternative for comparison
- [structured-rsql-query](../search/structured-rsql-query.md) — RSQL syntax and operators
- [multi-strategy-search-execution](../search/multi-strategy-search-execution.md) — Query execution details
