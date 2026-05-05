# Feature Set (dPKMS + `ctxt`)

## Universal Capture Layer (Every Interface)
First-class capture from CLI, TUI, REPL, browser extension, web UI, and mobile
share sheet with offline-first behavior, a local outbox, instant “inbox dump”
mode, and zero required classification at capture time.

## Ambient Capture (Continuous Local-Side)
Long-running local daemon (`ctxd`) hosts pluggable ambient sources — clipboard,
file-watch (drop-folder), browser history, foreground-window, screenshot,
meeting capture (audio + video) — feeding into existing pipelines without explicit
per-event user action. Runs client-side (because dPKMS is often deployed remote)
with a pluggable buffer (local-FS XDG / S3-compatible / in-memory), client-side
fingerprint dedup at the enqueue boundary, kit/policy CEL guards for privacy
enforcement before network egress, and a comprehensive bus event taxonomy at
every transformation. Three launch paths share one binary: `ctxt capture --ambient`
(auto-config CLI), `brew services start ctxd`, or `launchctl`/`systemctl`.

## Work Sessions (Temporal Grouping)
Three-rule cutter (idle / single-app focus + frequent-switching exception /
hard timeout) groups ambient captures into bounded work units — queryable via
`ctxt session list`, `ctxt session show`, `ctxt compose --session <id>`. Sessions
cut client-side in `ctxd` (the foreground-window signal is local); dPKMS
persists them with soft-FK semantics so network-loss replay handles arrival
ordering correctly.

## Meeting Capture (Audio + Video, Multi-Platform)
Explicit-trigger recording of video calls (Zoom, Meet, Teams, FaceTime, Discord)
on macOS 13+, Windows 10+, and Linux (Wayland-first). Captures system audio +
window framebuffer via OS-blessed public APIs (ScreenCaptureKit / WASAPI loopback +
Graphics Capture / xdg-desktop-portal + PipeWire). Routes to existing
`audio.transcribe` (diarization, alignment) or `video.full` (transcript + frame
OCR + scene-aligned timeline) pipelines. First-class redact-as-supersede for
post-hoc segment removal. Mobile companion apps (iOS / Android) as Phase 6+ work
in separate repos. Mandatory recording indicator; consent-law guidance documented
but enforcement is the user's.

## Agent-Native MCP Read-Surface
Two MCP servers expose the knowledge graph and live local state to AI agents
(Claude Code, Claude Desktop, Cursor, Codex, opencode, custom). dpkms-side
(`/api/v1/mcp/`) is authoritative with 10 tools spanning search, list, get,
entity, recent, sessions, session, compose, mentions, schema. ctxd-side
(`:8744/mcp`) is local-only with 5 tools (current_session, recent_local,
pending_enqueue, sources, health) surfacing state dPKMS cannot see when remote.
Read-only by design; writes go through the existing `ctxt analyze` enqueue path.
Streamable-HTTP per MCP spec 2025-03-26. `ctxt mcp install <client>` writes the
appropriate config for the major MCP-aware clients; idempotent and reversible.

## Multimodal + Polyglot Ingestion Pipelines
Text, URL, image, audio, video, documents, code snippets, and chat transcripts
flow through configurable pipelines with extensible steps, custom AI models,
mention extraction, entity resolution, multilingual handling, and plugin-defined
transformations.

## Structured Knowledge Objects (Formless → Atomic)
Each ingested item becomes a structured knowledge object with:
summaries, extracted atomic notes, sections, tags, mentions, decisions, tasks,
metadata, embeddings, and provenance — with progressive enrichment from raw to
refined over time.

## Semantic Identity + Knowledge Graph (Atomic + Verifiable)
Stable canonical entities referenced via `@mentions` across all objects, with a
graph index of object ↔ entity and entity ↔ entity edges used for querying,
navigation, relationship discovery, and agent reasoning.

## Traceability + Version History (Receipts + Reversible Change)
Full provenance preservation (source + time + pipeline + model) with revision
history for knowledge objects, entities, and graph edges, including diff views,
rollback, and “why this exists / why this changed” explanation metadata.

## Transactional Job Queue (Durable Local Outbox Pattern)
All ingestion and refresh operations run through a transactional jobs table,
guaranteeing crash-safe recovery, resumable execution, background pipelines,
and deterministic replays for auditability and debugging.

## Permissioned Sharing Model (Trusted by Default)
A formal permission system governs exposure and contribution at the level of:
collections, entities, knowledge objects, and views — enabling private-by-default
workspaces, safe collaboration, intentional publishing, scoped subscriptions,
and reversible sharing.

## Self-Authenticating Knowledge (Optional Signatures + Integrity)
Optional cryptographic signing and verification for entities, bundles, registries,
and revisions, enabling tamper-evident history and portable trust without requiring
a centralized authority.

## Decentralized Registries (Optional Subscriptions)
Subscribe to external or local registries for taxonomies, entity definitions,
localized labels and aliases, weights, shared knowledge packs, and workflows —
with optional authentication, paid access, trust policies, and controlled import
rules. Registries may support **index-only replication** (thin sync) with
**just-in-time pulls** for full content, enabling subscriptions and credit-based
metering without forcing bulk redistribution.

## Federated Query + Scatter–Gather Retrieval (Across Sources)
Query across local storage plus multiple registries, merge results with hybrid
(symbolic + vector) retrieval, and rerank outputs using provenance-aware scoring,
mention-aware filters, graph-informed expansion, and source trust weighting.

## Advanced Query Language (AST-Based, Explainable)
An AST-based query language supports boolean logic, nested expressions, ranges,
time windows, provenance filters, mention-aware constraints, and graph operators,
compiling cleanly into SQL, FTS, vector search, and graph traversal — with
explainable match reasoning.

## Evergreen Engine (Refresh + Resurface)
Policy-driven refresh behaviors re-run pipelines on stale items, detect changes
in upstream sources, rescore relevance by recency and active focus profiles, and
resurface knowledge “just in time” through configurable review queues and
reminders.

## Focus Profiles (Role / Project Lenses)
Profile-driven scoping and ranking reshapes retrieval, surfacing, pipeline
selection, and composition defaults for modes like Founder, Engineer, Research,
Family, or “Project X”.

## Action Layer (Decisions, Tasks, and Outputs)
Action extraction identifies tasks, decisions, follow-ups, and commitments, with
output builders that assemble drafts, briefs, plans, checklists, meeting packets,
and publishing-ready artifacts from atomic nodes and graph context.

## Plugin Architecture (Formal Contract + Permissions)
Plugins extend pipelines, registries, commands, storage backends, query operators,
AI providers, refresh policies, export formats, notification systems, UI surfaces,
and automation hooks using a documented Plugin API and strict permission model.

## Language-Native Storage + Indexing (Multilingual by Design)
Unicode-safe and RTL-safe storage and indexing support mixed-language knowledge
without hacks, including localized entity labels, aliases, and language-aware
querying across Arabic/English/French content.

## Security + Encryption (Sovereign by Design)
Encryption at rest and in transit with pluggable key management, controlled sharing
handshakes, and an optional privacy-preserving search mode that supports filtering
and retrieval without exposing plaintext content.

## Authentication + Authorization (Capability-Scoped)
Built-in support for local-only mode, token-based access, scoped permissions for
workspaces and registries, and policy-driven authorization for publishing,
subscribing, and collaboration.

## Multiple Storage Backends (Sovereign + Portable)
Default SQLite backend with optional Postgres, remote stores, vector databases,
and community backends — all behind a portable storage contract that guarantees
stable IDs and reversible migration paths.

## Export + Portability Contract (No Lock-In)
Full export/import support for Markdown + JSON + SQLite bundles, preserving
entities, edges, provenance, IDs, and attachments, enabling complete migration
without data loss or broken links.

## Performance Guarantees (Fast at Scale)
Indexing strategy for FTS + vectors + graph adjacency, background compaction,
incremental embedding updates, and caching layers to maintain predictable latency
and “instant-feeling” retrieval as data grows.
