# ADR-017 – Composition Engine and Template-Based Knowledge Assembly

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Knowledge systems excel at capture and retrieval but often fail at synthesis. Users face:

**Manual Synthesis Burden:**
- Assembling briefs from scattered notes is tedious
- Creating project documentation requires copy-pasting from multiple sources
- Meeting preparation involves manually gathering context
- Status reports need manual aggregation of progress

**Lost Connections:**
- Related knowledge exists but isn't connected in output
- Graph relationships not reflected in composed documents
- Atomic notes remain isolated rather than composed
- Provenance trails lost during manual assembly

**Inconsistent Output:**
- No standard formats for briefs, plans, or reports
- Tone and style vary unpredictably
- Different team members produce incompatible artifacts
- Templates exist but aren't integrated with knowledge base

**Context Gaps:**
- Composed documents lack links back to source knowledge
- Updates to source notes don't propagate to compositions
- Readers can't trace claims to original evidence
- Iterative refinement requires starting over

**Profile Misalignment:**
- Engineer briefs sound like founder memos
- Technical documentation uses business jargon
- Audience expectations not met by generic output
- No adaptation to role or project context

**Constraints:**
- Must preserve atomic note principles (compose from nodes, don't duplicate)
- Must maintain full provenance (traceable to sources)
- Must respect profile scoping (adapt to context)
- Must support graph-aware assembly (follow entity/mention links)
- Must remain deterministic (same inputs → same output)
- Must be template-extensible (users/plugins add templates)
- Must work offline (no mandatory external AI calls)

**Affected Subsystems:**
- Query engine (gathering source objects)
- Graph traversal (following mentions and backlinks)
- Focus profiles (template and style selection)
- Pipeline system (composition as pipeline step)
- Export system (composition outputs)
- CLI/API (composition commands)

**Goals:**
- One-command generation of structured artifacts from atomic notes
- Graph-aware assembly following entity/mention relationships
- Profile-adapted tone, style, and structure
- Full provenance preservation (traceable to sources)
- Template extensibility for users and plugins
- Support iterative refinement without data duplication

---

## Decision

**`ctxt` will implement a Composition Engine that generates structured artifacts (briefs, plans, reports, drafts) by assembling atomic knowledge nodes following graph relationships, applying profile-specific templates, and preserving full provenance trails.**

The composition engine operates through:

1. **Template Schema:**
   ```yaml
   template:
     name: technical-brief
     type: brief | plan | report | draft | memo | checklist
     profile: engineer  # or "all"

     metadata:
       output_format: markdown | html | pdf | json
       tone: formal | casual | technical | executive
       style: concise | detailed | bullet-points | narrative

     structure:
       sections:
         - name: Summary
           source: auto-summary
           max_length: 200
           required: true

         - name: Context
           source: related-entities
           filters:
             entity_patterns: ["@api.*", "@component.*"]
             lookback_days: 30
           graph_depth: 2

         - name: Key Decisions
           source: objects-with-decisions
           filters:
             decision_status: ["pending", "made"]
           sort: chronological

         - name: Action Items
           source: tasks
           filters:
             task_status: ["open"]
           format: checklist

         - name: References
           source: provenance
           format: links

     prompts:
       summary_generation: |
         Generate a technical summary for an engineering audience.
         Focus on implementation details and technical trade-offs.

       section_intro: |
         Provide a one-sentence introduction to this section.
   ```

2. **Assembly Algorithm:**
   ```
   1. Parse composition request (type, inputs, profile)
   2. Select template based on type and profile
   3. For each template section:
      a. Query dPKMS for source objects (filtered, sorted)
      b. Traverse graph if depth > 0
      c. Collect atomic nodes and relationships
      d. Apply section-specific transformations
      e. Generate section content (via template or AI)
   4. Assemble sections into document structure
   5. Apply profile-specific formatting (tone, style)
   6. Generate provenance metadata
   7. Return composition with source links
   ```

3. **Graph-Aware Assembly:**
   ```
   Composition follows entity/mention links:

   Starting object → mentions @api.stripe
     ↓
   Graph traversal finds:
     - Other objects mentioning @api.stripe
     - Related entities (@api.webhooks, @api.authentication)
     - Backlinks (objects mentioning current object)

   Assembly includes:
     - Primary source content
     - Related context from graph neighbors
     - Entity definitions and descriptions
     - Chronological ordering where relevant
   ```

4. **Provenance Preservation:**
   ```json
   {
     "composition_id": "uuid",
     "type": "technical-brief",
     "template": "technical-brief-v1",
     "profile": "engineer",
     "created_at": "timestamp",
     "sources": [
       {
         "object_id": "uuid",
         "section": "Context",
         "excerpt_hash": "sha256",
         "contribution": "provided API integration details"
       }
     ],
     "graph_context": {
       "entities": ["@api.stripe", "@component.checkout"],
       "traversal_depth": 2,
       "objects_included": 12
     },
     "output_hash": "sha256"
   }
   ```

5. **Profile Adaptation:**

   **Engineer Profile:**
   - Technical brief template
   - Focuses on implementation details
   - Includes code snippets and technical decisions
   - Concise, bullet-point style
   - Links to source objects and entities

   **Founder Profile:**
   - Executive brief template
   - Focuses on strategy and outcomes
   - Suppresses technical minutiae
   - Narrative, flowing style
   - Emphasizes decisions and next steps

   **Research Profile:**
   - Research report template
   - Includes methodology and sources
   - Detailed analysis and citations
   - Academic tone
   - Full reference list

6. **Composition Types:**

   **Brief:**
   - Summary of topic or entity
   - Context and background
   - Key insights and decisions
   - Action items and next steps
   - Usage: `ctxt make brief --entity @api.stripe`

   **Plan:**
   - Goal and objectives
   - Current state analysis
   - Proposed approach
   - Task breakdown
   - Dependencies and timeline
   - Usage: `ctxt make plan --topic "payment integration"`

   **Report:**
   - Executive summary
   - Detailed findings
   - Analysis and recommendations
   - Supporting data
   - Appendices
   - Usage: `ctxt make report --project Q1-goals`

   **Draft:**
   - Outline structure from knowledge graph
   - Populated sections with source content
   - Editing placeholders where gaps exist
   - Full provenance for fact-checking
   - Usage: `ctxt make draft --type blog-post --topic "API design"`

   **Checklist:**
   - Extracted tasks and action items
   - Organized by priority or dependency
   - Completion tracking
   - Source links for context
   - Usage: `ctxt make checklist --project migration`

7. **CLI Interface:**
   ```bash
   # Generate compositions
   ctxt make brief --entity @api.stripe
   ctxt make brief --from <object-id>
   ctxt make brief --query "tag==integration AND created_at>=2024-01-01"

   ctxt make plan --topic "payment migration"
   ctxt make plan --from <object-ids...>

   ctxt make report --project Q1-goals
   ctxt make report --profile founder

   ctxt make draft --type blog-post --topic "API design"
   ctxt make checklist --from <object-id>

   # Template management
   ctxt templates list
   ctxt templates show technical-brief
   ctxt templates create my-template --from template.yaml
   ctxt templates validate my-template

   # Output options
   ctxt make brief --entity @api.stripe --format markdown
   ctxt make brief --entity @api.stripe --format pdf --output ./brief.pdf
   ctxt make brief --entity @api.stripe --include-provenance
   ```

8. **Iterative Refinement:**
   ```bash
   # Initial composition
   ctxt make brief --entity @api.stripe --output brief.md

   # User edits brief.md manually

   # Re-compose with updates (preserves manual edits in sections marked)
   ctxt make brief --entity @api.stripe --update brief.md

   # Show what changed in sources since last composition
   ctxt make brief --entity @api.stripe --diff-sources brief.md
   ```

---

## Rationale

### Alternatives Considered

#### 1. **Manual Copy-Paste (Current State) (Rejected)**
Users manually assemble documents from search results.

**Rejected because:**
- High friction and cognitive load
- No graph awareness
- Lost provenance
- Inconsistent quality
- Time-consuming

#### 2. **Generic Document Generator (Rejected)**
Single template for all outputs, no profile awareness.

**Rejected because:**
- One-size-fits-all doesn't match role needs
- No adaptation to audience
- Missing context-specific formatting
- Ignores graph relationships

#### 3. **External Tool Integration (Rejected)**
Export to Notion/Confluence/Docs for composition.

**Rejected because:**
- Breaks local-first principle
- Loses provenance tracking
- External dependency
- Context not preserved
- No graph awareness

#### 4. **LLM-Only Generation (Rejected)**
Use LLM to generate entire document from prompt.

**Rejected because:**
- Hallucination risk
- No provenance preservation
- Not deterministic
- Expensive and requires API
- Doesn't leverage atomic notes

#### 5. **Static Export Templates (Rejected)**
Pre-defined export formats without composition logic.

**Rejected because:**
- No graph traversal
- No context gathering
- No profile adaptation
- Limited to simple dumps

### Benefits of Chosen Approach

**Graph-Aware Assembly:**
- Follows entity/mention relationships
- Discovers related context automatically
- Enables lateral connections
- Respects semantic structure

**Profile-Adapted Output:**
- Tone and style match audience
- Template selection matches role
- Content filtering appropriate to context
- Reduces post-generation editing

**Provenance Preservation:**
- Every claim traceable to source
- Updates to sources detectable
- Fact-checking enabled
- Trust through transparency

**Atomic Composition:**
- Reuses existing notes (no duplication)
- Updates propagate through references
- Knowledge compounds through composition
- Encourages granular capture

**Template Extensibility:**
- Users define custom templates
- Plugins add domain-specific templates
- Community can share templates
- Evolution without core changes

**Deterministic Output:**
- Same inputs → same output
- Testable and predictable
- Cacheable for performance
- Debugging-friendly

### Drawbacks / Risks

**Complexity:**
- Template schema is feature-rich
- Graph traversal adds computational cost
- Profile integration adds dependencies
- Provenance tracking adds overhead

**Template Maintenance:**
- Templates need updates as schema evolves
- Poor templates produce poor output
- Users may struggle with template creation
- Version compatibility concerns

**Over-Assembly:**
- May include too much context
- Graph depth tuning is non-trivial
- Balancing comprehensive vs concise

**LLM Dependency:**
- Some templates may require AI generation
- Offline capability limited without local models
- Quality varies with model choice
- Cost for API-based models

---

## Consequences

### Positive

**One-Command Synthesis:**
- Briefs, plans, reports generated instantly
- No manual aggregation required
- Consistent output format
- Time savings significant

**Knowledge Reuse:**
- Atomic notes composed into artifacts
- No duplication of content
- Updates flow through compositions
- Evergreen principle reinforced

**Traceability:**
- Full provenance preserved
- Source links included
- Fact-checking enabled
- Trust through transparency

**Context Adaptation:**
- Profile-appropriate output
- Audience-aware tone and style
- Template flexibility
- Extensible by plugins

### Negative

**Implementation Complexity:**
- Template engine is sophisticated
- Graph traversal optimization required
- Profile integration adds coupling
- Provenance tracking adds overhead

**User Learning Curve:**
- Template syntax to understand
- Composition commands to learn
- Profile effects to internalize
- Template creation requires skill

**Computational Cost:**
- Graph traversal can be expensive
- AI generation adds latency
- Large compositions slow
- Caching required for performance

**Quality Variance:**
- Template quality varies
- Graph depth tuning impacts results
- AI generation introduces uncertainty
- Bad templates produce poor output

### Neutral / Considerations

**Template Evolution:**
- Templates need versioning
- Breaking changes impact users
- Migration paths required
- Community curation needed

**LLM Selection:**
- Local models vs API trade-off
- Quality vs speed vs cost
- Profile-specific model preferences
- Fallback strategies needed

**Composition Storage:**
- Store compositions as objects?
- Track composition history?
- Reuse vs regenerate?
- Version control integration

**Cross-Profile Composition:**
- Can engineer profile use founder templates?
- Template inheritance/mixing?
- Override mechanisms needed
- Defaults per profile

---

## Implementation Notes

### Core Components

**Composition Engine (`ctxt/composition/`):**
```go
type CompositionEngine struct {
    templates   TemplateRegistry
    queryEngine QueryEngine
    graphStore  GraphStore
    profiles    ProfileManager
}

func (ce *CompositionEngine) Compose(
    req CompositionRequest,
) (*Composition, error)
```

**Template Engine:**
```go
type Template struct {
    Name      string
    Type      CompositionType
    Profile   string
    Structure SectionList
    Prompts   map[string]string
}

type Section struct {
    Name         string
    Source       SourceType
    Filters      FilterSet
    GraphDepth   int
    MaxLength    int
    Required     bool
    Format       OutputFormat
    Transformer  func([]Object) (string, error)
}

type SourceType string
const (
    SourceAutoSummary        SourceType = "auto-summary"
    SourceRelatedEntities    SourceType = "related-entities"
    SourceObjectsWithDecisions SourceType = "objects-with-decisions"
    SourceTasks              SourceType = "tasks"
    SourceProvenance         SourceType = "provenance"
    SourceQuery              SourceType = "query"
)
```

**Built-in Templates:**
- `technical-brief` (engineer profile)
- `executive-brief` (founder profile)
- `research-report` (research profile)
- `implementation-plan` (engineer profile)
- `strategy-memo` (founder profile)
- `meeting-prep` (all profiles)
- `status-update` (all profiles)
- `blog-post-draft` (writer profile)
- `task-checklist` (all profiles)

**CLI Commands:**
```bash
# Generate compositions
ctxt make <type> [options]

# Options
--entity <entity>        # Compose about entity
--from <ids...>          # Source objects
--query <query>          # RSQL query for sources
--topic <text>           # Free-text topic
--project <name>         # Project context
--profile <name>         # Override active profile
--template <name>        # Specific template
--format <format>        # Output format
--output <path>          # Write to file
--include-provenance     # Add provenance section
--graph-depth <n>        # Traversal depth
--update <file>          # Update existing composition
--diff-sources <file>    # Show source changes
```

**Storage Schema:**

Compositions table (optional):
```sql
CREATE TABLE compositions (
    id UUID PRIMARY KEY,
    type TEXT NOT NULL,
    template TEXT NOT NULL,
    profile TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP,
    input_query TEXT,
    graph_context JSON,
    output_hash TEXT,
    output_path TEXT,
    provenance JSON,
    INDEX(type),
    INDEX(created_at),
    INDEX(profile)
);

CREATE TABLE composition_sources (
    composition_id UUID NOT NULL,
    object_id UUID NOT NULL,
    section TEXT,
    contribution TEXT,
    PRIMARY KEY(composition_id, object_id),
    FOREIGN KEY(composition_id) REFERENCES compositions(id),
    FOREIGN KEY(object_id) REFERENCES objects(id)
);
```

### Integration Points

**With Query Engine:**
1. Template section defines query
2. Query engine retrieves filtered objects
3. Sorting and limits applied
4. Objects passed to transformer

**With Graph System:**
1. Starting objects identified
2. Graph traversal to specified depth
3. Related entities and objects collected
4. Backlinks included if relevant
5. Graph context preserved in provenance

**With Focus Profiles:**
1. Active profile selects template
2. Profile filters applied to queries
3. Profile tone/style applied to generation
4. Profile language preferences respected

**With Pipeline System:**
1. Composition can be pipeline step
2. Generated compositions can be re-ingested
3. Pipeline steps can output composition requests
4. Templates can specify pipelines for sources

### Migration Strategy

**Phase 1: Basic Composition (Skeleton 7)**
- Single template (technical-brief)
- Simple section sources (query-based)
- No graph traversal
- Markdown output only

**Phase 2: Graph-Aware (Skeleton 7)**
- Graph traversal support
- Entity-based composition
- Provenance tracking
- Multiple output formats

**Phase 3: Profile Integration (Skeleton 7)**
- Profile-specific templates
- Tone and style adaptation
- Template selection logic
- Language preferences

**Phase 4: Advanced Features (Skeleton 8+)**
- Iterative refinement (--update)
- Source diff tracking
- Custom template creation
- Plugin-defined templates
- AI-powered section generation

**Backward Compatibility:**
- No breaking changes (new feature)
- Optional composition storage
- Works without profile system
- Degrades gracefully without AI

### Testing Requirements

**Unit Tests:**
- Template parsing and validation
- Section source resolution
- Graph traversal logic
- Provenance generation
- Profile selection

**Integration Tests:**
- End-to-end composition workflows
- Graph-aware assembly
- Profile-adapted output
- Multiple output formats

**User Acceptance Tests:**
- Template usability
- Output quality assessment
- Provenance correctness
- Iterative refinement workflows

**Performance Tests:**
- Composition of 100+ source objects
- Graph traversal depth 3-5
- Large template rendering
- Concurrent compositions

### Performance Optimization

**Caching:**
- Cache compiled templates
- Cache graph traversal results (TTL 5m)
- Cache entity definitions
- Cache rendered sections

**Lazy Evaluation:**
- Skip optional sections if sources empty
- Defer AI generation until needed
- Stream output for large compositions

**Parallelization:**
- Query multiple sections in parallel
- Concurrent graph traversals
- Parallel AI generation calls

---

## References

- **architecture.md:274-278** – Composition Engine specification
- **architecture.md:1116-1128** – Composition Path diagram
- **ROADMAP.md** – Skeleton 3: Daily Usability (composition features)
- ADR-015 – Focus Profiles (profile-adapted composition)
- ADR-013 – Knowledge Graph (graph-aware assembly)
- ADR-014 – Two-Package Architecture (ctxt responsibility)

**Related Documents:**
- `ctxt/composition/` – Composition engine (to be created)
- `ctxt/templates/` – Template definitions (to be created)
- `ctxt/profiles/` – Profile integration (ADR-015)

---
