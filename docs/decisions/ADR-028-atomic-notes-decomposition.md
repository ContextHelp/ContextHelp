# ADR-028 – Atomic Notes and Decomposition Strategy

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

The **Atomic Principle** states: "Notes are nodes, not essays. Small enough to link, reuse, remix, and reference. Built for composition."

Traditional knowledge systems treat notes as monolithic documents—entire articles, meeting transcriptions, or long-form essays stored as single units. This creates fundamental problems:

**Discovery Problems:**
- Large documents match queries broadly but not precisely
- Relevant paragraph buried in 5-page document
- Search returns entire document when only one section matters
- Users must manually scan to find specific insight

**Linking Problems:**
- Can't link to specific idea within document
- Backlinks show entire document, not relevant section
- Related concepts in different documents remain disconnected
- Graph relationships too coarse-grained

**Reuse Problems:**
- Can't compose from specific insights across documents
- Copy-paste creates duplication and drift
- Templates can't assemble from document fragments
- Version updates don't propagate to reused content

**Composition Problems:**
- Briefs require manual excerpt extraction
- Plans can't automatically gather related insights
- Reports duplicate content instead of referencing
- No provenance to source paragraphs

**Graph Problems:**
- Entity mentions span multiple concepts in one document
- Backlinks connect documents, not ideas
- Traversal returns too much irrelevant context
- Semantic density too low for effective linking

**Performance Problems:**
- Large documents slow vector similarity search
- Embeddings average across disparate concepts
- Ranking struggles with documents covering many topics
- Storage overhead for repeated access to fragments

**Constraints:**
- Original content must be preserved (no destructive modifications)
- Decomposition must be deterministic (same input → same chunks)
- Section boundaries must respect semantic coherence
- Must work for all content types (text, transcripts, documents)
- Must integrate with existing pipelines without breaking changes
- Must support composition without data duplication
- Must enable precise linking and backlinks

**Affected Subsystems:**
- Pipeline system (decomposition step)
- Storage schema (sections table)
- Query engine (section-level search and ranking)
- Graph traversal (section-level backlinks)
- Composition engine (assembling from sections)
- Vector indexing (per-section embeddings)
- Export/import (section preservation)

**Goals:**
- Enable precise discovery at idea granularity
- Support fine-grained linking and backlinks
- Enable composition from atomic units
- Improve search relevance and ranking
- Preserve full provenance to source objects
- Maintain deterministic, reproducible decomposition

---

## Decision

**`ctxt` will implement automatic content decomposition into atomic sections during pipeline enrichment, storing sections as first-class entities linked to their parent objects, enabling precise search, linking, composition, and graph traversal at the idea level while preserving original content intact.**

The decomposition system provides:

1. **Atomicity Criteria:**

   **An atomic section is:**
   - **Semantically coherent** – expresses a single idea, concept, or decision
   - **Self-contained** – understandable with minimal surrounding context
   - **Linkable** – small enough to reference specifically
   - **Composable** – can be combined with other sections meaningfully
   - **Preserving context** – includes enough detail to stand alone

   **Not atomic:**
   - Single sentences (too granular, lack context)
   - Entire documents (too coarse, multiple concepts)
   - Random word boundaries (semantically incoherent)
   - Dependent fragments requiring parent context

   **Ideal Size:**
   - 50-500 words (paragraph to multi-paragraph)
   - 1-5 sentences minimum for coherence
   - Content-dependent (code blocks may be smaller, decisions larger)

2. **Section Schema:**

   **Storage Model:**
   ```sql
   CREATE TABLE sections (
       id UUID PRIMARY KEY,
       object_id UUID NOT NULL,
       index INT NOT NULL,  -- Sequential position in parent
       section_type TEXT NOT NULL,  -- paragraph, heading, code_block, decision, list, quote
       heading TEXT,        -- Optional: section heading/title
       content TEXT NOT NULL,
       summary TEXT,        -- Optional: AI-generated section summary
       word_count INT NOT NULL,
       char_count INT NOT NULL,
       embedding VECTOR,    -- Optional: per-section vector embedding
       created_at TIMESTAMP NOT NULL,
       FOREIGN KEY(object_id) REFERENCES objects(id),
       UNIQUE(object_id, index)
   );

   CREATE INDEX idx_sections_object ON sections(object_id, index);
   CREATE INDEX idx_sections_type ON sections(section_type);
   CREATE INDEX idx_sections_embedding ON sections USING ivfflat(embedding vector_cosine_ops);

   -- Section-level mentions and entity connections
   CREATE TABLE section_mentions (
       id UUID PRIMARY KEY,
       section_id UUID NOT NULL,
       entity_slug TEXT NOT NULL,
       mention_text TEXT NOT NULL,
       position INT NOT NULL,  -- Character offset in section
       FOREIGN KEY(section_id) REFERENCES sections(id),
       FOREIGN KEY(entity_slug) REFERENCES entities(slug)
   );

   CREATE INDEX idx_section_mentions_section ON section_mentions(section_id);
   CREATE INDEX idx_section_mentions_entity ON section_mentions(entity_slug);
   ```

   **Object Schema Extension:**
   ```json
   {
     "id": "uuid",
     "type": "url",
     "raw_content": "...",  // Original content preserved
     "summary": "...",      // Overall summary
     "sections": [          // Array of section references
       {
         "id": "section-uuid-1",
         "type": "heading",
         "heading": "Introduction",
         "content": "...",
         "summary": "...",
         "word_count": 87,
         "mentions": ["@ux.design-system"]
       },
       {
         "id": "section-uuid-2",
         "type": "paragraph",
         "heading": null,
         "content": "...",
         "summary": "...",
         "word_count": 234,
         "mentions": ["@component.button", "@pattern.responsive"]
       }
     ]
   }
   ```

3. **Decomposition Strategies:**

   **Text Decomposition:**
   ```yaml
   strategy: semantic-boundary
   method: heading-aware

   rules:
     # Heading boundaries
     - match: markdown_heading
       create_section: before_heading
       include_heading: true

     # Paragraph boundaries
     - match: double_newline
       create_section: if_word_count > 50
       merge_short: true

     # List boundaries
     - match: markdown_list
       create_section: if_complete_list
       preserve_structure: true

     # Code block boundaries
     - match: code_fence
       create_section: always
       type: code_block

     # Quote boundaries
     - match: blockquote
       create_section: always
       type: quote

     # Minimum coherence
     - min_words: 50
     - min_sentences: 2
     - max_words: 500
   ```

   **Transcript Decomposition:**
   ```yaml
   strategy: speaker-turn-aware
   method: topic-shift-detection

   rules:
     # Speaker turn boundaries
     - match: speaker_change
       create_section: if_topic_coherent
       include_speaker: true

     # Topic shift detection
     - match: topic_shift_keywords
       keywords: ["moving on", "next topic", "another thing"]
       create_section: true

     # Time-based boundaries
     - match: silence_gap
       duration_seconds: 5
       create_section: if_coherent

     # Semantic coherence
     - use_embeddings: true
       similarity_threshold: 0.75
       create_section: if_below_threshold
   ```

   **Document Decomposition:**
   ```yaml
   strategy: structural-hierarchy
   method: heading-based

   rules:
     # Document structure
     - match: h1_heading
       create_section: always
       section_type: chapter

     - match: h2_heading
       create_section: always
       section_type: section

     - match: h3_heading
       create_section: if_word_count > 100
       section_type: subsection

     # Paragraphs within sections
     - match: paragraph
       create_section: if_word_count > 150
       merge_related: true

     # Tables and figures
     - match: table
       create_section: always
       section_type: table
       include_caption: true
   ```

4. **Pipeline Integration:**

   **Decomposition Step:**
   ```go
   type DecompositionStep struct {
       Strategy string
       Config   DecompositionConfig
   }

   func (ds *DecompositionStep) Execute(obj Object) ([]Section, error) {
       // Select decomposition strategy based on content type
       decomposer := ds.selectDecomposer(obj.Type, obj.Subtype)

       // Parse content structure
       structure := decomposer.Parse(obj.RawContent)

       // Identify section boundaries
       boundaries := decomposer.IdentifyBoundaries(structure)

       // Extract sections
       sections := []Section{}
       for i, boundary := range boundaries {
           section := Section{
               ID:        generateUUID(),
               ObjectID:  obj.ID,
               Index:     i,
               Type:      boundary.Type,
               Heading:   boundary.Heading,
               Content:   boundary.Content,
               WordCount: countWords(boundary.Content),
               CharCount: len(boundary.Content),
           }

           // Extract section-level mentions
           section.Mentions = extractMentions(section.Content)

           // Generate section summary (optional)
           if ds.Config.GenerateSummaries {
               section.Summary = ds.generateSummary(section.Content)
           }

           // Generate section embedding (optional)
           if ds.Config.GenerateEmbeddings {
               section.Embedding = ds.generateEmbedding(section.Content)
           }

           sections = append(sections, section)
       }

       // Validate coherence
       for _, section := range sections {
           if !ds.isCoherent(section) {
               return nil, fmt.Errorf("section %s lacks semantic coherence", section.ID)
           }
       }

       return sections, nil
   }
   ```

   **Pipeline Configuration:**
   ```yaml
   pipelines:
     url.article:
       steps:
         - fetch
         - extract_article
         - decompose:
             strategy: semantic-boundary
             min_words: 50
             max_words: 500
             generate_summaries: true
             generate_embeddings: true
         - extract_mentions_per_section
         - enrich_metadata
         - store

     text.long:
       steps:
         - infer_type
         - decompose:
             strategy: paragraph-boundary
             min_words: 75
             merge_short: true
             generate_summaries: false
         - extract_mentions_per_section
         - enrich_metadata
         - store

     audio.transcript:
       steps:
         - transcribe
         - decompose:
             strategy: speaker-turn
             topic_shift_detection: true
             min_words: 100
         - extract_mentions_per_section
         - enrich_metadata
         - store
   ```

5. **Section-Level Search:**

   **Query Execution:**
   ```sql
   -- FTS search at section level
   SELECT
       s.id AS section_id,
       s.object_id,
       s.index,
       s.heading,
       s.content,
       o.title,
       o.type,
       bm25(sections_fts, 1.0, 0.5) AS relevance
   FROM sections_fts
   JOIN sections s ON sections_fts.rowid = s.rowid
   JOIN objects o ON s.object_id = o.id
   WHERE sections_fts MATCH 'design system'
   ORDER BY relevance DESC
   LIMIT 20;

   -- Vector similarity at section level
   SELECT
       s.id AS section_id,
       s.object_id,
       s.heading,
       s.content,
       1 - (s.embedding <=> query_embedding) AS similarity
   FROM sections s
   WHERE s.embedding IS NOT NULL
   ORDER BY s.embedding <=> query_embedding
   LIMIT 20;

   -- Hybrid search (FTS + Vector at section level)
   WITH fts_results AS (
       SELECT section_id, relevance
       FROM section_fts_search('design patterns')
   ),
   vector_results AS (
       SELECT section_id, similarity
       FROM section_vector_search([...embedding...])
   )
   SELECT
       COALESCE(f.section_id, v.section_id) AS section_id,
       reciprocal_rank_fusion(f.relevance, v.similarity) AS score
   FROM fts_results f
   FULL OUTER JOIN vector_results v ON f.section_id = v.section_id
   ORDER BY score DESC;
   ```

   **Result Presentation:**
   ```bash
   ctxt find "design system principles"

   # Output:
   Found 12 sections across 5 objects:

   1. Introduction to Design Systems [0.89]
      Object: Design System Best Practices
      Section: Principles (paragraph 2)
      Content: A design system is built on core principles that ensure
      consistency across products. These principles include modularity,
      reusability, and scalability...
      Link: object:abc123/section:2

   2. Component Library Structure [0.85]
      Object: Building a Component Library
      Section: Architecture (paragraph 4)
      Content: The component library follows atomic design principles,
      starting with atoms (buttons, inputs) and building up to molecules
      (forms, cards) and organisms (headers, footers)...
      Link: object:def456/section:4

   # User can jump to specific section
   ctxt open object:abc123/section:2
   ```

6. **Section-Level Linking:**

   **Backlinks at Section Granularity:**
   ```sql
   -- Find sections that mention a specific entity
   SELECT
       s.id,
       s.object_id,
       s.heading,
       s.content,
       sm.mention_text
   FROM sections s
   JOIN section_mentions sm ON s.id = sm.section_id
   WHERE sm.entity_slug = 'ux.design-system'
   ORDER BY s.created_at DESC;
   ```

   **Graph Traversal:**
   ```go
   // Get all sections related to entity and their neighbors
   func GetRelatedSections(entitySlug string, depth int) []Section {
       // Start with sections mentioning entity
       sections := querySectionsByEntity(entitySlug)

       // Traverse to connected entities
       for i := 0; i < depth; i++ {
           connectedEntities := getConnectedEntities(sections)
           for _, entity := range connectedEntities {
               moreSections := querySectionsByEntity(entity.Slug)
               sections = append(sections, moreSections...)
           }
       }

       return deduplicate(sections)
   }
   ```

   **Reference Links:**
   ```markdown
   In my note: "The button component (@component.button) follows
   the design system principles (see: object:abc123/section:2)
   established in our guidelines."

   # Rendered as:
   The button component (→ component.button) follows the design
   system principles (→ Design System Best Practices § Principles)
   established in our guidelines.
   ```

7. **Composition from Sections:**

   **Template Assembly:**
   ```yaml
   # Brief template
   template: project-brief
   profile: founder

   sections:
     - name: overview
       gather:
         sections_matching: "project:scope AND entity:@project.launch"
         max_sections: 3
         sort: relevance

     - name: decisions
       gather:
         sections_matching: "type:decision AND profile:founder"
         max_sections: 5
         sort: created_at

     - name: next_steps
       gather:
         sections_matching: "tags:actionable AND NOT status:done"
         max_sections: 10
         sort: priority
   ```

   **Composition Output:**
   ```markdown
   # Project Launch Brief

   ## Overview

   [Section from object:abc123/section:2]
   The launch strategy focuses on three core user segments...

   [Section from object:def456/section:5]
   Market research indicates strong demand in the enterprise segment...

   ## Key Decisions

   [Section from object:ghi789/section:3]
   Decision: Launch with MVP feature set rather than full product.
   Rationale: Faster time to market enables early feedback...

   ## Next Steps

   - [Section from object:jkl012/section:7] Complete beta testing with 50 users
   - [Section from object:mno345/section:2] Finalize pricing strategy
   - [Section from object:pqr678/section:4] Prepare launch materials

   ---
   Composed from 11 sections across 7 objects
   Generated: 2026-01-26 10:00:00
   ```

8. **Section Management Commands:**

   ```bash
   # View object sections
   ctxt sections <object-id>

   # Output:
   Object: Design System Best Practices
   Sections: 8

   1. Introduction (paragraph, 87 words)
      Content: A design system is built on core principles...
      Mentions: @ux.design-system

   2. Principles (paragraph, 234 words)
      Content: The core principles include modularity, reusability...
      Mentions: @principle.modularity, @principle.reusability

   # Open specific section
   ctxt open <object-id>/section:<index>

   # Search at section level
   ctxt find "design patterns" --granularity section

   # Link to section
   ctxt link <section-id> to <entity-slug>

   # Export object with sections
   ctxt export <object-id> --include-sections

   # Section-level backlinks
   ctxt backlinks <section-id>
   ```

9. **Decomposition Quality Validation:**

   **Validation Criteria:**
   ```go
   type SectionValidator struct {
       MinWords      int
       MaxWords      int
       MinSentences  int
       MaxSentences  int
   }

   func (sv *SectionValidator) Validate(section Section) error {
       // Word count validation
       if section.WordCount < sv.MinWords {
           return fmt.Errorf("section too short: %d words (min %d)",
               section.WordCount, sv.MinWords)
       }

       if section.WordCount > sv.MaxWords {
           return fmt.Errorf("section too long: %d words (max %d)",
               section.WordCount, sv.MaxWords)
       }

       // Sentence count validation
       sentenceCount := countSentences(section.Content)
       if sentenceCount < sv.MinSentences {
           return fmt.Errorf("section lacks coherence: %d sentences (min %d)",
               sentenceCount, sv.MinSentences)
       }

       // Semantic coherence check
       if !sv.isSemanticallyCoheren(section) {
           return fmt.Errorf("section lacks semantic coherence")
       }

       // Self-contained check
       if !sv.isSelfContained(section) {
           return fmt.Errorf("section requires parent context to understand")
       }

       return nil
   }

   func (sv *SectionValidator) isSemanticallyCoheren(section Section) bool {
       // Use embeddings to check internal coherence
       sentences := splitIntoSentences(section.Content)
       embeddings := generateEmbeddings(sentences)

       // Check pairwise similarity between sentences
       avgSimilarity := averagePairwiseSimilarity(embeddings)
       return avgSimilarity > 0.6  // Threshold for coherence
   }
   ```

10. **Migration and Backward Compatibility:**

    **Gradual Rollout:**
    ```bash
    # Enable decomposition for new objects
    ctxt config set decomposition.enabled true

    # Backfill existing objects
    ctxt backfill decompose --since "30 days ago" --batch-size 100

    # Validate decomposition quality
    ctxt validate decomposition --object-id <id>

    # Revert if needed (sections remain, query behavior reverts)
    ctxt config set search.granularity object
    ```

    **Compatibility:**
    - Original content always preserved in `raw_content`
    - Section-level search optional (can query at object level)
    - Existing queries work unchanged (return objects, not sections)
    - Opt-in section-level features via flags

---

## Rationale

### Alternatives Considered

#### 1. **Store Only Whole Documents (Rejected)**
Keep monolithic documents without decomposition.

**Rejected because:**
- Violates Atomic principle
- Coarse-grained search results
- Can't link to specific ideas
- Composition requires manual excerpt extraction
- Graph relationships too broad

#### 2. **Manual User Decomposition (Rejected)**
Require users to manually split documents into atomic notes.

**Rejected because:**
- Excessive user burden
- Inconsistent granularity across users
- Time-consuming for long documents
- Discourages capture of long-form content
- Manual process error-prone

#### 3. **Sentence-Level Atomicity (Rejected)**
Decompose to individual sentences.

**Rejected because:**
- Too granular (lacks context)
- Sentences not self-contained
- Poor user experience (too many results)
- Storage overhead excessive
- Sentences lack semantic coherence

#### 4. **Fixed-Size Chunks (Rejected)**
Split documents into fixed word/character counts.

**Rejected because:**
- Ignores semantic boundaries
- Breaks paragraphs and thoughts mid-concept
- Poor user experience (arbitrary boundaries)
- Violates coherence principle
- Doesn't respect document structure

#### 5. **Separate Database Per Section (Rejected)**
Treat each section as independent object.

**Rejected because:**
- Loses parent document context
- No way to view original structure
- Duplication of metadata
- Complex provenance tracking
- Export/import complexity

### Benefits of Chosen Approach

**Precise Discovery:**
- Search returns exactly relevant sections
- Higher relevance scores (less noise)
- Better ranking (focused embeddings)
- Clear context boundaries

**Fine-Grained Linking:**
- Link to specific ideas, not whole documents
- Backlinks show exact relevant sections
- Graph traversal more precise
- Mention resolution scoped correctly

**Effective Composition:**
- Assemble from atomic units naturally
- Full provenance to source sections
- No content duplication
- Template-driven assembly works

**Performance:**
- Faster vector search (smaller embeddings)
- Better ranking (focused semantic content)
- Reduced storage overhead for partial access
- Parallelizable section processing

**Backward Compatible:**
- Original content preserved
- Existing queries work unchanged
- Gradual opt-in for features
- No breaking schema changes

### Drawbacks / Risks

**Decomposition Complexity:**
- Determining optimal boundaries hard
- Language/structure-dependent logic
- May split incorrectly in edge cases
- Requires careful validation

**Storage Overhead:**
- Additional sections table
- Section-level embeddings
- Section-level mention tracking
- Indices on sections

**Query Complexity:**
- Section-level vs object-level modes
- Result aggregation logic
- UI must handle sections gracefully
- Ranking across granularities

**User Mental Model:**
- Users think in documents, not sections
- Must explain section concept
- Viewing sections vs whole document
- Links to sections unfamiliar

---

## Consequences

### Positive

**Atomic Knowledge Graph:**
- Ideas are nodes, documents are compositions
- Fine-grained linking and backlinks
- Precise entity mention tracking
- Effective graph traversal

**Search Quality:**
- Results match precisely
- Higher relevance scores
- Less noise in results
- Better ranking

**Composition Power:**
- Briefs assemble from atomic insights
- Plans gather relevant sections automatically
- Reports compose without duplication
- Full provenance preserved

**Extensibility:**
- Plugins can process sections independently
- Templates reference sections directly
- Workflows operate at idea granularity
- Export/import preserves structure

### Negative

**Implementation Complexity:**
- Decomposition algorithm sophisticated
- Validation logic required
- Storage schema extension
- Query engine modifications

**Performance Overhead:**
- Additional decomposition pipeline step
- Section storage overhead
- Section-level indexing cost
- Query result aggregation

**User Experience:**
- Explaining section concept
- Viewing modes (section vs document)
- Link ambiguity (object vs section)
- Navigation complexity

### Neutral / Considerations

**Granularity Trade-offs:**
- Too fine → too many results, lacks context
- Too coarse → defeats atomicity purpose
- Content-dependent optimal size
- User preferences may vary

**Original vs Decomposed:**
- Both views valuable
- UI must support both seamlessly
- Default view preference needed
- Context switching important

**Backfill Strategy:**
- Existing objects need decomposition
- Gradual vs batch processing
- Validation of results
- Performance during backfill

**Evolution:**
- Decomposition strategies will improve
- May need re-decomposition over time
- Version tracking for decomposition
- Migration between strategies

---

## Implementation Notes

### Core Components

**Decomposer (`ctxt/decomposition/`):**
```go
type Decomposer interface {
    Name() string
    Parse(content string) Structure
    IdentifyBoundaries(structure Structure) []Boundary
    ExtractSections(boundaries []Boundary) []Section
    Validate(section Section) error
}

// Built-in decomposers
type SemanticBoundaryDecomposer struct { ... }
type HeadingAwareDecomposer struct { ... }
type SpeakerTurnDecomposer struct { ... }
type StructuralHierarchyDecomposer struct { ... }
```

**Section Store (`dPKMS/sections/`):**
```go
type SectionStore interface {
    Create(section Section) error
    Get(sectionID string) (*Section, error)
    ListByObject(objectID string) ([]Section, error)
    Search(query SectionQuery) ([]Section, error)
    Delete(sectionID string) error
}
```

**Section Query Engine:**
```go
type SectionQueryEngine struct {
    ftsIndex    FTSIndex
    vectorIndex VectorIndex
    graphIndex  GraphIndex
}

func (sqe *SectionQueryEngine) Search(query Query) ([]SectionResult, error)
func (sqe *SectionQueryEngine) Rank(results []SectionResult) []SectionResult
func (sqe *SectionQueryEngine) AggregateToObjects(sections []SectionResult) []ObjectResult
```

### Storage Schema Extensions
Already covered in Decision section above.

### Integration Points

**With Pipeline System:**
1. Decomposition step after content extraction
2. Section-level mention extraction
3. Section-level embedding generation
4. Section storage before object finalization

**With Query Engine:**
1. Section-level FTS index
2. Section-level vector index
3. Hybrid ranking at section level
4. Optional aggregation to object level

**With Composition Engine:**
1. Template references sections directly
2. Assembly from section collection
3. Provenance tracking to sections
4. Section-level formatting

**With Graph System:**
1. Section-level mention tracking
2. Section backlinks
3. Graph traversal at section granularity
4. Entity-section relationships

### Migration Strategy

**Phase 1: Schema and Storage (Skeleton 5)**
- Add sections table
- Section storage implementation
- Basic decomposition strategies
- CLI section commands

**Phase 2: Search Integration (Skeleton 6)**
- Section-level FTS indexing
- Section-level vector embeddings
- Query engine modifications
- Result presentation

**Phase 3: Composition Integration (Skeleton 8)**
- Template section references
- Composition from sections
- Provenance tracking
- Export with sections

**Phase 4: Advanced Features (Skeleton 9+)**
- Improved decomposition algorithms
- Section-level collaboration
- Section version history
- Re-decomposition workflows

**Backward Compatibility:**
- Sections stored alongside objects
- Original content always in `raw_content`
- Existing queries work unchanged
- Opt-in section-level features

### Testing Requirements

**Unit Tests:**
- Decomposition boundary detection
- Semantic coherence validation
- Self-contained check
- Section extraction accuracy

**Integration Tests:**
- End-to-end pipeline with decomposition
- Section-level search accuracy
- Composition from sections
- Graph traversal at section level

**Quality Tests:**
- Decomposition consistency (same input → same sections)
- Semantic coherence across content types
- Boundary detection accuracy
- User experience validation

**Performance Tests:**
- Decomposition overhead measurement
- Section storage performance
- Section-level search latency
- Composition assembly speed

---

## References

- **architecture.md:140-141** – Atomic principle
- **design.md:103** – Assembling atomic nodes in composition
- **ctxt/schema-object.md:325** – Sections array in object schema
- ADR-013 – Knowledge Graph and Mentions (entity-section relationships)
- ADR-017 – Composition Engine (assembling from sections)
- ADR-022 – Vector and Semantic Search (section-level embeddings)

**Related Documents:**
- `ctxt/decomposition/` – Decomposition implementation (to be created)
- `dPKMS/sections/` – Section storage (to be created)
- `ctxt/composition/sections.md` – Section-based composition (to be created)

**External References:**
- Zettelkasten Method: https://zettelkasten.de/introduction/
- Atomic Notes: https://notes.andymatuschak.org/Evergreen_notes
- Semantic Chunking: https://arxiv.org/abs/2103.15691

---
