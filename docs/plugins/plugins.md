# Plugins

A plugin is an in-process Go package that implements interfaces from
`pkg/pluginapi` ([ADR-012](../decisions/ADR-012-use-plugins-to-extend-any-layer.md)).
This page is the overview; [plugin-authoring-guide.md](../plugin-authoring-guide.md)
is the reference.

## Current state

- **No runtime loading.** Neither `ctxt` nor `dpkms` creates a plugin registry
  (`Service.PluginRegistry` is set only in tests), so no plugin is registered,
  initialised or called. Plugins under `plugins/` run in their own unit tests and
  in integration tests that register them by hand.
- **No configuration.** The top-level `plugins:` key is parsed into
  `[]PluginConfig{type, plugin, config}` and read by no code; `ctxt config
  validate` warns (`! ignored key`) when a config file sets it. There is no
  `plugins.<name>` block.
- **No plugin directory, commands or routes.** No `~/.contexthelp/plugins/`,
  no `ctxt plugin` / `dpkms plugin` command, no plugin-contributed CLI
  commands, REST or gRPC routes.
- **No sandbox.** Plugins are in-process code. Manifest `permissions` are
  validated; nothing enforces them.

## Extension points

| Interface | What it adds | Host accessor on `internal/plugin.Registry` |
|---|---|---|
| `Plugin` | `Name`, `Version`, `Init(ctx, cfg, deps)`, `PipelineSteps`, `Close` | `Register`, `InitAll`, `CloseAll` |
| `PipelineStep` | a step with a `Requires` / `Produces` contract | `ExtraSteps()` |
| `PostIngestHook` | `PostIngest(ctx, obj)` after an object is stored | `PostIngestHooks()` |
| `AliasResolver` | alias → object ID | `AliasResolvers()`, `ResolveID()` |
| `OutputGenerator` | an output format | `OutputGenerators()`, `FindOutputGenerator()` |
| `PluginRegistryProvider` | taxonomy + entities for registry sync | `RegistryProviders()` |

Apart from the alias REST handlers (`internal/server/http/handlers_aliases.go`),
no core code calls these accessors. `Init` receives a raw config map and `Deps`
with a CloudEvents `Bus` and a `Store` exposing only `Aliases()`; either may be
nil.

## Per-object plugin data

Knowledge objects carry a `plugins` map (`object.plugins.<pluginName>`),
persisted as a JSON column on SQLite and PostgreSQL. Core treats it as opaque.

```json
"plugins": {
  "rss-feed": { "guid": "abc123", "first_seen": 1710000000 }
}
```

## Manifest

One `manifest.json` or `manifest.yaml` in the plugin directory (`manifest.json`
wins if both exist): `apiVersion: v1`, `name`, `version`, optional `slug`,
`description`, `type: registry_provider`, `permissions`. `plugin.LoadManifest(dir)`
validates it; no host code calls it.
See [the authoring guide §2](../plugin-authoring-guide.md#2-plugin-manifest).

## Bundled plugin packages

`plugins/aliasing`, `autosuggest`, `content-monitor`, `dir-watcher`, `ica`,
`local-registry-plugin`, `markdown-export`, `rss-feed`.

## Extending a running dpkms today

Use built-in steps and `pipelines.overrides`, or install steps from a step
registry with `dpkms pipeline step install <name> --registry <url>`.

## Related

- [plugin-authoring-guide.md](../plugin-authoring-guide.md) — interfaces, manifest, scaffolding, tests
- [plugins-aliasing.md](plugins-aliasing.md), [plugins-ica.md](plugins-ica.md) — bundled plugins
