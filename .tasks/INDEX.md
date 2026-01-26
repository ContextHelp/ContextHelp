# Task Index - Quick Reference

This index provides a quick lookup of all tasks across the Living Skeleton sprints.

---

## Sprint Status Overview

| Sprint | Name | Package Focus | Status |
|--------|------|---------------|--------|
| 0 | Shared Kernel | dPKMS (100%) | [000-shared-kernel.md](000-shared-kernel.md) |
| 1 | Echo Loop | dPKMS (70%) + ctxt (30%) | [001-echo-loop.md](001-echo-loop.md) |
| 2 | Real Data & Queries | Balanced (50/50) | [002-real-data-queries.md](002-real-data-queries.md) |
| 3 | Beyond Text | Balanced (50/50) | [003-beyond-text.md](003-beyond-text.md) |
| 4 | Interfaces & Independence | dPKMS (60%) + ctxt (40%) | [004-interfaces-independence.md](004-interfaces-independence.md) |
| 5 | Semantics & Sidecars | Balanced (50/50) | [005-semantics-sidecars.md](005-semantics-sidecars.md) |
| 6 | Polish & Multimedia | dPKMS (60%) + ctxt (40%) | [006-polish-multimedia.md](006-polish-multimedia.md) |
| 7 | Sovereign & Scalable | Balanced (50/50) | [007-sovereign-scalable.md](007-sovereign-scalable.md) |
| 8 | Trust & Automation | dPKMS (70%) + ctxt (30%) | [008-trust-automation.md](008-trust-automation.md) |

---

## Key Features by Sprint

### Skeleton 0: Foundation
- SQL schema with entities, mentions, backlinks
- Go domain model (KnowledgeObject, Entity, Job, Tag)
- Interface definitions (Storage, PipelineRunner, RegistryClient)

### Skeleton 1: First Integration
- dPKMS bootloader and job queue
- Echo pipeline (ctxt)
- `ctxt list`, `ctxt open`, `ctxt jobs` commands
- Empty `mentions: []` field established

### Skeleton 2: Intelligence & Queries
- LLM client interfaces (OpenAI adapters)
- `text.short` pipeline with real enrichment
- RSQL parser with `mention:` operator support
- Registry HTTP client and caching

### Skeleton 3: Semantic Identity
- FTS5 full-text search
- URL and image pipelines (multimodal)
- Mention extraction + entity resolution
- Knowledge graph (backlinks)
- Zombie job recovery

### Skeleton 4: APIs & Agents
- REST and gRPC servers
- Agent profiles (scoped worldviews)
- Offline registry sync
- Mention-aware query endpoints

### Skeleton 5: Vector Search & Plugins
- Embedding generation (OpenAI, Ollama)
- Vector storage and similarity search
- Plugin system (semantic augmentors)
- RRF hybrid ranking (FTS + vector + mentions)
- Entity weight application

### Skeleton 6: Production Ready
- Database migrations system
- Backup/restore commands
- Audio/video transcription pipelines
- Ranking explainability
- Terminal UI polish
- "Did you mean?" suggestions
- Default registry publication

### Skeleton 7: Offline & Scale
- Ollama integration (local AI)
- Vector optimization (sqlite-vec/HNSW)
- Strict offline mode
- Performance profiling
- Registry linter
- Plugin scaffolding
- Multi-registry reconciliation

### Skeleton 8: Security & Automation
- Plugin manifest and permissions
- Secret redaction
- Entity integrity guard
- Graph safety validator
- Clipboard and directory watchers
- `related:` query operator
- Entity provenance tracking
- Registry trust levels

---

## Search by Component

### Storage & Database
- **Schema:** 000, 003, 005, 006
- **Migrations:** 006
- **Backup/Restore:** 006
- **Performance:** 007

### Semantic Identity
- **Mentions:** 001, 002, 003, 004, 005, 006, 007, 008
- **Entities:** 000, 003, 004, 005, 006, 007, 008
- **Graph (Backlinks):** 000, 003, 004, 005, 006, 007, 008
- **Provenance:** 006, 008

### Pipelines
- **Text:** 001, 002
- **URL:** 003
- **Image/OCR:** 003
- **Audio:** 006
- **Video:** 006

### Search & Query
- **RSQL Parser:** 002, 003
- **FTS5:** 003
- **Vector Search:** 005, 007
- **RRF Ranking:** 005, 006
- **Explainability:** 006

### APIs & Interfaces
- **REST:** 004
- **gRPC:** 004
- **CLI Commands:** 001, 002, 003, 004, 006

### AI Integration
- **LLM Clients:** 002, 007
- **Embeddings:** 005, 007
- **Ollama:** 007
- **Offline AI:** 007

### Registries
- **Local Loading:** 001
- **HTTP Client:** 002
- **Caching:** 002, 004
- **Sync:** 004
- **Entity Sync:** 005
- **Trust Levels:** 008
- **Discovery:** 008

### Plugins
- **Interface Definitions:** 005
- **Loader:** 005
- **Permissions:** 008
- **Semantic Augmentation:** 005, 008
- **Scaffolding:** 007
- **Validation:** 007

### Security
- **Permissions:** 008
- **Secret Redaction:** 008
- **Entity Integrity:** 008
- **Graph Safety:** 008
- **Trust Levels:** 008

### Automation
- **Watchers:** 008
- **Clipboard:** 008
- **Directory:** 008
- **Background Jobs:** 001, 003

---

## Task Status Legend

Tasks are marked with prefixes:
- `[PLAN]` - Needs breakdown into smaller, actionable subtasks
- `[ ]` - Actionable task, not started
- No tasks are marked as complete in this initial breakdown

---

## Related Documentation

- **Sprint Specifications:** `docs/sprints/`
- **High-Level Roadmap:** `ROADMAP.md`
- **Cross-Package Contracts:** `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
- **Package Placement Guide:** `docs/dpkms-or-ctxt.md`
- **Architecture:** `docs/architecture.md`
- **Branding/Terminology:** `docs/branding.md`

---

## How to Use This Index

1. **Find your sprint:** Look up the sprint number (0-8)
2. **Read the sprint file:** Each sprint has detailed tasks organized by team
3. **Identify [PLAN] items:** These need further breakdown before implementation
4. **Check dependencies:** Review "Integration Check" sections for cross-package dependencies
5. **Track progress:** Update checkboxes as tasks are completed

---

Generated: 2026-01-26
Version: 1.0
