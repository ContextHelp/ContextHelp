# Roadmap (Living Skeleton Method)

This roadmap is built to ship a usable system early, then grow capability by
replacing placeholders with real implementations without breaking contracts.

We build and maintain a **living skeleton**:
- a working end-to-end path exists from day one
- every phase strengthens the same spine
- each new capability plugs in without rewrites
- correctness and sovereignty come before “features”

Two-track architecture remains the rule:
- **dPKMS** = substrate (safe execution + storage + identity + indexing)
- **`ctxt`** = brain (recipes + behavior + surfacing + composition)

---

## Skeleton 0: Hello Context (Bootable Spine)

**Goal:** Prove the whole system can run end-to-end with minimal features.

- [ ] `ctxt add "hello world"` creates a local object
- [ ] `ctxt find "hello"` returns it via basic search
- [ ] `ctxt open <id>` shows structured output
- [ ] Jobs exist, even if the worker is single-threaded
- [ ] Export produces a portable bundle

This skeleton is the permanent foundation.
Everything else upgrades parts of it.

---

## Skeleton 1: Durable Core (dPKMS Minimal Runtime)

**Goal:** Make the spine reliable under real usage.

### Storage
- [ ] SQLite backend with WAL enabled
- [ ] Attachments store (files + blobs)
- [ ] Stable IDs for objects and entities
- [ ] Basic schema migration support

### Jobs
- [ ] `jobs` + `job_steps` tables
- [ ] background worker runtime (single process)
- [ ] crash recovery for stale `Running` jobs
- [ ] deterministic step replay support

### Query
- [ ] basic query API (list + filters)
- [ ] FTS5 for text search
- [ ] explainable match output (minimal)

---

## Skeleton 2: Meaning Spine (Entities + Mentions + Graph)

**Goal:** The system stops being “storage” and becomes “semantic”.

- [ ] entity schema + local entity definitions
- [ ] mention extraction (`@...`) on ingestion
- [ ] object ↔ entity edges
- [ ] backlinks + adjacency indexes
- [ ] entity lookup + mention-aware filtering (`mention:` / `entity:`)

This is where “context” becomes real.

---

## Skeleton 3: `ctxt` Daily Usability (Capture + Retrieve)

**Goal:** Make it feel frictionless enough to use every day.

- [ ] `ctxt add` supports URL, stdin, files
- [ ] `ctxt inbox` shows raw and pending items
- [ ] `ctxt status` shows running and failed jobs
- [ ] `ctxt find` returns ranked results (FTS-first)
- [ ] `ctxt open` renders structured objects cleanly
- [ ] `ctxt export` produces Markdown + JSON bundle

---

## Skeleton 4: Recipes, Not Frameworks (Built-in Pipelines)

**Goal:** Add intelligence without breaking determinism.

- [ ] `text.short` and `text.long`
- [ ] `url.generic` and `url.repo`
- [ ] `image.ocr`
- [ ] raw mode ingestion to skip AI enrichment
- [ ] configurable defaults per content type
- [ ] provenance stored for every pipeline step

This is where `ctxt` starts feeling agentic.

---

## Skeleton 5: Registry Subscriptions (Decentralized Semantics)

**Goal:** Introduce federation without central dependency.

- [ ] registry protocol client
- [ ] registry add/remove/list/disable
- [ ] taxonomy sync + entity sync
- [ ] deterministic merging + local overrides
- [ ] registry provenance visible in objects
- [ ] federated query against local + registries
- [ ] paid registries supported (auth + entitlement checks)
- [ ] "thin sync" as a first-class mode (index/schema sync without content replication)
- [ ] just-in-time pull for full content (resolve endpoints gated by policy/entitlements)
- [ ] metering hooks for registry access (credits, quotas, export gating)

---

## Skeleton 6: Hybrid Retrieval (Discoverability Upgrades)

**Goal:** Search becomes forgiving under uncertainty.

- [ ] pluggable vector backend (sqlite-vec default)
- [ ] pgvector backend (pairs with Skeleton 10 Postgres)
- [ ] Qdrant backend (external vector DB option)
- [ ] hybrid query execution (AST filters + FTS + vector)
- [ ] scatter–gather merge + reranking (RRF)
- [ ] graph-informed expansion
- [ ] "why ranked" explain scoring output

---

## Skeleton 7: Profiles + Just-in-Time Context

**Goal:** Knowledge becomes situationally relevant.

- [ ] focus profiles (Founder / Research / Project X)
- [ ] profile-scoped registries and weights
- [ ] profile-specific pipelines and outputs
- [ ] resurfacing queue (“what matters now”)
- [ ] lightweight reminders and review loops

---

## Skeleton 8: Trust That Travels (Optional Crypto)

**Goal:** Make trust portable, without making it mandatory.

- [ ] signed bundles (export + import verify)
- [ ] signed registry updates
- [ ] trust policies (allowlist/denylist/required signatures)
- [ ] dry-run registry updates
- [ ] pin/freeze entity versions to prevent drift
- [ ] key rotation support

---

## Skeleton 9: Plugins + Ecosystem

**Goal:** Extend without forks.

- [ ] plugin contract (capabilities + permissions)
- [ ] pipeline extension hooks
- [ ] query operator plugins
- [ ] registry provider plugins
- [ ] output generator plugins
- [ ] translation and localization plugins

---

## Skeleton 10: Scale Track (Optional Enterprise Mode)

**Goal:** Support growth without sacrificing the local-first core.

- [ ] Postgres backend (multi-user)
- [ ] rqlite backend (HA SQLite without Postgres)
- [ ] distributed workers (optional external queue)
- [ ] gRPC streaming for ingestion and retrieval
- [ ] advanced ACL models where needed
- [ ] audit trails for compliance environments

**Non-OSS track (context.help cloud):**
- [ ] multi-tenant admin interface for orgs and nodes (access management)
- [ ] SSO/SCIM onboarding and lifecycle management
- [ ] billing/credits + marketplace surfaces for paid registries and extensions
