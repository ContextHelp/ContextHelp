---
status: paper
---

# US-0048: Detect Sentiment And Tone

**System Types:** dpkms (self-hosted)
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an AI system or knowledge worker, I want the sentiment and emotional tone of captured content to be detected and stored so I can filter by tone and surface urgent or negative signals.

---

## Context

Meeting notes, feedback, and communications carry emotional signals that are otherwise invisible in a knowledge base. Detecting sentiment (positive/neutral/negative) and tone (urgent, confident, uncertain, frustrated) allows agents and search to surface high-signal items and filter noise.

---

## Acceptance Criteria

- [ ] Sentiment is classified as one of: `positive`, `neutral`, `negative`
- [ ] Tone is classified into one or more canonical tones (e.g., `urgent`, `confident`, `uncertain`, `frustrated`, `informational`)
- [ ] A confidence score (0.0–1.0) is stored per classification
- [ ] Results are stored under `enrichment.sentiment` on the knowledge object
- [ ] Request payload includes the AI provider
- [ ] Results are persisted before the job is marked complete

---

## Implementation Notes

### CLI Interface

```bash
# Trigger sentiment detection
ctxt enrich <object_id> --step detect-sentiment --ai-provider lmql

# Returns immediately with job ID
{
  "job_id": "j-abc123",
  "object_id": "o-def456",
  "status": "pending"
}
```

### REST API Endpoint

```
POST /enrich/{object_id}/detect-sentiment
Content-Type: application/json

{
  "step": "detect-sentiment",
  "ai_provider": "lmql"
}

→ 200 OK
{
  "object_id": "o-def456",
  "sentiment": {
    "label": "negative",
    "confidence": 0.87
  },
  "tones": [
    { "label": "urgent", "confidence": 0.91 },
    { "label": "frustrated", "confidence": 0.74 }
  ]
}
```

### Knowledge Object Update

```json
{
  "id": "o-def456",
  "enrichment": {
    "sentiment": {
      "label": "negative",
      "confidence": 0.87
    },
    "tones": [
      { "label": "urgent", "confidence": 0.91 }
    ],
    "sentiment_detected_at": "2025-01-18T10:30:45Z",
    "detection_method": "lmql"
  }
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich <object_id> --step detect-sentiment --ai-provider lmql` exits 0
- [ ] CLI: `--step detect-sentiment` flag is present in the request payload sent to server as `step` field (verified via request capture or server log)
- [ ] CLI: `--ai-provider lmql` flag is present in the request payload as `ai_provider` field
- [ ] Server: POST `/enrich/{object_id}/detect-sentiment` receives `step` and `ai_provider` in request body
- [ ] Server: Response contains `sentiment` object with `label` and `confidence` fields
- [ ] Server: Response contains `tones` array with `label` and `confidence` per entry
- [ ] Server: `sentiment.label` is constrained to `positive`, `neutral`, or `negative`
- [ ] Server: `tones[*].label` values are constrained to the canonical tone vocabulary
- [ ] Server: `confidence` values are in the range 0.0–1.0
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.sentiment` populated
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.tones` populated
- [ ] Storage: `enrichment.sentiment_detected_at` timestamp is set on the stored object
- [ ] Storage: `enrichment.detection_method` matches the `ai_provider` value sent in request
- [ ] Constraint: Invalid `sentiment.label` value is never returned (LMQL hard constraint or Pydantic enum)
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0014](./US-0014-constrain-extraction-with-lmql.md) — LMQL constraint enforcement
- [US-0009](./US-0009-extract-entities-and-mentions.md) — Entity extraction step

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

- planned: `test/integration/us0048_sentiment_test.go::TestSentiment_PositiveText`
- planned: `test/integration/us0048_sentiment_test.go::TestSentiment_NegativeText`
- planned: `test/integration/us0048_sentiment_test.go::TestSentiment_NeutralText`
- planned: `test/integration/us0048_sentiment_test.go::TestSentiment_ToneClassification`
