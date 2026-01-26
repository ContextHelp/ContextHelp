# ADR-012 – Plugins Are First-Class and May Extend Any Layer (Revised & Expanded)

> **Status:** Accepted
> **Date:** 2025-10-05 (Revised 2025-12-09)
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp is a decentralized, local-first knowledge engine designed to adapt to radically different workflows, data modalities, environments, and user requirements. Users ingest content from heterogeneous sources—URLs, code, feeds, APIs, files, media—and rely on context-aware pipelines, semantic enrichment, agents, and registries.

No single binary can cover all workflows meaningfully. Many real use cases require:

- custom ingestion logic
- domain-specific enrichments
- specialized pipelines or observers
- integrations with external systems
- background automation
- enterprise workflows
- context-aware assistants
- semantic or operational extensions

To support this, ContextHelp must expose a safe, deterministic, versioned, well-documented plugin model that allows external modules to extend **any layer of the system** without altering the core binary or compromising reliability, privacy, or consistency.

The original ADR-012 declared plugins as first-class citizens.
This revised version expands ADR-012 with the **formal Plugin Interface Contract**, replacing the need for ADR-013 entirely.

---

## Decision

**Plugins are first-class citizens that may extend any layer of ContextHelp—including pipelines, storage adapters, registries, job scheduling, refresh rules, search operators, CLI commands, REST/gRPC endpoints, event hooks, and semantic enrichment—using a formal, versioned, permission-bound Plugin Interface Contract.**

Plugins may not modify internal core behavior and must operate exclusively through documented extension points, preserving determinism, privacy, and core stability.

---

## Rationale

### Why plugins must be first-class

1. **Decentralization:**
   Registries, vocabularies, ontologies, and enrichment logic must be free to evolve independently.

2. **Customization:**
   Users need workflows and pipelines tailored to domains such as design systems, documentation parsing, feeds, price tracking, and enterprise metadata extraction.

3. **Modularity:**
   Optional functionality—feeds, notifications, integrations—belongs in plugins, not core.

4. **Sovereignty:**
   Local-first architecture requires user-owned extensions, not monolithic binaries.

5. **Minimal core:**
   Core should remain deterministic, portable, and unbloated.

### Why we need a formal Plugin Interface Contract

Without a formal contract:

- Plugins may rely on internal functions not meant for public use
- Upgrades risk breaking plugin behavior
- Security and privacy boundaries remain unclear
- Plugins may modify state unsafely
- Extension surfaces become inconsistent

By defining stable interfaces, hooks, boundaries, and forbidden operations, plugins can grow powerful without jeopardizing system integrity.

---

## Consequences

### Positive

- Extreme extensibility without modifying core
- Clean boundaries and predictable behavior
- Enables ecosystem: registries, feed ingestion, price monitoring, notifications, semantic extensions
- Enterprise and professional workflows become simple to integrate
- Core remains small, stable, deterministic

### Negative

- More complexity in designing and maintaining plugin APIs
- Additional documentation and testing overhead
- Poorly designed plugins may degrade performance

### Neutral / Considerations

- Plugin permissions must be explicit and user-authorized
- Versioning and compatibility guarantees must be maintained
- Plugins should degrade gracefully when disabled

---

## Formal Plugin Interface Contract

The following contract defines **everything a plugin may or may not do**, across all subsystems.

---

## 1. Plugin Lifecycle Hooks

Plugins may subscribe to these deterministic events:

- `on_cli_start`
- `on_rest_request`
- `on_grpc_request`
- `post_ingest`
- `post_pipeline_step`
- `post_job_completed`
- `post_refresh`
- `plugin_event(type, payload)` (plugin-defined)

Plugins must not block or override core control flow.

---

## 2. Plugin Storage API

Plugins receive a dedicated storage namespace:

```
~/.contexthelp/plugins/<pluginName>/
```

Rules:

- JSON storage recommended
- atomic write helpers provided
- plugins may maintain their own directories, indexes, or metadata
- plugins must not write outside their namespace
- core will never mutate plugin-owned files

---

## 3. Bookmark Metadata Namespace

Plugins may store structured metadata ONLY under:

```
bookmark.plugins.<pluginName>
```

Example:

```
"plugins": {
  "rss_feed": { ... },
  "price_monitor": { ... }
}
```

Core guarantees:

- namespaced plugin metadata will not be overwritten
- merging logic preserves this namespace

Plugins may not write outside their namespace.

---

## 4. Plugin-Defined Bookmark Types

Plugins may register new bookmark types:

- `feed`
- `feed_item`
- `price_monitored_item`
- `semantic_fragment`
- etc.

Core must:

- treat unknown types as valid
- preserve their value
- allow pipelines to specialize on them

Plugins may not override core bookmark types.

---

## 5. Pipeline Extension API

Plugins may:

- register entire pipelines
- add pipeline steps
- wrap pipeline execution
- override pipeline inference for matching sources
- inject pre/post logic

Plugins may not:

- delete core pipeline steps
- override core semantics
- modify pipeline scheduling rules

---

## 6. Job Scheduling API

Plugins may:

- enqueue analysis jobs
- enqueue refresh jobs
- schedule delayed jobs
- attach pipeline overrides to jobs

Plugins may not:

- modify job state machine
- alter retry policies
- bypass transactional guarantees

---

## 7. Refresh Integration API

Plugins may request:

- refresh activation
- refresh interval
- minimum refresh interval
- fetch-new semantics for feed-like sources

Core validates:

- minimum safety thresholds
- user permissions
- system-level constraints

Plugins cannot disable refresh protections.

---

## 8. Search & Query Extension API

Plugins may:

- define new query operators
- define metadata fields usable in queries
- extend indexing within plugin namespace

Plugins may not:

- override built-in operators
- change semantic meaning of tag/hint/mention search
- modify entity system behavior

---

## 9. CLI/REST/gRPC Injection API

Plugins may:

- inject banners (e.g., notifications)
- add custom CLI commands (`ch rss sync`, `ch prices ls`)
- add plugin-specific endpoints under `/plugins/<pluginName>/`
- add gRPC services under `plugins.<pluginName>`

Plugins may not:

- override or delete core commands
- modify core CLI behavior
- alter existing REST or gRPC schemas

---

## 10. Inter-Plugin API

Plugins may expose API functions to other plugins via the plugin registry.

Example:

```
notifications.Create(type, level, payload)
```

Rules:

- only exported, declared APIs may be consumed
- plugins may not read another plugin’s storage directly
- cyclic dependencies are discouraged but allowed if intentional

---

## 11. Security & Permissions

Plugins:

- must explicitly request permissions (network, filesystem, clipboard, etc.)
- cannot bypass user consent
- cannot modify core configuration
- cannot access private core state
- cannot perform privileged OS operations

Core enforces:

- sandbox boundaries
- permission checks
- network access gating
- deterministic execution

---

## 12. Forbidden Plugin Actions

Plugins may NOT:

- modify core database schema
- override entity/hint/tag systems
- modify or delete bookmarks except those they created
- change pipeline scheduler behavior
- modify registry behavior
- inject into core command names
- bypass job transaction boundaries
- write outside plugin storage directory

---

## 13. Stability Guarantees

Core guarantees:

- lifecycle hook names remain stable or properly deprecated
- plugin storage paths remain stable
- bookmark plugin metadata namespace remains stable
- job and pipeline APIs follow semantic versioning rules

Plugins must:

- handle backward-compatible schema migrations
- gracefully handle missing optional hooks
- degrade safely if dependencies are unavailable

---

## Implementation Notes

- Add `plugin-api.md` as authoritative public specification
- Update `architecture.md` and `plugins.md` to reflect this ADR
- Implement lifecycle hooks in plugin engine
- Add runtime permission validations
- Update `schema-bookmark.md` with plugin metadata namespace
- Define plugin registry for inter-plugin APIs
- Create sample plugins using this contract (RSS, Refresh, Notifications, Price Monitor)

No core DB migrations are required.

---

## References

- ADR-003 – Read/write separation
- ADR-004 – Step-based pipelines
- ADR-005 – Decorator pattern for AI
- ADR-007 – Transactional outbox ingestion
- ADR-008 – Registry protocol
- ADR-009 – Multi-source retrieval
- ADR-010 – Query language
- **ADR-014 – Two-Package Architecture (dPKMS + ctxt)**
- Plugin architecture patterns (VSCode, Raycast, Obsidian)

---