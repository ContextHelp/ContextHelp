# ICA Plugin

`plugins/ica/` is a plugin that maps the ICA content aggregator's feed API and
processor service onto ctxt pipeline steps. The package builds and is tested,
but **neither the `ctxt` nor the `dpkms` binary loads it**: nothing registers
plugins at startup, and the `plugins:` config key is parsed but not read. The
ICA steps are therefore not in any running pipeline registry, and
`dpkms pipeline step list` does not show them.

This page documents the package for contributors.

---

## Init config

`Init` receives a `map[string]interface{}` parsed by `ConfigFromMap`
(`plugins/ica/config.go`). No config file key delivers this map today.

| Key | Type | Default | Effect |
|-----|------|---------|--------|
| `api_url` | string | `""` | ICA API base URL; validated with `url.ParseRequestURI`. Enables `ica_feed_manager` and `ica_fetcher` |
| `processor_url` | string | `""` | ICA processor base URL. Enables `ica_processor` |
| `dist_channels_as_mentions` | bool | `false` | Passed to the normalizer constructor |
| `default_feed_pipeline` | bool | `false` | Parsed and stored; no code reads it |

At `Init` the plugin sends `GET <url>/health` (2 s timeout) to each configured
URL and records the result, exposed through `HasAPI()` and `HasProcessor()`.
Probe failures are logged and never fail `Init`. The probe results do **not**
change which steps `PipelineSteps()` returns: that depends only on which URLs are
set (and on a step factory being wired, see below).

---

## Pipeline steps

`PipelineSteps()` returns nothing unless the plugin was built with
`NewWithSteps(steps.Factory())`; `New()` has no step factory.

| Step | Requires | Produces | Capabilities | Returned when |
|------|----------|----------|--------------|---------------|
| `ica_feed_manager` | `Source` | `Metadata` | `ica_api` | `api_url` set |
| `ica_fetcher` | `Source` | `RawContent`, `Metadata` | `ica_api` | `api_url` set |
| `ica_normalizer` | `Metadata` | `Metadata`, `Mentions`, `Sections`, `Tags` | none | always |
| `ica_processor` | `RawContent` | `Metadata`, `Sections` | `ica_processor` | `processor_url` set |

The `ica_api` and `ica_processor` capability names are not part of the core
capability set (`internal/pipeline/builtins/capabilities.go`), so a built-in
pipeline that listed these steps would prune them.

### Upstream calls

| Step | Request |
|------|---------|
| `ica_feed_manager` | `GET {api_url}/api/v1/feeds?url=...`; on 404, `POST {api_url}/api/v1/feeds` |
| `ica_fetcher` | `GET {api_url}/api/v1/feeds/sync?url=...` with `If-None-Match` / `If-Modified-Since` when known |
| `ica_processor` | `POST {processor_url}/process` |

### Error handling

| Step | Condition | Behaviour |
|------|-----------|-----------|
| `ica_feed_manager` | Feed not found | Creates the feed, continues |
| `ica_fetcher` | HTTP 304 | Sets `Metadata["not_modified"] = true` |
| `ica_fetcher` | HTTP 404 / 410 | Sets `Metadata["feed_gone"] = true` |
| `ica_normalizer` | No `feed_items` in metadata | No-op |
| `ica_processor` | HTTP 4xx | Permanent error, no retry |
| `ica_processor` | HTTP 5xx or network error | Up to 3 attempts, waiting 1 s then 2 s |

`plugins/ica/pipeline.go` declares a pipeline definition `ica.feed_sync`
(`ica_feed_manager`, `ica_fetcher`, `ica_normalizer`, `item_deduplicator`,
`ica_processor`, `alternative_detector`, `item_enqueuer`) through
`PipelineDefs()`. No core code reads `PipelineDefs()`, so this pipeline is not
registered.

---

## Field mapping

`NormalizedItemToDraft` (`plugins/ica/convert.go`) converts an ICA
`NormalizedItem` into a `KnowledgeObject`; `DraftToNormalizedItem` converts back
for the processor request.

| NormalizedItem field | KnowledgeObject field |
|----------------------|-----------------------|
| `ID` | `ID` |
| `ArticleBody` | `RawContent` and `TextContent` |
| `ContentHash` | `ContentHash` |
| `CanonicalURL` | `Source` |
| `Title`, `Description`, `Language` | `Metadata["title"]`, `["description"]`, `["language"]` |
| `PrimarySource.Domain`, `.Publisher`, `.AuthorOrSpeaker`, `.FeedURL` | `Metadata["domain"]`, `["publisher"]`, `["author"]`, `["feed_url"]` |
| `PublishedAt` | `Metadata["published_at"]` (RFC 3339) |
| `ChannelCount` | `Metadata["channel_count"]` (omitted when 0) |
| `MediaAssets` | `Metadata["media_assets"]` (`type`, `url`, `duration_seconds`) |
| `MediaTypesPresent` | `Tags` labelled `media:<type>`, source `media` |
| `DistChannels` | `Mentions` with scheme `https`, namespace = channel domain, ID = channel URL |
| `ExtractedEntities` | `Mentions` with scheme `ctxt`, namespace `entity`, ID `<type>/<value>` |
| `StructuredAnalysis.Chapters` | `Sections` (title = phase, content = voiceover summary) |

`NormalizedItem.Embedding` is not mapped. `Type`/`Subtype` are inferred from
`MediaTypesPresent`: video, then audio, then image give `media`/`<kind>`;
otherwise `article`.

---

## Key files

| File | Purpose |
|------|---------|
| `plugins/ica/plugin.go` | `Plugin`: `Init`, health probes, `PipelineSteps`, no-op `PostIngest` |
| `plugins/ica/config.go` | `Config`, `ConfigFromMap` |
| `plugins/ica/pipeline.go` | `PipelineDefs()` (unused by core) |
| `plugins/ica/convert.go` | Field mapping and type inference |
| `plugins/ica/types.go` | Local copies of ICA types |
| `plugins/ica/steps/` | The four steps and `Factory()` |

### Running tests

```bash
go test ./plugins/ica/...
```

---

## See also

- [Plugin authoring guide](../plugin-authoring-guide.md) — the plugin API and its current wiring state
