# Documentation Guide

Welcome to the **dPKMS + `ctxt`** documentation.

This project consists of two packages with distinct concerns:

- **dPKMS** — Decentralized knowledge substrate (mechanics: storage, jobs, security, federation)
- **`ctxt`** — Agentic context brain (meaning: capture, enrichment, surfacing, composition)

Together they provide **context-as-a-service** for humans and AI agents.

---

## 📖 Quick Start

**New to the project?**
1. Read [architecture.md](architecture.md) - System-wide architecture
2. Read [design.md](design.md) - Consolidated design documentation
3. Review [dpkms-or-ctxt.md](dpkms-or-ctxt.md) - Package placement guide

**Looking for specific capabilities?**
- [featureset.md](featureset.md) - Complete feature overview

**Want to understand terminology?**
- [glossary.md](glossary.md) - Shared vocabulary

---

## 📁 Documentation Structure

```
docs/
├── README.md                      # This file
├── architecture.md                # System architecture (both packages)
├── design.md                      # Consolidated design doc
├── glossary.md                    # Terminology
├── dpkms-or-ctxt.md               # Package placement guide
├── featureset.md                  # Complete feature set
│
├── dpkms/                         # dPKMS substrate documentation
│   ├── README.md                  # dPKMS overview
│   ├── non-negotiables.md         # Design principles
│   ├── featureset.md              # dPKMS-specific features
│   ├── storage.md                 # Storage layer
│   ├── jobs-and-ingestion.md      # Job queue
│   ├── query-language-spec.md     # Query engine
│   ├── knowledge-graph.md         # Graph index
│   ├── mentions.md                # Mention system
│   ├── schema-entity.md           # Entity schema
│   ├── registries.md              # Registry system
│   ├── registry-protocol.md       # Registry protocol
│   ├── security.md                # Security model
│   ├── privacy.md                 # Privacy guarantees
│   └── ...                        # More substrate docs
│
├── ctxt/                          # ctxt brain documentation
│   ├── README.md                  # ctxt overview
│   ├── non-negotiables.md         # Design principles
│   ├── featureset.md              # ctxt-specific features
│   ├── api-cli.md                 # CLI reference
│   ├── pipelines.md               # Enrichment recipes
│   ├── pipelines-reference.md     # Pipeline catalog
│   ├── schema-object.md           # Knowledge object schema
│   ├── tags.md                    # Tag semantics
│   ├── configuration.md           # Profiles & preferences
│   ├── user-stories.md            # User stories
│   ├── user-story.md              # Narrative example
│   └── ...                        # More brain docs
│
├── plugins/                       # Plugin system (cross-cutting)
│   ├── README.md                  # Plugin overview
│   ├── plugins.md                 # Plugin architecture
│   ├── plugins-api.md             # Plugin API contract
│   ├── plugins-notifications.md   # Notification plugin
│   ├── plugins-refresh.md         # Refresh plugin
│   └── examples/
│       └── plugins-sample-price-monitor.md
│
├── api/                           # External APIs
│   ├── README.md                  # API overview
│   ├── api-rest.md                # REST API reference
│   └── api-grpc.md                # gRPC API reference
│
├── integrations/                  # External integrations
│   ├── README.md                  # Integration overview
│   ├── leann-integration.md       # LEANN integration
│   ├── glm-pov.md                 # GLM integration
│   └── gemini-pov.md              # Gemini integration
│
├── security/                      # Security & secrets management
│   ├── README.md                  # Security documentation index
│   ├── secrets-validation-and-log-sanitization.md  # Comprehensive guide
│   ├── security-model.md          # Threat model & guarantees
│   ├── secret-management.md       # Lifecycle & best practices
│   └── configuration-security.md  # Securing configuration files
│
├── decisions/                     # Architectural Decision Records
│   └── ADR-*.md                   # Individual ADRs
│
└── sprints/                       # Sprint planning docs
    └── 00*.md                     # Sprint documents
```

---

## 🎯 Documentation by Role

### For End Users
Start here to understand what the system does:
- [architecture.md](architecture.md) - What the system is
- [ctxt/user-story.md](ctxt/user-story.md) - Daily usage narrative
- [ctxt/api-cli.md](ctxt/api-cli.md) - CLI commands
- [featureset.md](featureset.md) - What's possible

### For Developers
Building with or extending the system:
- [architecture.md](architecture.md) - System architecture
- [dpkms/README.md](dpkms/README.md) - Substrate layer
- [ctxt/README.md](ctxt/README.md) - Brain layer
- [plugins/plugins-api.md](plugins/plugins-api.md) - Plugin development
- [api/api-rest.md](api/api-rest.md) - REST API
- [api/api-grpc.md](api/api-grpc.md) - gRPC API

### For Contributors
Contributing to the codebase:
- [design.md](design.md) - Design philosophy
- [decisions/](decisions/) - Architectural decisions (ADRs)
- [dpkms/testing.md](dpkms/testing.md) - Testing strategy
- [dpkms/non-negotiables.md](dpkms/non-negotiables.md) - dPKMS principles
- [ctxt/non-negotiables.md](ctxt/non-negotiables.md) - ctxt principles

### For Registry Maintainers
Building decentralized knowledge registries:
- [dpkms/registries.md](dpkms/registries.md) - Registry overview
- [dpkms/registry-protocol.md](dpkms/registry-protocol.md) - Protocol spec
- [dpkms/registry-syncing-and-retrieval.md](dpkms/registry-syncing-and-retrieval.md) - Sync mechanisms
- [dpkms/schema-registry.md](dpkms/schema-registry.md) - Registry schema
- [dpkms/schema-entity.md](dpkms/schema-entity.md) - Entity definitions

### For System Integrators
Integrating with external systems:
- [integrations/](integrations/) - Integration examples
- [api/api-rest.md](api/api-rest.md) - REST endpoints
- [api/api-grpc.md](api/api-grpc.md) - gRPC services
- [plugins/plugins.md](plugins/plugins.md) - Plugin architecture

### For Security & DevOps Teams
Managing secrets, compliance, and secure deployments:
- [security/README.md](security/README.md) - Security documentation overview
- [security/secrets-validation-and-log-sanitization.md](security/secrets-validation-and-log-sanitization.md) - How to detect and protect secrets
- [security/security-model.md](security/security-model.md) - Threat model and guarantees
- [security/secret-management.md](security/secret-management.md) - Rotation, incident response, and best practices
- [security/configuration-security.md](security/configuration-security.md) - Securing configuration files
- [environment-variables.md#security](environment-variables.md#security) - Secure environment variable handling
- [scaling.md](scaling.md) - Deployment security patterns

---

## 🧭 Documentation by Topic

### Core Concepts
- [architecture.md](architecture.md) - Two-package architecture
- [dpkms-or-ctxt.md](dpkms-or-ctxt.md) - Which package owns what
- [glossary.md](glossary.md) - Terminology guide

### Storage & Data
- [dpkms/storage.md](dpkms/storage.md) - Storage backends
- [dpkms/jobs-and-ingestion.md](dpkms/jobs-and-ingestion.md) - Job queue
- [dpkms/queue.md](dpkms/queue.md) - Queue implementation
- [ctxt/schema-object.md](ctxt/schema-object.md) - Knowledge objects

### Semantic Identity
- [dpkms/knowledge-graph.md](dpkms/knowledge-graph.md) - Graph index
- [dpkms/mentions.md](dpkms/mentions.md) - Mention system
- [dpkms/schema-entity.md](dpkms/schema-entity.md) - Entity schema
- [ctxt/tags.md](ctxt/tags.md) - Tag semantics
- [ctxt/schema-tag.md](ctxt/schema-tag.md) - Tag schema

### Enrichment & Pipelines
- [ctxt/pipelines.md](ctxt/pipelines.md) - Enrichment recipes
- [ctxt/pipelines-reference.md](ctxt/pipelines-reference.md) - Pipeline catalog
- [ctxt/hints.md](ctxt/hints.md) - User hints

### Search & Retrieval
- [dpkms/query-language-spec.md](dpkms/query-language-spec.md) - Query language
- [dpkms/ranking-and-reranking.md](dpkms/ranking-and-reranking.md) - Result ranking
- [dpkms/caching.md](dpkms/caching.md) - Caching strategies

### Federation & Registries
- [dpkms/registries.md](dpkms/registries.md) - Registry system
- [dpkms/registry-protocol.md](dpkms/registry-protocol.md) - Protocol spec
- [dpkms/registry-syncing-and-retrieval.md](dpkms/registry-syncing-and-retrieval.md) - Sync
- [dpkms/decentralization.md](dpkms/decentralization.md) - Decentralization model

### Security & Privacy
- [security/README.md](security/README.md) - Security documentation index
- [security/secrets-validation-and-log-sanitization.md](security/secrets-validation-and-log-sanitization.md) - Complete guide to secrets and log sanitization
- [security/security-model.md](security/security-model.md) - Threat model and security guarantees
- [security/secret-management.md](security/secret-management.md) - Secret lifecycle and best practices
- [security/configuration-security.md](security/configuration-security.md) - Securing configuration files
- [dpkms/security.md](dpkms/security.md) - dPKMS security model
- [dpkms/privacy.md](dpkms/privacy.md) - Privacy guarantees

### Extensibility
- [plugins/plugins.md](plugins/plugins.md) - Plugin architecture
- [plugins/plugins-api.md](plugins/plugins-api.md) - Plugin API
- [plugins/examples/](plugins/examples/) - Plugin examples

### Configuration & Profiles
- [ctxt/configuration.md](ctxt/configuration.md) - Focus profiles
- [ctxt/l10n-i18n.md](ctxt/l10n-i18n.md) - Localization

### User Experience
- [ctxt/user-stories.md](ctxt/user-stories.md) - User stories
- [ctxt/user-story.md](ctxt/user-story.md) - Usage narrative
- [ctxt/personas-end-users.md](ctxt/personas-end-users.md) - Personas
- [ctxt/user-roles.md](ctxt/user-roles.md) - User roles

### Quality & Testing
- [dpkms/testing.md](dpkms/testing.md) - Testing strategy

---

## 🔑 Key Principles

### dPKMS (Substrate)
- **Sovereign** - Full ownership, no vendor lock-in
- **Durable** - Crash-safe, resumable operations
- **Verifiable** - Provenance and receipts
- **Federated** - Decentralized knowledge distribution
- **Fast** - Instant-feeling at scale

See [dpkms/non-negotiables.md](dpkms/non-negotiables.md)

### `ctxt` (Brain)
- **Frictionless** - Zero-resistance capture
- **Accessible** - Same brain, every interface
- **Formless** - Accepts reality as-is
- **Polyglot** - Multilingual knowledge
- **Actionable** - Insight creates movement

See [ctxt/non-negotiables.md](ctxt/non-negotiables.md)

---

## 💡 Common Questions

**Q: Where does X belong - dPKMS or ctxt?**
A: See [dpkms-or-ctxt.md](dpkms-or-ctxt.md)

**Q: How do I extend the system?**
A: See [plugins/plugins-api.md](plugins/plugins-api.md)

**Q: What's the query language?**
A: See [dpkms/query-language-spec.md](dpkms/query-language-spec.md)

**Q: How do registries work?**
A: See [dpkms/registries.md](dpkms/registries.md)

**Q: How do I use the CLI?**
A: See [ctxt/api-cli.md](ctxt/api-cli.md)

**Q: What are focus profiles?**
A: See [ctxt/configuration.md](ctxt/configuration.md)

---

## 📝 Contributing to Documentation

Documentation contributions are welcome! When adding or updating docs:

1. **Determine package ownership** - Use [dpkms-or-ctxt.md](dpkms-or-ctxt.md) to place docs correctly
2. **Update relevant READMEs** - Keep subdirectory indexes current
3. **Cross-reference appropriately** - Link between related docs
4. **Follow structure** - Maintain the organization shown above
5. **Update this README** - Add new docs to the appropriate section

---

## 🗺️ Related Documentation

- **Root project docs** - `../README.md`, `../PROPOSAL.md`, `../ROADMAP.md`
- **Decisions** - [decisions/](decisions/) - ADRs documenting key choices
- **Sprints** - [sprints/](sprints/) - Iteration planning and history

---

This documentation evolves continuously. Contributions, questions, and improvements are encouraged.
