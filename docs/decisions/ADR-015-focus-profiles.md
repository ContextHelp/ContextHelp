# ADR-015 – Focus Profiles for Context-Aware Knowledge Scoping

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Users operate in multiple distinct contexts throughout their day—switching between roles (founder, engineer, researcher, writer), projects (client work, personal projects, open-source contributions), and modes (deep focus, triage, learning). Without explicit context boundaries, knowledge systems suffer from:

**Context Collapse:**
- All captured knowledge treated equally regardless of current role or project
- Search results mix professional, personal, technical, and strategic content
- No way to filter or boost relevance based on current work mode
- Cognitive overhead from irrelevant results

**One-Size-Fits-All Retrieval:**
- Same ranking weights applied regardless of user intent
- No differentiation between "founder reviewing strategy" vs "engineer debugging code"
- Composition templates unable to adapt tone, style, or structure to context
- Pipeline selection cannot optimize for context-specific needs

**Missing Semantic Boundaries:**
- No way to scope registry subscriptions to specific contexts
- Entity visibility cannot be filtered by relevance to current work
- Tags and mentions lack context-aware boosting/suppression
- Just-in-time surfacing cannot adapt to current focus

**Constraints:**
- Must remain entirely in `ctxt` layer (dPKMS stays context-agnostic)
- Must not break existing ingestion or retrieval flows
- Must support both role-based (e.g., "engineer") and project-based (e.g., "Q1 Planning") profiles
- Must allow dynamic switching without data duplication
- Must be declarative and configuration-driven
- Must support plugin extensions to profiles

**Affected Subsystems:**
- Capture layer (profile context at ingestion time)
- Pipeline selection (context-aware routing)
- Search and retrieval (profile-scoped filtering and ranking)
- Just-in-time surfacing (profile-relevant resurfacing)
- Composition engine (profile-specific templates and tone)
- Configuration system (profile definitions)

**Goals:**
- Enable users to work in distinct, well-defined contexts
- Reduce cognitive load through relevance filtering
- Improve retrieval precision via context-aware ranking
- Support natural switching between roles and projects
- Maintain data sovereignty (profiles are local, not synced by default)

---

## Decision

**`ctxt` will implement Focus Profiles as declarative, composable context lenses that scope knowledge visibility, ranking, surfacing, pipeline selection, and composition behavior based on user's current role or project.**

Focus Profiles provide:

1. **Profile Types:**
   - **Role Profiles:** Long-lived contexts (Founder, Engineer, Research, Writer, Family)
   - **Project Profiles:** Time-bounded or initiative-scoped (Project X, Q1 Planning, Client Acme)

2. **Profile Configuration Schema:**
   ```yaml
   profile:
     name: string
     description: string
     type: role | project

     scopes:
       registries: [registry-names]
       entities:
         include: [entity-patterns]
         exclude: [entity-patterns]
       tags:
         boost: [tag-patterns]
         suppress: [tag-patterns]

     pipelines:
       preferred: [pipeline-names]
       skip: [pipeline-step-names]

     ranking:
       recency_weight: float
       entity_match_boost: float
       tag_match_boost: float
       mention_match_boost: float

     surfacing:
       max_results: int
       time_window: duration
       triggers: [entity-patterns | tag-patterns]

     composition:
       templates: [template-names]
       tone: string
       style: string
       language_preferences: [language-codes]
   ```

3. **Profile Effects on System Behavior:**

   **Ingestion (Write Path):**
   - Active profile recorded in object metadata (optional)
   - Pipeline selection influenced by profile preferences
   - No mandatory profile requirement (backwards compatible)

   **Retrieval (Read Path):**
   - Results filtered by entity/tag include/exclude rules
   - Ranking weights adjusted per profile configuration
   - Boosted/suppressed tags applied to scoring
   - Registry scoping limits federated queries

   **Surfacing:**
   - Resurfacing filtered by profile triggers and time windows
   - Notification thresholds respect profile preferences
   - Entity-based surfacing scoped to relevant entities

   **Composition:**
   - Template selection based on profile type
   - Tone and style applied to generated content
   - Language preferences override global settings

4. **Profile Switching:**
   - Command: `ctxt profile use <name>`
   - Stored in user session state (not persistent by default)
   - Can be overridden per-command: `ctxt find "query" --profile engineer`
   - Optional profile pinning for longer sessions

5. **Profile Permissions:**
   - Plugins must declare required profile access
   - Profiles can restrict plugin visibility to scoped data
   - Agent operations inherit active profile constraints

---

## Rationale

### Alternatives Considered

#### 1. **Workspace-Based Separation (Rejected)**
Create entirely separate knowledge stores per context (e.g., `~/.ctxt/work/`, `~/.ctxt/personal/`).

**Rejected because:**
- Violates evergreen principle (knowledge can't compound across contexts)
- Breaks atomic linking (notes can't reference across workspaces)
- Forces users to predict context at capture time
- Prevents serendipitous discovery of cross-context connections
- Creates data duplication and sync problems

#### 2. **Tag-Based Filtering Only (Rejected)**
Rely on users manually adding context tags (#work, #personal) and filter at query time.

**Rejected because:**
- Tags are emergent, not declarative boundaries
- No support for registry scoping or pipeline preferences
- Requires manual tagging discipline (high friction)
- Cannot adapt ranking weights or composition behavior
- No support for entity-level scoping

#### 3. **Global Preferences with Manual Overrides (Rejected)**
Single global configuration with per-query manual filters.

**Rejected because:**
- High cognitive overhead for every query
- No persistent context state across session
- Cannot influence ingestion or surfacing behavior
- No support for role-specific templates or pipelines

#### 4. **AI-Inferred Context (Rejected)**
Automatically infer user's current context via behavior analysis.

**Rejected because:**
- Unpredictable and non-deterministic
- Cannot handle explicit context boundaries
- Violates user control and transparency principles
- Complex to implement and test reliably

### Benefits of Chosen Approach

**Explicit Control:**
- Users explicitly declare and switch contexts
- No ambiguity about active scope
- Declarative configuration enables sharing and versioning

**Cognitive Load Reduction:**
- Results automatically filtered to relevant context
- No need to mentally filter irrelevant matches
- Surfacing adapts to current work focus

**Behavioral Adaptation:**
- Pipelines optimize for context-specific extractions
- Composition adapts tone, style, and templates
- Ranking weights reflect context priorities

**Data Unification:**
- All knowledge stored in single graph
- Cross-context linking remains possible
- Evergreen principle preserved

**Extensibility:**
- Plugins can define profile-specific behaviors
- Templates can be profile-scoped
- Registry subscriptions can be context-limited

**Sovereignty:**
- Profiles stored locally, no forced sync
- Users control all profile boundaries
- Export/import preserves profile metadata

### Drawbacks / Risks

**Increased Complexity:**
- Users must learn profile concept
- Configuration grows with multiple profiles
- Profile switching adds interaction overhead

**Potential Over-Filtering:**
- Strict scopes may hide relevant cross-context knowledge
- Users may forget to switch profiles
- Include/exclude rules require maintenance

**Performance Overhead:**
- Profile filtering adds query complexity
- Registry scoping requires conditional fan-out
- Ranking adjustments increase computation

**Migration Complexity:**
- Existing users start with no profiles
- Default profile behavior must be intuitive
- Profile-less operation must remain viable

---

## Consequences

### Positive

**Reduced Context Collapse:**
- Users experience focused, relevant results
- Cognitive load decreases during retrieval
- Work modes become well-defined and switchable

**Adaptive Behavior:**
- System adapts to user's current needs automatically
- Pipeline selection optimizes for context
- Composition output matches expected style and tone

**Improved Precision:**
- Ranking weights tuned for context priorities
- Tag/entity boosting improves relevance
- Surfacing becomes proactive and contextual

**Extensible Foundation:**
- Plugins can leverage profiles for scoping
- Templates and behaviors can be profile-aware
- Future features inherit profile benefits

### Negative

**Learning Curve:**
- Users must understand profile concept
- Configuration requires defining scopes
- Profile switching adds interaction step

**Maintenance Overhead:**
- Profile definitions need updates as work changes
- Include/exclude rules require pruning
- Multiple profiles increase configuration size

**Hidden Knowledge Risk:**
- Strict filtering may hide serendipitous connections
- Users may miss cross-context relevance
- Forgotten profile switches cause confusion

**Implementation Complexity:**
- Filtering logic spans ingestion, retrieval, surfacing
- Ranking adjustments increase query complexity
- Profile state management adds session concerns

### Neutral / Considerations

**Profile Granularity:**
- Too few profiles = insufficient scoping
- Too many profiles = switching overhead
- Users must find personal balance

**Default Behavior:**
- System must work sensibly with no active profile
- Profile-less mode should not feel degraded
- Migration path must be smooth

**Cross-Profile Linking:**
- Mentions and backlinks work across profiles
- Graph navigation honors profile scopes
- Export bundles can include profile metadata

**Plugin Interactions:**
- Plugins must declare profile compatibility
- Profile scopes apply to plugin-added content
- Plugin-defined profiles must follow same schema

---

## Implementation Notes

### Core Components

**Profile Manager (`ctxt/profiles/`):**
- Load profile definitions from config
- Validate profile schemas
- Track active profile per session
- Handle profile switching

**Profile Context (`ctxt/context/`):**
- Inject profile into ingestion jobs
- Apply profile filters to queries
- Adjust ranking weights per profile
- Scope registry queries

**Profile Configuration:**
```yaml
# User config: ~/.config/ctxt/config.yaml
profiles:
  engineer:
    type: role
    description: "Technical implementation focus"
    scopes:
      registries:
        - technical-patterns
        - api-references
      entities:
        include:
          - "@api.*"
          - "@component.*"
        exclude:
          - "@marketing.*"
      tags:
        boost:
          - technical
          - debugging
        suppress:
          - marketing
    pipelines:
      preferred:
        - url.repo
        - code.snippet
    ranking:
      recency_weight: 0.7
      entity_match_boost: 2.0
    surfacing:
      max_results: 10
      time_window: 30d
    composition:
      templates:
        - technical-brief
        - implementation-plan

  founder:
    type: role
    description: "Strategic and high-level focus"
    scopes:
      registries:
        - business-patterns
      entities:
        include:
          - "@strategy.*"
          - "@market.*"
      tags:
        boost:
          - strategic
          - growth
        suppress:
          - technical-details
    ranking:
      recency_weight: 0.5
      entity_match_boost: 1.5
    surfacing:
      max_results: 5
      time_window: 7d
    composition:
      templates:
        - executive-brief
        - strategy-memo

# Default active profile (optional)
active_profile: engineer
```

**CLI Commands:**
```bash
# Profile management
ctxt profile list
ctxt profile show <name>
ctxt profile use <name>
ctxt profile validate <name>

# Profile-scoped operations
ctxt find "query" --profile engineer
ctxt add "content" --profile founder
ctxt make brief --profile engineer

# Profile creation/editing
ctxt profile create <name> --type role
ctxt profile edit <name>
```

**Storage Schema Extensions:**

No dPKMS schema changes required. Profile context stored in object metadata:

```json
{
  "id": "uuid",
  "metadata": {
    "captured_with_profile": "engineer",
    "profiles": ["engineer", "project-x"]
  }
}
```

### Integration Points

**Ingestion Pipeline:**
1. User runs `ctxt add` with active profile
2. Profile name recorded in job metadata
3. Pipeline selection influenced by profile preferences
4. Enrichment steps may use profile context for hints

**Query Engine (dPKMS):**
1. `ctxt` provides profile filter AST nodes
2. dPKMS applies filters during query execution
3. Registry scatter respects profile registry scoping
4. Results returned to `ctxt` for profile-aware reranking

**Surfacing Engine:**
1. Profile triggers define entity/tag patterns
2. Time windows filter resurfacing candidates
3. Max results limit notification volume
4. Profile-relevant items prioritized

**Composition Engine:**
1. Template selection based on profile type
2. Tone/style applied to generated content
3. Language preferences override global settings
4. Provenance includes profile context

### Migration Strategy

**Phase 1: Introduce Profiles (Optional, Opt-In)**
- Add profile configuration schema
- Implement profile manager and context
- Profile-less operation remains default
- No breaking changes

**Phase 2: Profile-Aware Features**
- Add profile filtering to queries
- Implement profile-scoped registry queries
- Add profile context to ingestion
- CLI commands for profile management

**Phase 3: Profile-Driven Behaviors**
- Profile-aware ranking adjustments
- Profile-scoped surfacing
- Profile-specific composition templates
- Plugin profile integration

**Backward Compatibility:**
- All existing operations work without profiles
- Profile metadata optional in objects
- Default profile can be disabled
- Profile-less queries use global config

### Testing Requirements

**Unit Tests:**
- Profile schema validation
- Profile filter generation
- Ranking weight adjustments
- Template selection logic

**Integration Tests:**
- Profile switching across sessions
- Profile-scoped queries end-to-end
- Registry filtering by profile
- Composition with profile context

**User Acceptance Tests:**
- Profile creation and editing flows
- Context switching usability
- Cross-profile linking behavior
- Profile export/import

### Performance Considerations

**Query Overhead:**
- Profile filters add AST complexity: ~5-10ms
- Registry scoping reduces remote queries
- Ranking adjustments: negligible overhead

**Storage Overhead:**
- Profile metadata per object: ~50-100 bytes
- No additional indexes required
- Profile definitions: ~1-5KB per profile

**Optimization Strategies:**
- Cache compiled profile filters
- Pre-compute profile-scoped registry lists
- Lazy-load profile ranking adjustments

---

## References

- **architecture.md:970-1068** – Agent Context Profiles specification
- **ctxt/configuration.md:332-345** – Agent profiles configuration
- **design.md** – Focus profiles design rationale
- ADR-003 – Separate Read/Write Paths (profiles affect both)
- ADR-010 – Extended RSQL (profile filters as AST nodes)
- ADR-014 – Two-Package Architecture (profiles are ctxt-only)

**Related Documents:**
- `ctxt/profiles/` – Profile implementation (to be created)
- `ctxt/context/` – Profile context management (to be created)
- `ctxt/surfacing/` – Profile-aware surfacing (to be created)
- `ctxt/composition/` – Profile-specific templates (to be created)

---
