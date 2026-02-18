# ctxt Domain Index

This document lists all domains within the **`ctxt`** (Context Brain) package, organized by responsibility.

**Last Updated:** 2026-01-26

---

## About ctxt

**`ctxt`** is the brain layer providing meaningful behaviors for knowledge management:
- Capture workflows and ingestion
- AI-driven enrichment and tagging
- Context surfacing and composition
- User interfaces (CLI, TUI, Web UI)
- Configuration and personalization

Together with **dPKMS** (the substrate layer), they provide **context-as-a-service** for humans and AI agents.

---

# ctxt Domains

## 1. Capture & Ingestion Domain
**Location:** `docs/ctxt/pipelines.md`, `docs/ctxt/pipelines-reference.md`

**Responsibilities:**
- Multi-source capture workflows (URLs, files, GitHub PRs, tweets)
- Content extraction and normalization
- Multimodal content handling (text, images, audio, video)
- Source-specific adapters and scrapers
- Raw content preservation
- Ingestion validation and sanitization

**Capture Sources:**
- Web pages and articles
- GitHub repositories, issues, PRs
- Local files and directories
- Social media (tweets, threads)
- PDFs and documents
- Audio/video transcripts

---

## 2. Enrichment Pipeline Domain
**Location:** `docs/ctxt/pipelines.md`, `docs/ctxt/pipelines-reference.md`

**Responsibilities:**
- AI-powered content analysis
- Summarization and key point extraction
- Automatic tagging and classification
- Entity extraction and linking
- Sentiment and tone analysis
- Topic modeling and clustering
- Pipeline step orchestration
- Configurable enrichment strategies

**Enrichment Steps:**
- Summarization (extractive, abstractive)
- Tag generation (automatic, guided)
- Entity recognition (people, places, concepts)
- Taxonomy classification
- Mention extraction and resolution
- Metadata augmentation

---

## 3. Tags Domain
**Location:** `docs/ctxt/tags.md`, `docs/ctxt/schema-tag.md`

**Responsibilities:**
- Tag vocabulary management
- Hierarchical tag relationships
- Tag validation and normalization
- Tag suggestions and auto-completion
- Tag usage analytics
- Tag merging and aliasing
- Multi-language tag support

**Tag Features:**
- Flat or hierarchical structures
- Registry-backed vocabularies
- Custom user tags
- Automatic tag inference
- Tag confidence scores

---

## 4. Taxonomy Domain
**Location:** `docs/ctxt/schema-taxonomy.md`

**Responsibilities:**
- Hierarchical classification systems
- Category tree management
- Multi-level taxonomies
- Taxonomy navigation and browsing
- Category assignment rules
- Taxonomy versioning
- Cross-taxonomy mapping

**Taxonomy Features:**
- Pluggable taxonomy sources
- Registry-provided taxonomies
- Custom user taxonomies
- Automatic classification
- Multi-taxonomy support

---

## 5. CLI Domain
**Location:** `docs/ctxt/api-cli.md`, `docs/ctxt/cli-flag-binding.md`, `docs/cli-implementation.md`

**Responsibilities:**
- Command-line interface implementation
- Command routing and execution
- Flag parsing and validation
- Environment variable binding
- Configuration override hierarchy
- Interactive prompts and wizards
- Output formatting (JSON, table, YAML)
- Error handling and help text

**CLI Commands:**
- `ctxt capture` — Ingest content
- `ctxt search` — Query knowledge
- `ctxt tag` — Manage tags
- `ctxt config` — Configuration management
- `ctxt sync` — Registry operations
- `ctxt export` — Data export

---

## 6. TUI Domain
**Location:** `docs/ctxt/tui.md`

**Responsibilities:**
- Terminal user interface rendering
- Interactive browsing and navigation
- Keyboard shortcuts and commands
- Real-time search and filtering
- Multi-pane layouts
- Context-aware help
- Accessibility support

**TUI Features:**
- Split-pane browser
- Live search results
- Tag and entity navigation
- Knowledge graph visualization
- Quick actions and commands
- Customizable themes

---

## 7. Web UI Domain
**Location:** `docs/ctxt/webui.md`

**Responsibilities:**
- Web-based user interface
- Responsive design
- Real-time collaboration features
- Visual knowledge graph explorer
- Rich media preview
- Sharing and publishing workflows
- REST API backend

**Web UI Features:**
- Dashboard and analytics
- Visual graph explorer
- Rich text editor
- Media gallery
- Export and sharing
- User management

---

## 8. Configuration Domain
**Location:** `docs/ctxt/configuration.md`, `docs/configuration-structure.md`

**Responsibilities:**
- Configuration file management (YAML, TOML, JSON)
- Environment variable processing
- Configuration validation and schema
- Default value resolution
- Workspace-specific settings
- User preference storage
- Configuration migration

**Configuration Layers:**
- System defaults
- Global config (`~/.config/ctxt/`)
- Workspace config (`.ctxt/config.yaml`)
- Environment variables
- CLI flags (highest priority)

---

## 9. Hints Domain
**Location:** `docs/ctxt/hints.md`

**Responsibilities:**
- Just-in-time context surfacing
- Proactive suggestion system
- Context-aware recommendations
- Intelligent nudges and reminders
- Related content discovery
- Workflow optimization hints
- Learning from user behavior

**Hint Types:**
- Related knowledge suggestions
- Tag recommendations
- Entity disambiguation
- Query refinement suggestions
- Workflow shortcuts
- Best practice reminders

---

## 10. L10n/i18n Domain
**Location:** `docs/ctxt/l10n-i18n.md`

**Responsibilities:**
- Multi-language support
- String localization
- Translation management
- Locale-aware formatting
- RTL language support
- Pluralization rules
- Date/time/number formatting

**Supported Languages:**
- English (default)
- Extensible translation system
- Community-contributed translations

---

## 11. Schema (Object) Domain
**Location:** `docs/ctxt/schema-object.md`

**Responsibilities:**
- Knowledge object schema definition
- Validation rules and constraints
- Field types and formats
- Schema evolution and versioning
- Custom field support
- Schema documentation

**Core Object Fields:**
```
id (UUID)
url (optional)
title, content
tags (array)
mentions (array)
summary
source_type
metadata (JSON)
created_at, updated_at
```

---

## 12. User Roles & Personas Domain
**Location:** `docs/ctxt/user-roles.md`, `docs/ctxt/personas-user-roles.md`, `docs/ctxt/personas-end-users.md`

**Responsibilities:**
- User role definitions
- Permission models
- Persona-based UX adaptation
- Workspace access control
- Collaborative roles (owner, editor, viewer)
- Focus profiles per role

**User Roles:**
- Individual knowledge worker
- Team member
- Organization admin
- Plugin developer
- Registry maintainer

---

## 13. User Stories Domain
**Location:** `docs/ctxt/user-stories.md`, `docs/ctxt/user-story.md`, `docs/ctxt/user-story-github-pr-insights.md`

**Responsibilities:**
- Requirements capture via user stories
- Use case documentation
- Workflow narratives
- Acceptance criteria
- User journey mapping
- Feature validation

**Example User Stories:**
- GitHub PR insights
- Research paper organization
- Meeting notes capture
- Code snippet bookmarking
- Learning path tracking

---

## 14. Interface Mappings Domain
**Location:** `docs/ctxt/interface-mappings.md`

**Responsibilities:**
- Mapping between dPKMS interfaces and ctxt implementations
- Adapter pattern implementations
- Interface contracts
- Type conversions
- Error translation
- Cross-package communication

**Key Mappings:**
- ObjectStore ↔ Storage layer
- Pipeline ↔ Enrichment engine
- QueryEngine ↔ Search interface
- GraphStore ↔ Entity browser

---

## 15. Testing Domain (ctxt)
**Location:** `docs/ctxt/testing.md`

**Responsibilities:**
- UI testing strategies
- Integration tests with dPKMS
- End-to-end workflow tests
- Mock AI provider tests
- CLI command tests
- TUI interaction tests
- API endpoint tests

**Test Coverage:**
- Unit tests (domain logic)
- Integration tests (CLI, API)
- E2E tests (full workflows)
- Performance tests
- Accessibility tests

---

## 16. API Domain
**Location:** `docs/api/`

**Responsibilities:**
- REST API design and implementation
- GraphQL query support (future)
- API authentication and authorization
- Rate limiting and quotas
- OpenAPI/Swagger documentation
- Webhook support
- API versioning

**API Endpoints:**
- `/api/v1/objects` — Knowledge objects CRUD
- `/api/v1/search` — Search and query
- `/api/v1/tags` — Tag management
- `/api/v1/entities` — Entity operations
- `/api/v1/graph` — Graph queries

---

## 17. Plugin Integration Domain
**Location:** `docs/plugins/`, `docs/plugins/plugins.md`, `docs/plugins/plugin-isolation.md`

**Responsibilities:**
- Plugin discovery and loading
- Plugin lifecycle management
- Capability-based security
- Plugin API contracts
- Custom pipeline steps
- Custom UI components
- Event system and hooks

**Plugin Types:**
- Capture plugins (new sources)
- Enrichment plugins (AI steps)
- Storage plugins (backends)
- UI plugins (components)
- Notification plugins
- Integration plugins

---

# Total: 17 ctxt Domains

For the complete system architecture including dPKMS domains and cross-cutting concerns, see:
- **dPKMS domains:** `docs/dpkms/domains.md`
- **Complete index:** `docs/domains.md`
