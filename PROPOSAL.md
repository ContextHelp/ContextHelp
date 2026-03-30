# Proposal: dPKMS + `ctxt` — A Decentralized Knowledge Substrate and an Agentic Context Brain

## Overview

This proposal introduces a two-package architecture:

- **dPKMS** — a decentralized, local-first **knowledge substrate** for durable storage, encryption, identity, indexing, registries, federation, and safe execution.
- **`ctxt`** — an agentic **context brain** that uses dPKMS capabilities to ingest, enrich, compose, and surface knowledge just-in-time through human-friendly interfaces.

Together, they transform raw multimodal inputs into structured, contextualized knowledge that humans and AI agents can reliably use as **private, task-relevant context**.

This design explicitly separates:
- **mechanics** (dPKMS: safe execution + data guarantees)
from
- **meaning** (`ctxt`: intelligence + behavior + workflows)

---

## Problem Statement

Modern AI agents—even highly capable ones—operate without the user’s personal context.

They lack reliable access to:

- What the user reads and captures
- What the user believes matters (and why)
- Which concepts are canonical in the user’s world
- How knowledge connects across time, projects, and decisions
- What vocabulary, frameworks, and entities the user references
- Which sources are trusted, current, or outdated

Without context, agents reason in a vacuum.
Without structure, knowledge remains fragmented.
Without identity, meaning can’t anchor to stable concepts.
Without traceability, outputs can’t be trusted.
Without durability, nothing can run autonomously.

Existing PKM tools store information, but they don’t provide a **sovereign, verifiable, federated substrate** that agentic systems can safely build on.

This proposal fills that gap.

---

## Vision

### dPKMS becomes the “Context Operating System”
A stable substrate that guarantees:

- local-first storage and portability
- privacy, encryption, and ownership
- versioned provenance and reversibility
- registry subscriptions and federation
- safe background execution (jobs + pipelines)
- a queryable semantic graph of knowledge

### `ctxt` becomes the daily context brain
An agentic interface that delivers:

- capture without thinking
- automatic enrichment pipelines
- search that forgives uncertainty
- focus profiles (role/project lenses)
- just-in-time resurfacing
- one-command composition into briefs, plans, drafts, and decisions
- controlled sharing and publishing when desired

The outcome:
Humans and agents that operate inside *your* world, not a generic one.

---

## Core Non-Negotiables (System Principles)

- Formless
- Frictionless
- Polyglot
- Sovereign
- Accessible
- Trusted
- Federated
- Self-authenticating
- Verifiable
- Interoperable
- Atomic
- Discoverable
- Evergreen
- Actionable
- Extensible
- Durable
- Fast

These guide architectural decisions without requiring every deployment to enable every capability.

---

# Architecture: Two Packages, Clean Boundaries

## 1) dPKMS (The Substrate)

dPKMS is the execution and data layer.
It is designed to be **agent-ready**, not agent-opinionated.

### What dPKMS provides

- **Storage + Indexing**
  Local-first database, attachments, FTS + vectors + graph adjacency indexes, and stable IDs.

- **Knowledge Objects + Graph Model**
  Structured objects, canonical entities, mentions, and edges that remain queryable and portable.

- **Jobs + Pipelines Runtime**
  A safe pipeline runtime plus a transactional job queue (local outbox pattern) supporting retries, resumability, and deterministic replay.

- **Encryption + Key Management**
  Encryption at rest and in transit, with pluggable key strategies.

- **Authentication + Authorization**
  Support for private-by-default workspaces, registries, scoped collaboration, and permission checks.

- **Registries + Federation**
  Decentralized registries for taxonomies, entity definitions, workflows, and shared packs with subscription and synchronization primitives.

- **Self-Authenticating Capabilities (Optional)**
  Signing and verification for bundles, registries, and revisions so trust can travel with the data.

- **Export / Import Contract**
  Portable bundles preserving IDs, provenance, edges, and attachments.

### What dPKMS does *not* do
- decide which enrichments matter
- choose models or writing style
- generate briefs or plans
- decide what to surface or notify
- enforce a worldview

dPKMS runs work **correctly**.
It does not decide what work is **valuable**.

---

## 2) `ctxt` (The Context Brain)

`ctxt` is the agentic product layer.
It builds human workflows on top of dPKMS capabilities.

### What `ctxt` provides

- **Universal Capture**
  CLI/TUI/REPL commands, browser extension entrypoints, mobile capture, offline-first dumping.

- **Multimodal Enrichment Pipelines**
  Recipes for summarization, entity extraction, decision detection, task extraction, translation hooks, and structured decomposition.

- **Meaningful Defaults**
  “Capture first, structure later” behavior with background enrichment and graceful degradation.

- **Focus Profiles (Role / Project Lenses)**
  Founder vs Engineer vs Research vs “Project X” profiles that shape ranking, surfacing, templates, and outputs.

- **Just-In-Time Surfacing**
  Relevant knowledge resurfacing based on active work, projects, time windows, people, and recent activity.

- **Composition Engine**
  One-command generation of briefs, plans, meeting packets, drafts, checklists, and publish-ready artifacts.

- **Safe Agent Behavior**
  Propose → dry-run → apply workflows with audits, scope controls, and reversibility.

`ctxt` decides which jobs and pipelines are worth running.
dPKMS guarantees they run safely.

---

# Key Concepts (Shared Vocabulary)

## Pipelines
Modular processing graphs that transform raw inputs into structured knowledge objects.

Examples:
- `ingest.text.short`
- `ingest.url.generic`
- `ingest.image.ocr`
- `ingest.audio.transcribe`
- `refresh.stale`

dPKMS provides the runtime.
`ctxt` provides the recipes.

---

## Knowledge Objects (Successor to “Bookmarks”)
Each processed item becomes a structured object with:
- identity scalars: `ID`, `Type`, `Subtype`, `Source`, `Pipeline`, `ProfileID`
- `ObjectGraph` — intra-object graph of typed nodes (write source of truth; ADR-063):
  sections, tags, entity mentions, decisions, tasks, summaries, code blocks
- stable node IDs (`<objectID>/<nodeType>/<ordinal>`) + URI scheme (`ctxt:node/…`)
- `DocumentProjection` and `IndexProjection` derived on read from the graph
- provenance and version history
- embeddings and retrieval metadata
- flat fields (`Sections`, `Tags`, `Decisions`, `Tasks`) retained as projection
  cache; writers MUST target `ObjectGraph`

---

## Mentions & Canonical Entities
Mentions like:
- `@ui.best-practice`
- `@stripe.api.checkout`
- `@component.form.input`

Reference stable entities with:
- titles, descriptions, aliases
- translations / localized labels
- namespaces and versions
- registry or local ownership

This creates durable semantic anchors for:
- navigation
- linking
- filtering
- reasoning
- composition

---

## Knowledge Graph
Graph edges connect:
- object ↔ entity
- entity ↔ entity
- object ↔ object (optional)

This enables:
- entity-based retrieval
- graph-aware reranking
- semantic neighborhoods
- concept evolution over time
- agent reasoning grounded in receipts

---

## Registries
Registries distribute structured meaning:
- taxonomies
- entity definitions
- weights and heuristics
- shared knowledge packs
- workflows and recipes

Registries may be:
- local or remote
- private or public
- authenticated or paid
- signed and verifiable (optional)

Users choose what to trust.

---

# Interfaces

## dPKMS Interfaces
- library API (core contract)
- optional daemon API (HTTP/gRPC) for local clients
- plugin API (capabilities + permissions)

## `ctxt` Interfaces
- CLI (`ctxt`)
- optional TUI
- optional browser extension and mobile entrypoints
- REST/gRPC for automations and agents

---

# Primary Use Cases

## Individuals
- capture anything without organizing
- build a personal knowledge graph automatically
- retrieve knowledge even under uncertainty
- write faster using contextual composition
- maintain evergreen knowledge without gardening

## Teams
- shared canonical entities and taxonomies
- controlled collaboration and publishing
- consistent mention conventions
- subscribed knowledge packs and workflows

## Developers
- agent runtimes with private context
- stable entity schema for domain alignment
- federated retrieval across knowledge sources
- plugin-driven integration into tools and codebases

---

# Roadmap (Two-Track)

## Phase 1 — dPKMS Core Substrate (MVP)
- SQLite storage + attachments
- jobs queue (outbox pattern)
- pipeline runtime primitives
- entities + mentions + graph core
- export/import bundle contract
- basic query API

## Phase 2 — `ctxt` CLI Brain (MVP)
- `ctxt add`, `ctxt find`, `ctxt open`
- ingestion recipes for text + URL
- structured objects + mentions
- composition: “make brief”
- focus profiles (basic)

## Phase 3 — Federation + Registries
- registry protocol v1
- subscription + sync behavior
- federated retrieval + reranking
- permissioned sharing model

## Phase 4 — Verifiability + Signing
- optional signature support for bundles and registries
- trust policies
- tamper-evident revision trails

## Phase 5 — Agentic Surfacing + Evergreen Ops
- resurfacing engine (just-in-time knowledge)
- refresh policies + staleness detection
- deeper composition workflows
- performance hardening

---

# Why This Matters

As AI systems become more autonomous, **context becomes the new compute**.

This architecture makes context:
- owned
- durable
- queryable
- verifiable
- federated
- and usable by humans and agents

It is not just a tool.
It is a substrate + brain for contextual cognition.

---

## Call for Participation

We are seeking collaborators interested in:

- agentic systems and workflows
- local-first, sovereign computing
- knowledge graphs and semantic identity
- encryption, signatures, and trust models
- open registry protocols
- multimodal enrichment pipelines

The project will be open source under **AGPL-3.0**.
