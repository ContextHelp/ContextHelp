---
status: shipped
---

# US-0002: URL Capture and Extraction

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to capture content from URLs (articles, repositories, PDFs) and have it automatically processed into my knowledge base.

---

## Context

Much of a knowledge worker's information consumption happens via links (articles, blog posts, documentation, GitHub repositories). Manual copying/pasting is friction. Direct URL ingestion with automatic content extraction saves time and maintains provenance.

---

## Acceptance Criteria

- [ ] User can provide URL to `ctxt add --url <url>`
- [ ] System fetches URL and detects content type (HTML, PDF, repository)
- [ ] System selects appropriate pipeline (url.article, url.pdf, url.repository)
- [ ] Content is extracted and cleaned (HTML removed, text extracted)
- [ ] Original URL stored as source reference
- [ ] User is not blocked during fetching (async)
- [ ] Captured content available for search within 30 seconds
- [ ] Failed URL fetches are logged with reason (404, timeout, etc.)

---

## Implementation Notes

### CLI Interface
```bash
# Basic URL capture
ctxt add --url https://example.com/article

# With custom type hint
ctxt add --url https://github.com/org/repo --type url.repository

# Returns immediately with job ID
{
  "job_id": "j-xyz789",
  "object_id": "o-abc123",
  "status": "pending_enrichment",
  "pipeline": "url.article",
  "source": "https://example.com/article"
}
```

### URL Detection Logic
```go
type URLDetector struct {
  httpClient *http.Client
}

func (d *URLDetector) Detect(url string) (string, error) {
  // HEAD request to get content-type
  resp, _ := d.httpClient.Head(url)
  contentType := resp.Header.Get("Content-Type")

  switch {
  case strings.Contains(contentType, "pdf"):
    return "url.pdf", nil
  case strings.Contains(url, "github.com"):
    return "url.repository", nil
  case strings.Contains(url, "arxiv.org"):
    return "url.research", nil
  default:
    return "url.article", nil  // Default to article
  }
}
```

### Pipeline Steps
- **url.article** → Fetch → Clean HTML → Extract article → Summarize
- **url.pdf** → Fetch PDF → Extract text → Structure → Index
- **url.repository** → Fetch repo info → Parse README → Extract structure

### REST API
```
POST /analyze
Content-Type: application/json

{
  "source_type": "url",
  "source_url": "https://example.com/article",
  "type_hint": "url.article"
}

→ 202 Accepted
{
  "job_id": "j-xyz789",
  "object_id": "o-abc123"
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt add --url https://example.com/article` sends `"source_url": "https://example.com/article"` and `"source_type": "url"` in the request payload to the server
- [ ] CLI: `ctxt add --url https://github.com/org/repo --type url.repository` sends `"type_hint": "url.repository"` in the server request payload
- [ ] CLI: Response includes job ID and `"pipeline"` field matching the detected type (e.g., `"url.article"`)
- [ ] Fetch: Content fetched successfully; server confirms receipt of `source_url` in stored object's Source field
- [ ] Type Detection: Correct pipeline selected for URL type; stored object's `pipeline` field matches the detected type
- [ ] Content Extraction: HTML cleaned, text extracted; stored object's RawContent contains extracted text (not raw HTML)
- [ ] Source: Original URL stored in object's Source field — GET /objects/{object_id} confirms `"source_url"` matches input
- [ ] Async: CLI returns immediately without blocking; job transitions `pending` → `completed` in background
- [ ] Enrichment: Content summarized and indexed within 30s; `GET /objects/{id}` returns non-empty
  `document.sections` and `document.body` (derived from graph via DocumentProjection)
- [ ] Error Handling: 404 URL handled gracefully with error message; job status set to `failed` with descriptive reason
- [ ] Error Handling: Timeout (>30s fetch) handled gracefully; job status set to `failed` with timeout reason
- [ ] Repository: GitHub repo content extracted and indexed; stored object type reflects `url.repository` pipeline
- [ ] REST API: `POST /analyze` with `{"source_type": "url", "source_url": "...", "type_hint": "url.article"}` returns 202 + job ID
- [ ] REST API: Server stores all submitted fields — GET /objects/{object_id} confirms `source_url` and selected pipeline in stored object

---

## Related Stories

- [US-0001](./US-0001-text-capture-minimal-friction.md) — Base capture functionality
- [US-0003](./US-0003-image-ocr-and-analysis.md) — Image extraction
- [US-0012](../enrichment/US-0012-generate-summaries-and-sections.md) — Summarization enrichment

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- Solo Developer
- Automation Builder

---

## E2E Tests

- `test/integration/us0002_url_capture_test.go::TestUS0002_URLIngestReturnsJobID`
- `test/integration/us0002_url_capture_test.go::TestUS0002_TitleAndBodyExtracted`
- `test/integration/us0002_url_capture_test.go::TestUS0002_SourceURLStoredOnObject`
- `test/integration/us0002_url_capture_test.go::TestUS0002_DedupOnReIngestSameURL`
- `test/integration/us0002_url_capture_test.go::TestUS0002_URLPipelineAssigned`
