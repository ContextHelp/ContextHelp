# Plugin System Documentation

This directory contains documentation for the **plugin system** — the extensibility layer for both dPKMS and `ctxt`.

Plugins enable powerful extensions without modifying core code, using stable, documented interfaces.

## Plugin Architecture

The plugin system is **cross-cutting** — plugins can extend both dPKMS (substrate) and `ctxt` (brain) layers through capability-based contracts.

## Documentation Index

### Core Documentation
- [plugins.md](plugins.md) - Plugin system overview and architecture
- [plugins-api.md](plugins-api.md) - Plugin API contract and interfaces

### Bundled Plugins
- [plugins-aliasing.md](plugins-aliasing.md) - Aliasing plugin: human-readable names for knowledge objects
- [plugins-audit-log.md](plugins-audit-log.md) - Audit log: immutable append-only record of object mutations (shipped in core, not a plugin module)

### Plugin Patterns
- [patterns-url-adapter-post-processor.md](patterns-url-adapter-post-processor.md) - URL adapter +
  post-processor patterns: interface contracts, annotated examples, composition guide

### Plugin Examples
- [plugins-notifications.md](plugins-notifications.md) - Notification plugin example
- [plugins-refresh.md](plugins-refresh.md) - Refresh policy plugin example
- [plugins-github-pr-review-watcher.md](plugins-github-pr-review-watcher.md) - GitHub PR review watcher plugin
- [examples/plugins-sample-price-monitor.md](examples/plugins-sample-price-monitor.md) - Price monitoring plugin

### Annotated Walkthroughs
- [examples/plugins-example-github-annotated.md](examples/plugins-example-github-annotated.md) -
  Step-by-step tutorial: GitHub PR watcher from scratch, inline design annotations

## Plugin Capabilities

### dPKMS Plugin Extensions
- Storage backends
- Encryption providers
- Query operators
- Registry connectors
- Graph algorithms
- Job types
- Pipeline runtime hooks

### `ctxt` Plugin Extensions
- Capture interfaces
- Enrichment recipes and pipeline steps
- Focus profiles and surfacing rules
- Composition templates
- Action extraction logic
- CLI commands and output formatters
- Translation and localization

### Cross-Layer Extensions
- AI providers (used by both)
- Background observers (screen watcher, etc.)
- Notification handlers
- Export/import formats

## Plugin Development

### Key Principles
- **Capability-scoped** - Explicit permissions required
- **Isolated** - Plugins own their storage and state
- **Versioned** - Stable API contracts
- **Safe** - Sandboxed execution

### Plugin Types
1. **Pipeline extensions** - Add new enrichment steps
2. **Storage backends** - Custom storage implementations
3. **Query operators** - New search capabilities
4. **Registry providers** - Custom registry connectors
5. **UI extensions** - Commands, formatters, interfaces
6. **Observers** - Background monitors and triggers

## See Also

- [../dpkms/](../dpkms/) - dPKMS substrate (provides plugin runtime)
- [../ctxt/](../ctxt/) - `ctxt` brain (uses plugins for enrichment)
- [../architecture.md](../architecture.md) - System architecture with plugin integration points
