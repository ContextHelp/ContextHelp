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
| **US-0003** | [image-ocr-and-analysis](./ingestion/US-0003-image-ocr-and-analysis.md) | ctxt | Knowledge Workers |
| **US-0004** | [audio-transcription-and-indexing](./ingestion/US-0004-audio-transcription-and-indexing.md) | ctxt | Knowledge Workers |
| **US-0005** | [video-processing-with-scenes](./ingestion/US-0005-video-processing-with-scenes.md) | ctxt | Knowledge Workers |
| **US-0006** | [document-parsing-and-decomposition](./ingestion/US-0006-document-parsing-and-decomposition.md) | ctxt | Knowledge Workers |
| **US-0007** | [feed-ingestion-and-sync](./ingestion/US-0007-feed-ingestion-and-sync.md) | ctxt | Knowledge Workers |
| **US-0008** | [batch-import-from-file](./ingestion/US-0008-batch-import-from-file.md) | ctxt, dpkms (self-hosted), dpkms cloud | Maintainers, Operations |

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

#### Advanced Enrichment (US-0046 to US-0050)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0046** | [extract-relationships-between-entities](./enrichment/US-0046-extract-relationships-between-entities.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs |
| **US-0047** | [extract-temporal-information](./enrichment/US-0047-extract-temporal-information.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0048** | [detect-sentiment-and-tone](./enrichment/US-0048-detect-sentiment-and-tone.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs, Knowledge Workers |
| **US-0049** | [classify-content-with-taxonomy](./enrichment/US-0049-classify-content-with-taxonomy.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs |
| **US-0050** | [extract-code-metrics-and-complexity](./enrichment/US-0050-extract-code-metrics-and-complexity.md) | dpkms (self-hosted) | Agents/LLMs, Platform Integrators |

### Search & Retrieval (US-0016 to US-0055)

#### Core Search (US-0016 to US-0021)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0016** | [natural-language-search](./search/US-0016-natural-language-search.md) | ctxt | Knowledge Workers, Agents/LLMs |
| **US-0017** | [structured-rsql-query](./search/US-0017-structured-rsql-query.md) | ctxt, dpkms (self-hosted), dpkms cloud | Agents/LLMs, Maintainers |
| **US-0018** | [multi-strategy-search-execution](./search/US-0018-multi-strategy-search-execution.md) | dpkms (self-hosted), dpkms cloud | Agents/LLMs |
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

#### Multimodal Search (US-0061+)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0061** | [visual-similarity-search](./search/US-0061-visual-similarity-search.md) | ctxt, dpkms | Knowledge Workers, Agents/LLMs |

### Configuration & Administration (US-0027 to US-0031)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0027** | [configure-ai-provider](./admin/US-0027-configure-ai-provider.md) | dpkms (self-hosted), dpkms cloud | Maintainers, Operations |
| **US-0028** | [register-custom-pipeline](./admin/US-0028-register-custom-pipeline.md) | dpkms (self-hosted) | Platform Integrators, Maintainers |
| **US-0029** | [install-and-enable-plugin](./admin/US-0029-install-and-enable-plugin.md) | dpkms (self-hosted), dpkms cloud | Platform Integrators, Operations |
| **US-0030** | [set-up-focus-profiles](./admin/US-0030-set-up-focus-profiles.md) | dpkms cloud | ctxt | Knowledge Workers, Maintainers |
| **US-0031** | [configure-encryption-and-secrets](./admin/US-0031-configure-encryption-and-secrets.md) | dpkms (self-hosted), dpkms cloud | Security Engineers, Operations |
| **US-0032** | [monitor-job-queue-health](./operations/US-0032-monitor-job-queue-health.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0033** | [debug-failed-enrichment-job](./operations/US-0033-debug-failed-enrichment-job.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers | Knowledge Workers |
| **US-0034** | [export-and-backup-all-knowledge](./operations/US-0034-export-and-backup-all-knowledge.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0035** | [migrate-storage-backend](./operations/US-0035-migrate-storage-backend.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0036** | [scale-worker-pool-for-load](./operations/US-0036-scale-worker-pool-for-load.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0037** | [monitor-job-job-health](./operations/US-0037-monitor-job-job-health.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |

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

---

### Monitoring & Operations (US-0032 to US-0036)

| ID | Story | System Types | Personas |
|----|-------|-------------|----------|
| **US-0032** | [monitor-job-queue-health](./operations/US-0032-monitor-job-queue-health.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0033** | [debug-failed-enrichment-job](./operations/US-0033-debug-failed-enrichment-job.md) | dpkms (self-hosted) | Operations, Maintainers |
| **US-0034** | [export-and-backup-all-knowledge](./operations/US-0034-export-and-backup-all-knowledge.md) | dpkms (self-hosted), dpkms cloud | Operations, Maintainers |
| **US-0035** | [migrate-storage-backend](./operations/US-0035-migrate-storage-backend.md) | dpkms (self-hosted) | Operations, Maintainers |
| **US-0036** | [scale-worker-pool-for-load](./operations/US-0036-scale-worker-pool-for-load.md) | dpkms (self-hosted) | Operations |

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
| **US-0042** | [implement-custom-enrichment-plugin](./plugins/US-0042-implement-custom-enrichment-plugin.md) | dpkms (self-hosted) | Platform Integrators, Maintainers |
| **US-0043** | [implement-custom-ai-provider-plugin](./plugins/US-0043-implement-custom-ai-provider-plugin.md) | dpkms (self-hosted) | Platform Integrators, Maintainers |
| **US-0044** | [implement-registry-adapter-plugin](./plugins/US-0044-implement-registry-adapter-plugin.md) | dpkms (self-hosted) | Platform Integrators |
| **US-0045** | [implement-custom-ranking-algorithm](./plugins/US-0045-implement-custom-ranking-algorithm.md) | dpkms (self-hosted) | Platform Integrators |

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
- **US-0046 to US-0060** — Advanced Features
- **US-0061+** — Multimodal Search

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
```

---

## Personas × System Types Coverage

| Persona | dpkms (self-hosted) | dpkms cloud | ctxt |
|---------|-------------------|-------------|------|
| **Maintainers** | US-0008, US-0015, US-0027-0035 | US-0015, US-0027, US-0032, US-0034 | US-0030 |
| **Agents/LLMs** | US-0009-0014, US-0018-0019, US-0024, US-0037-0041, US-0046-0050 | US-0009-0012, US-0018-0019, US-0024, US-0037-0040, US-0046-0051 | US-0016 |
| **Knowledge Workers** | US-0001-0007, US-0009-0012, US-0019-0021 | US-0008 | US-0001-0007, US-0016, US-0020-0021, US-0022-0026, US-0030, US-0047-0048, US-0053-0055, US-0056-0060 |
| **Platform Integrators** | US-0013-0014, US-0028-0029, US-0041-0045, US-0050 | — | — |
| **Operations** | US-0008, US-0015, US-0029, US-0032-0036 | US-0015, US-0027, US-0032, US-0034 | — |

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

**Fully Documented (11):**
- US-0001, US-0002, US-0009, US-0014, US-0016, US-0022, US-0027, US-0037, US-0038

**Scaffolded - Ready for Implementation (50):**
- US-0003 to US-0008, US-0010 to US-0013, US-0015, US-0017 to US-0021, US-0023 to US-0026, US-0028 to US-0036, US-0039 to US-0045, US-0046 to US-0060

**Fully Documented - Multimodal (1):**
- US-0061

**Total: 61 stories across 9 categories**
