# Skeleton 6 Goal: "Production Polish & Multimedia"

## Package Focus

**Primary Package:** dPKMS (60%) + ctxt (40%)

Production readiness requires solid infrastructure (migrations, backups) and rich pipelines (audio/video transcription).

**Package Breakdown:**
- **dPKMS:** Migration system, backup/restore, schema evolution, graph stability
- **ctxt:** Audio pipeline, video pipeline, error handling, CLI UX polish, ranking explainability

---

By the end of this skeleton:

1. **Multimedia:** Users can ingest Audio/Video files (transcription) with full mention extraction and entity resolution support across modalities.
2. **Stability:** Database schema migrations are automated; backups and restores are supported; entity and graph schema evolution is safe.
3. **UX:** The CLI outputs polished tables and provides explainability for ranking, including mention-aware and graph-aware scoring.

---

## 1. dPKMS Infrastructure Team (The Custodian)

**Focus:** Data safety, lifecycle management, schema evolution for mentions, entities, and the knowledge graph.
**Why:** The introduction of semantic identity (mentions → entities → graph) requires new tables and guarantees that cannot risk user data.

### Task 1.1: Database Migrations System

- Implement a migration runner using an embedded migration engine.
- Embed SQL migration files using `embed.FS`.
- Required migrations:
  - new `mentions` column in bookmarks
  - new `entities` table with canonical fields
  - new `backlinks` table for entity–knowledge object edges
  - new indexes for mention filtering and entity lookup
- Action: Transition automatically from Skeleton 5 schema to v1.0 schema when needed.

### Task 1.2: Backup and Restore Commands

- Implement `ctxt config backup --out ./backup.zip`.
- Backup must include:
  - SQLite DB
  - configuration files
  - plugin folder contents
  - registry caches, including synced entities and taxonomies
  - knowledge graph tables and entity indexes
- Restore command should validate schema compatibility before writing.

### Task 1.3: Update and Version Check

- Implement `ctxt version --check`.
- Check release metadata to warn about:
  - entity schema updates
  - graph schema updates
  - migration requirements
- Provide structured upgrade notes for semantic schema evolution.

---

## 2. ctxt Ingestion Team (The Transcriber)

**Focus:** Full multimodal ingestion with transcription, mention extraction, and entity resolution.
**Constraint:** Avoid bundling heavy system libraries; rely on shell-outs or lightweight adapters.

### Task 2.1: `audio.transcript` Pipeline

- Implement `AudioClient` abstraction (e.g., Whisper API adapter).
- Workflow:
  - Upload audio → receive VTT/Text → feed to `text.long`.
  - After transcription:
    - extract mentions
    - resolve mentions to entities (local → registry fallback)
    - update graph edges
- Must gracefully handle missing transcription or rate limits.

### Task 2.2: `video.youtube` Pipeline

- Integration options:
  - use `yt-dlp` if available
  - or Go native client for metadata + transcript retrieval
- After transcript retrieval:
  - extract mentions
  - resolve mentions
  - populate graph edges
- If no transcript is available:
  - mark job as `NeedsHumanInput` or fail cleanly.

### Task 2.3: Robust Error Handling

- Transient network issues → Retry.
- Permanent errors (unsupported format) → Failed.
- Mention extraction errors:
  - log warning
  - do not block ingestion
- Entity resolution failures:
  - fallback to unresolved local entity placeholders
  - preserve mention text for future resolution

---

## 3. dPKMS Search Team & ctxt Retrieval Team (The Explainer)

**Focus:** Ranking transparency, polished CLI UX, and explicit accounting for semantic identity signals.
**Why:** Mentions and entity graph edges must contribute meaningfully to ranking and must be visible in explainability output.

### Task 3.1: Explainability

- Update reranker to return a structured score breakdown.
- Include:
  - FTS score
  - vector similarity score
  - recency boost
  - entity alignment contribution
  - graph relevance contribution
- Expose via `ctxt list --explain`.

### Task 3.2: Terminal UI Polish

- Replace raw output with a proper table-based renderer.
- Color-code tags and semantic signals.
- Add columns for:
  - mentions (`@ui.best-practice`)
  - resolved entities (human-readable titles)
- Support multi-language display for entities where translations exist.

### Task 3.3: "Did you mean?"

- When search returns 0 results:
  - run vector fallback
  - suggest mention wildcard patterns (`mention:ux.*`)
  - suggest entity aliases
- Provide ranked, semantically meaningful suggestions.

---

## 4. dPKMS Registry Team (The Publisher)

**Focus:** Official registry publication, multilingual output, and entity-aware registry capability negotiation.
**Why:** The semantic identity layer requires reliable, canonical entity definitions.

### Task 4.1: The "ContextHelp Default" Registry

- Publish a default registry with:
  - taxonomy definitions
  - canonical entities
  - aliases and translations
- Provide examples like:
  - `ui.best-practice`
  - `component.form.input`
  - `workflow.youtube.production`
- Add default configuration pointing to this registry.

### Task 4.2: Localization in CLI Output

- Implement `--lang` flag for CLI.
- Entity and mention display:
  - prefer translated titles
  - fallback to slug if untranslated
- Ensure mention slugs remain stable regardless of language.

### Task 4.3: Registry Capability Check

- Add `/capabilities` handshake:
  - indicates whether registry supports entity sync
  - indicates taxonomy/translation features
- Warn users if they subscribe to a registry lacking entity definitions when using mentions.

---

## Integration Check

**Scenario:** Multimedia ingestion with mention extraction and graph updates.

1. Ingest:
   `ctxt analyze --file "vlog_draft.mp3" --hints "#youtube #draft"`
   Result:
   - audio pipeline transcribes
   - text goes to `text.long`
   - mentions extracted (`@content.strategy`, `@youtube.workflow`)
   - entities resolved
   - graph edges created

2. Search:
   `ctxt list --q "video ideas" --explain`
   Result:
   - well-formatted table output
   - explanation shows:
     - vector similarity
     - entity alignment
     - hint contribution
     - recency boost

---

## Risks to Watch For

- **External tool dependencies:**
  Ensure `ffmpeg`, `yt-dlp`, or other tools do not reduce cross-platform portability.

- **Migration safety:**
  Mention, entity, and graph schema evolution must not risk data loss.

- **Cloud inference cost:**
  Add flags to control upload size and model choice.

- **Graph consistency:**
  Validate entity definitions and aliases from registries to avoid cycles or corruption.
---

## See Also

**Package Boundaries:**
- [CROSS-PACKAGE-CONTRACTS.md](CROSS-PACKAGE-CONTRACTS.md) - dPKMS ↔ ctxt integration points
- [../branding.md](../branding.md) - Naming conventions (dPKMS vs ctxt vs ContextHelp)
- [../dpkms-or-ctxt.md](../dpkms-or-ctxt.md) - Package placement guide

**Configuration:**
- [CONFIGURATION-STRUCTURE.md](CONFIGURATION-STRUCTURE.md) - Config file organization
- [../ctxt/configuration.md](../ctxt/configuration.md) - Focus profiles & preferences

**Architecture:**
- [../architecture.md](../architecture.md) - System architecture overview
- [../../ROADMAP.md](../../ROADMAP.md) - Living skeleton roadmap
- [README.md](README.md) - Sprint documentation index
