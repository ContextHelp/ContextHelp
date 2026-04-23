# User Stories — dPKMS + `ctxt`

This directory contains detailed user stories organized by workflow and linkable to personas and system types.

All stories use **US-XXXX** numbering (e.g., US-0001, US-0002) for easy reference and filename compatibility.

---

## System Types

Each story can be implemented on one or more of these deployment models:

| System Type | Description | Deployment |
|------------|-------------|-----------|
| **dpkms (self-hosted)** | Locally-hosted knowledge substrate; full control, sovereign data | Single machine, Docker, Kubernetes |
| **dpkms cloud** | Managed dPKMS service; zero-ops backend, optional `ctxt` frontend | SaaS managed service |
| **ctxt** | Full application layer (capture, enrichment, search, composition); requires dPKMS backend | Web, CLI, TUI, mobile |

---

## Story Categories

### Ingestion & Capture (US-0001 to US-0008)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0001** | [text-capture-minimal-friction](./ingestion/US-0001-text-capture-minimal-friction.md) | ctxt | Knowledge Workers |
| **US-0002** | [url-capture-and-extraction](./ingestion/US-0002-url-capture-and-extraction.md) | ctxt | Knowledge Workers |
| **US-0003** | [image-ocr-and-analysis](./ingestion/US-0003-image-ocr-and-analysis.md) | ctxt, dpkms (self-hosted) | Knowledge Workers, Agents/LLMs |
| **US-0004** | [audio-transcription-and-indexing](./ingestion/US-0004-audio-transcription-and-indexing.md) | ctxt, dpkms (self-hosted) | Knowledge Workers |
| **US-0005** | [video-processing-with-scenes](./ingestion/US-0005-video-processing-with-scenes.md) | ctxt, dpkms (self-hosted) | Knowledge Workers, Agents/LLMs |
| **US-0006** | [document-parsing-and-decomposition](./ingestion/US-0006-document-parsing-and-decomposition.md) | ctxt, dpkms (self-hosted) | Knowledge Workers, Maintainers |
| **US-0007** | [feed-ingestion-and-sync](./ingestion/US-0007-feed-ingestion-and-sync.md) | ctxt, dpkms (self-hosted) | Knowledge Workers, Operations |
| **US-0008** | [batch-import-from-file](./ingestion/US-0008-batch-import-from-file.md) | ctxt, dpkms (self-hosted), dpkms cloud | Maintainers, Operations |

#### Importer Stories (US-0300 to US-0317)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0300** | [importer-extension-interface](./ingestion/US-0300-importer-extension-interface.md) | dpkms (self-hosted), dpkms cloud, ctxt | Platform Integrators, Maintainers |
| **US-0301** | [import-chrome-bookmarks](./ingestion/US-0301-import-chrome-bookmarks.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0302** | [import-edge-bookmarks](./ingestion/US-0302-import-edge-bookmarks.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0303** | [import-firefox-bookmarks](./ingestion/US-0303-import-firefox-bookmarks.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0304** | [import-safari-bookmarks](./ingestion/US-0304-import-safari-bookmarks.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0305** | [import-google-drive](./ingestion/US-0305-import-google-drive.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0306** | [import-onedrive](./ingestion/US-0306-import-onedrive.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0307** | [import-notion](./ingestion/US-0307-import-notion.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0308** | [import-dropbox](./ingestion/US-0308-import-dropbox.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0309** | [import-slack](./ingestion/US-0309-import-slack.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0310** | [import-discord](./ingestion/US-0310-import-discord.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0311** | [import-obsidian-vault](./ingestion/US-0311-import-obsidian-vault.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0312** | [import-logseq-graph](./ingestion/US-0312-import-logseq-graph.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0313** | [import-evernote-enex](./ingestion/US-0313-import-evernote-enex.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0314** | [import-pinboard-bookmarks](./ingestion/US-0314-import-pinboard-bookmarks.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0315** | [import-raindrop-bookmarks](./ingestion/US-0315-import-raindrop-bookmarks.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0316** | [import-twitter-archive](./ingestion/US-0316-import-twitter-archive.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |
| **US-0317** | [import-linkedin-export](./ingestion/US-0317-import-linkedin-export.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Operations, Maintainers |

### Platform Capture (US-0200 to US-0210)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0200** | [browser-cookie-bridge](./capture/US-0200-browser-cookie-bridge.md) | ctxt | Knowledge Workers, Researchers & OSINT Analysts |
| **US-0201** | [x-twitter-capture](./capture/US-0201-x-twitter-capture.md) | ctxt | Knowledge Workers, Researchers & OSINT Analysts |
| **US-0202** | [github-capture](./capture/US-0202-github-capture.md) | ctxt | Knowledge Workers, Platform Integrators, Researchers & OSINT Analysts |
| **US-0203** | [arxiv-capture](./capture/US-0203-arxiv-capture.md) | ctxt | Knowledge Workers, Researchers & OSINT Analysts, Agents/LLMs |
| **US-0204** | [linkedin-capture](./capture/US-0204-linkedin-capture.md) | ctxt | Knowledge Workers, Researchers & OSINT Analysts |
| **US-0205** | [wikipedia-capture](./capture/US-0205-wikipedia-capture.md) | ctxt | Knowledge Workers, Researchers & OSINT Analysts, Agents/LLMs |
| **US-0206** | [osint-entity-aggregation](./capture/US-0206-osint-entity-aggregation.md) | ctxt, dpkms (self-hosted) | Researchers & OSINT Analysts, Agents/LLMs |
| **US-0207** | [web-tab-capture](./capture/US-0207-web-tab-capture.md) | ctxt | Knowledge Workers, Researchers & OSINT Analysts |
| **US-0208** | [temporal-watch](./capture/US-0208-temporal-watch.md) | ctxt, dpkms (self-hosted) | Researchers & OSINT Analysts, Operations |
| **US-0209** | [authenticated-web-fetch](./capture/US-0209-authenticated-web-fetch.md) | ctxt, dpkms (self-hosted) | Knowledge Workers, Researchers & OSINT Analysts |
| **US-0210** | [cross-platform-entity-resolution](./capture/US-0210-cross-platform-entity-resolution.md) | dpkms (self-hosted), dpkms cloud | Researchers & OSINT Analysts, Agents/LLMs |

### Enrichment & Processing (US-0009 to US-0050)

#### Core Enrichment (US-0009 to US-0015)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0009** | [extract-entities-and-mentions](./enrichment/US-0009-extract-entities-and-mentions.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0010** | [extract-decisions-and-tasks](./enrichment/US-0010-extract-decisions-and-tasks.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0011** | [assign-tags-from-vocabulary](./enrichment/US-0011-assign-tags-from-vocabulary.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0012** | [generate-summaries-and-sections](./enrichment/US-0012-generate-summaries-and-sections.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0013** | [detect-and-extract-code-snippets](./enrichment/US-0013-detect-and-extract-code-snippets.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Platform Integrators |
| **US-0014** | [constrain-extraction-with-lmql](./enrichment/US-0014-constrain-extraction-with-lmql.md) | dpkms (self-hosted) | Agents/LLMs, Platform Integrators |
| **US-0015** | [batch-enrichment-with-progress](./enrichment/US-0015-batch-enrichment-with-progress.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |

#### Graph Extraction (US-0063)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0063** | [unified-graph-extraction-at-ingest](./enrichment/US-0063-unified-graph-extraction-at-ingest.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |

#### Advanced Enrichment (US-0046 to US-0050)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0046** | [extract-relationships-between-entities](./enrichment/US-0046-extract-relationships-between-entities.md) | dpkms (self-hosted) | Agents/LLMs, Knowledge Workers |
| **US-0047** | [extract-temporal-information](./enrichment/US-0047-extract-temporal-information.md) | dpkms (self-hosted) | Agents/LLMs, Knowledge Workers |
| **US-0048** | [detect-sentiment-and-tone](./enrichment/US-0048-detect-sentiment-and-tone.md) | dpkms (self-hosted) | Agents/LLMs, Knowledge Workers |
| **US-0049** | [classify-content-with-taxonomy](./enrichment/US-0049-classify-content-with-taxonomy.md) | dpkms (self-hosted) | Agents/LLMs, Platform Integrators |
| **US-0050** | [extract-code-metrics-and-complexity](./enrichment/US-0050-extract-code-metrics-and-complexity.md) | dpkms (self-hosted) | Agents/LLMs, Platform Integrators |

### Search & Retrieval (US-0016 to US-0055, US-0061)

#### Core Search (US-0016 to US-0021)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0016** | [natural-language-search](./search/US-0016-natural-language-search.md) | ctxt | Knowledge Workers, Agents/LLMs |
| **US-0017** | [structured-rsql-query](./search/US-0017-structured-rsql-query.md) | ctxt, dpkms (self-hosted), dpkms cloud | Agents/LLMs, Maintainers |
| **US-0018** | [multi-strategy-search-execution](./search/US-0018-multi-strategy-search-execution.md) | ctxt, dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0019** | [federated-registry-search](./search/US-0019-federated-registry-search.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0020** | [apply-focus-profile-to-search](./search/US-0020-apply-focus-profile-to-search.md) | ctxt | Knowledge Workers |
| **US-0021** | [search-with-result-explanation](./search/US-0021-search-with-result-explanation.md) | ctxt, dpkms (self-hosted), dpkms cloud | Knowledge Workers, Agents/LLMs |

#### Advanced Search (US-0051 to US-0055)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0051** | [semantic-search-with-embeddings](./search/US-0051-semantic-search-with-embeddings.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0052** | [graph-based-entity-search](./search/US-0052-graph-based-entity-search.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs |
| **US-0053** | [cross-profile-search-aggregation](./search/US-0053-cross-profile-search-aggregation.md) | ctxt | Knowledge Workers |
| **US-0054** | [saved-search-and-alerts](./search/US-0054-saved-search-and-alerts.md) | ctxt | Knowledge Workers |
| **US-0055** | [search-history-and-recommendations](./search/US-0055-search-history-and-recommendations.md) | ctxt | Knowledge Workers |

#### Multimodal Search (US-0061+)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0061** | [visual-similarity-search](./search/US-0061-visual-similarity-search.md) | ctxt, dpkms | Knowledge Workers, Agents/LLMs |

### Composition & Assembly (US-0022 to US-0060)

#### Core Composition (US-0022 to US-0026)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0022** | [generate-brief-from-objects](./composition/US-0022-generate-brief-from-objects.md) | ctxt | Knowledge Workers |
| **US-0023** | [generate-plan-from-decisions](./composition/US-0023-generate-plan-from-decisions.md) | ctxt | Knowledge Workers |
| **US-0024** | [compose-with-graph-traversal](./composition/US-0024-compose-with-graph-traversal.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs |
| **US-0025** | [export-brief-to-markdown-pdf](./composition/US-0025-export-brief-to-markdown-pdf.md) | ctxt | Knowledge Workers |
| **US-0026** | [share-composition-with-team](./composition/US-0026-share-composition-with-team.md) | ctxt, dpkms cloud | Knowledge Workers |

#### Advanced Composition (US-0056 to US-0060)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0056** | [compose-decision-timeline](./composition/US-0056-compose-decision-timeline.md) | ctxt | Knowledge Workers |
| **US-0057** | [compose-stakeholder-analysis](./composition/US-0057-compose-stakeholder-analysis.md) | ctxt | Knowledge Workers |
| **US-0058** | [compose-impact-assessment](./composition/US-0058-compose-impact-assessment.md) | ctxt | Knowledge Workers |
| **US-0059** | [compose-recommendation-document](./composition/US-0059-compose-recommendation-document.md) | ctxt | Knowledge Workers |
| **US-0060** | [compose-with-custom-template](./composition/US-0060-compose-with-custom-template.md) | ctxt | Knowledge Workers |

### Configuration & Administration (US-0027 to US-0031)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0027** | [configure-ai-provider](./admin/US-0027-configure-ai-provider.md) | dpkms (self-hosted), dpkms cloud | Maintainers, Operations |
| **US-0028** | [register-custom-pipeline](./admin/US-0028-register-custom-pipeline.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0029** | [install-and-enable-plugin](./admin/US-0029-install-and-enable-plugin.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Operations |
| **US-0030** | [set-up-focus-profiles](./admin/US-0030-set-up-focus-profiles.md) | ctxt | Knowledge Workers, Maintainers |
| **US-0031** | [configure-encryption-and-secrets](./admin/US-0031-configure-encryption-and-secrets.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |

---

### Pipeline Management (US-0101 to US-0113)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0101** | [create-custom-pipeline](./pipelines/US-0101-create-custom-pipeline.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0102** | [list-and-filter-pipelines](./pipelines/US-0102-list-and-filter-pipelines.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0103** | [show-pipeline-details](./pipelines/US-0103-show-pipeline-details.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0104** | [delete-custom-pipeline](./pipelines/US-0104-delete-custom-pipeline.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0105** | [archive-pipeline](./pipelines/US-0105-archive-pipeline.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0106** | [enqueue-content-via-dpkms.md](./pipelines/US-0106-enqueue-content-via-dpkms.md) | dpkms (self-hosted), dpkms cloud | Knowledge Workers, Platform Integrators |
| **US-0107** | [discover-local-steps](./pipelines/US-0107-discover-local-steps.md) | dpkms (self-hosted), dpkms cloud | Maintainers, Plugin Developers |
| **US-0108** | [install-step-from-registry](./pipelines/US-0108-install-step-from-registry.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0109** | [fetch-registry-manifest](./pipelines/US-0109-fetch-registry-manifest.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers, Registry Operators |
| **US-0110** | [check-registry-updates](./pipelines/US-0110-check-registry-updates.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0111** | [configure-registry-autoupdate.md](./pipelines/US-0111-configure-registry-autoupdate.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers, Registry Operators |
| **US-0112** | [configure-sandbox-per-pipeline.md](./pipelines/US-0112-configure-sandbox-per-pipeline.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0113** | [ctxt-analyze-api-client.md](./pipelines/US-0113-ctxt-analyze-api-client.md) | dpkms (self-hosted), dpkms cloud | Knowledge Workers, Platform Integrators |
| **US-0114** | [configure-domain-scraper-rules](./pipelines/US-0114-configure-domain-scraper-rules.md) | ctxt, dpkms (self-hosted) | Platform Integrators, Maintainers |

---

### Monitoring & Operations (US-0032 to US-0036)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0032** | [monitor-job-queue-health](./operations/US-0032-monitor-job-queue-health.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0033** | [debug-failed-enrichment-job](./operations/US-0033-debug-failed-enrichment-job.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0034** | [export-and-backup-all-knowledge](./operations/US-0034-export-and-backup-all-knowledge.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0035** | [migrate-storage-backend](./operations/US-0035-migrate-storage-backend.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0036** | [scale-worker-pool-for-load](./operations/US-0036-scale-worker-pool-for-load.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |

### Agent Integration (US-0037 to US-0041)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0037** | [agent-discovers-query-schema](./agents/US-0037-agent-discovers-query-schema.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs |
| **US-0038** | [agent-constructs-rsql-query](./agents/US-0038-agent-constructs-rsql-query.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs |
| **US-0039** | [agent-ingests-content-and-waits](./agents/US-0039-agent-ingests-content-and-waits.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs |
| **US-0040** | [agent-composes-brief-programmatically](./agents/US-0040-agent-composes-brief-programmatically.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs |
| **US-0041** | [agent-uses-constrained-enrichment](./agents/US-0041-agent-uses-constrained-enrichment.md) | dpkms (self-hosted) | Agents/LLMs, Platform Integrators |

### Plugin & Extension (US-0042 to US-0045)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0042** | [implement-custom-enrichment-plugin](./plugins/US-0042-implement-custom-enrichment-plugin.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0043** | [implement-custom-ai-provider-plugin](./plugins/US-0043-implement-custom-ai-provider-plugin.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Maintainers |
| **US-0044** | [implement-registry-adapter-plugin](./plugins/US-0044-implement-registry-adapter-plugin.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators |
| **US-0045** | [implement-custom-ranking-algorithm](./plugins/US-0045-implement-custom-ranking-algorithm.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators |

### Federation (US-0318 to US-0323)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0318** | [configure-federation-targets](./federation/US-0318-configure-federation-targets.md) | dpkms (self-hosted) | Platform Integrators |
| **US-0319** | [async-push-local-merged](./federation/US-0319-async-push-local-merged.md) | dpkms (self-hosted) | Knowledge Workers |
| **US-0320** | [inline-push-during-ingest](./federation/US-0320-inline-push-during-ingest.md) | dpkms (self-hosted) | Platform Integrators |
| **US-0321** | [multi-instance-lifecycle](./federation/US-0321-multi-instance-lifecycle.md) | dpkms (self-hosted) | Operations |
| **US-0322** | [backup-and-federation-rebuild](./federation/US-0322-backup-and-federation-rebuild.md) | dpkms (self-hosted) | Operations |
| **US-0323** | [dag-federation-chain](./federation/US-0323-dag-federation-chain.md) | dpkms (self-hosted) | Platform Integrators |

### Knowledge Compiler (US-0400 to US-0409)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0400** | [fan-out-enrichment](./enrichment/US-0400-fan-out-enrichment.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers, Maintainers |
| **US-0401** | [persistent-composed-pages](./composition/US-0401-persistent-composed-pages.md) | dpkms (self-hosted), dpkms cloud, ctxt | Knowledge Workers, Agents/LLMs |
| **US-0402** | [knowledge-lint](./operations/US-0402-knowledge-lint.md) | dpkms (self-hosted), dpkms cloud, ctxt | Maintainers, Operations, Agents/LLMs |
| **US-0403** | [structured-metadata-extraction](./enrichment/US-0403-structured-metadata-extraction.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0404** | [fingerprint-dedup](./ingestion/US-0404-fingerprint-dedup.md) | dpkms (self-hosted), dpkms cloud, ctxt | Knowledge Workers, Operations |
| **US-0405** | [append-only-changelog](./knowledge-graph/US-0405-append-only-changelog.md) | dpkms (self-hosted), dpkms cloud, ctxt | Maintainers, Knowledge Workers, Operations |
| **US-0406** | [associative-object-links](./knowledge-graph/US-0406-associative-object-links.md) | dpkms (self-hosted), dpkms cloud | Knowledge Workers, Agents/LLMs |
| **US-0407** | [metadata-facet-search](./search/US-0407-metadata-facet-search.md) | dpkms (self-hosted), dpkms cloud, ctxt | Knowledge Workers, Agents/LLMs |
| **US-0408** | [index-first-retrieval](./search/US-0408-index-first-retrieval.md) | dpkms (self-hosted), dpkms cloud, ctxt | Knowledge Workers, Agents/LLMs |
| **US-0409** | [schema-co-evolution](./enrichment/US-0409-schema-co-evolution.md) | dpkms (self-hosted), dpkms cloud | Maintainers, Knowledge Workers |

---

## Story Template

Each story file contains:

```markdown
# US-XXXX: [Story Title]

**System Types:** [list]
**Personas:** [links to persona files]

## User Goal
What the user is trying to accomplish

## Context
Why this matters, when it's used

## Acceptance Criteria
Specific, testable outcomes

## Implementation Notes
Technical details, API endpoints, config

## E2E Test Checklist
Steps to verify the story works end-to-end

## Related Stories
Links to related stories
```

---

## Story Numbering Convention

- **US-0001 to US-0008** — Ingestion & Capture
- **US-0009 to US-0015** — Core Enrichment
- **US-0016 to US-0021** — Core Search
- **US-0022 to US-0026** — Core Composition
- **US-0027 to US-0031** — Admin & Configuration
- **US-0032 to US-0036** — Operations
- **US-0037 to US-0041** — Agent Integration
- **US-0042 to US-0045** — Plugins & Extensions
- **US-0046 to US-0060** — Advanced Features (enrichment, search, composition)
- **US-0061+** — Multimodal Search (grouped under Search & Retrieval)
- **US-0101 to US-0113** — Pipeline Management
- **US-0200 to US-0210** — Platform Capture (cookie bridge, social media, academic, OSINT, temporal watch)
- **US-0300 to US-0317** — Importer Interface and Source Importers
- **US-0318 to US-0323** — Federation
- **US-0400 to US-0409** — Knowledge Compiler (fan-out, persistent pages, lint, dedup, links, facets, index)

---

## Cross-Story Dependencies

Key dependency chains:

```
Ingestion → Enrichment → Search → Composition
  US-0001    US-0009-0015  US-0016-0021  US-0022-0026

Admin → Enrichment → Agent Integration
  US-0027    US-0014      US-0041

Operations ← Enrichment
  US-0032        US-0015

Pipeline Management (US-0101-0113):
  US-0101 (create) → US-0106 (enqueue) → US-0113 (ctxt client)
  US-0107 (discover steps) → US-0108 (install) → US-0109 (fetch manifest)
  US-0109 → US-0110 (check updates) → US-0111 (auto-update)
  US-0101 → US-0112 (sandbox config)
  US-0101 → US-0102 (list) → US-0103 (show) → US-0104/0105 (delete/archive)

Importer Stories (US-0300-0317):
  US-0300 (interface contract) → US-0301 to US-0317 (source-specific importers)
  US-0008 (batch import) → US-0300 (foundation for fan-out and batch tracking)
  US-0106 (enqueue) → US-0300 (unified enqueue endpoint used by all importers)

Knowledge Compiler (US-0400-0409):
  US-0403 (structured metadata) → US-0400 (fan-out) → US-0401 (persistent pages)
  US-0403 → US-0407 (facet search)
  US-0404 (fingerprint dedup) — standalone at ingest
  US-0400 → US-0405 (changelog) ← US-0402 (lint)
  US-0400 → US-0406 (associative links) → US-0402 (lint)
  US-0401 → US-0408 (index-first retrieval)
  US-0409 (schema co-evolution) → US-0403 (metadata extraction)
```

---

## Personas × System Types Coverage

| Persona | dpkms (self-hosted) | dpkms cloud | ctxt |
|---------|-------------------|-------------|------|
| **Maintainers** | US-0006, US-0008, US-0015, US-0027-0035, US-0400, US-0402, US-0405, US-0409 | US-0015, US-0027, US-0032, US-0034, US-0400, US-0402, US-0405, US-0409 | US-0030, US-0402, US-0405 |
| **Agents/LLMs** | US-0003, US-0005, US-0009-0014, US-0018-0019, US-0024, US-0037-0041, US-0046-0050, US-0400, US-0403, US-0406-0408 | US-0009-0012, US-0018-0019, US-0024, US-0037-0040, US-0400, US-0403, US-0406-0408 | US-0003, US-0005, US-0016, US-0401, US-0407-0408 |
| **Knowledge Workers** | US-0003-0007, US-0009-0012, US-0019-0021, US-0046-0048, US-0400, US-0403-0409 | US-0008, US-0400-0409 | US-0001-0007, US-0016, US-0020-0021, US-0022-0026, US-0030, US-0053-0055, US-0056-0060, US-0401, US-0404-0405, US-0407-0408 |
| **Platform Integrators** | US-0013-0014, US-0028-0029, US-0041-0045, US-0049-0050, US-0101-0113 | US-0101-0113 | — |
| **Operations** | US-0007, US-0008, US-0015, US-0029, US-0032-0036, US-0402, US-0404-0405 | US-0015, US-0027, US-0032, US-0034, US-0402, US-0404-0405 | US-0402, US-0404-0405 |

---

## Usage

### For Documentation
- Link to relevant stories when explaining features
- Use acceptance criteria as feature specifications
- Reference E2E test checklists in implementation guides
- Include code examples from story implementation notes

### For E2E Testing
- Create test suite per story (US-XXXX.test.go or test_us_xxxx.py)
- Use acceptance criteria as test assertions
- Tag tests with personas and system types
- Run story tests on each deployment type

### For Development Planning
- Stories represent user-facing features
- Group related stories into sprints
- Track story completion rate per persona
- Identify missing stories from user feedback
- Link GitHub issues to story IDs (e.g., #US-0001)

---

## Story Status

**Fully Documented (41):**
- US-0001, US-0002, US-0009, US-0010, US-0011, US-0012, US-0013, US-0014, US-0015, US-0016,
  US-0022, US-0023, US-0024, US-0025, US-0026, US-0027, US-0028, US-0029, US-0030, US-0031,
  US-0032, US-0033, US-0034, US-0035, US-0036, US-0037, US-0038, US-0039, US-0040, US-0041,
  US-0046, US-0047, US-0048, US-0049, US-0050, US-0056, US-0057, US-0058, US-0059, US-0060,
  US-0063

**Expanded E2E + Narrative - Search Category (11):**
- US-0017, US-0018, US-0019, US-0020, US-0021 (core search)
- US-0051, US-0052, US-0053, US-0054, US-0055 (advanced search)

**Expanded E2E + Narrative - Ingestion Category (6):**
- US-0003, US-0004, US-0005, US-0006, US-0007, US-0008 (ingestion core, full implementation notes + E2E)

**Importer Stories - Fully Documented (18):**
- US-0300 to US-0317

**Fully Documented - Plugins Category (4):**
- US-0042, US-0043, US-0044, US-0045

**Fully Documented - Multimodal (1):**
- US-0061

**Pipeline Management - Fully Documented (13):**
- US-0101 to US-0113

**Platform Capture - Fully Documented (11):**
- US-0200, US-0201, US-0202, US-0203, US-0204, US-0205, US-0206, US-0207, US-0208, US-0209, US-0210

**Knowledge Compiler - Fully Documented (10):**
- US-0400, US-0401, US-0402, US-0403, US-0404, US-0405, US-0406, US-0407, US-0408, US-0409

**Total: 120 stories across 14 categories**
