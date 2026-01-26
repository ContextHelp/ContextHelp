# Branding & Naming Conventions

This document explains the naming conventions used throughout ContextHelp and clarifies what each name refers to.

---

## ContextHelp

**ContextHelp** is the overall project and product name.

- **Website:** https://context.help
- **Scope:** The complete personal knowledge engine system
- **Comprised of:** dPKMS (substrate) + ctxt (brain) + future interfaces (TUI, GUI, etc.)
- **Usage:** When referring to the entire system, project, or product

**Examples:**
- "ContextHelp is a local-first knowledge engine."
- "Contributing to ContextHelp requires understanding both dPKMS and ctxt."
- "Download ContextHelp to get started."

---

## dPKMS

**dPKMS** (lowercase "d", uppercase "PKMS") is the **execution substrate** package.

- **Pronunciation:** "dee-P-K-M-S"
- **Stands for:** Decentralized Personal Knowledge Management System
- **Role:** Provides the mechanics of running work safely
- **Binary:** `dpkms` (e.g., `dpkms serve`)
- **Package:** `github.com/ideacrafterslabs/ctxt/dpkms`

**dPKMS owns:**
- Transactional **Jobs Queue** (durable outbox)
- **Pipeline runtime** (step execution, isolation)
- **Storage** and **indexing** (SQLite, multilingual-safe)
- **Persistence**, **retries**, and **recovery guarantees**
- **State tracking**, **logs**, and **auditability**
- **Capability-scoped access** to storage, graph, query, crypto, registries

**dPKMS answers:**
- *Can this work run safely?*
- *Can it resume after a crash?*
- *Can we replay it deterministically?*
- *Can we prove what happened and why?*
- *Can multiple workers run it without corruption?*

**dPKMS does NOT decide:**
- *What work should be*
- *What pipeline to use for a specific input*
- *How to interpret or present results*
- *What language to translate into*

**Examples:**
- "dPKMS provides the job queue that ensures crash-safe ingestion."
- "The `dpkms serve` command runs the background worker."
- "Plugins that implement storage backends integrate with dPKMS."

**When to use "dPKMS":**
- Discussing storage, jobs, pipelines infrastructure
- Writing storage plugins
- Implementing background workers or daemons
- Testing infrastructure (not behavior)

---

## ctxt

**ctxt** (all lowercase) is the **user-facing brain** package.

- **Pronunciation:** "see-tekst" (rhymes with "context")
- **Stands for:** Context intelligence + user-facing behavior
- **Role:** Provides intent, meaning, and composition of work
- **Binary:** `ctxt` (e.g., `ctxt analyze`, `ctxt list`)
- **Package:** `github.com/ideacrafterslabs/ctxt/ctxt`

**ctxt owns:**
- **Pipeline definitions** (what steps, in what order)
- **AI provider** selection and configuration
- **Job types** ("ingest:url", "enrich:audio", "refresh:stale")
- **Multi-source retrieval** logic and reranking
- **User-facing CLI commands** and behavior
- **Multilingual behavior** (whether to translate, preferred languages)

**ctxt answers:**
- *What should we do with this input?*
- *What matters right now?*
- *How should this be enriched?*
- *What output should be generated?*
- *What should be surfaced to the user today?*
- *What language should this become useful in?*

**ctxt does NOT own:**
- *How jobs are stored or executed* (dPKMS responsibility)
- *Storage backends* (dPKMS responsibility)
- *Pipeline runtime mechanics* (dPKMS responsibility)

**Examples:**
- "Use `ctxt analyze` to ingest a new bookmark."
- "The `ctxt list` command shows your knowledge objects."
- "ctxt decides which pipeline to run based on input type and user profile."

**When to use "ctxt":**
- Discussing user-facing CLI commands
- Writing pipeline definitions or AI integration
- Implementing retrieval logic, reranking, or search
- User documentation, tutorials, and guides
- Discussing multilingual behavior

---

## Relationship Summary

```
ContextHelp (overall project)
│
├── dPKMS (substrate)
│   ├── Jobs queue (transactional outbox)
│   ├── Pipeline runtime
│   ├── Storage & indexing
│   └── CLI: dpkms serve (daemon)
│
└── ctxt (brain)
    ├── Pipeline definitions
    ├── AI provider selection
    ├── Multi-source retrieval
    └── CLI: ctxt analyze, ctxt list, ctxt search
```

**Key principle:** `ctxt` uses `dPKMS` to execute work. `dPKMS` does not depend on `ctxt` and can be used independently as a library or daemon.

---

## Naming in Different Contexts

### Git Repository
- Repository name: `ctxt` (refers to the overall project)
- Packages: `dpkms/` and `ctxt/`

### Documentation
- High-level docs refer to **ContextHelp**
- Infrastructure docs refer to **dPKMS**
- User-facing docs refer to **ctxt**

### CLI Commands
- Daemon/worker: `dpkms serve`
- User commands: `ctxt analyze`, `ctxt list`, `ctxt search`
- Both can be used together (ctxt enqueues jobs, dpkms executes them)

### Code Organization
- `/cmd/dpkms/` - Daemon binary
- `/cmd/ctxt/` - User-facing CLI binary
- `/internal/` - Shared code between packages
- `/dpkms/` - dPKMS-specific code
- `/ctxt/` - ctxt-specific code

### Plugin Development
- Storage plugins → integrate with **dPKMS**
- Pipeline plugins → integrate with **ctxt**
- AI provider plugins → integrate with **ctxt**
- Registry plugins → integrate with both (dPKMS protocol, ctxt usage)

---

## Quick Reference

| Name | Pronunciation | Binary | Primary Focus | Audience |
|------|--------------|--------|----------------|----------|
| ContextHelp | "Context Help" | N/A | Entire system | Everyone |
| dPKMS | "dee-P-K-M-S" | `dpkms` | Infrastructure | Developers, Sysadmins |
| ctxt | "see-tekst" | `ctxt` | User behavior | Users, Developers |

---

## Common Mistakes to Avoid

❌ **Incorrect:** "The ch CLI does ingestion."
✅ **Correct:** "The `ctxt analyze` command enqueues a job for ingestion."

❌ **Incorrect:** "dPKMS decides which pipeline to run."
✅ **Correct:** "ctxt decides which pipeline to run; dPKMS executes it."

❌ **Incorrect:** "The dpkms package provides user-facing search."
✅ **Correct:** "The ctxt package provides user-facing search; dpkms provides the underlying storage."

❌ **Incorrect:** "ContextHelp stores data in SQLite."
✅ **Correct:** "dPKMS (part of ContextHelp) stores data in SQLite."

---

## Why This Naming Matters

1. **Clear boundaries:** Separates "how it works" (dPKMS) from "what to do" (ctxt)
2. **Independent evolution:** Each package can be updated without breaking the other
3. **Multiple interfaces:** Future GUIs, TUIs, or mobile apps can use dPKMS as a library
4. **Testing infrastructure:** Easier to test storage and jobs separately from pipeline logic
5. **Documentation clarity:** Contributors know which code belongs to which concern

---

## See Also

- **docs/dpkms-or-ctxt.md** - Detailed architectural boundary specification
- **docs/decisions/ADR-014-two-package-architecture.md** - Architectural decision for two-package split
- **docs/decisions/README.md** - Index of all architectural decisions
