---
status: shipped
---

# US-0408: Index-First Retrieval

**System Types:** dpkms (self-hosted), dpkms cloud, ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker or agent, I want an auto-generated topic
index — a compressed summary layer organized by category — that
I can scan before drilling into full objects, so I can orient
quickly and reduce unnecessary vector searches.

---

## Context

Karpathy's wiki uses `index.md` as content-oriented catalog:
category-organized, one-line summaries, read first on any query.
At small scale, the index suffices; at larger scale, add vector
search as fallback. ctxt has `ctxt list` and `ctxt find` but no
intermediate summary/index layer between "list all" and "vector
search".

This is the knowledge equivalent of xray's `map` command — a
compressed structural overview before targeted drill-down.

---

## Acceptance Criteria

- [ ] Auto-generated topic index maintained as the knowledge
  graph evolves
- [ ] Index organized by topic/category with one-line
  summaries per entry
- [ ] Index updated incrementally when objects are added,
  updated, or removed
- [ ] `ctxt index` displays the current topic index
- [ ] `ctxt index <topic>` shows entries under a specific
  topic
- [ ] Index is searchable (text match, not vector) for fast
  orientation
- [ ] Agents can read index before deciding whether to run
  full semantic search
- [ ] Index generation is configurable (auto vs manual refresh)

---

## Implementation Notes

### Index Structure

```
knowledge_index {
  last_updated: "2026-04-23T..."
  categories: {
    "authentication": {
      entry_count: 14,
      entries: [
        { id: "o-abc", one_liner: "OAuth2 token rotation..." },
        { id: "ep-auth-patterns", one_liner: "Entity page: auth..." }
      ]
    },
    "deployment": { ... },
    "ux-research": { ... }
  }
}
```

### CLI Interface

```bash
# Show full index
ctxt index

# Show index for topic
ctxt index authentication

# Refresh index
ctxt index --refresh

# Agent workflow: index -> decide -> search
ctxt index | grep -i "auth"  # fast text scan
ctxt find "token rotation"    # targeted vector search
```

---

## E2E Test Checklist

- [ ] Ingest 10 objects across 3 topics -> index shows 3
  categories with entries
- [ ] Ingest 11th object -> index updated incrementally
- [ ] Delete object -> index entry removed
- [ ] `ctxt index auth` shows only auth-related entries
- [ ] Index one-liners are meaningful summaries (not truncated
  content)
- [ ] `ctxt index --refresh` regenerates from scratch
- [ ] Agent reads index, identifies relevant topic, then runs
  targeted `ctxt find`

---

## Related Stories

- US-0401: Persistent composed pages (index references pages)
- US-0016: Natural language search (fallback after index scan)
- US-0403: Structured metadata extraction (topics feed index
  categories)

---

## E2E Tests

- `test/integration/us0408_index_retrieval_test.go::TestUS0408_IndexShowsThreeCategories`
- `test/integration/us0408_index_retrieval_test.go::TestUS0408_IncrementalUpdate`
- `test/integration/us0408_index_retrieval_test.go::TestUS0408_FilterByTopic`
- `test/integration/us0408_index_retrieval_test.go::TestUS0408_RefreshRegenerates`
