# Documentation Guide

Welcome to the **dPKMS + `ctxt`** documentation.

This project consists of two packages with distinct concerns:

- **dPKMS** — Decentralized knowledge substrate (mechanics: storage, jobs, security, federation)
- **`ctxt`** — Agentic context brain (meaning: capture, enrichment, surfacing, composition)

Together they provide **context-as-a-service** for humans and AI agents (self-hosted nodes, or via **context.help cloud**).

---

## 📖 Quick Start

**New to the project?**
1. Read [developer-quickstart.md](developer-quickstart.md) - Developer onboarding
2. Read [cli-quickstart.md](cli-quickstart.md) - CLI quickstart
3. Read [architecture.md](architecture.md) - System-wide architecture
4. Read [design.md](design.md) - Consolidated design documentation
5. Review [dpkms-or-ctxt.md](dpkms-or-ctxt.md) - Package placement guide
6. Read [manual/README.md](manual/README.md) - Persona and workflow user manual

**Looking for specific capabilities?**
- [featureset.md](featureset.md) - Complete feature overview

**Want to understand terminology?**
- [glossary.md](glossary.md) - Shared vocabulary

**Need to configure the system?**
- [configuration-structure.md](configuration-structure.md) - Configuration file structure
- [environment-variables/README.md](environment-variables/README.md) - Environment variables

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
├── manual/                        # Persona and workflow user manual
│   ├── README.md                  # Manual index
│   ├── personas/                  # Persona quickstarts
│   ├── workflows/                 # Task-oriented workflows
│   ├── admin-extensibility/       # Admin, pipelines, plugins
│   ├── operations/                # Operations runbook
│   ├── reference/                 # API, query, config references
│   ├── troubleshooting/           # FAQ and issue playbooks
│   └── appendix/                  # Story/persona indexes and maturity
│
├── Getting Started
│   ├── developer-quickstart.md    # Developer onboarding
│   ├── cli-quickstart.md          # CLI quickstart guide
│   └── cross-package-contracts.md  # Interface contracts
│
├── Development
│   ├── development.md              # Development workflow
│   ├── development-infrastructure.md  # Dev environment setup
│   ├── dependencies.md            # Dependency management
│   ├── cli-implementation.md      # CLI implementation details
│   ├── configuration-structure.md # Configuration file structure
│   ├── ci-cd.md                   # CI/CD pipeline
│   ├── git-hooks.md               # Git hooks configuration
│   ├── branding.md                # Brand guidelines
│   └── quick-wins-strategy.md      # Quick wins strategy
│
├── Environment Variables
│   └── environment-variables/
│       ├── README.md              # Environment variables overview
│       ├── core.md                # Core settings
│       ├── storage.md             # Storage configuration
│       ├── api.md                 # API settings
│       ├── pipelines.md           # Pipeline configuration
│       ├── registries.md          # Registry configuration
│       ├── workers.md             # Worker settings
│       ├── ai-providers.md        # AI provider settings
│       ├── services.md            # Service configuration
│       ├── development.md         # Development settings
│       └── security.md            # Security settings
│
├── dpkms/                         # dPKMS substrate documentation
│   ├── README.md                  # dPKMS overview
│   ├── non-negotiables.md         # Design principles
│   ├── featureset.md              # dPKMS-specific features
│   ├── storage.md                 # Storage layer
│   ├── jobs-and-ingestion.md      # Job queue
│   ├── queue.md                   # Queue implementation
│   ├── query-language-spec.md     # Query engine
│   ├── knowledge-graph.md         # Graph index
│   ├── mentions.md                # Mention system
│   ├── schema-entity.md           # Entity schema
│   ├── schema-registry.md         # Registry schema
│   ├── registries.md              # Registry system
│   ├── registry-protocol.md       # Registry protocol
│   ├── registry-syncing-and-retrieval.md  # Sync mechanisms
│   ├── security.md                # Security model
│   ├── privacy.md                 # Privacy guarantees
│   ├── decentralization.md        # Decentralization model
│   ├── caching.md                 # Caching strategies
│   ├── embeddings.md              # Embedding support
│   ├── ranking-and-reranking.md    # Result ranking
│   ├── testing.md                 # Testing strategy
│   ├── domains.md                 # Domain management
│   └── personas/                  # dPKMS personas
│       └── README.md
│
├── ctxt/                          # ctxt brain documentation
│   ├── README.md                  # ctxt overview
│   ├── non-negotiables.md         # Design principles
│   ├── featureset.md              # ctxt-specific features
│   ├── api-cli.md                 # CLI reference
│   ├── tui.md                     # TUI reference
│   ├── webui.md                   # Web UI reference
│   ├── pipelines.md               # Enrichment recipes
│   ├── pipelines-reference.md     # Pipeline catalog
│   ├── schema-object.md           # Knowledge object schema
│   ├── schema-tag.md              # Tag schema
│   ├── schema-taxonomy.md         # Taxonomy schema
│   ├── tags.md                    # Tag semantics
│   ├── configuration.md           # Profiles & preferences
│   ├── hints.md                   # User hints
│   ├── interface-mappings.md      # Interface mappings
│   ├── domains.md                 # Domain-specific features
│   ├── user-stories.md            # User stories
│   ├── user-story.md              # Narrative example
│   ├── user-story-github-pr-insights.md  # Specific use case
│   ├── user-roles.md              # User roles
│   ├── personas-end-users.md      # End user personas
│   ├── personas-user-roles.md     # User role personas
│   ├── l10n-i18n.md              # Localization
│   └── testing.md                # Testing strategy
│
├── plugins/                       # Plugin system (cross-cutting)
│   ├── README.md                  # Plugin overview
│   ├── plugins.md                 # Plugin architecture
│   ├── plugins-api.md             # Plugin API contract
│   ├── plugin-isolation.md        # Plugin isolation
│   ├── plugins-notifications.md   # Notification plugin
│   ├── plugins-refresh.md         # Refresh plugin
│   ├── plugins-github-pr-review-watcher.md  # PR review plugin
│   └── examples/                  # Plugin examples
│
├── api/                           # External APIs
│   ├── README.md                  # API overview
│   ├── api-rest.md                # REST API reference
│   ├── api-grpc.md                # gRPC API reference
│   └── node-admin-api.md          # Node admin API
│
├── integrations/                  # External integrations
│   ├── README.md                  # Integration overview
│   └── leann-integration.md       # LEANN integration
│
├── security/                      # Security & secrets management
│   ├── README.md                  # Security documentation index
│   ├── secrets-validation-and-log-sanitization.md  # Comprehensive guide
│   ├── security-model.md          # Threat model & guarantees
│   ├── secret-management.md       # Lifecycle & best practices
│   ├── configuration-security.md  # Securing configuration files
│   ├── log-sanitization.md        # Log sanitization
│   ├── validation.md             # Security validation
│   └── model/                     # Security model docs
│       ├── README.md
│       ├── threat.md              # Threat modeling
│       ├── boundaries.md          # Security boundaries
│       ├── compliance.md          # Compliance requirements
│       └── controls.md            # Security controls
│
├── decisions/                     # Architectural Decision Records
│   └── ADR-*.md                   # Individual ADRs
│
├── cloud/                         # context.help cloud (hosted service)
│   ├── README.md                  # Cloud overview
│   ├── domains.md                 # Cloud-owned domain index
│   ├── node-enrollment.md         # Attaching nodes to the cloud
│   ├── admin-access.md            # Org + node access management
│   └── billing-and-credits.md     # Subscriptions + credits model
│
├── marketplace/                   # Registry + plugin marketplace
│   └── README.md                  # Marketplace overview
│
├── policy/                        # Entitlements, metering, receipts
│   ├── entitlements-and-metering.md
│   └── receipts-and-traceability.md
│
├── registries/                    # Publisher-facing registry docs
│   └── publishing.md              # Build a registry (thin sync + JIT pull)
│
├── deployment/                    # Deployment documentation
│   ├── README.md                  # Deployment overview
│   ├── 00-START-HERE.md           # Deployment guide
│   ├── WEB2-WEB3-HYBRID.md        # Hybrid deployment
│   ├── COMPARISON-MATRIX.md      # Deployment comparisons
│   ├── DEPLOYMENT-SCENARIOS.md   # Deployment scenarios
│   └── SYNC-ORCHESTRATION.md     # Sync orchestration
│
├── personas/                      # Personas documentation
│   ├── README.md                  # Personas overview
│   ├── agents-llms-tools.md       # AI agent personas
│   ├── knowledge-workers.md       # Knowledge worker personas
│   ├── maintainers.md            # Maintainer personas
│   ├── operations.md             # Operations personas
│   └── platform-integrators.md   # Platform integrator personas
│
├── stories/                       # User stories
│   ├── README.md                  # Stories overview
│   ├── admin/                     # Admin stories
│   ├── agents/                    # Agent stories
│   ├── composition/              # Composition stories
│   ├── enrichment/               # Enrichment stories
│   ├── ingestion/                # Ingestion stories
│   ├── operations/                # Operations stories
│   ├── plugins/                   # Plugin stories
│   └── search/                    # Search stories
│
├── blog/                          # Blog posts
│   ├── preamble.md                # Blog preamble
│   ├── pkms-pick-a-mess.md        # Blog post
│   ├── few-standards-many-vendors.md  # Blog post
│   └── taming-ms-model-stupidity.md    # Blog post
│
├── diagrams/                      # Documentation diagrams
│   └── ecosystem-overview.*        # Ecosystem visualizations
│
├── web/                           # context.help marketing site
│   ├── README.md                  # Web docs index
│   ├── messaging.md               # Messaging and copy
│   ├── target-audience.md         # Personas and JTBD
│   ├── design-system.md           # Visual direction
│   ├── stack.md                   # Web stack
│   ├── landing-page-copy.md       # Landing page copy
│   ├── brand-guidelines.md        # Brand guidelines
│   ├── logo-concepts.md           # Logo concepts
│   ├── drafts/                    # Draft content
│   └── assets/                    # Web assets
│       ├── favicon/
│       ├── icons/
│       │   └── feature/
│       ├── logo/
│       └── social/
│
├── sprints/                       # Sprint planning docs
│   └── 00*.md                     # Sprint documents
│
└── scaling.md                     # Scaling documentation
```

---

## 🎯 Documentation by Role

### For End Users
Start here to understand what the system does:
- [developer-quickstart.md](developer-quickstart.md) - Get started quickly
- [cli-quickstart.md](cli-quickstart.md) - CLI quickstart
- [manual/README.md](manual/README.md) - User manual by persona and workflow
- [architecture.md](architecture.md) - What the system is
- [ctxt/user-story.md](ctxt/user-story.md) - Daily usage narrative
- [ctxt/api-cli.md](ctxt/api-cli.md) - CLI commands
- [ctxt/tui.md](ctxt/tui.md) - TUI reference
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
- [developer-quickstart.md](developer-quickstart.md) - Developer onboarding
- [cli-quickstart.md](cli-quickstart.md) - CLI quickstart
- [development.md](development.md) - Development workflow
- [development-infrastructure.md](development-infrastructure.md) - Dev environment setup
- [design.md](design.md) - Design philosophy
- [decisions/](decisions/) - Architectural decisions (ADRs)
- [dpkms/testing.md](dpkms/testing.md) - Testing strategy
- [ctxt/testing.md](ctxt/testing.md) - Testing strategy
- [dpkms/non-negotiables.md](dpkms/non-negotiables.md) - dPKMS principles
- [ctxt/non-negotiables.md](ctxt/non-negotiables.md) - ctxt principles
- [git-hooks.md](git-hooks.md) - Git hooks configuration
- [ci-cd.md](ci-cd.md) - CI/CD pipeline
- [dependencies.md](dependencies.md) - Dependency management

### For Registry Maintainers
Building decentralized knowledge registries:
- [dpkms/registries.md](dpkms/registries.md) - Registry overview
- [dpkms/registry-protocol.md](dpkms/registry-protocol.md) - Protocol spec
- [dpkms/registry-syncing-and-retrieval.md](dpkms/registry-syncing-and-retrieval.md) - Sync mechanisms
- [dpkms/schema-registry.md](dpkms/schema-registry.md) - Registry schema
- [dpkms/schema-entity.md](dpkms/schema-entity.md) - Entity definitions

### For Publishers
Publishing paid registries and extensions:
- [registries/publishing.md](registries/publishing.md) - Registry packaging (thin sync + JIT pull)
- [policy/entitlements-and-metering.md](policy/entitlements-and-metering.md) - Subscriptions, credits, and gating
- [policy/receipts-and-traceability.md](policy/receipts-and-traceability.md) - Receipts, provenance, and watermarking
- [marketplace/README.md](marketplace/README.md) - Marketplace concepts

### For Cloud Admins
Using **context.help cloud** for teams:
- [cloud/README.md](cloud/README.md) - Cloud overview
- [cloud/node-enrollment.md](cloud/node-enrollment.md) - Node enrollment and trust
- [cloud/admin-access.md](cloud/admin-access.md) - Access management (org -> nodes)
- [cloud/billing-and-credits.md](cloud/billing-and-credits.md) - Billing and credits

### For Web and Marketing
Website and positioning:
- [web/README.md](web/README.md) - Web docs index
- [web/messaging.md](web/messaging.md) - Messaging and copy
- [web/target-audience.md](web/target-audience.md) - Target audience
- [web/design-system.md](web/design-system.md) - Web design direction
- [web/stack.md](web/stack.md) - Web stack
- [web/landing-page-copy.md](web/landing-page-copy.md) - Landing page content
- [web/brand-guidelines.md](web/brand-guidelines.md) - Brand guidelines
- [web/logo-concepts.md](web/logo-concepts.md) - Logo concepts
- [branding.md](branding.md) - Overall brand guidelines

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
- [security/log-sanitization.md](security/log-sanitization.md) - Log sanitization
- [security/validation.md](security/validation.md) - Security validation
- [security/model/](security/model/) - Security model (threat, boundaries, compliance, controls)
- [environment-variables/security.md](environment-variables/security.md) - Secure environment variable handling
- [deployment/](deployment/) - Deployment security patterns
- [scaling.md](scaling.md) - Scaling documentation

---

## 🧭 Documentation by Topic

### Core Concepts
- [architecture.md](architecture.md) - Two-package architecture
- [dpkms-or-ctxt.md](dpkms-or-ctxt.md) - Which package owns what
- [glossary.md](glossary.md) - Terminology guide
- [cross-package-contracts.md](cross-package-contracts.md) - Interface contracts
- [domains.md](domains.md) - Domain concepts

### Development & Setup
- [developer-quickstart.md](developer-quickstart.md) - Developer onboarding
- [cli-quickstart.md](cli-quickstart.md) - CLI quickstart
- [development.md](development.md) - Development workflow
- [development-infrastructure.md](development-infrastructure.md) - Dev environment setup
- [dependencies.md](dependencies.md) - Dependency management
- [cli-implementation.md](cli-implementation.md) - CLI implementation
- [configuration-structure.md](configuration-structure.md) - Config structure
- [git-hooks.md](git-hooks.md) - Git hooks
- [ci-cd.md](ci-cd.md) - CI/CD pipeline
- [branding.md](branding.md) - Brand guidelines

### Storage & Data
- [dpkms/storage.md](dpkms/storage.md) - Storage backends
- [dpkms/jobs-and-ingestion.md](dpkms/jobs-and-ingestion.md) - Job queue
- [dpkms/queue.md](dpkms/queue.md) - Queue implementation
- [ctxt/schema-object.md](ctxt/schema-object.md) - Knowledge objects
- [dpkms/embeddings.md](dpkms/embeddings.md) - Embedding support
- [dpkms/caching.md](dpkms/caching.md) - Caching strategies

### Semantic Identity
- [dpkms/knowledge-graph.md](dpkms/knowledge-graph.md) - Graph index
- [dpkms/mentions.md](dpkms/mentions.md) - Mention system
- [dpkms/schema-entity.md](dpkms/schema-entity.md) - Entity schema
- [dpkms/schema-registry.md](dpkms/schema-registry.md) - Registry schema
- [ctxt/tags.md](ctxt/tags.md) - Tag semantics
- [ctxt/schema-tag.md](ctxt/schema-tag.md) - Tag schema
- [ctxt/schema-taxonomy.md](ctxt/schema-taxonomy.md) - Taxonomy schema

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
- [configuration-structure.md](configuration-structure.md) - Configuration file structure
- [environment-variables/README.md](environment-variables/README.md) - Environment variables overview
- [environment-variables/core.md](environment-variables/core.md) - Core settings
- [ctxt/configuration.md](ctxt/configuration.md) - Focus profiles
- [ctxt/l10n-i18n.md](ctxt/l10n-i18n.md) - Localization

### User Experience
- [stories/](stories/) - User stories by category
- [manual/README.md](manual/README.md) - User manual by persona and workflow
- [ctxt/user-stories.md](ctxt/user-stories.md) - User stories
- [ctxt/user-story.md](ctxt/user-story.md) - Usage narrative
- [ctxt/user-story-github-pr-insights.md](ctxt/user-story-github-pr-insights.md) - Specific use case
- [ctxt/personas-end-users.md](ctxt/personas-end-users.md) - Personas
- [ctxt/user-roles.md](ctxt/user-roles.md) - User roles
- [personas/](personas/) - Comprehensive personas documentation

### Quality & Testing
- [dpkms/testing.md](dpkms/testing.md) - Testing strategy
- [ctxt/testing.md](ctxt/testing.md) - Testing strategy
- [security/validation.md](security/validation.md) - Security validation

### Deployment & Operations
- [deployment/](deployment/) - Deployment scenarios and strategies
- [scaling.md](scaling.md) - Scaling documentation
- [stories/operations/](stories/operations/) - Operations stories
- [personas/operations.md](personas/operations.md) - Operations personas

### Brand & Marketing
- [branding.md](branding.md) - Brand guidelines
- [web/](web/) - Web documentation and assets
- [blog/](blog/) - Blog posts

### Personas & Stakeholders
- [personas/](personas/) - Comprehensive personas documentation
- [stories/](stories/) - User stories by category

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

**Q: How do I get started?**
A: See [developer-quickstart.md](developer-quickstart.md) and [cli-quickstart.md](cli-quickstart.md)

**Q: How do I extend the system?**
A: See [plugins/plugins-api.md](plugins/plugins-api.md)

**Q: What's the query language?**
A: See [dpkms/query-language-spec.md](dpkms/query-language-spec.md)

**Q: How do registries work?**
A: See [dpkms/registries.md](dpkms/registries.md)

**Q: How do I use the CLI?**
A: See [ctxt/api-cli.md](ctxt/api-cli.md) and [cli-implementation.md](cli-implementation.md)

**Q: What are focus profiles?**
A: See [ctxt/configuration.md](ctxt/configuration.md)

**Q: How do I configure the system?**
A: See [configuration-structure.md](configuration-structure.md) and [environment-variables/](environment-variables/)

**Q: How do I deploy?**
A: See [deployment/](deployment/) for deployment scenarios and strategies

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
