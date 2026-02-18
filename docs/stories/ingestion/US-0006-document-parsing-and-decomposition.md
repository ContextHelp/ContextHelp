# US-0006: Document Parsing and Decomposition

**System Types:** ctxt, dpkms (self-hosted)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As a knowledge worker, I want to add documents (PDFs, Markdown files, source code, DOCX, EPUB, HTML) and have them automatically parsed, structurally decomposed, and indexed so that I can search and retrieve knowledge at the section, function, or paragraph level rather than treating the entire document as a single opaque blob.

---

## Context

Documents are the backbone of organizational knowledge -- design specs, architecture decision records, onboarding guides, research papers, codebases, contracts. Yet most knowledge systems index documents as flat text: the entire PDF becomes one search result, forcing the user to manually locate the relevant paragraph in a 40-page spec.

The problem compounds with heterogeneous formats. A PDF has pages, headings, tables, and embedded images. A Markdown file has heading hierarchy and code blocks. Source code has functions, classes, and doc comments. Each format has its own structural semantics, and ignoring that structure means losing the very organization the author intended. A developer searching for "retry logic" should find the specific function, not a link to a 2,000-line file.

This story implements hierarchical decomposition: a document is parsed into its native structural units, and each unit becomes a searchable Section within the parent KnowledgeObject. For large documents (multi-chapter PDFs, codebases), the system creates child KnowledgeObjects linked to the parent via edges, enabling both broad document-level search and precise section-level retrieval. Embedded images within documents trigger child objects that are processed through the image pipeline (US-0003), maintaining provenance links back to the source document and page.

---

## Acceptance Criteria

- [ ] User can add documents via `ctxt add --file report.pdf` with automatic format detection
- [ ] Supported formats: PDF, Markdown (.md), source code (.go, .py, .js, .ts, .rs, .java, etc.), DOCX, EPUB, HTML
- [ ] System selects the correct pipeline based on detected format (`doc.pdf`, `doc.markdown`, `doc.code`, `doc.office`)
- [ ] Documents are decomposed into structural Sections (headings, pages, functions) preserving hierarchy
- [ ] Large documents produce child KnowledgeObjects linked to parent via edges
- [ ] Embedded images are extracted and processed as child objects via the image pipeline
- [ ] Document metadata (page count, language, author, creation date) stored in Metadata
- [ ] Processing is async -- returns job ID immediately
- [ ] Corrupt, password-protected, or oversized files are rejected with clear error messages
- [ ] Individual sections are independently searchable after processing

---

## Implementation Notes

### CLI Interface

```bash
# Basic document capture -- format auto-detected
ctxt add --file report.pdf

# Explicit type hint
ctxt add --file README.md --type document

# Explicit pipeline selection
ctxt add --file main.go --pipeline doc.code

# Add a DOCX file with profile
ctxt add --file design-spec.docx --profile engineering --project mobile-app

# Returns immediately with JSON
{
  "job_id": "j-doc-4e7a12",
  "object_id": "o-doc-b19f83",
  "status": "pending_enrichment",
  "pipeline": "doc.pdf",
  "source": "/home/user/report.pdf",
  "metadata": {
    "format": "pdf",
    "pages": 42,
    "size_bytes": 3145728
  }
}

# Check job progress
ctxt job get j-doc-4e7a12
{
  "job_id": "j-doc-4e7a12",
  "status": "processing",
  "pipeline": "doc.pdf",
  "progress": {
    "current_step": "TableExtractor",
    "steps_completed": 4,
    "steps_total": 8,
    "percent": 50
  }
}
```

### Format Detection Logic

```go
type FormatDetector struct{}

func (d *FormatDetector) Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error) {
    ext := strings.ToLower(filepath.Ext(draft.Source.Path))

    switch ext {
    case ".pdf":
        draft.Subtype = "pdf"
        draft.Pipeline = "doc.pdf"
    case ".md", ".markdown":
        draft.Subtype = "markdown"
        draft.Pipeline = "doc.markdown"
    case ".go", ".py", ".js", ".ts", ".rs", ".java", ".rb", ".cpp", ".c", ".cs":
        draft.Subtype = "code"
        draft.Pipeline = "doc.code"
    case ".docx", ".doc", ".odt", ".rtf":
        draft.Subtype = "office"
        draft.Pipeline = "doc.office"
    case ".epub":
        draft.Subtype = "epub"
        draft.Pipeline = "doc.office"
    case ".html", ".htm":
        draft.Subtype = "html"
        draft.Pipeline = "doc.markdown" // HTML parsed similarly to Markdown
    default:
        return nil, fmt.Errorf("unsupported document format: %s", ext)
    }

    return draft, nil
}
```

### Pipeline Steps

**`doc.pdf`** (PDF documents):

```
FileReader → PDFExtractor → PageSplitter → ImageExtractor →
TableExtractor → Sectioner → Tagger → EmbeddingGenerator
```

**`doc.markdown`** (Markdown and HTML files):

```
FileReader → MarkdownParser → HeadingSplitter → CodeBlockExtractor →
Sectioner → Tagger → EmbeddingGenerator
```

**`doc.code`** (Source code files):

```
FileReader → LanguageDetector → ASTParser → FunctionExtractor →
CommentExtractor → Sectioner → Tagger → EmbeddingGenerator
```

**`doc.office`** (DOCX, EPUB, RTF):

```
FileReader → OfficeExtractor → PageSplitter → ImageExtractor →
TableExtractor → Sectioner → Tagger → EmbeddingGenerator
```

Each step implements the `PipelineStep` interface:

```go
type PipelineStep interface {
    Name() string
    Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error)
}
```

Step responsibilities:

| Step | Input | Output |
|------|-------|--------|
| FileReader | File path | Raw bytes, file metadata (size, modification time) |
| PDFExtractor | PDF bytes | Extracted text per page, document metadata (author, title, page count) |
| MarkdownParser | Markdown/HTML text | Parsed AST with heading hierarchy, links, code blocks |
| LanguageDetector | Source code text | Detected programming language, confidence score |
| ASTParser | Source code + language | Abstract syntax tree with function/class/method nodes |
| PageSplitter | Extracted pages | Section per page or per heading-delimited region |
| HeadingSplitter | Markdown AST | Section per heading, preserving nesting depth |
| FunctionExtractor | AST | Section per function/method with signature, body, doc comment |
| CommentExtractor | AST | Standalone comments, TODO/FIXME annotations |
| ImageExtractor | Document bytes | Extracted images as child KnowledgeObjects (triggers US-0003 pipeline) |
| TableExtractor | Document pages | Tables converted to structured data (Markdown table format) |
| CodeBlockExtractor | Markdown AST | Fenced code blocks extracted with language annotation |
| Sectioner | Extracted structure | Hierarchical Sections with parent-child relationships |
| Tagger | Sections + full text | Tags from vocabulary |
| EmbeddingGenerator | Sections + full text | Embeddings per section + full-document embedding |

### REST API

```
POST /analyze
Content-Type: multipart/form-data

file: <binary document data>
content_type: application/pdf
pipeline: doc.pdf
profile: engineering

-> 202 Accepted
{
  "job_id": "j-doc-4e7a12",
  "object_id": "o-doc-b19f83",
  "status": "pending_enrichment",
  "pipeline": "doc.pdf"
}
```

Retrieving the processed document with its decomposition:

```
GET /objects/o-doc-b19f83

-> 200 OK
{
  "id": "o-doc-b19f83",
  "type": "document",
  "subtype": "pdf",
  "raw_content": "<full extracted text>",
  "source": {
    "type": "file",
    "path": "/home/user/report.pdf"
  },
  "metadata": {
    "format": "pdf",
    "pages": 42,
    "author": "Jane Smith",
    "title": "Q1 Architecture Review",
    "created_at": "2026-01-15T09:00:00Z",
    "language": "en"
  },
  "sections": [
    {
      "id": "sec-001",
      "title": "Executive Summary",
      "depth": 1,
      "content": "This document outlines the architectural decisions...",
      "page_start": 1,
      "page_end": 2
    },
    {
      "id": "sec-002",
      "title": "System Overview",
      "depth": 1,
      "content": "The platform consists of three core services...",
      "page_start": 3,
      "page_end": 7
    },
    {
      "id": "sec-003",
      "title": "Authentication Service",
      "depth": 2,
      "parent_section": "sec-002",
      "content": "Authentication is handled via OAuth2 with...",
      "page_start": 4,
      "page_end": 5
    }
  ],
  "children": [
    {
      "object_id": "o-img-f3a921",
      "type": "image",
      "relationship": "embedded_image",
      "metadata": {"source_page": 5, "caption": "Figure 3: Auth flow diagram"}
    }
  ],
  "tags": ["architecture", "authentication", "oauth2"],
  "mentions": [{"entity": "@auth-service"}, {"entity": "@platform-team"}]
}
```

### Backend Processing

1. `ctxt` receives file via CLI (`--file`) or REST API (multipart upload)
2. FileReader reads document from disk, stores file reference in `Source`, extracts file metadata
3. Format-specific extractor runs (PDFExtractor, MarkdownParser, ASTParser, or OfficeExtractor) to convert raw bytes into structured text
4. Splitter step divides content into structural units based on format semantics: pages and headings for PDF, heading hierarchy for Markdown, functions and classes for code
5. ImageExtractor (PDF/Office only) identifies embedded images, extracts them, and creates child KnowledgeObjects that are queued for processing through the image pipeline (US-0003)
6. TableExtractor (PDF/Office only) identifies tables and converts them to structured Markdown table format, preserving them as annotated content within the relevant Section
7. CodeBlockExtractor (Markdown only) extracts fenced code blocks with language annotations, preserving them within Sections but also indexing them independently for code search
8. Sectioner assembles the structural units into a hierarchical Section tree; for documents exceeding the configured decomposition depth, creates child KnowledgeObjects linked to the parent via edges in the graph
9. Tagger assigns tags from vocabulary based on Section content and full-text analysis
10. EmbeddingGenerator creates embeddings for each Section (enabling section-level search) plus a full-document embedding
11. Parent KnowledgeObject is persisted with all Sections; child objects (chapters, embedded images) are persisted with graph edges linking them to the parent
12. Graph edges created for mentions, entities, and parent-child relationships
13. Job status updated to `completed`

### Hierarchical Decomposition

Large documents are decomposed into a tree of KnowledgeObjects:

```
report.pdf (parent: o-doc-b19f83)
  ├── Chapter 1: Executive Summary (child: o-doc-ch1-a1b2)
  ├── Chapter 2: System Overview (child: o-doc-ch2-c3d4)
  │   ├── Section 2.1: Auth Service (within child, as Section)
  │   ├── Section 2.2: API Gateway (within child, as Section)
  │   └── Figure 3: Auth flow (image child: o-img-f3a921)
  ├── Chapter 3: Data Layer (child: o-doc-ch3-e5f6)
  └── Appendix A: API Reference (child: o-doc-app-g7h8)
```

Edges in the graph:

```
o-doc-b19f83 --[has_chapter]--> o-doc-ch1-a1b2
o-doc-b19f83 --[has_chapter]--> o-doc-ch2-c3d4
o-doc-ch2-c3d4 --[has_image]--> o-img-f3a921
```

The decomposition depth is configurable. At depth 0, no children are created (everything stays as Sections within the parent). At depth 1, top-level headings become children. At depth 2, sub-headings also become children.

### Code Pipeline Detail

For source code files, the pipeline leverages AST parsing for precise structural decomposition:

```go
// Example: processing a Go file
// Input: main.go with 3 functions and 2 types

// LanguageDetector output:
draft.Metadata["language"] = "go"
draft.Metadata["confidence"] = 0.99

// ASTParser + FunctionExtractor output:
draft.Sections = []Section{
    {
        ID:      "sec-pkg",
        Title:   "Package: main",
        Content: "// Package main implements the CLI entry point.",
        Metadata: map[string]any{
            "node_type": "package",
        },
    },
    {
        ID:      "sec-fn-main",
        Title:   "func main()",
        Content: "func main() {\n    app := cli.NewApp()\n    ...\n}",
        Metadata: map[string]any{
            "node_type":   "function",
            "line_start":  15,
            "line_end":    42,
            "doc_comment":  "main is the CLI entry point.",
            "complexity":  4,
        },
    },
    {
        ID:      "sec-fn-run",
        Title:   "func run(ctx context.Context, args []string) error",
        Content: "func run(ctx context.Context, args []string) error {\n    ...\n}",
        Metadata: map[string]any{
            "node_type":   "function",
            "line_start":  44,
            "line_end":    89,
            "doc_comment":  "run executes the main application logic.",
            "complexity":  7,
        },
    },
    // CommentExtractor adds:
    {
        ID:      "sec-todo-1",
        Title:   "TODO: Add retry logic",
        Content: "// TODO(jsmith): Add retry logic for transient failures",
        Metadata: map[string]any{
            "node_type":  "comment",
            "annotation": "TODO",
            "line":       67,
            "author":     "jsmith",
        },
    },
}
```

### Configuration

```yaml
# In configuration.yaml
document:
  # Maximum file size (bytes). Default: 100MB
  maxFileSize: 104857600

  # Decomposition depth: 0 = flat (sections only), 1 = chapters, 2 = sub-sections
  decompositionDepth: 1

  pdf:
    # Maximum page count. Default: 500
    maxPages: 500
    # Extract embedded images
    extractImages: true
    # Extract tables
    extractTables: true
    # OCR fallback for scanned PDFs
    ocrFallback: true
    ocrProvider: ${aiProvider}

  markdown:
    # Maximum heading depth to split on (1-6)
    splitHeadingDepth: 3
    # Extract and index code blocks separately
    extractCodeBlocks: true

  code:
    # Languages to parse (empty = all detected)
    languageWhitelist: []
    # Extract TODO/FIXME/HACK annotations
    extractAnnotations: true
    # Include complexity metrics per function
    complexityMetrics: true
    # Supported languages for AST parsing
    supportedLanguages:
      - go
      - python
      - javascript
      - typescript
      - rust
      - java
      - ruby
      - cpp
      - c
      - csharp

  office:
    # DOCX/EPUB extraction engine
    engine: pandoc  # or: native, libreoffice
    # Extract embedded media
    extractMedia: true
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt add --file report.pdf` returns job ID within 2 seconds
- [ ] CLI: Job status transitions from `pending` -> `processing` -> `completed`
- [ ] PDF: 42-page PDF produces parent object with hierarchical Sections matching heading structure
- [ ] PDF: Embedded images extracted as child KnowledgeObjects with `has_image` edges
- [ ] PDF: Tables extracted and stored as structured Markdown within Sections
- [ ] PDF: Password-protected PDF rejected with clear error message
- [ ] Markdown: Heading hierarchy preserved (h1 > h2 > h3 nesting) in Section tree
- [ ] Markdown: Fenced code blocks extracted with language annotation
- [ ] Code: Go file parsed into function-level Sections with signatures and doc comments
- [ ] Code: Python file parsed into class and method Sections
- [ ] Code: TODO/FIXME annotations extracted as separate Sections
- [ ] Code: Unsupported language falls back to line-based splitting (not AST)
- [ ] Office: DOCX file parsed and decomposed similarly to PDF
- [ ] Error: Corrupt file rejected with descriptive error message
- [ ] Error: File exceeding `maxFileSize` (default 100MB) rejected before processing starts
- [ ] REST API: Multipart upload returns 202 + job ID
- [ ] Search: Section-level search returns specific function or heading, not entire document
- [ ] Hierarchy: Child objects linked to parent via graph edges, traversable via API

---

## Related Stories

- [US-0001](./US-0001-text-capture-minimal-friction.md) -- Base capture flow and async job pattern
- [US-0002](./US-0002-url-capture-and-extraction.md) -- URL-to-PDF pipeline triggers `doc.pdf` for fetched PDF content
- [US-0003](./US-0003-image-ocr-and-analysis.md) -- Embedded images from documents are processed through the image pipeline
- [US-0008](./US-0008-batch-import-from-file.md) -- Batch import of multiple documents triggers individual `doc.*` pipelines
- [US-0012](../enrichment/US-0012-generate-summaries-and-sections.md) -- Sectioner step shared with enrichment pipeline
- [US-0013](../enrichment/US-0013-detect-and-extract-code-snippets.md) -- Code snippet extraction enrichment complements `doc.code` pipeline
- [US-0050](../enrichment/US-0050-extract-code-metrics-and-complexity.md) -- Code metrics enrichment uses AST data produced by `doc.code` pipeline
