# Persona: Maintainers, Collaborators, Contributors

**Primary Role:** Project leadership, core team, extended contributors

---

## Goals

- Ensure system reliability, correctness, and extensibility
- Guide architectural decisions and plugin ecosystem growth
- Balance local-first sovereignty with federation and integration capabilities
- Maintain comprehensive documentation, schema contracts, and upgrade paths
- Foster a healthy contributor ecosystem

---

## Interaction Pattern

### Configuration & Schema Design
- Edit `configuration.md` to formalize polymorphic config system
- Design storage backends, AI providers, and registry protocols
- Maintain schema stability while supporting versioned migrations

### Plugin Ecosystem
- Define plugin interface contracts (`plugins.md`)
- Approve new plugin types (pipeline extensions, storage adapters, AI providers, query operators)
- Set permission boundaries (subprocess, network, filesystem, clipboard access)
- Ensure plugins follow capability scoping rules

### Pipeline Architecture
- Design enrichment recipes (`pipelines.md`) for each content type
- Define AI-backed steps (tag assignment, entity extraction, decision extraction)
- Balance progressive enrichment with deterministic guarantees
- Maintain domain-specific scraper rule coverage for high-value sites without hardcoding every fix

### Query Language Evolution
- Extend RSQL operators and AST node types (`query-language-spec.md`)
- Define intent patterns for NLQ normalizer
- Maintain registry weighting and conflict resolution rules

### API Contracts
- Maintain CLI commands (`api-cli.md`), REST endpoints (`api-rest.md`), gRPC services (`api-grpc.md`)
- Define `/query-schema`, `/search`, `/analyze`, `/objects` contracts for agents
- Document breaking changes in changelog + migration guide

### Storage & Migrations
- Design knowledge object schema evolution
- Manage reversible SQL migrations
- Maintain stable ID preservation across exports

---

## Key Pain Points

- **Documentation Drift:** Interfaces evolve faster than docs; keeping examples synchronized is error-prone
- **Breaking Changes:** Changes to core schema/API impact plugins, agents, and production instances
- **Flexibility vs. Stability Tradeoff:** "Pluggable everything" makes testing and debugging harder
- **Distributed Coordination:** Federated registries, plugin versions, and schema compatibility across deployments
- **User Guidance:** New contributors struggle to understand dual-path query architecture and enrichment pipeline layering

---

## System Leverage

### Deterministic Replay
- Job state tracking enables debugging of failed enrichments
- Same input + same config → reproducible job output
- Facilitates testing, auditing, and blame-free postmortems

### Explicit Permission Model
- Plugins declare capabilities (subprocess, network, filesystem, screen capture)
- Reduces attack surface and makes security assumptions transparent
- Eases code review and deployment approval

### Federated Registry Protocol
- Scatter-gather queries distribute load across multiple sources
- Conflict resolution rules (namespacing, canonical preference, user override) prevent data chaos
- Registry weighting and domain specificity scoring guide result merging

### Polymorphic Config System
- Type-dispatched providers (openai, anthropic, lmql, instructor, outlines, etc.) allow flexible deployments
- Plugin schema registration extends config loader without core changes
- Environment variable overrides enable containerized deployment

### Dual-Path Query Architecture
- RSQL (agent/deterministic) enforces explicit intent and reproducibility
- NLQ (human/semantic) offers natural expression with intelligent strategy selection
- Both paths log intent separately from execution for transparency

---

## User Stories

Maintainers interact with the system through these key stories:

### Configuration & Schema
- [US-0006](../stories/ingestion/US-0006-document-parsing-and-decomposition.md) — Document Parsing and Decomposition (schema design)
- [US-0027](../stories/admin/US-0027-configure-ai-provider.md) — Configure AI Provider (hot-reload support)
- [US-0028](../stories/admin/US-0028-register-custom-pipeline.md) — Register Custom Pipeline

### Plugin Management
- [US-0029](../stories/admin/US-0029-install-and-enable-plugin.md) — Install and Enable Plugin
- [US-0300](../stories/ingestion/US-0300-importer-extension-interface.md) — Importer Extension Interface (plugin contract design)
- [US-0042](../stories/plugins/US-0042-implement-custom-enrichment-plugin.md) — Implement Custom Enrichment Plugin
- [US-0043](../stories/plugins/US-0043-implement-custom-ai-provider-plugin.md) — Implement Custom AI Provider Plugin
- [US-0114](../stories/pipelines/US-0114-configure-domain-scraper-rules.md) — Configure Domain Scraper Rules

### Operations & Monitoring
- [US-0008](../stories/ingestion/US-0008-batch-import-from-file.md) — Batch Import from File
- [US-0015](../stories/enrichment/US-0015-batch-enrichment-with-progress.md) — Batch Enrichment with Progress
- [US-0032](../stories/operations/US-0032-monitor-job-queue-health.md) — Monitor Job Queue Health
- [US-0033](../stories/operations/US-0033-debug-failed-enrichment-job.md) — Debug Failed Enrichment Job
- [US-0034](../stories/operations/US-0034-export-and-backup-all-knowledge.md) — Export and Backup All Knowledge
- [US-0035](../stories/operations/US-0035-migrate-storage-backend.md) — Migrate Storage Backend

---

## Success Metrics

- **Plugin ecosystem size:** Number of verified, production-grade plugins
- **Documentation coverage:** % of public APIs with examples + migration guides
- **Stability:** Breaking change frequency, regression test pass rate, production incident mean-time-to-recovery
- **Contributor velocity:** Time from issue → PR → merge, contributor retention
- **Schema stability:** Version adoption curve (% of deployments on latest schema vs. older versions)

---

## Collaboration with Other Personas

- **Agents/LLMs:** Maintainers provide stable API contracts and query schemas; agents provide feedback on usability
- **Knowledge Workers:** Maintainers observe usage patterns; knowledge workers request features and report friction
- **Integrators:** Maintainers review plugin PRs, provide plugin templates, maintain backwards compatibility
- **Operations:** Maintainers provide clear migration paths; operations report scaling bottlenecks and reliability issues
