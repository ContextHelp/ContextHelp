# US-0203: arXiv Paper Capture and Indexing

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/knowledge-workers.md), [Agents & LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a researcher, I want to capture arXiv papers by URL or ID and have them automatically decomposed into searchable sections with extracted metadata (authors, abstract, citations, BibTeX) so that academic knowledge becomes part of my knowledge base.

---

## Context

Academic papers are the bedrock of technical knowledge. Every significant advance in machine learning, distributed systems, cryptography, and adjacent fields is first documented as an arXiv preprint before (or instead of) formal journal publication. Researchers, engineers, and technical leaders routinely encounter arXiv links in Twitter threads, Slack channels, blog posts, and reading group recommendations. Yet the knowledge inside these papers remains trapped in PDFs -- unsearchable, unlinked, and disconnected from the rest of the knowledge base.

The problem is compounded by volume. A machine learning researcher might encounter 5-10 paper recommendations per day. Reading each paper takes 30-60 minutes; extracting and recording the key insights takes another 15 minutes. Without automation, the choice becomes "read and forget" or "read and spend significant effort capturing." Most researchers default to the first option, building a personal collection of PDF files that they never revisit because they cannot search them effectively.

This story bridges that gap. Given an arXiv URL or shorthand ID (e.g., `arxiv:2401.12345`), the system fetches the paper metadata from the arXiv API, downloads the PDF, processes it through the document parsing pipeline (US-0006) for section decomposition, extracts structured metadata (authors, abstract, categories, BibTeX), and parses the references section into structured citations. Each author becomes an entity in the knowledge graph, enabling queries like "show me all papers by Ilya Sutskever" or "what papers cite the attention mechanism paper." The citation graph creates edges between papers, building an explorable web of academic knowledge that grows richer with each captured paper.

---

## Acceptance Criteria

- [ ] `ctxt capture https://arxiv.org/abs/2401.12345` captures paper by URL
- [ ] `ctxt capture arxiv:2401.12345` works with shorthand ID syntax
- [ ] `ctxt capture https://arxiv.org/pdf/2401.12345` also works (PDF URL variant)
- [ ] Extracts: title, authors, abstract, full text (from PDF), BibTeX, categories, submission date
- [ ] PDF downloaded and processed through `doc.pdf` pipeline for section decomposition
- [ ] Creates entities for each author (linked to canonical entities if previously captured)
- [ ] Extracts citation graph: references section parsed into structured citations
- [ ] Known cited papers (already in knowledge base) linked via `cites` edges
- [ ] Stores arXiv metadata (categories, comments, journal-ref, DOI) in object Metadata
- [ ] BibTeX citation stored for easy export and reference management
- [ ] Processing is async -- returns job ID immediately
- [ ] Handles arXiv API rate limiting (no more than 1 request per 3 seconds)
- [ ] Multiple revisions: captures the latest version by default, `--version v2` for specific versions

---

## Implementation Notes

### CLI Interface

```bash
# Capture by arXiv abstract URL
ctxt capture https://arxiv.org/abs/2401.12345
{
  "job_id": "j-arxiv-1a2b3c",
  "object_id": "o-arxiv-4d5e6f",
  "status": "pending_enrichment",
  "pipeline": "academic.arxiv",
  "source": "https://arxiv.org/abs/2401.12345",
  "metadata": {
    "arxiv_id": "2401.12345",
    "title": "Scaling Laws for Neural Language Models",
    "authors": ["Jared Kaplan", "Sam McCandlish", "Tom Henighan"],
    "categories": ["cs.LG", "cs.CL"]
  }
}

# Capture by shorthand ID
ctxt capture arxiv:2401.12345
{
  "job_id": "j-arxiv-7g8h9i",
  "object_id": "o-arxiv-0j1k2l",
  "status": "pending_enrichment",
  "pipeline": "academic.arxiv",
  "source": "arxiv:2401.12345"
}

# Capture a specific version
ctxt capture arxiv:2401.12345 --version v2

# Capture by PDF URL
ctxt capture https://arxiv.org/pdf/2401.12345

# Check job progress
ctxt job get j-arxiv-1a2b3c
{
  "job_id": "j-arxiv-1a2b3c",
  "status": "processing",
  "pipeline": "academic.arxiv",
  "progress": {
    "current_step": "PDFExtractor",
    "steps_completed": 3,
    "steps_total": 9,
    "percent": 33
  }
}

# Search across captured papers
ctxt search "attention mechanism transformer" --type academic
[
  {
    "object_id": "o-arxiv-4d5e6f",
    "title": "Attention Is All You Need",
    "authors": ["Ashish Vaswani", "Noam Shazeer", "..."],
    "relevance": 0.95
  }
]
```

### REST API

```
POST /api/v1/analyze
Content-Type: application/json

{
  "source_type": "arxiv",
  "source_url": "https://arxiv.org/abs/2401.12345",
  "options": {
    "version": "latest",
    "extract_citations": true,
    "extract_bibtex": true,
    "decompose_sections": true
  }
}

-> 202 Accepted
{
  "job_id": "j-arxiv-1a2b3c",
  "object_id": "o-arxiv-4d5e6f",
  "status": "pending_enrichment",
  "pipeline": "academic.arxiv"
}
```

Retrieving the captured paper:

```
GET /api/v1/objects/o-arxiv-4d5e6f

-> 200 OK
{
  "id": "o-arxiv-4d5e6f",
  "type": "academic",
  "subtype": "arxiv",
  "raw_content": "<full extracted paper text>",
  "source": {
    "type": "arxiv",
    "url": "https://arxiv.org/abs/2401.12345",
    "arxiv_id": "2401.12345",
    "version": "v3",
    "pdf_url": "https://arxiv.org/pdf/2401.12345v3",
    "captured_at": "2026-02-18T10:30:45Z"
  },
  "metadata": {
    "title": "Scaling Laws for Neural Language Models",
    "authors": [
      {"name": "Jared Kaplan", "affiliation": "Johns Hopkins University"},
      {"name": "Sam McCandlish", "affiliation": "OpenAI"},
      {"name": "Tom Henighan", "affiliation": "OpenAI"}
    ],
    "abstract": "We study empirical scaling laws for language model performance...",
    "categories": ["cs.LG", "cs.CL", "stat.ML"],
    "primary_category": "cs.LG",
    "submitted": "2024-01-15",
    "updated": "2024-03-22",
    "version": "v3",
    "doi": "10.xxxx/xxxxx",
    "journal_ref": null,
    "comments": "20 pages, 12 figures",
    "bibtex": "@article{kaplan2024scaling,\n  title={Scaling Laws for Neural Language Models},\n  author={Kaplan, Jared and McCandlish, Sam and Henighan, Tom},\n  journal={arXiv preprint arXiv:2401.12345},\n  year={2024}\n}",
    "citation_count_extracted": 47,
    "page_count": 20,
    "figure_count": 12
  },
  "sections": [
    {
      "id": "sec-abstract",
      "title": "Abstract",
      "content": "We study empirical scaling laws for language model performance...",
      "depth": 0
    },
    {
      "id": "sec-001",
      "title": "1. Introduction",
      "content": "Recent work has shown that the performance of neural language models...",
      "depth": 1,
      "page_start": 1,
      "page_end": 3
    },
    {
      "id": "sec-002",
      "title": "2. Background and Methods",
      "content": "We train a series of Transformer language models...",
      "depth": 1,
      "page_start": 3,
      "page_end": 6
    },
    {
      "id": "sec-002-1",
      "title": "2.1 Dataset and Training",
      "content": "We use a large corpus of English text...",
      "depth": 2,
      "parent_section": "sec-002",
      "page_start": 4,
      "page_end": 5
    },
    {
      "id": "sec-refs",
      "title": "References",
      "content": "[1] Vaswani et al., Attention Is All You Need, 2017...",
      "depth": 1,
      "page_start": 18,
      "page_end": 20
    }
  ],
  "children": [
    {
      "object_id": "o-img-fig1-a1b2",
      "type": "image",
      "relationship": "figure",
      "metadata": {"caption": "Figure 1: Language model loss vs. compute budget", "page": 4}
    }
  ],
  "citations": [
    {
      "ref_number": 1,
      "title": "Attention Is All You Need",
      "authors": ["Ashish Vaswani", "Noam Shazeer"],
      "year": 2017,
      "venue": "NeurIPS",
      "arxiv_id": "1706.03762",
      "linked_object": "o-arxiv-attn-x1y2"
    },
    {
      "ref_number": 2,
      "title": "BERT: Pre-training of Deep Bidirectional Transformers",
      "authors": ["Jacob Devlin", "Ming-Wei Chang"],
      "year": 2019,
      "venue": "NAACL",
      "arxiv_id": "1810.04805",
      "linked_object": null
    }
  ],
  "tags": ["scaling-laws", "language-models", "neural-networks", "transformers"],
  "mentions": [
    {"entity": "@person.jared-kaplan"},
    {"entity": "@person.sam-mccandlish"},
    {"entity": "@org.openai"},
    {"entity": "@org.johns-hopkins"}
  ]
}
```

### Pipeline Steps

**`academic.arxiv`** (arXiv paper capture):

```
ArxivFetcher -> MetadataExtractor -> PDFDownloader -> PDFExtractor -> CitationParser -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

Step responsibilities:

| Step | Input | Output |
|------|-------|--------|
| ArxivFetcher | arXiv ID or URL | arXiv API response (Atom XML with metadata) |
| MetadataExtractor | Atom XML response | Structured metadata: title, authors, abstract, categories, dates, BibTeX |
| PDFDownloader | arXiv PDF URL | Downloaded PDF file in temp storage |
| PDFExtractor | PDF bytes | Full text extraction with page-level structure (reuses US-0006 `doc.pdf` step) |
| CitationParser | Extracted references section | Structured citation list with parsed author, title, year, venue, arXiv ID |
| EntityResolver | Authors + citations | Creates author entities, links known cited papers, creates organization entities |
| Sectioner | Full text + heading structure | Hierarchical Sections matching paper heading structure |
| Tagger | Sections + abstract + categories | Tags from vocabulary + arXiv categories mapped to tags |
| EmbeddingGenerator | Sections + abstract | Embeddings per section + full-paper embedding + abstract embedding |

### arXiv API Integration

```go
type ArxivFetcher struct {
    httpClient  *http.Client
    rateLimiter *rate.Limiter  // 1 request per 3 seconds per arXiv policy
}

func (f *ArxivFetcher) Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error) {
    arxivID := f.extractID(draft.Source.URL)

    // Rate limit: arXiv asks for max 1 request per 3 seconds
    if err := f.rateLimiter.Wait(ctx); err != nil {
        return nil, fmt.Errorf("arxiv_fetcher: rate limit wait: %w", err)
    }

    apiURL := fmt.Sprintf("http://export.arxiv.org/api/query?id_list=%s", arxivID)
    resp, err := f.httpClient.Get(apiURL)
    if err != nil {
        return nil, fmt.Errorf("arxiv_fetcher: API request failed: %w", err)
    }
    defer resp.Body.Close()

    feed, err := parseAtomFeed(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("arxiv_fetcher: parse error: %w", err)
    }

    if len(feed.Entries) == 0 {
        return nil, fmt.Errorf("arxiv_fetcher: paper not found: %s", arxivID)
    }

    entry := feed.Entries[0]
    draft.Metadata["arxiv_id"] = arxivID
    draft.Metadata["title"] = entry.Title
    draft.Metadata["abstract"] = entry.Summary
    draft.Metadata["authors"] = entry.Authors
    draft.Metadata["categories"] = entry.Categories
    draft.Metadata["published"] = entry.Published
    draft.Metadata["updated"] = entry.Updated
    draft.Metadata["pdf_url"] = entry.PDFLink()

    return draft, nil
}

// extractID handles multiple URL formats:
//   "https://arxiv.org/abs/2401.12345"   -> "2401.12345"
//   "https://arxiv.org/pdf/2401.12345"   -> "2401.12345"
//   "arxiv:2401.12345"                    -> "2401.12345"
//   "2401.12345"                          -> "2401.12345"
//   "https://arxiv.org/abs/2401.12345v2"  -> "2401.12345v2"
func (f *ArxivFetcher) extractID(input string) string {
    // Strip URL prefix variations
    for _, prefix := range []string{
        "https://arxiv.org/abs/",
        "http://arxiv.org/abs/",
        "https://arxiv.org/pdf/",
        "http://arxiv.org/pdf/",
        "arxiv:",
    } {
        if strings.HasPrefix(input, prefix) {
            return strings.TrimSuffix(strings.TrimPrefix(input, prefix), ".pdf")
        }
    }
    return input
}
```

### Citation Parser

```go
type CitationParser struct {
    aiProvider AIProvider  // For complex reference parsing
}

func (p *CitationParser) Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error) {
    // Find the references section
    var refsSection *Section
    for i, s := range draft.Sections {
        if isReferencesSection(s.Title) {
            refsSection = &draft.Sections[i]
            break
        }
    }

    if refsSection == nil {
        return draft, nil  // No references found, continue without citations
    }

    // Parse individual references
    refs := splitReferences(refsSection.Content)
    citations := make([]Citation, 0, len(refs))

    for i, ref := range refs {
        citation := parseReference(ref)
        citation.RefNumber = i + 1

        // Try to match arXiv ID in reference text
        if arxivID := extractArxivIDFromText(ref); arxivID != "" {
            citation.ArxivID = arxivID
        }

        citations = append(citations, citation)
    }

    draft.Metadata["citations"] = citations
    draft.Metadata["citation_count_extracted"] = len(citations)

    return draft, nil
}
```

### Backend Processing

1. User provides arXiv URL or shorthand ID via CLI (`ctxt capture arxiv:2401.12345`) or REST API
2. ArxivFetcher normalizes the input to an arXiv ID and queries the arXiv Atom API
3. ArxivFetcher respects arXiv rate limits (1 request per 3 seconds) via rate limiter
4. MetadataExtractor parses the Atom response: title, authors with affiliations, abstract, categories, dates, comments, DOI, journal-ref
5. MetadataExtractor generates BibTeX citation from the extracted metadata
6. PDFDownloader fetches the PDF from `arxiv.org/pdf/{id}` and stores it in temp storage
7. PDFExtractor (reused from US-0006 `doc.pdf` pipeline) extracts full text with page-level structure, heading detection, and figure extraction
8. Embedded figures are extracted as child KnowledgeObjects and processed through the image pipeline (US-0003)
9. CitationParser locates the References section and parses individual citations into structured data: author, title, year, venue, arXiv ID (if present)
10. EntityResolver creates entities for each author (`@person.firstname-lastname`) with affiliation metadata
11. EntityResolver links to canonical entities if the author was previously captured (e.g., from their X/Twitter profile or another paper)
12. For cited papers that already exist in the knowledge base (matched by arXiv ID), creates `cites` edges
13. Sectioner creates hierarchical Sections matching the paper's heading structure (1. Introduction, 2. Methods, 2.1 Dataset, etc.)
14. Tagger assigns tags from vocabulary; arXiv categories (e.g., `cs.LG`) mapped to human-readable tags (e.g., `machine-learning`)
15. EmbeddingGenerator creates per-section embeddings, full-paper embedding, and a dedicated abstract embedding for abstract-to-abstract similarity search
16. Job status updated to `completed`

### Configuration

```yaml
# In configuration.yaml
arxiv:
  # arXiv API rate limit (requests per second)
  # arXiv requests max 1 req / 3 seconds
  rateLimitPerSecond: 0.33

  # Default version to capture ("latest" or "v1", "v2", etc.)
  defaultVersion: latest

  # Download and process PDF
  downloadPDF: true

  # Parse citations from references section
  extractCitations: true

  # Use AI for complex reference parsing (when regex fails)
  aiCitationParsing: true
  citationParsingProvider: ${aiProvider}

  # Extract and process embedded figures
  extractFigures: true

  # Maximum PDF size (bytes). Default: 50MB
  maxPDFSize: 52428800

  # Category-to-tag mapping
  categoryMapping:
    cs.LG: machine-learning
    cs.CL: natural-language-processing
    cs.CV: computer-vision
    cs.AI: artificial-intelligence
    cs.CR: cryptography
    cs.DC: distributed-computing
    cs.SE: software-engineering
    stat.ML: statistical-machine-learning
    math.OC: optimization

  # BibTeX generation
  bibtex:
    enabled: true
    # Include abstract in BibTeX entry
    includeAbstract: false
    # Preferred citation key format: "authorYEARfirstword" or "arxivID"
    keyFormat: authorYEARfirstword

  # PDF processing (delegates to doc.pdf pipeline)
  pdf:
    # Heading detection sensitivity for section decomposition
    headingDetection: aggressive
    # Extract tables from PDF
    extractTables: true
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt capture https://arxiv.org/abs/2401.12345` returns job ID within 2 seconds
- [ ] CLI: `ctxt capture arxiv:2401.12345` shorthand syntax works
- [ ] CLI: `ctxt capture https://arxiv.org/pdf/2401.12345` PDF URL variant works
- [ ] CLI: `ctxt capture arxiv:2401.12345 --version v2` sends `options.version: v2` in the request payload to the server
- [ ] Metadata: Title, authors, abstract, categories extracted from arXiv API
- [ ] Metadata: Submission date and last-updated date captured
- [ ] Metadata: BibTeX citation generated and stored in metadata
- [ ] Metadata: DOI and journal-ref captured when available
- [ ] PDF: Paper PDF downloaded and full text extracted
- [ ] PDF: Section decomposition matches paper heading hierarchy (Introduction, Methods, Results, etc.)
- [ ] PDF: Embedded figures extracted as child KnowledgeObjects
- [ ] Citations: References section parsed into structured citation list
- [ ] Citations: arXiv IDs extracted from references where present
- [ ] Citations: Previously captured papers linked via `cites` edges
- [ ] Entity: Author entities created with name and affiliation
- [ ] Entity: Same author in multiple papers resolves to same entity
- [ ] Entity: Organization entities created for author affiliations
- [ ] Tags: arXiv categories mapped to human-readable tags
- [ ] REST API: `POST /api/v1/analyze` request payload contains `source_type: arxiv`, `source_url`, and `options` object with `version`, `extract_citations`, `extract_bibtex`, and `decompose_sections` fields
- [ ] REST API: `POST /api/v1/analyze` returns 202 with `job_id` and `object_id`
- [ ] Storage: Completed object stored with `metadata.arxiv_id`, `metadata.bibtex`, `metadata.citation_count_extracted`, `metadata.version`, `source.arxiv_id`, and `source.pdf_url` (verifiable via `GET /api/v1/objects/<id>`)
- [ ] Search: Abstract searchable via `ctxt search "scaling laws language models"`
- [ ] Search: Section-level search returns specific paper section, not entire paper
- [ ] Rate Limit: Rapid successive captures respect arXiv 3-second interval
- [ ] Error: Invalid arXiv ID returns clear "paper not found" error
- [ ] Error: Retracted paper handled gracefully with retraction notice
- [ ] Error: PDF download failure falls back to metadata-only capture with warning
- [ ] Pipeline: `academic.arxiv` completes all 9 steps in correct order

---

## Related Stories

- [US-0006](../ingestion/US-0006-document-parsing-and-decomposition.md) -- PDF processing pipeline reused for paper section decomposition
- [US-0003](../ingestion/US-0003-image-ocr-and-analysis.md) -- Figure extraction processed through image pipeline
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entity extraction from paper content
- [US-0046](../enrichment/US-0046-extract-relationships-between-entities.md) -- Citation relationships between papers
- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) -- Base URL capture; arXiv extends with academic-specific parsing
- [US-0205](./US-0205-wikipedia-capture.md) -- Reference capture for linking mentioned concepts from Wikipedia
- [US-0202](./US-0202-github-capture.md) -- Code repository capture for linking papers to implementations

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

- `test/integration/us0203_arxiv_capture_test.go::TestUS0203_TitleAbstractAuthorsStored`
- `test/integration/us0203_arxiv_capture_test.go::TestUS0203_ArxivIDStoredInMetadata`
- `test/integration/us0203_arxiv_capture_test.go::TestUS0203_CaptureIsAsync`
