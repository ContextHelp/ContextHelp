# Skeleton 6: Production Polish & Multimedia Tasks

**Package Focus:** dPKMS (60%) + ctxt (40%)

**Goal:** Production readiness (migrations, backups) + multimedia pipelines (audio/video transcription). Full mention/entity support across all modalities.

---

## dPKMS Infrastructure Team (The Custodian)

### Database Migrations System

- [PLAN] 📦 *dPKMS* Implement automated schema migrations
  - [ ] Choose migration engine (golang-migrate, goose, or custom)
  - [ ] Embed SQL migration files using `embed.FS`:
    ```go
    //go:embed migrations/*.sql
    var migrationFiles embed.FS
    ```
  - [ ] Required migrations for semantic identity:
    - [ ] `002_add_mentions_column.sql` - add `mentions` JSONB array
    - [ ] `003_create_entities_table.sql` - canonical entity definitions
    - [ ] `004_create_backlinks_table.sql` - knowledge graph edges
    - [ ] `005_add_entity_indexes.sql` - mention filtering and entity lookup
    - [ ] `006_add_vectors_table.sql` - embedding storage
  - [ ] Migration runner:
    - [ ] Track applied migrations in `schema_migrations` table
    - [ ] Run pending migrations on startup
    - [ ] Rollback support for development
    - [ ] Validation before applying
  - [ ] Automatic transition from Skeleton 5 → v1.0 schema

### Backup and Restore Commands

- [PLAN] 📦 *ctxt* Implement `ctxt config backup` and `restore`
  - [ ] Backup command: `ctxt config backup --out ./backup.zip`
  - [ ] Backup must include:
    - [ ] SQLite database file (all tables including graph)
    - [ ] Configuration files (`config.yaml`)
    - [ ] Plugin folder contents (`~/.config/contexthelp/plugins/`)
    - [ ] Registry caches (synced entities and taxonomies)
    - [ ] **Knowledge graph tables** (entities, backlinks, aliases)
    - [ ] **Entity indexes** (for fast mention resolution)
    - [ ] Vector storage (embeddings)
  - [ ] Backup metadata:
    - [ ] ContextHelp version
    - [ ] Schema version
    - [ ] Backup timestamp
    - [ ] Integrity checksums
  - [ ] Restore command: `ctxt config restore --from ./backup.zip`
  - [ ] Restore validation:
    - [ ] Check schema compatibility before writing
    - [ ] Warn on version mismatches
    - [ ] Offer migration path if needed
    - [ ] Validate entity schema integrity
  - [ ] Progress indicators for large backups

### Version Check and Update Warnings

- [ ] 📦 *ctxt* Implement `ctxt version --check` command
  - [ ] Check release metadata from GitHub/registry
  - [ ] Warn about:
    - [ ] Entity schema updates (breaking changes)
    - [ ] Graph schema updates (new tables/indexes)
    - [ ] Migration requirements (manual steps needed)
    - [ ] Plugin compatibility (API version changes)
  - [ ] Provide structured upgrade notes:
    - [ ] Link to migration guide
    - [ ] Breaking changes summary
    - [ ] Semantic schema evolution details
  - [ ] Display current vs latest version
  - [ ] Optional auto-update flag (future)

---

## ctxt Ingestion Team (The Transcriber)

### Audio Pipeline Implementation

- [PLAN] 📦 *ctxt* Create `audio.transcript` pipeline
  - [ ] Implement `AudioClient` abstraction:
    ```go
    type AudioClient interface {
        Transcribe(ctx context.Context, audioData []byte) (Transcript, error)
    }
    ```
  - [ ] Adapter options:
    - [ ] Whisper API (OpenAI)
    - [ ] Local Whisper model
    - [ ] AssemblyAI or similar services
  - [ ] Pipeline workflow:
    1. [ ] Upload/process audio file
    2. [ ] Receive transcript (VTT, SRT, or plain text)
    3. [ ] Feed transcript text to `text.long` pipeline
    4. [ ] Extract mentions from transcript
    5. [ ] Resolve mentions to entities (local → registry)
    6. [ ] Update graph edges (backlinks)
    7. [ ] Store knowledge object with audio provenance
  - [ ] Handle gracefully:
    - [ ] Missing transcription (silence, unsupported format)
    - [ ] API rate limits (retry with backoff)
    - [ ] Large files (chunked upload)
  - [ ] Log transcription quality metrics

### Video Pipeline Implementation

- [PLAN] 📦 *ctxt* Create `video.youtube` pipeline
  - [ ] Integration options:
    - [ ] Use `yt-dlp` if available (shell-out)
    - [ ] Go native client for YouTube API
  - [ ] Pipeline workflow:
    1. [ ] Extract video ID from URL
    2. [ ] Fetch metadata (title, description, author)
    3. [ ] Retrieve transcript:
       - [ ] Auto-generated captions
       - [ ] Manual captions (preferred)
       - [ ] Multiple languages (user preference)
    4. [ ] Feed transcript to `text.long`
    5. [ ] Extract mentions from transcript
    6. [ ] Resolve mentions to entities
    7. [ ] Populate graph edges (backlinks)
    8. [ ] Store with video metadata
  - [ ] Handle missing transcript:
    - [ ] Mark job as `NeedsHumanInput` or
    - [ ] Fail cleanly with descriptive error
    - [ ] Offer manual transcript upload option
  - [ ] Support other video platforms (Vimeo, etc.) in future

### Robust Error Handling Policy

- [PLAN] 📦 *ctxt* Implement error classification and handling
  - [ ] Transient errors → Retry:
    - [ ] Network timeouts
    - [ ] API rate limits (with exponential backoff)
    - [ ] Temporary service unavailability
  - [ ] Permanent errors → Fail:
    - [ ] Unsupported file format
    - [ ] Invalid URL
    - [ ] Authentication failure (invalid API key)
  - [ ] **Mention extraction errors:**
    - [ ] Log warning but don't block ingestion
    - [ ] Store partial mentions if any detected
    - [ ] Mark object for manual review
  - [ ] **Entity resolution failures:**
    - [ ] Fallback to unresolved local entity placeholders
    - [ ] Preserve mention text for future resolution
    - [ ] Log resolution failures for registry debugging
  - [ ] Job status updates:
    - [ ] `Completed` - success
    - [ ] `Failed` - permanent error
    - [ ] `NeedsHumanInput` - requires user action
    - [ ] `Retrying` - transient error, will retry

---

## dPKMS Search Team & ctxt Retrieval Team (The Explainer)

### Ranking Explainability

- [PLAN] 📦 *dPKMS* Implement score breakdown
  - [ ] Update reranker to return structured breakdown:
    ```go
    type ScoreBreakdown struct {
        FTSScore          float64
        VectorScore       float64
        RecencyBoost      float64
        EntityAlignment   float64
        GraphRelevance    float64
        RegistryWeight    float64
        TotalScore        float64
        Components        []Component
    }
    ```
  - [ ] Component details:
    - [ ] FTS score (keyword match quality)
    - [ ] Vector similarity (semantic match)
    - [ ] Recency boost (time decay function)
    - [ ] **Entity alignment contribution** (mentions overlap)
    - [ ] **Graph relevance contribution** (backlink paths)
    - [ ] Registry weights applied
  - [ ] Expose via `ctxt list --explain` flag
  - [ ] JSON output for programmatic access
  - [ ] Human-readable summary in CLI

### Terminal UI Polish

- [PLAN] 📦 *ctxt* Implement polished table renderer
  - [ ] Replace raw output with table library (tablewriter, lipgloss)
  - [ ] Color-code outputs:
    - [ ] Tags (blue/cyan)
    - [ ] Mentions (green with `@` prefix)
    - [ ] Entity titles (bold)
    - [ ] Scores (yellow)
  - [ ] Add columns for semantic signals:
    - [ ] **Mentions** column: `@ui.best-practice, @design.pattern`
    - [ ] **Entities** column: Human-readable entity titles
    - [ ] Truncate long values with ellipsis
  - [ ] Support multi-language display:
    - [ ] Show translated entity titles where available
    - [ ] Fallback to slug if untranslated
    - [ ] Indicate language in footer
  - [ ] Responsive column widths (terminal size)
  - [ ] Optional compact mode for scripting

### "Did You Mean?" Suggestions

- [PLAN] 📦 *dPKMS* Implement zero-result suggestions
  - [ ] When search returns 0 results:
    - [ ] Run vector fallback search (fuzzy semantic match)
    - [ ] Suggest mention wildcard patterns:
      - [ ] Query `@ui.button` → suggest `mention:ui.*`
      - [ ] Query `@stripe.checkout` → suggest `mention:stripe.api.*`
    - [ ] Suggest entity aliases:
      - [ ] Query mentions `@ux.pattern` → suggest `@ui.best-practice`
      - [ ] Use alias table from registries
    - [ ] Suggest related entities (graph neighbors)
  - [ ] Rank suggestions by:
    - [ ] Semantic similarity (vector distance)
    - [ ] Entity popularity (backlink count)
    - [ ] Registry confidence weights
  - [ ] Display format:
    ```
    No results found. Did you mean:
    - mention:ui.best-practice (73 objects)
    - mention:design.pattern.* (45 objects)
    - @ux.usability (related entity)
    ```

---

## dPKMS Registry Team (The Publisher)

### Default ContextHelp Registry

- [PLAN] 📦 *dPKMS* Publish official registry
  - [ ] Create registry repository structure:
    - [ ] `taxonomy.json` - tag taxonomy
    - [ ] `entities.json` - canonical entity definitions
    - [ ] `aliases.json` - entity aliases and redirects
    - [ ] `translations/` - i18n files per language
  - [ ] Include example entities:
    - [ ] `ui.best-practice` - UI/UX patterns
    - [ ] `component.form.input` - Component library
    - [ ] `workflow.youtube.production` - Content workflows
    - [ ] `design.pattern.strategy` - Software patterns
  - [ ] Entity metadata:
    - [ ] Title, description, aliases
    - [ ] Namespace grouping
    - [ ] Version numbers
    - [ ] Translations (EN, ES, FR, DE, JA)
  - [ ] Publish to:
    - [ ] GitHub Pages (static JSON)
    - [ ] CDN (fast global access)
  - [ ] Default config points to this registry:
    ```yaml
    registries:
      - name: contexthelp-official
        url: https://registry.contexthelp.ai/v1/
        enabled: true
        priority: 100
    ```

### CLI Localization Support

- [ ] 📦 *ctxt* Implement `--lang` flag
  - [ ] Accept language code: `--lang es`, `--lang ja`
  - [ ] Entity and mention display:
    - [ ] Prefer translated titles from registry
    - [ ] Fallback to English if translation missing
    - [ ] Fallback to slug if no translations
  - [ ] **Ensure mention slugs remain stable:**
    - [ ] Internal storage always uses canonical slug
    - [ ] Display layer shows translation
    - [ ] Graph queries use canonical IDs
  - [ ] Localize CLI messages and help text
  - [ ] Respect system locale as default

### Registry Capability Handshake

- [ ] 📦 *dPKMS* Implement `/capabilities` endpoint check
  - [ ] Registry advertises capabilities:
    ```json
    {
      "version": "1.0",
      "features": {
        "entities": true,
        "aliases": true,
        "translations": ["en", "es", "fr"],
        "entity_sync": true,
        "taxonomy_weights": true
      }
    }
    ```
  - [ ] Client checks capabilities on registry add
  - [ ] Warn users if registry lacks features:
    - [ ] "Warning: Registry 'example' does not support entity sync. Mentions may not resolve."
    - [ ] "Warning: Registry 'example' has no translations for language 'ja'."
  - [ ] Graceful degradation:
    - [ ] Use registry for taxonomy only if no entities
    - [ ] Skip entity sync if unsupported
  - [ ] Cache capabilities with registry metadata

---

## Integration Check (Multimedia + Mentions + Graph)

### Scenario: Audio Ingestion

1. [ ] Run:
   ```bash
   ctxt analyze --file "vlog_draft.mp3" --hints "#youtube #draft"
   ```

2. [ ] Verify:
   - [ ] Audio pipeline transcribes via Whisper
   - [ ] Transcript text goes to `text.long`
   - [ ] Mentions extracted: `@content.strategy`, `@youtube.workflow`
   - [ ] Entities resolved from registry
   - [ ] Graph edges created (backlinks)
   - [ ] Job logs show all steps
   - [ ] Knowledge object stored with audio metadata

### Scenario: Explainable Search

1. [ ] Run:
   ```bash
   ctxt list --q "video ideas" --explain
   ```

2. [ ] Verify output format:
   ```
   ╭─────────────────────────────────────────────────────────╮
   │ ID      │ Title           │ Mentions           │ Score │
   ├─────────────────────────────────────────────────────────┤
   │ abc123  │ Video Ideas...  │ @youtube.workflow  │ 0.87  │
   │         │                 │ @content.strategy  │       │
   ╰─────────────────────────────────────────────────────────╯

   Score Breakdown for abc123:
   - FTS Score:          0.45  (keyword "video ideas")
   - Vector Score:       0.32  (semantic similarity)
   - Entity Alignment:   0.25  (mention overlap)
   - Graph Relevance:    0.15  (backlink proximity)
   - Recency Boost:      0.08  (3 days old)
   - Registry Weight:    1.2x  (official registry boost)
   Total Score:          0.87
   ```

### Validation Checklist

**dPKMS Validation:**
- [ ] Migration system runs automatically on startup
- [ ] Backup includes all graph tables and indexes
- [ ] Restore validates schema compatibility
- [ ] Version check warns about breaking changes
- [ ] Score breakdown exposes all ranking components
- [ ] "Did you mean?" provides useful suggestions
- [ ] Registry capabilities detected correctly

**ctxt Validation:**
- [ ] Audio pipeline transcribes and extracts mentions
- [ ] Video pipeline retrieves transcripts
- [ ] Error handling classifies and retries appropriately
- [ ] Terminal UI displays entities and mentions cleanly
- [ ] Localization works (translations displayed)
- [ ] `--explain` flag shows score breakdown

**Cross-Package Validation:**
- [ ] Multimedia pipelines persist mentions/entities to dPKMS
- [ ] Search results include semantic signals in UI
- [ ] Registry sync populates entity cache for offline use
- [ ] Backup/restore preserves full semantic graph

---

## Risks to Watch For

- **External Tool Dependencies:** `ffmpeg`, `yt-dlp` may reduce portability
- **Migration Safety:** Entity/graph schema evolution must not risk data loss
- **Cloud Inference Cost:** Need flags to control upload size and model choice
- **Graph Consistency:** Validate entity definitions from registries to avoid cycles
- **Transcription Quality:** Noisy transcripts may produce poor mention extraction
- **Registry Availability:** Default registry must be highly available (CDN)

---

## See Also

- Sprint spec: `docs/sprints/006-polish-and-multimedia.md`
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
