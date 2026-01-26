# Glossary

- **Action Layer**
  The system behavior that extracts tasks, decisions, follow-ups, and outputs,
  turning knowledge into movement.

- **Attachment Store**
  The storage layer for binary content such as files, images, audio, video, and
  documents, linked to knowledge objects.

- **Backlinks**
  Reverse edges or “who references this” relationships used for navigation and
  graph exploration.

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
