# Skeleton 8: Trust & Automation Tasks

**Package Focus:** dPKMS (70%) + ctxt (30%)

**Goal:** Security (plugin permissions), automation (watchers), and semantic identity integrity (entity provenance, graph safety).

---

## dPKMS Infrastructure Team (The Sheriff)

### Plugin Manifest and Permission System

- [PLAN] 📦 *dPKMS* Define and enforce plugin manifest specification
  - [ ] Create `manifest.yaml` schema:
    ```yaml
    name: my-plugin
    version: 1.0.0
    type: semantic_augmentor
    permissions:
      - network
      - filesystem
      - clipboard
      - refresh
      - notifications
      - entity.write
      - entity.alias
    capabilities:
      semantic_augmentation: true
      mention_extraction: true
      entity_enrichment: true
    ```
  - [ ] Required fields:
    - [ ] `name`, `version`, `type`
    - [ ] `permissions` array
    - [ ] `capabilities` object
  - [ ] Privileged permissions (require explicit grant):
    - [ ] `entity.write` - create/modify entities
    - [ ] `entity.alias` - define entity aliases
    - [ ] `graph.write` - create graph edges directly
  - [ ] Permission semantics:
    - [ ] `network` - HTTP/HTTPS requests
    - [ ] `filesystem` - read/write files
    - [ ] `clipboard` - access clipboard
    - [ ] `refresh` - trigger background refreshes
    - [ ] `notifications` - send user notifications
  - [ ] Validation:
    - [ ] Parse manifest on plugin discovery
    - [ ] Reject plugins with invalid manifests
    - [ ] Check for permission changes on update

### Permission Enforcement Layer

- [PLAN] 📦 *dPKMS* Implement capability gates
  - [ ] On `dpkms serve` startup:
    - [ ] Load all plugin manifests
    - [ ] Detect permission changes from previous version
    - [ ] Halt execution and request user confirmation if permissions expanded
    - [ ] Display: "Plugin 'example' requests NEW permission: entity.write. Allow? [y/N]"
  - [ ] Runtime enforcement:
    - [ ] Wrap all plugin-facing capabilities
    - [ ] Check permission before allowing action
    - [ ] Return `PermissionDenied` error if unauthorized
  - [ ] Capability wrappers:
    - [ ] `FetchURL(url)` → requires `network` permission
    - [ ] `ReadFile(path)` → requires `filesystem` permission
    - [ ] `WriteFile(path, data)` → requires `filesystem` permission
    - [ ] `WatchClipboard()` → requires `clipboard` permission
    - [ ] `refresh.Trigger()` → requires `refresh` permission
    - [ ] `notifications.Create(msg)` → requires `notifications` permission
    - [ ] `PublishEntity(e)` → requires `entity.write` permission
    - [ ] `UpdateEntity(id, e)` → requires `entity.write` permission
    - [ ] `DefineAlias(from, to)` → requires `entity.alias` permission
  - [ ] Audit log:
    - [ ] Log all permission checks (grant/deny)
    - [ ] Log file: `~/.config/contexthelp/audit.log`
    - [ ] Include: timestamp, plugin, action, permission, result

### Secret Redaction System

- [PLAN] 📦 *dPKMS* Implement comprehensive secret scrubbing
  - [ ] Identify secret patterns:
    - [ ] LLM API keys (OpenAI, Anthropic, etc.)
    - [ ] Embedding API keys
    - [ ] Registry access tokens
    - [ ] HTTP Authorization headers
    - [ ] Database credentials
    - [ ] Plugin-defined secrets
  - [ ] Redaction rules:
    - [ ] Replace with `[REDACTED]` in logs
    - [ ] Never persist secrets to `job_steps` table
    - [ ] Scrub from stderr/stdout
    - [ ] Scrub from plugin-produced logs
  - [ ] Apply to:
    - [ ] `ctxt jobs logs <id>` command output
    - [ ] dPKMS worker logs
    - [ ] Plugin execution logs
    - [ ] Error messages and stack traces
  - [ ] Configuration:
    ```yaml
    logging:
      redact_secrets: true
      custom_patterns:
        - "MY_SECRET_.*"
    ```
  - [ ] Test coverage: ensure secrets never appear in any log

### Entity Integrity Guard

- [PLAN] 📦 *dPKMS* Validate entity updates from registries and plugins
  - [ ] Validation rules when registries/plugins propose updates:
    1. [ ] **Canonical namespace mapping:**
       - [ ] Entity `ui.button` can only come from registry owning `ui.*`
       - [ ] Plugin can only define entities in `plugin.<name>.*`
    2. [ ] **Version increments:**
       - [ ] Must be linear and sequential (v1 → v2 → v3)
       - [ ] Warn on version gaps (v1 → v3 without v2)
       - [ ] Reject version downgrades
    3. [ ] **Required fields:**
       - [ ] `id` (namespace.slug format)
       - [ ] `title` (non-empty string)
       - [ ] `namespace` (matches ID prefix)
       - [ ] `version` (positive integer)
    4. [ ] **Namespace ownership:**
       - [ ] No entity may overwrite namespace not owned by source
       - [ ] Local entities always in `local.*` namespace
       - [ ] Registry entities in registry-declared namespace
       - [ ] Plugin entities in `plugin.<plugin-name>.*` namespace
  - [ ] Rejection behavior:
    - [ ] Log rejected update with provenance
    - [ ] Include: timestamp, source, entity ID, reason
    - [ ] Notify user if interactive mode
    - [ ] Continue without applying update
  - [ ] Audit trail:
    - [ ] Store in `entity_audit` table
    - [ ] Fields: timestamp, entity_id, action, source, result, reason

### Graph Safety Validator

- [PLAN] 📦 *dPKMS* Protect knowledge graph from corruption
  - [ ] Validation before linking or updating graph edges:
    1. [ ] **Mention syntax validation:**
       - [ ] Must match `@namespace.slug` pattern
       - [ ] Namespace and slug: lowercase, alphanumeric, hyphens only
       - [ ] No special characters or spaces
    2. [ ] **Namespace spoofing prevention:**
       - [ ] Reject mentions to reserved namespaces (system.*, core.*)
       - [ ] Reject mentions with suspicious patterns
       - [ ] Validate against registry namespace ownership
    3. [ ] **Backlink validation:**
       - [ ] Target entity must exist (or be valid placeholder)
       - [ ] Source knowledge object must exist
       - [ ] No circular references (A→B→A)
    4. [ ] **Canonical slug formatting:**
       - [ ] Enforce lowercase conversion
       - [ ] Validate against registry rules
       - [ ] Normalize Unicode to ASCII
  - [ ] Error handling:
    - [ ] Log validation failures
    - [ ] Don't create invalid edges
    - [ ] Mark knowledge object for manual review
  - [ ] Performance:
    - [ ] Cache validated entities (avoid repeated lookups)
    - [ ] Batch validation for bulk operations
    - [ ] <1ms per edge validation

---

## ctxt Ingestion Team (The Watcher)

### Watcher Interface Definition

- [PLAN] 📦 *ctxt* Define plugin-extensible watcher abstraction
  - [ ] Create `Watcher` interface:
    ```go
    type Watcher interface {
        Name() string
        Start(ctx context.Context) error
        Stop() error
    }
    ```
  - [ ] Watcher behavior:
    - [ ] Emit raw content into dPKMS job queue
    - [ ] Annotate jobs with `origin=watcher` metadata
    - [ ] Include source information (clipboard, file path, etc.)
  - [ ] Prevent re-ingestion loops:
    - [ ] Hash content before enqueueing
    - [ ] Check if hash already processed (dedup table)
    - [ ] Skip if recently ingested (<1 hour)
  - [ ] Allow watchers provided by plugins:
    - [ ] Plugin manifests declare watcher capability
    - [ ] Plugin implements Watcher interface
    - [ ] dPKMS loads and manages watcher lifecycle
  - [ ] Configuration:
    ```yaml
    watchers:
      enabled: true
      dedup_window: 3600  # seconds
    ```

### Clipboard Watcher Implementation

- [PLAN] 📦 *ctxt* Reference implementation of watcher
  - [ ] Poll clipboard contents:
    - [ ] Platform-specific clipboard access (macOS, Linux, Windows)
    - [ ] Poll interval: 500ms (configurable)
  - [ ] Detect changes:
    - [ ] Hash current clipboard content
    - [ ] Compare with previous hash
    - [ ] Trigger on new content only
  - [ ] Ingestion heuristics:
    - [ ] URL detected → enqueue `url.generic` pipeline
    - [ ] Code block detected → enqueue `text.short` with hint
    - [ ] Plain text (>100 chars) → enqueue `text.short`
    - [ ] Image data → enqueue `image.ocr`
  - [ ] Configuration:
    ```yaml
    watchers:
      clipboard:
        enabled: true
        auto_ingest: false  # if false, prompt user
        poll_interval: 500  # ms
        min_content_length: 50
    ```
  - [ ] User prompts (if `auto_ingest: false`):
    - [ ] Display: "New clipboard content detected. Ingest? [y/N]"
    - [ ] Include preview (first 100 chars)
  - [ ] Pass raw content into pipelines for mention extraction

### Directory Watcher Implementation

- [PLAN] 📦 *ctxt* Filesystem watcher for drop folders
  - [ ] Monitor configured directory using `fsnotify`:
    ```yaml
    watchers:
      directory:
        enabled: true
        path: ~/Documents/ContextHelp/
        recursive: false
    ```
  - [ ] Detect file events:
    - [ ] CREATE - new file added
    - [ ] MODIFY - file updated (debounce to avoid multiple triggers)
  - [ ] File type routing:
    - [ ] `.pdf` → PDF extraction pipeline
    - [ ] `.md`, `.txt` → `text.long` pipeline
    - [ ] `.html` → `url.generic` pipeline (local file)
    - [ ] `.png`, `.jpg` → `image.ocr` pipeline
    - [ ] `.mp3`, `.wav` → `audio.transcript` pipeline
  - [ ] Ingestion workflow:
    1. [ ] File detected
    2. [ ] Wait for file write completion (check size stability)
    3. [ ] Enqueue appropriate pipeline job
    4. [ ] Include full pipeline logic:
       - [ ] OCR if needed
       - [ ] Mention extraction
       - [ ] Entity resolution
  - [ ] Error handling:
    - [ ] Permissions errors (can't read file)
    - [ ] Unsupported formats (log and skip)
    - [ ] File deleted before processing (graceful failure)

### Semantic Watcher Hooks

- [ ] 📦 *ctxt* Allow lightweight pre-semantic checks in watchers
  - [ ] Detect inline mentions in filenames:
    - [ ] `design-pattern-@strategy.md` → early-guess mention: `@strategy`
    - [ ] Attach as job metadata hint
  - [ ] Detect inline mentions in file metadata:
    - [ ] EXIF tags, PDF metadata, audio tags
    - [ ] Extract and attach as hints
  - [ ] Use for optimization only:
    - [ ] Pipelines remain authoritative source
    - [ ] Early guesses don't override pipeline extraction
    - [ ] Validation still happens in pipeline
  - [ ] Log early-detected mentions:
    - [ ] "Watcher detected potential mention: @ui.best-practice"
    - [ ] Compare with pipeline results for accuracy metrics

---

## dPKMS Search Team & ctxt Retrieval Team (The Graph)

### Pipeline Mention and Entity Integration

- [PLAN] 📦 *ctxt + dPKMS* Ensure every pipeline performs semantic extraction
  - [ ] Pipelines with text output must:
    1. [ ] Extract mentions using `@namespace.slug` rules
    2. [ ] Resolve mentions to entities:
       - [ ] Local entity registry
       - [ ] Synced external registries
       - [ ] Plugin-defined entities
       - [ ] Create placeholder if unresolved
    3. [ ] Store in knowledge object:
       - [ ] `mentions[]` field (canonical entity IDs)
       - [ ] Entity metadata (optional denormalized cache)
    4. [ ] Create backlinks in graph:
       - [ ] Insert into `entity_backlinks` table
       - [ ] Handle duplicates gracefully (UPSERT)
    5. [ ] Store provenance:
       - [ ] Which pipeline detected mention
       - [ ] Resolution source (local, registry, placeholder)
       - [ ] Timestamp and version
  - [ ] Apply to all modalities:
    - [ ] Text (plain, markdown, HTML)
    - [ ] OCR output
    - [ ] Transcripts (audio, video)
    - [ ] Structured data (JSON, YAML)
  - [ ] Validation:
    - [ ] Mention syntax checked
    - [ ] Entity exists or valid placeholder created
    - [ ] Backlinks referential integrity

### `related:` Query Operator

- [PLAN] 📦 *dPKMS* Implement graph-based related content
  - [ ] Operator syntax:
    ```
    related:<bookmark-id>
    ```
  - [ ] Execution strategy:
    1. [ ] Identify entities referenced by target bookmark:
       - [ ] Query `entity_backlinks` WHERE `knowledge_object_id = <id>`
       - [ ] Get list of `entity_id`s
    2. [ ] Find other bookmarks with overlapping entities:
       - [ ] Query `entity_backlinks` WHERE `entity_id IN (...)`
       - [ ] Exclude original bookmark
    3. [ ] Rank by shared-entity weight:
       - [ ] More entities in common → higher score
       - [ ] Registry-weighted entities count more
       - [ ] Recent bookmarks boosted slightly
  - [ ] Configurable depth:
    - [ ] `related:<id>:1` - direct entity overlap only
    - [ ] `related:<id>:2` - include 2-hop neighbors
  - [ ] Performance:
    - [ ] Use covering indexes
    - [ ] Limit to top-N results (default: 20)
    - [ ] Cache results for popular bookmarks

### "See Also" in CLI Output

- [ ] 📦 *ctxt* Enhance `ctxt show <id>` command
  - [ ] Display sections:
    1. [ ] **Knowledge Object Details:**
       - [ ] ID, title, created_at, updated_at
       - [ ] Raw content preview
       - [ ] Tags and hints
    2. [ ] **Entities Referenced:**
       ```
       Entities:
       - @ui.best-practice (UI Best Practices)
       - @design.pattern.strategy (Strategy Pattern)
       ```
       - [ ] Show mention syntax and human title
       - [ ] Include provenance (registry source)
    3. [ ] **Related Knowledge Objects:**
       ```
       See Also (via shared entities):
       1. abc123 - "Design System Guidelines" (2 shared entities)
       2. def456 - "Component Architecture" (1 shared entity)
       ```
       - [ ] Top 5 related objects
       - [ ] Show shared entity count
       - [ ] Link to details: `ctxt show abc123`
    4. [ ] **Entity Provenance:**
       ```
       Provenance:
       - @ui.best-practice: contexthelp-official (v2.3, synced 2026-01-20)
       - @design.pattern.strategy: local.unresolved (placeholder)
       ```
       - [ ] Source registry or local definition
       - [ ] Version and sync date
       - [ ] Warn if placeholder or stale

### Entity-Aware Query Filters

- [PLAN] 📦 *dPKMS* Extend query language for entity operations
  - [ ] Supported filters:
    - [ ] `mention:ui.best-practice` - exact entity match
    - [ ] `mention:stripe.api.*` - namespace wildcard
    - [ ] `mention:*` - any mention present
    - [ ] `entity:frontend.react` - entity-aware filter (via ID)
    - [ ] Wildcard entity namespace: `entity:frontend.*`
  - [ ] Mention-to-entity resolution during query parsing:
    - [ ] User query: `mention:ui.button`
    - [ ] Resolve `ui.button` to canonical entity ID
    - [ ] Generate SQL filtering on resolved ID
  - [ ] Combine with other filters:
    ```
    mention:ui.* AND tag:design AND created:>2026-01-01
    ```
  - [ ] Performance:
    - [ ] Use entity index for fast lookups
    - [ ] Apply mention filters early (reduce result set)
    - [ ] Avoid full table scans

### Graph Integrity Corridor

- [PLAN] 📦 *dPKMS* Validate edges before insertion
  - [ ] Validation gates:
    1. [ ] **Namespace validation:**
       - [ ] Entity defined in namespace it claims
       - [ ] No spoofing of system/reserved namespaces
    2. [ ] **Entity existence:**
       - [ ] Target entity exists in `entities` table OR
       - [ ] Valid placeholder pattern (`local.unresolved.*`)
    3. [ ] **Referential integrity:**
       - [ ] Knowledge object exists
       - [ ] Entity ID format valid
       - [ ] No circular references
    4. [ ] **Plugin safety:**
       - [ ] Plugins cannot inject edges to entities outside their namespace
       - [ ] Plugin-created edges stored with provenance
       - [ ] Malformed edges rejected with error log
  - [ ] Edge metadata:
    - [ ] `created_by` (pipeline or plugin name)
    - [ ] `created_at` (timestamp)
    - [ ] `confidence` (0.0-1.0, based on extraction method)
  - [ ] Audit:
    - [ ] Log rejected edges
    - [ ] Include: entity_id, object_id, reason
    - [ ] Alert on repeated failures (possible attack)

---

## dPKMS Registry Team (The Librarian)

### Registry Index (Reference Registry)

- [PLAN] 📦 *dPKMS* Create root discovery registry
  - [ ] Define registry index format:
    ```json
    {
      "version": "1.0",
      "registries": [
        {
          "name": "contexthelp-official",
          "url": "https://registry.contexthelp.ai/v1/",
          "namespaces": ["ui", "design", "workflow"],
          "trust_level": "official",
          "entity_count": 1234,
          "last_updated": "2026-01-26T12:00:00Z"
        }
      ]
    }
    ```
  - [ ] Metadata per registry:
    - [ ] Name and URL
    - [ ] Namespaces provided
    - [ ] Trust level (official, community, experimental)
    - [ ] Entity count
    - [ ] Version timestamp
  - [ ] Purpose:
    - [ ] Discovery (not automatic subscription)
    - [ ] User browses and manually adds registries
  - [ ] Publish at: `https://registry.contexthelp.ai/index.json`

### Registry Search Command

- [ ] 📦 *ctxt* Implement `ctxt registry search <topic>` command
  - [ ] Discover registries by:
    - [ ] Namespace prefix (`ctxt registry search ui`)
    - [ ] Tags/keywords (`ctxt registry search "design patterns"`)
    - [ ] Subject matter (`ctxt registry search kubernetes`)
  - [ ] Fetch from registry index
  - [ ] Display results:
    ```
    Found 3 registries matching "ui":
    1. contexthelp-official
       Namespaces: ui, design, workflow
       Entities: 1234
       Trust: official
       Add: ctxt registry add contexthelp-official

    2. ui-patterns-community
       Namespaces: ui.patterns
       Entities: 567
       Trust: community
       Add: ctxt registry add ui-patterns-community https://...
    ```
  - [ ] Filter by trust level:
    - [ ] `--trust official` - only official registries
    - [ ] `--trust community` - include community
    - [ ] `--trust all` - include experimental

### Registry Trust Flags

- [PLAN] 📦 *dPKMS* Implement per-registry trust levels
  - [ ] Configuration:
    ```yaml
    registries:
      - name: contexthelp-official
        url: https://registry.contexthelp.ai/v1/
        trusted: true
      - name: experimental-registry
        url: https://example.com/registry/
        trusted: false
    ```
  - [ ] Trusted registry behavior:
    - [ ] May define entities and aliases
    - [ ] May propose backlinks
    - [ ] Weights applied in ranking
    - [ ] Used for entity resolution
  - [ ] Untrusted registry behavior:
    - [ ] May be used for lookup only (read-only)
    - [ ] Cannot define aliases (safety)
    - [ ] Cannot override local entities
    - [ ] Weights ignored or severely limited
  - [ ] Trust verification:
    - [ ] Optional: HTTPS certificate validation
    - [ ] Optional: GPG signature verification
    - [ ] User must explicitly grant trust

### Entity Provenance Tracking

- [PLAN] 📦 *dPKMS* Store and expose entity lineage
  - [ ] Provenance fields in `entities` table:
    - [ ] `origin_registry` (TEXT: registry name or "local")
    - [ ] `namespace` (TEXT: entity namespace)
    - [ ] `version` (INTEGER: entity version)
    - [ ] `trust_level` (TEXT: "official", "community", "local")
    - [ ] `synced_at` (TIMESTAMP: last sync time)
    - [ ] `redirect_history` (JSONB: alias chain)
  - [ ] Track for every entity:
    - [ ] Source registry URL
    - [ ] Namespace ownership
    - [ ] Version number
    - [ ] Trust level
    - [ ] Alias history (if entity was renamed)
  - [ ] Expose via `ctxt show entity <slug>` command:
    ```
    Entity: ui.best-practice
    Title: UI Best Practices
    Namespace: ui
    Source: contexthelp-official (https://registry.contexthelp.ai/v1/)
    Trust Level: official
    Version: 2.3
    Synced: 2026-01-20 14:30:00
    Aliases: ui.ux-patterns, design.best-practice
    Backlinks: 142 knowledge objects
    ```
  - [ ] Warn on provenance issues:
    - [ ] Stale sync (>30 days)
    - [ ] Placeholder entity (unresolved)
    - [ ] Conflicting definitions across registries

---

## Integration Check (Automated Researcher Demo)

### Setup

1. [ ] Enable clipboard watching:
   ```yaml
   watchers:
     clipboard:
       enabled: true
       auto_ingest: false  # prompt user
   ```

2. [ ] Install "PDF Summarizer" plugin
3. [ ] Plugin requests permissions:
   - [ ] `filesystem` - READ
   - [ ] `entity.write` - WRITE

4. [ ] User approves `filesystem`, denies `entity.write`:
   ```
   Plugin 'pdf-summarizer' requests permissions:
   - filesystem: allow? [y/N] y
   - entity.write: allow? [y/N] n

   Note: Plugin will not be able to create custom entities.
   ```

### Scenario: Automated Ingestion

1. [ ] User copies URL to clipboard:
   ```
   https://react.dev/blog/2026/01/server-components
   ```

2. [ ] Clipboard watcher detects new content:
   ```
   New content detected: https://react.dev/blog/2026/01/server-components
   Ingest? [y/N] y
   ```

3. [ ] Verify:
   - [ ] Job enqueued: `url.generic` pipeline
   - [ ] Pipeline extracts mentions: `@react.server-components`
   - [ ] Resolver matches entity to trusted registry: `contexthelp-official`
   - [ ] Entity resolved with provenance
   - [ ] Graph updated (backlink created)
   - [ ] Job completes successfully

4. [ ] User runs:
   ```bash
   ctxt show <new-bookmark-id>
   ```

5. [ ] Verify output includes:
   - [ ] **Extracted mentions:** `@react.server-components`
   - [ ] **Resolved entities with provenance:**
     ```
     Provenance:
     - @react.server-components: contexthelp-official (v3.1, synced today)
     ```
   - [ ] **"See Also" powered by shared entities:**
     ```
     See Also:
     1. abc123 - "Next.js 15 Features" (1 shared entity)
     2. def456 - "React Suspense Guide" (1 shared entity)
     ```

### Validation Checklist

**dPKMS Validation:**
- [ ] Plugin manifest validated on load
- [ ] Permission enforcement blocks unauthorized actions
- [ ] Secret redaction prevents API key leaks
- [ ] Entity integrity guard rejects invalid updates
- [ ] Graph safety validator blocks malformed edges
- [ ] Registry trust levels enforced
- [ ] Entity provenance tracked and displayed

**ctxt Validation:**
- [ ] Watcher interface functional
- [ ] Clipboard watcher detects content changes
- [ ] Directory watcher monitors file drops
- [ ] Semantic watcher hooks provide early mention hints
- [ ] `related:` operator returns shared-entity results
- [ ] `ctxt show <id>` displays entities and provenance
- [ ] `ctxt show entity <slug>` shows full lineage

**Cross-Package Validation:**
- [ ] Watchers enqueue jobs to dPKMS
- [ ] Pipelines extract mentions and resolve entities
- [ ] Graph queries use backlinks table
- [ ] Provenance information flows end-to-end
- [ ] Security boundaries enforced across packages

---

## Risks to Watch For

### Clipboard Loop
- Automation may re-ingest content that ContextHelp itself writes
- **Mitigation:** Store hash of last clipboard value, skip if match

### Entity Explosion
- LLM noise may generate excessive placeholder entities
- **Mitigation:** Namespaced validators, stop-lists, frequency thresholds

### Namespace Spoofing
- Malicious plugins may attempt to define entities in reserved namespaces
- **Mitigation:** Enforce namespace ownership and provenance constraints

### OS Permission Requirements
- Clipboard and directory watchers may require OS-level permissions
- **Mitigation:** Document per-platform setup, fallback modes

### Graph Poisoning
- Malicious content may flood graph with bogus or adversarial links
- **Mitigation:** Entity validator, edge gating, alias verification, permission-controlled mutation

---

## See Also

- Sprint spec: `docs/sprints/008-trust-and-automation.md`
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
- Package placement: `docs/dpkms-or-ctxt.md`
