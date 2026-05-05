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

## Skeleton 9.5: Ambient Capture + Sessions + Agent-Native MCP

**Goal:** Capture happens *while you work*, not just when you remember to. Group ambient signals into temporal work units. Expose the graph natively to AI agents.

ADRs locked: [066](decisions/ADR-066-ambient-capture-substrate.md), [067](decisions/ADR-067-session-workunit.md), [068](decisions/ADR-068-mcp-read-surface.md), [069](decisions/ADR-069-meeting-capture-source.md). Track: `tlc track show ambient-capture`.

### Substrate (Phase 2)
- [ ] `internal/ambient/` skeleton — `AmbientSource` interface, runner, registry, lifecycle bus events
- [ ] Client-side fingerprint dedup at the enqueue boundary (avoid pipeline cost on no-op events)
- [ ] Local-FS XDG-compliant buffer + in-memory test buffer
- [ ] Standalone `ctxd` binary; CLI launcher (`ctxt capture --ambient`); service-manager integration

### Sources (Phase 3)
- [ ] Clipboard daemon (cross-platform) — first end-to-end source
- [ ] Local file-watch / drop-folder
- [ ] Browser tabs/history (Chrome/Firefox/Safari/Edge SQLite history)
- [ ] Foreground app/window (macOS via AX; Linux X11/Wayland separate; Windows separate)
- [ ] Screenshot-on-demand → `image.ocr` pipeline
- [ ] Meeting capture — macOS audio-only → macOS full → Windows → Linux

### Quality (Phase 4)
- [ ] Session/WorkUnit type + 3-rule cutter (idle / soft-cut + frequent-switching / timeout)
- [ ] sessions table + `objects.session_id` (soft-FK)
- [ ] S3-compatible buffer backend
- [ ] Adapter-side redaction hooks (passwords, OAuth tokens) before pipeline
- [ ] Media retention tier (separate from event buffer; default 48h local + optional S3 archive)

### UX (Phase 5)
- [ ] `ctxt session list/show/tail` + `ctxt compose --session <id>` + `--since` time-range
- [ ] `ctxt capture meeting redact <id> --segment HH:MM-HH:MM` (supersede + media segment removal)
- [ ] `ctxt capture meeting export <id> --format md|srt|vtt`
- [ ] Auto-detect prompt for known meeting bundle-ids (opt-in)
- [ ] User docs + ops runbook

### MCP read-surface (Phase 5)
- [ ] dpkms-side MCP at `/api/v1/mcp/` — 10 tools (search, list, get, entity, recent, sessions, session, compose, mentions, schema)
- [ ] ctxd-side MCP at `:8744/mcp` — 5 local-only tools (current_session, recent_local, pending_enqueue, sources, health)
- [ ] `ctxt mcp install/uninstall <client>` for claude-code/desktop, cursor, codex, opencode, mcp-json

### Mobile companions (Phase 6/7 — separate repos, separate tracks)
- [ ] iOS companion app (`ctxt-ios`, Swift + ReplayKit) — POSTs to configured dpkms via `/api/v1/analyze`
- [ ] Android companion app (`ctxt-android`, Kotlin + MediaProjection + AudioPlaybackCapture)

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
