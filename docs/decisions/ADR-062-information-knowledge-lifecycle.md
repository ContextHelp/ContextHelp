# ADR-062 – Information/Knowledge Lifecycle Model

> **Status:** Proposed
> **Date:** 2026-03-26
> **Author:** $USER
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** N/A
> **References:** ADR-013, ADR-016, ADR-020, ADR-034, ADR-042, ADR-049

---

## Context

All current knowledge objects share the same first-class status regardless of how ephemeral or
durable they are. A tweet captured at 9am and a decision made after 12 months of cross-team
debate both live in `objects` with identical schema, identical resurfacing weight, identical
export treatment.

This conflates two fundamentally different things:

- **Information:** perishable. Shelf-life measured in hours–days. A job status, a price signal,
  a news article, a PR review comment. Relevance decays as time passes and context shifts.
- **Knowledge:** persistent. Earned through repeated interaction, corroboration across sources,
  explicit confirmation. A design decision, a resolved entity, a concept that has been mentioned
  dozens of times across unrelated pipelines.

The system treats both identically, which causes:

1. **Resurfacing noise** — stale information resurfaces alongside durable knowledge, diluting
   signal and eroding user trust in the surfacing engine (ADR-016).
2. **Export bloat** — expired information pollutes full exports (ADR-020) with content users
   don't need and didn't ask to keep.
3. **Search signal pollution** — ranking (ADR-011) treats a single-ingestion tweet the same as
   an entity mentioned across 40 objects.
4. **Thin sync waste** — ADR-034 defines `none | index | content` replication levels; without
   tier awareness, index-only sync pulls ephemeral content that will be stale before first use.
5. **Graph edge noise** — edges (ADR-049) carry uniform weight=1.0; a backlink from a stale
   news article and one from a year-old decision document are indistinguishable.

The gap was crystallised by ADR-042 (cognitive memory taxonomy), which identified _cognitive
efficiency_ (heat-based archival for unbounded growth) as a Phase-4 gap but did not define the
underlying lifecycle semantics.

**Affected subsystems:**
- `objects` schema (new fields)
- `edges` schema (confidence already present; needs semantic guidance)
- Resurfacing engine (ADR-016) — tier-aware queue population
- Export/import (ADR-020) — tier-aware inclusion defaults
- Thin sync (ADR-034) — map tier to replication level
- Search ranking (ADR-011, ADR-061) — confidence/tier as ranking signals
- Ingestion pipelines (ADR-004, ADR-007) — TTL assignment at enqueue time
- CLI (`ctxt inbox --stale`) — user-facing triage surface

---

## Decision

**dPKMS and ctxt adopt a formal information/knowledge lifecycle model with three tiers, a
configurable decay model for perishable objects, and an interaction-driven confidence model for
durable objects.**

See diagram: [lifecycle-tiers-v1.mmd](ADR-062-information-knowledge-lifecycle/lifecycle-tiers-v1.mmd)

### 1. Object Tiers

Every object carries an `object_tier` field:

| Tier | Value | Semantics |
|------|-------|-----------|
| 0 | `information` | Perishable. Has TTL. Decays over time. Default for ingested content. |
| 1 | `consolidating` | Accumulating evidence. TTL extended. Decay paused. User review pending. |
| 2 | `knowledge` | Durable. No TTL. Persists indefinitely. Included in all exports. |

Default on ingestion: `information`. Pipelines may override based on pipeline type config.

### 2. Decay Model (information tier)

Objects at tier `information` carry:

- `expires_at TEXT` — absolute expiry timestamp. NULL = no expiry.
- `decay_score REAL DEFAULT 1.0` — current relevance weight [0.0, 1.0]. Applied at query
  and resurfacing time as a multiplier against the base relevance score.
- `interaction_count INTEGER DEFAULT 0` — total interactions (ingestion, search hit, manual
  reference, surfacing dismissal/keep).

TTL defaults are configurable per pipeline type (see Config Shape below). Decay function:

```
decay_score = exp(-λ * age_hours)
λ = ln(2) / half_life_hours    -- half_life from pipeline TTL config
decay_score clamped to [0.05, 1.0]  -- floor prevents full invisibility
```

`decay_score` is recomputed lazily at read time (not stored persistently until a write occurs).
Score written to DB only when object is explicitly touched (search hit, surfacing, pipeline
re-run), avoiding unnecessary write amplification.

### 3. Interaction-Driven Growth (consolidating → knowledge)

Objects accumulate evidence passively. Signals tracked:

| Signal | `interaction_count` increment |
|--------|-------------------------------|
| New ingestion path references same content hash | +3 |
| Search result returned to user (clicked/used) | +2 |
| Entity mention in a new object | +1 |
| Backlink edge added (ADR-049) | +1 |
| User explicit `ctxt promote` | threshold override, immediate → knowledge |
| User explicit `ctxt demote` / TTL extend | stays information |

**Tier transition thresholds (defaults, configurable):**

```yaml
lifecycle:
  thresholds:
    to_consolidating:
      interaction_count: 3      # min interactions
      source_count: 2           # min distinct ingestion sources
    to_knowledge:
      interaction_count: 10     # min interactions
      source_count: 3           # min distinct sources
      age_hours: 48             # min age (prevents flash-in-pan promotions)
      or_explicit_confirm: true # user explicit confirm always promotes
```

Transition engine runs as a background observer (ADR-012 plugin hook) or inline at
write time. It is idempotent: re-evaluating a `knowledge` object leaves it at `knowledge`.

### 4. Resurfacing Queue — Tier Awareness

ADR-016 resurfacing engine must respect tier:

- `knowledge` objects: always eligible for resurfacing. Score not decay-penalised.
- `consolidating` objects: eligible. Score slightly penalised (decay paused, not removed).
- `information` objects: eligible only if `decay_score > 0.3` (configurable threshold).
- Expired `information` (`expires_at < now`): excluded from resurfacing queue. Moved to
  `ctxt inbox --stale` surface for user decision.

### 5. Export/Import — Tier-Aware Inclusion

ADR-020 export modes updated:

| Export mode | information | consolidating | knowledge |
|-------------|-------------|---------------|-----------|
| `full` | included if not expired | always | always |
| `incremental` | included if not expired | always | always |
| `selective` | user-specified | user-specified | user-specified |
| `minimal` | excluded | excluded | always |

Default for `--minimal` flag: knowledge only. Expired information excluded by default from
all modes except `full --include-expired`.

### 6. Thin Sync Tier Mapping

ADR-034 `local_copy` level auto-derived from tier if not overridden:

| Tier | Default `local_copy` |
|------|----------------------|
| `information` | `index` — fetch full content only if still live |
| `consolidating` | `index` — upgrade to `content` if promoted |
| `knowledge` | `content` — always replicated locally |

### 7. Edge Confidence

`edges.weight REAL DEFAULT 1.0` already exists (migration 001). ADR-049 treats it as a
numeric weight without semantic definition. This ADR assigns meaning:

- Edge weight = corroboration confidence, range [0.0, 2.0]:
  - `1.0` = single observation (default)
  - Weight increments by `+0.1` per additional corroborating mention
  - Capped at `2.0`
  - Edge weight decays proportionally if `from` or `to` object is `information` tier and
    `decay_score < 0.5`

### 8. CLI Surface

```
ctxt inbox --stale              # list expired/decaying information objects
ctxt inbox --stale --promote    # interactive: promote each to consolidating/knowledge
ctxt inbox --stale --discard    # bulk discard expired information
ctxt promote <id>               # explicit tier promotion
ctxt demote <id>                # explicit tier demotion (resets to information)
ctxt show <id> --lifecycle      # show tier, decay_score, expires_at, interaction_count
```

---

## Rationale

### Why three tiers (not two, not four)

- **Two** (information | knowledge): loses the intermediate state where evidence is
  accumulating but hasn't crossed the knowledge threshold. Without `consolidating`, the
  system must either promote too eagerly (noise) or too conservatively (misses real knowledge).
- **Four or more**: cognitive overhead for users and API consumers. Three maps to a natural
  human mental model: ephemeral → accumulating → settled.

### Why decay function rather than hard TTL

Hard TTL creates cliff edges: an object is fully relevant until the second it expires, then
gone. Decay provides a gradient that allows:
- Objects approaching expiry to rank lower naturally
- Users to see declining objects in `--stale` before they fully expire
- Promotion paths to halt decay mid-flight

SuperMemory (ADR-038) proposed "intelligent forgetting" as a cloud concept; this ADR
implements it locally without cloud dependency.

### Why interaction_count as primary promotion signal

Interaction count is the most direct proxy for system-level corroboration. It captures:
- Multi-source ingestion (strongest signal)
- Human validation (explicit search use)
- Graph density (mention backlinks)

Time alone (age) is insufficient: a stale tweet grows old without becoming knowledge.
Age is used as a gate (minimum age) to prevent flash-in-pan promotions, not as a driver.

### Why lazy decay_score recomputation

Writing `decay_score` on every read would generate O(n_objects) writes per background scan,
defeating WAL efficiency. Computing at read time with periodic lazy writes balances
freshness against write amplification. SQLite WAL (ADR-021) handles concurrent reads fine.

### Why edge weight [0.0, 2.0] not [0.0, 1.0]

A single-observation edge (weight 1.0) should be distinguishable from both no-evidence
(0.0) and strong corroboration (>1.0). Starting at 1.0 and growing to 2.0 makes the
semantic clear: above-baseline means corroborated.

---

## Consequences

### Positive

- Resurfacing signal quality improves: knowledge resurfaces; stale information doesn't.
- Export sizes shrink meaningfully for `minimal` mode.
- Search ranking gains a principled second signal (tier + confidence) beyond recency.
- Graph edges become semantically meaningful, enabling trust-weighted traversal.
- `ctxt inbox --stale` closes the loop: users can act on decaying objects before they
  vanish, enabling deliberate promote-or-discard workflows.
- Thin sync behaviour aligns with actual durability needs (ADR-034).

### Negative

- Schema migration required: 4 new columns on `objects`, semantic guidance on edges.
- Decay computation added to read path — small CPU cost per object in results.
- Tier transitions require a background observer or inline promotion check — complexity.
- Config surface grows: per-pipeline TTL config must be documented and validated.
- UX complexity: users need to understand three tiers. Documentation and onboarding needed.

### Neutral

- `object_tier` is additive — existing objects default to `information` without breakage.
- Decay floor (0.05) ensures no object becomes permanently invisible without explicit
  discard; user always has a path to recover it.
- ADR-042 Gap 3 (cognitive efficiency) is now explicitly addressed by the decay + promote
  cycle; ADR-042 can reference this ADR for Phase 4 implementation guidance.

---

## Implementation Notes

### Schema Migration (new migration: 022_lifecycle_model.sql)

```sql
-- Lifecycle tier: information | consolidating | knowledge
ALTER TABLE objects ADD COLUMN object_tier TEXT NOT NULL DEFAULT 'information';
-- Absolute expiry; NULL = no expiry (knowledge tier objects)
ALTER TABLE objects ADD COLUMN expires_at TEXT;
-- Current relevance weight [0.0, 1.0]; recomputed lazily
ALTER TABLE objects ADD COLUMN decay_score REAL NOT NULL DEFAULT 1.0;
-- Total interaction signals accumulated
ALTER TABLE objects ADD COLUMN interaction_count INTEGER NOT NULL DEFAULT 0;
-- Distinct ingestion sources (stored as count; full list in metadata if needed)
ALTER TABLE objects ADD COLUMN source_count INTEGER NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS idx_objects_tier ON objects(object_tier);
CREATE INDEX IF NOT EXISTS idx_objects_expires_at ON objects(expires_at)
    WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_objects_decay_score ON objects(decay_score);
```

Note: `edges.weight` already exists; no schema change. Semantic definition only.

### Config Shape

```yaml
lifecycle:
  ttl_defaults:                   # half-life for decay function
    tweet:            6h
    news_article:     24h
    job_status:       4h
    pr_review:        72h
    web_page:         168h        # 7 days
    document:         720h        # 30 days
    note:             null        # no TTL (user-created content)
    decision:         null        # no TTL
    entity:           null        # no TTL; entities are always knowledge
  tier_overrides:
    decision:         knowledge   # pipelines for decisions always emit knowledge
    entity:           knowledge
  thresholds:
    to_consolidating:
      interaction_count: 3
      source_count: 2
    to_knowledge:
      interaction_count: 10
      source_count: 3
      age_hours: 48
      or_explicit_confirm: true
  resurfacing:
    min_decay_score: 0.3          # below this: exclude from resurfacing, move to stale
  export:
    minimal_includes_expired: false
```

### Affected Modules

- `internal/storage/sqlite/migrations/` — new migration 022
- `internal/storage/sqlite/` — query builder: inject `decay_score` multiplier at read time
- `internal/engine/lifecycle/` — new package: decay calculator, tier transition evaluator
- `internal/engine/resurfacing/` — tier-aware queue population filter
- `internal/engine/export/` — tier-aware inclusion logic
- `cmd/ctxt/inbox.go` — `--stale` flag and interactive promote/discard
- `cmd/ctxt/promote.go` / `cmd/ctxt/demote.go` — explicit tier commands
- `docs/design.md` — add lifecycle section
- `docs/configuration.md` — add `lifecycle:` config block

### Sprint Impact

| ADR/Sprint | Amendment required |
|------------|--------------------|
| ADR-016 (resurfacing) | add tier-aware filter to queue population |
| ADR-020 (export/import) | add tier-aware inclusion defaults per mode |
| ADR-034 (thin sync) | map tier to default `local_copy` level |
| ADR-042 (cognitive memory) | mark Gap 3 as addressed by ADR-062 |
| ADR-049 (edges) | add semantic definition for weight field |
| US-0032 (backup ops) | note expired information exclusion from minimal backup |
| US-0034 (backup test) | add test: minimal export excludes expired information tier |

### New Tasks Required

- `T-XXXX implement(schema): migration 022 — lifecycle fields on objects`
- `T-XXXX implement(engine): decay calculator + lazy score recomputation`
- `T-XXXX implement(engine): tier transition evaluator + background observer hook`
- `T-XXXX implement(engine): tier-aware resurfacing queue filter`
- `T-XXXX implement(cli): ctxt inbox --stale surface + promote/discard UX`
- `T-XXXX implement(export): tier-aware inclusion defaults`
- `T-XXXX implement(sync): tier → local_copy mapping for thin sync`
- `T-XXXX docs(config): lifecycle config block in configuration.md`

### Phasing

- **Phase 3 (Q2 2026):** Schema + decay model + `ctxt inbox --stale` + export tier filter.
  Minimum viable: objects get `expires_at`; stale surface works; minimal export shrinks.
- **Phase 3 (Q3 2026):** Tier transitions (consolidating → knowledge) + edge confidence.
  Interaction counting wired into ingestion and search hit paths.
- **Phase 4 (Q1 2027):** Full cognitive efficiency (ADR-042 Gap 3): heat-based archival,
  bulk tier re-evaluation, graph pruning for very-low-confidence edges.

---

## References

- [lifecycle-tiers-v1.mmd](ADR-062-information-knowledge-lifecycle/lifecycle-tiers-v1.mmd)
- ADR-013 – Knowledge Graph and Mentions (backlink density as promotion signal)
- ADR-016 – Just-In-Time Surfacing (tier-aware queue population)
- ADR-020 – Export/Import Portability (tier-aware inclusion)
- ADR-021 – Multi-Backend Storage (SQLite WAL, lazy write rationale)
- ADR-034 – Registry Thin Sync (tier → local_copy mapping)
- ADR-038 – SuperMemory Not Adopted (memory decay reference)
- ADR-042 – Cognitive Memory Taxonomy (Gap 3: cognitive efficiency)
- ADR-049 – Edges Table (edge weight semantic definition)
- ADR-061 – Search Quality Tiers (orthogonal; tier signals feed ranking)
- migration 001_initial.sql – objects + edges base schema
- migration 017_resurfacing_queue.sql – resurfacing queue base schema

---
