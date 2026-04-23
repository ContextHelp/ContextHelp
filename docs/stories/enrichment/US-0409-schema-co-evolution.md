# US-0409: Schema Co-Evolution per Profile

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Maintainers](../../personas/maintainers.md),
[Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a maintainer, I want each focus profile to evolve its own
conventions — entity types, classification rules, topic
vocabulary, extraction templates — so the system adapts to
domain-specific knowledge patterns without global schema
changes.

---

## Context

Karpathy's wiki co-evolves its schema document with the domain:
conventions, page types, and workflows grow organically. ctxt
profiles are currently static filters. Upgrading profiles to
carry domain-specific schema enables per-domain classification
without polluting the global vocabulary.

---

## Acceptance Criteria

- [ ] Each profile can define custom entity types beyond the
  global set
- [ ] Each profile can define custom topic vocabulary
- [ ] Each profile can define custom classification rules
  (e.g., "in research profile, classify arxiv links as
  'paper' not 'reference'")
- [ ] Profile schema versioned with migration support
- [ ] Schema changes propagate to new ingestions but don't
  retroactively re-classify existing objects (unless
  explicitly requested)
- [ ] `ctxt profile schema show <profile>` displays current
  schema
- [ ] `ctxt profile schema evolve <profile>` suggests schema
  improvements based on recent ingestions

---

## Implementation Notes

### Profile Schema Extension

```
profile_schema {
  profile: "research"
  version: 3
  entity_types: ["paper", "author", "dataset", "benchmark"]
  topic_vocabulary: ["nlp", "cv", "rl", "llm", "scaling"]
  classification_rules: [
    { pattern: "arxiv.org", type: "paper" },
    { pattern: "huggingface.co", type: "dataset" }
  ]
  extraction_templates: {
    "paper": {
      fields: ["title", "authors", "abstract", "year"]
    }
  }
}
```

### CLI Interface

```bash
# Show profile schema
ctxt profile schema show research

# Add entity type to profile
ctxt profile schema add-type research paper

# Add topic to vocabulary
ctxt profile schema add-topic research "scaling-laws"

# Suggest schema evolution
ctxt profile schema evolve research
# -> "Based on last 50 ingestions, suggest adding:
#     entity types: [benchmark, leaderboard]
#     topics: [reasoning, multimodal]"

# Apply suggested evolution
ctxt profile schema evolve research --apply
```

---

## E2E Test Checklist

- [ ] Profile with custom entity type "paper" -> arxiv ingest
  classified as paper (not generic reference)
- [ ] Global profile doesn't see "paper" type
- [ ] Schema version incremented on change
- [ ] `evolve` suggests types/topics from recent content
- [ ] `evolve --apply` updates schema and increments version
- [ ] New ingestions use updated schema; old objects unchanged
- [ ] `ctxt profile schema show` displays full schema

---

## Related Stories

- US-0403: Structured metadata extraction (uses profile schema)
- US-0014: Constrain extraction with LMQL (mechanism)
- US-0020: Apply focus profile to search (profile as filter)
- US-0400: Fan-out enrichment (uses profile-specific rules)
