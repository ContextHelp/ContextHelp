# Story: Agent Discovers Query Schema

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As an autonomous agent, I want to discover the query schema on startup so I can understand what properties and operators are available for constructing queries.

---

## Context

Agents need to bootstrap knowledge about the system's queryable properties, available operators, and usage examples. The `/query-schema` endpoint provides this contract, enabling agents to construct deterministic RSQL queries without prior domain knowledge.

---

## Acceptance Criteria

- [ ] Agent can invoke `GET /query-schema` endpoint
- [ ] Endpoint returns schema with: properties, operators, examples
- [ ] Properties include: name, type, constraints, description
- [ ] Operators include: ==, !=, <, >, <=, >=, =in=, =out=, ;, ,
- [ ] Examples include: natural language intent + RSQL query
- [ ] Schema is versioned (allows for evolution)
- [ ] Agent can cache schema (stable across runs)
- [ ] Schema response includes MIME type (application/json)
- [ ] Agent handles schema fetch timeout gracefully (> 5 seconds)

---

## Implementation Notes

### REST Endpoint: `/query-schema`

```
GET /query-schema
Accept: application/json

→ 200 OK
Content-Type: application/json

{
  "version": "1.0",
  "lastUpdated": "2025-01-18T00:00:00Z",
  "properties": [
    {
      "name": "type",
      "type": "enum",
      "values": [
        "text", "url", "image", "video", "document",
        "decision", "task", "concept"
      ],
      "indexed": true,
      "description": "object type classification",
      "examples": [
        { "usage": "type==article", "description": "find articles" },
        { "usage": "type==decision", "description": "find decisions" }
      ]
    },
    {
      "name": "tags",
      "type": "string_array",
      "indexed": true,
      "indexing_strategy": "json",
      "description": "user-defined tags (comma-separated)",
      "examples": [
        { "usage": "tags=in=recommended,important", "description": "any of these tags" }
      ]
    },
    {
      "name": "created_at",
      "type": "timestamp",
      "indexed": true,
      "description": "creation date (ISO 8601)",
      "examples": [
        { "usage": "created_at>2025-01-01", "description": "after date" },
        { "usage": "created_at<2025-01-31", "description": "before date" }
      ]
    },
    {
      "name": "mentions",
      "type": "string_array",
      "pattern": "@[a-z0-9]+\\.[a-z0-9-]+",
      "indexed": true,
      "description": "entity mentions (@type.slug format)",
      "examples": [
        { "usage": "mentions=in=@person.alice,@project.mobile", "description": "mentions entities" }
      ]
    },
    {
      "name": "pipeline",
      "type": "enum",
      "values": [
        "text.short", "text.long",
        "url.article", "url.repository",
        "image.ocr", "image.diagram",
        "audio.transcription",
        "video.transcription",
        "document.pdf", "document.markdown",
        "feed.item"
      ],
      "indexed": true,
      "description": "processing pipeline used"
    }
  ],
  "operators": [
    {
      "symbol": "==",
      "name": "equality",
      "usage": "property==value",
      "example": "type==article",
      "supported_types": ["enum", "string", "number", "timestamp"]
    },
    {
      "symbol": "!=",
      "name": "inequality",
      "usage": "property!=value",
      "example": "type!=draft",
      "supported_types": ["enum", "string"]
    },
    {
      "symbol": "<",
      "name": "less_than",
      "usage": "property<value",
      "example": "created_at<2025-01-31",
      "supported_types": ["number", "timestamp"]
    },
    {
      "symbol": ">",
      "name": "greater_than",
      "usage": "property>value",
      "example": "created_at>2025-01-01",
      "supported_types": ["number", "timestamp"]
    },
    {
      "symbol": "<=",
      "name": "less_than_or_equal",
      "usage": "property<=value",
      "supported_types": ["number", "timestamp"]
    },
    {
      "symbol": ">=",
      "name": "greater_than_or_equal",
      "usage": "property>=value",
      "supported_types": ["number", "timestamp"]
    },
    {
      "symbol": "=in=",
      "name": "any_of",
      "usage": "property=in=value1,value2",
      "example": "tags=in=recommended,important",
      "supported_types": ["string_array", "enum"]
    },
    {
      "symbol": "=out=",
      "name": "none_of",
      "usage": "property=out=value1,value2",
      "example": "tags=out=draft",
      "supported_types": ["string_array"]
    },
    {
      "symbol": ";",
      "name": "logical_and",
      "usage": "condition1;condition2",
      "example": "type==article;tags=in=recommended",
      "description": "both conditions must be true"
    },
    {
      "symbol": ",",
      "name": "logical_or",
      "usage": "condition1,condition2",
      "example": "type==article,type==decision",
      "description": "either condition can be true"
    }
  ],
  "examples": [
    {
      "intent": "find recent recommended articles",
      "rsql": "type==article;tags=in=recommended;created_at>2025-01-01",
      "explanation": "type matches article AND has tag 'recommended' AND created after Jan 1"
    },
    {
      "intent": "find all decisions by Alice or Bob",
      "rsql": "type==decision;mentions=in=@person.alice,@person.bob",
      "explanation": "type is decision AND mentions Alice or Bob"
    },
    {
      "intent": "find engineering content excluding drafts",
      "rsql": "mentions=in=@project.backend;tags=out=draft",
      "explanation": "mentions backend project AND not tagged as draft"
    }
  ],
  "constraints": {
    "max_query_length": 1000,
    "max_results": 10000,
    "timeout_ms": 5000
  },
  "deprecations": []
}
```

### gRPC Endpoint: `GetQuerySchema`

```protobuf
service QueryService {
  rpc GetQuerySchema(Empty) returns (QuerySchema);
}

message QuerySchema {
  string version = 1;
  int64 last_updated = 2;  // Unix timestamp
  repeated Property properties = 3;
  repeated Operator operators = 4;
  repeated Example examples = 5;
  Constraints constraints = 6;
}

message Property {
  string name = 1;
  string type = 2;
  repeated string values = 3;  // For enum types
  bool indexed = 4;
  string description = 5;
  repeated Example examples = 6;
}

message Operator {
  string symbol = 1;
  string name = 2;
  string usage = 3;
  string example = 4;
  repeated string supported_types = 5;
}

message Example {
  string intent = 1;
  string rsql = 2;
  string explanation = 3;
}

message Constraints {
  int32 max_query_length = 1;
  int32 max_results = 2;
  int32 timeout_ms = 3;
}
```

### Agent Bootstrap Logic

```python
import requests
import json
from typing import Optional, Dict

class QueryAgent:
  def __init__(self, api_url: str):
    self.api_url = api_url
    self.schema: Optional[Dict] = None
    self.schema_cached_at: Optional[float] = None

  def discover_schema(self, force_refresh=False):
    """Fetch query schema on startup (cached 1 hour)"""
    import time
    current_time = time.time()

    # Use cached schema if recent
    if self.schema and not force_refresh:
      cache_age_seconds = current_time - self.schema_cached_at
      if cache_age_seconds < 3600:  # 1 hour
        return self.schema

    # Fetch fresh schema
    try:
      response = requests.get(
        f"{self.api_url}/query-schema",
        timeout=5,
        headers={"Accept": "application/json"}
      )
      response.raise_for_status()
      self.schema = response.json()
      self.schema_cached_at = current_time
      return self.schema
    except requests.RequestException as e:
      if self.schema:
        # Fallback to cached schema on error
        return self.schema
      raise Exception(f"Failed to discover query schema: {e}")

  def validate_rsql(self, rsql: str) -> bool:
    """Validate RSQL against schema before execution"""
    if not self.schema:
      self.discover_schema()

    # Parse RSQL to properties/operators
    # This is simplified; real implementation would parse fully
    max_len = self.schema["constraints"]["max_query_length"]
    if len(rsql) > max_len:
      return False

    # Could validate operators exist in schema
    valid_operators = [op["symbol"] for op in self.schema["operators"]]
    # ... more validation

    return True

  def list_queryable_properties(self):
    """Inform user what can be queried"""
    if not self.schema:
      self.discover_schema()

    properties = self.schema["properties"]
    print("Queryable properties:")
    for prop in properties:
      print(f"  - {prop['name']} ({prop['type']}): {prop['description']}")
      if prop.get('values'):
        print(f"    Values: {', '.join(prop['values'])}")

# Usage
agent = QueryAgent("http://localhost:8080")
schema = agent.discover_schema()

# Now agent knows what's queryable
properties = agent.list_queryable_properties()
```

### Caching Strategy

```python
# Cache schema locally with TTL
schema_cache = {
  "data": None,
  "cached_at": None,
  "ttl_seconds": 3600
}

def get_schema_cached(api_url):
  import time
  current_time = time.time()

  # Return cached if fresh
  if schema_cache["data"] and schema_cache["cached_at"]:
    age = current_time - schema_cache["cached_at"]
    if age < schema_cache["ttl_seconds"]:
      return schema_cache["data"]

  # Fetch and cache
  response = requests.get(f"{api_url}/query-schema", timeout=5)
  schema = response.json()

  schema_cache["data"] = schema
  schema_cache["cached_at"] = current_time

  return schema
```

---

## E2E Test Checklist

- [ ] Request: GET /query-schema request is sent with `Accept: application/json` header
- [ ] Endpoint: GET /query-schema returns 200 OK
- [ ] Endpoint: Response `Content-Type` header is `application/json`
- [ ] Endpoint: Response body includes `version` field matching documented value ("1.0")
- [ ] Endpoint: Response body includes `lastUpdated` field in ISO 8601 format
- [ ] Endpoint: Response body includes `properties` array with all required fields: `type`, `tags`, `created_at`, `mentions`, `pipeline`
- [ ] Endpoint: Each property object includes `name`, `type`, `indexed`, `description` fields
- [ ] Endpoint: Response body includes `operators` array covering all documented operators: `==`, `!=`, `<`, `>`, `<=`, `>=`, `=in=`, `=out=`, `;`, `,`
- [ ] Endpoint: Each operator object includes `symbol`, `name`, `usage`, `example`, `supported_types` fields
- [ ] Endpoint: Response body includes `examples` array, each entry containing `intent`, `rsql`, `explanation`
- [ ] Endpoint: Response body includes `constraints` object with `max_query_length`, `max_results`, `timeout_ms`
- [ ] Endpoint: Response body includes `deprecations` array (may be empty)
- [ ] Endpoint: Schema `version` field changes when new properties are added (server-side versioning validated)
- [ ] Timeout: Server responds within 5 seconds (validated against `constraints.timeout_ms`)
- [ ] Agent: Sends GET /query-schema on startup before constructing any query
- [ ] Agent: Caches schema locally; second call within TTL does not issue a new HTTP request
- [ ] Agent: After TTL expiry, agent re-fetches schema from server
- [ ] Agent: Can validate RSQL string against schema (rejects query with unknown property)
- [ ] Agent: Can enumerate queryable properties from response (produces list of `name`+`type` pairs)
- [ ] gRPC: GetQuerySchema RPC returns equivalent `version`, `properties`, `operators`, `examples`, and `constraints` as REST endpoint
- [ ] Error: Agent falls back to cached schema when server is unreachable (no panic, stale schema used)

---

## Related Stories

- [agent-constructs-rsql-query](./agent-constructs-rsql-query.md) — Using schema to build queries
- [structured-rsql-query](../search/structured-rsql-query.md) — RSQL query execution
- [natural-language-search](../search/natural-language-search.md) — NLQ as alternative to RSQL

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

> Not yet implemented.
