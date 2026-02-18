# ADR-035 – Docling as Optional Document Parsing Backend

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp needs to parse structured content from documents (PDF, DOCX, PPTX, XLSX, HTML, Markdown) as part of the ingestion write path (ADR-003). User stories US-0002 (URL capture), US-0003 (image OCR), and US-0006 (document parsing and decomposition) all require extracting structured text, tables, headings, code blocks, and metadata from heterogeneous document formats.

The `document.pdf` pipeline (ADR-026) defines these steps:

```
ExtractText → OCRScannedPages → ExtractStructure → PassToTextPipeline
```

The first three steps are pure parsing — they produce structured text from binary content. This is a well-understood problem with several existing solutions. The question is whether to adopt an existing document parsing library or build bespoke parsers.

[Docling](https://github.com/docling-project/docling) (v2.74.0, MIT license, LF AI & Data Foundation) is a document conversion toolkit from IBM Research that parses multiple formats into a unified structured representation. It was evaluated as a potential dependency for the document parsing steps of the ingestion pipeline.

### Evaluation Scope

Docling was assessed against:

- US-0002 (URL capture — PDF extraction from fetched URLs)
- US-0003 (image OCR and analysis)
- US-0006 (document parsing and decomposition)
- ADR-004 (step-based pipeline architecture)
- ADR-012 (plugin extensibility)
- ADR-026 (multimodal content processing)

---

## Decision

**Docling will NOT be adopted as a core dependency. It MAY be offered as an optional, plugin-based backend for the `document.*` and `image.ocr` pipeline steps, installable separately from the core system.**

When present, Docling replaces the default lightweight parsers for PDF, DOCX, PPTX, and XLSX with higher-fidelity structure extraction. When absent, the system falls back to built-in lightweight parsers with no loss of functionality — only reduced fidelity for complex documents.

---

## Rationale

### What Docling Does Well

1. **Best-in-class table extraction.** TableFormer achieves ~97.9% accuracy on complex tables with spanning cells, multi-level headers, and nested content. No lightweight alternative matches this.

2. **Unified multi-format parsing.** Single API handles PDF, DOCX, PPTX, XLSX, HTML, Markdown, images, and audio. Output normalizes to a Pydantic-based `DoclingDocument` with full spatial provenance (page, bounding box, character spans).

3. **Modular pipeline architecture.** Four internal pipelines (StandardPdfPipeline, VlmPipeline, SimplePipeline, AsrPipeline) with pluggy-based extensibility for OCR engines, layout models, and table models.

4. **Local-first execution.** MIT license, runs fully offline, models downloaded once and cached. Aligns with ADR-001.

5. **Active maintenance.** 153+ releases, production-stable since December 2025, two peer-reviewed publications (AAAI 2025), governed by LF AI & Data Foundation.

### Why Not a Core Dependency

1. **Heavyweight footprint contradicts local-first simplicity.** Docling requires PyTorch (~600MB–2GB), ONNX Runtime, and multiple ML model weights (~632K downloads for layout+table models alone). Runtime memory is ~6.2GB during PDF processing. This is unacceptable for a Go CLI tool that targets zero-install, single-binary distribution (ADR-002).

2. **Scope mismatch.** Docling is a parser. ContextHelp's ingestion is a knowledge substrate. Docling produces structured text; ContextHelp needs entity extraction (`@namespace.slug`), decision/task extraction, tag assignment, summary generation, embedding creation, and graph construction — none of which Docling provides. Docling covers approximately one step (`ExtractStructure`) in one pipeline (`document.pdf`) out of 8 ingestion categories and 20+ pipeline variants.

3. **Language boundary.** Docling is Python. The core system is Go (ADR-002). Adopting Docling as a core dependency would require either embedding a Python runtime (heavy, fragile), running Docling as a sidecar service (operational complexity), or using its MCP server (network dependency). All options add significant complexity for a narrow benefit.

4. **80/20 alternatives exist.** For basic PDF text extraction, `pypdfium2` or Go-native PDF libraries achieve adequate results at a fraction of the cost. For DOCX/PPTX/XLSX, lightweight parsing libraries exist in Go. The marginal value of Docling over these alternatives only manifests for complex tables and unusual layouts — a minority of the documents most users will ingest.

5. **GPU acceleration immature.** Docling's GPU support is self-described as "work-in-progress and largely untested." CPU-only OCR performance is slow (EasyOCR: 30+ sec/page, TableFormer: 2-6 sec/table). This latency is acceptable for async background jobs but limits batch processing throughput.

### Alternatives Considered

| Alternative | Strengths | Weaknesses | Verdict |
|---|---|---|---|
| **Docling (core)** | Best tables, unified API | Heavy, Python, scope mismatch | Rejected as core |
| **Docling (plugin)** | Best tables when needed, opt-in | Requires Python sidecar | Accepted |
| **Go-native parsers** (pdfcpu, goldmark, docconv) | Lightweight, same language, fast | Weak table extraction, no layout analysis | Default path |
| **LlamaParse** | Good quality | Proprietary API, not local-first | Rejected |
| **Unstructured** | Multi-format | Heavy, slow (51s/page), mixed license | Rejected |
| **Marker** | Moderate quality, open source | GPL license, limited extensibility | Rejected |
| **Build bespoke** | Full control | High effort, reinventing solved problems | Rejected for complex cases |

---

## Consequences

### Positive

- Core remains a single Go binary with zero Python dependencies
- Users who need high-fidelity document parsing can opt in via `ctxt plugin install docling`
- Plugin architecture (ADR-012) validated — document parsing is a clean plugin boundary
- Default lightweight parsers keep ingestion fast for common cases (text, URLs, simple PDFs)
- No vendor lock-in — Docling is MIT, and the plugin interface accepts any parser

### Negative

- Default document parsing quality is lower for complex tables and unusual layouts
- Plugin requires Python runtime on user's machine (or a container)
- Two code paths to test and maintain (lightweight default + Docling plugin)
- Users may not know they need the plugin until they encounter a poorly parsed document

### Neutral / Considerations

- The plugin interface for document parsing (`DocumentParser`) must be stable and well-defined before the Docling plugin can be built
- Docling's `DoclingDocument` output must be mapped to ContextHelp's knowledge object schema — spatial provenance (bounding boxes) will be discarded; structural provenance (headings, sections, tables) will be preserved
- Future VLM-based document understanding models (GraniteDocling, SmolDocling) may shift the cost/quality tradeoff — revisit when models become lightweight enough for local inference

---

## Implementation Notes

### Default Path (No Plugin)

The `document.pdf` pipeline uses Go-native libraries:

- **PDF text extraction:** pdfcpu or similar Go library
- **DOCX/PPTX/XLSX:** Go libraries for Office Open XML
- **HTML:** golang.org/x/net/html or goquery
- **Markdown:** goldmark

These produce plain text with basic structure (headings, paragraphs, lists). Tables are extracted as best-effort text. This feeds into `PassToTextPipeline` for enrichment.

### Plugin Path (Docling Installed)

When the Docling plugin is present:

1. Plugin registers as a `DocumentParser` provider for MIME types it supports
2. Pipeline step `ExtractStructure` delegates to plugin instead of default parser
3. Plugin invokes Docling via subprocess or local HTTP API (docling-serve)
4. Plugin maps `DoclingDocument` → ContextHelp section/table/heading structure
5. Enriched structure feeds into `PassToTextPipeline` as usual

### Plugin Interface

```go
// DocumentParser is the extension point for document structure extraction.
// Default implementation uses lightweight Go-native parsers.
// Plugins (e.g., Docling) may register as providers for specific MIME types.
type DocumentParser interface {
    SupportedMIMETypes() []string
    Parse(ctx context.Context, content io.Reader, opts ParseOptions) (ParseResult, error)
}

type ParseOptions struct {
    MIMEType    string
    Filename    string
    EnableOCR   bool
    EnableTables bool
    Language    string
}

type ParseResult struct {
    Sections []Section
    Tables   []Table
    Metadata map[string]string
}
```

### Installation

```bash
# Default: no Docling, lightweight parsers
ctxt ingest document.pdf report.pdf

# With Docling plugin: high-fidelity parsing
ctxt plugin install docling
ctxt ingest document.pdf report.pdf  # automatically uses Docling
```

---

## References

- ADR-001 – Local-First and Decentralized
- ADR-002 – Use Go
- ADR-004 – Step-Based Pipeline Architecture
- ADR-005 – Decorator Pattern for AI Providers
- ADR-012 – Plugins Extend Any Layer
- ADR-026 – Multimodal Content Processing
- US-0002 – URL Capture and Extraction
- US-0003 – Image OCR and Analysis
- US-0006 – Document Parsing and Decomposition
- [Docling GitHub Repository](https://github.com/docling-project/docling)
- [Docling Technical Report (arXiv:2408.09869)](https://arxiv.org/html/2408.09869v5)
- [Docling Toolkit Paper (arXiv:2501.17887)](https://arxiv.org/html/2501.17887v1)
- [PDF Extraction Benchmark (Procycons)](https://procycons.com/en/blogs/pdf-data-extraction-benchmark/)

---
