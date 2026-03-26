# US-0003: Image OCR and Analysis

**System Types:** ctxt, dpkms (self-hosted)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Agents/LLMs/Tools](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker, I want to capture images (screenshots, photos, diagrams) and have their text content automatically extracted and indexed so that visual information becomes searchable alongside my text-based knowledge.

---

## Context

Knowledge workers routinely encounter information locked inside images: whiteboard photos from brainstorming sessions, screenshots of error messages or UI states, architecture diagrams, conference slides, and handwritten notes. Without OCR extraction, this visual information exists outside the searchable knowledge base, creating blind spots in decision-making and recall.

The problem is compounded for teams that share screenshots as the primary medium for bug reports, design feedback, or quick documentation. These images accumulate in chat channels and ticket systems with no structured way to retrieve the information they contain. Manual transcription is tedious and rarely done.

By supporting pluggable OCR providers (Tesseract for offline/local use, cloud APIs for higher accuracy, or LLM vision models for contextual understanding), the system accommodates different deployment constraints. A self-hosted installation behind a firewall can use Tesseract with no external calls, while a cloud deployment can leverage higher-accuracy services. The `image.analysis` pipeline goes further by using LLM vision to describe scene content, read charts, and interpret diagrams beyond raw text extraction.

---

## Acceptance Criteria

- [ ] User can ingest an image via `ctxt add --file photo.png` and receive a job ID within 1 second
- [ ] System auto-detects image format and selects the `image.ocr` pipeline by default
- [ ] User can explicitly select the `image.analysis` pipeline via `--type image.analysis`
- [ ] Supported formats: PNG, JPG/JPEG, WEBP, TIFF, BMP, GIF (first frame only)
- [ ] Unsupported or corrupt images return a clear error message without crashing the pipeline
- [ ] OCR-extracted text is stored in the knowledge object and is searchable
- [ ] Image metadata (dimensions, format, color depth, file size) is stored in the object Metadata field
- [ ] OCR confidence score is recorded; objects below the configurable threshold are flagged for human review
- [ ] Files exceeding the configurable size limit (default 50MB) are rejected with a descriptive error
- [ ] Processing is fully async: the user is never blocked waiting for OCR to complete

---

## Implementation Notes

### CLI Interface
```bash
# Basic image capture (auto-detects format, selects image.ocr pipeline)
ctxt add --file photo.png

# Explicit type hint for vision-based analysis
ctxt add --file screenshot.jpg --type image.analysis

# With focus profile
ctxt add --file whiteboard.jpg --profile engineering --project mobile-app

# From stdin (piped screenshot tool output)
screencapture -o - | ctxt add --file - --content-type image/png

# Returns immediately with JSON
{
  "job_id": "j-img-7f3a2b",
  "object_id": "o-img-c91e4d",
  "status": "pending_enrichment",
  "pipeline": "image.ocr",
  "will_enrich_by": "2025-01-18T10:31:15Z"
}
```

### REST API
```
POST /analyze
Content-Type: multipart/form-data

------boundary
Content-Disposition: form-data; name="file"; filename="screenshot.png"
Content-Type: image/png

<binary image data>
------boundary
Content-Disposition: form-data; name="source_type"

image
------boundary
Content-Disposition: form-data; name="type_hint"

image.ocr
------boundary
Content-Disposition: form-data; name="profile"

engineering
------boundary--

-> 202 Accepted
{
  "job_id": "j-img-7f3a2b",
  "object_id": "o-img-c91e4d",
  "status": "pending_enrichment",
  "pipeline": "image.ocr"
}
```

### Pipeline Steps

**image.ocr** (OCR-focused extraction):
```
FileReader -> FormatDetector -> OCRExtractor -> TextCleaner -> Tagger -> EmbeddingGenerator
```

**image.analysis** (LLM vision analysis):
```
FileReader -> FormatDetector -> OCRExtractor -> VisionAnalyzer -> TextCleaner -> Tagger -> EmbeddingGenerator
```

Each step implements the `PipelineStep` interface:

```go
type FileReaderStep struct{}

func (s *FileReaderStep) Name() string { return "file_reader" }

func (s *FileReaderStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    data, err := os.ReadFile(draft.Source.Path)
    if err != nil {
        return nil, fmt.Errorf("file_reader: %w", err)
    }

    if len(data) > s.maxFileSize {
        return nil, fmt.Errorf("file_reader: file size %d exceeds limit %d",
            len(data), s.maxFileSize)
    }

    draft.RawContent = data
    return draft, nil
}
```

```go
type FormatDetectorStep struct{}

func (s *FormatDetectorStep) Name() string { return "format_detector" }

func (s *FormatDetectorStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    mime := http.DetectContentType(draft.RawContent)

    supported := map[string]bool{
        "image/png":  true,
        "image/jpeg": true,
        "image/webp": true,
        "image/tiff": true,
        "image/bmp":  true,
        "image/gif":  true,
    }

    if !supported[mime] {
        return nil, fmt.Errorf("format_detector: unsupported image format %s", mime)
    }

    // Decode image header for metadata
    cfg, format, err := image.DecodeConfig(bytes.NewReader(draft.RawContent))
    if err != nil {
        return nil, fmt.Errorf("format_detector: corrupt image: %w", err)
    }

    draft.ContentType = mime
    draft.Metadata["image_width"] = cfg.Width
    draft.Metadata["image_height"] = cfg.Height
    draft.Metadata["image_format"] = format
    draft.Metadata["file_size_bytes"] = len(draft.RawContent)

    return draft, nil
}
```

```go
type OCRExtractorStep struct {
    provider OCRProvider  // Tesseract, cloud API, or LLM vision
}

func (s *OCRExtractorStep) Name() string { return "ocr_extractor" }

func (s *OCRExtractorStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    result, err := s.provider.Extract(ctx, draft.RawContent, draft.ContentType)
    if err != nil {
        return nil, fmt.Errorf("ocr_extractor: %w", err)
    }

    if result.Confidence < s.confidenceThreshold {
        draft.Metadata["ocr_low_confidence"] = true
        draft.Metadata["ocr_confidence"] = result.Confidence
        draft.Tags = append(draft.Tags, "needs-review")
    }

    draft.Sections = append(draft.Sections, Section{
        Title:   "OCR Text",
        Content: result.Text,
    })
    draft.Metadata["ocr_confidence"] = result.Confidence
    draft.Metadata["ocr_provider"] = s.provider.Name()

    return draft, nil
}
```

### OCR Provider Interface

```go
type OCRProvider interface {
    Name() string
    Extract(ctx context.Context, imageData []byte, contentType string) (*OCRResult, error)
}

type OCRResult struct {
    Text       string   // Extracted text
    Confidence float64  // 0.0 to 1.0
    Regions    []Region // Bounding boxes for text regions (optional)
}

// Tesseract (local, no network calls)
type TesseractProvider struct {
    language string
    binPath  string
}

// Cloud API (e.g., Google Vision, AWS Textract)
type CloudOCRProvider struct {
    endpoint string
    apiKey   string
}

// LLM Vision (e.g., GPT-4V, Claude vision)
type LLMVisionProvider struct {
    aiProvider AIProvider
    prompt     string
}
```

### Backend Processing

1. `ctxt` receives the image file via CLI (`--file`) or REST API (multipart upload)
2. `FormatDetector` validates MIME type against the supported set and rejects unsupported formats
3. `FormatDetector` decodes the image header to extract dimensions, format, and color depth
4. System checks file size against the configurable limit (default 50MB) and rejects oversized files
5. System selects pipeline: `image.ocr` (default) or `image.analysis` (if user specified)
6. Creates Job in `jobs` table with status `pending` and returns job ID immediately
7. Worker picks up job and runs the pipeline:
   - `FileReader` loads bytes from disk or upload buffer
   - `FormatDetector` validates and extracts image metadata
   - `OCRExtractor` calls the configured OCR provider (Tesseract, cloud, or LLM vision)
   - `TextCleaner` normalizes whitespace, removes OCR artifacts, corrects common OCR errors
   - `VisionAnalyzer` (image.analysis only) sends image to LLM vision for scene description, chart reading, diagram interpretation
   - `Tagger` assigns tags from vocabulary based on extracted text content
   - `EmbeddingGenerator` generates embeddings for the extracted text
8. Stores the enriched KnowledgeObject with OCR text in Sections, image path in Source, metadata in Metadata
9. Creates graph edges for any extracted mentions
10. Marks job as `completed` (or `failed` with error detail)

### Knowledge Object Structure

```json
{
  "id": "o-img-c91e4d",
  "type": "image",
  "subtype": "ocr",
  "raw_content": "/data/objects/o-img-c91e4d/original.png",
  "content_type": "image/png",
  "metadata": {
    "image_width": 1920,
    "image_height": 1080,
    "image_format": "png",
    "file_size_bytes": 2458624,
    "ocr_confidence": 0.92,
    "ocr_provider": "tesseract"
  },
  "sections": [
    {
      "title": "OCR Text",
      "content": "Error: Connection refused at port 5432\nPostgreSQL service is not running..."
    }
  ],
  "tags": ["error", "postgresql", "infrastructure"],
  "mentions": ["@system.postgresql"],
  "source": {
    "path": "/data/objects/o-img-c91e4d/original.png",
    "original_filename": "screenshot.png",
    "ingested_at": "2025-01-18T10:30:45Z"
  },
  "pipeline": {
    "name": "image.ocr",
    "steps_completed": ["file_reader", "format_detector", "ocr_extractor", "text_cleaner", "tagger", "embedding_generator"],
    "completed_at": "2025-01-18T10:31:02Z"
  }
}
```

### Configuration

```yaml
# In configuration.yaml
pipelines:
  image.ocr:
    steps:
      - file_reader
      - format_detector
      - ocr_extractor
      - text_cleaner
      - tagger
      - embedding_generator

  image.analysis:
    steps:
      - file_reader
      - format_detector
      - ocr_extractor
      - vision_analyzer
      - text_cleaner
      - tagger
      - embedding_generator

ocr:
  provider: tesseract            # tesseract | cloud | llm_vision
  confidenceThreshold: 0.60      # Below this, flag for human review
  tesseract:
    language: eng                 # Tesseract language pack
    binPath: /usr/bin/tesseract   # Path to tesseract binary
  cloud:
    endpoint: https://vision.googleapis.com/v1/images:annotate
    apiKey: ${GOOGLE_VISION_API_KEY}
  llm_vision:
    provider: ${aiProvider}
    prompt: "Extract all visible text from this image verbatim."

image:
  maxFileSize: 52428800           # 50MB in bytes
  supportedFormats:
    - image/png
    - image/jpeg
    - image/webp
    - image/tiff
    - image/bmp
    - image/gif
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt add --file photo.png` returns job ID within 1 second; server receives file and creates job record
- [ ] CLI: Job status transitions from `pending` to `completed` within 60 seconds
- [ ] CLI: Retrieved object contains OCR-extracted text in Sections
- [ ] CLI: `ctxt add --file screenshot.jpg --type image.analysis` sends `"type_hint": "image.analysis"` in the request payload; stored object's pipeline field equals `"image.analysis"`
- [ ] CLI: `ctxt add --file whiteboard.jpg --profile engineering --project mobile-app` sends `"profile": "engineering"` and `"project": "mobile-app"` in the server request payload; stored object Metadata contains both fields
- [ ] Format: PNG, JPG, WEBP, TIFF, BMP images all processed successfully
- [ ] Format: GIF input extracts text from the first frame only
- [ ] Format: Unsupported format (e.g., SVG, RAW) returns descriptive error without crash
- [ ] Format: Corrupt/truncated image file returns error with clear message
- [ ] Size: File exceeding 50MB limit is rejected with size limit error
- [ ] OCR: Extracted text is searchable via `ctxt search "text from image"`
- [ ] OCR: Low-confidence result is flagged with `needs-review` tag
- [ ] Metadata: Image dimensions, format, and file size stored in object Metadata — GET /objects/{object_id} confirms `image_width`, `image_height`, `image_format`, `file_size_bytes` keys present
- [ ] REST API: Multipart upload via `POST /analyze` with `source_type=image` and `type_hint=image.ocr` returns 202 + job ID; server records both fields
- [ ] REST API: `GET /objects/{object_id}` returns enriched object with OCR text in Sections, pipeline name, and image metadata all populated
- [ ] Async: User can immediately query for the object before enrichment completes
- [ ] Resilience: Worker crash during OCR step causes automatic job retry

---

## Related Stories

- [US-0001](./US-0001-text-capture-minimal-friction.md) -- Base text capture functionality
- [US-0005](./US-0005-video-processing-with-scenes.md) -- Video processing extracts frames for OCR
- [US-0006](./US-0006-document-parsing-and-decomposition.md) -- Document parsing handles embedded images
- [US-0061](../search/US-0061-visual-similarity-search.md) -- Multimodal search across image content
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entity extraction from OCR text
- [US-0011](../enrichment/US-0011-assign-tags-from-vocabulary.md) -- Tag assignment from extracted content

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

[us0003_image_ocr_test.go](../../../test/integration/us0003_image_ocr_test.go)
