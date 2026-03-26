# Query Language and Ranking

## Goal

Retrieve relevant context predictably by combining semantic search with explicit structured filters.

## Query modes in manual workflows

1. Semantic retrieval (exploration):

```bash
ctxt find "checkout conversion risks"
```

2. Structured retrieval (deterministic filtering):

```bash
ctxt list --type url --after 2026-02-01 --tag checkout,pricing
ctxt list --q "type==url;tag=in=(checkout,pricing)"
```

## Structured query patterns

### Equality and multi-value filters

```bash
ctxt list --q "type==url;tag=in=(ux,design)"
```

### Mention/entity filters

```bash
ctxt list --mention @project.checkout-redesign
ctxt list --q "mention:project.checkout-redesign"
```

### Related objects (graph traversal)

Find objects that share mention targets with a given entity (1-hop neighbourhood):

```bash
ctxt list --q "related==@arch.decision"
```

### Time-bounded retrieval

```bash
ctxt list --after 2026-02-01 --before 2026-02-18
```

## Ranking behavior (practical view)

Expected ranking inputs include:

- text relevance
- metadata filters
- mention/entity alignment
- profile context
- optional federated sources

For day-to-day usage:

1. Start with semantic mode for recall.
2. Constrain with structured filters for precision.
3. Save final structured query patterns in team docs for repeatability.

## Determinism guidance

Use structured mode when outcomes must be reproducible:

- release notes
- audit artifacts
- incident reports
- recurring automated summaries

Semantic mode is best for discovery and ideation.

## Runtime-vs-story note

- Story-target contracts (`US-0037`, `US-0038`) describe explicit agent bootstrap/query contracts.
- Current runtime workflows in this manual rely on `ctxt find` and `ctxt list --q` behavior documented in CLI/API references.

## Canonical references

- [`../../dpkms/query-language-spec.md`](../../dpkms/query-language-spec.md)
- [`../../dpkms/ranking-and-reranking.md`](../../dpkms/ranking-and-reranking.md)
- [`../../stories/search/US-0017-structured-rsql-query.md`](../../stories/search/US-0017-structured-rsql-query.md)

## Story alignment

- Core search: `US-0016`, `US-0017`, `US-0018`, `US-0021`
- Advanced search: `US-0051`, `US-0052`, `US-0053`, `US-0054`, `US-0055`, `US-0061`
- Ranking extensibility: `US-0045`
