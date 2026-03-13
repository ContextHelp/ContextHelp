# Plugin: Auto-Suggest Tags/Mentions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Plugin that suggests tags and @mentions during and after object ingestion, with two configurable modes: "select" (present suggestions for user approval) and "generate" (auto-apply without confirmation).

**Architecture:** Implemented as a pipeline step plugin. Hooks into post-enrichment stage. In "generate" mode, directly mutates the object's tags/mentions. In "select" mode, stores suggestions in object.Metadata["plugin.autosuggest.pending"] for CLI/UI approval. Extraction-ready: all plugin code in `plugins/autosuggest/`.

**Tech Stack:** Go, existing pipeline step interface, LLM provider (via existing providers.Factory), config-driven vocabulary constraints.

---

## Plugin System Findings (read before implementing)

### How the plugin system works today (2026-03-13)

**Registration mechanism (`internal/config/config.go`):**
`PluginConfig` has three fields: `Type string`, `Plugin string`, `Config map[string]interface{}`. This is the config-file side. There is currently no runtime `Plugin` interface in the Go codebase — the design documents (`docs/plugins/plugins-api.md`, `docs/plugins/plugins.md`) describe the intended API extensively but the Go implementation does not yet have a `Plugin` interface, plugin loader, or hook registry. The first concrete plugin (this one) must **define that interface** as Task 0 (below).

**Pipeline step interface (`internal/pipeline/pipeline.go`):**
```go
type PipelineStep interface {
    Name() string
    Contract() StepContract
    Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
}
```
Steps are registered in `internal/pipeline/builtins/builtins.go` in the `stepConstructors` and `providerStepConstructors` maps. Provider-aware steps receive `*providers.Factory`. This is the exact hook point for auto-suggest.

**Event bus (`internal/events/bus.go`):**
A `LocalBus` (in-memory, async goroutine per handler) implements `Bus`:
```go
type Bus interface {
    Publish(ctx context.Context, e Event) error
    Subscribe(eventType string, handler Handler)
    Close() error
}
```
Events use CloudEvents v1.0. Currently published event types: `job.enqueued`, `job.completed`, `job.failed`, `object.updated`, `object.deleted`. `object.created` is **not yet published** (objects are created inside `WorkerPool.process` in `internal/jobs/worker.go` at line 169, without an event). The `*` wildcard subscription is supported.

**LLM provider (`providers.Factory`):**
`f.LLM()` returns a `providers.LLMProvider`. The existing `tagger` step calls `t.llm.Generate(ctx, prompt)` and receives a plain string back. That is the pattern to follow. `tagger.go` is the reference implementation.

**Plugin metadata namespace in objects:**
`storage.KnowledgeObject.Plugins map[string]any` is the sanctioned store for plugin data on objects (per `docs/plugins/plugins.md`). Use `obj.Plugins["autosuggest"]` rather than `obj.Metadata`.

**Key gap: no Plugin interface exists yet.** Task 0 defines it. The plugin registers its step by adding to the step registry (builtins map) and adding the step name to pipeline definitions — or by the service/worker accepting an injected list of extra post-enrichment steps.

**Extraction-readiness pattern:** The main `go.work` file currently only lists `.` (the root module). Adding a plugin module requires adding it to `go.work`. The plugin's own `go.mod` will declare the main module as a `require` with `replace` pointing to `../..` for local development, and the replace is removed when extracting to a standalone repo.

---

## Task List

| # | Task | File(s) | Est. |
|---|------|---------|------|
| T0 | Define Plugin interface in core | `internal/plugin/plugin.go`, `internal/plugin/registry.go` | 10 min |
| T1 | Plugin directory structure + go.mod | `plugins/autosuggest/` | 5 min |
| T2 | AutoSuggestConfig | `plugins/autosuggest/config.go` | 5 min |
| T3 | Suggestion LLM call | `plugins/autosuggest/suggest.go` | 15 min |
| T4 | Apply logic — generate mode | `plugins/autosuggest/apply.go` | 10 min |
| T5 | Pending storage — select mode | `plugins/autosuggest/apply.go` | 10 min |
| T6 | AutoSuggestStep (pipeline step) | `plugins/autosuggest/step.go` | 10 min |
| T7 | plugin.go — Plugin struct, registration helper | `plugins/autosuggest/plugin.go` | 10 min |
| T8 | Wire plugin into service startup | `cmd/ctxt/main.go` or server init | 10 min |
| T9 | REST handlers for approve/reject | `internal/server/http/handlers_suggestions.go` | 15 min |
| T10 | CLI approve/reject commands | `plugins/autosuggest/cli.go` | 15 min |
| T11 | Integration test | `plugins/autosuggest/plugin_test.go` | 15 min |
| T12 | Extraction-readiness verification | `plugins/autosuggest/go.mod`, `go.work` | 5 min |

---

## Task 0: Define Plugin Interface in Core

**Why:** The config struct `PluginConfig` exists but no Go `Plugin` interface does. This is a prerequisite shared by all three plugins. Do this once; the other two plugins reuse it.

**Files:**
- create: `internal/plugin/plugin.go`
- create: `internal/plugin/registry.go`

**Step 0.1 — write plugin.go**

Create `internal/plugin/plugin.go`:

```go
package plugin

import (
    "context"

    "github.com/ideacrafterslabs/ctxt/internal/events"
    "github.com/ideacrafterslabs/ctxt/internal/pipeline"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Plugin is the interface every plugin must satisfy.
type Plugin interface {
    // Name returns the unique plugin identifier (matches config key).
    Name() string
    // Version returns the plugin's semver string.
    Version() string
    // Init is called once at startup with the plugin's config block and shared deps.
    Init(ctx context.Context, cfg map[string]interface{}, deps Deps) error
    // PipelineSteps returns zero or more pipeline steps to register.
    PipelineSteps() []pipeline.PipelineStep
    // Close is called on graceful shutdown.
    Close(ctx context.Context) error
}

// PostIngestHook is implemented by plugins that want to run after an object is created.
type PostIngestHook interface {
    Plugin
    PostIngest(ctx context.Context, obj *storage.KnowledgeObject) error
}

// Deps carries shared dependencies injected at plugin init.
type Deps struct {
    Bus   events.Bus
    Store storage.StorageDriver
}
```

**Step 0.2 — write registry.go**

Create `internal/plugin/registry.go`:

```go
package plugin

import (
    "context"
    "fmt"
    "log"

    "github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

// Registry holds registered plugins and coordinates their lifecycle.
type Registry struct {
    plugins []Plugin
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds a plugin. Panics on duplicate name.
func (r *Registry) Register(p Plugin) {
    for _, existing := range r.plugins {
        if existing.Name() == p.Name() {
            panic(fmt.Sprintf("plugin: duplicate name %q", p.Name()))
        }
    }
    r.plugins = append(r.plugins, p)
}

// InitAll initialises all registered plugins.
func (r *Registry) InitAll(ctx context.Context, cfgs map[string]map[string]interface{}, deps Deps) error {
    for _, p := range r.plugins {
        cfg := cfgs[p.Name()]
        if cfg == nil {
            cfg = map[string]interface{}{}
        }
        if err := p.Init(ctx, cfg, deps); err != nil {
            return fmt.Errorf("plugin %q init: %w", p.Name(), err)
        }
        log.Printf("plugin: %s %s initialised", p.Name(), p.Version())
    }
    return nil
}

// ExtraSteps collects all pipeline.PipelineStep contributions from every plugin.
func (r *Registry) ExtraSteps() []pipeline.PipelineStep {
    var out []pipeline.PipelineStep
    for _, p := range r.plugins {
        out = append(out, p.PipelineSteps()...)
    }
    return out
}

// PostIngestHooks returns all plugins that implement PostIngestHook.
func (r *Registry) PostIngestHooks() []PostIngestHook {
    var out []PostIngestHook
    for _, p := range r.plugins {
        if h, ok := p.(PostIngestHook); ok {
            out = append(out, h)
        }
    }
    return out
}

// CloseAll shuts down all plugins gracefully.
func (r *Registry) CloseAll(ctx context.Context) {
    for _, p := range r.plugins {
        if err := p.Close(ctx); err != nil {
            log.Printf("plugin: %s close error: %v", p.Name(), err)
        }
    }
}
```

**Step 0.3 — write test**

Create `internal/plugin/registry_test.go`:

```go
package plugin_test

import (
    "context"
    "testing"

    "github.com/ideacrafterslabs/ctxt/internal/pipeline"
    "github.com/ideacrafterslabs/ctxt/internal/plugin"
    "github.com/stretchr/testify/require"
)

type stubPlugin struct{ name string }

func (s *stubPlugin) Name() string    { return s.name }
func (s *stubPlugin) Version() string { return "0.0.1" }
func (s *stubPlugin) Init(_ context.Context, _ map[string]interface{}, _ plugin.Deps) error {
    return nil
}
func (s *stubPlugin) PipelineSteps() []pipeline.PipelineStep { return nil }
func (s *stubPlugin) Close(_ context.Context) error          { return nil }

func TestRegistry_Register(t *testing.T) {
    r := plugin.NewRegistry()
    r.Register(&stubPlugin{name: "test"})
    require.Panics(t, func() { r.Register(&stubPlugin{name: "test"}) })
}

func TestRegistry_InitAll(t *testing.T) {
    r := plugin.NewRegistry()
    r.Register(&stubPlugin{name: "a"})
    r.Register(&stubPlugin{name: "b"})
    err := r.InitAll(context.Background(), nil, plugin.Deps{})
    require.NoError(t, err)
}
```

**Step 0.4 — commit**

```
git add internal/plugin/
git commit -m "feat(plugin): add Plugin interface and Registry"
```

Expected: `go test ./internal/plugin/...` passes.

---

## Task 1: Plugin Directory Structure + go.mod

**Files:**
- create: `plugins/autosuggest/` (directory)
- create: `plugins/autosuggest/go.mod`

**Step 1.1 — create directory**

```bash
mkdir -p plugins/autosuggest
```

**Step 1.2 — write go.mod**

Create `plugins/autosuggest/go.mod`:

```go
module github.com/ideacrafterslabs/ctxt-plugin-autosuggest

go 1.25.6

require (
    github.com/google/uuid v1.6.0
    github.com/ideacrafterslabs/ctxt v0.0.0
    github.com/stretchr/testify v1.11.1
)

replace github.com/ideacrafterslabs/ctxt => ../..
```

**Step 1.3 — add to go.work**

Edit `/Users/jadb/.w/ideacrafterslabs/ctxt/go.work`:

```
go 1.26.1

use .
use ./plugins/autosuggest
```

**Step 1.4 — commit**

```
git add plugins/autosuggest/go.mod go.work
git commit -m "feat(plugin/autosuggest): scaffold module"
```

---

## Task 2: AutoSuggestConfig

**Files:**
- create: `plugins/autosuggest/config.go`
- create: `plugins/autosuggest/config_test.go`

**Step 2.1 — write test first**

Create `plugins/autosuggest/config_test.go`:

```go
package autosuggest_test

import (
    "testing"

    autosuggest "github.com/ideacrafterslabs/ctxt-plugin-autosuggest"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "gopkg.in/yaml.v3"
)

func TestConfigDefaults(t *testing.T) {
    cfg := autosuggest.DefaultConfig()
    assert.Equal(t, "select", cfg.Mode)
    assert.Equal(t, 5, cfg.MaxTags)
    assert.Equal(t, 3, cfg.MaxMentions)
    assert.True(t, cfg.Enabled)
}

func TestConfigFromMap(t *testing.T) {
    raw := map[string]interface{}{
        "mode":         "generate",
        "max_tags":     10,
        "max_mentions": 5,
        "enabled":      true,
        "vocabulary_hint": []interface{}{"golang", "architecture", "decision"},
    }
    cfg, err := autosuggest.ConfigFromMap(raw)
    require.NoError(t, err)
    assert.Equal(t, "generate", cfg.Mode)
    assert.Equal(t, 10, cfg.MaxTags)
    assert.Equal(t, []string{"golang", "architecture", "decision"}, cfg.VocabularyHint)
}

func TestConfigFromYAML(t *testing.T) {
    src := `
mode: select
max_tags: 3
max_mentions: 2
vocabulary_hint:
  - go
  - plugin
`
    var raw map[string]interface{}
    require.NoError(t, yaml.Unmarshal([]byte(src), &raw))
    cfg, err := autosuggest.ConfigFromMap(raw)
    require.NoError(t, err)
    assert.Equal(t, "select", cfg.Mode)
    assert.Equal(t, 3, cfg.MaxTags)
}
```

**Step 2.2 — write config.go**

Create `plugins/autosuggest/config.go`:

```go
package autosuggest

import "fmt"

// AutoSuggestConfig controls plugin behaviour.
type AutoSuggestConfig struct {
    // Mode is "select" (default, stores pending for human approval) or "generate" (auto-applies).
    Mode string
    // MaxTags is the maximum number of tag suggestions to produce (default 5).
    MaxTags int
    // MaxMentions is the maximum number of @mention suggestions to produce (default 3).
    MaxMentions int
    // VocabularyHint is a list of preferred tags to bias the LLM toward.
    VocabularyHint []string
    // Enabled controls whether the step runs at all (default true).
    Enabled bool
}

// DefaultConfig returns a config with safe defaults.
func DefaultConfig() AutoSuggestConfig {
    return AutoSuggestConfig{
        Mode:        "select",
        MaxTags:     5,
        MaxMentions: 3,
        Enabled:     true,
    }
}

// ConfigFromMap decodes a raw map (from PluginConfig.Config) into AutoSuggestConfig.
func ConfigFromMap(m map[string]interface{}) (AutoSuggestConfig, error) {
    cfg := DefaultConfig()
    if m == nil {
        return cfg, nil
    }
    if v, ok := m["mode"].(string); ok {
        if v != "select" && v != "generate" {
            return cfg, fmt.Errorf("autosuggest: mode must be 'select' or 'generate', got %q", v)
        }
        cfg.Mode = v
    }
    if v, ok := m["max_tags"].(int); ok {
        cfg.MaxTags = v
    }
    if v, ok := m["max_mentions"].(int); ok {
        cfg.MaxMentions = v
    }
    if v, ok := m["enabled"].(bool); ok {
        cfg.Enabled = v
    }
    if raw, ok := m["vocabulary_hint"].([]interface{}); ok {
        for _, item := range raw {
            if s, ok := item.(string); ok {
                cfg.VocabularyHint = append(cfg.VocabularyHint, s)
            }
        }
    }
    return cfg, nil
}
```

**Step 2.3 — run test, commit**

```bash
cd plugins/autosuggest && go test ./... -run TestConfig
```

Expected: `PASS`.

```
git add plugins/autosuggest/config.go plugins/autosuggest/config_test.go
git commit -m "feat(plugin/autosuggest): add AutoSuggestConfig"
```

---

## Task 3: Suggestion LLM Call

**Files:**
- create: `plugins/autosuggest/suggest.go`
- create: `plugins/autosuggest/suggest_test.go`

**Step 3.1 — write test first**

Create `plugins/autosuggest/suggest_test.go`:

```go
package autosuggest_test

import (
    "context"
    "testing"

    autosuggest "github.com/ideacrafterslabs/ctxt-plugin-autosuggest"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// mockLLM is a test double for providers.LLMProvider.
type mockLLM struct{ response string }

func (m *mockLLM) Generate(_ context.Context, _ string) (string, error) {
    return m.response, nil
}

func TestSuggestTagsAndMentions_ParsesJSON(t *testing.T) {
    obj := &storage.KnowledgeObject{
        ID:         "obj_001",
        RawContent: "We decided to adopt Go as the primary language for the project.",
        Summaries:  []string{"Architectural decision about language choice"},
    }
    cfg := autosuggest.DefaultConfig()
    cfg.MaxTags = 3
    cfg.MaxMentions = 2
    llm := &mockLLM{response: `{"tags":["go","architecture","decision"],"mentions":["eng.backend"]}`}

    tags, mentions, err := autosuggest.SuggestTagsAndMentions(context.Background(), obj, cfg, llm)
    require.NoError(t, err)
    assert.Equal(t, []string{"go", "architecture", "decision"}, tags)
    assert.Equal(t, []string{"eng.backend"}, mentions)
}

func TestSuggestTagsAndMentions_MalformedJSON_ReturnsEmpty(t *testing.T) {
    obj := &storage.KnowledgeObject{ID: "obj_002", RawContent: "short"}
    cfg := autosuggest.DefaultConfig()
    llm := &mockLLM{response: "not json at all"}

    tags, mentions, err := autosuggest.SuggestTagsAndMentions(context.Background(), obj, cfg, llm)
    require.NoError(t, err) // soft failure: returns empty, not an error
    assert.Empty(t, tags)
    assert.Empty(t, mentions)
}

func TestSuggestTagsAndMentions_NilLLM_ReturnsEmpty(t *testing.T) {
    obj := &storage.KnowledgeObject{ID: "obj_003", RawContent: "some content"}
    cfg := autosuggest.DefaultConfig()

    tags, mentions, err := autosuggest.SuggestTagsAndMentions(context.Background(), obj, cfg, nil)
    require.NoError(t, err)
    assert.Empty(t, tags)
    assert.Empty(t, mentions)
}
```

**Step 3.2 — write suggest.go**

Create `plugins/autosuggest/suggest.go`:

```go
package autosuggest

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"

    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// LLMProvider is the minimal interface needed from providers.LLMProvider.
// Defined locally so the plugin module does not need to import the full providers package.
type LLMProvider interface {
    Generate(ctx context.Context, prompt string) (string, error)
}

type suggestResponse struct {
    Tags     []string `json:"tags"`
    Mentions []string `json:"mentions"`
}

// SuggestTagsAndMentions calls the LLM to produce tag and @mention suggestions.
// Returns empty slices (not an error) if the LLM is nil or returns unparseable output.
func SuggestTagsAndMentions(ctx context.Context, obj *storage.KnowledgeObject, cfg AutoSuggestConfig, llm LLMProvider) (tags []string, mentions []string, err error) {
    if llm == nil {
        return nil, nil, nil
    }

    summary := buildSummary(obj)
    vocab := ""
    if len(cfg.VocabularyHint) > 0 {
        vocab = fmt.Sprintf(" Prefer tags from this vocabulary where relevant: %s.", strings.Join(cfg.VocabularyHint, ", "))
    }

    prompt := fmt.Sprintf(
        "Analyse this knowledge object and suggest tags and entity @mentions.\n"+
            "Return a JSON object with exactly two fields: \"tags\" (array of strings, max %d) and \"mentions\" (array of strings in @namespace.slug format, max %d).%s\n"+
            "Do not include any explanation or markdown. Output raw JSON only.\n\n"+
            "Content summary:\n%s",
        cfg.MaxTags, cfg.MaxMentions, vocab, summary,
    )

    raw, err := llm.Generate(ctx, prompt)
    if err != nil {
        // LLM errors are soft: log-worthy but not pipeline-fatal.
        return nil, nil, nil
    }

    // Strip optional markdown fences.
    raw = strings.TrimSpace(raw)
    raw = strings.TrimPrefix(raw, "```json")
    raw = strings.TrimPrefix(raw, "```")
    raw = strings.TrimSuffix(raw, "```")
    raw = strings.TrimSpace(raw)

    var resp suggestResponse
    if err := json.Unmarshal([]byte(raw), &resp); err != nil {
        // Unparseable response: soft failure.
        return nil, nil, nil
    }

    // Enforce caps.
    if len(resp.Tags) > cfg.MaxTags {
        resp.Tags = resp.Tags[:cfg.MaxTags]
    }
    if len(resp.Mentions) > cfg.MaxMentions {
        resp.Mentions = resp.Mentions[:cfg.MaxMentions]
    }

    return resp.Tags, resp.Mentions, nil
}

// buildSummary constructs a compact representation of the object for the LLM prompt.
func buildSummary(obj *storage.KnowledgeObject) string {
    var parts []string
    if len(obj.Summaries) > 0 {
        parts = append(parts, obj.Summaries[0])
    }
    content := obj.RawContent
    if len(content) > 800 {
        content = content[:800] + "…"
    }
    if content != "" {
        parts = append(parts, content)
    }
    return strings.Join(parts, "\n\n")
}
```

**Step 3.3 — run test, commit**

```bash
cd plugins/autosuggest && go test ./... -run TestSuggest
```

Expected: `PASS`.

```
git add plugins/autosuggest/suggest.go plugins/autosuggest/suggest_test.go
git commit -m "feat(plugin/autosuggest): add LLM suggestion logic"
```

---

## Task 4: Apply Logic — Generate Mode

**Files:**
- create: `plugins/autosuggest/apply.go`
- create: `plugins/autosuggest/apply_test.go` (partial — extend in Task 5)

**Step 4.1 — write test**

Create `plugins/autosuggest/apply_test.go`:

```go
package autosuggest_test

import (
    "testing"

    autosuggest "github.com/ideacrafterslabs/ctxt-plugin-autosuggest"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestApplyGenerate_AddsTags(t *testing.T) {
    obj := &storage.KnowledgeObject{
        ID:   "obj_gen_001",
        Tags: []storage.Tag{{Label: "existing", Weight: 1.0}},
    }
    tags := []string{"go", "existing", "architecture"} // "existing" is a dupe
    mentions := []string{"eng.backend"}

    require.NoError(t, autosuggest.ApplyGenerate(obj, tags, mentions))

    labels := make([]string, len(obj.Tags))
    for i, t := range obj.Tags {
        labels[i] = t.Label
    }
    assert.Contains(t, labels, "go")
    assert.Contains(t, labels, "architecture")
    assert.Contains(t, labels, "existing")
    assert.Equal(t, 3, len(obj.Tags), "no duplicate tags")
    assert.Contains(t, obj.Mentions, "eng.backend")
}

func TestApplyGenerate_DeduplicatesMentions(t *testing.T) {
    obj := &storage.KnowledgeObject{
        ID:       "obj_gen_002",
        Mentions: []string{"eng.frontend"},
    }
    require.NoError(t, autosuggest.ApplyGenerate(obj, nil, []string{"eng.frontend", "eng.backend"}))
    assert.Equal(t, 2, len(obj.Mentions))
}
```

**Step 4.2 — write apply.go (partial)**

Create `plugins/autosuggest/apply.go`:

```go
package autosuggest

import (
    "encoding/json"

    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

const pendingMetaKey = "plugin.autosuggest.pending"

// pendingPayload is stored in obj.Plugins["autosuggest"] in select mode.
type pendingPayload struct {
    Tags     []string `json:"tags"`
    Mentions []string `json:"mentions"`
}

// ApplyGenerate directly appends suggested tags and mentions to obj, deduplicating.
// The caller is responsible for persisting the updated object.
func ApplyGenerate(obj *storage.KnowledgeObject, tags []string, mentions []string) error {
    // Merge tags.
    existing := make(map[string]bool, len(obj.Tags))
    for _, t := range obj.Tags {
        existing[t.Label] = true
    }
    for _, label := range tags {
        if label == "" || existing[label] {
            continue
        }
        obj.Tags = append(obj.Tags, storage.Tag{
            Label:  label,
            Weight: 0.5,
            Source: "plugin:autosuggest",
        })
        existing[label] = true
    }

    // Merge mentions.
    existingM := make(map[string]bool, len(obj.Mentions))
    for _, m := range obj.Mentions {
        existingM[m] = true
    }
    for _, mention := range mentions {
        if mention == "" || existingM[mention] {
            continue
        }
        obj.Mentions = append(obj.Mentions, mention)
        existingM[mention] = true
    }

    return nil
}

// ApplySelect stores suggestions as pending in obj.Plugins["autosuggest"] for later approval.
// The caller is responsible for persisting the updated object.
func ApplySelect(obj *storage.KnowledgeObject, tags []string, mentions []string) error {
    if obj.Plugins == nil {
        obj.Plugins = make(map[string]any)
    }
    payload := pendingPayload{Tags: tags, Mentions: mentions}
    b, err := json.Marshal(payload)
    if err != nil {
        return err
    }
    obj.Plugins["autosuggest"] = map[string]any{
        "pending": json.RawMessage(b),
    }
    return nil
}

// GetPending reads any pending suggestions stored by ApplySelect.
// Returns empty slices if none are found.
func GetPending(obj *storage.KnowledgeObject) (tags []string, mentions []string) {
    if obj.Plugins == nil {
        return nil, nil
    }
    pluginData, ok := obj.Plugins["autosuggest"].(map[string]any)
    if !ok {
        return nil, nil
    }
    raw, ok := pluginData["pending"]
    if !ok {
        return nil, nil
    }

    var b []byte
    switch v := raw.(type) {
    case json.RawMessage:
        b = v
    case []byte:
        b = v
    case string:
        b = []byte(v)
    default:
        return nil, nil
    }

    var payload pendingPayload
    if err := json.Unmarshal(b, &payload); err != nil {
        return nil, nil
    }
    return payload.Tags, payload.Mentions
}

// ClearPending removes pending suggestions from the object.
func ClearPending(obj *storage.KnowledgeObject) {
    if obj.Plugins == nil {
        return
    }
    if pluginData, ok := obj.Plugins["autosuggest"].(map[string]any); ok {
        delete(pluginData, "pending")
    }
}
```

**Step 4.3 — run test, commit**

```bash
cd plugins/autosuggest && go test ./... -run TestApplyGenerate
```

Expected: `PASS`.

```
git add plugins/autosuggest/apply.go plugins/autosuggest/apply_test.go
git commit -m "feat(plugin/autosuggest): add generate-mode apply logic"
```

---

## Task 5: Pending Storage — Select Mode

**Step 5.1 — extend apply_test.go**

Append to `plugins/autosuggest/apply_test.go`:

```go
func TestApplySelect_StoresPending(t *testing.T) {
    obj := &storage.KnowledgeObject{ID: "obj_sel_001"}
    require.NoError(t, autosuggest.ApplySelect(obj, []string{"go", "plugin"}, []string{"eng.backend"}))

    tags, mentions := autosuggest.GetPending(obj)
    assert.Equal(t, []string{"go", "plugin"}, tags)
    assert.Equal(t, []string{"eng.backend"}, mentions)
}

func TestApplySelect_ClearPending(t *testing.T) {
    obj := &storage.KnowledgeObject{ID: "obj_sel_002"}
    require.NoError(t, autosuggest.ApplySelect(obj, []string{"go"}, nil))
    autosuggest.ClearPending(obj)
    tags, mentions := autosuggest.GetPending(obj)
    assert.Empty(t, tags)
    assert.Empty(t, mentions)
}
```

**Step 5.2 — run test, commit**

```bash
cd plugins/autosuggest && go test ./... -run TestApplySelect
```

Expected: `PASS`.

```
git add plugins/autosuggest/apply_test.go
git commit -m "test(plugin/autosuggest): add select-mode pending storage tests"
```

---

## Task 6: AutoSuggestStep (Pipeline Step)

**Files:**
- create: `plugins/autosuggest/step.go`
- create: `plugins/autosuggest/step_test.go`

**Step 6.1 — write test**

Create `plugins/autosuggest/step_test.go`:

```go
package autosuggest_test

import (
    "context"
    "testing"

    autosuggest "github.com/ideacrafterslabs/ctxt-plugin-autosuggest"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAutoSuggestStep_GenerateMode_AppliesTags(t *testing.T) {
    cfg := autosuggest.AutoSuggestConfig{
        Mode:        "generate",
        MaxTags:     3,
        MaxMentions: 2,
        Enabled:     true,
    }
    llm := &mockLLM{response: `{"tags":["go","plugin"],"mentions":[]}`}
    step := autosuggest.NewAutoSuggestStep(cfg, llm)

    obj := &storage.KnowledgeObject{ID: "obj_step_001", RawContent: "Using Go for plugins."}
    out, err := step.Run(context.Background(), obj)
    require.NoError(t, err)
    labels := make([]string, len(out.Tags))
    for i, t := range out.Tags {
        labels[i] = t.Label
    }
    assert.Contains(t, labels, "go")
}

func TestAutoSuggestStep_SelectMode_StoresPending(t *testing.T) {
    cfg := autosuggest.AutoSuggestConfig{
        Mode:        "select",
        MaxTags:     3,
        MaxMentions: 2,
        Enabled:     true,
    }
    llm := &mockLLM{response: `{"tags":["architecture"],"mentions":["eng.platform"]}`}
    step := autosuggest.NewAutoSuggestStep(cfg, llm)

    obj := &storage.KnowledgeObject{ID: "obj_step_002", RawContent: "Platform architecture decisions."}
    out, err := step.Run(context.Background(), obj)
    require.NoError(t, err)
    assert.Empty(t, out.Tags, "select mode: tags NOT applied directly")
    tags, _ := autosuggest.GetPending(out)
    assert.Equal(t, []string{"architecture"}, tags)
}

func TestAutoSuggestStep_Disabled_IsNoop(t *testing.T) {
    cfg := autosuggest.AutoSuggestConfig{Enabled: false}
    llm := &mockLLM{response: `{"tags":["go"],"mentions":[]}`}
    step := autosuggest.NewAutoSuggestStep(cfg, llm)

    obj := &storage.KnowledgeObject{ID: "obj_step_003", RawContent: "content"}
    out, err := step.Run(context.Background(), obj)
    require.NoError(t, err)
    assert.Empty(t, out.Tags)
}
```

**Step 6.2 — write step.go**

Create `plugins/autosuggest/step.go`:

```go
package autosuggest

import (
    "context"

    "github.com/ideacrafterslabs/ctxt/internal/pipeline"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// AutoSuggestStep is a pipeline.PipelineStep that suggests tags and mentions.
type AutoSuggestStep struct {
    pipeline.BaseContract
    cfg AutoSuggestConfig
    llm LLMProvider
}

// NewAutoSuggestStep creates an AutoSuggestStep with the given config and LLM provider.
func NewAutoSuggestStep(cfg AutoSuggestConfig, llm LLMProvider) *AutoSuggestStep {
    return &AutoSuggestStep{
        BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
            Requires:     []string{"RawContent"},
            Produces:     []string{"Tags", "Mentions", "Metadata"},
            Capabilities: []string{"llm"},
        }),
        cfg: cfg,
        llm: llm,
    }
}

func (s *AutoSuggestStep) Name() string { return "autosuggest" }

func (s *AutoSuggestStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
    if !s.cfg.Enabled {
        return draft, nil
    }

    tags, mentions, err := SuggestTagsAndMentions(ctx, draft, s.cfg, s.llm)
    if err != nil {
        // Soft failure: return unchanged draft.
        return draft, nil
    }
    if len(tags) == 0 && len(mentions) == 0 {
        return draft, nil
    }

    switch s.cfg.Mode {
    case "generate":
        _ = ApplyGenerate(draft, tags, mentions)
    default: // "select"
        _ = ApplySelect(draft, tags, mentions)
    }

    return draft, nil
}
```

**Step 6.3 — run test, commit**

```bash
cd plugins/autosuggest && go test ./... -run TestAutoSuggestStep
```

Expected: `PASS`.

```
git add plugins/autosuggest/step.go plugins/autosuggest/step_test.go
git commit -m "feat(plugin/autosuggest): add AutoSuggestStep pipeline step"
```

---

## Task 7: Plugin Struct and Registration Helper

**Files:**
- create: `plugins/autosuggest/plugin.go`

**Step 7.1 — write plugin.go**

Create `plugins/autosuggest/plugin.go`:

```go
// Package autosuggest provides a pipeline plugin that uses an LLM to suggest
// tags and @mentions for knowledge objects post-enrichment.
package autosuggest

import (
    "context"
    "fmt"

    "github.com/ideacrafterslabs/ctxt/internal/pipeline"
    "github.com/ideacrafterslabs/ctxt/internal/plugin"
    "github.com/ideacrafterslabs/ctxt/internal/providers"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Plugin is the top-level plugin.Plugin implementation for auto-suggest.
type Plugin struct {
    cfg  AutoSuggestConfig
    step *AutoSuggestStep
}

// New creates an uninitialized Plugin. Call via plugin.Registry.Register.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "autosuggest" }
func (p *Plugin) Version() string { return "1.0.0" }

func (p *Plugin) Init(ctx context.Context, raw map[string]interface{}, deps plugin.Deps) error {
    cfg, err := ConfigFromMap(raw)
    if err != nil {
        return fmt.Errorf("autosuggest: config: %w", err)
    }
    p.cfg = cfg

    // deps.Store exposes the StorageDriver; deps.Bus is the event bus.
    // The LLM provider is not injected via Deps today — it requires *providers.Factory.
    // If Factory is not available here, the step is created without an LLM and falls back
    // to a noop (suggestions skipped). Callers should use NewWithFactory when possible.
    p.step = NewAutoSuggestStep(cfg, nil)
    return nil
}

// InitWithFactory creates the step with a real LLM from the factory.
func (p *Plugin) InitWithFactory(ctx context.Context, raw map[string]interface{}, deps plugin.Deps, factory *providers.Factory) error {
    if err := p.Init(ctx, raw, deps); err != nil {
        return err
    }
    if factory != nil {
        p.step = NewAutoSuggestStep(p.cfg, factory.LLM())
    }
    return nil
}

func (p *Plugin) PipelineSteps() []pipeline.PipelineStep {
    if p.step == nil {
        return nil
    }
    return []pipeline.PipelineStep{p.step}
}

// PostIngest implements plugin.PostIngestHook.
// After the object is stored, the step is re-run if mode == "generate" and no tags were set.
// This is the event-driven path; the step also runs inline in the pipeline.
func (p *Plugin) PostIngest(ctx context.Context, obj *storage.KnowledgeObject) error {
    if p.step == nil || !p.cfg.Enabled {
        return nil
    }
    // Only run if we did not already enrich (e.g., autosuggest step was not in the pipeline).
    if p.cfg.Mode == "generate" && len(obj.Tags) == 0 {
        _, err := p.step.Run(ctx, obj)
        return err
    }
    return nil
}

func (p *Plugin) Close(_ context.Context) error { return nil }
```

**Step 7.2 — commit**

```
git add plugins/autosuggest/plugin.go
git commit -m "feat(plugin/autosuggest): add Plugin struct implementing plugin.Plugin"
```

---

## Task 8: Wire Plugin into Service Startup

**Files:**
- edit: `cmd/ctxt/main.go` (or wherever the service is wired — check `cmd/` for the server entrypoint)

**Step 8.1 — locate wiring point**

```bash
ls /Users/jadb/.w/ideacrafterslabs/ctxt/cmd/
```

Find the file that creates `jobs.NewWorkerPool` and the HTTP server. Add plugin registry init there.

**Step 8.2 — add plugin registry**

In the server startup (pseudo-code, adapt to actual file structure):

```go
import (
    autosuggest "github.com/ideacrafterslabs/ctxt-plugin-autosuggest"
    "github.com/ideacrafterslabs/ctxt/internal/plugin"
)

// After factory and store are available:
pluginRegistry := plugin.NewRegistry()
asp := autosuggest.New()
pluginRegistry.Register(asp)

// Build plugin config map from cfg.Plugins slice.
pluginCfgMap := make(map[string]map[string]interface{})
for _, pc := range cfg.Plugins {
    pluginCfgMap[pc.Plugin] = pc.Config
}

deps := plugin.Deps{Bus: bus, Store: store}
if err := pluginRegistry.InitAll(ctx, pluginCfgMap, deps); err != nil {
    log.Fatalf("plugin init: %v", err)
}

// Register extra pipeline steps from plugins.
// For each pipeline in builtins, append autosuggest step at end.
// (Simpler: inject into the "text.long", "text.short" pipelines after tagger.)
for _, stepImpl := range pluginRegistry.ExtraSteps() {
    builtins.RegisterExtraStep(stepImpl.Name(), stepImpl)
}
```

Note: `builtins.RegisterExtraStep` does not exist yet. Add it to `builtins.go`:

```go
// RegisterExtraStep adds a pre-built step (e.g. from a plugin) to the step registry.
// It can be referenced by name in pipeline definitions.
func RegisterExtraStep(name string, step pipeline.PipelineStep) {
    stepConstructors[name] = func() pipeline.PipelineStep { return step }
}
```

**Step 8.3 — commit**

```
git add internal/pipeline/builtins/builtins.go cmd/
git commit -m "feat(plugin): wire plugin registry into server startup"
```

---

## Task 9: REST Handlers for Approve/Reject

**Files:**
- create: `internal/server/http/handlers_suggestions.go`
- create: `internal/server/http/handlers_suggestions_test.go`

**Step 9.1 — write handlers_suggestions.go**

Create `internal/server/http/handlers_suggestions.go`:

```go
package http

import (
    "encoding/json"
    "net/http"

    autosuggest "github.com/ideacrafterslabs/ctxt-plugin-autosuggest"
    "github.com/go-chi/chi/v5"
)

// GET /api/v1/suggestions — list objects with pending autosuggest suggestions.
func (s *Server) handleListSuggestions(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    // List all objects, filter those with non-empty pending suggestions.
    objs, _, err := s.svc.ListObjects(ctx, filterAll())
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    type item struct {
        ObjectID string   `json:"object_id"`
        Tags     []string `json:"tags"`
        Mentions []string `json:"mentions"`
    }
    var results []item
    for _, obj := range objs {
        tags, mentions := autosuggest.GetPending(obj)
        if len(tags) > 0 || len(mentions) > 0 {
            results = append(results, item{ObjectID: obj.ID, Tags: tags, Mentions: mentions})
        }
    }
    writeJSON(w, http.StatusOK, results)
}

// POST /api/v1/suggestions/{id}/approve
func (s *Server) handleApproveSuggestion(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    id := chi.URLParam(r, "id")

    obj, err := s.svc.GetObject(ctx, id)
    if err != nil {
        writeError(w, http.StatusNotFound, "object not found")
        return
    }

    tags, mentions := autosuggest.GetPending(obj)
    if err := autosuggest.ApplyGenerate(obj, tags, mentions); err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    autosuggest.ClearPending(obj)

    if err := s.svc.UpdateObject(ctx, obj); err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "approved", "id": id})
}

// POST /api/v1/suggestions/{id}/reject
func (s *Server) handleRejectSuggestion(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    id := chi.URLParam(r, "id")

    obj, err := s.svc.GetObject(ctx, id)
    if err != nil {
        writeError(w, http.StatusNotFound, "object not found")
        return
    }
    autosuggest.ClearPending(obj)
    if err := s.svc.UpdateObject(ctx, obj); err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "rejected", "id": id})
}
```

Register routes in `server.go`:

```go
r.Get("/api/v1/suggestions", s.handleListSuggestions)
r.Post("/api/v1/suggestions/{id}/approve", s.handleApproveSuggestion)
r.Post("/api/v1/suggestions/{id}/reject", s.handleRejectSuggestion)
```

**Step 9.2 — write handler tests** (follow pattern of `handlers_objects_test.go`)

**Step 9.3 — commit**

```
git add internal/server/http/handlers_suggestions.go internal/server/http/server.go
git commit -m "feat(api): add suggestions approve/reject handlers"
```

---

## Task 10: CLI Approve/Reject Commands

**Files:**
- create: `plugins/autosuggest/cli.go`

**Step 10.1 — write cli.go**

Create `plugins/autosuggest/cli.go`:

```go
package autosuggest

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"

    "github.com/spf13/cobra"
)

// NewCLICommands returns a cobra.Command subtree for the autosuggest plugin.
// Mount it as: rootCmd.AddCommand(autosuggest.NewCLICommands(baseURL))
func NewCLICommands(baseURL string) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "suggest",
        Short: "Manage auto-suggest tag/mention proposals",
    }

    listCmd := &cobra.Command{
        Use:   "list",
        Short: "List objects with pending tag/mention suggestions",
        RunE: func(cmd *cobra.Command, args []string) error {
            resp, err := http.Get(baseURL + "/api/v1/suggestions")
            if err != nil {
                return err
            }
            defer resp.Body.Close()
            var items []map[string]interface{}
            if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
                return err
            }
            for _, item := range items {
                fmt.Fprintf(os.Stdout, "%s: tags=%v mentions=%v\n",
                    item["object_id"], item["tags"], item["mentions"])
            }
            return nil
        },
    }

    approveCmd := &cobra.Command{
        Use:   "approve <object-id>",
        Short: "Apply pending tag/mention suggestions to an object",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            resp, err := http.Post(baseURL+"/api/v1/suggestions/"+args[0]+"/approve",
                "application/json", nil)
            if err != nil {
                return err
            }
            defer resp.Body.Close()
            fmt.Fprintf(os.Stdout, "approved: %s\n", args[0])
            return nil
        },
    }

    rejectCmd := &cobra.Command{
        Use:   "reject <object-id>",
        Short: "Discard pending tag/mention suggestions for an object",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            resp, err := http.Post(baseURL+"/api/v1/suggestions/"+args[0]+"/reject",
                "application/json", nil)
            if err != nil {
                return err
            }
            defer resp.Body.Close()
            fmt.Fprintf(os.Stdout, "rejected: %s\n", args[0])
            return nil
        },
    }

    cmd.AddCommand(listCmd, approveCmd, rejectCmd)
    return cmd
}
```

In `cmd/ctxt/cmd/root.go` (or equivalent):

```go
rootCmd.AddCommand(autosuggest.NewCLICommands(serverBaseURL))
```

**Step 10.2 — commit**

```
git add plugins/autosuggest/cli.go
git commit -m "feat(plugin/autosuggest): add CLI suggest list/approve/reject"
```

---

## Task 11: Integration Test

**Files:**
- create: `plugins/autosuggest/plugin_test.go`

**Step 11.1 — write integration test**

Create `plugins/autosuggest/plugin_test.go`:

```go
package autosuggest_test

import (
    "context"
    "testing"

    autosuggest "github.com/ideacrafterslabs/ctxt-plugin-autosuggest"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestPluginE2E_SelectMode simulates the full pipeline step + pending retrieve flow.
func TestPluginE2E_SelectMode(t *testing.T) {
    cfg := autosuggest.AutoSuggestConfig{
        Mode:        "select",
        MaxTags:     3,
        MaxMentions: 2,
        Enabled:     true,
    }
    llm := &mockLLM{response: `{"tags":["golang","testing"],"mentions":["eng.qa"]}`}
    step := autosuggest.NewAutoSuggestStep(cfg, llm)

    obj := &storage.KnowledgeObject{
        ID:         "e2e_001",
        RawContent: "Integration testing strategy for Go services in the eng.qa team.",
    }

    // Run step.
    out, err := step.Run(context.Background(), obj)
    require.NoError(t, err)

    // In select mode: tags NOT applied directly.
    assert.Empty(t, out.Tags, "select mode must not mutate Tags directly")

    // Pending suggestions stored.
    tags, mentions := autosuggest.GetPending(out)
    assert.Equal(t, []string{"golang", "testing"}, tags)
    assert.Equal(t, []string{"eng.qa"}, mentions)

    // Approve: apply and clear.
    require.NoError(t, autosuggest.ApplyGenerate(out, tags, mentions))
    autosuggest.ClearPending(out)

    labels := make([]string, len(out.Tags))
    for i, t := range out.Tags {
        labels[i] = t.Label
    }
    assert.Contains(t, labels, "golang")
    assert.Contains(t, labels, "testing")
    t2, m2 := autosuggest.GetPending(out)
    assert.Empty(t, t2)
    assert.Empty(t, m2)
}

// TestPluginE2E_GenerateMode simulates auto-apply flow.
func TestPluginE2E_GenerateMode(t *testing.T) {
    cfg := autosuggest.AutoSuggestConfig{
        Mode:        "generate",
        MaxTags:     5,
        MaxMentions: 3,
        Enabled:     true,
    }
    llm := &mockLLM{response: `{"tags":["plugin","architecture"],"mentions":[]}`}
    step := autosuggest.NewAutoSuggestStep(cfg, llm)

    obj := &storage.KnowledgeObject{
        ID:         "e2e_002",
        RawContent: "Plugin architecture for extending dPKMS.",
    }

    out, err := step.Run(context.Background(), obj)
    require.NoError(t, err)

    labels := make([]string, len(out.Tags))
    for i, t := range out.Tags {
        labels[i] = t.Label
    }
    assert.Contains(t, labels, "plugin")
    assert.Contains(t, labels, "architecture")
}
```

**Step 11.2 — run, commit**

```bash
cd plugins/autosuggest && go test ./... -v
```

Expected: all tests `PASS`.

```
git add plugins/autosuggest/plugin_test.go
git commit -m "test(plugin/autosuggest): add E2E integration tests"
```

---

## Task 12: Extraction-Readiness Verification

**Step 12.1 — verify standalone build**

```bash
cd /Users/jadb/.w/ideacrafterslabs/ctxt/plugins/autosuggest
go build ./...
```

Expected: succeeds with no errors.

**Step 12.2 — verify tests pass standalone**

```bash
go test ./... -count=1
```

Expected: `PASS`.

**Step 12.3 — check no internal relative imports leak**

```bash
grep -r "\"\.\./" /Users/jadb/.w/ideacrafterslabs/ctxt/plugins/autosuggest/
```

Expected: zero output (all cross-references go through the module path `github.com/ideacrafterslabs/ctxt`).

**Step 12.4 — document extraction procedure**

To extract to standalone repo:

1. Copy `plugins/autosuggest/` to new repo root.
2. In `go.mod`, change `replace github.com/ideacrafterslabs/ctxt => ../..` to a proper version tag once the core module is tagged.
3. Remove entry from parent `go.work`.
4. Publish as `github.com/ideacrafterslabs/ctxt-plugin-autosuggest`.

**Step 12.5 — commit**

```
git add .
git commit -m "chore(plugin/autosuggest): verify extraction-readiness"
```

---

## Example config.yaml

```yaml
plugins:
  - plugin: autosuggest
    type: pipeline-step
    config:
      enabled: true
      mode: select         # or "generate"
      max_tags: 5
      max_mentions: 3
      vocabulary_hint:
        - architecture
        - decision
        - golang
        - plugin
```

---

## TDD Cycle Summary

```
T0: go test ./internal/plugin/...          → PASS → commit
T1: go build ./plugins/autosuggest/...     → PASS → commit
T2: go test -run TestConfig                → PASS → commit
T3: go test -run TestSuggest               → PASS → commit
T4: go test -run TestApplyGenerate         → PASS → commit
T5: go test -run TestApplySelect           → PASS → commit
T6: go test -run TestAutoSuggestStep       → PASS → commit
T7: go build ./plugins/autosuggest/...     → PASS → commit
T8: go build ./cmd/...                     → PASS → commit
T9: go test ./internal/server/http/...     → PASS → commit
T10: go build ./plugins/autosuggest/...    → PASS → commit
T11: go test ./plugins/autosuggest/...     → all PASS → commit
T12: standalone go build + go test         → PASS → commit
```
