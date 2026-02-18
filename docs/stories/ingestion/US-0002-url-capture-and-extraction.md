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

- [ ] CLI: URL provided via `--url` flag
- [ ] Fetch: Content fetched successfully
- [ ] Type Detection: Correct pipeline selected for URL type
- [ ] Content Extraction: HTML cleaned, text extracted
- [ ] Source: Original URL stored in object
- [ ] Async: Returns immediately without blocking
- [ ] Enrichment: Content summarized and indexed within 30s
- [ ] Error Handling: 404 URL handled gracefully with error message
- [ ] Error Handling: Timeout (>30s fetch) handled gracefully
- [ ] Repository: GitHub repo content extracted and indexed

---

## Related Stories

- [US-0001](./US-0001-text-capture-minimal-friction.md) — Base capture functionality
- [US-0003](./US-0003-image-ocr-and-analysis.md) — Image extraction
- [US-0012](../enrichment/US-0012-generate-summaries-and-sections.md) — Summarization enrichment
