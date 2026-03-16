# US-0014: Constrain Extraction with LMQL

**System Types:** dpkms (self-hosted)
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As a developer or operator, I want to use LMQL (Language Model Query Language) to enforce hard constraints on extracted data so no post-processing validation is needed.

---

## Context

Traditional AI-backed extraction (prompt-engineering) produces unreliable output requiring fallible post-processing and validation. LMQL enables token-level constrained generation where the model is physically prevented from emitting invalid outputs. This eliminates parsing errors and ensures output is always correctly formatted and valid.

**Key tradeoff:** LMQL requires locally-hosted models (llama.cpp, HuggingFace). API-hosted models (OpenAI, Anthropic) fall back to prompt-engineering only, or use `instructor` (Pydantic validation + retry) / `outlines` (regex/schema sampling).

---

## Acceptance Criteria

- [ ] Operators can configure LMQL as AI provider with local backend
- [ ] LMQL enforces hard constraints: output shape, enum values, format, regex
- [ ] Extraction step outputs are always valid (no parsing errors)
- [ ] Multiple constraints can be combined (cross-variable constraints)
- [ ] LMQL supports entity extraction, decision extraction, tag assignment, etc.
- [ ] LMQL executes efficiently (no repeated attempts, single-pass generation)
- [ ] Fallback to API-hosted model (with reduced guarantees) if local unavailable
- [ ] Agent can verify extraction succeeded (output matches schema)

---

## Implementation Notes

### LMQL Setup

```bash
# Install LMQL
pip install lmql

# Start local LLM server (llama.cpp or HuggingFace)
# Option 1: ollama
ollama run llama3

# Option 2: llama.cpp
./llama-server -m ./llama-7b.gguf -ngl 99 --port 8080

# Option 3: HuggingFace (vLLM)
python -m vllm.entrypoints.openai.api_server \
  --model mistralai/Mistral-7B-Instruct-v0.1 \
  --port 8080
```

### Configuration

```yaml
# config.yaml
aiProvider:
  type: lmql
  backend: local
  model: llama3
  endpoint: http://localhost:8080
  timeout: 30s

  # Fallback for when local is unavailable
  fallback:
    type: instructor
    provider: openai
    model: gpt-4
    apiKey: ${OPENAI_API_KEY}
```

### LMQL Entity Extraction

```lmql
argmax
    "Extract entities from text:\n"
    f"{text}\n\n"
    "Output JSON array:\n"
    "[\n"
    for i in range(max_entities):
        "  {\n"
        '    "type": "'
        in(["person", "organization", "concept", "location", "product", "event", "project", "system", "entity"])
        '",\n'
        '    "name": "'
        gen(name_pattern)
        '",\n'
        '    "slug": "'
        gen(slug_pattern)  // lowercase, hyphenated
        '"\n'
        "  },\n" if i < max_entities - 1 else "  }\n"
    "]\n"
from
    local(model_name="llama3", port=8080)
where
    len(STOP) < 500 and
    max_tokens < 200  // Prevent too-long output
```

### LMQL Decision Extraction

```lmql
argmax
    "Extract decisions from text:\n"
    f"{text}\n\n"
    "Output JSON array:\n"
    "[\n"
    for i in range(max_decisions):
        "  {\n"
        '    "decision": "'
        gen(stop_at="\",")  // Decision text
        '",\n'
        '    "impact": "'
        in(["LOW", "MEDIUM", "HIGH"])  // Hard constraint to enum
        '",\n'
        '    "status": "'
        in(["OPEN", "RESOLVED", "SUPERSEDED"])  // Hard constraint
        '"\n'
        "  },\n" if i < max_decisions - 1 else "  }\n"
    "]\n"
from
    local(model_name="llama3", port=8080)
where
    len(STOP) < 500
```

### LMQL Tag Assignment

```lmql
argmax
    "Assign tags from vocabulary to text:\n"
    f"{text}\n\n"
    f"Allowed tags: {', '.join(allowed_tags)}\n\n"
    "Output JSON:\n"
    "{\n"
    '  "tags": [\n'
    for i in range(max_tags):
        '    "'
        in(allowed_tags)  // Hard constraint to allowed vocabulary
        '",\n' if i < max_tags - 1 else '    "' + in(allowed_tags) + '"\n'
    "  ]\n"
    "}\n"
from
    local(model_name="llama3", port=8080)
where
    len(STOP) < 300
```

### LMQL NLQ Normalization

```lmql
argmax
    "Analyze user query:\n"
    f'"{user_query}"\n\n'
    "Output JSON:\n"
    "{\n"
    '  "intent": "'
    in([
        "relationship_pattern",
        "semantic_summary",
        "temporal_entity",
        "similarity_discovery",
        "metadata_filter"
    ])
    '",\n'
    '  "entities": ['
    for i in range(3):
        '"@'
        in(["person", "organization", "concept", "location", "product", "event", "project", "system", "entity"])
        '.'
        gen(slug_pattern)
        '"'
        if i < 2: "," else ""
    "],\n"
    '  "strategies": {\n'
    '    "graph": ' + in(["true", "false"]) + ',\n'
    '    "vector": ' + in(["true", "false"]) + ',\n'
    '    "fts": ' + in(["true", "false"]) + ',\n'
    '    "metadata": ' + in(["true", "false"]) + '\n'
    "  }\n"
    "}\n"
from
    local(model_name="llama3", port=8080)
where
    len(STOP) < 400
```

### Pipeline Step Implementation

```go
type LMQLExtractionStep struct {
  lmqlEndpoint string
  model        string
  fallback     AIProvider  // instructor/outlines
}

func (s *LMQLExtractionStep) Execute(ctx ExecutionContext,
    input interface{}) (interface{}, error) {

  // Step 1: Try LMQL with hard constraints
  result, err := s.executeLMQL(ctx, input)
  if err == nil {
    return result, nil  // Success: output guaranteed valid
  }

  // Step 2: If LMQL unavailable, fall back to instructor/outlines
  if s.fallback != nil {
    result, err := s.fallback.Call(ctx, prompt, constraints)
    if err == nil {
      return result, nil
    }
  }

  return nil, err
}

func (s *LMQLExtractionStep) executeLMQL(ctx ExecutionContext,
    input interface{}) (interface{}, error) {

  // Call LMQL runtime with constraint script
  client := lmql.NewClient(s.lmqlEndpoint)

  result, err := client.Execute(ctx, &lmql.ExecutionRequest{
    Script:  entityExtractionScript,  // LMQL program
    Model:   s.model,
    Inputs:  map[string]string{"text": input.(string)},
    Timeout: 30 * time.Second,
  })

  if err != nil {
    return nil, fmt.Errorf("LMQL execution failed: %w", err)
  }

  // Parse result: guaranteed to match schema (hard constraints)
  var entities []Entity
  if err := json.Unmarshal([]byte(result.Output), &entities); err != nil {
    return nil, err  // Should never happen with hard constraints
  }

  return entities, nil
}
```

### Fallback to Instructor (API)

```python
from pydantic import BaseModel, Field
from typing import List, Literal
from instructor import Instructor
import openai

class Entity(BaseModel):
    type: Literal["person", "organization", "concept", "location", "product", "event", "project", "system", "entity"]
    name: str
    slug: str = Field(..., pattern=r"^[a-z0-9-]+$")

class EntityList(BaseModel):
    entities: List[Entity]

# Create instructor client with auto-retry
client = Instructor(openai.OpenAI())

# instructor automatically:
# 1. Validates output against schema
# 2. Retries if validation fails (up to max_retries)
# 3. Raises on final failure
response = client.messages.create(
    model="gpt-4",
    messages=[{"role": "user", "content": prompt}],
    response_model=EntityList,
    max_retries=3,
)

# Result is guaranteed to match schema
entities = response.entities
```

### Monitoring & Observability

```
# Prometheus metrics
ch_lmql_executions_total{step="entity-extraction"}
ch_lmql_success_rate{step="entity-extraction"}
ch_lmql_duration_seconds{step="entity-extraction"}
ch_lmql_constraint_violations{step="entity-extraction"}  // Should be 0

ch_fallback_invocations_total{from="lmql",to="instructor"}
ch_fallback_success_rate{from="lmql",to="instructor"}
```

---

## E2E Test Checklist

- [ ] Config: `aiProvider.type: lmql` and `aiProvider.endpoint` are present in the config file sent to the server on startup
- [ ] Config: `aiProvider.fallback.type` (e.g., `instructor`) is present in the config when a fallback is configured
- [ ] CLI: Enrichment command with `--ai-provider lmql` sends `ai_provider: "lmql"` in the request payload (verified via request capture or server log)
- [ ] CLI: Enrichment command with `--ai-provider instructor` sends `ai_provider: "instructor"` in the request payload
- [ ] Server: Enrichment request body contains `step` and `ai_provider` fields for every LMQL-backed enrichment call
- [ ] Server: Response contains extraction results matching the schema for the requested step (entities, decisions, or tags)
- [ ] Storage: GET `/objects/{object_id}` after LMQL enrichment returns object with the relevant enrichment field populated (`mentions`, `enrichment.decisions`, or `tags`)
- [ ] Storage: `enrichment.extraction_method` on the stored object equals `"lmql"` when LMQL was used
- [ ] Storage: `enrichment.extraction_method` on the stored object equals `"instructor"` when the fallback was used
- [ ] LMQL: Local LLM server running at configured endpoint
- [ ] LMQL: Entity extraction succeeds without post-processing
- [ ] LMQL: All extracted entities match `@type.slug` format
- [ ] LMQL: Decision extraction respects impact/status enum constraints
- [ ] LMQL: Tag assignment constrained to vocabulary set
- [ ] LMQL: No invalid outputs produced (hard constraints enforce)
- [ ] LMQL: Execution completes within timeout
- [ ] Fallback: If LMQL unavailable, falls back to instructor (verified by taking LMQL endpoint offline)
- [ ] Fallback: Instructor retries on schema validation failure
- [ ] Fallback: Fallback results are still valid (but with less guarantees)
- [ ] Determinism: Same input + same LMQL model → same output
- [ ] Performance: LMQL extraction faster than API-based (local latency)
- [ ] Metrics: Prometheus metrics show execution count and duration
- [ ] Metrics: Success rate is 100% (hard constraints)

---

## Related Stories

- [US-0009](./US-0009-extract-entities-and-mentions.md) — Extraction pipeline
- [US-0011](./US-0011-assign-tags-from-vocabulary.md) — Tag assignment with constraints
- [US-0027](../admin/US-0027-configure-ai-provider.md) — LMQL provider configuration
- [US-0041](../agents/US-0041-agent-uses-constrained-enrichment.md) — Agent perspective
- [US-0016](../search/US-0016-natural-language-search.md) — NLQ normalization with LMQL
