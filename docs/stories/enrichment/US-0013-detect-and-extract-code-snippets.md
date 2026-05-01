---
status: shipped
---

# US-0013: Detect And Extract Code Snippets

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As an AI system or platform integrator, I want code snippets embedded in captured content to be automatically detected, extracted, and stored with language metadata so they are searchable and reusable.

---

## Context

Notes, documentation, and meeting transcripts often contain inline code. Detecting and extracting these snippets enables code search, syntax highlighting, language-specific indexing, and code-aware enrichment (metrics, complexity). Each snippet needs a language label and its original position in the source.

---

## Acceptance Criteria

- [ ] Code snippets are detected from fenced code blocks and inline code
- [ ] Each snippet is labeled with a detected language (e.g., `go`, `python`, `bash`, `unknown`)
- [ ] Snippets are stored as structured entries under `enrichment.code_snippets` on the object
- [ ] Each snippet entry contains `language`, `body`, and `position` fields
- [ ] Request payload includes the detection strategy (heuristic or AI-assisted)
- [ ] Snippets are persisted before the job is marked complete

---

## Implementation Notes

### CLI Interface

```bash
# Trigger code snippet detection and extraction
ctxt enrich <object_id> --step detect-code-snippets --detection heuristic

# Returns immediately with job ID
{
  "job_id": "j-abc123",
  "object_id": "o-def456",
  "status": "pending"
}
```

### REST API Endpoint

```
POST /enrich/{object_id}/detect-code
Content-Type: application/json

{
  "step": "detect-code-snippets",
  "detection": "heuristic"
}

→ 200 OK
{
  "object_id": "o-def456",
  "code_snippets": [
    {
      "position": 42,
      "language": "go",
      "body": "func main() {\n  fmt.Println(\"hello\")\n}"
    }
  ],
  "snippets_found": 1
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
        "body": "func main() {\n  fmt.Println(\"hello\")\n}"
      }
    ],
    "code_extracted_at": "2025-01-18T10:30:45Z",
    "detection_method": "heuristic",
    "snippets_found": 1
  }
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich <object_id> --step detect-code-snippets --detection heuristic` exits 0
- [ ] CLI: `--step detect-code-snippets` flag is present in the request payload sent to server (verified via request capture or server log)
- [ ] CLI: `--detection heuristic` flag is present in the request payload as `detection` field
- [ ] Server: POST `/enrich/{object_id}/detect-code` receives `step` and `detection` in request body
- [ ] Server: Response contains `code_snippets` array with `language`, `body`, and `position` fields per entry
- [ ] Server: `snippets_found` in response matches the actual count in `code_snippets` array
- [ ] Language Detection: Fenced code blocks with explicit language labels produce correct `language` value
- [ ] Language Detection: Fenced code blocks without labels produce `language: "unknown"` (not an error)
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.code_snippets` populated
- [ ] Storage: `enrichment.code_extracted_at` timestamp is set on the stored object
- [ ] Storage: `enrichment.detection_method` matches the `detection` value sent in request
- [ ] Content without code: Returns empty `code_snippets` array (not an error)
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0009](./US-0009-extract-entities-and-mentions.md) — Entity extraction step
- [US-0050](./US-0050-extract-code-metrics-and-complexity.md) — Code metrics derived from extracted snippets

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

- `test/integration/us0013_code_extraction_test.go::TestUS0013_FencedCodeBlocksExtracted`
- `test/integration/us0013_code_extraction_test.go::TestUS0013_CodeObjectsCreatedWithLanguageTag`
- `test/integration/us0013_code_extraction_test.go::TestUS0013_UnlabeledFencedBlocksGetUnknownLanguage`
- `test/integration/us0013_code_extraction_test.go::TestUS0013_NoCodeContentReturnsEmptySnippets`
- `test/integration/us0013_code_extraction_test.go::TestUS0013_DetectionMethodRecordedInMetadata`
