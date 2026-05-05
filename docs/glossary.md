# Glossary

- **Action Layer**
  The system behavior that extracts tasks, decisions, follow-ups, and outputs,
  turning knowledge into movement.

- **Ambient Capture**
  Long-running, local-machine subsystem (`ctxd`) that produces a steady stream of
  small captures from the user's working environment (clipboard, file-watch,
  browser history, foreground window, screenshots, meetings) into the knowledge
  graph automatically, without explicit user action per capture. See
  [ADR-066](decisions/ADR-066-ambient-capture-substrate.md).

- **Ambient Source**
  Independent event producer inside `ctxd` that reads one local-machine signal
  (clipboard, file system, browser SQLite history, foreground window, etc.)
  and emits `RawEvent` values. Multiple sources run concurrently. Distinct from
  ADR-065 adapters (which are protocol-shaped).

- **Attachment Store**
  The storage layer for binary content such as files, images, audio, video, and
  documents, linked to knowledge objects.

- **Backlinks**
  Reverse edges or “who references this” relationships used for navigation and
  graph exploration.

- **Buffer (Ambient)**
  Pluggable client-side staging area in `ctxd` that holds RawEvents while waiting
  for dpkms enqueue. Three backends: local-FS XDG-compliant (default), S3-compatible
  (AWS / R2 / B2 / MinIO), in-memory (tests). Tiered retention: raw 7d / normalized
  30d / durable forever (in dpkms). Media files (meetings) get their own retention
  tier. See [ADR-066](decisions/ADR-066-ambient-capture-substrate.md).

- **Bundle (Knowledge Pack)**
  A portable export/import artifact preserving knowledge objects, entities,
  edges, provenance, and stable IDs for sharing or archiving.

- **Capability**
  A permission-scoped interface exposed to plugins or higher layers (storage,
  query, jobs, graph, registries, crypto), preventing unsafe access by default.

- **Canonical Entity**
  The “source of truth” entity definition after resolution, either locally
  defined or provided by a subscribed registry.

- **Composition**
  The generation of structured artifacts (briefs, plans, checklists, meeting
  packets, drafts) assembled from atomic knowledge and graph context.

- **`ctxd`**
  The standalone local daemon binary that hosts the ambient capture substrate.
  Runs on the user's machine (because dpkms is often remote and cannot read
  local signals). Launchable via `ctxt capture --ambient` (auto-config CLI),
  `brew services start ctxd`, `launchctl`, or `systemctl --user enable --now
  ctxd.service`. All paths share `internal/ambient/`. See
  [ADR-066](decisions/ADR-066-ambient-capture-substrate.md).

- **Cutter (Session Cutter)**
  Three-rule state machine in `ctxd` that decides when to end the active session.
  Idle (gap > `gap_minutes`, default 5), soft cut (single-app focus >
  `soft_cut_minutes` AND not frequent-switching), or hard timeout
  (> `max_session_hours`, default 2). Ported verbatim from OpenChronicle's
  `session/manager.py`. See [ADR-067](decisions/ADR-067-session-workunit.md).

- **dPKMS**
  A decentralized Personal Knowledge Management Substrate that provides durable
  storage, indexing, federation primitives, security, and safe execution for
  context-aware systems.

- **Deterministic Replay**
  The ability to rerun a job and reproduce the same outputs when inputs and
  configuration are unchanged, enabling auditability and debugging.

- **Edge**
  A relationship in the graph, such as object ↔ entity or entity ↔ entity, often
  stored with type, weight, confidence, and provenance.

- **Entity**
  A canonical concept with stable identity referenced across knowledge objects.
  Entities may have descriptions, aliases, translations, metadata, and versions.

- **Entity Resolution**
  The process of mapping mentions, aliases, and extracted terms to canonical
  entity IDs, including fallback to placeholders when unknown.

- **Explainable Ranking**
  The ability to show “why this result appeared” and “why it ranked here” using
  transparent scoring signals.

- **Federated Query**
  A query executed across local storage and one or more registries, merging
  results into a single ranked response.

- **Fingerprint (RawEvent)**
  SHA-256 hash over a RawEvent's normalized payload, used for client-side dedup
  at the enqueue boundary in `ctxd`. Cheap pre-enqueue dedup eliminates no-op
  events (e.g. lock-screen spam, idle-app repeats) before they cost pipeline
  enrichment. Configurable window (default 60s for ambient sources).

- **Focus Profile (Profile)**
  A role or project lens that changes ranking, default pipelines, surfacing
  rules, registries, and output behavior (e.g. Founder, Research, Project X).

- **Hybrid Retrieval**
  Retrieval combining symbolic search (filters, boolean, FTS) with semantic
  similarity (vectors) and graph signals.

- **Input**
  Any raw content captured by the system: text, URL, image, audio, video,
  document, repository link, or pasted snippet.

- **Job**
  A durable unit of background work stored in the job queue, representing a
  pipeline run, refresh, sync, migration, or maintenance task.

- **Job Queue (Transactional Outbox)**
  The database-backed queue used to persist jobs and guarantee crash-safe,
  resumable execution without losing work.

- **Just-In-Time Surfacing**
  Proactive resurfacing of the most relevant knowledge based on current work,
  profiles, time windows, and recent activity.

- **Key Rotation**
  The process of replacing encryption keys safely while maintaining access to
  existing encrypted knowledge.

- **Knowledge Graph**
  A graph built from stored edges connecting objects to entities and entities to
  each other, used for navigation, retrieval, and reasoning.

- **MCP Read-Surface**
  Two MCP servers exposing read-only tools to AI agents. **dpkms-side**
  (`/api/v1/mcp/`) is authoritative: 10 tools spanning search, list, get, entity,
  recent, sessions, session, compose, mentions, schema. **ctxd-side**
  (`:8744/mcp`) is local-only: 5 tools surfacing live local state (current_session,
  recent_local, pending_enqueue, sources, health) that dpkms cannot see when
  remote. Both use streamable-HTTP per MCP spec 2025-03-26. See
  [ADR-068](decisions/ADR-068-mcp-read-surface.md).

- **Meeting Capture**
  Specialized ambient source for recording video calls (Zoom, Meet, Teams,
  FaceTime, Discord). Captures system audio + window framebuffer via OS-blessed
  public APIs (ScreenCaptureKit on macOS 13+, WASAPI loopback + Graphics Capture
  on Windows 10+, xdg-desktop-portal + PipeWire on Linux). Routes to existing
  `audio.transcribe` or `video.full` pipelines. Explicit-trigger only in v1
  (no always-on). See [ADR-069](decisions/ADR-069-meeting-capture-source.md).

- **Knowledge Object**
  A structured unit of stored knowledge produced from an input (text, URL, file,
  audio, video). Contains metadata, sections, tags, mentions, entities, tasks,
  decisions, provenance, and optionally embeddings.

- **Language-Native Storage + Indexing**
  Unicode-safe and RTL-safe storage and indexing supporting mixed-language
  knowledge without hacks, including localized entity labels, aliases, and
  language-aware querying.

- **Local-First**
  The system works fully offline and does not require cloud services to function,
  syncing or federation happens only when explicitly enabled.

- **Mention**
  A durable reference to an entity using `@namespace.concept` syntax, used inside
  knowledge objects and queries to anchor meaning.

- **Multimodal**
  Refers to handling multiple input types such as text, URLs, images, audio,
  video, documents, and code.

- **MeetingRecorder**
  Cross-platform Go interface (`internal/ambient/meeting/recorder.go`) that
  abstracts per-OS recording implementations. Three implementations behind build
  tags: `recorder_darwin.go` (Swift bridge to ScreenCaptureKit + AVCaptureSession),
  `recorder_windows.go` (C++ bridge to WASAPI + Graphics Capture + Media Foundation),
  `recorder_linux.go` (D-Bus to xdg-desktop-portal + gstreamer pipeline). Mobile
  companions (iOS / Android) implement the same semantics in native code.

- **Pipeline**
  A sequence of deterministic steps that transforms an input into a structured
  knowledge object. Pipelines can include OCR, summarization, tagging, entity
  extraction, and other transformations.

- **Pipeline Runtime**
  The execution environment that runs pipelines safely with step isolation,
  logging, retries, and deterministic replay guarantees.

- **Plugin**
  An extension unit that adds new pipelines, query operators, registry providers,
  storage backends, outputs, or integrations through a formal contract.

- **Portable Storage Contract**
  Guarantees that storage backends preserve stable IDs, graph meaning, and
  reversibility across migrations.

- **Privacy-Preserving Search**
  A mode that allows filtering and retrieval without exposing plaintext content,
  typically via encrypted storage plus local indexing strategies.

- **Progressive Enrichment**
  The workflow where raw captures are accepted immediately, then refined over
  time through pipelines without requiring upfront organization.

- **Provenance**
  Trace metadata describing where knowledge came from (source URL/file, time,
  pipeline steps, registry influences, models used).

- **RawEvent**
  Typed envelope produced by an ambient source carrying `Source`, `OccurredAt`,
  `Kind` (text/url/image/file/window-focus/meeting), `Payload`, `Fingerprint`,
  `SuggestedPipeline`, `SessionID`, and per-source `Metadata`. Pre-redacted by
  the source before emission. Traversed by the runner through redaction →
  policy filter → fingerprint dedup → compression → session-tag → buffer →
  enqueue.

- **Recording Indicator**
  Mandatory UI element shown while meeting capture is active (menubar/tray icon
  pulse, OS-native indicator). Emits `ctxt.ambient.meeting.indicator_displayed`
  bus event on display; absence during active recording is observably alarming
  and CEL-rule detectable.

- **Redact-as-Supersede**
  Privacy operation that removes a transcript segment by writing a new
  KnowledgeObject revision with the segment removed AND deleting the
  corresponding media segment from local disk + S3 archive. Original record
  is marked `superseded_by` the new revision rather than hard-deleted; preserves
  audit trail. Per ADR-069 §6.

- **Registry**
  A decentralized source of structured semantics or behavior such as taxonomies,
  entity definitions, weights, workflows, or shared knowledge packs.

- **Registry Protocol**
  The standard API/format used to subscribe to registries, sync updates, and
  validate schema compatibility across providers.

- **Reranking**
  A scoring phase that reorders candidate results using signals such as
  provenance, entity overlap, graph proximity, weights, and recency.

- **Scatter–Gather Retrieval**
  A retrieval strategy where the query is sent to multiple sources, results are
  gathered, merged, and reranked using hybrid scoring.

- **Self-Authenticating Knowledge**
  Knowledge artifacts that can prove integrity and authorship via cryptographic
  signing of bundles, registries, or revisions.

- **Session (WorkUnit)**
  Bounded chunk of focused work, cut by the three-rule cutter in `ctxd`.
  Groups ambient captures (clipboard, browser, foreground, meeting, etc.)
  into a queryable unit identified by `sess_<12-hex>`. Schema: id, started_at,
  ended_at, end_reason (idle/soft_cut/timeout/shutdown/daily_safety_net),
  app_mix, event_count, source_mix, profile_id. `KnowledgeObject` gains an
  optional `SessionID`. Soft-FK to absorb network-loss replay ordering.
  See [ADR-067](decisions/ADR-067-session-workunit.md).

- **Soft-FK**
  Foreign key relationship enforced semantically rather than by RDBMS constraint.
  In ADR-067, `objects.session_id` is a soft-FK to `sessions.id`: a
  KnowledgeObject can reference a session row that doesn't yet exist. Required
  because network-loss replay can deliver events tagged with a session_id
  before the session-opened event lands at dpkms.

- **Sovereignty**
  The user owns the data, controls sharing, and can export or migrate without
  lock-in or dependency on centralized services.

- **Subscription**
  A persisted relationship to a registry that enables syncing its definitions,
  packs, and updates into the local system.

- **Trust Policy**
  User-defined rules for accepting or rejecting registry updates based on
  publisher identity, signature requirements, namespaces, or allow/deny lists.

- **Version History**
  Revision tracking for knowledge objects, entities, and edges, including diffs
  and rollback support.

- **WAL Mode**
  SQLite Write-Ahead Logging, improving concurrency and crash resilience for
  local-first systems.

- **`ctxt`**
  The agentic “context brain” built on top of dPKMS that defines ingestion
  recipes, enrichment behavior, surfacing, and composition workflows.
