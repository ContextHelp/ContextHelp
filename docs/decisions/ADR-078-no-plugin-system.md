# ADR-078 – No Plugin System

> **Status:** Accepted. Installed steps run in pipelines since the [ADR-058 amendment](ADR-058-external-step-execution-protocol.md#amendment-2026-10-14-implementation) (2026-10-14).
> **Date:** 2026-10-14
> **Author:** jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** ADR-012 (plugins extend any layer), ADR-027 (plugin isolation and sandboxing)
> **References:** ADR-004 (step-based pipeline), ADR-023 (authentication and authorization), ADR-058 (external step execution protocol), ADR-059 (runtime pipeline mutation), ADR-075 (external hook surface)

---

## Context

ADR-012 made plugins first-class and ADR-027 specified how to sandbox them. The code that followed never loaded a plugin: `pkg/pluginapi` declared the interfaces, `internal/plugin` held a registry, and eight modules sat under `plugins/`, but neither binary created the registry, read the `plugins:` config key or called a plugin. The only runtime traces were an HTTP route set fed by a plugin nothing loaded and a scaffold command for plugins nothing could run.

Meanwhile the extension surfaces people actually use grew outside that model: pipeline overrides in config, custom pipelines through the API, step registries, and registries for entities and taxonomies. Two more are designed outside it: out-of-process execution of installed steps (ADR-058) and pre/post hooks for other applications (ADR-075).

`pkg/pluginapi` had also become the home of the core knowledge-object model, so every package depended on a "plugin API" that served no plugin.

---

## Decision

**ctxt and dpkms have no plugin system and will not load plugins at runtime, now or later.** There is no in-process plugin API, no plugin registry, no plugin manifest and no `plugins:` config key.

ctxt and dpkms are extended through:

- **Pipelines**: `pipelines.overrides.<pipeline>` (`providers`, `skip_steps`, `insert_steps`) and custom pipelines (`dpkms pipeline create`, `POST /api/v1/pipelines`).
- **Steps**: built-in steps, plus steps installed from a step registry (`dpkms pipeline step install`). Installed steps run out of process under ADR-058 wherever `insert_steps` places them (since 2026-10-14; see the ADR-058 amendment).
- **Registries**: entity, taxonomy and step registries (`registries:` config and `ctxt registry` for knowledge registries, `dpkms pipeline step registry` for step registries).
- **Hooks**: ADR-075 pre and post hooks for applications that react to ctxt and dpkms events (accepted, not implemented yet).

Authentication is extended by putting dpkms behind a trusted proxy or an OIDC provider. That design is still to be written; it will extend ADR-023 and will not add a plugin layer.

---

## Rationale

- Nothing used the plugin layer. Keeping it cost a public Go package, a config key that did nothing, docs describing behaviour that did not exist, and tests for code with no caller.
- Every concrete plugin use case is covered by another surface: inserted steps and custom pipelines for processing (working today), registries for vocabularies (working today), hooks for integrations (designed in ADR-075).
- Out-of-process steps (ADR-058) and hooks (ADR-075) are designed to keep third-party code outside the daemon. In-process plugins would have needed the ADR-027 sandbox to get the same isolation.
- ctxt is unreleased, so removing the layer breaks no one.

---

## Consequences

- `plugins/`, `internal/plugin`, `pkg/pluginapi`, `pkg/projection`, `dpkms dev init-plugin` and the `/api/v1/suggestions` routes are removed.
- The knowledge-object model (`KnowledgeObject`, `ObjectGraph`, node identity, `NodeAwareFilter`) is defined in `internal/storage`. The step contract (`PipelineStep`, `StepContract`) is defined in `internal/pipeline`.
- A `plugins:` key in a config file is an unknown key: `ctxt config validate` warns about it like any other.
- `KnowledgeObject.Plugins` (the `objects.plugins` column) and the `admin:plugins` scope that guards step install and uninstall keep their names: renaming them changes stored data and issued tokens, which needs its own change. See the 2026-10-14 amendment.
- ADR-012 and ADR-027 stay as historical record, marked superseded.

---

## Alternatives considered

- **Keep the interfaces, load nothing.** This was the state before this ADR. Rejected: it documented an extension model that did not work.
- **Implement in-process loading (Go `plugin` package or embedded modules).** Rejected: the Go `plugin` package ties plugins to the exact toolchain and dependency versions of the binary, compiled-in modules are not plugins, and both run third-party code inside the daemon.
- **Out-of-process plugins over RPC.** Rejected: external steps (ADR-058, running since its 2026-10-14 amendment) and hooks (ADR-075, designed) cover that, scoped to the places where extension is needed.

---

## Amendment 2026-10-14

> **Status:** Accepted

ctxt is unreleased, so the leftovers the consequences above kept are removed outright, with no aliases:

- `KnowledgeObject.Plugins` and the `objects.plugins` column are gone. SQLite migration 045 and Postgres migration 25 drop the column; any value stored there is discarded. The column was never an FTS input, so no re-projection follows.
- The scope that guards step install and uninstall (`POST /steps/install`, `DELETE /steps/{name}`) is `admin:steps`, named after the resource those routes act on, as `admin:pipelines` and `admin:watches` are. `admin:plugins` is gone with no alias. Tokens get scopes from their roles, so an `admin` token holds `admin:steps` with no config change.
