# Plan: Inline [ref:ID] Citations in Composition

**Date:** 2026-02-18
**Status:** Draft
**Related:** US-0022 (Generate Brief from Objects), mentions.md

---

## Overview

Implement inline `[ref:ID]` citation syntax in compositions (briefs, plans, summaries, drafts) that traces generated content back to source knowledge objects. Inspired by memU's cross-reference system and academic citation patterns.

---

## Reference: memU Approach

memU's relevant patterns:
1. **Resources → Items**: Each memory item traces to original resource
2. **Cross-references**: Symlink-style links between related memories
3. **Traceability**: `retrieve()` returns resources for provenance

```
{
    "items": [...],         # Extracted facts
    "resources": [...],     # Original sources for traceability
}
```

---

## Proposed Design

### Citation Syntax

```
[ref:ID]           # Single reference
[ref:ID1,ID2]      # Multiple references
[ref:ID#section]   # Reference with anchor
```

### Example Output

```markdown
# Architecture Migration Brief

## Executive Summary

The team decided to migrate from monolith to microservices, prioritizing
customer-facing features over infrastructure optimization in Q1 [ref:o-abc123].
The decision was driven by market timing pressures [ref:o-def456,o-ghi789].

## Key Decisions

### Defer Infrastructure Refactor [ref:o-abc123]
- **Impact:** HIGH
- **Status:** OPEN
- **Stakeholders:** Alice (Tech Lead), Bob (PM)

The rationale was that infrastructure work should not block feature delivery
during the critical Q1 window [ref:o-abc123#rationale].

---

## References

| ID | Type | Summary | Source | Created |
|----|------|---------|--------|---------|
| o-abc123 | decision | Defer infrastructure refactor | engineering-meeting.pdf | 2025-01-15 |
| o-def456 | note | Market timing analysis | slack-#engineering | 2025-01-16 |
| o-ghi789 | email | Competitor launch timeline | email-from-bob | 2025-01-17 |
```

---

## Implementation Phases

### Phase 1: Citation Injection (Composition Layer)

**Goal:** Modify composition generation to inject `[ref:ID]` markers

**Changes:**
1. Update `Compose()` in `internal/service/service.go`
2. Pass source object context to AI prompts
3. Prompt template instructs AI to cite sources inline

```go
type CompositionContext struct {
    Objects   []*KnowledgeObject
    ObjectMap map[string]*KnowledgeObject  // ID -> Object lookup
}

func (s *Service) Compose(ctx context.Context, objects []*KnowledgeObject, compositionType string) (*Composition, error) {
    // Build context with object IDs for citation
    cctx := &CompositionContext{
        Objects:   objects,
        ObjectMap: make(map[string]*KnowledgeObject),
    }
    for _, obj := range objects {
        cctx.ObjectMap[obj.ID] = obj
    }
    
    // Generate with citation-aware prompt
    prompt := s.buildCitationPrompt(cctx, compositionType)
    content := s.aiProvider.Generate(ctx, prompt)
    
    // Parse citations from content
    citations := s.extractCitations(content)
    
    // Build reference table
    refTable := s.buildReferenceTable(citations, cctx)
    
    return &Composition{
        Content:     content,
        Citations:   citations,
        RefTable:    refTable,
        GeneratedAt: time.Now(),
    }, nil
}
```

**Prompt Template:**
```
You are generating a {composition_type} from the following knowledge objects.

IMPORTANT: When you use information from a specific object, cite it inline using [ref:ID] syntax.
Example: "The team decided to proceed [ref:o-abc123]."

Knowledge Objects:
{#for obj in objects}
---
ID: {obj.id}
Type: {obj.type}
Source: {obj.source}
Content: {obj.summary_or_raw}
---
{/for}

Generate a {composition_type} that:
1. Synthesizes information from these objects
2. Cites each claim with [ref:ID] 
3. Groups related information logically
```

### Phase 2: Citation Parser

**Goal:** Parse and validate citations in composition output

```go
type Citation struct {
    IDs      []string  // Referenced object IDs
    Position int       // Character position in content
    Anchor   string    // Optional #anchor suffix
}

func ParseCitations(content string) []Citation {
    re := regexp.MustCompile(`\[ref:([a-zA-Z0-9,\-#]+)\]`)
    matches := re.FindAllStringSubmatchIndex(content, -1)
    
    var citations []Citation
    for _, m := range matches {
        refStr := content[m[2]:m[3]]
        c := Citation{Position: m[0]}
        
        // Parse IDs and optional anchor
        parts := strings.Split(refStr, "#")
        c.IDs = strings.Split(parts[0], ",")
        if len(parts) > 1 {
            c.Anchor = parts[1]
        }
        citations = append(citations, c)
    }
    return citations
}

func ValidateCitations(citations []Citation, objectMap map[string]*KnowledgeObject) []error {
    var errs []error
    seen := make(map[string]bool)
    
    for _, c := range citations {
        for _, id := range c.IDs {
            if seen[id] {
                continue
            }
            seen[id] = true
            
            if _, exists := objectMap[id]; !exists {
                errs = append(errs, fmt.Errorf("invalid citation: object %s not found", id))
            }
        }
    }
    return errs
}
```

### Phase 3: Reference Table Generator

**Goal:** Auto-generate formatted reference tables

```go
func (s *Service) buildReferenceTable(citations []Citation, ctx *CompositionContext) string {
    // Collect unique IDs
    seen := make(map[string]bool)
    var ids []string
    for _, c := range citations {
        for _, id := range c.IDs {
            if !seen[id] {
                seen[id] = true
                ids = append(ids, id)
            }
        }
    }
    
    var b strings.Builder
    b.WriteString("\n---\n\n## References\n\n")
    b.WriteString("| ID | Type | Summary | Source | Created |\n")
    b.WriteString("|----|------|---------|--------|--------|\n")
    
    for _, id := range ids {
        obj := ctx.ObjectMap[id]
        if obj == nil {
            continue
        }
        summary := obj.Summaries[0]
        if len(summary) > 50 {
            summary = summary[:50] + "..."
        }
        fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
            id, obj.Type, summary, obj.Source, obj.CreatedAt.Format("2006-01-02"))
    }
    
    return b.String()
}
```

### Phase 4: Entity/Mention Integration

**Goal:** Link citations to existing mentions system

```go
type Citation struct {
    IDs       []string
    Position  int
    Anchor    string
    Entities  []string  // Extracted @mentions from cited object
}

// When building reference table, include entity links
func (s *Service) enrichCitationsWithEntities(citations []Citation, ctx *CompositionContext) {
    for i := range citations {
        for _, id := range citations[i].IDs {
            obj := ctx.ObjectMap[id]
            if obj != nil {
                citations[i].Entities = append(citations[i].Entities, obj.Mentions...)
            }
        }
    }
}
```

### Phase 5: CLI & API Integration

**CLI:**
```bash
# Generate brief with citations (default)
ctxt make brief --from o-abc123,o-def456

# Export formats
ctxt make brief --from o-abc123 --export json  # Includes citations array
ctxt make brief --from o-abc123 --export markdown  # With reference table
ctxt make brief --from o-abc123 --no-citations  # Disable citations

# Validate citations
ctxt validate citations brief.md
```

**API:**
```json
POST /compositions/brief
{
    "object_ids": ["o-abc123", "o-def456"],
    "template": "executive",
    "citations": {
        "enabled": true,
        "format": "inline"  // inline | footnote | endnote
    }
}

→ 200 OK
{
    "brief_id": "b-xyz789",
    "content": "# Brief...\n\nContent [ref:o-abc123]...",
    "citations": [
        {"ids": ["o-abc123"], "position": 45, "entities": ["@team.alice"]}
    ],
    "references": [
        {"id": "o-abc123", "type": "decision", "summary": "...", "source": "..."}
    ]
}
```

---

## Data Model Changes

```go
// internal/storage/composition.go
type Composition struct {
    ID           string
    Type         string
    Content      string
    Citations    []CitationRef
    SourceIDs    []string
    GeneratedAt  time.Time
    Template     string
}

type CitationRef struct {
    ID         string
    Position   int
    ObjectID   string
    Object     *KnowledgeObject `json:"-"`  // Loaded on demand
}
```

**Database migration:**
```sql
CREATE TABLE compositions (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    content TEXT NOT NULL,
    source_ids TEXT NOT NULL,  -- JSON array
    generated_at TIMESTAMP NOT NULL,
    template TEXT
);

CREATE TABLE citation_refs (
    id TEXT PRIMARY KEY,
    composition_id TEXT REFERENCES compositions(id),
    object_id TEXT REFERENCES knowledge_objects(id),
    position INTEGER NOT NULL
);
```

---

## Edge Cases

| Case | Handling |
|------|----------|
| Citation to non-existent object | Validation error, exclude from reference table |
| Multiple citations to same object | Dedupe in reference table, show count |
| Object deleted after composition | Reference table shows "[deleted]" |
| Circular references | Not applicable (composition → objects is one-way) |
| Large number of citations | Group by object type in reference table |

---

## Testing

### Unit Tests
- `TestParseCitations`: Various citation formats
- `TestValidateCitations`: Missing objects, valid references
- `TestBuildReferenceTable`: Formatting, truncation
- `TestEnrichCitationsWithEntities`: Mention extraction

### Integration Tests
- `TestComposeWithCitations`: End-to-end composition with citations
- `TestCitationValidation`: Invalid IDs caught
- `TestExportFormats`: JSON vs markdown output

### E2E Tests
```bash
# Generate brief and verify citations
ctxt make brief --from o-abc123 > brief.md
grep -q '\[ref:o-abc123\]' brief.md
grep -q '## References' brief.md

# Validate JSON output
ctxt make brief --from o-abc123 --export json | jq '.citations | length'
```

---

## Open Questions

1. **Citation density threshold?** Warn if >50% of sentences have citations?
2. **Citation grouping?** Allow `[ref:o-abc123]{.important}` for styling?
3. **Bidirectional linking?** Add "cited by" to knowledge objects?
4. **Citation hover preview?** For web UI, show object summary on hover?

---

## Timeline

| Phase | Effort | Priority |
|-------|--------|----------|
| Phase 1: Citation Injection | 2 days | P0 |
| Phase 2: Citation Parser | 1 day | P0 |
| Phase 3: Reference Table | 1 day | P0 |
| Phase 4: Entity Integration | 1 day | P1 |
| Phase 5: CLI/API | 1 day | P0 |

**Total:** ~6 days

---

## Success Criteria

- [ ] All compositions include `[ref:ID]` citations
- [ ] Reference table auto-generated and accurate
- [ ] Citations validate against existing objects
- [ ] JSON export includes structured citation data
- [ ] Markdown export includes formatted reference table
- [ ] CLI flags for controlling citation behavior
