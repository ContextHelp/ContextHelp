# Pipeline Stories US-0003 to US-0008: Implementation Design

## Summary

Implement the 6 remaining ingestion pipeline stories (image, audio, video, document, feed, batch) using 3 parallel agents. Each agent owns 2 stories grouped by domain affinity.

## Current State

- 2 built-in pipelines: `text.short`, `text.long`
- 4 existing steps: TypeDetector, Sectioner, Tagger, Noop
- Full infrastructure: job queue, worker pool, storage (SQLite), HTTP API, CLI (`ctxt analyze`)
- `SelectPipeline()` only routes by content length
- `Section` type lacks metadata for timestamps/speaker labels/page numbers
- No provider abstraction for external tools (OCR, transcription, ffmpeg)

## Target State

16 new pipelines across 6 stories, ~30 new step implementations, pluggable provider interfaces with stub defaults.

## Agent Assignment

### Agent 1: Media Pipelines (US-0003 Image + US-0004 Audio)

Owns shared foundation steps used by all agents.

**Shared steps (new):**
- `FileReader` -- reads bytes, validates size limits, stores file metadata
- `FormatDetector` -- detects MIME type, validates support, extracts format-specific metadata
- `TextCleaner` -- normalizes whitespace, removes artifacts
- `EmbeddingGenerator` -- terminal step, stub that sets `VectorIndexed = true`

**Image steps (new):**
- `OCRExtractor` -- calls OCRProvider, stores extracted text + confidence
- `VisionAnalyzer` -- calls VisionProvider for scene description, chart reading

**Audio steps (new):**
- `AudioTranscriber` -- calls TranscriptionProvider, produces timestamped segments
- `SpeakerDiarizer` -- calls DiarizationProvider (optional, non-fatal)
- `TimestampAligner` -- reconciles transcript + diarization into clean segments

**Provider interfaces (new package `internal/providers/`):**
- `OCRProvider` -- Name(), Extract(ctx, imageData, contentType) -> OCRResult
- `VisionProvider` -- Name(), Analyze(ctx, imageData, contentType) -> VisionResult
- `TranscriptionProvider` -- Name(), Transcribe(ctx, audioPath, opts) -> TranscriptResult
- `DiarizationProvider` -- Name(), Diarize(ctx, audioPath, segments) -> DiarizedResult

**Storage changes:**
- Add `Metadata map[string]any` to `storage.Section`

**Registry changes:**
- Register `image.ocr`, `image.analysis`, `audio.transcribe`
- Update `SelectPipeline()` to route by source type/content type

**Pipelines:**
- `image.ocr`: FileReader -> FormatDetector -> OCRExtractor -> TextCleaner -> Tagger -> EmbeddingGenerator
- `image.analysis`: FileReader -> FormatDetector -> OCRExtractor -> VisionAnalyzer -> TextCleaner -> Tagger -> EmbeddingGenerator
- `audio.transcribe`: FileReader -> FormatDetector -> AudioTranscriber -> SpeakerDiarizer -> TimestampAligner -> Sectioner -> Tagger -> EmbeddingGenerator

### Agent 2: Complex Pipelines (US-0005 Video + US-0006 Document)

Depends on Agent 1 for shared steps and providers.

**Video steps (new):**
- `AudioExtractor` -- demuxes audio track via VideoProvider (ffmpeg interface)
- `FrameSampler` -- extracts keyframes at configurable interval
- `SceneDetector` -- detects visual scene changes (histogram-based)
- `FrameOCR` -- OCR on keyframes (reuses OCRProvider from Agent 1)
- `TimelineAssembler` -- merges transcript + scenes + OCR into unified timeline

**Document steps (new):**
- `PDFExtractor` -- PDF text extraction via DocumentProvider
- `MarkdownParser` -- Markdown AST with heading hierarchy
- `LanguageDetector` -- programming language detection
- `ASTParser` -- source code AST parsing
- `FunctionExtractor` -- function/method extraction from AST
- `CommentExtractor` -- TODO/FIXME/HACK annotation extraction
- `ImageExtractor` -- embedded image extraction (creates child objects)
- `TableExtractor` -- table extraction to Markdown format
- `CodeBlockExtractor` -- fenced code block extraction
- `OfficeExtractor` -- DOCX/EPUB extraction
- `HeadingSplitter` -- heading-based section splitting
- `PageSplitter` -- page-based section splitting

**Provider interfaces (new):**
- `VideoProvider` -- ExtractAudio(), SampleFrames(), ProbeMetadata()
- `DocumentProvider` -- ExtractPDF(), ExtractOffice()

**Pipelines:**
- `video.full`: FileReader -> FormatDetector -> AudioExtractor -> AudioTranscriber -> FrameSampler -> SceneDetector -> FrameOCR -> TimelineAssembler -> Sectioner -> Tagger -> EmbeddingGenerator
- `video.audio_only`: FileReader -> FormatDetector -> AudioExtractor -> AudioTranscriber -> Sectioner -> Tagger -> EmbeddingGenerator
- `doc.pdf`: FileReader -> PDFExtractor -> PageSplitter -> ImageExtractor -> TableExtractor -> Sectioner -> Tagger -> EmbeddingGenerator
- `doc.markdown`: FileReader -> MarkdownParser -> HeadingSplitter -> CodeBlockExtractor -> Sectioner -> Tagger -> EmbeddingGenerator
- `doc.code`: FileReader -> LanguageDetector -> ASTParser -> FunctionExtractor -> CommentExtractor -> Sectioner -> Tagger -> EmbeddingGenerator
- `doc.office`: FileReader -> OfficeExtractor -> PageSplitter -> ImageExtractor -> TableExtractor -> Sectioner -> Tagger -> EmbeddingGenerator

### Agent 3: Subscription/Import Pipelines (US-0007 Feed + US-0008 Batch)

Independent from media steps. Adds new storage tables and CLI commands.

**Feed steps (new):**
- `FeedFetcher` -- conditional GET with ETag/Last-Modified
- `FeedParser` -- RSS 2.0, Atom 1.0, JSON Feed 1.1 detection and parsing
- `ItemDeduplicator` -- GUID-based dedup against feed_items table
- `ItemEnqueuer` -- fan-out: each item becomes an ingestion job (url.article or text.long)

**Batch steps (new):**
- `JSONLParser` -- JSONL line-by-line parsing
- `CSVParser` -- CSV/TSV parsing with configurable delimiter
- `ColumnMapper` -- maps CSV columns to KnowledgeObject fields
- `OPMLParser` -- OPML parsing, delegates to feed subsystem
- `DirectoryScanner` -- recursive Markdown directory scanning
- `RecordValidator` -- schema validation per record
- `BatchEnqueuer` -- fan-out with configurable concurrency limit

**Storage additions:**
- `feeds` table -- subscriptions with URL, format, status, sync state
- `feed_items` table -- GUID-based dedup
- `batches` table -- batch import tracking
- New types: Feed, FeedItem, FeedFilter, Batch, BatchFilter, ImportRecord

**CLI additions:**
- `ctxt feed add|list|sync|remove` -- feed subscription management
- `ctxt import --file|--dir [--format] [--dry-run]` -- batch import with progress

**Pipelines:**
- `feed.sync`: FeedFetcher -> FeedParser -> ItemDeduplicator -> ItemEnqueuer
- `batch.jsonl`: FileReader -> JSONLParser -> RecordValidator -> BatchEnqueuer
- `batch.csv`: FileReader -> CSVParser -> ColumnMapper -> RecordValidator -> BatchEnqueuer
- `batch.tsv`: FileReader -> CSVParser -> ColumnMapper -> RecordValidator -> BatchEnqueuer
- `batch.opml`: FileReader -> OPMLParser -> FeedSubscriber
- `batch.directory`: DirectoryScanner -> MarkdownParser -> RecordValidator -> BatchEnqueuer

## Design Principles

- **Provider agnostic**: All external tool calls go through provider interfaces. Stub providers ship as defaults; real implementations are pluggable.
- **Opinionated defaults**: Tesseract for OCR, Whisper for transcription, ffmpeg for video, pandoc for documents. Configurable via YAML.
- **Step reuse**: Steps like FileReader, FormatDetector, Sectioner, Tagger, EmbeddingGenerator are shared across pipelines.
- **Fail gracefully**: Optional steps (diarization, image extraction) are non-fatal. Per-record batch errors don't halt the batch.
- **Async everything**: All pipelines return a job ID immediately. Workers process in background.

## Coordination

- Agent 1 runs first (or in parallel with explicit awareness that its shared code is needed)
- Agents share no files except `registry.go` (each adds distinct pipeline names) and `storage/types.go` (Agent 1 extends Section; Agent 3 adds feed/batch types)
- Provider interfaces live in `internal/providers/` created by Agent 1, extended by Agent 2
- Each agent writes its own tests matching the E2E checklists in the stories

## Testing Strategy

Each step gets a unit test with mock providers. Integration tests validate full pipeline execution with stub providers that return realistic data. E2E tests match the checklists in each story file.
