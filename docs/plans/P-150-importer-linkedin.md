# LinkedIn Importer Plan

> **Status:** Proposed
> **Date:** 2026-02-18
> **Depends on:** Phase 10 (complete), Phase 8 (pending polish), Phase 9 (pending distribution)

## Goal
Ship an out-of-box LinkedIn importer that supports first-time backfill and repeat sync into dPKMS/ctxt with stable deduplication and operational visibility.

## Fit With Current Roadmap
- **Phase 10 (complete):** Reuse persistent pipelines, step registry/discovery, sandbox execution, and unified enqueue path.
- **Phase 8 (pending):** This importer will consume upcoming UX polish items (progress bars, clearer error messages, better prompts during auth/setup).
- **Phase 9 (pending):** This importer will be included in CI/release packaging, installer docs, and distribution artifacts.

## Data Surface
- **Primary surface:** LinkedIn data export package (MVP)
- **Auth model:** Export upload (MVP); OAuth only for approved partner scopes
- **Incremental sync:** Export timestamp windows + activity IDs

## MVP Scope
- Import object types: posts, articles, profile sections, connections metadata where present
- Support one-time backfill and resumable incremental sync.
- Use stable dedup key: source + external_id + canonical_url_hash.
- Preserve source attribution and import timestamps in metadata.
- Out of scope (MVP): Unauthorized scraping or unsupported API access patterns

## Implementation Plan
1. **Contract + fixtures (0.5-1 day)**
- Define importer config schema and validation rules.
- Build golden fixtures and parser tests for source payloads.
- Define normalized metadata mapping for knowledge objects.

2. **Fetcher + parser (1-2 days)**
- Implement source client (API or file parser).
- Add pagination/rate-limit/backoff handling.
- Emit normalized draft objects for enqueue.

3. **Sync + dedup state (1 day)**
- Persist checkpoint cursor/token (or file fingerprint) per profile.
- Add idempotent upsert behavior and duplicate suppression.
- Add retry semantics for transient failures.

4. **CLI/API integration (0.5-1 day)**
- Add import command wiring and profile-scoped config.
- Expose dry-run mode and max-items limit.
- Surface import run IDs for job tracking.

5. **Hardening + docs (0.5-1 day)**
- Add structured metrics/log fields (source, run_id, imported_count, skipped_count, error_count).
- Add troubleshooting and auth-expiry handling.
- Add release checklist hooks for Phase 9 packaging.

## API/CLI Touchpoints
- CLI: ctxt import linkedin --profile <name> [--since ...] [--max-items N] [--dry-run]
- API: POST /api/v1/importers/linkedin/run and GET /api/v1/importers/runs/{id}
- Storage: importer checkpoint table keyed by (profile, source, account)

## Key Risks
- Public API access is heavily restricted for many data types
- Export bundles differ by account region and product tier

## Verification
- Unit tests for parsing/normalization edge cases.
- Integration tests for backfill and incremental sync.
- Idempotency test: same batch imported twice produces no duplicate objects.
- Failure recovery test: restart from persisted checkpoint after simulated interruption.

## Exit Criteria
- Backfill and incremental both succeed on representative fixtures.
- Duplicate rate < 1% on repeated imports.
- Error classification is actionable (auth, rate limit, malformed payload, internal).
- Docs include setup, limitations, and expected throughput.
