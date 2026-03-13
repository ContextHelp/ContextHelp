# dPKMS + `ctxt` System — User Personas

This directory contains detailed profiles of the primary personas interacting with the dPKMS + `ctxt` system across different roles and contexts.

## Quick Overview

| Persona | File | Role | Primary Interaction |
|---------|------|------|-------------------|
| **Maintainers, Collaborators, Contributors** | [`maintainers.md`](./maintainers.md) | Project leadership & core team | Architecture, plugins, schema evolution |
| **Agents, LLMs, Tools** | [`agents-llms-tools.md`](./agents-llms-tools.md) | Autonomous systems | Structured queries, constrained enrichment, composition APIs |
| **Knowledge Workers / Context-Seeking Professionals** | [`knowledge-workers.md`](./knowledge-workers.md) | Humans (researchers, PMs, engineers) | Natural search, capture, composition via CLI/TUI |
| **Researchers & OSINT Analysts** | [`researchers-osint.md`](./researchers-osint.md) | Intelligence gatherers, competitive analysts | Authenticated capture, entity resolution, provenance tracking |
| **Platform Integrators** | [`platform-integrators.md`](./platform-integrators.md) | Third-party developers | Plugin development, custom backends, white-label |
| **Operations / DevOps** | [`operations.md`](./operations.md) | Production deployment & monitoring | Infrastructure, scaling, secrets, backups |

## System Philosophy

1. **dPKMS runs work correctly** — Mechanics guaranteed (idempotent, deterministic, safe)
2. **`ctxt` decides work is valuable** — Meaning decided by humans or agents (focus profiles, intent patterns)
3. **Together they provide context-as-a-service** — For humans and autonomous systems alike

Each persona leverages different layers of the system. See individual files for detailed goals, pain points, and system leverage.

## Persona Interaction Map

| Aspect | Maintainers | Agents/LLMs | Knowledge Workers | Integrators | Operations |
|--------|------------|-----------|-------------------|------------|-----------|
| **Query Path** | Design schema | RSQL (structured) | NLQ (natural) | Custom operators | Monitor queries |
| **Enrichment** | Define recipes | AI steps (constrained) | Automatic (background) | Custom providers | Monitor jobs |
| **Composition** | Design templates | Programmatic assembly | CLI/UI composition | Custom UI | Monitor composition |
| **Configuration** | `configuration.md` | `/query-schema` endpoint | Focus profiles | Plugin config schema | Env vars + YAML |
| **Plugins** | `plugins.md` | REST/gRPC API calls | Use pre-built plugins | Register plugins | Deploy plugins |
