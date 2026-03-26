# Plugin Authoring Guide

**Author:** $USER
**System:** ctxt / dPKMS
**API ref:** `pkg/pluginapi/` — `github.com/ideacrafterslabs/ctxt/pkg/pluginapi`
**ADR:** [ADR-012](decisions/ADR-012-use-plugins-to-extend-any-layer.md)

---

## Contents

1. [What plugins are](#1-what-plugins-are)
2. [Plugin manifest](#2-plugin-manifest)
3. [Extension points](#3-extension-points)
4. [Implementing a plugin in Go](#4-implementing-a-plugin-in-go)
5. [Config declaration](#5-config-declaration)
6. [Registering a plugin](#6-registering-a-plugin)
7. [Capabilities and permissions](#7-capabilities-and-permissions)
8. [Example: URL-fetch adapter plugin](#8-example-url-fetch-adapter-plugin)
9. [Testing your plugin](#9-testing-your-plugin)

---

## 1. What plugins are

Plugins extend ctxt/dPKMS without modifying core. They operate through **documented
extension points** only. Core guarantees backward-compatible plugin API across minor
versions (see §7 for constraints).

Two deployment models exist:

| Model | Binary | Load mechanism |
|---|---|---|
| In-process | Same binary as server | `pluginReg.Register(p)` at startup |
| Out-of-process | Separate binary | RPC/socket (planned; ADR-012 §2) |

Current production model: **in-process**. Plugin authors ship a Go package; the host
binary wires it in at startup.

Plugin data namespace on `KnowledgeObject`:

```
obj.Plugins["<plugin-name"]  →  map[string]any
```

Core preserves this namespace; merges never overwrite it.

---

## 2. Plugin manifest

Every plugin directory under `~/.config/contexthelp/plugins/<name>/` carries a
`plugin.json` manifest. For in-process plugins the manifest is informational;
for future out-of-process loading it governs dispatch.

```
{
  "name":               "url-fetch",
  "version":            "1.0.0",
  "description":        "Fetches and enriches URL content",
  "entrypoint":         "code.so",
  "hooks":              ["post_ingest"],
  "pipelines":          ["url.fetch"],
  "object_types":       ["url.enriched"],
  "requires_plugin_api": ">=1.0,<2.0",
  "permissions": {
    "network":    true,
    "filesystem": false
  }
}
```

Fields:

| Field | Required | Notes |
|---|---|---|
| `name` | yes | matches `Plugin.Name()` and config key |
| `version` | yes | semver |
| `hooks` | no | lifecycle hooks the plugin uses |
| `pipelines` | no | pipeline names the plugin registers |
| `object_types` | no | new KO types the plugin declares |
| `permissions` | yes | least-privilege; declare only what you need |
| `requires_plugin_api` | yes | semver range against `pluginapi` package version |

---

## 3. Extension points

### 3.1 Pipeline step (`pluginapi.PipelineStep`)

Transforms a `KnowledgeObject` during pipeline execution. Runs synchronously in the
worker goroutine.

```
interface PipelineStep {
    Name()     string
    Contract() StepContract   // declares Requires / Produces / Capabilities
    Run(ctx, draft *KO) (*KO, error)
}
```

`StepContract.Capabilities` values: `"llm"`, `"ocr"`, `"vision"`, `"embedding"`,
`"transcription"`. Scheduler uses these to route to capable workers.

### 3.2 Post-ingest hook (`pluginapi.PostIngestHook`)

Runs after the KO is stored. Useful for side effects: scoring, notifications,
downstream API calls. Must not assume the object is still mutable in storage —
persistence happened already.

```
interface PostIngestHook extends Plugin {
    PostIngest(ctx, obj *KO) error
}
```

### 3.3 Alias resolver (`pluginapi.AliasResolver`)

Resolves human-readable names to object IDs. Registry iterates resolvers in
registration order; first non-identity result wins.

```
interface AliasResolver extends Plugin {
    ResolveID(ctx, idOrAlias, profile string) (string, error)
}
```

Return `(idOrAlias, nil)` when the input is not an alias you own — never error on
unknown aliases.

### 3.4 Event bus subscriber (`pluginapi.Bus`)

Injected via `Deps.Bus`. Plugins publish and subscribe to CloudEvents v1.0.

```
bus.Subscribe("ranking.scored", func(ctx, e Event) error { ... })
bus.Publish(ctx, Event{Type: "url.enriched", ...})
```

Event type format: `<domain>.<verb>` (e.g., `pipeline.step_completed`).

### 3.5 Future extension points (planned, ADR-012)

- `PluginCommand` — custom CLI subcommands
- `PluginSemanticAugmentor` — propose mentions / aliases / metadata enrichments
- `PluginSearchOperator` — define custom RSQL operators
- `PluginRESTEndpoint` — custom routes under `/plugins/<name>/`

---

## 4. Implementing a plugin in Go

### 4.1 Module setup

Your plugin lives in its own Go module. Import only `pkg/pluginapi`:

```
module github.com/you/ctxt-plugin-url-fetch

go 1.22

require github.com/ideacrafterslabs/ctxt v0.x.y
```

For local development:

```
replace github.com/ideacrafterslabs/ctxt => /path/to/ctxt
```

Only import `github.com/ideacrafterslabs/ctxt/pkg/pluginapi`. Do not import
`internal/` — Go enforces this at compile time.

### 4.2 Core interface

Every plugin must implement `pluginapi.Plugin`:

```
type Plugin interface {
    Name()          string
    Version()       string
    Init(ctx, cfg map[string]interface{}, deps Deps) error
    PipelineSteps() []PipelineStep
    Close(ctx) error
}
```

`Deps` carries shared resources:

```
type Deps struct {
    Bus   Bus            // CloudEvents bus
    Store StorageDriver  // narrow storage view (aliases only today)
}
```

### 4.3 Config parsing

`Init` receives the raw config map from `plugins[n].config` in `config.yaml`.
Read defensively — keys may be missing:

```
func (p *Plugin) Init(_ context.Context, cfg map[string]interface{}, deps pluginapi.Deps) error {
    if v, ok := cfg["endpoint"].(string); ok {
        p.endpoint = v
    }
    if p.endpoint == "" {
        return fmt.Errorf("url-fetch: endpoint required")
    }
    p.bus = deps.Bus
    return nil
}
```

### 4.4 Writing a pipeline step

```
type FetchStep struct{ client *http.Client }

func (s *FetchStep) Name() string { return "url_fetch" }

func (s *FetchStep) Contract() pluginapi.StepContract {
    return pluginapi.StepContract{
        Requires:     []string{"RawContent"},           // URL in RawContent
        Produces:     []string{"TextContent", "Metadata"},
        Capabilities: []string{},                       // no special backend needed
    }
}

func (s *FetchStep) Run(ctx context.Context, draft *pluginapi.KnowledgeObject) (
    *pluginapi.KnowledgeObject, error,
) {
    // fetch URL, populate draft.TextContent, draft.Metadata
    return draft, nil
}
```

`Requires` / `Produces` are field names on `KnowledgeObject`. Scheduler uses them
for dependency ordering. Declare every field you read and every field you write.

`KnowledgeObject` writable fields available to plugins:

| Field | Type | Notes |
|---|---|---|
| `TextContent` | `string` | normalised text |
| `Metadata` | `map[string]any` | arbitrary enrichment |
| `Summaries` | `[]string` | short summaries |
| `Sections` | `[]Section` | structured content |
| `Tags` | `[]Tag` | weighted labels |
| `Mentions` | `[]uri.URI` | `@namespace.slug` references |
| `Decisions` | `[]Decision` | extracted decisions |
| `Tasks` | `[]Task` | extracted tasks |
| `Embeddings` | `[]float32` | vector embedding |
| `Plugins` | `map[string]any` | plugin-owned namespace |

Never write to `ID`, `CreatedAt`, `ContentHash`, or `Status` — those are
core-managed fields.

### 4.5 Writing a post-ingest hook

```
type URLFetchPlugin struct{ bus pluginapi.Bus }

func (p *URLFetchPlugin) PostIngest(ctx context.Context, obj *pluginapi.KnowledgeObject) error {
    if obj.Type != "url" {
        return nil
    }
    score := computeScore(obj)
    if obj.Plugins == nil {
        obj.Plugins = map[string]any{}
    }
    obj.Plugins["url-fetch"] = map[string]any{"score": score}
    if p.bus != nil {
        _ = p.bus.Publish(ctx, pluginapi.Event{
            Type:   "url.scored",
            Source: "plugin:url-fetch",
        })
    }
    return nil
}
```

`PostIngest` mutations to `obj` are **not** auto-persisted. The caller decides
whether to re-save; check the wiring in `internal/service/` for current behaviour.

### 4.6 Writing an alias resolver

See `plugins/aliasing/plugin.go` for the reference implementation. Key rule:

```
func (p *Plugin) ResolveID(ctx context.Context, idOrAlias, profile string) (string, error) {
    id, err := p.store.Resolve(ctx, idOrAlias, profile)
    if err != nil {
        return idOrAlias, nil  // pass-through; do not propagate "not found" as error
    }
    return id, nil
}
```

---

## 5. Config declaration

In `~/.config/contexthelp/config.yaml`, plugins are declared under `plugins:` as an
ordered list. The `plugin` field must match `Plugin.Name()` exactly.

```yaml
plugins:
  - plugin: url-fetch
    config:
      endpoint: https://fetch.internal/v1
      timeout_seconds: 10

  - plugin: autosuggest
    config:
      mode: generate
      max_tags: 5
      enabled: true
```

Config isolation: each plugin receives only its own `config` block. Other plugins'
config is never visible.

Environment variable substitution is supported for secret values:

```yaml
config:
  api_key: ${MY_PLUGIN_API_KEY}
```

---

## 6. Registering a plugin

Plugins are registered at server startup in `cmd/dpkms/cmd/serve.go` (or equivalent
wire-up entry point). Use the pattern established by built-in plugins:

```
pluginReg := plugin.NewRegistry()
pluginReg.Register(urlfetch.New())
pluginReg.Register(aliasing.New())

err := pluginReg.InitAll(ctx, cfg.PluginConfigs(), deps)

// Contribute pipeline steps from all plugins
pipes.RegisterExtraSteps(pluginReg.ExtraSteps())

// Wire post-ingest hooks into the job worker
worker.SetPostIngestHooks(pluginReg.PostIngestHooks())

// Wire alias resolvers into the service layer
svc.SetAliasResolvers(pluginReg.AliasResolvers())
```

Duplicate plugin names panic at `Register` — enforced by `Registry.Register`.

On shutdown:

```
defer pluginReg.CloseAll(ctx)
```

---

## 7. Capabilities and permissions

### Declared capabilities

`StepContract.Capabilities` signals subsystem requirements to the scheduler:

| Capability | Meaning |
|---|---|
| `llm` | requires LLM provider |
| `embedding` | requires embedding provider |
| `ocr` | requires OCR provider |
| `vision` | requires vision model |
| `transcription` | requires audio transcription |

Steps without capabilities run on any worker.

### Manifest permissions

Declare in `plugin.json`:

| Permission | Grants |
|---|---|
| `network: true` | outbound HTTP/TCP from the step |
| `filesystem: true` | read/write inside plugin sandbox only |

Plugins that require `network` but do not declare it will have requests blocked
by core in future enforcement layers.

### What plugins must not do

- Write to core DB schema tables directly
- Override canonical entity IDs or registry definitions
- Write to `KnowledgeObject` fields outside the list in §4.4
- Read or write another plugin's `Plugins["<other-name>"]` namespace
- Modify `obj.ID`, `obj.Status`, `obj.ContentHash`, `obj.CreatedAt`
- Register a `Name()` that collides with an existing plugin (panics)

---

## 8. Example: URL-fetch adapter plugin

Scenario: fetch a URL's HTML body, extract title and description, attach as metadata.

### Directory layout

```
ctxt-plugin-url-fetch/
  go.mod
  plugin.go       # Plugin struct + lifecycle
  step.go         # FetchStep pipeline step
  config.go       # config parsing
  plugin.json     # manifest
```

### `plugin.go` (pseudocode — adapt to exact signatures in `pkg/pluginapi`)

```
package urlfetch

import (
    "context"
    "net/http"
    "time"

    "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

type Plugin struct {
    endpoint string
    timeout  time.Duration
    client   *http.Client
}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "url-fetch" }
func (p *Plugin) Version() string { return "1.0.0" }

func (p *Plugin) Init(_ context.Context, cfg map[string]interface{}, _ pluginapi.Deps) error {
    p.endpoint, _ = cfg["endpoint"].(string)
    secs, _ := cfg["timeout_seconds"].(int)
    if secs == 0 { secs = 10 }
    p.timeout  = time.Duration(secs) * time.Second
    p.client   = &http.Client{Timeout: p.timeout}
    return nil
}

func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep {
    return []pluginapi.PipelineStep{&FetchStep{client: p.client}}
}

func (p *Plugin) Close(_ context.Context) error { return nil }
```

### `step.go` (pseudocode)

```
package urlfetch

import (
    "context"
    "net/http"

    "github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

type FetchStep struct{ client *http.Client }

func (s *FetchStep) Name() string { return "url_fetch" }

func (s *FetchStep) Contract() pluginapi.StepContract {
    return pluginapi.StepContract{
        Requires:     []string{"RawContent"},
        Produces:     []string{"TextContent", "Metadata"},
        Capabilities: []string{},
    }
}

func (s *FetchStep) Run(ctx context.Context, draft *pluginapi.KnowledgeObject) (
    *pluginapi.KnowledgeObject, error,
) {
    url := draft.RawContent
    req, _  := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    resp, err := s.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("url-fetch: %w", err)
    }
    defer resp.Body.Close()

    title, desc := extractMeta(resp.Body)  // your HTML parse logic
    draft.TextContent = title + " " + desc
    if draft.Metadata == nil {
        draft.Metadata = map[string]any{}
    }
    draft.Metadata["url_title"]       = title
    draft.Metadata["url_description"] = desc
    return draft, nil
}
```

### `plugin.json`

```
{
  "name":               "url-fetch",
  "version":            "1.0.0",
  "description":        "Fetches HTML and extracts title/description",
  "entrypoint":         "code.so",
  "hooks":              [],
  "pipelines":          [],
  "object_types":       [],
  "requires_plugin_api": ">=1.0,<2.0",
  "permissions": {
    "network":    true,
    "filesystem": false
  }
}
```

### `config.yaml` snippet

```yaml
plugins:
  - plugin: url-fetch
    config:
      timeout_seconds: 15
```

### Wire-up (in `serve.go`)

```
pluginReg.Register(urlfetch.New())
```

---

## 9. Testing your plugin

### Unit tests

Test `Init`, `Run`, and `Close` independently. Use `pluginapi.Deps{}` with nil fields
for plugins that don't need bus or store:

```
func TestFetchStep_Run(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, "<html><title>Test</title></html>")
    }))
    defer srv.Close()

    step := &FetchStep{client: srv.Client()}
    draft := &pluginapi.KnowledgeObject{RawContent: srv.URL}
    got, err := step.Run(context.Background(), draft)

    assert.NoError(t, err)
    assert.Equal(t, "Test", got.Metadata["url_title"])
}
```

### Contract assertions

Verify `Name()` is non-empty and matches your manifest and config key. Verify
`Contract().Requires` and `Contract().Produces` are non-nil slices (empty ok).

### Init edge cases

- missing required config key → `Init` returns descriptive error
- nil `deps.Bus` / nil `deps.Store` → no panic; degrade gracefully
- duplicate `Name()` registration → verify panic message contains the name

### Integration smoke test

1. Build host binary with plugin registered
2. Start server; check logs for `plugin initialised name=url-fetch version=1.0.0`
3. Enqueue a URL object: `POST /api/v1/pipelines/enqueue {"content":"https://...", "type":"url"}`
4. Wait for job completion; `GET /api/v1/objects/<id>` → `metadata.url_title` present
5. `SIGTERM` → `Close()` called; server exits 0

### Testing post-ingest hooks

Call `PostIngest` directly on a constructed `KnowledgeObject`. Verify `obj.Plugins`
contains your namespace key with expected values.

### Testing alias resolvers

Use `pluginapi.AliasStore` with an in-memory implementation. Verify `ResolveID`
returns pass-through for unknown aliases (no error).

---

## See also

- `pkg/pluginapi/pluginapi.go` — canonical type definitions (read this first)
- `internal/plugin/plugin.go` — host-side interface aliases
- `internal/plugin/registry.go` — registry lifecycle
- `plugins/aliasing/` — reference implementation (alias resolver)
- `plugins/autosuggest/` — reference implementation (pipeline step + post-ingest)
- [ADR-012](decisions/ADR-012-use-plugins-to-extend-any-layer.md) — design rationale
- [US-0042](stories/plugins/US-0042-implement-custom-enrichment-plugin.md) — enrichment
- [US-0043](stories/plugins/US-0043-implement-custom-ai-provider-plugin.md) — AI provider
- [US-0044](stories/plugins/US-0044-implement-registry-adapter-plugin.md) — registry adapter
- [US-0045](stories/plugins/US-0045-implement-custom-ranking-algorithm.md) — custom ranking
- [plugins-api.md](plugins/plugins-api.md) — broader API spec (pre-formalization)
