# US-0406: Associative Object Links

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker, I want first-class typed links between
objects — "contradicts", "extends", "supersedes", "supports",
"related-to" — so the knowledge graph captures associative
trails, not just co-occurrence.

---

## Context

Vannevar Bush's Memex (1945) valued connections as much as
documents. Karpathy's wiki builds associative trails via
cross-references. ctxt has `--explain` for search scores and
entity relationships (US-0046) but no explicit typed links
between arbitrary objects. Links enable graph traversal,
contradiction detection (US-0402), and richer composition.

---

## Acceptance Criteria

- [ ] Objects can be linked with typed relationships:
  `contradicts`, `extends`, `supersedes`, `supports`,
  `related-to`, `derived-from`
- [ ] Links are bidirectional (A extends B implies B is
  extended-by A)
- [ ] Links stored as first-class edges in the knowledge graph
- [ ] Links created automatically by fan-out (US-0400) and
  manually via CLI
- [ ] `ctxt links <object_id>` lists all links for an object
- [ ] Links are traversable: `ctxt links <id> --follow 2`
  (depth-limited traversal)
- [ ] Link types are extensible (user can add custom types)
- [ ] Deleting an object removes its links (cascade)

---

## Implementation Notes

### Link Schema

```
object_link {
  id: "lnk-001"
  source_id: "o-abc123"
  target_id: "o-def456"
  link_type: "extends"
  inverse_type: "extended-by"
  created_by: "system:fan-out" | "user:jad"
  created_at: "2026-04-23T..."
  context: "new object adds auth token rotation details"
}
```

### CLI Interface

```bash
# Create link manually
ctxt link o-abc123 o-def456 --type extends

# List links
ctxt links o-abc123
ctxt links o-abc123 --type contradicts

# Traverse (depth 2)
ctxt links o-abc123 --follow 2

# Remove link
ctxt unlink o-abc123 o-def456
```

### REST API

```
POST /objects/{id}/links
{
  "target_id": "o-def456",
  "link_type": "extends",
  "context": "adds detail on token rotation"
}

GET /objects/{id}/links?type=extends&depth=2
```

---

## E2E Test Checklist

- [ ] Create link A extends B -> verify bidirectional
  (A extends B, B extended-by A)
- [ ] `ctxt links A` shows all link types with targets
- [ ] `ctxt links A --follow 2` traverses 2 levels deep
- [ ] Delete object A -> links from/to A removed
- [ ] Fan-out creates links automatically when cross-refs
  detected
- [ ] Custom link type registered and usable
- [ ] Contradicts links surfaced by lint (US-0402)

---

## Related Stories

- US-0046: Extract relationships between entities (entity-level)
- US-0400: Fan-out enrichment (auto-creates links)
- US-0402: Knowledge lint (detects contradiction links)
- US-0401: Persistent composed pages (pages link to sources)
