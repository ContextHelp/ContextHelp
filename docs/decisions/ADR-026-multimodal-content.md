# ADR-026 – Multimodal Content Processing

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS + ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context and Problem Statement

The **Formless Principle** states: "Accepts reality as-is. Any format, length, or mess. Nothing is rejected."

The **Polyglot Principle** extends beyond language to modalities: "Speaks every language of knowledge and communication. Notes, tasks, code, media, research..."

Users capture knowledge in diverse formats: screenshots, diagrams, audio recordings, video tutorials, scanned documents, PDFs, and mixed-media content. The system must:

1. **Accept** any content type without rejection
2. **Detect** content type and subtype automatically
3. **Extract** structured information from unstructured media (OCR, transcription)
4. **Store** binary attachments efficiently
5. **Index** extracted text for search and retrieval
6. **Enrich** multimodal content with mentions, entities, tags
7. **Preserve** original media with content-addressed storage

### Key Questions

- How are binary attachments (images, audio, video) stored?
- How is content type detection performed?
- Which pipelines handle which media types?
- How are OCR and transcription integrated?
- How are multimodal embeddings generated for semantic search?
- How are attachments preserved during export/import?

---

## Decision Drivers

- **Formless Principle:** Any format, length, or mess must be accepted
- **Polyglot Principle:** Media is a language of knowledge
- **Performance:** Large media files must not block ingestion
- **Determinism:** Same media input must produce same enrichment
- **Extensibility:** New media types and processors must be pluggable
- **Storage Efficiency:** Binary content must be deduplicated and content-addressed
- **Offline-First:** Media processing must work without internet

---

## Considered Options

### Option 1: Inline Binary Storage

**Approach:**
- Store binary content directly in knowledge object (base64-encoded)
- No separate attachment store

**Pros:**
- Simple schema
- Single storage location

**Cons:**
- Massive knowledge objects (images/videos are large)
- Cannot deduplicate binary content
- Export/import becomes slow
- JSON parsing slow for large blobs

### Option 2: Content-Addressed Attachment Store

**Approach:**
- Binary content stored separately in content-addressed store (hash-based filenames)
- Knowledge object references attachments by hash + metadata
- Attachments deduplicated automatically (same hash = same file)
- Multiple storage backends (local filesystem, S3, registry-hosted)

**Pros:**
- Deduplication built-in (same image referenced multiple times = one file)
- Clean knowledge object schema
- Scalable storage (can use cloud backends)
- Fast export/import (copy by reference)

**Cons:**
- Requires attachment store abstraction
- Must handle attachment lifecycle (orphan cleanup)

### Option 3: External Media with URL References

**Approach:**
- Store media externally (user's cloud storage, Dropbox, etc.)
- Knowledge object stores URL reference only
- No local storage of binary content

**Pros:**
- Minimal local storage
- User controls media location

**Cons:**
- Breaks offline-first principle
- Media may disappear (broken links)
- Cannot guarantee media availability
- Cannot perform OCR/transcription without downloading

---

## Decision Outcome

**Chosen Option:** Option 2 — Content-Addressed Attachment Store

**Rationale:**

This approach aligns with all architectural principles:

- **Formless:** Any media type accepted, stored safely
- **Offline-First:** Media stored locally, always available
- **Deduplication:** Same media referenced multiple times = one file
- **Extensibility:** Storage backends pluggable (ADR-021)
- **Federation:** Attachments sync across registries with integrity guarantees
- **Determinism:** Content-addressed storage ensures reproducibility

---

## Implementation Details

### Content Type Detection (Input Layer)

Content type detection occurs in the **Input Layer** before inference:

```
Input → Type Detection → Inference → Pipeline Selection → Enrichment
```

**Type Detection Heuristics:**
1. **MIME Type:** Extracted from file extension or HTTP headers
2. **Magic Bytes:** Read first bytes of file (e.g., `FFD8FF` = JPEG)
3. **Content Analysis:** Heuristics for ambiguous cases (e.g., text/plain vs. code)
4. **User Override:** CLI flag `--type <type>` forces type

**Detected Types:**
- `text` — Plain text, markdown, rich text
- `url` — Web page, GitHub repo, YouTube video
- `image` — PNG, JPEG, WebP, GIF, SVG
- `audio` — MP3, WAV, AAC, OGG, FLAC
- `video` — MP4, WebM, MOV, AVI
- `document` — PDF, DOCX, PPTX, XLSX
- `code` — Source code files (language-detected)
- `feed` — RSS/Atom feeds

**Subtypes** (examples):
- `image.landing` — Screenshot of web page
- `image.ui` — UI mockup or design
- `image.diagram` — Architecture diagram
- `image.ocr` — Scanned document
- `audio.transcript` — Interview, podcast
- `video.youtube` — YouTube video
- `video.local` — Local video file
- `document.pdf` — PDF document

### Attachment Storage Schema

**Knowledge Object Reference:**
```json
{
  "id": "obj-abc123",
  "type": "image",
  "subtype": "image.landing",
  "summary": "Screenshot of GitHub's new UI...",
  "attachments": [
    {
      "hash": "sha256:a3b2c1d4e5f6...",
      "filename": "github-ui.png",
      "mime_type": "image/png",
      "size_bytes": 245832,
      "width": 1440,
      "height": 900
    }
  ]
}
```

**Attachment Store Layout:**
```
/Users/user/.local/share/ctxt/attachments/
  a3/
    b2/
      a3b2c1d4e5f6...png   ← Content-addressed file
```

**Attachment Store Interface (ADR-021):**
```go
type AttachmentStore interface {
    Store(ctx context.Context, content io.Reader, metadata AttachmentMetadata) (AttachmentRef, error)
    Retrieve(ctx context.Context, hash string) (io.ReadCloser, error)
    Exists(ctx context.Context, hash string) (bool, error)
    Delete(ctx context.Context, hash string) error
    List(ctx context.Context) ([]AttachmentRef, error)
}

type AttachmentMetadata struct {
    Filename  string
    MimeType  string
    SizeBytes int64
    Width     int // For images/videos
    Height    int
}

type AttachmentRef struct {
    Hash     string // sha256:...
    Filename string
    MimeType string
}
```

**Storage Backends:**
- `LocalFilesystemAttachmentStore` (default)
- `S3AttachmentStore` (plugin)
- `RegistryAttachmentStore` (federated, plugin)

### Multimodal Pipelines

**Image Pipelines:**

```yaml
image.landing:
  steps:
    - DetectUIElements      # Identify buttons, forms, nav
    - ExtractVisibleText    # OCR visible text
    - GenerateSummary       # LLM describes screenshot
    - ExtractMentions       # Extract @entity.slug from OCR text
    - ExtractTags           # Classify content (ui, landing-page, etc.)
    - AssembleObject

image.ui:
  steps:
    - AnalyzeDesign         # Detect design patterns
    - ExtractComponents     # Identify UI components
    - GenerateSummary
    - ExtractMentions
    - ExtractTags

image.ocr:
  steps:
    - OCRExtraction         # Extract all text from image
    - PassToTextPipeline    # Feed extracted text to text.long pipeline
```

**Audio Pipelines:**

```yaml
audio.transcript:
  steps:
    - TranscribeAudio       # Speech-to-text (Whisper, Deepgram, etc.)
    - DetectSpeakers        # Speaker diarization
    - PassToTextPipeline    # Feed transcript to text.long pipeline
```

**Video Pipelines:**

```yaml
video.youtube:
  steps:
    - FetchMetadata         # Title, description, channel
    - DownloadTranscript    # YouTube auto-generated or manual
    - SampleKeyframes       # Extract representative frames
    - OCRKeyframes          # Extract text from frames
    - PassToTextPipeline    # Feed transcript + OCR to text.long

video.local:
  steps:
    - ExtractAudio          # Separate audio track
    - TranscribeAudio       # Speech-to-text
    - SampleKeyframes       # Extract representative frames
    - OCRKeyframes          # Extract text from frames
    - DetectScenes          # Scene boundary detection
    - PassToTextPipeline
```

**Document Pipelines:**

```yaml
document.pdf:
  steps:
    - ExtractText           # Native PDF text extraction
    - OCRScannedPages       # OCR for image-based PDFs
    - ExtractStructure      # Headers, sections, tables
    - PassToTextPipeline
```

### OCR Integration (AI Decorator Pattern)

**OCR Client Interface (ADR-005):**
```go
type OCRClient interface {
    ExtractText(ctx context.Context, image io.Reader, opts OCROptions) (OCRResult, error)
}

type OCROptions struct {
    Language      string   // Hint for language-specific OCR
    DetectOrientation bool // Auto-rotate if needed
}

type OCRResult struct {
    Text       string
    Confidence float64
    Blocks     []TextBlock // Bounding boxes for text regions
}
```

**OCR Providers (Plugins):**
- `TesseractOCRClient` (local, offline)
- `GoogleVisionOCRClient` (cloud, high accuracy)
- `AWSTextractOCRClient` (cloud, form/table extraction)
- `LLMVisionOCRClient` (GPT-4V, Claude Vision)

**Caching (docs/dpkms/caching.md):**
```
ocr:<provider>:<image-hash> → OCRResult
```

### Transcription Integration

**Transcription Client Interface:**
```go
type TranscriptionClient interface {
    Transcribe(ctx context.Context, audio io.Reader, opts TranscriptionOptions) (TranscriptionResult, error)
}

type TranscriptionOptions struct {
    Language          string
    SpeakerDiarization bool
    TimestampGranularity string // word, sentence, paragraph
}

type TranscriptionResult struct {
    Text      string
    Speakers  []Speaker
    Segments  []TranscriptSegment
}

type TranscriptSegment struct {
    Text      string
    Speaker   string
    StartTime float64
    EndTime   float64
}
```

**Transcription Providers (Plugins):**
- `WhisperTranscriptionClient` (local, offline)
- `DeepgramTranscriptionClient` (cloud, fast)
- `AWSTranscribeClient` (cloud)
- `GoogleSpeechClient` (cloud)

**Caching:**
```
asr:<provider>:<audio-hash> → TranscriptionResult
```

### Multimodal Embeddings (ADR-022)

**Cross-Modal Search:**

For semantic search across modalities (e.g., "find images similar to this text description"):

```go
type MultimodalEmbeddingClient interface {
    EmbedText(ctx context.Context, text string) ([]float32, error)
    EmbedImage(ctx context.Context, image io.Reader) ([]float32, error)
    EmbedAudio(ctx context.Context, audio io.Reader) ([]float32, error)
}
```

**Implementation:**
- Use multimodal models (e.g., CLIP, ImageBind, SigLIP)
- Generate embeddings for both extracted text (OCR/transcription) and raw media
- Store both text embedding and media embedding in vector index
- Query can use either modality (text query → find images, image query → find similar images)

**Example:**
```bash
# Text query finds images
ctxt search "architecture diagram with microservices"
# → Returns images tagged "architecture", "microservices"

# Image query finds similar images
ctxt search --image screenshot.png
# → Returns images with similar visual content
```

### Export/Import Integration (ADR-020)

**Bundle Structure:**
```
bundle/
  manifest.json
  objects/
    obj-abc123.json
  attachments/
    {hash}.{ext}    # Content-addressed attachments
  metadata.json
```

**Export Behavior:**
- Copy all referenced attachments to `attachments/` directory
- Use content-addressed filenames (deduplicated)
- Include attachment metadata in manifest

**Import Behavior:**
- Restore attachments to local attachment store
- Verify attachment hashes match manifest
- Skip attachments already present (same hash)

### Offline-First Considerations

**Media Processing Without Internet:**
- Local OCR (Tesseract) works offline
- Local transcription (Whisper) works offline
- LLM-based analysis requires local LLM or fails gracefully
- User notified when offline processing used

**Graceful Degradation:**
```json
{
  "enrichment_metadata": {
    "ocr_provider": "tesseract",
    "ocr_confidence": 0.78,
    "transcription_provider": "whisper",
    "offline_mode": true
  }
}
```

---

## Consequences

### Positive

- **Formless:** Any media type accepted and processed
- **Deduplication:** Same media stored once, referenced many times
- **Offline-First:** Local processing works without internet
- **Extensible:** New media types and processors pluggable
- **Federated:** Attachments sync cleanly across registries
- **Searchable:** Extracted text indexed for semantic and keyword search
- **Cross-Modal:** Can search images with text queries and vice versa

### Negative

- **Storage Growth:** Large media files increase storage requirements (mitigated by deduplication)
- **Processing Latency:** OCR and transcription are slow (mitigated by async job system)
- **API Costs:** Cloud OCR/transcription may be expensive (mitigated by caching and local fallbacks)
- **Accuracy Variance:** OCR and transcription quality varies by provider

### Neutral

- **Attachment Lifecycle:** Must handle orphaned attachments (cleanup job)
- **Offline Quality:** Local models have lower accuracy than cloud (user controls trade-off)

---

## Compliance

**Must Have for v1.0:**
- [ ] Content-addressed attachment store
- [ ] Attachment reference schema in knowledge object
- [ ] MIME type detection and magic byte analysis
- [ ] Image pipeline (`image.ocr`, `image.landing`)
- [ ] OCR client interface and local provider (Tesseract)
- [ ] Attachment store backends (local filesystem)
- [ ] Export/import includes attachments

**Should Have for v1.0:**
- [ ] Audio pipeline (`audio.transcript`)
- [ ] Transcription client interface and local provider (Whisper)
- [ ] Video pipeline (`video.youtube`, `video.local`)
- [ ] PDF pipeline (`document.pdf`)
- [ ] Cloud OCR provider (Google Vision or AWS Textract)

**May Have for v2.0:**
- [ ] Multimodal embeddings (CLIP, ImageBind)
- [ ] Cross-modal search (text → image, image → text)
- [ ] Video scene detection
- [ ] Speaker diarization in audio
- [ ] Attachment orphan cleanup job

---

## Notes

**Design References:**
- `docs/architecture.md:131-132` (Formless principle)
- `docs/architecture.md:134-135` (Polyglot principle)
- `docs/architecture.md:572-578` (Pipeline examples)
- `docs/ctxt/pipelines-reference.md:244-368` (Pipeline specifications)

**Related User Stories:**
- User ingests screenshot of UI mockup → OCR extracts text, LLM identifies UI patterns
- User records podcast interview → Whisper transcribes, entities extracted from transcript
- User uploads scanned document → OCR extracts text, text pipeline enriches
- User captures YouTube video → Transcript downloaded, keyframes analyzed

**Open Questions:**
- Should attachment store support compression (e.g., WebP for images, Opus for audio)?
- Should we support attachment streaming for large files (GB-sized videos)?
- Should OCR/transcription be triggered automatically or on-demand?

**Future Considerations:**
- Video summarization (multi-frame analysis)
- Audio speaker identification (voice recognition)
- Document structure extraction (tables, forms)
- Image similarity search (visual embeddings)
