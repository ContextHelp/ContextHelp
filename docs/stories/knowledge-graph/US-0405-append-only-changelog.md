# US-0405: Append-Only Knowledge Changelog

**System Types:** dpkms (self-hosted), dpkms cloud, ctxt
**Personas:** [Maintainers](../../personas/maintainers.md),
[Knowledge Workers](../../personas/knowledge-workers.md),
[Operations](../../personas/operations.md)

---

## User Goal

As a maintainer, I want an append-only chronological log of all
knowledge graph mutations — captures, enrichments, page updates,
deletions, merges — so I can audit the system's evolution,
debug issues, and understand how knowledge accumulated over time.

---

## Context

Karpathy's wiki uses `log.md` as append-only chronological
record with parseable format. ctxt has no explicit changelog.
As fan-out (US-0400) and lint (US-0402) produce mutations, an
audit trail becomes essential for debugging and trust.

---

## Acceptance Criteria

- [ ] Every mutation to the knowledge graph produces a
  changelog entry
- [ ] Entry format: timestamp, mutation_type, object_id(s),
  actor (user|agent|system), delta summary
- [ ] Log is append-only (immutable after write)
- [ ] Log is queryable by time range, mutation type, object,
  actor
- [ ] `ctxt log` displays recent entries
- [ ] Log entries are first-class objects (searchable via
  `ctxt find`)
- [ ] Mutation types: `create`, `update`, `delete`, `merge`,
  `enrich`, `link`, `unlink`, `page_update`, `lint_fix`

---

## Implementation Notes

### Entry Schema

```
changelog_entry {
  timestamp: "2026-04-23T14:30:00Z"
  mutation_type: "enrich"
  object_ids: ["o-abc123"]
  actor: "system:fan-out"
  delta: "extracted 3 entities, created 2 cross-refs"
  metadata: {
    job_id: "j-def456",
    profile: "research"
  }
}
```

### CLI Interface

```bash
# View recent log
ctxt log
ctxt log --limit 50

# Filter by type
ctxt log --type create,enrich

# Filter by time
ctxt log --since 2026-04-20 --until 2026-04-23

# Filter by object
ctxt log --object o-abc123

# Filter by actor
ctxt log --actor system:lint
```

---

## E2E Test Checklist

- [ ] Ingest object -> changelog entry with type `create`
- [ ] Enrich object -> entry with type `enrich` + delta
- [ ] Delete object -> entry with type `delete`
- [ ] Fan-out creates multiple entries (one per downstream
  mutation)
- [ ] `ctxt log --type create` only shows creates
- [ ] `ctxt log --since` respects time boundary
- [ ] `ctxt log --object` shows full history of one object
- [ ] Log entries are immutable (no update/delete API)
- [ ] Log entries searchable via `ctxt find "changelog"`

---

## Related Stories

- US-0400: Fan-out enrichment (produces many mutations to log)
- US-0402: Knowledge lint (lint_fix mutations logged)
- US-0401: Persistent composed pages (page_update logged)
