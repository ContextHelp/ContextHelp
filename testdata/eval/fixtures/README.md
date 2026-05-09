# Lateral eval fixtures

Labelled JSONL fixtures for the lateral discovery eval harness. The
T-0335 CI workflow runs `ctxt lateral eval replay` against every file
in this directory and gates PRs on regression of precision/recall vs
the `main` branch baseline.

## Fixture format

One JSON object per line. `#` comments and blank lines ignored.

```jsonl
{"event": {"object_id": "...", "source_url": "...", ...}, "expect": {...}}
```

The `event` block mirrors `lateral.CapturedEvent` with snake_case
fields:

- `object_id`, `namespace`, `source_url`, `capture_pipeline`, `persisted_at`
- `hints.generator`, `hints.meta_platform`, `hints.canonical_host`

The `expect` block (FixtureExpectation) is the operator's label:

- `strategy_id` — the strategy expected to claim the event
- `negative` — true when the event MUST emit zero candidates
- `min_candidates` — lower bound on candidate count from `strategy_id`
- `candidate_urls` — canonical URLs the candidates must include

## Curation

Fixtures here are synthetic-but-plausible URLs. Goal is **shape
regression**: detect when a strategy stops claiming a URL it used to,
or starts claiming one it shouldn't. They are not real-world accuracy
benchmarks — that's what the conversion-rate metric (T-0327) is for.

Add fixtures by:

1. Picking a strategy and a representative URL pattern from its
   `Applies` clauses.
2. For positive fixtures: pick the strategy ID that should claim, set
   `min_candidates: 1` (since URL-only candidates are still emit).
3. For negative fixtures: pick a URL that looks platform-shaped but
   shouldn't match (e.g. an api.* host on a platform that ignores
   API subdomains), set `negative: true`.

Negative fixtures are the more important regression guard: precision
breakages usually look like "we now claim something we used to skip."

## File layout

- `<strategy>.jsonl` — fixtures whose primary expected strategy is
  `<strategy>`. Each file is curated for that strategy specifically;
  cross-strategy fixtures live in `cross.jsonl`.
- `negative.jsonl` — global negative cases that no strategy should
  claim (admin pages, IP literals, etc.).
