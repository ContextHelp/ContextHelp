# GitHub Ingestion Consolidation — Design

Created: 2026-07-26
Status: active

## Goal

One GitHub ingestion path: a REST-backed extraction core feeding typed
pipeline steps, shared by URL capture and the bulk importer. Deep repo
analysis (clone, code maps, cross-referencing) stays out of scope; it
builds on this foundation later.

## Architecture

```
api.github.com (GITHUB_TOKEN, anon fallback)
        │
internal/github            extraction core
  FetchRepo / FetchIssue / FetchPR / FetchProfile → typed snapshots
        │
pipeline steps
  github_repo_fetcher      snapshot → draft metadata
  github_repo_mapper       snapshot → KnowledgeObject
        │
url.github.*               parameterized defs, one per URL shape
        │
post-ingest
  repo_health_enricher     health metadata on type:repo
  dependency_enricher      fed by manifest fetch (dep_files)
```

## Components

### Extraction core (`internal/github`)

`RepoSnapshot`, `IssueSnapshot`, and sibling structs are populated by
REST fetchers. Token from `GITHUB_TOKEN`; unauthenticated calls work
within the reduced rate budget. Pagination and rate-limit headers are
handled in one place. Manifests (`go.mod`, `package.json`,
`requirements.txt`, `Gemfile`) are fetched via the contents API and
attached to the snapshot.

### Pipeline steps

`github_repo_fetcher` resolves the URL kind, calls the core, and stores
the snapshot in draft metadata. `github_repo_mapper` maps it onto a
`KnowledgeObject`: `Type` says what the thing is (`repo`, `issue`,
`pull_request`, `release`, `person`, `org`), `Subtype` says where it
came from (`github`). `metadata.forge` / `metadata.forge_id` carry
forge identity. Stars, language, topics, license, and releases land as
structured metadata, not page text.

### Pipelines

`url.github.repo|issue|pr|release|profile` share one parameterized
registration: fetcher → mapper → the same enrichment tail as
`text.long` (entity extraction, graph extraction, structured metadata,
classification, embedding) where the object type warrants it.
`url.github.starred` is selectable both explicitly and via content
test, and never re-fetches a payload that arrives pre-rendered.

### Importer

`ctxt import github` uses the same extraction core and routes each item
through the typed pipelines, producing `type:repo` objects — not
flattened markdown.

### Type taxonomy (`internal/types`)

Named constants for every `Type`/`Subtype` value, with the registry
documented in `docs/plugins/type-taxonomy.md`. Other forges (`gitlab`,
`bitbucket`) reuse the same `Type` set with their own `Subtype`.

## Testing

All GitHub paths run off recorded cassettes; no live network in tests.
End-to-end coverage: each URL shape to its typed object, importer run,
dependency fan-out, health enrichment.

## Out of scope

Repo cloning and code analysis, DeepWiki/GitSummarize extraction,
package-registry health, marketing surface discovery, job dispatch
budgeting, resync/living summaries, and the Repo Context Layer. See
`2026-04-05-grip-v2-deep-repo-ingestion.md` (deferred) for that
direction.
