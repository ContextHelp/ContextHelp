# dPKMS Documentation

This directory contains documentation for **dPKMS** — the decentralized, local-first knowledge substrate.

dPKMS provides the **mechanical guarantees** that make the system sovereign, durable, and verifiable.

## What dPKMS Provides

- **Durable Storage** - Local-first with pluggable backends
- **Transactional Jobs** - Crash-safe execution with deterministic replay
- **Semantic Identity** - Entities, mentions, and knowledge graph
- **Query Engine** - AST-based, explainable retrieval
- **Federated Registries** - Decentralized knowledge distribution
- **Security & Privacy** - Encryption, auth, integrity
- **Portability** - Export/import with zero lock-in

## Key Principle

**dPKMS runs work correctly. It does not decide what work is valuable.**

## Documentation Index

### Core Substrate
- [storage.md](storage.md) - Storage layer, backends, schema
- [jobs-and-ingestion.md](jobs-and-ingestion.md) - Transactional job queue
- [events.md](events.md) - CloudEvents-based event bus
- [query-language-spec.md](query-language-spec.md) - AST-based query engine
- [caching.md](caching.md) - Caching strategies

### Semantic Identity
- [knowledge-graph.md](knowledge-graph.md) - Graph index, edges, traversal
- [mentions.md](mentions.md) - Mention extraction and resolution
- [schema-entity.md](schema-entity.md) - Entity schema

### Federation
- [registries.md](registries.md) - Registry system overview
- [registry-protocol.md](registry-protocol.md) - Registry protocol spec
- [registry-syncing-and-retrieval.md](registry-syncing-and-retrieval.md) - Sync mechanisms
- [tiered-deployment.md](tiered-deployment.md) - Org / team / personal registry tiers
- [schema-registry.md](schema-registry.md) - Registry schema
- [ranking-and-reranking.md](ranking-and-reranking.md) - Result merging, reranking
- [decentralization.md](decentralization.md) - Decentralization model

### Security
- [security.md](security.md) - Encryption, auth, integrity
- [privacy.md](privacy.md) - Privacy model and guarantees

### Quality
- [testing.md](testing.md) - Testing strategies for dPKMS

## Non-Negotiables

See [non-negotiables.md](non-negotiables.md) for dPKMS design principles:
- Sovereign
- Durable
- Verifiable
- Self-authenticating
- Interoperable
- Federated
- Language-native
- Extensible
- Fast

## See Also

- [../ctxt/](../ctxt/) - `ctxt` agentic brain documentation
- [../architecture.md](../architecture.md) - System-wide architecture
- [../design.md](../design.md) - Consolidated design documentation
