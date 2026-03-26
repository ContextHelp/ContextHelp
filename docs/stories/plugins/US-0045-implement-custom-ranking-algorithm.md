# US-0045: Implement Custom Ranking Algorithm

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As a platform integrator, I want to implement a custom ranking algorithm plugin so that I can
replace or augment the default proximity/relevance scoring with domain-specific logic (e.g.,
recency-weighted scoring, authority signals, citation counts) without modifying core search code.

---

## Context

The system uses a two-layer ranking model:

1. **Proximity scoring** (`internal/proximity/proximity.go`) — five-factor score
   (semantic, temporal, entity, origin, behavioral) stored in the `proximity_scores` table
2. **Retrieval ranking** (`internal/retrieval/`) — RAG-mode vector similarity or LLM-mode
   reranking, configured via `retrieval.method` in `config.yaml`

A custom ranking plugin contributes a `PipelineStep` (post-enrichment) that rewrites the
`Metadata` fields that downstream ranking reads, or it hooks `PostIngest` to update proximity
factors after object creation.

Alternatively, the plugin implements `pluginapi.PostIngestHook` to run custom scoring logic
after every ingest event and write results to `KnowledgeObject.Plugins["my-ranking"]`.

The `Plugins` field (`map[string]any`) on `KnowledgeObject` is the designated namespace for
plugin-specific data persisted alongside the object.

---

## Acceptance Criteria

- **Plugin satisfies `pluginapi.Plugin`** and optionally `pluginapi.PostIngestHook`
- **Custom scores computed** — after ingest, the object's `Plugins["my-ranking"]` map (or
  metadata fields) contains the custom score values
- **Server-side storage updated** — `knowledge_objects.plugins` JSON column contains
  plugin-specific ranking data after job completion
- **Ranking does not break existing queries** — objects without plugin data are still returned
  by search/list endpoints without error
- **Config-driven weights** — ranking weights (e.g., recency weight, authority weight) are
  read from `plugins[n].config` at `Init` time
- **PostIngest hook registered** — `PostIngestHooks()` in the plugin registry includes this
  plugin's instance after server start
- **Events published** — plugin optionally publishes a `ranking.scored` CloudEvent via
  `deps.Bus` after each scoring run (event type and payload documented)

---

## Implementation Notes

### PostIngestHook Implementation

```go
type MyRankingPlugin struct {
    recencyWeight float64
    bus           pluginapi.Bus
}

func (p *MyRankingPlugin) Name() string    { return "my-ranking" }
func (p *MyRankingPlugin) Version() string { return "1.0.0" }

func (p *MyRankingPlugin) Init(
    _ context.Context, cfg map[string]interface{}, deps pluginapi.Deps,
) error {
    if w, ok := cfg["recency_weight"].(float64); ok {
        p.recencyWeight = w
    } else {
        p.recencyWeight = 0.5
    }
    p.bus = deps.Bus
    return nil
}

func (p *MyRankingPlugin) PipelineSteps() []pluginapi.PipelineStep { return nil }
func (p *MyRankingPlugin) Close(_ context.Context) error           { return nil }

// PostIngest implements pluginapi.PostIngestHook.
func (p *MyRankingPlugin) PostIngest(
    ctx context.Context, obj *pluginapi.KnowledgeObject,
) error {
    score := p.computeScore(obj)
    if obj.Plugins == nil {
        obj.Plugins = map[string]any{}
    }
    obj.Plugins["my-ranking"] = map[string]any{
        "score":          score,
        "recency_weight": p.recencyWeight,
    }
    if p.bus != nil {
        ev, _ := events.NewEvent("my-ranking", "ranking.scored", obj)
        _ = p.bus.Publish(ctx, ev)
    }
    return nil
}
```

### Pipeline Step Variant

If ranking is implemented as a step rather than a hook, it reads input fields and writes to
`Metadata` or `Plugins`:

```go
func (s *MyRankingStep) Contract() pluginapi.StepContract {
    return pluginapi.StepContract{
        Requires:     []string{"RawContent", "Tags", "Mentions"},
        Produces:     []string{"Metadata"},
        Capabilities: []string{},
    }
}
```

### Config Declaration

```yaml
plugins:
  - plugin: my-ranking
    config:
      recency_weight: 0.6
      authority_weight: 0.4
```

### Storage Reference

`knowledge_objects.plugins` is a JSON column. After the hook runs, the column contains:

```json
{
  "my-ranking": {
    "score": 0.82,
    "recency_weight": 0.6
  }
}
```

---

## E2E Test Checklist

- [ ] Register plugin implementing `PostIngestHook`; start server — logs show
  `plugin: my-ranking 1.0.0 initialised`
- [ ] `Init` reads `recency_weight` from config; verify plugin uses configured value (not
  default) when weight is explicitly set in `plugins[n].config`
- [ ] `Init` with missing `recency_weight` → uses default (0.5); server starts without error
- [ ] Enqueue content via `dpkms pipeline enqueue <content>`; verify POST
  `/api/v1/pipelines/enqueue` request body contains
  `{"content":..., "type":"text"}` — pipeline field present (even if empty, confirming
  request structure)
- [ ] Wait for job completion; GET `/api/v1/objects/<id>` — response body contains `plugins`
  field with `"my-ranking"` key and `score` sub-field (non-zero float)
- [ ] Verify DB directly: `SELECT plugins FROM knowledge_objects WHERE id=?` — JSON column
  contains `{"my-ranking":{"score":...,"recency_weight":...}}`
- [ ] Enqueue two objects; verify both have distinct `plugins["my-ranking"]["score"]` values
  reflecting content-specific scoring (not a fixed constant)
- [ ] Query `GET /api/v1/objects` (list) — objects without `plugins` data (pre-existing)
  returned without error; no null-pointer panics in server logs
- [ ] `PostIngestHooks()` on plugin registry returns slice containing the ranking plugin
  instance after server start — verify via server startup log or registry introspection
- [ ] Plugin publishes `ranking.scored` CloudEvent via bus after each `PostIngest` call;
  subscribe a test handler and verify event `Type` and `Data` fields match expected shape
- [ ] Stop server — `Close()` called without error; no goroutine leaks in server log
- [ ] Two ranking plugins registered; both `PostIngest` called in order; both scores stored
  in `plugins` map under distinct keys

---

## Related Stories

- [US-0042](./US-0042-implement-custom-enrichment-plugin.md) - Custom enrichment plugin
- [US-0043](./US-0043-implement-custom-ai-provider-plugin.md) - Custom AI provider plugin
- [US-0044](./US-0044-implement-registry-adapter-plugin.md) - Registry adapter plugin
- [US-0029](../admin/US-0029-install-and-enable-plugin.md) - Install and enable plugin
- [US-0018](../search/US-0018-multi-strategy-search-execution.md) - Multi-strategy search
- [US-0021](../search/US-0021-search-with-result-explanation.md) - Search result explanation

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [OSS Go Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/contributors/oss-go-developer.md)

---

## E2E Tests

> Not yet implemented.
