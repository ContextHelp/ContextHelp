# ContextHelp

**A decentralized, local-first context engine built on two packages:**
- **dPKMS** (substrate) - Safe execution, storage, graph, federation
- **ctxt** (brain) - Intelligence, pipelines, composition, surfacing

> [!IMPORTANT]
> **New to the repo?** Read [CLAUDE.md](./CLAUDE.md) first. It contains critical instructions for AI agents and human contributors to work effectively in this workspace.

ContextHelp is a **semantic layer, not a replacement**. It augments Obsidian, Notion, Pocket, and similar
tools by adding stable entity identities, a queryable knowledge graph, and AI-ready retrieval on top of your
existing content — without asking you to abandon what already works.

---

## DNS for Concepts

The **dPKMS registry** system works like DNS, but for knowledge:

- DNS maps `stripe.com` → IP address; dPKMS maps `@stripe.api` → canonical knowledge object
- Just as you subscribe to DNS resolvers, you subscribe to knowledge registries
- Registries resolve `@stripe.api` to a canonical entity with stable ID, aliases, tags, and relationships
- Any content mentioning `@stripe.api` automatically backlinks to that canonical entity

> [!TIP]
> This structured concept identity is what makes ctxt different from "RAG on your notes" — it provides precise resolution, not just fuzzy text similarity.

---

## When NOT to Use ContextHelp

ContextHelp is deliberately scoped. It is **not** the right tool if:

- **You want a note-taking app** — use Obsidian or Notion.
- **You need real-time collaboration** — use Confluence or Notion.
- **Your notes don't have semantic complexity** — the setup cost won't pay off for simple journals.
- **You want zero-configuration** — ContextHelp requires configuration (registries, pipelines, profiles).

---

## Two-Package Architecture

ContextHelp is built as two independent but cooperating packages:

### dPKMS (Substrate)
**The execution and data layer**
- Transactional job queue, storage backends, knowledge graph, query engine.
- **Binary:** `dpkms` | **Focus:** Durability, correctness, sovereignty

### ctxt (Brain)
**The intelligence and behavior layer**
- Pipeline definitions, AI integrations, focus profiles, multi-source retrieval.
- **Binary:** `ctxt` | **Focus:** Intelligence, usability, actionability

---

## Quick Start

> [!NOTE]
> Detailed installation instructions are in [INSTALL.md](./INSTALL.md).

**Docker (Local Dev):**
```bash
docker compose --profile dev up
```

**Build from source:**
```bash
task build
./bin/dpkms serve
```

---

## Contribution & Standards

We've adopted high standards for contributions to ensure quality and interoperability:

- **Manifest:** Every contribution in `extensions/`, `plugins/`, or `skills/` must include a `manifest.json` file. See [.github/manifest.schema.json](./.github/manifest.schema.json) for the specification.
- **Agent Ready:** We use [CLAUDE.md](./CLAUDE.md) to maintain context for AI coding tools.
- **Visuals:** Use GitHub alerts and clear headers in all documentation.

Refer to [CONTRIBUTING.md](./CONTRIBUTING.md) for the full workflow.

