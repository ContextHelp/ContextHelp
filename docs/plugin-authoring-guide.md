# Plugin Authoring Guide

**API ref:** `pkg/pluginapi/` — `github.com/ideacrafterslabs/ctxt/pkg/pluginapi`
**Host side:** `internal/plugin/`
**ADR:** [ADR-012](decisions/ADR-012-use-plugins-to-extend-any-layer.md)

---

## Contents

1. [Current state](#1-current-state)
2. [Plugin manifest](#2-plugin-manifest)
3. [Extension points](#3-extension-points)
4. [Implementing a plugin in Go](#4-implementing-a-plugin-in-go)
5. [Host registry](#5-host-registry)
6. [Capabilities and permissions](#6-capabilities-and-permissions)
7. [Scaffolding](#7-scaffolding)
8. [Testing your plugin](#8-testing-your-plugin)

---

## 1. Current state

`pkg/pluginapi` defines Go interfaces that in-process plugins implement, and
`internal/plugin.Registry` holds and initialises plugin instances. The following
does **not** exist today:

- **No plugin loading in the binaries.** `ctxt` and `dpkms` never create a
  plugin registry (`Service.PluginRegistry` is only set in tests), so no plugin
  is registered, initialised, or called at runtime.
- **No config.** The top-level `plugins:` config key is parsed into
  `[]PluginConfig{type, plugin, config}` (`internal/config/config.go`), but no
  code reads it.
- **No out-of-process plugins**, no plugin directory under the config dir, no
  `ctxt plugin` / `dpkms plugin` command.

Plugins exist as Go packages under `plugins/` and are exercised by their unit
tests and by `test/integration/us00{29,42,43,45}_*_test.go`, which register
plugins on a test service by hand.

To extend pipelines in a running dpkms today, use built-in steps and pipeline
config (`pipelines.overrides`), or install steps from a step registry with
`dpkms pipeline step install <name> --registry <url>`.

---

## 2. Plugin manifest

`pluginapi.PluginManifest` (`pkg/pluginapi/manifest.go`) is read from
`manifest.json` or `manifest.yaml` in a plugin directory by
`plugin.LoadManifest(dir)`. No host code calls `LoadManifest` yet.

```yaml
apiVersion: v1
name: local-registry
version: 1.0.0
description: Local filesystem registry
type: registry_provider
permissions:
  - filesystem
  - registry.read
```

| Field | Required | Notes |
|---|---|---|
| `apiVersion` | yes | must be `v1` |
| `name` | yes | should match `Plugin.Name()` |
| `version` | yes | semver string |
| `slug` | no | identifier |
| `description` | no | |
| `type` | no | only `registry_provider` is recognised |
| `permissions` | no | list; unknown values fail validation (see §6) |

Examples: `plugins/local-registry-plugin/manifest.yaml`,
`plugins/rss-feed/manifest.yaml`. Note `plugins/dir-watcher/manifest.json` uses a
different, older shape and does not pass `Validate()`.

---

## 3. Extension points

All interfaces are in `pkg/pluginapi/pluginapi.go` unless noted.

| Interface | Methods (besides `Plugin`) | Host accessor |
|---|---|---|
| `Plugin` | `Name`, `Version`, `Init`, `PipelineSteps`, `Close` | `Register`, `InitAll`, `CloseAll` |
| `PipelineStep` | `Name`, `Contract`, `Run` | `ExtraSteps()` |
| `PostIngestHook` | `PostIngest(ctx, obj)` | `PostIngestHooks()` |
| `AliasResolver` | `ResolveID(ctx, idOrAlias, profile)` | `AliasResolvers()`, `ResolveID()` |
| `OutputGenerator` | `GeneratorName`, `Accepts`, `Generate` | `OutputGenerators()`, `FindOutputGenerator()` |
| `PluginRegistryProvider` (`registry_provider.go`) | `Metadata`, `FetchTaxonomy`, `FetchEntities`, `ResolveEntity` | `RegistryProviders()` |

The host accessors exist on `internal/plugin.Registry`; apart from the alias
REST handlers (`internal/server/http/handlers_aliases.go`), no core code calls
them.

`Deps` passed to `Init` carries `Bus` (CloudEvents v1.0 `Event`, with `Publish`,
`Subscribe`, `Close`) and `Store` (a `StorageDriver` exposing only `Aliases()`).
Either may be nil; handle that.

Alias resolvers must return `(idOrAlias, nil)` for input they do not recognise.
`Registry.ResolveID` tries resolvers in registration order and takes the first
result that differs from the input.

---

## 4. Implementing a plugin in Go

### 4.1 Module setup

A plugin can live in its own Go module and import only `pkg/pluginapi`
(`internal/` is not importable from outside the module). There is no tagged
ctxt release, so point at a local checkout:

```
module github.com/you/ctxt-plugin-example

go 1.26.1

require github.com/ideacrafterslabs/ctxt v0.0.0

replace github.com/ideacrafterslabs/ctxt => /path/to/ctxt
```

`plugins/aliasing/go.mod` follows this pattern.

### 4.2 Core interface

```go
type Plugin interface {
    Name() string
    Version() string
    Init(ctx context.Context, cfg map[string]interface{}, deps Deps) error
    PipelineSteps() []PipelineStep
    Close(ctx context.Context) error
}
```

`Init` receives a raw config map. `Registry.InitAll(ctx, cfgs, deps)` passes
`cfgs[p.Name()]`, or an empty map. Read keys defensively:

```go
func (p *Plugin) Init(_ context.Context, cfg map[string]interface{}, deps pluginapi.Deps) error {
    if v, ok := cfg["endpoint"].(string); ok {
        p.endpoint = v
    }
    if p.endpoint == "" {
        return fmt.Errorf("example: endpoint required")
    }
    p.bus = deps.Bus
    return nil
}
```

### 4.3 Writing a pipeline step

```go
type FetchStep struct{ client *http.Client }

func (s *FetchStep) Name() string { return "example_fetch" }

func (s *FetchStep) Contract() pluginapi.StepContract {
    return pluginapi.StepContract{
        Requires: []string{"RawContent"},
        Produces: []string{"TextContent", "Metadata"},
    }
}

func (s *FetchStep) Run(ctx context.Context, draft *pluginapi.KnowledgeObject) (*pluginapi.KnowledgeObject, error) {
    // read draft.RawContent, set draft.TextContent and draft.Metadata
    return draft, nil
}
```

`Requires` / `Produces` name `KnowledgeObject` fields (dot notation such as
`Metadata.ocr_confidence` is allowed). `pipeline.ValidateComposability` checks
that each step's requirements are produced by earlier steps.

Main `KnowledgeObject` fields a step writes:

| Field | Type | Notes |
|---|---|---|
| `Graph` | `*ObjectGraph` | preferred target for structured content (ADR-063) |
| `TextContent` | `string` | normalised text |
| `Metadata` | `map[string]any` | enrichment |
| `Summaries`, `Sections`, `Tags`, `Mentions`, `Decisions`, `Tasks` | slices | flat fields |
| `Vectors` | `[]ObjectVector` | per-model embedding chunks; not serialised to JSON |
| `Plugins` | `map[string]any` | per-plugin namespace, persisted with the object |

Leave `ID`, `CreatedAt`, `ContentHash` and `Status` to core.

### 4.4 Graph nodes (ADR-063)

```go
if draft.Graph == nil {
    draft.Graph = &pluginapi.ObjectGraph{}
}
for i, label := range tags {
    draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
        ID:       pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeTag, i),
        NodeType: pluginapi.NodeTypeTag,
        Label:    label,
        Order:    i,
    })
}
```

Node types (`pkg/pluginapi/node_identity.go`): `section`, `tag`,
`entity_mention`, `decision`, `task`, `summary`, `code_block`, `open_question`,
`artifact`. Edge types: `contains`, `references`, `resolves_to`,
`derives_from` (intra-object only; inter-object edges use the edges table,
ADR-049). `pluginapi.NodeURI` builds a node URI. `pkg/projection` derives
`DocumentProjection` and `IndexProjection` from an object.

### 4.5 Post-ingest hook

```go
func (p *Plugin) PostIngest(ctx context.Context, obj *pluginapi.KnowledgeObject) error {
    if obj.Plugins == nil {
        obj.Plugins = map[string]any{}
    }
    obj.Plugins[p.Name()] = map[string]any{"score": computeScore(obj)}
    return nil
}
```

Whether mutations are saved is up to the caller; no core caller exists yet.

### 4.6 Reference implementations

| Package | Implements |
|---|---|
| `plugins/aliasing/` | `AliasResolver` (see [plugins-aliasing.md](plugins/plugins-aliasing.md)) |
| `plugins/autosuggest/` | pipeline step |
| `plugins/ica/` | pipeline steps (see [plugins-ica.md](plugins/plugins-ica.md)) |
| `plugins/markdown-export/` | `OutputGenerator` (`obsidian-md`) |
| `plugins/local-registry-plugin/` | `PluginRegistryProvider` |

---

## 5. Host registry

`internal/plugin.Registry` (`internal/plugin/registry.go`):

```go
reg := plugin.NewRegistry()
reg.Register(p)                      // panics on duplicate Name()
reg.RegisterWithManifest(p, m)       // also stores the manifest
err := reg.InitAll(ctx, map[string]map[string]interface{}{
    p.Name(): {"endpoint": "https://example.test"},
}, deps)
steps := reg.ExtraSteps()
defer reg.CloseAll(ctx)
```

This is the API the integration tests use. No binary calls it, and nothing maps
the `plugins:` config key onto the `InitAll` config map.

---

## 6. Capabilities and permissions

### Step capabilities

`StepContract.Capabilities` lists runtime requirements. When a built-in pipeline
is built, steps whose capabilities are unavailable are pruned (or rejected in
strict mode). Capability names the core sets
(`internal/pipeline/builtins/capabilities.go`): `io` (always), `llm`, `ocr`,
`vision`, `transcription`, `diarization` (provider-dependent), `blob-externalize`
(blob store configured), `browser` (browser client configured). Any other name is
never satisfied.

### Manifest permissions

Valid values (`pkg/pluginapi/manifest.go`): `read_objects`, `write_objects`,
`call_llm`, `network`, `filesystem`, `clipboard`, `refresh`, `notifications`,
`entity.write`, `entity.alias`, `registry.read`. `entity.write` and
`entity.alias` are listed as privileged.

`internal/plugin.CapabilityEnforcer` checks permissions for a few operations
(`FetchURL`, `ReadFile`, `WriteFile`, `TriggerRefresh`, `CreateNotification`, ...)
and `CheckPermissionChange` reports added permissions between two manifests.
Both live in `internal/`, so external plugins cannot call them, and no host code
uses them yet.

---

## 7. Scaffolding

```bash
dpkms dev init-plugin my-plugin            # creates plugins/my-plugin/
dpkms dev init-plugin my-plugin --dir ./x  # creates ./x/my-plugin/
```

It writes `manifest.yaml`, `plugin.go` (a `pluginapi.Plugin` stub) and
`README.md`. The generated manifest carries `apiVersion: v1` and passes
`Validate()` as written; the stub compiles against `pluginapi.Plugin`.

---

## 8. Testing your plugin

- Unit-test `Init`, each step's `Run` and `Close` directly. Pass
  `pluginapi.Deps{}` when the plugin needs no bus or store, and check that nil
  deps do not panic.
- Check that `Name()` matches the manifest `name`, and that `Contract()` lists
  every field the step reads and writes.
- Test `PostIngest` and `ResolveID` on constructed objects; for resolvers, check
  that unknown input passes through with no error.
- For an end-to-end run against a real service, follow
  `test/integration/us0042_enrichment_plugin_test.go`: register the plugin on
  `svc.PluginRegistry`, call `InitAll`, then upsert a pipeline that uses its step.

```bash
go test ./plugins/ica/... ./plugins/aliasing/...
```

---

## See also

- `pkg/pluginapi/pluginapi.go` — type definitions (read first)
- `pkg/pluginapi/README.md` — short API overview
- `internal/plugin/registry.go` — host registry
- [ADR-012](decisions/ADR-012-use-plugins-to-extend-any-layer.md) — design rationale
- [US-0042](stories/plugins/US-0042-implement-custom-enrichment-plugin.md), [US-0043](stories/plugins/US-0043-implement-custom-ai-provider-plugin.md), [US-0044](stories/plugins/US-0044-implement-registry-adapter-plugin.md), [US-0045](stories/plugins/US-0045-implement-custom-ranking-algorithm.md) — plugin stories
- [plugins-api.md](plugins/plugins-api.md) — earlier API spec
