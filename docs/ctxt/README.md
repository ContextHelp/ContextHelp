# `ctxt` Documentation

This directory contains documentation for **`ctxt`** — the agentic context brain.

`ctxt` provides the **meaningful behaviors** that make the system frictionless, intelligent, and actionable.

## What `ctxt` Provides

- **Universal Capture** - CLI, TUI, browser, mobile — anywhere, instantly
- **Multimodal Enrichment** - Progressive processing from raw → refined
- **Focus Profiles** - Role and project lenses (Founder, Engineer, Research)
- **Just-In-Time Surfacing** - The right knowledge at the right moment
- **Composition Engine** - Briefs, plans, drafts from atomic knowledge
- **Safe Agent Execution** - Propose → dry-run → apply workflows
- **Search That Forgives** - Retrieval under uncertainty

## Key Principle

**`ctxt` decides which work is valuable. dPKMS guarantees it runs correctly.**

## Documentation Index

### Daily Interface
- [api-cli.md](api-cli.md) - CLI command reference (`ctxt` command)
- [cli-lateral.md](cli-lateral.md) - `ctxt lateral` daemon reference + config schema

### Capture & Sources
- [capture.md](capture.md) - `ctxt capture` command spec
- [ambient.md](ambient.md) - Ambient capture configuration (`policy/ambient.yaml`)
- [knowledge-directory.md](knowledge-directory.md) - Repo-local knowledge directory as a content source

### Enrichment
- [pipelines.md](pipelines.md) - Enrichment recipes and workflows
- [pipelines-reference.md](pipelines-reference.md) - Complete pipeline catalog

### Knowledge Model
- [schema-object.md](schema-object.md) - Knowledge object schema (successor to bookmarks)
- [tags.md](tags.md) - Tag semantics and usage
- [schema-tag.md](schema-tag.md) - Tag schema
- [schema-taxonomy.md](schema-taxonomy.md) - Taxonomy schema
- [hints.md](hints.md) - Hint system

### Localization
- [l10n-i18n.md](l10n-i18n.md) - Multilingual support (Arabic/English/French)

### Configuration & Profiles
- [configuration.md](configuration.md) - Focus profiles and preferences

### Users
- [user-roles.md](user-roles.md) - User role definitions
- [personas-end-users.md](personas-end-users.md) - End user personas
- [personas-user-roles.md](personas-user-roles.md) - User role personas
- [user-stories.md](user-stories.md) - User stories and requirements

## Non-Negotiables

See [non-negotiables.md](non-negotiables.md) for `ctxt` design principles:
- Frictionless
- Accessible
- Formless
- Polyglot
- Trusted
- Atomic
- Discoverable
- Evergreen
- Actionable

## See Also

- [../dpkms/](../dpkms/) - dPKMS substrate documentation
- [../architecture.md](../architecture.md) - System-wide architecture
- [../design.md](../design.md) - Consolidated design documentation
