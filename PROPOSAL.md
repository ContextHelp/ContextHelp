# Proposal: ContextHelp — A Decentralized Context Engine for Human and Agent Intelligence

## Overview

ContextHelp is a decentralized, local-first engine that transforms raw multimodal content into structured, contextualized knowledge. It is designed to give humans and AI agents the one thing they universally lack: **personal, private, task-relevant context**.

By ingesting content, enriching it through pipelines, aligning it with registry-defined taxonomies and entities, building a knowledge graph, and exposing it through configurable agent profiles, ContextHelp becomes the foundational substrate for context-aware computation.

This proposal outlines the vision, architecture, use cases, and roadmap for ContextHelp, now enhanced with a **semantic identity layer** powered by **Mentions** and **Entities**.

---

## Problem Statement

Modern AI agents—even the most advanced—operate largely without access to the user’s personal context.

They lack visibility into:

- What the user reads
- What the user saves
- How the user classifies information
- Which canonical concepts they rely on
- How their domain knowledge is structured
- What vocabulary, frameworks, and entities they reference
- How content connects across time, tasks, and domains

Without context, agents must reason in a vacuum.
Without structure, personal knowledge remains fragmented.
Without identity, meaning cannot anchor itself in reusable concepts.
Without a graph, relationships remain implicit and undiscoverable.

Existing PKM apps store data, but they do not bridge the gap between **unstructured personal knowledge**, **semantic identity**, **entity-aware reasoning**, and **graph-driven retrieval**.

ContextHelp fills that gap.

---

## Vision

**ContextHelp becomes the personal and organizational “context layer” that powers intelligent agents.**

- A local-first, privacy-first engine
- A decentralized ecosystem of registries (taxonomies, entities, bookmark sources, weights)
- A unified pipeline system for multimodal AI enrichment
- A programmable substrate for agent worldviews
- A consistent interface for CLI, REST, and gRPC access
- A **semantic identity layer powered by mentions and canonical entities**
- A **knowledge graph connecting concepts, content, and meaning**

The outcome:
Agents that understand *your* universe, not a generic one.

---

## Core Concepts

### 1. Pipelines

Modular, event-driven processing graphs that transform raw content into structured knowledge.

Examples:

- `text.short`, `text.long`
- `url.generic`, `url.repo`
- `image.landing`, `image.ocr`
- `audio.transcript`
- `video.analysis`

Pipelines support:

- LLM reasoning
- OCR and metadata extraction
- embeddings
- **mention extraction and entity resolution**
- Decomposition into sections, tags, decisions, summaries

---

### 2. Registries

Registries provide structured meaning through external definitions.

Types:

- **Taxonomy Registries** — controlled vocabularies, tag definitions
- **Entity Registries** — canonical concepts, API surfaces, domain ontologies
- **Bookmark Registries** — shared knowledge sources
- **Weights Registries** — scoring, heuristics

Registry providers:

- Can be static, hosted, self-hosted, authenticated, or commercial
- Implement an open Registry Protocol
- Are composable, decentralized, and user-selectable

Users (or teams) choose the registries they trust.

---

### 3. Bookmark Schema

Represents enriched input:

- metadata
- structured sections
- tags
- decisions and rationales
- hints
- **mentions referencing canonical entities**
- provenance
- pipeline used
- registry influences

Bookmarks form the user’s **semantic knowledge graph**, stored locally and optionally enriched by registries.

---

### 4. Mentions & Entities

Mentions (`@ui.best-practice`, `@stripe.api.checkout`, `@component.form.input`) allow users and pipelines to reference **canonical concepts** with durable identity.

Entities:

- are defined locally or by registries
- provide stable semantic anchors
- include titles, descriptions, aliases, translations, metadata
- are versioned and namespace-aware

Mentions create **edges** between bookmarks and entities, enabling:

- semantic navigation
- linking
- filtering
- reasoning
- summarization

They form the backbone of contextual understanding.

---

### 5. Knowledge Graph

The knowledge graph connects:

- bookmarks → entities
- entities → entities
- clusters and inferred relationships
- backlinks and semantic neighborhoods

This enables:

- semantic browsing
- backlink navigation
- concept-driven summarization
- entity-based retrieval
- graph-powered agent reasoning
- emerging concept detection

It becomes the backbone of ContextHelp’s semantic navigation and retrieval engine.

---

### 6. Agent Profiles

Each agent views a curated, scoped subset of the user's knowledge.

An agent profile defines:

- active pipelines
- subscribed registries
- which entities matter to the agent
- retrieval preferences
- worldview shaping heuristics

Agents become **role-based**, **domain-aware**, and **semantically aligned** with the user’s conceptual universe.

---

### 7. Local-First Engine

ContextHelp operates without requiring cloud access.

Key properties:

- data never leaves the device unless explicitly allowed
- registries are optional
- offline mode is fully supported
- storage backends: JSON, SQLite, Postgres
- concurrency-safe, event-driven core

---

### 8. Developer Interfaces

Available interfaces:

- **CLI (`ch`)** — ingestion, retrieval, serving
- **REST API** — frontend and automation
- **gRPC API** — high-performance agent runtimes

All interfaces support **mention-based querying**, **entity lookup**, and **graph navigation**.

---

## Why Now?

Three converging forces make ContextHelp timely:

1. **Rise of AI agents** — They require durable personal context.
2. **Explosion of multimodal content** — Abundant but unstructured.
3. **Demand for digital sovereignty** — Users want control of their context and identity.

ContextHelp solves all three with a decentralized, extensible architecture enhanced by **entities + mentions + knowledge graph semantics**.

---

## Primary Use Cases

### **For Individuals**

- Personal knowledge graphing
- Entity-aware research workflows
- Concept-driven writing assistance
- Learning systems that adapt to user vocabulary

### **For Teams**

- Shared conceptual ontologies
- Semantic governance
- Context-aware internal assistants
- Consistent tagging + mention conventions

### **For Developers**

- Build context-driven agents
- Use entity schemas for domain alignment
- Integrate context into dashboards, BI tools, codebases

---

## Technical Architecture Overview

1. Input → Pipeline Inference
2. Pipeline Execution (LLM reasoning, **mention extraction**, metadata)
3. Registry-Based Semantic Alignment (taxonomies + entities)
4. Bookmark Creation
5. **Knowledge Graph Updates**
6. Retrieval (filtering by tags, mentions, entities)
7. Agent-Scoped Access
8. CLI/API Consumption

Each step is modular, testable, and replaceable.
See `docs/architecture.md` for details.

---

## Decentralization Model

ContextHelp defines the **protocol**, not the network.

Features:

- registries are peer-like and independent
- selective subscription
- worldview composition per agent or user
- no central authority
- compatible with private/commercial registries
- supports local-only, hosted, and distributed options

ContextHelp acts as the **sovereign client** of a semantic ecosystem.

---

## Roadmap

### Phase 1 — Core Engine (MVP)

- Go runtime
- Pipelines (text, URL, basic image)
- Local JSON storage
- CLI: analyze, list
- Basic registry support
- **Introduce mentions, entity schema, and graph core**

### Phase 2 — Full Pipelines + Registries

- Expanded multimodal pipelines
- Taxonomy + Entity registries
- Registry protocol v1
- Bookmark schema v1 (with mentions)
- SQLite/Postgres storage
- Graph building + backlink indexing

### Phase 3 — Agents + API

- Agent profiles
- Knowledge graph subsystem
- REST + gRPC APIs
- Multi-registry harmonization
- Entity resolution improvements

### Phase 4 — Ecosystem Extensions

- Plugin framework
- Paid/private registries
- Embedding search
- Graph visualization layer
- Optional cloud sync

---

## Why ContextHelp Will Matter

As AI systems become more autonomous, **context becomes the new compute**.

ContextHelp:

- restores user sovereignty
- empowers agents with personal, conceptual knowledge
- introduces the missing semantic identity layer via **entities**
- enables decentralized semantic ecosystems
- connects content with meaning through the **knowledge graph**
- establishes a new foundation for personal AI infrastructure

It is not just a tool — it is a **platform for contextual cognition**.

---

## Call for Participation

We are seeking early collaborators and contributors who are passionate about:

- AI agents
- decentralized architectures
- knowledge graphs
- semantic identity models
- multimodal pipelines
- open protocols

ContextHelp will be open source under AGPL-3.0.

If this resonates with your vision of the future, you're invited to help shape it.