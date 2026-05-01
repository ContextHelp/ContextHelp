# US-0402: Knowledge Lint and Health Check

**System Types:** dpkms (self-hosted), dpkms cloud, ctxt
**Personas:** [Maintainers](../../personas/maintainers.md),
[Operations](../../personas/operations.md),
[Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a maintainer, I want automated lint/health checks that detect
contradictions, stale claims, orphan objects, duplicate content,
and missing metadata — so the knowledge graph stays clean without
manual curation burden.

---

## Context

Human-maintained wikis die from maintenance burden. LLM-powered
lint solves this: periodic sweeps detect inconsistencies that
compound over time. Karpathy identifies this as the key
differentiator — LLMs don't get bored, don't miss cross-refs.
OB1's fingerprint dedup is a subset of this (content-level dedup
at ingest). Maps to ADR-042 Gap #2 (multi-pass refinement).

---

## Acceptance Criteria

- [ ] `ctxt lint` runs a health check across the knowledge
  graph and reports issues
- [ ] Detects contradictions (conflicting claims across objects)
- [ ] Detects stale objects (age + no references + no access)
- [ ] Detects orphan objects (no tags, no links, no entity
  mentions)
- [ ] Detects near-duplicate content (embedding distance <
  threshold)
- [ ] Detects missing metadata (no tags, no summary, no
  entities extracted)
- [ ] Reports severity per issue (info, warning, error)
- [ ] Supports `--fix` to auto-resolve safe issues (dedup,
  missing metadata)
- [ ] Supports `--profile` to scope lint to a subset
- [ ] Runs as scheduled background job (configurable interval)
- [ ] Results stored as lint report objects for trend tracking

---

## Implementation Notes

### Lint Checks

```
contradictions:
  - cluster objects by entity/topic
  - detect conflicting claims via LLM comparison
  - severity: warning (soft conflict) | error (hard conflict)

staleness:
  - objects not accessed in N days + no inbound links
  - configurable threshold per profile
  - severity: info

orphans:
  - no tags, no entity mentions, no cross-refs, no page links
  - severity: warning

duplicates:
  - embedding cosine similarity > 0.95
  - content hash collision
  - severity: warning (suggest merge)

missing_metadata:
  - no summary, no tags, no entities extracted
  - severity: info (auto-fixable)
```

### CLI Interface

```bash
# Run full lint
ctxt lint

# Scoped lint
ctxt lint --profile research
ctxt lint --check contradictions,duplicates

# Auto-fix safe issues
ctxt lint --fix

# Schedule recurring lint
ctxt lint --schedule daily

# View lint history
ctxt lint --history
```

### REST API

```
POST /lint
{
  "checks": ["contradictions", "staleness", "orphans",
             "duplicates", "missing_metadata"],
  "profile": "research",
  "auto_fix": false
}

-> 202 Accepted
{
  "lint_job_id": "j-lint-001",
  "status": "pending"
}
```

---

## E2E Test Checklist

- [ ] Two objects with contradicting claims -> lint reports
  contradiction with both object IDs
- [ ] Object with no tags, no links, 90+ days old -> lint
  reports as stale
- [ ] Object with no metadata -> lint reports missing; `--fix`
  triggers enrichment
- [ ] Two near-identical objects -> lint reports as duplicate
  with similarity score
- [ ] `--profile research` only checks research-tagged objects
- [ ] Lint report stored as object -> queryable via
  `ctxt find "lint report"`
- [ ] Scheduled lint runs at configured interval
- [ ] `--fix` resolves missing metadata but does NOT auto-merge
  duplicates (too risky)

---

## Related Stories

- US-0400: Fan-out enrichment (produces the metadata lint checks)
- US-0401: Persistent composed pages (pages can become stale)
- US-0404: Fingerprint dedup at ingest (prevents duplicates
  before they enter)
- US-0011: Assign tags from vocabulary (related enrichment)

---

## E2E Tests

- `test/integration/us0402_lint_test.go::TestUS0402_LintOrphan`
- `test/integration/us0402_lint_test.go::TestUS0402_LintMissingMetadata`
- `test/integration/us0402_lint_test.go::TestUS0402_LintCheckFilter`
- `test/integration/us0402_lint_test.go::TestUS0402_LintAuditLog`
