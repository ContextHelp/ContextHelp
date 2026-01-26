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
