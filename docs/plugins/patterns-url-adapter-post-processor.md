# Plugin Patterns: URL Adapter & Post-Processor

Two of the most useful plugin patterns in ctxt:

- **URL Adapter** — route a URL pattern to a custom pipeline with a dedicated
  fetcher + mapper
- **Post-Processor** — run enrichment logic after any object lands in storage

This document covers each pattern independently, then shows how to compose them
in a single plugin.

---

## Contents

1. [URL Adapter](#1-url-adapter)
2. [Post-Processor](#2-post-processor)
3. [Composition: both in one plugin](#3-composition-both-in-one-plugin)
4. [When to use which](#4-when-to-use-which)
5. [See Also](#5-see-also)

---

## 1. URL Adapter

### What it does

A URL adapter intercepts an incoming URL before the default `url.generic`
pipeline runs. It:

1. Registers a `URLPatternDetector` — routes matching URLs to the named pipeline
2. Registers a named pipeline — a sequence of steps tailored to that URL class
3. Contributes at least two steps: a **fetcher** (produces typed data) and a
   **mapper** (writes standard `KnowledgeObject` fields)

The pattern is additive: the rest of the pipeline system is unchanged.

### Interface contracts

Three types from `pkg/pluginapi` and `internal/pipeline` drive this pattern.

**`PipelineStep`** — one transformation unit:

```pseudocode
interface PipelineStep {
    Name()     string
    Contract() StepContract   // Requires + Produces field names
    Run(ctx, draft *KnowledgeObject) (*KnowledgeObject, error)
}
```

**`Detector` / `URLPatternDetector`** — decides which pipeline to use:

```pseudocode
interface Detector {
    Detect(in DetectInput) (pipelineName string, err error)
    // return ("", ErrDelegate) to pass control to the next detector
}

// Concrete ready-made implementation:
URLPatternDetector {
    PipelineName  string
    Pattern       *regexp.Regexp
}
```

`DetectInput` carries `.Source` (URL or path), `.ContentType`, and `.Sniff`
(first ~512 bytes of content). The URL adapter only needs `.Source`.

**`Plugin`** — lifecycle entry point:

```pseudocode
interface Plugin {
    Name()    string
    Version() string
    Init(ctx, cfg map[string]any, deps Deps) error
    PipelineSteps() []PipelineStep
    Close(ctx) error
}
```

`PipelineSteps()` is how a plugin contributes steps. Detector registration
happens inside `Init` via the pipeline registry.

### Annotated example — `url.example.product`

> Exact Go code lives in
> `internal/pipeline/detectors.go` (URLPatternDetector),
> `internal/pipeline/builtins/url_generic.go` (url.generic reference),
> `pkg/pluginapi/pluginapi.go` (interfaces).
> The pseudocode below illustrates the pattern only.

**Step 1 — fetcher** (`product_fetcher`)

```pseudocode
// Reads: Source (URL)
// Writes: Metadata["_product_snapshot"], RawContent

ProductFetcher.Run(ctx, draft):
    snapshot = fetchProductPage(draft.Source)   // call idx or http client
    draft.Metadata["_product_snapshot"] = snapshot
    draft.RawContent = snapshot.PageText
    return draft
```

**Step 2 — mapper** (`product_mapper`)

```pseudocode
// Reads: Metadata["_product_snapshot"]
// Writes: Type, Subtype, TextContent, Sections, Tags, Metadata.*

ProductMapper.Run(ctx, draft):
    snapshot = draft.Metadata["_product_snapshot"]
    draft.Type        = "product"
    draft.Subtype     = "example-store"
    draft.TextContent = snapshot.Description
    draft.Tags        = tagsFrom(snapshot.Categories)
    draft.Metadata["price"]    = snapshot.Price
    draft.Metadata["in_stock"] = snapshot.InStock
    return draft
```

**Plugin wiring**

```pseudocode
ExampleStorePlugin.Init(ctx, cfg, deps):
    pattern = compile("^https://store\.example\.com/products/[^/]+$")
    detector = NewURLPatternDetector("url.example.product", pattern)
    pipelineRegistry.RegisterDetector(detector)

    pipelineRegistry.Register("url.example.product", Pipeline{
        Steps: [ProductFetcher, ProductMapper, Tagger, Embedding],
    })

ExampleStorePlugin.PipelineSteps():
    return [ProductFetcher, ProductMapper]
```

**Manifest (`plugin.json`)**

```pseudocode
{
  name: "example-store",
  version: "1.0.0",
  hooks: [],
  pipelines: ["url.example.product"],
  permissions: { network: true }
}
```

**Config block**

```yaml
plugins:
  example-store:
    enabled: true
    timeout_seconds: 15
```

### Key rules

- **Fetcher must set `Metadata["_<plugin>_snapshot"]`** — mapper reads it; keeps
  steps decoupled.
- **Mapper must clear the snapshot key** after mapping — avoids bloating stored
  objects.
- **`Contract().Requires/Produces`** must be accurate — pipeline composability
  validation checks them at build time.
- **Return `ErrDelegate`** from `Detect` when the pattern does not match — never
  return `nil` with an empty name.
- **Register the detector before registering the pipeline** — the registry
  runs detectors in order; ordering matters when patterns overlap.

---

## 2. Post-Processor

### What it does

A post-processor runs **after** a knowledge object has been written to storage.
It:

- Implements `PostIngestHook` in addition to `Plugin`
- Receives the fully-enriched `*KnowledgeObject`
- Writes derived data back via the API (edges, metadata fields, queued jobs)
- Never re-runs the pipeline itself

Use this to enrich objects whose enrichment depends on data that only exists
after ingestion (storage queries, cross-object links, external API calls).

### Interface contract

```pseudocode
interface PostIngestHook extends Plugin {
    PostIngest(ctx, obj *KnowledgeObject) error
}
```

`PostIngest` is called once per object, synchronously after storage write.
**Keep it fast or enqueue async work** — blocking here delays the API response.

The plugin registry calls `PostIngestHooks()` to collect all registered
implementations; the service wires the call site. See
`internal/plugin/registry.go` and `internal/plugin/plugin.go`.

### Annotated example — `dependency_enricher`

Fires on `type: "repo"`. Reads `metadata.dependencies`, checks storage for
existing objects, enqueues ingestion for missing ones, and creates edges.

```pseudocode
DependencyEnricher.PostIngest(ctx, obj):
    if obj.Type != "repo":
        return nil                    // guard: ignore non-repo objects

    deps = obj.Metadata["dependencies"]  // []Dependency set by mapper
    for each dep in deps:
        existing = storage.QueryObjects("source = " + dep.RegistryURL)
        if existing is empty:
            bus.Publish(ctx, Event{
                Type: "ctxt.ingest.requested",
                Data: { url: dep.RegistryURL, pipeline: "url.package" },
            })
        else:
            edgeStore.Create(Edge{
                FromID:   obj.ID,
                ToID:     existing[0].ID,
                Relation: "depends_on",
            })
    return nil
```

**Plugin declaration**

```pseudocode
DependencyEnricher.Name()    = "dependency-enricher"
DependencyEnricher.Version() = "1.0.0"

DependencyEnricher.Init(ctx, cfg, deps):
    d.store = deps.Store
    d.bus   = deps.Bus

DependencyEnricher.PipelineSteps():
    return []         // this plugin contributes no steps

// PostIngest defined above; registry picks it up via type assertion
```

**Manifest**

```pseudocode
{
  name: "dependency-enricher",
  version: "1.0.0",
  hooks: ["post_ingest"],
  permissions: {
    storage: { read: ["objects:*"], write: ["objects:metadata.plugin_dep"] },
    network: false
  }
}
```

### Guard patterns

Post-processors receive **every** ingested object. Always guard early:

```pseudocode
PostIngest(ctx, obj):
    // Guard 1: type filter
    if obj.Type not in ["repo", "package"]:
        return nil

    // Guard 2: subtype filter
    if obj.Subtype != "github":
        return nil

    // Guard 3: required metadata present
    if obj.Metadata["dependencies"] is nil:
        return nil

    // ... actual logic
```

### Async pattern

For heavy work (external API calls, large graph updates), enqueue a job instead
of blocking:

```pseudocode
PostIngest(ctx, obj):
    if not shouldProcess(obj):
        return nil

    bus.Publish(ctx, Event{
        Type: "ctxt.job.enqueue",
        Data: { plugin: "dependency-enricher", object_id: obj.ID },
    })
    return nil   // return fast; actual work runs in background
```

### Key rules

- **Always guard on type/subtype** — post-processors receive all objects.
- **Never write core fields** — write only to `obj.Plugins["<name>"]` or
  `obj.Metadata["plugin_<name>_*"]`.
- **Do not re-ingest** — publish an event or enqueue a job; never call the
  ingest pipeline directly.
- **Return `nil` on skip** — a non-nil error aborts the ingest transaction.
- **Idempotent** — objects may be re-ingested on refresh; post-processors fire
  again each time.

---

## 3. Composition: both in one plugin

A single plugin can implement both patterns. The canonical use case: a URL
adapter that fetches structured data from an external service, and a
post-processor that links the resulting object to related objects already in
storage.

Example: `github-enricher` plugin

- URL adapter: routes `github.com` repo/issue/PR/release URLs to typed
  pipelines, produces structured objects
- Post-processor: after any `type:repo` lands in storage, computes health score,
  detects alternatives, links dependencies

```pseudocode
GitHubPlugin implements Plugin, PostIngestHook

GitHubPlugin.Init(ctx, cfg, deps):
    // URL adapter side
    registry.RegisterDetector(NewURLPatternDetector(
        "url.github.repo",
        compile("^https://github\.com/[^/]+/[^/]+/?$"),
    ))
    registry.Register("url.github.repo", Pipeline{
        Steps: [GHRepoFetcher, GHRepoMapper, Tagger, Embedding],
    })
    // ... register issue, PR, release detectors + pipelines

    // Post-processor side
    d.store = deps.Store
    d.bus   = deps.Bus

GitHubPlugin.PipelineSteps():
    return [GHRepoFetcher, GHRepoMapper, GHIssueFetcher, GHIssueMapper, ...]

GitHubPlugin.PostIngest(ctx, obj):
    switch obj.Subtype:
    case "github" and obj.Type == "repo":
        enqueueHealthScore(obj)
        linkDependencies(ctx, obj)
    default:
        return nil
```

**Manifest**

```pseudocode
{
  name: "github-enricher",
  version: "1.0.0",
  hooks: ["post_ingest"],
  pipelines: ["url.github.repo", "url.github.issue", "url.github.pr"],
  permissions: { network: true, storage: { read: ["objects:*"] } }
}
```

**Config block**

```yaml
plugins:
  github-enricher:
    enabled: true
    health_score: true
    dependency_depth: 1    # 0 = skip, 1 = direct deps only
    alternative_threshold: 0.85
```

---

## 4. When to use which

| Scenario | Pattern |
|---|---|
| New URL class needs custom fetch logic | URL adapter |
| Existing URL class; result needs domain-specific parsing | URL adapter (add detector + mapper) |
| Enrich objects after ingestion based on their content | Post-processor |
| Create cross-object edges (depends_on, alternative, etc.) | Post-processor |
| Compute derived scores from already-stored fields | Post-processor |
| Periodic monitoring of ingested objects | Post-processor + scheduled job |
| Both custom fetch AND cross-object linking | Compose both in one plugin |

**Prefer URL adapter when** the enrichment requires a fetch (the URL is the
primary source of truth).

**Prefer post-processor when** the enrichment derives from data already present
in the `KnowledgeObject` or from querying other stored objects.

---

## 5. See Also

- [`pkg/pluginapi/pluginapi.go`](../../pkg/pluginapi/pluginapi.go) — canonical
  interface definitions (`Plugin`, `PostIngestHook`, `PipelineStep`,
  `KnowledgeObject`)
- [`internal/pipeline/detectors.go`](../../internal/pipeline/detectors.go) —
  `URLPatternDetector`, `ExtensionDetector`, `ContentTestDetector`
- [`internal/pipeline/builtins/url_generic.go`](
  ../../internal/pipeline/builtins/url_generic.go) — `url.generic` reference
  pipeline
- [`internal/plugin/registry.go`](../../internal/plugin/registry.go) —
  `PostIngestHooks()` collection and dispatch
- [plugins-api.md](plugins-api.md) — full plugin API reference
- [plugins.md](plugins.md) — plugin system overview
- [plugin-isolation.md](plugin-isolation.md) — sandbox and permissions
- [plugins-refresh.md](plugins-refresh.md) — scheduling and refresh integration
- [examples/plugins-sample-price-monitor.md](
  examples/plugins-sample-price-monitor.md) — post-processor example (price
  monitoring)
- [docs/plans/2026-03-25-github-ingestion-adapters.md](
  ../plans/2026-03-25-github-ingestion-adapters.md) — reference implementation
  plan combining both patterns
