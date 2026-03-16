# US-0043: Implement Custom AI Provider Plugin

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/platform-integrators.md),
[Maintainers](../../personas/maintainers.md)

---

## User Goal

As a platform integrator, I want to implement a custom AI provider plugin so that I can
substitute a proprietary or self-hosted model (LLM, embedding, OCR, transcription, vision)
for the built-in backends without patching core code.

---

## Context

AI providers are selected per-type in `config.yaml` under `providers:`. Built-in backends
include `ollama`, `openai`, `anthropic`, `whisper-cli`, `tesseract`, etc. A custom AI provider
plugin contributes a `PipelineStep` that internally uses its own model client. Alternatively,
a plugin may inject a custom `ProviderBackendConfig` for an existing provider type.

The system exposes five provider types relevant to enrichment: `llm`, `embedding`, `ocr`,
`transcription`, `vision`. Config is injected at plugin `Init` via `cfg map[string]interface{}`.

Provider selection is resolved at server startup by `providers.Factory`; a plugin that wraps
a custom LLM backend should implement `pluginapi.PipelineStep` and expose it via
`PipelineSteps()`, where it uses the client initialised during `Init`.

---

## Acceptance Criteria

- **Plugin satisfies `pluginapi.Plugin`** — all five interface methods implemented
- **`Init` reads provider config** — endpoint, model, API key (from cfg or env) initialised
  without panicking on missing optional keys
- **Custom model client initialised** — connection to custom AI backend verified at `Init` time
  (or lazily on first call) without blocking server startup beyond a configured timeout
- **`PipelineSteps()` returns at least one step** that invokes the custom model
- **Step produces AI-enriched fields** — after `Run`, draft contains fields the step declares
  in `Contract().Produces` (e.g., `Summaries`, `Tags`, `Embeddings`)
- **Server-side storage updated** — resulting `KnowledgeObject` in DB contains model output
- **Config keys documented** — `endpoint`, `model`, `api_key` (or equivalent) are the
  documented config keys for the plugin
- **Errors propagate** — model request failures return errors that cause the job to fail with
  a retrievable error message in the `jobs` table

---

## Implementation Notes

### Plugin Skeleton

```go
type MyLLMPlugin struct {
    endpoint string
    model    string
    client   *http.Client
}

func (p *MyLLMPlugin) Name() string    { return "my-llm" }
func (p *MyLLMPlugin) Version() string { return "1.0.0" }

func (p *MyLLMPlugin) Init(
    ctx context.Context, cfg map[string]interface{}, _ pluginapi.Deps,
) error {
    p.endpoint, _ = cfg["endpoint"].(string)
    p.model, _ = cfg["model"].(string)
    if p.endpoint == "" {
        return fmt.Errorf("my-llm: endpoint required")
    }
    p.client = &http.Client{Timeout: 30 * time.Second}
    return nil
}

func (p *MyLLMPlugin) PipelineSteps() []pluginapi.PipelineStep {
    return []pluginapi.PipelineStep{&MyLLMStep{plugin: p}}
}

func (p *MyLLMPlugin) Close(_ context.Context) error { return nil }
```

### Step Contract

```go
func (s *MyLLMStep) Contract() pluginapi.StepContract {
    return pluginapi.StepContract{
        Requires:     []string{"RawContent"},
        Produces:     []string{"Summaries", "Tags"},
        Capabilities: []string{"llm"},
    }
}
```

### Config Declaration

```yaml
providers:
  llm:
    backend: auto  # built-in; override via plugin step in pipeline instead

plugins:
  - plugin: my-llm
    config:
      endpoint: https://my-llm.internal/v1
      model: my-model-v2
      api_key: ${MY_LLM_API_KEY}
```

### Per-Pipeline Provider Override

Custom providers can also be scoped to specific pipelines via `pipelines.<name>.providers`:

```yaml
pipelines:
  overrides:
    - name: legal-pipeline
      providers:
        llm:
          backend: my-llm
          endpoint: https://legal-llm.internal/v1
          model: legal-v3
```

### Error Propagation Path

`Run` returns `error` → job worker marks job `failed` → error stored in `jobs.error` column.

---

## E2E Test Checklist

- [ ] Build plugin with valid `Init` reading `endpoint` and `model` from cfg; start server —
  logs show `plugin: my-llm 1.0.0 initialised`
- [ ] `Init` called with missing `endpoint` → returns error; server logs error and refuses
  to start (or skips plugin with warning per policy)
- [ ] After server start, verify in-memory pipeline runtime includes `my_llm_step`
  (plugin steps are registered into the live pipeline runtime, not the `RegisteredStep` DB
  table that `dpkms pipeline step list` queries)
- [ ] Create pipeline including the custom LLM step; POST `/api/v1/pipelines` request body
  has `steps` as a **JSON string** (stringified array):
  `{"name":"my-llm-pipe","steps":"[{\"name\":\"my_llm_step\"}]"}`; response returns `id`
- [ ] Verify pipeline stored: `SELECT steps FROM pipelines WHERE name=?` — JSON includes
  the custom step name
- [ ] Enqueue content via `dpkms pipeline enqueue --pipeline my-llm-pipe <content>`; verify
  POST `/api/v1/pipelines/enqueue` payload contains
  `{"content":..., "pipeline":"my-llm-pipe"}`
- [ ] Plugin model endpoint receives request during job execution — verify via mock server log
  or request capture that payload includes content (not empty)
- [ ] After job completes, GET `/api/v1/objects/<id>` response includes fields declared in
  `Contract().Produces` (e.g., `summaries` non-empty, `tags` non-empty)
- [ ] Verify DB directly: `SELECT summaries, tags FROM knowledge_objects WHERE id=?` — JSON
  contains model-generated values
- [ ] Simulate model endpoint returning 500; verify job status becomes `failed` and
  `jobs.error` column contains descriptive error message
- [ ] Simulate model endpoint timeout; verify job fails with timeout error, not hang
- [ ] Two plugins with different names and endpoints both `Init` without conflict; both steps
  are available in the in-process pipeline runtime (verifiable by building pipelines that
  reference each step and confirming jobs complete without "step not found" errors)
- [ ] Stop server — `Close()` called on each plugin without error

---

## Related Stories

- [US-0042](./US-0042-implement-custom-enrichment-plugin.md) - Custom enrichment plugin
- [US-0044](./US-0044-implement-registry-adapter-plugin.md) - Registry adapter plugin
- [US-0045](./US-0045-implement-custom-ranking-algorithm.md) - Custom ranking algorithm
- [US-0029](../admin/US-0029-install-and-enable-plugin.md) - Install and enable plugin
- [US-0101](../pipelines/US-0101-create-custom-pipeline.md) - Create custom pipeline
- [US-0106](../pipelines/US-0106-enqueue-content-via-dpkms.md) - Enqueue content via dpkms
- [US-0027](../admin/US-0027-configure-ai-provider.md) - Configure AI provider (built-in)
