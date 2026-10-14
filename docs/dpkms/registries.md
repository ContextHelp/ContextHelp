# Registries

Registries are a core architectural pillar in ContextHelp.
They allow users, teams, communities, and commercial providers to publish **structured semantic knowledge**—including taxonomies, entities, bookmarks, weights, translations, and signals—through a stable, decentralized interface.

Registries operate independently of the engine.
They do **not** need to run ContextHelp; they only need to expose data that conforms to the **Registry Protocol Specification**.

Registries are decentralized, composable, pluggable, and now support **Entities & Mentions** as a first-class semantic layer without requiring changes to core.

---

## Overview

A **registry** is any service, file, or endpoint that exposes structured knowledge through a predictable interface.
Registries are **read-only** from the engine’s perspective—they provide data but never receive data.

Registries now expose four major semantic layers:

- **Tags** (classification vocabulary)
- **Entities** (canonical concepts referenced by Mentions)
- **Bookmarks** (curated datasets)
- **Weights** (ranking and shaping influence)

Optional layers include:

- translations
- aliasing rules

---

## Registry Types

### Taxonomy Registries

Provide structured tag definitions:

- tag labels
- hierarchical relationships
- synonyms and alias rules
- polarity metadata
- classification vocabularies
- translations

These support pipelines, retrieval, ranking, and agent reasoning.

### Entity Registries (New)

Provide **canonical concepts** used by Mentions.

Entities include:

- stable IDs (e.g., `ui.best-practice`)
- titles and descriptions
- aliases
- translations
- metadata

Entities form a semantic graph via backlinks and relationships.

### Bookmark Registries

Provide externally stored bookmark collections:

- curated examples
- design galleries
- research corpora
- enterprise knowledge libraries

Bookmarks retrieved from registries:

- do not pollute local storage
- appear in retrieval results
- integrate with local tags, entities, and mentions
- remain immutable unless overridden locally

### Weights Registries

Provide scoring heuristics:

- tag weight overrides
- domain-specific rank shaping
- custom weighting functions
- entity-level weight adjustments

Weights influence classification, retrieval ranking, and agent reasoning.

### Translation Registries

Provide translations for:

- tag labels
- entity titles & descriptions
- bookmark metadata

They integrate with the I18N/L10N layer.

---

## Registry Architecture

```mermaid
flowchart LR
    A[Registry Provider] --> B[Registry Protocol]
    B --> C[Engine Registry Client]
    C --> D[Local Registry Cache]
    D --> E1[Pipeline Execution]
    D --> E2[Search & Retrieval]
    D --> E3[Agents & Worldviews]
```

Registries are pluggable and interchangeable.

---

## Guiding Principles

- **Decentralized** — Anyone can publish a registry.
- **Local-first** — Engine caches registry data automatically.
- **Composable** — Multiple registries can coexist harmoniously.
- **Versioned** — Content and schema are explicitly versioned.
- **Deterministic** — Merging and lookup produce deterministic results.
- **Extensible** — Pipelines and steps consume registry layers without core changes.
- **Optional** — ContextHelp runs without any external registry.
- **Protocol-driven** — Guarantees compatibility across providers.

---

## Registry Providers

Registries may be:

- static JSON/YAML files
- Git repositories
- authenticated HTTP endpoints
- commercial SaaS APIs
- local files
- distributed storage (future)

As long as they conform to the Registry Protocol, the engine can subscribe.

For deploying separate org / team / personal registry tiers, see [tiered-deployment.md](tiered-deployment.md).

---

## Entities & Mentions in Registries (New)

Registries expose an **entities** section:

```json
{
  "entities": {
    "ui.best-practice": {
      "title": "UI Best Practice",
      "aliases": ["ux.best-practice"],
      "description": "Canonical UX/UI principle",
      "translations": { "fr": "Bonne pratique UI" },
      "metadata": { "domain": "design" }
    }
  }
}
```

### Purpose

- Provide stable identity for **mentions** (`@ui.best-practice`)
- Allow pipelines to resolve references to canonical concepts
- Bootstrap semantic graphs for retrieval & reasoning
- Enable step-driven workflows (validation, metadata lookups)

### Behavior

- Entities may come from multiple registries
- Namespaces determine conflict boundaries
- Aliases are resolved deterministically
- Conflict resolution uses:
  - registry priority
  - entity namespace
  - user overrides

A merged semantic graph is built locally from registry and local entities.

---

## How Registries Integrate with ContextHelp

### Pipelines

During ingestion, pipelines may:

- validate & normalize tags
- resolve mentions → entities
- fetch entity metadata
- apply tag or entity weights
- align content with registry semantics
- support translations

Two new components handle mention/entity semantics:

```mermaid
flowchart LR
    A[Raw Content] --> B[Mention Extractor]
    B --> C[Mention → Entity Resolver]
    C --> D[Pipeline Tasks]
    D --> E[Bookmark Construction]
```

### Retrieval

Retrieval now spans:

- local bookmarks
- remote bookmark registries
- entity-based graph lookup
- taxonomy-aware expansions

Reranking incorporates:

- tag weights
- entity relationships
- registry priorities
- agent preferences

### Agents

Agents define which registries they subscribe to:

```yaml
agents:
  ux_critic:
    taxonomies:
      - global-default
      - registry:uxpatterns/taxonomy
    entities:
      - registry:uxpatterns/entities
    bookmarks:
      - local
      - registry:uxpatterns/examples
    translations:
      - registry:uxpatterns/i18n
```

Agents operate within a **scoped worldview** defined by local and registry sources.

---

## Registry Sources

### Local File Registry

```yaml
registries:
  taxonomy:
    - name: local-ux
      type: file
      path: ./taxonomies/ux.json
```

### HTTP Registry

```yaml
registries:
  entities:
    - name: uxpatterns
      type: http
      url: https://registry.uxpatterns.io/entities
      auth:
        token: ENV.UXPATTERNS_TOKEN
```

Supports caching, throttling, authentication, versioning.

### Git Registry

Cloned locally, then consumed as a file registry.

### Commercial Registry

Premium endpoints with curated taxonomies, entities, or corpora.

---

## Versioning & Caching

Registries declare:

- schema version
- content version
- entity graph version
- ETag / Last-Modified
- checksum

Engine updates only when content changes.
Local cache enables full offline capability.

---

## Merging Multiple Registries

### Tag Merging

- deterministic unification
- alias resolution
- conflict resolution via priority
- translation merging

### Entity Merging (New)

Entities merge using:

- namespace precedence
- alias graph flattening
- deterministic tie-breaking rules

If conflicts remain unresolved:

- highest-priority registry wins
- user-defined overrides always take precedence

### Bookmark Merging

- retrieval-time merging
- deterministic deduplication
- stable IDs used for reconciliation

### Weight Merging

- explicit overrides
- per-registry defaults
- fallback strategies

### Translation Merging

- entity translations
- tag translations
- bookmark summary translations

---

## Authentication

Registries may require:

- static tokens
- Bearer tokens
- header-based keys
- Basic auth
- OAuth2 (future)

Credentials remain local and are never shared.

---

## Registry Failure Behavior

If a registry fails:

- cached data remains active
- ingestion continues gracefully
- retrieval does not break
- merges degrade safely

---

## Registry Discovery (Roadmap)

Future enhancements include:

- decentralized registry directories
- DID/IPFS-based registry hosting
- signed and verified manifests
- registry introspection APIs

---

## Summary

Registries now provide ContextHelp with:

- extensible taxonomies
- canonical entities resolving mentions
- curated bookmarks
- weight models and translations
- mergeable, decentralized semantic sources
- agent-scoped worldview configuration

Registries are one of ContextHelp's extension surfaces, alongside pipelines and steps; there is no plugin system ([ADR-078](../decisions/ADR-078-no-plugin-system.md)).
