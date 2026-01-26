# Living Skeleton Sprint Documentation

This directory contains the detailed sprint plans for building ContextHelp using the **Living Skeleton methodology**.

---

## What is Living Skeleton?

A living skeleton is:
- A working end-to-end path that exists from day one
- Every phase strengthens the same spine
- Each new capability plugs in without rewrites
- Correctness and sovereignty come before "features"

We build a **minimal working system first**, then progressively strengthen it.

---

## Two-Package Architecture

All sprints follow the **dPKMS + ctxt** separation:

- **dPKMS (Substrate):** Jobs, storage, graph, query, registries, security
- **ctxt (Brain):** Pipelines, AI providers, profiles, composition, CLI

See [CROSS-PACKAGE-CONTRACTS.md](CROSS-PACKAGE-CONTRACTS.md) for detailed boundaries.

---

## Sprint Overview

### Skeleton 0: The Shared Kernel
**Goal:** Define the immutable minimum - schema, domain model, and interfaces.

**Deliverables:**
- Storage contract (SQL schema)
- Domain model (Go structs)
- Interface definitions (dPKMS ↔ ctxt contracts)

📄 [000-shared-kernel.md](000-shared-kernel.md)

---

### Skeleton 1: The Echo Loop
**Goal:** Prove end-to-end execution with minimal features.

**Deliverables:**
- `ctxt analyze` creates a knowledge object
- `dpkms serve` processes jobs
- `ctxt list` retrieves objects
- Mentions schema prepared (empty)

📄 [001-echo-loop.md](001-echo-loop.md)

---

### Skeleton 2: Real Data, Real Queries
**Goal:** Add intelligence and query capabilities.

**Deliverables:**
- LLM-based enrichment (summaries, tags)
- Remote HTTP registries
- RSQL query language
- Mention-aware queries (preparation)

📄 [002-real-data-and-queries.md](002-real-data-and-queries.md)

---

### Skeleton 3: Beyond Text
**Goal:** Multimodal ingestion and semantic identity.

**Deliverables:**
- URL pipeline (HTML → Markdown)
- Image pipeline (OCR)
- Mention extraction + entity resolution
- Knowledge graph (backlinks)

📄 [003-beyont-text.md](003-beyont-text.md)

---

### Skeleton 4: Interfaces & Independence
**Goal:** Multiple interfaces and agent worldviews.

**Deliverables:**
- REST + gRPC APIs
- Agent profiles (scoped worldviews)
- Registry sync (offline-capable)
- Mention-aware search

📄 [004-interface-and-independence.md](004-interface-and-independence.md)

---

### Skeleton 5: Semantics & Sidecars
**Goal:** Semantic search and plugin extensibility.

**Deliverables:**
- Vector embeddings
- Semantic search
- Plugin system
- Hybrid reranking (FTS + vector + mentions)

📄 [005-semantics-sidecars.md](005-semantics-sidecars.md)

---

### Skeleton 6: Polish & Multimedia
**Goal:** Production stability and multimedia support.

**Deliverables:**
- Audio/video transcription
- Database migrations
- Backup/restore
- CLI UX polish
- Ranking explainability

📄 [006-polish-and-multimedia.md](006-polish-and-multimedia.md)

---

### Skeleton 7: Sovereign & Scalable
**Goal:** Offline AI and vector scalability.

**Deliverables:**
- Local LLM support (Ollama)
- Vector optimization (sqlite-vec or HNSW)
- Developer tooling (linters, scaffolding)
- Multi-registry reconciliation

📄 [007-sovereign-and-scalable.md](007-sovereign-and-scalable.md)

---

### Skeleton 8: Trust & Automation
**Goal:** Safe automation and semantic identity integrity.

**Deliverables:**
- Plugin permissions + sandboxing
- Background watchers (clipboard, directory)
- Entity provenance tracking
- Graph safety validation

📄 [008-trust-and-automation.md](008-trust-and-automation.md)

---

### Skeleton 9: Proof of Platform
**Goal:** Stress testing and ecosystem validation.

**Deliverables:**
- Browser extension (reference integration)
- Raycast script
- 100k+ knowledge object simulation
- Static documentation site

📄 [009-proof-of-plarform.md](009-proof-of-plarform.md)

---

## Supporting Documentation

### Cross-Package Contracts
Defines boundaries and integration points between dPKMS and ctxt.

📄 [CROSS-PACKAGE-CONTRACTS.md](CROSS-PACKAGE-CONTRACTS.md)

### Configuration Structure
Explains config file organization and package-specific sections.

📄 [CONFIGURATION-STRUCTURE.md](CONFIGURATION-STRUCTURE.md)

---

## How to Use These Documents

### For Developers

1. **Starting a new skeleton?**
   - Read the skeleton document
   - Review cross-package contracts
   - Check configuration structure

2. **Working on a specific task?**
   - Look for 📦 *Package* annotations
   - Reference interface definitions
   - Follow the integration check at the end

3. **Confused about boundaries?**
   - Check CROSS-PACKAGE-CONTRACTS.md
   - Review branding.md for terminology
   - Ask in team discussions

### For Project Managers

- Each skeleton is an independent milestone
- Integration checks validate completeness
- Risks sections identify blockers early

### For New Contributors

1. Start with [000-shared-kernel.md](000-shared-kernel.md)
2. Read [CROSS-PACKAGE-CONTRACTS.md](CROSS-PACKAGE-CONTRACTS.md)
3. Review [../branding.md](../branding.md) for naming conventions

---

## Terminology

| Term | Meaning |
|------|---------|
| **Skeleton** | An iteration that strengthens the living spine |
| **dPKMS** | Substrate package (jobs, storage, graph, query) |
| **ctxt** | Brain package (pipelines, AI, profiles, CLI) |
| **Knowledge Object** | Formerly "bookmark" - the core data unit |
| **Mention** | Reference to an entity using `@namespace.slug` syntax |
| **Entity** | Canonical concept with ID, title, aliases |
| **Profile** | User/role-based lens (Founder, Engineer, Research) |

---

## Version History

- **v1.0** (2026-01-25): Initial sprint documentation with dPKMS/ctxt separation
- **v0.9** (2026-01-16): Original sprint docs (monolithic "ContextHelp")

---

## Related Documentation

- [../README.md](../README.md) - Main documentation index
- [../architecture.md](../architecture.md) - System architecture
- [../branding.md](../branding.md) - Naming conventions
- [../dpkms-or-ctxt.md](../dpkms-or-ctxt.md) - Package placement guide
- [../../ROADMAP.md](../../ROADMAP.md) - Living skeleton roadmap

---

## Contributing

When updating sprint documentation:

1. **Maintain package annotations** (📦 *dPKMS*, 📦 *ctxt*, 📦 *dPKMS + ctxt*)
2. **Update cross-package contracts** if interfaces change
3. **Keep integration checks current** with latest features
4. **Document risks** specific to the skeleton
5. **Use correct terminology** per branding.md

---

This living skeleton ensures we ship a usable system early, then grow capability without breaking the spine.
