# US-0001: Text Capture with Minimal Friction

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to capture a text insight or note in seconds without interrupting my workflow.

---

## Context

Knowledge workers constantly encounter insights, decisions, and important information in their daily work. The barrier to capturing these should be negligible — ideally under 10 seconds. Friction (complex UIs, required categorization, waiting for processing) leads to lost insights.

---

## Acceptance Criteria

- [ ] User can invoke capture interface (CLI, TUI, or browser) with one keystroke/click
- [ ] User types or pastes text content (no minimum length)
- [ ] System accepts input immediately and returns acknowledgment within 1 second
- [ ] User is not blocked waiting for enrichment (async processing)
- [ ] User can optionally apply focus profile at capture time (`--profile engineering`)
- [ ] System returns job ID for tracking async progress
- [ ] Captured text appears in knowledge base within 30 seconds (enriched) or immediately (indexed)
- [ ] Submitted `profile` and `project` values are persisted in the stored object's Metadata and are retrievable via `GET /objects/{object_id}`

---

## Implementation Notes

### CLI Interface
```bash
# Most basic: type directly
ctxt add "Here's an insight about error handling patterns"

# From stdin
echo "Insight text" | ctxt add

# From file
ctxt add --file ~/notes.txt

# With focus profile
ctxt add "Text here" --profile engineering --project mobile-app

# Returns immediately with JSON
{
  "job_id": "j-abc123xyz",
  "object_id": "o-def456",
  "status": "pending_enrichment",
  "pipeline": "text.short",
  "will_enrich_by": "2025-01-18T10:30:45Z"
}
```

### TUI Interface
- Modal dialog: paste/type text
- Optional profile selector dropdown
- Confirm button
- Returns to main screen with "Captured!" toast notification

### Browser Extension
- Highlight text on webpage
- Right-click context menu → "Capture to ctxt"
- Optional profile selector in popup
- Confirmation badge

### REST API
```
POST /analyze
Content-Type: application/json

{
  "content": "Text content here",
  "content_type": "text/plain",
  "profile": "engineering",
  "project": "mobile-app"
}

→ 202 Accepted
{
  "job_id": "j-abc123xyz",
  "object_id": "o-def456",
  "status": "pending_enrichment",
  "pipeline": "text.short"
}
```

### gRPC Interface
```protobuf
service Analyze {
  rpc AnalyzeContent(AnalyzeRequest) returns (AnalyzeResponse);
}

message AnalyzeRequest {
  string content = 1;
  string content_type = 2;
  string profile = 3;  // optional
  string project = 4;  // optional
}

message AnalyzeResponse {
  string job_id = 1;
  string object_id = 2;
  string status = 3;
  string pipeline = 4;
}
```

### Backend Processing
1. `ctxt` receives request, normalizes content type
2. Detects text type (short vs long, language)
3. Selects pipeline: `text.short` (default) or `text.long` (if long-form)
4. Creates Job in `jobs` table with status `pending`
5. Returns immediately (async)
6. dPKMS worker picks up job, executes pipeline:
   - Extract summary
   - Extract entities (mentions: `@person.name`, `@project.name`)
   - Extract decisions (if present)
   - Extract tasks (if present)
   - Assign tags from vocabulary (or leave empty for human review)
   - Generate embeddings
7. Create knowledge object in `objects` table
8. Update graph edges (mentions, relationships)
9. Mark job as `completed`

### Configuration
```yaml
# In configuration.yaml
nlqNormalizer:
  enabled: true
  provider: ${aiProvider}
  intentPatterns: ./config/intent-patterns.yaml

aiProvider:
  type: openai
  model: gpt-4
  apiKey: ${CH_OPENAI_API_KEY}

storage:
  type: sqlite
  path: ./data/db.sqlite
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt add "test insight"` returns job ID within 1 second
- [ ] CLI: Job status transitions from `pending` → `completed` within 30 seconds
- [ ] CLI: Retrieved object contains extracted summary (`document.body`), entities
  (`index.mentions`), and tags (`index.tags`) — all derived from graph via projections
- [ ] CLI: `ctxt add "text" --profile engineering` sends `"profile": "engineering"` in the request payload to the server
- [ ] CLI: `ctxt add "text" --profile engineering --project mobile-app` sends both `"profile": "engineering"` and `"project": "mobile-app"` in the server request payload
- [ ] CLI: With `--profile engineering`, stored object Metadata includes profile reference and object is tagged appropriately
- [ ] CLI: With `--project mobile-app`, stored object Metadata includes `"project": "mobile-app"`
- [ ] TUI: Modal accepts input, returns to main screen, shows toast notification
- [ ] Browser: Context menu appears, popup allows profile selection, confirmation sent
- [ ] REST API: POST /analyze with `{"content": "...", "profile": "engineering", "project": "mobile-app"}` returns 202 + job ID
- [ ] REST API: Server receives and persists `profile` and `project` fields — GET /objects/{object_id} confirms both fields in stored object
- [ ] REST API: GET /jobs/{job_id} shows completion status
- [ ] REST API: GET /objects/{object_id} returns enriched object with all submitted fields present
- [ ] Async: User can immediately search for content after capture (before enrichment)
- [ ] Multi-modal: Capture same text via CLI, TUI, REST, gRPC → identical objects
- [ ] Resilience: Worker crash during enrichment → job retried automatically

---

## Related Stories

- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) — Entity extraction pipeline step
- [US-0011](../enrichment/US-0011-assign-tags-from-vocabulary.md) — Tag assignment enrichment
- [US-0020](../search/US-0020-apply-focus-profile-to-search.md) — Using profiles at query time
- [US-0016](../search/US-0016-natural-language-search.md) — Searching captured content

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

> Not yet implemented.
