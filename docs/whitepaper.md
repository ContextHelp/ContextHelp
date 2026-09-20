# ContextHelp: Architecture of a Local-First Knowledge Substrate and Context Brain

## A Sovereign System for Durable, Agent-Ready Personal Knowledge

**Draft**

ContextHelp project

---

## Abstract

Personal knowledge is fragmented across files, browser history, chat logs, source repositories, note systems, cloud drives, meeting transcripts, feeds, and screenshots. Modern AI systems can reason over large public corpora, but they still operate with weak access to the private, evolving context that gives a user's work meaning. Existing personal knowledge management tools preserve notes or files, but they rarely provide stable semantic identity, verifiable provenance, safe background execution, or a queryable graph that software agents can depend on.

ContextHelp addresses this with a two-package architecture:

- **dPKMS**, a decentralized personal knowledge management substrate responsible for storage, indexing, identity, registries, federation, security, and deterministic execution.
- **`ctxt`**, an agentic context brain responsible for capture, enrichment recipes, focus profiles, retrieval workflows, just-in-time surfacing, and composition.

The system separates mechanical guarantees from behavioral intelligence. dPKMS runs work correctly. `ctxt` decides what work is valuable. Together they provide a local-first, sovereign context engine that can ingest multimodal sources, convert them into structured knowledge objects, resolve canonical entities, preserve receipts, and expose private context to humans and agents through explicit, auditable interfaces.

This paper describes the architecture of ContextHelp, including its structured object model, transactional pipeline runtime, mention and registry system, graph-aware retrieval, ambient capture model, plugin surface, and deployment options. The central claim is that useful personal AI requires more than retrieval over documents: it requires a durable substrate for identity, provenance, permissions, and change.

## Keywords

Local-first software, personal knowledge management, knowledge graph, agent memory, semantic retrieval, provenance, decentralized registries, pipeline runtime, MCP, sovereign computing.

---

## 1. Introduction

Knowledge work now produces more context than humans can deliberately organize. A single project can span code review comments, Slack decisions, meeting audio, browser tabs, PDFs, bookmarks, diagrams, command output, design notes, issue trackers, and personal annotations. The content is not only scattered; it is also semantically unstable. The same company, API, customer, component, or research claim may appear under different names in different tools.

AI agents amplify the problem. They are useful when they have relevant context and risky when they do not. A model can draft a plan, review a change, or summarize a meeting, but without the user's trusted sources, project vocabulary, decision history, and canonical entities, it operates from generic priors. The result is often fluent but ungrounded output.

ContextHelp is designed around a simple architectural boundary:

- dPKMS is the substrate: durable, local-first, verifiable, capability-scoped, and agent-ready.
- `ctxt` is the brain: capture-oriented, workflow-aware, profile-sensitive, and human-facing.

This boundary is intentional. Durable storage, background jobs, graph indexes, registry synchronization, access control, and export/import must be boring and reliable. Enrichment pipelines, search ranking, summaries, briefs, and surfacing behavior must evolve quickly. Coupling those concerns would make the system either fragile or rigid.

The project is not a replacement for Obsidian, Notion, Pocket, Raycast, browser bookmarks, GitHub, or a filesystem. It is a semantic layer above them. Its purpose is to preserve context where it already lives, assign stable identities to concepts, and make private knowledge queryable by humans and agents.

## 2. Related Work

### 2.1 Personal Knowledge Management

Tools such as Obsidian, Logseq, Notion, and Roam demonstrate the value of personal knowledge graphs, backlinks, and user-controlled organization. They work best when a human deliberately structures information. ContextHelp instead assumes capture will often be messy, automatic, multimodal, and distributed across existing tools. It adds a storage and identity substrate underneath the human-facing workflows.

### 2.2 Read-It-Later and Bookmark Systems

Pocket, Raindrop, browser bookmarks, and similar tools are effective at saving URLs, but they generally treat saved items as flat records. ContextHelp models captured content as structured knowledge objects with provenance, sections, mentions, embeddings, graph edges, and revision history.

### 2.3 Enterprise Knowledge Bases

Confluence, SharePoint, Google Drive, and Slack provide team-scale repositories but usually centralize identity, access, and search inside one vendor boundary. ContextHelp treats those systems as sources, not as the substrate of truth. A user or organization can ingest from them while preserving local-first ownership and exportability.

### 2.4 Retrieval-Augmented Generation

RAG systems make private content available to language models, but many implementations stop at chunking, embedding, and vector search. That is useful but insufficient for durable agent memory. ContextHelp combines symbolic identity, graph traversal, structured provenance, full-text search, vector retrieval, and profile-aware ranking so retrieval can answer not just "what text is similar?" but "what entity, source, decision, or chain of evidence is relevant?"

### 2.5 Local-First and Decentralized Systems

Local-first software prioritizes ownership, offline operation, and sync without making a cloud service the authority of record. Decentralized systems add portable trust, replication, and federation. ContextHelp adopts these principles for knowledge rather than files alone: entities, taxonomies, registries, object revisions, and graph edges can remain local, synchronize selectively, or be published through explicit trust policies.

### 2.6 Agent Memory Systems

Agent memory products and frameworks often optimize for conversational recall. ContextHelp targets a broader substrate: user knowledge that predates a conversation, survives tool changes, carries provenance, and can be queried by multiple clients. Agents consume dPKMS as an authoritative read surface rather than owning the user's memory.

## 3. Design Principles

ContextHelp is governed by substrate-level principles that shape both product and implementation decisions.

- **Sovereign:** the user owns the data, keys, exports, and deployment path.
- **Durable:** jobs are resumable, operations are recoverable, and state does not disappear silently.
- **Verifiable:** conclusions and generated outputs can point back to receipts.
- **Self-authenticating:** entities, registries, bundles, and revisions can carry signatures.
- **Interoperable:** captured knowledge survives tool migration and remains addressable.
- **Federated:** useful knowledge can live across local stores, peers, and registries.
- **Language-native:** multilingual labels, aliases, text, and queries are first-class.
- **Extensible:** pipelines, providers, registries, storage backends, and query operators can evolve outside the core.
- **Fast:** capture and retrieval must feel immediate enough to become daily infrastructure.

The core non-negotiable is the boundary between correctness and value judgment. dPKMS provides correctness guarantees. `ctxt` provides value judgments through recipes, profiles, ranking, and composition.

## 4. System Overview

ContextHelp consists of two cooperating packages.

### 4.1 dPKMS: The Knowledge Substrate

dPKMS provides the mechanical foundation:

- local-first storage with pluggable backends
- transactional job queue
- deterministic pipeline runtime
- structured knowledge objects
- entity and mention resolution
- graph indexes
- full-text and vector indexes
- registry synchronization
- authentication and authorization
- encryption and key management
- export/import bundles
- plugin interfaces
- REST, gRPC, and local read surfaces

dPKMS is deliberately conservative. It should not decide which summary is best, whether a meeting matters, or which insight deserves resurfacing. Its job is to store, execute, index, verify, and expose knowledge safely.

### 4.2 `ctxt`: The Context Brain

`ctxt` provides the behavior layer:

- command-line, TUI, browser, and ambient capture flows
- pipeline recipes for common content types
- focus profiles for role, project, or intent-specific ranking
- just-in-time surfacing
- natural language and structured search interfaces
- composition into briefs, plans, drafts, tasks, and decision records
- controlled sharing workflows
- safe agent execution patterns

`ctxt` can change quickly because it delegates durable state and execution guarantees to dPKMS.

### 4.3 Data Flow

A typical capture follows this path:

1. A user or local source captures raw content.
2. `ctxt` selects an ingestion recipe based on content type, source, and profile.
3. dPKMS creates a durable job in its transactional queue.
4. The pipeline runtime executes capability-scoped steps.
5. Extracted structure becomes a knowledge object.
6. Mentions resolve to canonical entities through local and subscribed registries.
7. Index projections update FTS, vector, and graph stores.
8. Retrieval surfaces expose the object and its relationships to humans and agents.

This flow supports partial progress. A text capture can be stored immediately, enriched later, re-indexed when a model changes, and re-resolved when a registry publishes better entity metadata.

## 5. Structured Knowledge Objects

The fundamental unit of the system is the knowledge object. A knowledge object is not just a bookmark, note, file, or chunk. It is a structured record with stable identity, source metadata, derived graph nodes, provenance, and projections for retrieval.

Knowledge objects include:

- stable object ID
- type and subtype
- source and capture metadata
- pipeline and profile identifiers
- object graph nodes such as sections, mentions, decisions, tasks, summaries, and code blocks
- embeddings and index metadata
- provenance receipts
- revision history
- attachment references where applicable

The object graph is the write source of truth. Flat fields such as tags, sections, or decisions can exist as projection caches, but writers target the graph. This keeps the system extensible as new node types emerge without forcing every consumer to understand every enrichment format.

## 6. Semantic Identity: Mentions, Entities, and Registries

Text similarity is not identity. A retrieval system can find similar passages about "Stripe," but a knowledge substrate needs to know whether a mention refers to the company, an API product, a customer integration, a checkout component, or an internal project codename.

ContextHelp uses mentions and entities to anchor meaning:

- A mention is an occurrence of a concept in an object.
- An entity is a canonical concept with stable ID, labels, aliases, namespaces, descriptions, and relationships.
- A registry is a source of entity definitions, taxonomies, weights, workflows, or shared packs.

The registry system works like DNS for concepts. A namespace such as `@stripe.api.checkout` can resolve to a canonical entity with aliases, localized labels, relationships, and trust metadata. Registries can be local, private, paid, public, or plugin-backed. Users decide which registries to trust and how much influence they have over local resolution.

This gives ContextHelp three properties that plain RAG lacks:

- **Canonical recall:** retrieve by concept even when surface language differs.
- **Graph navigation:** move from objects to entities to related entities.
- **Portable meaning:** preserve semantic links across export, import, sync, and tool migration.

## 7. Pipeline Runtime and Jobs

All ingestion, refresh, sync, and maintenance work runs through the dPKMS job system. The queue follows a durable local outbox pattern so work can survive crashes, retries, restarts, and offline operation.

The pipeline runtime provides:

- typed inputs and outputs
- step isolation
- idempotency boundaries
- capability-scoped execution
- structured logs
- retry and backoff policy
- deterministic replay where possible
- caching hooks
- audit metadata

This matters because knowledge enrichment is inherently fallible. OCR can fail. A web page can change. A model provider can be unavailable. A file watcher can emit duplicate events. The substrate must treat every enrichment as a recoverable operation rather than an invisible side effect.

`ctxt` owns recipes such as URL ingestion, document parsing, image OCR, audio transcription, GitHub capture, feed sync, and meeting processing. dPKMS owns the execution contract.

## 8. Search and Retrieval

ContextHelp retrieval combines multiple strategies:

- full-text search for lexical precision
- vector search for semantic similarity
- graph traversal for entity neighborhoods
- structured query filters for time, source, profile, type, and provenance
- registry-aware expansion
- reranking using trust, recency, entity matches, and focus profiles

The query engine is AST-based so user-facing interfaces can compile different query forms into a common representation. Natural language search, RSQL-style filters, saved searches, and agent queries can all target the same substrate.

Search results should be explainable. A result can match because it mentions an entity, falls within a time window, came from a trusted source, shares graph neighbors with the active project, or has close vector similarity. This explanation is part of the trust model: the user should know why context appeared.

## 9. Ambient Capture and Work Sessions

Many useful signals are not explicit documents. They are local, time-bound, and privacy-sensitive: clipboard changes, foreground windows, browser history, screenshots, meetings, file edits, and active directories. These signals belong on the user's machine, not in a remote service.

ContextHelp models ambient capture through a local daemon and source adapters. Local buffers can collect ephemeral events, then session cutting groups them into work units. A work unit is a queryable slice of activity shaped by time, application focus, and user intent.

This enables questions such as:

- What was I working on yesterday afternoon?
- Which documents informed this decision?
- What changed between the meeting and the pull request?
- Which browser tabs and files were active while I wrote this plan?

Ambient capture must remain privacy-first. Sensitive sources can be disabled, redacted, scoped by profile, or retained only locally. dPKMS stores durable objects and indexes; local capture components preserve machine-specific signals that cannot safely or practically live in a remote node.

## 10. Federation and Decentralization

ContextHelp supports multiple deployment and synchronization models:

- single-user local SQLite node
- self-hosted node on a workstation, NAS, or server
- team deployment backed by Postgres
- private registry subscriptions
- public registry publishing
- peer or cloud-assisted synchronization
- archival export bundles

Federation is optional. The system should work fully offline, but it should also support shared taxonomies, entity registries, paid knowledge packs, team libraries, and controlled publishing.

The design avoids making any one cloud service the canonical authority. A managed service can provide identity, billing, marketplace, relay, or availability features, but the node remains the boundary for user knowledge. Export/import preserves IDs, provenance, edges, entities, and attachments so users can leave without semantic data loss.

## 11. Security, Privacy, and Trust

The security model is built around local-first ownership and explicit capability boundaries.

dPKMS is responsible for:

- encryption at rest and in transit
- key management
- token-based access
- scoped workspace and registry permissions
- policy enforcement for publishing and subscription
- audit logs
- secret storage
- signed registries, bundles, and revisions where enabled

Plugins and pipeline steps operate under declared capabilities. A step that reads a local file, calls a remote API, writes an object, or publishes to a registry should be explicit about that authority.

Trust is also semantic. Registry influence should be inspectable. If an entity label, alias, ranking weight, or taxonomy relation came from a subscribed registry, the user should be able to identify that source and override it locally.

## 12. Plugin Architecture

ContextHelp is intended to outlive its initial assumptions. The plugin model allows extension across:

- ingestion sources
- pipeline steps
- registry providers
- storage backends
- ranking strategies
- query operators
- export formats
- notification targets
- AI providers

The plugin contract emphasizes manifest-declared capabilities, isolated execution, stable interfaces, and testable behavior. Core dPKMS should not need to change every time a new source, provider, or enrichment strategy appears.

Plugins also support local and domain-specific intelligence. A security researcher, design team, law office, or open-source maintainer may all need different extraction and ranking behavior while sharing the same substrate guarantees.

## 13. Interfaces

ContextHelp exposes multiple interfaces over the same knowledge substrate:

- `dpkms` daemon and operator CLI
- `ctxt` capture and retrieval CLI
- REST and gRPC APIs
- local MCP read surfaces for agents
- browser and Raycast extensions
- web UI
- mobile companion surfaces
- export/import bundles

The interface strategy is deliberately plural. Capture should happen where work happens. Retrieval should be available to humans and agents without making every client own indexing, security, or provenance logic.

## 14. Performance and Quality

The system optimizes for daily use rather than benchmark theatre. Capture should be fast enough that users do not hesitate. Search should return useful first results quickly. Background enrichment should degrade gracefully when resources, network access, or model providers are unavailable.

The substrate uses:

- incremental indexing
- FTS and vector projections
- graph adjacency indexes
- pipeline caches
- job backoff
- housekeeping and reindex commands
- deterministic test fixtures
- integration tests for importers, pipelines, query behavior, and API surfaces

Quality is measured by recoverability as much as speed. A failed OCR step should be visible. A stale index should be detectable. A backup should restore usable graph identity. A plugin should not silently exceed its declared authority.

## 15. Deployment Models

ContextHelp can run in several modes.

### 15.1 Local Individual

A single user runs dPKMS locally with SQLite. `ctxt` captures content from CLI, browser, files, and local sources. This is the default sovereignty path.

### 15.2 Self-Hosted Personal Node

A user runs dPKMS on a home server, NAS, or private VM. Local clients connect to the node, while machine-local capture sources remain on each device.

### 15.3 Team Node

An organization runs dPKMS with a shared backend, scoped access, audit trails, registry subscriptions, and policy controls. Team use does not require abandoning local-first principles; it makes access and sync explicit.

### 15.4 Managed Cloud

A managed service can operate nodes, identity, marketplace, billing, and high-availability infrastructure. The architecture treats this as one deployment option, not the only source of truth.

### 15.5 Hybrid Federation

Users and organizations can combine local storage, managed availability, private registries, public registries, and archival exports. The common contract is stable identity, provenance, and graph-preserving portability.

## 16. Limitations and Open Problems

ContextHelp deliberately accepts several hard constraints:

- Rich semantic identity requires configuration and trust decisions.
- Automatic capture can create privacy risk if defaults are careless.
- Entity resolution will sometimes be ambiguous and must expose uncertainty.
- Vector retrieval can surface plausible but wrong neighbors.
- Federation introduces conflict, staleness, and trust policy complexity.
- Plugin ecosystems require strong review, isolation, and compatibility discipline.
- Agent-facing interfaces need tight permission boundaries and understandable audit trails.

These are not peripheral issues. They are the core engineering surface of a trustworthy context engine.

## 17. Roadmap

The near-term roadmap focuses on making the substrate dependable and the brain useful:

- harden dPKMS storage, job, query, and backup contracts
- stabilize knowledge object and object graph schemas
- complete entity, mention, and registry resolution flows
- expand importer coverage for files, feeds, browsers, collaboration tools, and archives
- improve explainable hybrid search
- mature local MCP read surfaces
- strengthen plugin manifest validation and capability enforcement
- refine ambient capture privacy controls
- publish portable export/import conformance fixtures

The long-term direction is a federated knowledge ecosystem where individuals and teams can share taxonomies, entities, workflows, and evidence packs without surrendering ownership of their private context.

## 18. Conclusion

Personal AI will not become trustworthy by adding larger context windows alone. It needs a substrate that knows what the user captured, where it came from, how it changed, which concepts it refers to, which sources are trusted, and what authority an agent has to read or act.

ContextHelp provides that substrate through dPKMS and makes it useful through `ctxt`. The architecture separates correctness from judgment, storage from behavior, and private ownership from optional federation. This separation lets the system remain local-first and verifiable while still supporting rich capture, semantic retrieval, and agentic workflows.

The result is a knowledge system intended for humans and agents that need to work inside a user's actual context, not around it.

