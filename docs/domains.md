# Domain Index: dPKMS + `ctxt`

This document provides a comprehensive index of all domains across both the **dPKMS** (substrate) and **`ctxt`** (brain) packages, organized by responsibility and cross-cutting concerns.

**Last Updated:** 2026-01-27

---

## Package Overview

- **dPKMS** — Decentralized knowledge substrate providing mechanical guarantees (storage, jobs, security, federation)
- **`ctxt`** — Agentic context brain providing meaningful behaviors (capture, enrichment, surfacing, composition)

Together they provide **context-as-a-service** for humans and AI agents (self-hosted nodes, or via **context.help cloud**).

---

## Domain Counts

| Package | Domain Count | Documentation |
|---------|--------------|---------------|
| **dPKMS** | 15 domains | [`docs/dpkms/domains.md`](./dpkms/domains.md) |
| **`ctxt`** | 17 domains | [`docs/ctxt/domains.md`](./ctxt/domains.md) |
| **Cross-Cutting (shared contracts)** | 8 domains | Listed below |
| **Total (OSS)** | **40 domains** | |
| **Cloud (non-OSS)** | 6 domains | [`docs/cloud/domains.md`](./cloud/domains.md) |
| **Total (including cloud)** | **46 domains** | |

---

## Domain Classification

### dPKMS Domains (Substrate/Mechanics)
**15 domains** focused on **correctness, durability, and verifiable execution**

See detailed documentation: [`docs/dpkms/domains.md`](./dpkms/domains.md)

1. Storage Domain
2. Jobs & Ingestion Domain
3. Pipeline Runtime Domain
4. Query Engine Domain
5. Knowledge Graph Domain
6. Mentions Domain
7. Ranking & Reranking Domain
8. Registry & Federation Domain
9. Embeddings Domain
10. Caching Domain
11. Security Domain
12. Privacy Domain
13. Decentralization Domain
14. Schema Management Domain
15. Testing Domain (dPKMS)

---

### `ctxt` Domains (Brain/Meaning)
**17 domains** focused on **value, intelligence, and actionable outputs**

See detailed documentation: [`docs/ctxt/domains.md`](./ctxt/domains.md)

1. Capture & Ingestion Domain
2. Enrichment Pipeline Domain
3. Tags Domain
4. Taxonomy Domain
5. CLI Domain
6. TUI Domain
7. Web UI Domain
8. Configuration Domain
9. Hints Domain
10. L10n/i18n Domain
11. Schema (Object) Domain
12. User Roles & Personas Domain
13. User Stories Domain
14. Interface Mappings Domain
15. Testing Domain (ctxt)
16. API Domain
17. Plugin Integration Domain

---

## Cross-Cutting Domains

These **8 domains** span both packages with shared contracts and coordinated implementations.

### 1. Configuration & Environment Domain
**Locations:** `docs/configuration-structure.md`, `docs/environment-variables/`, `docs/ctxt/configuration.md`

**Responsibilities:**
- Unified configuration system across dPKMS and ctxt
- Environment variable definitions and precedence
- Configuration file formats (YAML, TOML, JSON)
- Validation and schema enforcement
- Secrets management integration
- Configuration migration strategies

**Cross-Package Coordination:**
- dPKMS exposes configuration primitives
- ctxt provides user-facing configuration interface
- Shared validation rules
- Consistent override hierarchy (defaults → global → workspace → env → CLI)

**Key Configuration Areas:**
- Storage backends
- AI providers
- Pipeline settings
- Registry connections
- Worker concurrency
- Security policies

---

### 2. Plugin Architecture Domain
**Locations:** `docs/plugins/`, `docs/plugins/plugins.md`, `docs/plugins/plugin-isolation.md`, `docs/plugins/plugins-api.md`

**Responsibilities:**
- Plugin discovery, loading, and lifecycle management
- Capability-based security model
- Plugin API contracts and versioning
- Isolation and sandboxing
- Event system and hooks
- Custom step registration

**Cross-Package Coordination:**
- dPKMS provides plugin runtime and capability enforcement
- ctxt provides plugin integration points (UI, CLI commands)
- Shared plugin manifest schema
- Coordinated security boundaries

**Plugin Integration Points:**
- Storage backends
- Pipeline steps (enrichment)
- Capture sources
- UI components
- Notification handlers
- Registry connectors

---

### 3. Security & Authentication Domain
**Locations:** `docs/security/`, `docs/dpkms/security.md`, `docs/security/model/`

**Responsibilities:**
- Encryption at rest and in transit
- Authentication mechanisms (API keys, OAuth, tokens)
- Authorization and RBAC
- Capability-based permissions
- Entitlements and metering (subscriptions, credits)
- Audit logging
- Receipts and traceability for high-leak operations (exports, JIT pulls)
- Cryptographic operations
- Secret management
- Tamper-evident history

**Cross-Package Coordination:**
- dPKMS provides cryptographic primitives and storage encryption
- ctxt provides user authentication flows
- Shared token validation
- Coordinated audit trails

**Security Features:**
- Pluggable key management
- Scoped workspace permissions
- Self-authenticating bundles
- Privacy-preserving search
- Secure plugin execution

---

### 4. Schema & Validation Domain
**Locations:** `docs/ctxt/schema-object.md`, `docs/ctxt/schema-tag.md`, `docs/ctxt/schema-taxonomy.md`, `docs/dpkms/schema-entity.md`, `docs/dpkms/schema-registry.md`

**Responsibilities:**
- Object schema definitions
- Entity schema definitions
- Tag and taxonomy schemas
- Registry schemas
- Schema versioning and evolution
- Validation rules and constraints
- Backward compatibility guarantees
- Custom field support

**Cross-Package Coordination:**
- dPKMS enforces storage-level schema constraints
- ctxt defines user-facing schemas and validation
- Shared schema migration tooling
- Coordinated versioning strategy

**Schema Types:**
- Knowledge objects (ctxt)
- Entities (dPKMS)
- Tags (ctxt)
- Taxonomies (ctxt)
- Registries (dPKMS)

---

### 5. Testing & Quality Assurance Domain
**Locations:** `docs/dpkms/testing.md`, `docs/ctxt/testing.md`

**Responsibilities:**
- Testing strategies and standards
- Mock implementations (AI, storage, registries)
- Integration test coordination
- E2E test scenarios
- Performance testing
- Security testing
- Accessibility testing

**Cross-Package Coordination:**
- Shared test utilities and mocks
- Coordinated E2E test scenarios
- Consistent quality gates
- Unified CI/CD pipelines

**Test Coverage:**
- Unit tests (both packages)
- Integration tests (cross-package)
- E2E workflows (capture → enrich → query → surface)
- Performance benchmarks
- Security audits

---

### 6. Observability & Monitoring Domain
**Locations:** Referenced in `docs/design.md`, `docs/dpkms/jobs-and-ingestion.md`

**Responsibilities:**
- Structured logging
- Metrics collection and export
- Distributed tracing
- Error tracking and reporting
- Performance profiling
- Audit trails
- Debug tooling

**Cross-Package Coordination:**
- dPKMS provides low-level telemetry
- ctxt provides user-facing monitoring
- Shared logging format and correlation IDs
- Coordinated trace propagation

**Observability Features:**
- Job execution traces
- Pipeline step timing
- Query performance metrics
- API request logging
- Error aggregation
- Health checks

---

### 7. Development Infrastructure Domain
**Locations:** `docs/development.md`, `docs/development-infrastructure.md`, `docs/developer-quickstart.md`, `docs/dependencies.md`

**Responsibilities:**
- Development environment setup
- Build tooling and scripts
- Dependency management
- Code generation
- Testing infrastructure
- Documentation generation
- Release management

**Cross-Package Coordination:**
- Shared build toolchain (Go)
- Coordinated dependency updates
- Unified CI/CD workflows
- Consistent coding standards

**Infrastructure Components:**
- Monorepo structure
- Task automation (Make, Just, Task)
- Linting and formatting
- Pre-commit hooks
- Version management

---

### 8. API & Integration Contracts Domain
**Locations:** `docs/api/`, `docs/ctxt/api-cli.md`, `docs/ctxt/interface-mappings.md`, `docs/cross-package-contracts.md`

**Responsibilities:**
- Cross-package interface definitions
- API versioning and compatibility
- Integration contract testing
- Breaking change management
- Interface evolution strategies
- Type conversions and adapters

**Cross-Package Coordination:**
- dPKMS exposes core interfaces (storage, graph, query, pipeline)
- ctxt implements and extends these interfaces
- Shared interface contracts
- Coordinated API versioning

**Key Contracts:**
- `ObjectStore` interface
- `Pipeline` interface
- `GraphStore` interface
- `QueryEngine` interface
- `VectorStore` interface
- `JobQueue` interface

Admin surfaces:
- Node Admin API (`docs/api/node-admin-api.md`)
- Reserved REST route prefix: `/admin/v1/*`

---

## Cloud Domains (Non-OSS)

Cloud-only domains (admin UI, org lifecycle, billing, marketplace, fleet ops)
live in:

- `docs/cloud/domains.md`

## Navigation

### Quick Links
- **[dPKMS Domains](./dpkms/domains.md)** — 15 substrate domains
- **[ctxt Domains](./ctxt/domains.md)** — 17 brain domains
- **[Architecture Overview](./architecture.md)** — System design
- **[Design Decisions](./decisions/)** — ADRs
- **[Cross-Package Contracts](./cross-package-contracts.md)** — Interface definitions

### Domain Lookup by Concern

**Storage & Persistence:**
- dPKMS: Storage, Caching, Schema Management
- Cross-Cutting: Configuration, Schema & Validation

**Knowledge Processing:**
- dPKMS: Pipeline Runtime, Jobs & Ingestion
- ctxt: Capture & Ingestion, Enrichment Pipeline
- Cross-Cutting: Plugin Architecture

**Search & Retrieval:**
- dPKMS: Query Engine, Knowledge Graph, Mentions, Ranking & Reranking
- ctxt: Hints

**Federation & Distribution:**
- dPKMS: Registry & Federation, Decentralization
- Cross-Cutting: API & Integration Contracts

**User Experience:**
- ctxt: CLI, TUI, Web UI, L10n/i18n, User Roles & Personas
- Cross-Cutting: Configuration & Environment

**Security & Privacy:**
- dPKMS: Security, Privacy
- Cross-Cutting: Security & Authentication

**Intelligence & Semantics:**
- dPKMS: Embeddings, Knowledge Graph
- ctxt: Enrichment Pipeline, Tags, Taxonomy

**Quality & Operations:**
- dPKMS: Testing (dPKMS)
- ctxt: Testing (ctxt)
- Cross-Cutting: Testing & QA, Observability & Monitoring, Development Infrastructure

**Extensibility:**
- ctxt: Plugin Integration
- Cross-Cutting: Plugin Architecture

---

## Maintenance Notes

This index is the authoritative source for domain organization. When adding new domains:

1. Determine package responsibility (dPKMS vs ctxt vs cross-cutting)
2. Update the appropriate domain document
3. Update this index
4. Ensure cross-package coordination is documented if applicable
5. Add ADR if domain introduces significant architectural decisions

**Last Major Reorganization:** 2026-01-26 — Split unified domains.md into package-specific documents
