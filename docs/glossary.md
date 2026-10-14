# Glossary

- **Action Layer**
  The system behavior that extracts tasks, decisions, follow-ups, and outputs,
  turning knowledge into movement.

- **Ambient Capture**
  Planned local-machine subsystem that would stream clipboard, file, browser,
  window, screenshot and meeting signals into the graph without a command per
  capture ([ADR-066](decisions/ADR-066-ambient-capture-substrate.md)). Not
  built: library code under `internal/ambient/`, no daemon, and
  `ctxt capture --ambient` exits with "not yet implemented". Local capture that
  runs today is `ctxt watch` and `ctxt capture tabs|history`. See
  [architecture/ambient-capture.md](architecture/ambient-capture.md).

- **Ambient Source**
  Implementation of the `ambient.Source` interface (`internal/ambient/`) that
  reads one local signal and emits `RawEvent` values. Only the browser-history
  reader is used by a command (`ctxt capture history`); the others are library
  code. Distinct from ADR-065 adapters (which are protocol-shaped).

- **Attachment Store**
  The storage layer for binary content such as files, images, audio, video, and
  documents, linked to knowledge objects.

- **Backlinks**
  Reverse edges or “who references this” relationships used for navigation and
  graph exploration.

- **Buffer (Ambient)**
  Event staging for the planned ambient runner (`internal/ambient/buffer/`):
  local filesystem, S3-compatible and in-memory backends. Library code; no
  command uses it. See [ADR-066](decisions/ADR-066-ambient-capture-substrate.md).

- **Bundle (Knowledge Pack)**
  A portable export/import artifact preserving knowledge objects, entities,
  edges, provenance, and stable IDs for sharing or archiving.

- **Capability**
  A permission-scoped interface exposed to steps or higher layers (storage,
  query, jobs, graph, registries, crypto), preventing unsafe access by default.

- **Canonical Entity**
  The “source of truth” entity definition after resolution, either locally
  defined or provided by a subscribed registry.

- **Composition**
  The generation of structured artifacts (briefs, plans, checklists, meeting
  packets, drafts) assembled from atomic knowledge and graph context.

- **`ctxd`**
  The planned standalone local daemon for ambient capture
  ([ADR-066](decisions/ADR-066-ambient-capture-substrate.md)). It does not
  exist: no binary, no Homebrew service, launchd plist or systemd unit. See
  [architecture/ambient-capture.md](architecture/ambient-capture.md).

- **Cutter (Session Cutter)**
  Three-rule state machine (`internal/ambient/session/`) that would end the
  active session: idle gap (default 5 min), single-app soft cut (3 min, unless
  frequent-switching) or hard timeout (2 h). Library code; no command runs it.
  See [ADR-067](decisions/ADR-067-session-workunit.md).

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
  SHA-256 over a RawEvent's normalized payload, used by `internal/ambient/dedup`
  to drop repeated events before enqueue. Library code for the planned ambient
  runner.

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
  The read-only MCP endpoint dpkms serves at `/api/v1/mcp/` (streamable HTTP,
  protocol 2025-03-26) with two tools, `search` and `schema`. The other dpkms
  tools and the local `ctxd`-side server in
  [ADR-068](decisions/ADR-068-mcp-read-surface.md) were not built. See
  [manual/workflows/mcp-agents.md](manual/workflows/mcp-agents.md).

- **Meeting Capture**
  Planned ambient source for recording calls
  ([ADR-069](decisions/ADR-069-meeting-capture-source.md)). Not built: no
  `ctxt capture meeting` command and no per-OS recorder. A recording made with
  another tool can be captured as a file, which routes to `audio.transcribe`
  or `video.full`. See [manual/workflows/meeting-capture.md](manual/workflows/meeting-capture.md).

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
  Go interface in `internal/ambient/meeting/meeting.go` that per-OS recorders
  (ScreenCaptureKit, WASAPI, PipeWire) would implement. None exists.

- **Pipeline**
  A sequence of deterministic steps that transforms an input into a structured
  knowledge object. Pipelines can include OCR, summarization, tagging, entity
  extraction, and other transformations.

- **Pipeline Runtime**
  The execution environment that runs pipelines safely with step isolation,
  logging, retries, and deterministic replay guarantees.

- **Plugin**
  None: ctxt and dpkms have no plugin system
  ([ADR-078](decisions/ADR-078-no-plugin-system.md)). Extension is through
  pipelines, steps installed from step registries, registries and hooks.

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
  Typed envelope (`internal/ambient`) produced by an ambient source: `Source`,
  `OccurredAt`, `Kind`, `Payload`, `Fingerprint`, `SuggestedPipeline`,
  `SessionID`, `Metadata`. Library code for the planned ambient runner.

- **Recording Indicator**
  Mandatory on-screen indicator planned for meeting capture
  ([ADR-069](decisions/ADR-069-meeting-capture-source.md)). Not built.

- **Redact-as-Supersede**
  Planned meeting-capture operation that removes a transcript segment by writing
  a superseding revision and deleting the matching media
  ([ADR-069 §6](decisions/ADR-069-meeting-capture-source.md)). No command
  performs it.

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
  Planned bounded chunk of focused work that groups ambient captures
  ([ADR-067](decisions/ADR-067-session-workunit.md)). Only the cutter exists, as
  library code; there is no session type in storage and no
  `objects.session_id` column.

- **Soft-FK**
  Foreign key enforced by convention rather than by an RDBMS constraint. ADR-067
  planned `objects.session_id` as a soft-FK to `sessions.id` so replayed events
  could arrive before their session row; neither column nor table exists.

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
