# JIT Strategy

The JIT (Just-In-Time) strategy is the catch-all `FamilyJIT` lateral discovery
strategy. It fires only when no platform/shape strategy claims a captured
event — see `lateral.Registry.Dispatch`.

Package: `internal/lateral/strategies/jit`
Family: `lateral.FamilyJIT`
Specificity: `0` (always matches, lowest priority)
Candidate type: `jit_subpath`

## Overview

Most lateral discovery is structural — the GitHub family knows that a repo
has siblings, an owner, sponsors. The JIT strategy is what runs when the
captured page lives on a domain *no platform strategy was written for*.
Instead of structural rules, it asks an LLM: "given this domain and this
page type, what sub-paths are likely to surface related entities?"

Pipeline:

1. **Classify** — host adapter (typically wrapping `pageshape.LLMClassifier`)
   produces a page-type label for the source URL.
2. **Propose** — `CachedProposer` consults its `ProposalCache` keyed on
   `(domain, page_type)`. Cache hit → cached sub-paths. Cache miss → call
   the host LLM proposer; cache the verdict.
3. **Execute** — sequentially fetch each proposed sub-path through the
   wired `Fetcher`. Cross-domain absolutes and unparseable paths drop
   silently; per-path errors do not abort the batch.
4. **Emit** — successes become `lateral.Candidate{CandidateType:"jit_subpath", Strategy:"jit"}` with `Preview.{page_type, body_size}`. Per-path failures route to the failure-event emitter.

## Wiring

The daemon registers JIT via the `Register` helper:

```go
import (
    "github.com/ideacrafterslabs/ctxt/internal/lateral"
    "github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

reg := lateral.NewRegistry()

strategy, ok := jit.Register(reg, jit.Config{Enabled: cfg.JIT.Enabled}, jit.Deps{
    Proposer:   adaptKitLLM(llmClient),     // → jit.Proposer
    Cache:      jit.NewMemoryProposalCache(),
    Fetcher:    adaptIBR(ibrClient),         // → jit.Fetcher
    Publisher:  busPublisher,                // optional: nil suppresses events
    Outage:     adaptBreaker(llmBreaker),    // optional: nil → NoOutage()
    Classifier: adaptPageshape(classifier),  // optional: nil → empty page-type
})
if !ok {
    log.Info("jit strategy disabled by config")
}
```

### Required deps (panic on nil when `Enabled=true`)

| Field | Production source |
| --- | --- |
| `Proposer` | `kit/ai/llm.Completer` adapter — implements `Propose(ctx, domain, pageType) ([]string, error)` |
| `Cache` | `jit.NewMemoryProposalCache()` (in-process) or persistent variant |
| `Fetcher` | `ibr` adapter — implements `Fetch(ctx, urlStr) (body, err)` |

### Optional deps (nil-safe defaults)

| Field | Default behavior when nil |
| --- | --- |
| `Publisher` | All event emission suppressed |
| `Outage` | `NoOutage()` — never reports outage; `recipe.served` events never fire |
| `Classifier` | Empty page-type passed to proposer |

## Operating

### Config

JIT lives under `lateral.strategies.jit` in your config layer. Operators
toggle it without restarting only when the substrate's reload pipeline is
configured to watch the strategies block — JIT itself does not subscribe
to SIGHUP for the gate.

```yaml
lateral:
  strategies:
    jit:
      enabled: false   # opt-in; default false
```

`scoring.threshold.jit_subpath` and `scoring.cap_k.jit_subpath` in the
substrate scoring block size JIT-specific cap-gate behavior. Recommended
starting values when first enabling:

```yaml
scoring:
  threshold:
    jit_subpath: 0.55  # tighter than structural strategies — JIT signal is weaker
  cap_k:
    jit_subpath: 3
```

### Disabling

Set `enabled: false` and either reload (if substrate reload watches the
strategies block) or restart. `Register` returns `(nil, false)` and the
strategy is not added to the registry — zero LLM calls, zero fetches.

### Resource cost

A single `Probe` makes:

- **0 or 1** LLM proposer calls (zero on cache hit).
- **N** Fetcher calls where N = sub-paths returned by the proposer (capped
  at 25 by `ParseProposalResponse`).

The `MemoryProposalCache` is unbounded. Long-running deployments seeing
unique-domain capture rates above ~1000/day should swap in a bounded or
persistent cache adapter (the `ProposalCache` interface is the seam).

## Events

JIT publishes through the wired `domain.EventPublisher` against the
substrate's lateral event catalog (`internal/lateral/events`).

| Topic | When | Severity | Payload |
| --- | --- | --- | --- |
| `ctxt.lateral.scan.failed` | Proposer (LLM/parse) error | Error | `{object_id, subject:<source URL>, qualifiers:{Mechanism:"jit_proposal", Reason:<err>}}` |
| `ctxt.lateral.subpath.failed` | Per-path fetch error | Error | `{object_id, subpath:<URL>, reason:<err>}` (one per failed URL) |
| `ctxt.lateral.recipe.served` | Cache hit while outage detector reports outage | Info | `{source_domain, page_type, qualifiers:{Circumstance:"llm_outage"}}` |

The happy path emits **zero** events — observe candidates flowing through
the substrate's downstream `candidate.created` event for cross-strategy
visibility.

### When `recipe.served` fires (and when it does not)

`recipe.served` requires **both**:

1. The cache had a prior verdict for `(domain, page_type)`.
2. The outage detector reports `IsOutage()==true` *at the moment the cache
   is consulted*.

A fresh cache miss that succeeds via the proposer does **not** fire
`recipe.served` even if the outage detector is active — the served value
came from the proposer, not the cache.

## Worked example

Captured event:

```json
{
  "object_id": "obj-2026-05-09-001",
  "namespace": "@x.note",
  "source_url": "https://random-no-tier-a.example/post/featured-1",
  "capture_pipeline": "browser_capture"
}
```

No platform/shape strategy claims this domain, so `Registry.Dispatch`
selects JIT. Probe runs:

1. `classifier.Classify(ctx, "https://random-no-tier-a.example/post/featured-1")` returns `"BlogPost"`.
2. `proposer.Propose(ctx, "random-no-tier-a.example", "BlogPost")` (cache miss, LLM consulted) returns:
   ```
   /about
   /team
   /blog
   ```
3. Cache populated with that slice keyed on `("random-no-tier-a.example", "BlogPost")`.
4. Executor fetches each (relative paths resolved against the source URL):
   - `https://random-no-tier-a.example/about` → 200 OK, 4.2 KB body
   - `https://random-no-tier-a.example/team` → 200 OK, 1.8 KB body
   - `https://random-no-tier-a.example/blog` → 200 OK, 12.0 KB body
5. Emit produces three candidates:
   ```go
   []lateral.Candidate{
       {URL: "https://random-no-tier-a.example/about", Strategy: "jit",
        CandidateType: "jit_subpath", Preview: {"page_type":"BlogPost", "body_size":4263}},
       {URL: "https://random-no-tier-a.example/team",  Strategy: "jit",
        CandidateType: "jit_subpath", Preview: {"page_type":"BlogPost", "body_size":1842}},
       {URL: "https://random-no-tier-a.example/blog",  Strategy: "jit",
        CandidateType: "jit_subpath", Preview: {"page_type":"BlogPost", "body_size":12089}},
   }
   ```

A subsequent capture on a sibling page (`/post/featured-2`) hits the cache
on step 2 — no LLM call, same three candidates emitted. No events fire on
either run.

If the LLM is unavailable on a *third* capture and the breaker has tripped,
the cache hit at step 2 still serves the cached sub-paths, but
`ctxt.lateral.recipe.served` fires with `Circumstance="llm_outage"`. The
fetches in step 4 still happen — only the proposer call is short-circuited.

## Limits & Future Work

- **Cache is unbounded.** A persistent or LRU-bounded `ProposalCache`
  adapter is a follow-on for production deployments.
- **Sequential fetch.** The executor walks sub-paths serially. Bounded
  parallelism is deferred until per-path retry/backoff semantics are
  pinned.
- **Single CandidateType.** `jit_subpath` is the only emission type. A
  taxonomy that infers candidate-type from path heuristics (e.g.
  `jit_team_page`, `jit_about_page`) is plausible but unproven and
  intentionally out of scope.
- **No structural metadata.** JIT cannot populate `Preview` with title,
  author, etc. — it has no domain-specific extraction. Downstream scorers
  see only `page_type` and `body_size`. Tighter scoring weights for
  `jit_subpath` are recommended (see Operating §Config).
- **Page-type signal is best-effort.** A nil or empty page-type is
  passed to the proposer as the empty string; the proposer is expected
  to handle that gracefully (the prompt builder includes the field
  unconditionally so an empty value still produces a parseable prompt).

## Related

- [Lateral Capture Discovery Design](../../superpowers/specs/2026-05-08-lateral-capture-discovery-design.md) — substrate spec
- [Amendment 1](../../superpowers/specs/2026-05-08-lateral-capture-discovery-design-amendment-1.md) — Tier B + multi-platform + shape family
- [Substrate event catalog](../../../internal/lateral/events/catalog.go) — full topic list
- [Event schema](../../../schemas/lateral_events.json) — wire format
