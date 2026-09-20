# ICA Plugin (Bundled)

The **ICA plugin** connects ctxt to the ICA content aggregation
platform. It bridges ICA's feed API and processor service into the
ctxt pipeline system, enabling automated feed sync, content
normalization, and enrichment without manual ingestion.

---

## Overview

ICA (Idea Crafters Aggregator) manages feed discovery, content
fetching, and structured analysis. The ctxt plugin wraps two
upstream services:

- **ICA API** — feed CRUD, item fetching, conditional sync
- **ICA Processor** — NLP enrichment, embeddings, chapter
  extraction, entity recognition

Both are optional. The plugin probes each service's `/health`
endpoint at init and gates pipeline steps accordingly. A minimal
setup with only the API URL still provides feed sync and
normalization; adding the processor URL enables deep enrichment.

---

## Config

Add under `plugins.ica` in `config.yaml`:

```yaml
plugins:
  ica:
    api_url: https://api.example.com
    processor_url: https://processor.example.com
    default_feed_pipeline: false
    dist_channels_as_mentions: false
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `api_url` | `string` | `""` | Base URL of ICA API server. Must be valid URI. Empty disables API steps. |
| `processor_url` | `string` | `""` | Base URL of ICA processor service. Empty disables processor step. |
| `default_feed_pipeline` | `bool` | `false` | When true, ICA becomes the default feed pipeline for all feed-type objects. |
| `dist_channels_as_mentions` | `bool` | `false` | Map distribution channel URIs to KnowledgeObject mentions. Affects scoring. |

---

## Capabilities

The plugin probes upstream services at init (2s timeout GET to
`/health`). Results gate which pipeline steps are available.

| Capability | Probe | Steps gated |
|------------|-------|-------------|
| `ica_api` | `{api_url}/health` returns 200 | `ica_feed_manager`, `ica_fetcher` |
| `ica_processor` | `{processor_url}/health` returns 200 | `ica_processor` |

Probe failures are logged but non-fatal. The plugin initialises
successfully even when both services are unreachable; gated steps
simply won't appear in `PipelineSteps()`.

---

## Pipeline Steps

| Step | Requires | Produces | Capabilities | Description |
|------|----------|----------|--------------|-------------|
| `ica_feed_manager` | `Source` | `Metadata` | `ica_api` | Ensure feed exists in ICA; populate feed metadata (id, status, sync time, item count) |
| `ica_fetcher` | `Source` | `RawContent`, `Metadata` | `ica_api` | Fetch feed items via `/api/v1/feeds/sync`; supports conditional requests (ETag, Last-Modified) |
| `ica_normalizer` | `Metadata` | `Metadata`, `Mentions`, `Sections`, `Tags` | *(none)* | Map raw feed items to typed KO fields via `NormalizedItemToDraft` |
| `ica_processor` | `RawContent` | `Embeddings`, `Metadata`, `Sections` | `ica_processor` | Send content to processor service for NLP enrichment; retry with exponential backoff |

---

## Pipeline: ica.feed_sync

The plugin registers one pipeline definition:

```
ica.feed_sync
├── ica_feed_manager       # ensure feed exists (ica_api)
├── ica_fetcher            # fetch items (ica_api)
├── ica_normalizer         # normalize items (always)
├── item_deduplicator      # core step: skip seen hashes
├── ica_processor          # NLP enrichment (ica_processor)
├── alternative_detector   # core step: detect duplicates
└── item_enqueuer          # core step: queue items
```

### Degradation behavior

Steps requiring unavailable capabilities are skipped at runtime.
The pipeline remains functional as long as `ica_normalizer` (which
has no capability requirements) can run.

| Service down | Effect |
|--------------|--------|
| API unreachable | `ica_feed_manager` + `ica_fetcher` skipped; pipeline needs pre-populated `Metadata.feed_items` |
| Processor unreachable | `ica_processor` skipped; no embeddings, chapters, or entity extraction |
| Both unreachable | Only `ica_normalizer` + core steps run; requires pre-seeded data |

---

## Field Mapping

`NormalizedItemToDraft` converts ICA's `NormalizedItem` into a
ctxt `KnowledgeObject`. Reverse conversion via
`DraftToNormalizedItem` is used by the processor step.

| NormalizedItem field | KnowledgeObject field | Notes |
|---------------------|-----------------------|-------|
| `ID` | `ID` | direct |
| `ArticleBody` | `RawContent`, `TextContent` | both set to same value |
| `ContentHash` | `ContentHash` | direct |
| `CanonicalURL` | `Source` | direct |
| `Embedding` | `Embeddings` | `[]float32` |
| `Title` | `Metadata["title"]` | |
| `Description` | `Metadata["description"]` | |
| `Language` | `Metadata["language"]` | |
| `PrimarySource.Domain` | `Metadata["domain"]` | |
| `PrimarySource.Publisher` | `Metadata["publisher"]` | |
| `PrimarySource.AuthorOrSpeaker` | `Metadata["author"]` | |
| `PrimarySource.FeedURL` | `Metadata["feed_url"]` | |
| `PublishedAt` | `Metadata["published_at"]` | RFC3339 string |
| `ChannelCount` | `Metadata["channel_count"]` | int; omitted if 0 |
| `MediaAssets` | `Metadata["media_assets"]` | `[]map` with type, url, duration_seconds |
| `MediaTypesPresent` | `Tags` | `media:<type>` labels (source=media) |
| `DistChannels` | `Mentions` | `https://<domain>/<url>` URIs |
| `ExtractedEntities` | `Mentions` | `ctxt://entity/<type>/<value>` URIs |
| `StructuredAnalysis.Chapters` | `Sections` | phase → title; voiceover_summary → content |

### Type inference

KO `Type` and `Subtype` are inferred from `MediaTypesPresent`:

| Media types present | Type | Subtype |
|--------------------|------|---------|
| contains `video` | `media` | `video` |
| contains `audio` | `media` | `audio` |
| contains `image` | `media` | `image` |
| none / text only | `article` | `""` |

Priority: video > audio > image > article.

---

## AlternativeDetector Integration

When `dist_channels_as_mentions` is `true`, distribution channels
from `NormalizedItem.DistChannels` are appended to
`KnowledgeObject.Mentions` as `https://` URIs:

```
scheme: https
space:  <channel.Domain>    (e.g. "youtube.com")
id:     <channel.URL>       (e.g. "https://youtube.com/watch?v=...")
```

The `alternative_detector` core step uses mentions to find
objects sharing the same content across different distribution
channels. More mentions = higher chance of deduplication across
sources, which affects scoring and surfacing.

When `false`, distribution channels are ignored; only extracted
entities produce mentions.

---

## Usage Examples

### Full setup (API + processor)

```yaml
plugins:
  ica:
    api_url: https://ica-api.internal:8080
    processor_url: https://ica-proc.internal:8081
    default_feed_pipeline: true
    dist_channels_as_mentions: true
```

All four ICA steps active. Feeds auto-sync, items normalized,
content enriched with embeddings and chapters, cross-channel
deduplication enabled.

### API only (no processor)

```yaml
plugins:
  ica:
    api_url: https://ica-api.internal:8080
```

Feed management and fetching via ICA API. Normalization runs
locally. No embeddings, chapters, or entity extraction.
`ica_processor` step skipped.

### Mixing ICA steps into other pipelines

ICA steps are registered in the step registry like any plugin
step. Override a custom pipeline to include specific ICA steps:

```yaml
pipelines:
  overrides:
    my_custom_feed:
      steps:
        - url_fetcher
        - ica_normalizer      # reuse ICA normalization
        - item_deduplicator
        - ica_processor       # reuse ICA processor
        - item_enqueuer
```

Requires `ica` plugin enabled with appropriate URLs configured.
Steps requiring unavailable capabilities are skipped.

---

## Error Handling

| Step | Error type | Behavior |
|------|-----------|----------|
| `ica_feed_manager` | Network / HTTP error | Step fails; pipeline halts |
| `ica_feed_manager` | Feed not found | Creates new feed via POST; continues |
| `ica_fetcher` | HTTP 304 Not Modified | Sets `Metadata["not_modified"] = true`; continues |
| `ica_fetcher` | HTTP 404/410 | Sets `Metadata["feed_gone"] = true`; continues |
| `ica_fetcher` | Other HTTP error | Step fails; pipeline halts |
| `ica_normalizer` | No `feed_items` in metadata | No-op; returns draft unchanged |
| `ica_normalizer` | JSON unmarshal error | Step fails; pipeline halts |
| `ica_processor` | HTTP 4xx | Permanent error; no retry |
| `ica_processor` | HTTP 5xx / network | Retries up to 3x with exponential backoff (1s, 2s, 4s) |
| `ica_processor` | Retries exhausted | Step fails; pipeline halts |

---

## Contributor Reference

### Key Files

| File | Purpose |
|------|---------|
| `plugins/ica/plugin.go` | Plugin struct; Init, PipelineSteps, capability probing |
| `plugins/ica/config.go` | Config struct, validation, `ConfigFromMap` |
| `plugins/ica/pipeline.go` | `PipelineDefs()` — declares `ica.feed_sync` |
| `plugins/ica/convert.go` | `NormalizedItemToDraft`, `DraftToNormalizedItem`, type inference |
| `plugins/ica/types.go` | Local mirrors of ICA domain types (NormalizedItem, etc.) |
| `plugins/ica/steps/feed_manager.go` | `ica_feed_manager` step |
| `plugins/ica/steps/fetcher.go` | `ica_fetcher` step |
| `plugins/ica/steps/normalizer.go` | `ica_normalizer` step |
| `plugins/ica/steps/processor.go` | `ica_processor` step with retry logic |

### Interfaces

The plugin implements:

- **`pluginapi.Plugin`** — `Init`, `Close`, `PipelineSteps`
- **`pluginapi.PostIngestHook`** — `PostIngest` (currently no-op)

### Running Tests

```bash
GOWORK=off go test ./plugins/ica/...
```
