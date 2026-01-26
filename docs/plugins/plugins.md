# Plugins

ContextHelp provides a powerful, modular plugin architecture enabling developers to extend or override nearly every subsystem **without modifying core**. Plugins remain isolated, sandboxed, composable, deterministic, and fully optional.

This document defines the plugin model, lifecycle events, allowed extension points, plugin-owned storage, configuration patterns, and the isolation guarantees that ensure plugins never need direct access to core internals.

---

## Overview

A **plugin** is an extension package that registers itself with the engine at startup and declares:

- capabilities
- event subscriptions
- pipeline extensions
- storage usage
- configuration schema
- optional CLI/API routes
- optional background behavior

Plugins can safely add new behaviors by relying on:

- **plugin event hooks**
- **plugin-defined pipelines**
- **plugin-owned storage**
- **plugin-owned knowledge object metadata**
- **plugin-defined knowledge object types**
- **job enqueue API**
- **refresh configuration API**
- **inter-plugin services**

Plugins do not modify the core database schema, query engine, pipeline engine, or storage layer directly.

---

## Design Principles

### Local-first

Plugins default to offline-safe behavior. Any network usage requires explicit opt-in via configuration.

### Decentralized

Plugins may introduce new registries or remote services but must not assume any global central authority.

### Composable

Plugins should extend the system without interfering with other plugins unless explicitly configured to do so.

### Deterministic

Plugins should yield stable results given the same input and configuration.

### Isolated

Plugins must operate strictly through sanctioned APIs, events, and namespaces.

---

## Plugin Capabilities

A plugin can independently provide any of the following capabilities:

- new pipelines or pipeline steps
- new knowledge object types
- new query operators or AST visitors
- new scoring/reranking logic
- new translation or localization engines
- new storage backends
- new registry providers
- new focus profile extensions
- new background tasks
- new CLI or API routes
- new analysis flows
- new notifier services (e.g., notification plugin)

Plugins declare capabilities at registration.

---

## Plugin Registration

Plugins register using a standard manifest and initialization function.

Conceptual example:

```
RegisterPlugin({
  name: "rss-feed",
  version: "1.0.0",
  capabilities: ["pipeline", "bookmark-type", "scheduler"],
  configKey: "rssFeed",
  events: [
    "pipeline.infer.after",
    "bookmark.create.after",
    "refresh.configure"
  ]
})
```

A plugin must declare:

- name
- version
- supported config block
- event subscriptions
- optional CLI/API contributions
- pipeline registrations
- knowledge object type declarations

---

## Plugin Storage Model

Plugins may store their own state under:

```
~/.contexthelp/plugins/<plugin_name>/
```

Recommended structure:

```
state.json
history.json
cache/
```

Rules:

- plugins **must not** modify the core SQLite/Postgres storage
- plugin storage is fully isolated
- plugins manage their own migrations and cleanup
- storage format must remain JSON-compatible

This enables plugins like:

- RSS Feed Plugin
- Notification Plugin
- Price Monitor Plugin

without any core schema changes.

---

## Plugin Metadata Namespace in Knowledge Objects

Plugins store metadata under:

```
object.plugins.<pluginName>
```

Example:

```
"plugins": {
  "rssFeed": {
    "item_pipeline": "text.long",
    "guid": "abc123",
    "first_seen": 1710000000
  },
  "priceMonitor": {
    "current_price": 199.99,
    "threshold_percent": 10
  }
}
```

This guarantees that plugin data remains isolated and conflict-free.

---

## Plugin-Defined Knowledge Object Types

Plugins may introduce new knowledge object types:

- `feed`
- `feed_item`
- `price_monitored`
- `api_resource`
- domain-specific types

The plugin declares these types during registration.

Core guarantees:

- knowledge object type registration is dynamic
- plugins may override pipeline inference for their types
- queries may filter on plugin types via `type:<name>`

No schema changes are required.

---

## Plugin Pipelines

Plugins may register:

- new pipeline kinds
- new pipeline steps
- pipeline decorators
- pipeline inference extensions

Example:

```
pipelines.register("feed.fetch", steps=[...])
pipelines.register("feed.parse", steps=[...])
```

Plugins can also enqueue jobs for their own pipelines using the Job API.

---

## Job Enqueue API for Plugins

Plugins may schedule jobs without core modification:

```
jobs.Enqueue({
  pipeline: "feed.parse",
  input: {...},
  metadata: { plugin: "rssFeed" }
})
```

Plugins may schedule:

- refresh jobs
- parsing jobs
- monitoring jobs
- enrichment jobs

Jobs remain isolated and traceable.

---

## Plugin Integration with Refresh System

Plugins may require refresh behavior for certain knowledge objects:

```
refresh.ConfigureForObject(objectID, {
  enabled: true,
  interval_seconds: 3600,
  fetch_new: true
})
```

Available to any plugin:

- RSS feed plugins force periodic checks
- Price monitor plugins require price refresh
- API resource plugins require stale-check intervals

Core validates permissions and merges with global refresh rules.

---

## Inter-Plugin Communication

Plugins may expose lightweight internal service APIs.

Example (Notification Plugin):

```
notifications.Create({
  type: "price_drop",
  level: "warning",
  payload: {...}
})
```

Plugins must explicitly import the other plugin’s service by name.

Core does not mediate inter-plugin calls; plugins only communicate through declared service interfaces.

---

## Event System

Plugins subscribe to engine events.

### Pipeline Events

- `pipeline.infer.before`
- `pipeline.infer.after`
- `pipeline.execute.before`
- `pipeline.execute.after`
- `pipeline.error`
- `pipeline.complete`

### Knowledge Object Events

- `object.create.before`
- `object.create.after`
- `object.update.before`
- `object.update.after`

### Job Events

- `job.create`
- `job.start`
- `job.complete`
- `job.fail`

### Refresh Events

- `refresh.configure`
- `refresh.evaluate.before`
- `refresh.evaluate.after`
- `refresh.error`

### CLI/API Events

- `cli.start`
- `cli.finish`
- `api.response.before`
- `api.response.after`

### Focus Profile Events

- `profile.prepare`
- `profile.query.before`
- `profile.query.after`

Plugins use these to extend system behavior deterministically.

---

## CLI & API Extensions

Plugins may:

- add new CLI commands
- wrap CLI output (e.g., show notifications)
- expose REST endpoints under namespaced routes
- expose gRPC services under plugin namespaces

Plugins may **not** override core commands unless explicitly configured.

---

## Configuration System

Plugins define configuration schemas under:

```
plugins.<pluginName>
```

Example:

```
plugins:
  rssFeed:
    enabled: true
    default_interval_seconds: 3600
  priceMonitor:
    enabled: true
    threshold_percent: 10
```

Plugins receive validated configuration blocks at initialization.

---

## Security & Isolation Rules

Plugins operate under strict boundaries:

- no direct DB access
- no global mutable state
- no unauthorized network calls
- no file writes outside plugin directory
- no SQL injections or schema alterations
- no overriding core types unless explicitly configured

These boundaries prevent plugin-induced core instability.

---

## Recommended Architectural Patterns

- **Event-driven design** for all plugin behaviors
- **Decorator patterns** for pipeline enrichment
- **Adapter patterns** for remote services
- **Visitor patterns** for AST query extension
- **Command patterns** for CLI tooling
- **State isolation** for plugin-owned data

---

## Example Plugins (Realistic Workflows)

- **RSS Feed Plugin**
  Fetches feed, parses items, schedules item ingestion.

- **Price Monitor Plugin**
  Extracts prices, tracks changes, triggers notifications.

- **Notification Plugin**
  Provides inter-plugin alerting system.

- **Refresh Plugin**
  Ensures dynamic sources are periodically updated.

- **Custom Storage Backend (Postgres)**
  Replaces or augments backend.

---

## Summary

The plugin system enables ContextHelp to remain:

- minimal
- fast
- stable
- extensible
- decentralized
- predictable

Plugins can introduce new semantic layers, pipelines, monitoring logic, feeds, integrations, notifications, and more **without ever touching the core engine**.

Core remains small and permanent.
Plugins make ContextHelp limitless.