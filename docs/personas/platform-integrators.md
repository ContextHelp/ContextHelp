# Persona: Platform Integrators

**Primary Role:** Third-party developers extending dPKMS + `ctxt` with custom pipelines, steps and integrations

---

## Goals

- Add custom processing: pipelines built from existing steps, plus external steps of their own
- Distribute steps and knowledge (taxonomies, entities) through registries
- Pick the AI and media backends a deployment uses (LLM, embeddings, OCR, transcription, vision)
- Push knowledge between instances (federation)
- Build capture or composition front ends on top of the dpkms REST API

---

## What Exists Today

| Extension point | How | Reference |
|-----------------|-----|-----------|
| Custom pipelines | `dpkms pipeline create <file>`, then `list`, `show`, `archive`, `unarchive`, `remove` | [manual guide](../manual/personas/platform-integrators.md), [pipelines](../ctxt/pipelines.md) |
| External steps | A directory per step under `steps.path` (default `~/.config/contexthelp/steps`), described by a `SKILL.md`. The step executable reads `{"config": ..., "object": ...}` as JSON on stdin and writes the resulting object as JSON on stdout. | `internal/steps/` |
| Step registries | `dpkms pipeline step registry add\|list\|update\|autoupdate`, `dpkms pipeline step install <name> --registry <url>` | `dpkms pipeline step --help` |
| Knowledge registries | `registries:` config entries (`name`, `url`, `auth`, `sync_mode`, `trust_level`, `entitlement_url`); `ctxt registry add\|sync\|info\|delete` | `ctxt registry --help` |
| Provider selection | `providers.<kind>.backend` (plus `model`, `endpoint`, `language`) for `llm`, `vision`, `ocr`, `transcription`, `document`, `video`, `diarization`; `providers.embedding` | [design: provider configuration](../design.md#provider-configuration) |
| Federation | `federations:` entries pushed by dpkms | [Federation](#federation-configuration) below |

Provider backends are a fixed set compiled into dpkms (for example `llm`: `openai`,
`anthropic`, `ollama`, `stub`, or auto). There is no API for registering a new
provider, ranking method, RSQL operator or storage backend from outside the
codebase; those change only by changing ctxt itself. There is no plugin system
([ADR-078](../decisions/ADR-078-no-plugin-system.md)): no Go plugin API, plugin
registry or `plugins:` config key.

### Federation Configuration

dpkms pushes objects, edges and entities to the targets listed under `federations`
in `dpkms.yaml`:

```yaml
federations:
  - name: local-merged          # local SQLite target; pushed every 5m
    url: /path/to/merged.sqlite
    interval: 5m
    sync_mode: async

  - name: team                  # remote dpkms over HTTP; pushed every 1h
    url: https://team.internal:8080
    interval: 1h
    sync_mode: async
    token: <bearer token the remote expects>
```

- `sync_mode: async` runs one background worker per target on its `interval`
  (required, > 0). Each successful batch advances a watermark at the target.
- `sync_mode: inline` is accepted by the validator, but no inline push is
  implemented: inline targets are skipped.
- `token` is sent as a Bearer token to HTTP targets. On the receiving instance,
  `federation.token` sets the token it expects (empty accepts any).
- Startup rejects duplicate target names and a local target that points back at
  the instance's own database. Remote topologies (A to B to A over HTTP) are not
  checked.

Refs: US-0318 (configure targets), US-0320 (inline push, not implemented), US-0323 (DAG chain)

---

## Key Pain Points

- **Step Versioning:** Which core version is my step compatible with? Changes to the step stdin/stdout contract
- **Documentation Lag:** Step and pipeline contracts change faster than the guides
- **Testing Isolation:** Hard to test an external step without a running dpkms and its storage
- **Deployment Friction:** Getting users to trust and install a step registry

---

## User Stories

Platform integrators interact with the system through these key stories:

### Platform Capture Integration
- [US-0202](../stories/capture/US-0202-github-capture.md) — GitHub Capture (API integration example)

### Extension Development
- [US-0300](../stories/ingestion/US-0300-importer-extension-interface.md) — Importer Extension Interface (build custom importers)

### Constraint & AI Integration
- [US-0014](../stories/enrichment/US-0014-constrain-extraction-with-lmql.md) — Constrain Extraction with LMQL (understand token-level constraints)
- [US-0050](../stories/enrichment/US-0050-extract-code-metrics-and-complexity.md) — Extract Code Metrics and Complexity (domain-specific extraction)

### Custom Enrichment
- [US-0013](../stories/enrichment/US-0013-detect-and-extract-code-snippets.md) — Detect and Extract Code Snippets (custom enrichment example)

### Pipeline & Configuration
- [US-0028](../stories/admin/US-0028-register-custom-pipeline.md) — Register Custom Pipeline (custom processing workflows)
- [US-0029](../stories/admin/US-0029-install-steps-from-registry.md) — Install Pipeline Steps From A Registry
- [US-0114](../stories/pipelines/US-0114-configure-domain-scraper-rules.md) — Configure Domain Scraper Rules (config-driven extraction quality)
- [US-0041](../stories/agents/US-0041-agent-uses-constrained-enrichment.md) — Agent Uses Constrained Enrichment (constraint patterns)

### Federation Configuration
- US-0318 — Configure federation targets (local SQLite + remote HTTP in `dpkms.yaml`)
- US-0320 — Inline push (synchronous pipeline step; not implemented)
- US-0323 — DAG chain (multi-hop federation topology)

Stories describe intended behavior; several (LMQL, scraper rules) are not
implemented.

---

## Success Metrics

- **Time to develop:** Hours to build and ship a basic external step or pipeline
- **Step reliability:** Step failure and retry rates in `dpkms job list`
- **Registry adoption:** Number of step and knowledge registries in use

---

## Collaboration with Other Personas

- **Maintainers:** Integrators submit step and pipeline PRs, request contract additions, report compatibility issues
- **Agents/LLMs:** Agents use the pipelines and steps integrators add
- **Knowledge Workers:** Workers benefit from domain-specific pipelines and steps
- **Operations:** Operations need clear deployment guidance for step registries and updates
