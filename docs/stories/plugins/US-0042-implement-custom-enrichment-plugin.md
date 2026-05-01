# US-0042: Implement Custom Enrichment Plugin

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/platform-integrators.md),
[Maintainers](../../personas/maintainers.md)

---

## User Goal

As a platform integrator, I want to implement a custom enrichment plugin so that I can
add domain-specific enrichment steps (e.g., legal classification, sentiment, custom tagging)
to the ctxt pipeline without modifying core code.

---

## Context

Plugins implement `pluginapi.Plugin` from `pkg/pluginapi`. An enrichment plugin contributes
one or more `PipelineStep` implementations via `PipelineSteps()`. The server loads plugins at
startup, injects `Deps` (event bus + storage driver), calls `Init`, then registers each step
returned by `PipelineSteps()` into the pipeline runtime.

Config is declared in `config.yaml` under `plugins:` as a list of `{plugin, config}` entries.
The `plugin` field must match the `Name()` return value of the plugin implementation.

The plugin receives enrichment input via `Run(ctx, draft *KnowledgeObject)` and writes output
fields (e.g., `Tags`, `Sections`, `Metadata`) back to the draft before returning it.

---

## Acceptance Criteria

- **Plugin satisfies `pluginapi.Plugin`** — `Name()`, `Version()`, `Init()`, `PipelineSteps()`,
  `Close()` all implemented
- **`Init` receives config map and `Deps`** — plugin reads its config keys without panicking on
  missing keys
- **`PipelineSteps()` returns at least one step** with a valid `StepContract` declaring
  `Requires` and `Produces` fields
- **Step registered in pipeline runtime** — after server start the in-process pipeline runtime
  includes the step; `dpkms pipeline step list` only queries the `RegisteredStep` DB table
  (file-system / registry steps) and will not list in-process plugin steps there
- **Step runs in pipeline** — when enqueued content is processed via a pipeline that includes
  the custom step, the step's `Run` method is called and output fields are written to the
  resulting `KnowledgeObject`
- **Server-side storage updated** — after pipeline completes, the `KnowledgeObject` in the DB
  reflects the fields written by the custom step (e.g., new tags, metadata entries, sections)
- **Plugin config isolated** — config keys defined under `plugins[n].config` are accessible
  only to the named plugin; other plugins are unaffected
- **Graceful close** — `Close()` is called on server shutdown without errors

---

## Implementation Notes

### Plugin Interface

```go
// implement in your plugin module
type MyEnrichmentPlugin struct{ /* fields */ }

func (p *MyEnrichmentPlugin) Name() string    { return "my-enrichment" }
func (p *MyEnrichmentPlugin) Version() string { return "1.0.0" }

func (p *MyEnrichmentPlugin) Init(
    _ context.Context, cfg map[string]interface{}, deps pluginapi.Deps,
) error {
    // read cfg["model"], cfg["threshold"], etc.
    return nil
}

func (p *MyEnrichmentPlugin) PipelineSteps() []pluginapi.PipelineStep {
    return []pluginapi.PipelineStep{&MyStep{}}
}

func (p *MyEnrichmentPlugin) Close(_ context.Context) error { return nil }
```

### Step Contract

```go
type MyStep struct{}

func (s *MyStep) Name() string { return "my_enrichment_step" }

func (s *MyStep) Contract() pluginapi.StepContract {
    return pluginapi.StepContract{
        Requires:     []string{"RawContent"},
        Produces:     []string{"Tags", "Metadata"},
        Capabilities: []string{"llm"},
    }
}

func (s *MyStep) Run(
    ctx context.Context, draft *pluginapi.KnowledgeObject,
) (*pluginapi.KnowledgeObject, error) {
    // enrich draft.Tags, draft.Metadata, etc.
    return draft, nil
}
```

### Config Declaration

```yaml
plugins:
  - plugin: my-enrichment
    config:
      model: legal-v2
      threshold: 0.7
```

### Module Setup

```
require github.com/ideacrafterslabs/ctxt v0.0.0
replace github.com/ideacrafterslabs/ctxt => /path/to/ctxt
```

### Registration at Server Startup

Plugin must be registered in `cmd/dpkms/cmd/serve.go` (or equivalent wire-up):

```go
pluginReg.Register(myenrichment.New())
pluginReg.InitAll(ctx, cfg.PluginConfigs(), deps)
pipes.RegisterExtraSteps(pluginReg.ExtraSteps())
```

### Storage Schema Reference

After pipeline execution, enrichment output is stored in the `knowledge_objects` table.
Fields written by the step (`Tags`, `Metadata`, `Sections`, etc.) are serialised as JSON.

---

## E2E Test Checklist

- [ ] Build plugin implementing `pluginapi.Plugin` + `PipelineStep`; verify `go build` succeeds
- [ ] Register plugin in server startup; start `dpkms serve` — server logs show
  `plugin: my-enrichment 1.0.0 initialised`
- [ ] After server start, verify in-memory pipeline runtime includes `my_enrichment_step`
  (e.g., via server startup log or by building a pipeline that references it and confirming
  the worker can run it — plugin steps are registered into the live pipeline runtime, not
  the `RegisteredStep` DB table that `dpkms pipeline step list` queries)
- [ ] Create pipeline that includes `my_enrichment_step`; verify POST `/api/v1/pipelines`
  request body has `steps` as a **JSON string** (stringified array):
  `{"name":"my-enrich-pipe","steps":"[{\"name\":\"my_enrichment_step\"}]"}`
- [ ] Verify pipeline creation response returns `id` and pipeline is persisted:
  `SELECT * FROM pipelines WHERE name=?` — row present with `steps` JSON including step name
- [ ] Enqueue content via `dpkms pipeline enqueue --pipeline my-enrich-pipe <content>`; verify
  POST `/api/v1/pipelines/enqueue` request body contains
  `{"content":..., "pipeline":"my-enrich-pipe"}`
- [ ] Wait for job completion; query resulting `KnowledgeObject` via GET
  `/api/v1/objects/<id>` — response includes field(s) populated by the custom step
  (e.g., `tags` array non-empty, `metadata` contains step-specific keys)
- [ ] Query DB directly: `SELECT tags, metadata FROM knowledge_objects WHERE id=?` — JSON
  fields reflect values written by the step
- [ ] Pass invalid config key in `plugins[0].config` — `Init` returns descriptive error; server
  logs error and refuses to start (or skips plugin with logged warning per policy)
- [ ] Stop server (`SIGTERM`) — `Close()` called without error; server exits cleanly
- [ ] Plugin with duplicate `Name()` value panics at `Register` call — confirm panic message
  includes the duplicate name string

---

## Related Stories

- [US-0043](./US-0043-implement-custom-ai-provider-plugin.md) - Custom AI provider plugin
- [US-0044](./US-0044-implement-registry-adapter-plugin.md) - Registry adapter plugin
- [US-0045](./US-0045-implement-custom-ranking-algorithm.md) - Custom ranking algorithm
- [US-0029](../admin/US-0029-install-and-enable-plugin.md) - Install and enable plugin
- [US-0101](../pipelines/US-0101-create-custom-pipeline.md) - Create custom pipeline
- [US-0106](../pipelines/US-0106-enqueue-content-via-dpkms.md) - Enqueue content via dpkms
- [US-0108](../pipelines/US-0108-install-step-from-registry.md) - Install step from registry

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [OSS Go Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/contributors/oss-go-developer.md)

---

## E2E Tests

- `test/integration/us0042_enrichment_plugin_test.go::TestUS0042_PluginSatisfiesInterface`
- `test/integration/us0042_enrichment_plugin_test.go::TestUS0042_DuplicatePluginNamePanics`
- `test/integration/us0042_enrichment_plugin_test.go::TestUS0042_StepRunsAndOutputMerged`
- `test/integration/us0042_enrichment_plugin_test.go::TestUS0042_PluginConfigIsolated`
