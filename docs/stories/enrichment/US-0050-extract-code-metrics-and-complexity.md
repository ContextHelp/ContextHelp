---
status: paper
---

# US-0050: Extract Code Metrics And Complexity

**System Types:** dpkms (self-hosted)
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As an AI system or platform integrator, I want code snippets in captured content to be analyzed for complexity and quality metrics so I can surface high-complexity code that may need review.

---

## Context

Code snippets extracted via US-0013 are raw text. Adding metrics (cyclomatic complexity, lines of code, nesting depth, function count) gives the knowledge base a signal for code quality and enables agents to flag complex or risky snippets during composition and review workflows.

---

## Acceptance Criteria

- [ ] Metrics are computed for each code snippet on the object (requires US-0013 to have run first)
- [ ] Each snippet's metrics include: `lines_of_code`, `cyclomatic_complexity`, `max_nesting_depth`, `function_count`
- [ ] Metrics are stored per-snippet under `enrichment.code_snippets[*].metrics`
- [ ] Request payload includes the object ID and optionally a language override
- [ ] Metrics are persisted before the job is marked complete

---

## Implementation Notes

### CLI Interface

```bash
# Trigger code metrics extraction (requires snippets already extracted)
ctxt enrich <object_id> --step extract-code-metrics

# Returns immediately with job ID
{
  "job_id": "j-abc123",
  "object_id": "o-def456",
  "status": "pending"
}
```

### REST API Endpoint

```
POST /enrich/{object_id}/code-metrics
Content-Type: application/json

{
  "step": "extract-code-metrics"
}

→ 200 OK
{
  "object_id": "o-def456",
  "snippets_analyzed": 1,
  "code_snippets": [
    {
      "position": 42,
      "language": "go",
      "metrics": {
        "lines_of_code": 15,
        "cyclomatic_complexity": 3,
        "max_nesting_depth": 2,
        "function_count": 1
      }
    }
  ]
}
```

### Knowledge Object Update

```json
{
  "id": "o-def456",
  "enrichment": {
    "code_snippets": [
      {
        "position": 42,
        "language": "go",
        "body": "func main() { ... }",
        "metrics": {
          "lines_of_code": 15,
          "cyclomatic_complexity": 3,
          "max_nesting_depth": 2,
          "function_count": 1
        }
      }
    ],
    "code_metrics_extracted_at": "2025-01-18T10:30:45Z"
  }
}
```

---

## E2E Test Checklist

- [ ] Prerequisite: US-0013 (detect-code-snippets) must have run on the object before this step
- [ ] CLI: `ctxt enrich <object_id> --step extract-code-metrics` exits 0
- [ ] CLI: `--step extract-code-metrics` flag is present in the request payload sent to server as `step` field (verified via request capture or server log)
- [ ] Server: POST `/enrich/{object_id}/code-metrics` receives `step` in request body
- [ ] Server: Response contains `code_snippets` array with `metrics` object per entry
- [ ] Server: Each `metrics` object contains `lines_of_code`, `cyclomatic_complexity`, `max_nesting_depth`, and `function_count`
- [ ] Server: `snippets_analyzed` in response matches the count of snippets with metrics computed
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.code_snippets[*].metrics` populated
- [ ] Storage: `enrichment.code_metrics_extracted_at` timestamp is set on the stored object
- [ ] Prerequisite error: Running this step before US-0013 on an object with no snippets returns empty `code_snippets` (not an error)
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0013](./US-0013-detect-and-extract-code-snippets.md) — Prerequisite: code snippet detection
- [US-0009](./US-0009-extract-entities-and-mentions.md) — Entity extraction step

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

- planned: `test/integration/us0050_code_metrics_test.go::TestCodeMetrics_LineCounts`
- planned: `test/integration/us0050_code_metrics_test.go::TestCodeMetrics_CyclomaticComplexity`
- planned: `test/integration/us0050_code_metrics_test.go::TestCodeMetrics_LanguageDetection`
- planned: `test/integration/us0050_code_metrics_test.go::TestCodeMetrics_FunctionInventory`
