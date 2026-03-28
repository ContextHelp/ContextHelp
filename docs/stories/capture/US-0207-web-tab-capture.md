# US-0207: Browser Tab and Element Capture

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## User Goal

As a knowledge worker, I want to capture the current browser tab, a selected text region, or a specific DOM element directly into my knowledge base via the browser extension so that I never need to copy-paste content manually.

---

## Context

Knowledge workers spend significant time in the browser -- reading articles, reviewing dashboards, scanning social feeds, and navigating internal tools. The most valuable information is often encountered in the flow of browsing, but capturing it requires breaking context: switching to a note-taking app, pasting a URL, copying relevant text, and manually adding metadata. This friction means most encountered knowledge is never captured, creating gaps in the knowledge base.

A browser extension that integrates directly with ctxt eliminates this friction entirely. A single click or keyboard shortcut captures the full page, a text selection, or a specific DOM element -- complete with source URL, page title, capture timestamp, and authentication state. The extension popup shows recent captures with status indicators, providing confidence that captured content is being processed.

Three capture modes address different use cases: full-page capture for articles and reference pages (processed through a domain-aware article extraction pipeline), selection capture for specific paragraphs or quotes (processed through the text pipeline), and element capture for structured data like tables, code blocks, or UI components (preserving both HTML structure and a visual screenshot). Each mode routes to the appropriate pipeline, ensuring the captured content is cleaned, enriched, and indexed optimally.

---

## Acceptance Criteria

- [ ] Extension button "Capture full page" sends page HTML + URL + title to ctxt and receives a job ID
- [ ] Right-click selected text -> "Capture selection to ctxt" sends selection + source URL to ctxt
- [ ] Right-click any element -> "Capture element" sends element outerHTML + screenshot + source URL
- [ ] All captures include: source URL, page title, capture timestamp, and authentication state
- [ ] Full page capture uses the `web.page` pipeline (domain-aware selector extraction on pre-fetched HTML with readability fallback)
- [ ] When a domain-specific scraper rule exists, full-page capture extracts the matching article body before generic readability is attempted
- [ ] When no scraper rule matches or selector extraction returns empty content, full-page capture falls back to readability extraction without failing the capture
- [ ] Selection capture routes to `web.selection` pipeline
- [ ] Element capture routes to `web.element` pipeline and stores both HTML and screenshot
- [ ] Extension popup shows the 10 most recent captures with status indicators (pending, completed, failed)
- [ ] Keyboard shortcut Cmd+Shift+C (configurable) triggers capture of current tab
- [ ] Extension communicates with local ctxt instance via localhost API
- [ ] Captures from authenticated pages include cookie state metadata (not the cookies themselves)
- [ ] Full-page captures record extraction metadata such as strategy used (`selector` or `readability`) and the matched rule domain when applicable
- [ ] Extension gracefully handles ctxt being unreachable (queues locally, retries)

---

## Implementation Notes

### CLI Interface

```bash
# The browser extension communicates with the local ctxt API server.
# These CLI commands support the same workflows for debugging and scripting:

# Capture a URL (equivalent to full page capture)
ctxt capture https://example.com/article
# -> routes to web.page pipeline

# Capture raw text with source attribution
ctxt capture --text "Selected paragraph content here" \
  --source-url https://example.com/article \
  --source-title "Article Title"

# Capture HTML element with source
ctxt capture --html "<table>...</table>" \
  --source-url https://example.com/dashboard \
  --screenshot /tmp/element-screenshot.png

# Returns immediately with JSON
{
  "job_id": "j-web-cap-3f1a2b",
  "object_id": "o-web-d4e5f6",
  "status": "pending_enrichment",
  "pipeline": "web.page",
  "source": "https://example.com/article"
}

# List recent captures from extension
ctxt capture list --limit 10
```

### REST API

```
# Full page capture (from extension)
POST /api/v1/capture/page
Content-Type: application/json

{
  "url": "https://example.com/article",
  "title": "Article Title",
  "html": "<html>...</html>",
  "timestamp": "2026-02-18T10:30:00Z",
  "auth_state": "authenticated",
  "domain_cookies_present": true
}

-> 202 Accepted
{
  "job_id": "j-web-cap-3f1a2b",
  "object_id": "o-web-d4e5f6",
  "status": "pending_enrichment",
  "pipeline": "web.page"
}

# Selection capture (from extension right-click)
POST /api/v1/capture/selection
Content-Type: application/json

{
  "text": "The selected text content from the page...",
  "html": "<p>The <strong>selected</strong> text content from the page...</p>",
  "source_url": "https://example.com/article",
  "source_title": "Article Title",
  "timestamp": "2026-02-18T10:31:00Z",
  "char_count": 142
}

-> 202 Accepted
{
  "job_id": "j-web-sel-7c8d9e",
  "object_id": "o-web-a1b2c3",
  "status": "pending_enrichment",
  "pipeline": "web.selection"
}

# Element capture (from extension right-click)
POST /api/v1/capture/element
Content-Type: multipart/form-data

------boundary
Content-Disposition: form-data; name="metadata"
Content-Type: application/json

{
  "outer_html": "<table class=\"data\">...</table>",
  "tag_name": "TABLE",
  "css_selector": "#dashboard > table.data:nth-child(2)",
  "source_url": "https://example.com/dashboard",
  "source_title": "Dashboard",
  "timestamp": "2026-02-18T10:32:00Z"
}
------boundary
Content-Disposition: form-data; name="screenshot"; filename="element.png"
Content-Type: image/png

<binary screenshot data>
------boundary--

-> 202 Accepted
{
  "job_id": "j-web-elem-2d3e4f",
  "object_id": "o-web-x9y8z7",
  "status": "pending_enrichment",
  "pipeline": "web.element"
}

# Recent captures
GET /api/v1/capture/recent?limit=10

-> 200 OK
{
  "captures": [
    {
      "job_id": "j-web-cap-3f1a2b",
      "object_id": "o-web-d4e5f6",
      "type": "page",
      "title": "Article Title",
      "source_url": "https://example.com/article",
      "status": "completed",
      "captured_at": "2026-02-18T10:30:00Z"
    },
    ...
  ]
}
```

### Pipeline Steps

**web.page** (full page capture from browser extension — HTML pre-fetched by extension):
```
HTMLReceiver -> ArticleScraper -> MetadataExtractor -> Sectioner -> Tagger -> EmbeddingGenerator
```

**web.selection** (text selection capture):
```
SelectionCleaner -> SourceAttacher -> Tagger -> EmbeddingGenerator
```

**web.element** (DOM element capture):
```
ElementParser -> ScreenshotAttacher -> SourceAttacher -> Tagger -> EmbeddingGenerator
```

Each step implements the `PipelineStep` interface:

```go
type SelectionCleanerStep struct{}

func (s *SelectionCleanerStep) Name() string { return "selection_cleaner" }

func (s *SelectionCleanerStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    rawText := draft.RawContent
    rawHTML := draft.Metadata["selection_html"].(string)

    // Determine pipeline routing based on length
    charCount := len([]rune(string(rawText)))
    if charCount < 280 {
        draft.Metadata["selection_type"] = "short"
    } else {
        draft.Metadata["selection_type"] = "long"
    }

    // Clean HTML: strip inline styles, normalize whitespace
    cleaned := sanitizeHTML(rawHTML)
    draft.Sections = append(draft.Sections, Section{
        Title:   "Selected Content",
        Content: string(rawText),
    })

    draft.Metadata["char_count"] = charCount
    draft.Metadata["has_html_formatting"] = rawHTML != string(rawText)

    return draft, nil
}
```

```go
type ElementParserStep struct{}

func (s *ElementParserStep) Name() string { return "element_parser" }

func (s *ElementParserStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    outerHTML := draft.Metadata["outer_html"].(string)
    tagName := draft.Metadata["tag_name"].(string)

    // Parse element based on tag type
    switch strings.ToUpper(tagName) {
    case "TABLE":
        rows, err := parseHTMLTable(outerHTML)
        if err != nil {
            return nil, fmt.Errorf("element_parser: table parse failed: %w", err)
        }
        draft.Sections = append(draft.Sections, Section{
            Title:   "Table Data",
            Content: formatTableAsText(rows),
        })
        draft.Metadata["element_type"] = "table"
        draft.Metadata["row_count"] = len(rows)

    case "PRE", "CODE":
        code := extractTextContent(outerHTML)
        draft.Sections = append(draft.Sections, Section{
            Title:   "Code Block",
            Content: code,
        })
        draft.Metadata["element_type"] = "code"

    default:
        text := extractTextContent(outerHTML)
        draft.Sections = append(draft.Sections, Section{
            Title:   fmt.Sprintf("Captured %s Element", tagName),
            Content: text,
        })
        draft.Metadata["element_type"] = "generic"
    }

    // Store raw HTML for faithful reproduction
    draft.Metadata["preserved_html"] = outerHTML

    return draft, nil
}
```

```go
type ScreenshotAttacherStep struct {
    blobStore BlobStore
}

func (s *ScreenshotAttacherStep) Name() string { return "screenshot_attacher" }

func (s *ScreenshotAttacherStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    screenshotData, ok := draft.Metadata["screenshot_data"].([]byte)
    if !ok || len(screenshotData) == 0 {
        // No screenshot provided; skip without error
        draft.Metadata["has_screenshot"] = false
        return draft, nil
    }

    // Store screenshot as blob
    blobID, err := s.blobStore.Put(ctx, screenshotData, "image/png")
    if err != nil {
        return nil, fmt.Errorf("screenshot_attacher: blob store failed: %w", err)
    }

    draft.Metadata["screenshot_blob_id"] = blobID
    draft.Metadata["has_screenshot"] = true
    draft.Metadata["screenshot_size_bytes"] = len(screenshotData)

    return draft, nil
}
```

```go
type SourceAttacherStep struct{}

func (s *SourceAttacherStep) Name() string { return "source_attacher" }

func (s *SourceAttacherStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    draft.Source.URL = draft.Metadata["source_url"].(string)
    draft.Source.Title = draft.Metadata["source_title"].(string)
    draft.Source.IngestedAt = time.Now().UTC()

    if authState, ok := draft.Metadata["auth_state"].(string); ok {
        draft.Metadata["capture_auth_state"] = authState
    }

    return draft, nil
}
```

### Backend Processing

1. Browser extension detects user action (button click, right-click menu, keyboard shortcut)
2. Extension captures the relevant content: full page HTML, selected text, or element outerHTML
3. For element capture, extension renders a canvas snapshot of the element as a PNG screenshot
4. Extension sends payload to localhost ctxt API with source URL, title, timestamp, and auth state
5. API server validates the payload and selects the appropriate pipeline:
   - Full page -> `web.page` (readability extraction on pre-fetched HTML, sectioning, tagging)
   - Selection -> `web.selection` (cleaning, source attribution, tagging)
   - Element -> `web.element` (HTML parsing, screenshot storage, tagging)
6. Job is created in the `jobs` table with status `pending` and job ID returned immediately
7. Worker picks up the job and runs the selected pipeline steps
8. For full pages: `ReadabilityConverter` strips nav, ads, and chrome; extracts article body
9. For selections: `SelectionCleaner` normalizes whitespace and determines short vs long routing
10. For elements: `ElementParser` handles tag-specific parsing (tables, code, generic)
11. `SourceAttacher` links the object to its origin URL and captures provenance metadata
12. `Tagger` and `EmbeddingGenerator` complete enrichment
13. Extension polls the recent captures endpoint to update status indicators in the popup

### Browser Extension Architecture

```
extension/
  manifest.json        # Chrome Manifest V3
  background.js        # Service worker: API communication, queue management
  content.js           # Content script: DOM access, selection capture, screenshots
  popup.html           # Extension popup: recent captures, status indicators
  popup.js             # Popup logic: fetch recent, display status
  options.html         # Settings: keyboard shortcut, ctxt API URL, defaults
  options.js           # Settings persistence
  icons/               # Extension icons (16, 32, 48, 128)
```

### Configuration

```yaml
# In configuration.yaml
pipelines:
  web.page:
    steps:
      - html_receiver
      - readability_converter
      - metadata_extractor
      - sectioner
      - tagger
      - embedding_generator

  web.selection:
    steps:
      - selection_cleaner
      - source_attacher
      - tagger
      - embedding_generator

  web.element:
    steps:
      - element_parser
      - screenshot_attacher
      - source_attacher
      - tagger
      - embedding_generator

capture:
  extension:
    apiBaseURL: "http://localhost:8080"   # Local ctxt API
    keyboardShortcut: "Cmd+Shift+C"      # Configurable
    recentCaptureLimit: 10               # Shown in popup
    offlineQueueSize: 50                 # Max queued captures when offline
    retryInterval: 30s                   # Retry interval for queued captures
    screenshotFormat: png                # png | jpeg
    screenshotQuality: 0.92             # JPEG quality (if jpeg format)

  selection:
    shortThreshold: 280                  # Characters; below = short, above = long
    preserveHTML: true                   # Keep HTML formatting in selection
    maxSelectionSize: 100000             # Max characters for a selection capture

  element:
    maxOuterHTMLSize: 500000             # Max bytes for element HTML
    screenshotMaxSize: 10485760          # 10MB max screenshot
    supportedTags:                        # Tags with special parsing
      - TABLE
      - PRE
      - CODE
      - BLOCKQUOTE
      - FIGURE
      - UL
      - OL
```

### Knowledge Object Structure

```json
{
  "id": "o-web-x9y8z7",
  "type": "web_element",
  "subtype": "table",
  "content_type": "text/html",
  "metadata": {
    "source_url": "https://example.com/dashboard",
    "source_title": "Sales Dashboard Q1 2026",
    "capture_auth_state": "authenticated",
    "tag_name": "TABLE",
    "css_selector": "#dashboard > table.data:nth-child(2)",
    "element_type": "table",
    "row_count": 24,
    "has_screenshot": true,
    "screenshot_blob_id": "blob-ss-a1b2c3",
    "screenshot_size_bytes": 245832,
    "preserved_html": "<table class=\"data\">...</table>"
  },
  "sections": [
    {
      "title": "Table Data",
      "content": "Region | Revenue | Growth\nNorth America | $2.4M | +12%\n..."
    }
  ],
  "tags": ["sales", "quarterly-report", "dashboard"],
  "source": {
    "url": "https://example.com/dashboard",
    "title": "Sales Dashboard Q1 2026",
    "ingested_at": "2026-02-18T10:32:00Z",
    "capture_method": "browser_extension"
  },
  "pipeline": {
    "name": "web.element",
    "steps_completed": ["element_parser", "screenshot_attacher", "source_attacher", "tagger", "embedding_generator"],
    "completed_at": "2026-02-18T10:32:08Z"
  }
}
```

---

## E2E Test Checklist

- [ ] Extension: "Capture full page" button sends page HTML + URL to ctxt and receives job ID
- [ ] Extension: Right-click selected text -> "Capture selection to ctxt" sends selection + source URL
- [ ] Extension: Right-click element -> "Capture element" sends outerHTML + screenshot + source URL
- [ ] Extension: Keyboard shortcut (Cmd+Shift+C) captures current tab as full page
- [ ] Extension: Popup shows 10 most recent captures with correct status indicators
- [ ] Extension: Captures from authenticated pages include auth state in metadata
- [ ] CLI: `ctxt capture --text "..." --source-url <url> --source-title "..."` sends all three fields (`text`, `source_url`, `source_title`) in the request payload
- [ ] CLI: `ctxt capture --html "..." --source-url <url> --screenshot <path>` sends `outer_html`, `source_url`, and screenshot binary in the request payload
- [ ] Pipeline: Full page capture routes to `web.page` and produces readability-extracted content
- [ ] Pipeline: Selection capture routes to `web.selection` and preserves source attribution
- [ ] Pipeline: Element capture routes to `web.element` and stores both HTML and screenshot
- [ ] Pipeline: Short selection (< 280 chars) tagged as `selection_type: short`
- [ ] Pipeline: Long selection (>= 280 chars) tagged as `selection_type: long`
- [ ] Element: TABLE element parsed into structured rows with column headers
- [ ] Element: PRE/CODE element preserves code content with formatting
- [ ] Element: Generic element extracts text content from outerHTML
- [ ] Screenshot: Element screenshot stored as blob and linked via `screenshot_blob_id`
- [ ] Screenshot: Missing screenshot does not cause pipeline failure
- [ ] REST API: `POST /api/v1/capture/page` request payload contains `url`, `title`, `html`, `timestamp`, `auth_state`, and `domain_cookies_present` fields; returns 202 with job ID
- [ ] REST API: `POST /api/v1/capture/selection` request payload contains `text`, `html`, `source_url`, `source_title`, `timestamp`, and `char_count` fields; returns 202 with job ID
- [ ] REST API: `POST /api/v1/capture/element` multipart request contains `metadata` part (with `outer_html`, `tag_name`, `css_selector`, `source_url`, `source_title`, `timestamp`) and `screenshot` binary part; returns 202 with job ID
- [ ] REST API: `GET /api/v1/capture/recent?limit=10` returns most recent captures with status
- [ ] Storage: Completed page object stored with `metadata.source_url`, `metadata.capture_auth_state`, and `source.capture_method: browser_extension` (verifiable via `GET /api/v1/objects/<id>`)
- [ ] Storage: Completed element object stored with `metadata.tag_name`, `metadata.css_selector`, `metadata.has_screenshot`, and `metadata.screenshot_blob_id` (verifiable via `GET /api/v1/objects/<id>`)
- [ ] Search: Captured page content is searchable via `ctxt search "text from captured page"`
- [ ] Search: Captured selection is searchable via `ctxt search "selected text"`
- [ ] Offline: Extension queues captures locally when ctxt is unreachable
- [ ] Offline: Queued captures are sent when ctxt becomes reachable again
- [ ] Size: Selection exceeding `maxSelectionSize` is rejected with descriptive error
- [ ] Size: Element outerHTML exceeding `maxOuterHTMLSize` is rejected with descriptive error
- [ ] Resilience: Worker crash during pipeline step causes automatic job retry

---

## Related Stories

- [US-0209](./US-0209-authenticated-web-fetch.md) -- Authenticated web fetch for paywalled content
- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) -- URL capture and extraction (base functionality)
- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) -- Text capture (base text pipeline)
- [US-0003](../ingestion/US-0003-image-ocr-and-analysis.md) -- Image OCR for screenshot processing
- [US-0012](../enrichment/US-0012-generate-summaries-and-sections.md) -- Summarization enrichment
- [US-0011](../enrichment/US-0011-assign-tags-from-vocabulary.md) -- Tag assignment from extracted content

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

> Not yet implemented.
