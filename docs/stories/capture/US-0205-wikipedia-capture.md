# US-0205: Wikipedia and Reference Article Capture

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/knowledge-workers.md), [Agents & LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker, I want to capture Wikipedia articles (and similar reference sources) and have them automatically decomposed into structured sections with extracted entities, categories, and references so that reference knowledge becomes part of my searchable knowledge base.

---

## Context

Wikipedia is the world's largest structured knowledge base, containing over 6 million English articles with consistent formatting, hierarchical section structures, categorization systems, and comprehensive reference lists. For knowledge workers building a personal or team knowledge base, Wikipedia articles serve as authoritative reference material that contextualizes captured insights. When a researcher captures a paper about "transformer architectures," having the Wikipedia article on transformers in the knowledge base provides foundational context that connects to the paper's concepts.

The value of Wikipedia capture extends beyond the article text. Wikipedia's structure is remarkably consistent and machine-friendly: every article has a lead section, a table of contents with heading hierarchy, infoboxes with structured key-value data, categories that form a taxonomy, and a references section with structured citations. This structure maps naturally to ctxt's KnowledgeObject model. Infobox data becomes structured metadata. Categories become tags. References become structured citations (similar to arXiv citation extraction in US-0203). The heading hierarchy becomes Sections with depth information.

Unlike most capture targets, Wikipedia requires no authentication -- it is entirely public. This makes it an ideal pipeline for agent-driven knowledge augmentation: when an agent or LLM encounters an unfamiliar concept while processing other content, it can autonomously capture the Wikipedia article to build context. The system also supports multiple Wikipedia languages (`--lang fr` for French, `--lang de` for German), Wikidata (structured data), and Wikimedia Commons (image metadata), enabling multilingual and multimedia reference capture. The MediaWiki API provides structured access to article content, categories, and metadata, avoiding the need for HTML scraping and ensuring reliable, stable parsing.

---

## Acceptance Criteria

- [ ] `ctxt capture https://en.wikipedia.org/wiki/Article_Name` captures article by URL
- [ ] `ctxt capture wiki:Article_Name` works with shorthand syntax
- [ ] `ctxt capture wiki:Article_Name --lang fr` captures the French Wikipedia version
- [ ] Extracts: structured sections (matching Wikipedia heading hierarchy), infobox data, categories, references
- [ ] Infobox data stored as structured metadata (key-value pairs)
- [ ] Categories mapped to tags in the knowledge base
- [ ] References extracted as structured citations (title, URL, date, author where available)
- [ ] Creates entities for subjects mentioned in the article (people, places, organizations, concepts)
- [ ] Wikidata QID stored in metadata for cross-language article linking
- [ ] Lead section (before first heading) captured as a summary section
- [ ] No authentication needed -- uses the same pipeline architecture with unauthenticated HTTP
- [ ] Also works with: Wikidata (structured entity data), Wikimedia Commons (image metadata)
- [ ] Processing is async -- returns job ID immediately
- [ ] Disambiguation pages detected and handled (list alternatives instead of capturing)
- [ ] Redirect pages followed automatically to the canonical article

---

## Implementation Notes

### CLI Interface

```bash
# Capture by Wikipedia URL
ctxt capture https://en.wikipedia.org/wiki/Transformer_(deep_learning_architecture)
{
  "job_id": "j-wiki-1a2b3c",
  "object_id": "o-wiki-4d5e6f",
  "status": "pending_enrichment",
  "pipeline": "reference.wikipedia",
  "source": "https://en.wikipedia.org/wiki/Transformer_(deep_learning_architecture)",
  "metadata": {
    "title": "Transformer (deep learning architecture)",
    "language": "en",
    "wikidata_qid": "Q44030644",
    "categories_count": 8,
    "references_count": 47
  }
}

# Capture by shorthand
ctxt capture wiki:Transformer_(deep_learning_architecture)
{
  "job_id": "j-wiki-7g8h9i",
  "object_id": "o-wiki-0j1k2l",
  "status": "pending_enrichment",
  "pipeline": "reference.wikipedia",
  "source": "wiki:Transformer_(deep_learning_architecture)"
}

# Capture French Wikipedia article
ctxt capture wiki:Apprentissage_automatique --lang fr

# Capture Wikidata entity
ctxt capture https://www.wikidata.org/wiki/Q44030644
{
  "job_id": "j-wd-m3n4o5",
  "object_id": "o-wd-p6q7r8",
  "status": "pending_enrichment",
  "pipeline": "reference.wikidata"
}

# Check job progress
ctxt job get j-wiki-1a2b3c
{
  "job_id": "j-wiki-1a2b3c",
  "status": "processing",
  "pipeline": "reference.wikipedia",
  "progress": {
    "current_step": "ReferenceParser",
    "steps_completed": 5,
    "steps_total": 9,
    "percent": 55
  }
}

# Search captured reference articles
ctxt search "attention mechanism" --type reference
[
  {
    "object_id": "o-wiki-4d5e6f",
    "title": "Transformer (deep learning architecture)",
    "source": "wikipedia:en",
    "relevance": 0.93
  }
]
```

### REST API

```
POST /api/v1/analyze
Content-Type: application/json

{
  "source_type": "wikipedia",
  "source_url": "https://en.wikipedia.org/wiki/Transformer_(deep_learning_architecture)",
  "options": {
    "language": "en",
    "extract_infobox": true,
    "extract_references": true,
    "extract_categories": true,
    "follow_redirects": true
  }
}

-> 202 Accepted
{
  "job_id": "j-wiki-1a2b3c",
  "object_id": "o-wiki-4d5e6f",
  "status": "pending_enrichment",
  "pipeline": "reference.wikipedia"
}
```

Retrieving the captured article:

```
GET /api/v1/objects/o-wiki-4d5e6f

-> 200 OK
{
  "id": "o-wiki-4d5e6f",
  "type": "reference",
  "subtype": "wikipedia",
  "raw_content": "<full article text>",
  "source": {
    "type": "wikipedia",
    "url": "https://en.wikipedia.org/wiki/Transformer_(deep_learning_architecture)",
    "language": "en",
    "page_id": 58195429,
    "revision_id": 1234567890,
    "captured_at": "2026-02-18T10:30:45Z"
  },
  "metadata": {
    "title": "Transformer (deep learning architecture)",
    "language": "en",
    "wikidata_qid": "Q44030644",
    "last_edited": "2026-02-15T09:42:00Z",
    "page_id": 58195429,
    "revision_id": 1234567890,
    "infobox": {
      "type": "Machine learning model",
      "introduced_by": "Vaswani et al.",
      "year": "2017",
      "based_on": "Self-attention mechanism",
      "used_for": "Natural language processing, Computer vision",
      "variants": "BERT, GPT, T5, ViT"
    },
    "categories_count": 8,
    "references_count": 47,
    "sections_count": 12,
    "word_count": 8420,
    "available_languages": ["en", "fr", "de", "es", "zh", "ja", "ko", "ru"]
  },
  "sections": [
    {
      "id": "sec-lead",
      "title": "Lead",
      "depth": 0,
      "content": "A transformer is a deep learning architecture based on the multi-head attention mechanism, proposed in the 2017 paper \"Attention Is All You Need\"...",
      "metadata": {"section_type": "lead"}
    },
    {
      "id": "sec-001",
      "title": "Background",
      "depth": 1,
      "content": "Before transformers, most state-of-the-art NLP systems relied on recurrent neural networks (RNNs) such as LSTMs and GRUs..."
    },
    {
      "id": "sec-002",
      "title": "Architecture",
      "depth": 1,
      "content": "The transformer architecture consists of an encoder and a decoder, each composed of layers that include..."
    },
    {
      "id": "sec-002-1",
      "title": "Self-attention mechanism",
      "depth": 2,
      "parent_section": "sec-002",
      "content": "The key innovation of the transformer is the self-attention mechanism, which computes attention weights..."
    },
    {
      "id": "sec-002-2",
      "title": "Multi-head attention",
      "depth": 2,
      "parent_section": "sec-002",
      "content": "Rather than performing a single attention function, multi-head attention runs multiple attention operations in parallel..."
    },
    {
      "id": "sec-003",
      "title": "Training",
      "depth": 1,
      "content": "Transformers are typically trained using teacher forcing..."
    },
    {
      "id": "sec-004",
      "title": "Applications",
      "depth": 1,
      "content": "Transformers have been applied to various tasks..."
    },
    {
      "id": "sec-004-1",
      "title": "Natural language processing",
      "depth": 2,
      "parent_section": "sec-004",
      "content": "The original transformer was designed for machine translation..."
    },
    {
      "id": "sec-004-2",
      "title": "Computer vision",
      "depth": 2,
      "parent_section": "sec-004",
      "content": "Vision Transformers (ViT) apply the transformer architecture to image recognition..."
    },
    {
      "id": "sec-005",
      "title": "History",
      "depth": 1,
      "content": "The transformer was introduced in 2017 by researchers at Google Brain..."
    },
    {
      "id": "sec-006",
      "title": "See also",
      "depth": 1,
      "content": "Attention (machine learning), BERT, GPT, Large language model",
      "metadata": {"section_type": "see_also", "linked_articles": ["Attention_(machine_learning)", "BERT_(language_model)", "GPT", "Large_language_model"]}
    },
    {
      "id": "sec-refs",
      "title": "References",
      "depth": 1,
      "content": "[parsed references]",
      "metadata": {"section_type": "references"}
    }
  ],
  "citations": [
    {
      "ref_number": 1,
      "title": "Attention Is All You Need",
      "authors": ["Ashish Vaswani", "Noam Shazeer", "Niki Parmar"],
      "year": 2017,
      "venue": "NeurIPS",
      "url": "https://arxiv.org/abs/1706.03762",
      "arxiv_id": "1706.03762",
      "linked_object": "o-arxiv-attn-x1y2"
    },
    {
      "ref_number": 2,
      "title": "BERT: Pre-training of Deep Bidirectional Transformers for Language Understanding",
      "authors": ["Jacob Devlin", "Ming-Wei Chang"],
      "year": 2019,
      "url": "https://arxiv.org/abs/1810.04805",
      "linked_object": null
    }
  ],
  "tags": ["transformers", "deep-learning", "attention-mechanism", "natural-language-processing", "machine-learning"],
  "mentions": [
    {"entity": "@concept.transformer"},
    {"entity": "@concept.self-attention"},
    {"entity": "@person.ashish-vaswani"},
    {"entity": "@org.google-brain"},
    {"entity": "@concept.bert"},
    {"entity": "@concept.gpt"}
  ]
}
```

### Pipeline Steps

**`reference.wikipedia`** (Wikipedia article capture):

```
HTTPFetcher -> WikipediaParser -> InfoboxExtractor -> SectionDecomposer -> ReferenceParser -> EntityResolver -> Sectioner -> Tagger -> EmbeddingGenerator
```

**`reference.wikidata`** (Wikidata entity capture):

```
HTTPFetcher -> WikidataParser -> PropertyExtractor -> EntityResolver -> Tagger -> EmbeddingGenerator
```

Step responsibilities:

| Step | Input | Output |
|------|-------|--------|
| HTTPFetcher | Wikipedia URL or MediaWiki API URL | Raw API response (JSON from MediaWiki API, not HTML scraping) |
| WikipediaParser | MediaWiki API response | Parsed article: wikitext converted to structured content, page metadata |
| InfoboxExtractor | Parsed article wikitext | Structured infobox data as key-value pairs in metadata |
| SectionDecomposer | Parsed article with headings | Hierarchical section tree matching Wikipedia heading levels (==, ===, ====) |
| ReferenceParser | Article wikitext references | Structured citations parsed from `<ref>` tags and `{{cite}}` templates |
| WikidataParser | Wikidata API response | Structured entity data: labels, descriptions, aliases, statements |
| PropertyExtractor | Wikidata statements | Key properties extracted as structured metadata |
| EntityResolver | Article subjects + linked articles | Creates entities for people, places, orgs, concepts mentioned in article |
| Sectioner | Decomposed sections | Final Section ordering with lead section first, depth information |
| Tagger | Sections + categories | Tags from vocabulary + Wikipedia categories mapped to tags |
| EmbeddingGenerator | Sections + lead section | Embeddings per section + full-article embedding + lead embedding |

### MediaWiki API Integration

```go
type WikipediaFetcher struct {
    httpClient *http.Client
    userAgent  string  // Wikipedia requests a descriptive User-Agent
}

func (f *WikipediaFetcher) Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error) {
    title, lang := f.parseInput(draft.Source.URL)

    // Use MediaWiki API for structured access (not HTML scraping)
    apiURL := fmt.Sprintf(
        "https://%s.wikipedia.org/w/api.php?action=parse&page=%s&prop=wikitext|categories|langlinks|externallinks|sections|displaytitle|properties&format=json",
        lang,
        url.QueryEscape(title),
    )

    req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
    req.Header.Set("User-Agent", f.userAgent)

    resp, err := f.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("wikipedia_fetcher: %w", err)
    }
    defer resp.Body.Close()

    var result MediaWikiResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("wikipedia_fetcher: parse error: %w", err)
    }

    // Handle special pages
    if result.Parse == nil {
        if result.Error != nil && result.Error.Code == "missingtitle" {
            return nil, fmt.Errorf("wikipedia_fetcher: article not found: %s", title)
        }
        return nil, fmt.Errorf("wikipedia_fetcher: unexpected API response")
    }

    // Check for disambiguation
    for _, cat := range result.Parse.Categories {
        if cat.Category == "All_article_disambiguation_pages" || cat.Category == "Disambiguation_pages" {
            return nil, &DisambiguationError{
                Title:   title,
                Options: extractDisambiguationOptions(result.Parse.Wikitext),
            }
        }
    }

    draft.Metadata["title"] = result.Parse.DisplayTitle
    draft.Metadata["page_id"] = result.Parse.PageID
    draft.Metadata["revision_id"] = result.Parse.RevID
    draft.Metadata["language"] = lang
    draft.Metadata["categories"] = extractCategoryNames(result.Parse.Categories)
    draft.Metadata["wikitext"] = result.Parse.Wikitext.Content
    draft.Metadata["available_languages"] = extractLanguageLinks(result.Parse.LangLinks)

    return draft, nil
}

// parseInput handles multiple input formats:
//   "https://en.wikipedia.org/wiki/Article_Name" -> ("Article_Name", "en")
//   "wiki:Article_Name"                          -> ("Article_Name", "en")
//   "wiki:Article_Name --lang fr"                -> ("Article_Name", "fr")
func (f *WikipediaFetcher) parseInput(input string) (string, string) {
    // URL format
    if strings.Contains(input, "wikipedia.org/wiki/") {
        u, _ := url.Parse(input)
        lang := strings.Split(u.Hostname(), ".")[0]
        title := strings.TrimPrefix(u.Path, "/wiki/")
        return title, lang
    }

    // Shorthand format
    if strings.HasPrefix(input, "wiki:") {
        return strings.TrimPrefix(input, "wiki:"), "en"
    }

    return input, "en"
}
```

### Infobox Extraction

```go
type InfoboxExtractor struct{}

func (e *InfoboxExtractor) Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error) {
    wikitext, ok := draft.Metadata["wikitext"].(string)
    if !ok {
        return draft, nil
    }

    // Parse {{Infobox ...}} template from wikitext
    infobox := parseInfoboxTemplate(wikitext)
    if infobox == nil {
        return draft, nil  // No infobox present
    }

    draft.Metadata["infobox"] = infobox.Fields
    draft.Metadata["infobox_type"] = infobox.Type

    return draft, nil
}

type Infobox struct {
    Type   string            // e.g., "person", "company", "software"
    Fields map[string]string // Key-value pairs
}

// parseInfoboxTemplate extracts structured data from wikitext infobox
func parseInfoboxTemplate(wikitext string) *Infobox {
    // Find {{Infobox ... }} block
    start := strings.Index(wikitext, "{{Infobox")
    if start == -1 {
        // Try {{infobox (lowercase)
        start = strings.Index(wikitext, "{{infobox")
    }
    if start == -1 {
        return nil
    }

    // Extract template content (handling nested templates)
    content := extractBalancedBraces(wikitext[start:])

    // Parse pipe-separated key=value fields
    fields := make(map[string]string)
    infoboxType := ""

    lines := strings.Split(content, "\n")
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, "| ") {
            parts := strings.SplitN(strings.TrimPrefix(line, "| "), "=", 2)
            if len(parts) == 2 {
                key := strings.TrimSpace(parts[0])
                value := cleanWikitext(strings.TrimSpace(parts[1]))
                if value != "" {
                    fields[key] = value
                }
            }
        } else if strings.HasPrefix(line, "{{Infobox ") || strings.HasPrefix(line, "{{infobox ") {
            infoboxType = strings.TrimPrefix(strings.TrimPrefix(line, "{{Infobox "), "{{infobox ")
            infoboxType = strings.TrimRight(infoboxType, " |")
        }
    }

    return &Infobox{Type: infoboxType, Fields: fields}
}
```

### Wikidata Integration

```go
type WikidataFetcher struct {
    httpClient *http.Client
}

func (f *WikidataFetcher) FetchByQID(ctx context.Context, qid string) (*WikidataEntity, error) {
    apiURL := fmt.Sprintf(
        "https://www.wikidata.org/w/api.php?action=wbgetentities&ids=%s&format=json&languages=en",
        qid,
    )

    resp, err := f.httpClient.Get(apiURL)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result WikidataResponse
    json.NewDecoder(resp.Body).Decode(&result)

    entity := result.Entities[qid]
    return &WikidataEntity{
        QID:         qid,
        Label:       entity.Labels["en"].Value,
        Description: entity.Descriptions["en"].Value,
        Aliases:     extractAliases(entity.Aliases["en"]),
        Properties:  extractKeyProperties(entity.Claims),
    }, nil
}
```

### Backend Processing

1. User provides Wikipedia URL, shorthand (`wiki:Article_Name`), or Wikidata URL via CLI or REST API
2. HTTPFetcher determines the target type (Wikipedia article, Wikidata entity, Commons file) and language
3. For Wikipedia articles: queries the MediaWiki Parse API to get structured wikitext, categories, language links, and page metadata
4. WikipediaParser converts wikitext to clean text content, resolving templates and stripping wiki markup
5. Handles special pages: disambiguation pages return an error with alternatives; redirects are followed automatically
6. InfoboxExtractor parses the `{{Infobox}}` template (if present) into structured key-value metadata
7. SectionDecomposer splits the article by heading hierarchy (`==`, `===`, `====`) into a section tree
8. Lead section (text before the first heading) captured as a dedicated summary section
9. ReferenceParser extracts citations from `<ref>` tags and `{{cite web}}`, `{{cite journal}}`, `{{cite book}}` templates, parsing into structured citation objects with title, authors, URL, date
10. For cited sources that are arXiv papers or other known content types, links to existing KnowledgeObjects via `cites` edges
11. EntityResolver creates entities for major subjects:
    - People mentioned (linked to `@person.name` entities)
    - Organizations (`@org.name`)
    - Concepts (`@concept.name`)
    - Wikidata QID stored for cross-language linking
12. Wikipedia categories mapped to tags (e.g., "Machine learning" category -> `machine-learning` tag)
13. "See also" section parsed for internal Wikipedia links, stored as suggested related articles
14. Tagger assigns additional tags based on content analysis
15. EmbeddingGenerator creates per-section embeddings, full-article embedding, and a dedicated lead-section embedding
16. For Wikidata entities: PropertyExtractor extracts key statements (instance of, part of, described by source, etc.) as structured metadata
17. Job status updated to `completed`

### Configuration

```yaml
# In configuration.yaml
wikipedia:
  # Default language for shorthand captures
  defaultLanguage: en

  # Supported Wikipedia instances
  supportedLanguages:
    - en
    - fr
    - de
    - es
    - zh
    - ja
    - ko
    - ru
    - pt
    - it

  # MediaWiki API User-Agent (Wikipedia requests a descriptive one)
  userAgent: "ctxt-knowledge-capture/1.0 (https://ctxt.dev; contact@ctxt.dev)"

  # Rate limiting (Wikipedia is generous but requests courtesy)
  rateLimit:
    requestsPerSecond: 5
    maxConcurrent: 2

  # Content extraction settings
  extraction:
    # Extract and store infobox data
    extractInfobox: true
    # Extract references as structured citations
    extractReferences: true
    # Extract "See also" links
    extractSeeAlso: true
    # Extract categories
    extractCategories: true
    # Maximum section depth to decompose (1-6, maps to == through ======)
    maxSectionDepth: 4

  # Category-to-tag mapping (additional to auto-generated)
  categoryMapping:
    "Machine learning": machine-learning
    "Natural language processing": nlp
    "Artificial intelligence": artificial-intelligence
    "Computer science": computer-science
    "Software engineering": software-engineering
    "Distributed computing": distributed-computing
    "Cryptography": cryptography

  # Disambiguation handling
  disambiguation:
    # "error" (return error with options) or "first" (capture first option)
    strategy: error

  # Follow redirects automatically
  followRedirects: true

  # Wikidata integration
  wikidata:
    enabled: true
    # Fetch Wikidata QID for captured articles
    fetchQID: true
    # Capture Wikidata entity properties
    captureProperties: true

  # Wikimedia Commons integration
  commons:
    enabled: true
    # Download and process lead image
    captureLeadImage: true
    # Maximum image size (bytes)
    maxImageSize: 10485760  # 10MB
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt capture https://en.wikipedia.org/wiki/Article_Name` returns job ID within 2 seconds
- [ ] CLI: `ctxt capture wiki:Article_Name` shorthand syntax works
- [ ] CLI: `ctxt capture wiki:Article_Name --lang fr` captures French article
- [ ] CLI: `ctxt capture https://www.wikidata.org/wiki/Q44030644` captures Wikidata entity
- [ ] Content: Lead section captured as summary section before first heading
- [ ] Content: Section hierarchy matches Wikipedia heading structure (h2, h3, h4)
- [ ] Content: Article text cleaned of wikitext markup (no `[[links]]`, `{{templates}}` in output)
- [ ] Infobox: Infobox data extracted as structured key-value metadata
- [ ] Infobox: Articles without infoboxes processed without error
- [ ] Categories: Wikipedia categories mapped to tags
- [ ] References: Citations extracted from `<ref>` tags as structured objects
- [ ] References: arXiv references detected and linked to existing paper objects
- [ ] References: Citation count matches actual number of references in article
- [ ] Entity: Subject entities created for people, organizations, and concepts
- [ ] Entity: Wikidata QID stored in metadata for cross-language linking
- [ ] Language: English article captured by default
- [ ] Language: Non-English articles (French, German) captured with `--lang` flag
- [ ] Language: Available language links stored in metadata
- [ ] Redirect: Redirect pages followed automatically to canonical article
- [ ] Disambiguation: Disambiguation pages detected and return error with alternative list
- [ ] Wikidata: Entity captured with labels, descriptions, and key properties
- [ ] Search: Article content searchable via `ctxt search "self-attention mechanism"`
- [ ] Search: Section-level search returns specific section, not entire article
- [ ] Search: Infobox values searchable (e.g., `ctxt search "introduced 2017"`)
- [ ] REST API: POST /api/v1/analyze with Wikipedia source returns 202
- [ ] Error: Non-existent article returns clear "article not found" error
- [ ] Error: Invalid Wikipedia URL returns descriptive error
- [ ] Rate Limit: Rapid captures respect rate limits without errors
- [ ] Pipeline: `reference.wikipedia` completes all 9 steps in correct order
- [ ] Pipeline: `reference.wikidata` completes all 6 steps in correct order
- [ ] No Auth: All captures work without any authentication (Wikipedia is public)

---

## Related Stories

- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) -- Base URL capture; Wikipedia extends with structured wiki parsing
- [US-0203](./US-0203-arxiv-capture.md) -- arXiv capture; Wikipedia references link to captured papers
- [US-0006](../ingestion/US-0006-document-parsing-and-decomposition.md) -- Document decomposition pattern shared with section decomposition
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entity extraction from article content
- [US-0046](../enrichment/US-0046-extract-relationships-between-entities.md) -- Relationship extraction between entities mentioned in articles
- [US-0049](../enrichment/US-0049-classify-content-with-taxonomy.md) -- Wikipedia categories contribute to taxonomy classification
- [US-0200](./US-0200-browser-cookie-bridge.md) -- Uses same pipeline architecture despite not needing authentication
