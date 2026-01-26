# Decentralization

ContextHelp is built on a decentralized architecture designed to give users full data ownership and full control over how knowledge is structured, enriched, shared, and consumed. The system avoids centralized dependencies, allows for federated knowledge ecosystems, and enables agents to operate on user-defined worldviews rather than global or vendor-controlled ones.

Decentralization is not an add-on — it is a core architectural principle.

---

# Goals of Decentralization

ContextHelp decentralization supports four primary goals:

### 1. **User Ownership**

All content, tags, summaries, taxonomies, enriched bookmarks, and pipeline outputs remain fully local unless explicitly shared.

### 2. **Modularity and Interoperability**

Registries, pipelines, agents, and storage implementations are modular and can be developed by independent entities.
No centralized authority defines a global ontology.

### 3. **Federated Knowledge Models**

Anyone can publish a registry (taxonomy, weights, bookmarks), and users subscribe selectively.
Registries may represent communities, teams, commercial datasets, or individual creators.

### 4. **Privacy by Design**

Processing is local-first.
External knowledge sources are optional, sandboxed, and only accessed when explicitly configured or required by the user/agent.

---

# Decentralized Components

Decentralization applies across storage, ingestion, search, registries, agents, and pipelines.
Below are the components most affected by federated and distributed operation.

---

## Registries

Registries are independent knowledge providers exposing structured semantics through the ContextHelp Registry Protocol.

A registry may contain:

- Taxonomies
- Tags and semantic schemas
- Weight heuristics
- Bookmark collections
- Vocabulary and translations
- Agent “starter worldviews”

Registry sources can be:

- Local JSON or YAML files
- Git repositories
- Remote HTTP services
- CDN-hosted static registries
- Paid or authenticated registries
- Organization-internal registries

The engine treats registries as *read-only semantic overlays*.
The user always chooses which ones apply.

Registries:

- Influence tagging
- Influence interpretations
- Influence agent behavior
- Influence search relevance
- Never receive user content
- May be refreshed on demand or on schedule

Merging across registries follows deterministic rules defined in configuration.

---

## Agents

Agents operate in **scoped worldviews**, defined by user preferences and registry subscriptions.

Agent profiles define:

- Which registries are active
- Which weight systems apply
- Which pipeline types are available
- What tags or controlled vocabularies matter
- Which bookmarks or storage namespaces they can access
- Whether they can use external services (LLMs, embeddings, translation)

This enables:

- UX agents relying on UX registries
- Growth agents using growth heuristics
- Coding agents using code syntax ontologies
- Privacy-bound personal agents using only local data

Each agent’s worldview is decentralized and personalized — no shared universal ontology is required.

---

## Pipelines

Pipelines are decentralized and user-controlled in several ways:

- Users choose which pipelines are installed
- Pipelines can come from local or plugin sources
- Registries can modify pipeline behavior:
  - Tags
  - Controlled vocabularies
  - Heuristics
  - Decision frameworks
- Users can override or disable registry influence
- Agents may use different pipelines for the same data
- Plugin registries may define new pipeline types

Pipeline execution is fully local unless configured otherwise.
Registries can supply semantics but never receive user data or pipeline output.

---

## Storage

Storage is decentralized by design:

- Completely local by default
- Never transmitted to any registry or service
- Pluggable backends (`json`, `sqlite`, `postgres`, custom plugins)
- Deterministic job scheduling and ingestion (transactional outbox)
- Optional team sync or cloud backup
- Optional remote read-only mount (e.g., team knowledge store)

Each installation is its own **sovereign knowledge environment**.

Storage decentralization ensures:

- No global lockstep schema
- No requirement to use a vendor cloud
- Compatible with backup/restore and air-gapped machines

---

# Federated Knowledge Ecosystem

Decentralization supports a layered, federated ecosystem where each participant decides what knowledge sources influence their environment.

### ✦ Individuals curate semantic preferences

Users subscribe to registries aligned with their fields, interests, and workflows.

### ✦ Teams share knowledge

Teams may publish internal registries defining:

- shared vocabularies
- annotation rules
- internal heuristics
- domain best practices
- shared bookmarks and examples

Agents within the team operate with aligned worldviews.

### ✦ Communities publish public registries

For example:

- Open-source UX pattern libraries
- Growth optimization heuristics
- Domain ontologies
- Research annotation and summarization taxonomies

Users adopt only the ones they trust.

### ✦ Organizations commercialize registries

Companies can sell:

- domain-specific taxonomies
- curated datasets
- semantic weights
- task frameworks
- agent-ready ontologies

A decentralized registry ecosystem makes paid knowledge markets viable.

---

# How Decentralization Works Technically

The decentralized model is not conceptual — it has explicit technical structures.

---

## Registry Protocol

Registries adhere to a simple protocol that exposes endpoints such as:

- `/taxonomy` (tags, types, labels)
- `/weights` (heuristic scores)
- `/bookmarks` (optional curated content)
- `/meta` (version, source, capabilities)

The protocol supports:

- Authentication
- Per-registry rate limits
- Versioning
- Caching
- Partial merges
- Delta updates

Registries never receive user data; they only return static or enriched semantic metadata.

---

## Deterministic Merging

Registry data is merged based on:

- Defined precedence order
- User overrides
- Optional per-agent overrides
- Schema validation rules
- Conflict policies (fail/merge/override/local-wins)

This ensures consistent behavior even when many registries supply overlapping semantics.

---

## Multi-Source Retrieval & Reranking

Search queries are resolved through a **scatter–gather** model:

1. Search local storage
2. Search subscribed registries (if enabled)
3. Normalize scores
4. Deduplicate by ID/URL/semantic match
5. Rerank via a dedicated Reranker Service
6. Return unified results

This allows:

- Multi-source knowledge lookups
- Contextual overrides
- Dynamic agent-specific worldviews
- Source-aware scoring

Local data always has priority unless configured otherwise.

---

## Ingestion Isolation

During ingestion:

- Registry data may provide semantics
- Pipelines run locally
- The ingestion system (jobs + job_steps) remains independent of registry availability
- Registries are consulted only for static, non-sensitive metadata

The pipeline never uploads user content to registries.

---

## Replaceable Components

Every subsystem supports plugin replacement:

- Storage backend
- Pipeline types
- LLM or embedding clients
- Ranking strategy
- Registry merging logic
- Query AST handlers
- Language/translation services
- Observability layers

Users and third parties extend the environment without modifying the core engine.

---

# Decentralization Benefits

### ✔ Strong Privacy Guarantees

User data stays local unless explicitly exported.

### ✔ Rich Personalization

Semantic meaning is not global — it comes from registries the user chooses.

### ✔ Knowledge Portability

The entire system (bookmarks, pipelines, registries, worldviews) is portable.

### ✔ Ecosystem Interoperability

Registries and plugins remain independent and compatible by specification, not by vendor.

### ✔ Domain-Specific Agents

Agents run with tailored worldviews instead of generic ones.

### ✔ Market and Community Growth

Anyone can publish registries, create pipelines, or define agent worldviews — enabling bottom-up evolution.

---

# Example: A Fully Decentralized Workflow

### User A subscribes to:
- `uxpatterns.io` taxonomy registry
- `growthvault.dev` weights registry
- Local bookmarks and notes

### User B subscribes to:
- No external registries
- Local taxonomies only

### User C subscribes to:
- Internal `research.company.internal` registry
- AI research ontology registry
- Translation/world-language registry

The same input analyzed by each user produces different, worldview-aligned structured data.

---

# Summary

ContextHelp decentralization ensures:

- Every user owns their data and worldview
- Semantic meaning is user-defined rather than vendor-defined
- Registries are optional, pluggable, and independent
- Agents operate with scoped, configurable worldviews
- Pipelines and storage remain local-first
- Multi-source retrieval produces coherent, reranked results
- The ecosystem supports individuals, teams, and commercial registry providers

This makes ContextHelp not just a tool, but a **platform for decentralized personal and organizational intelligence**.