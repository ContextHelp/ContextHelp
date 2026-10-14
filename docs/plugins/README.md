# Plugin System Documentation

Documentation for the plugin API (`pkg/pluginapi`). The binaries do not load
plugins yet: no plugin runs at runtime, `plugins:` config is read by no code,
and there is no plugin command, directory or sandbox. Start with
[plugins.md](plugins.md) for the current state, then the
[authoring guide](../plugin-authoring-guide.md).

Pages describing hooks, events, refresh, notification or storage APIs beyond
the interfaces in `pkg/pluginapi` are design notes, not the running system.

## Documentation Index

### Core Documentation
- [plugins.md](plugins.md) - Current state, extension points, manifest
- [plugins-api.md](plugins-api.md) - Plugin API design notes (hooks and services beyond `pkg/pluginapi` are not implemented)

### Bundled Plugins
- [plugins-aliasing.md](plugins-aliasing.md) - Aliasing plugin: human-readable names for knowledge objects
- [plugins-audit-log.md](plugins-audit-log.md) - Audit log: immutable append-only record of object mutations (shipped in core, not a plugin module)

### Plugin Patterns
- [patterns-url-adapter-post-processor.md](patterns-url-adapter-post-processor.md) - URL adapter +
  post-processor patterns: interface contracts, annotated examples, composition guide

### Design examples (not buildable on the current API)
- [plugins-notifications.md](plugins-notifications.md) - Notification plugin; what exists instead
- [plugins-refresh.md](plugins-refresh.md) - Refresh plugin; what exists instead
- [plugins-github-pr-review-watcher.md](plugins-github-pr-review-watcher.md) - GitHub PR review watcher plugin spec
- [examples/plugins-sample-price-monitor.md](examples/plugins-sample-price-monitor.md) - Price monitoring plugin; closest real code
- [examples/plugins-example-github-annotated.md](examples/plugins-example-github-annotated.md) - GitHub plugin walkthrough; GitHub support today

## Plugin Capabilities

Interfaces a plugin can implement: `Plugin`, `PipelineStep`, `PostIngestHook`,
`AliasResolver`, `OutputGenerator`, `PluginRegistryProvider`. Storage backends,
encryption, query operators, CLI commands, formatters and background observers
are not plugin points. See [plugins.md](plugins.md#extension-points).

## See Also

- [../dpkms/](../dpkms/) - dPKMS substrate
- [../ctxt/](../ctxt/) - `ctxt` brain (uses plugins for enrichment)
- [../architecture.md](../architecture.md) - System architecture with plugin integration points
