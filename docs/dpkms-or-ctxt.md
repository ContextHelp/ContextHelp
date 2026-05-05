# Clear Distinction: dPKMS vs `ctxt` (Jobs + Pipelines)

## dPKMS: The Execution Substrate (What it *offers*)
dPKMS owns the **mechanics of running work safely**.

It provides:
- a transactional **Jobs Queue** (durable outbox)
- an execution model for **Pipelines**
- persistence, retries, and recovery guarantees
- state tracking, logs, and auditability
- capability-scoped access to storage, graph, query, crypto, registries

It also provides:
- **multilingual-capable storage and indexing** (Unicode-safe, RTL-safe)
- **language-aware schemas** (localized labels, aliases, metadata per language)
- **multilingual query compatibility** (mixed-language inputs remain searchable)

dPKMS answers:
- *Can this work run safely?*
- *Can it resume after a crash?*
- *Can we replay it deterministically?*
- *Can we prove what happened and why?*
- *Can multiple workers run it without corruption?*
- *Can multilingual data remain portable, queryable, and stable over time?*

It does **not** decide:
- *what the work should be*
- *what language to translate into*
- *what writing style or interpretation is “best”*


## `ctxt`: The Brain (What it *decides* and *composes*)
`ctxt` owns the **intent and meaning of work**.

It defines:
- what pipelines exist (“summarize”, “extract entities”, “detect decisions”)
- what steps run per input type and per profile (Founder vs Research vs Project X)
- what AI providers/models to use for a step
- how to interpret results (confidence, merging rules, dedupe policy)
- what actions to produce (tasks, drafts, briefs, alerts)
- when to refresh and what to resurface “just in time”

It also defines multilingual *behavior*:
- whether translation happens at all
- preferred languages per user/profile
- how to summarize across languages
- how to display and compose outputs in Arabic/English/French
- how mixed-language content should be interpreted

`ctxt` answers:
- *What should we do with this input?*
- *What matters right now?*
- *How should this be enriched?*
- *What output should be generated?*
- *What should be surfaced to the user today?*
- *What language should this become useful in?*


# Jobs: The Boundary Line

## dPKMS Jobs (Infrastructure)
- Enqueue work
- Persist job state
- Run workers
- Retry safely
- Track progress
- Store outputs
- Guarantee crash-safe recovery

## `ctxt` Jobs (Behavior)
- Decide job types (“ingest:url”, “enrich:audio”, “refresh:stale”)
- Choose pipeline recipes
- Select plugins and models
- Score importance and relevance
- Schedule refresh/surfacing jobs
- Turn results into user-facing outputs
- Decide when translation/summarization should be language-specific


# Pipelines: The Boundary Line

## dPKMS Pipelines (Runtime)
dPKMS provides a generic pipeline runtime with:
- step execution
- step isolation
- typed inputs/outputs
- caching hooks
- idempotency and replay support
- permission and capability enforcement
- structured logs + audit trails

dPKMS does not care whether a pipeline step is:
- AI summarization
- OCR
- entity extraction
- language detection
- graph linking
- anything else

It only guarantees the pipeline can run safely and consistently.

dPKMS also guarantees:
- multilingual storage remains correct (Unicode/RTL-safe)
- localized fields remain queryable and portable
- mixed-language objects won’t break indexing or retrieval

## `ctxt` Pipelines (Meaning + Recipes)
`ctxt` defines the real pipeline content:
- how to process text vs image vs audio
- what “good summaries” look like
- which entities matter and how to resolve them
- how to classify and tag knowledge
- what a “decision” or “task” means
- how to build outputs (briefs, plans, posts)

It also defines multilingual intelligence:
- translation and rewriting rules (if enabled)
- cross-language entity resolution behavior
- language-specific templates and composition styles
- multilingual surfacing and ranking policies per profile

It turns:
- inputs → knowledge objects
- knowledge → outputs
- history → resurfacing


# The One-Line Summary

- **dPKMS runs jobs and pipelines correctly (including multilingual-safe storage + indexing).**
- **`ctxt` decides which jobs and pipelines are worth running (and how language is used).**


# Where ambient capture lives (and why it can't be in dPKMS)

A common follow-up: *"Where does the ambient capture daemon (`ctxd`) fit in this boundary?"*

**`ctxd` is a `ctxt`-side concern, not a dPKMS-side concern.**

In the canonical deployment topology, **dPKMS is often deployed remote** — managed instance, household NAS, federated peer per ADR-064. A remote dPKMS:

- cannot read the user's clipboard,
- cannot see what app currently has foreground focus,
- cannot watch `~/Inbox` for new files,
- cannot subscribe to browser SQLite history,
- cannot capture system audio during a video call.

All of these signals are local-machine-only. Any subsystem that depends on them must run on the user's machine, **not** on dPKMS. Therefore:

- The **ambient capture substrate** (sources, runner, fingerprint dedup, session cutter) lives in `ctxd` — `ctxt`-side. See [ADR-066](decisions/ADR-066-ambient-capture-substrate.md).
- The **session cutter** lives in `ctxd` because the foreground-window signal is local. dPKMS receives `session_id` as opaque metadata. See [ADR-067](decisions/ADR-067-session-workunit.md).
- The **local MCP read-surface** lives in `ctxd` because it surfaces information dPKMS cannot see when remote (live cutter state, buffered events, fingerprint dedup state). The dpkms-side MCP is the authoritative surface; the ctxd-side MCP is the local complement. Agents on the user's machine attach to both. See [ADR-068](decisions/ADR-068-mcp-read-surface.md).
- The **meeting capture source** lives in `ctxd` (with mobile companion apps as Phase 6+) because system-audio + window-framebuffer capture requires OS APIs that only run on the device. See [ADR-069](decisions/ADR-069-meeting-capture-source.md).

`ctxd` enqueues against whichever dPKMS is configured (local or remote) via the existing `/api/v1/analyze` HTTP path (per [ADR-056](decisions/ADR-056-unified-enqueue-api.md)). dPKMS remains a pure pipeline+storage worker; it gains zero ambient/capture responsibilities.

**Boundary, restated for the ambient case:**

| Concern | Where it lives | Why |
|---|---|---|
| Reading local-machine signals | `ctxd` (`ctxt`-side) | dPKMS may be remote |
| Cutting sessions | `ctxd` (`ctxt`-side) | Needs foreground-window signal |
| Privacy/policy gates on capture | `ctxd` (`ctxt`-side, via kit/runtime/policy) | Privacy enforcement must precede network egress |
| Pipeline execution | dPKMS | Pipeline runtime is dPKMS's job |
| KnowledgeObject persistence | dPKMS | Storage is dPKMS's job |
| Federation between instances | dPKMS-to-dPKMS via ADR-064 | Federation is substrate-level |
| Authoritative MCP read-surface | dPKMS (`/api/v1/mcp/`) | Authoritative graph lives in dPKMS |
| Local MCP read-surface | `ctxd` (`:8744/mcp`) | Live local state cannot be queried from dPKMS when remote |
