---
title: Lateral Capture Discovery — Amendment 1 (Tier B + multi-platform + shape family)
date: 2026-05-08
status: amendment
amends: 2026-05-08-lateral-capture-discovery-design.md
---

# Lateral Capture Discovery — Amendment 1

## Why this amendment

The original design scoped v1 to a single platform-keyed strategy
(`GitHubOwnerStrategy`) and explicitly cut JIT (Tier B). After spec review the
v1 scope was widened along three axes:

1. **Tier B (JIT LLM-proposed strategies)** — back in scope. lateral.discover
   must work for any captured source, not only ones with hand-coded Tier-A
   strategies.
2. **Multiple platform-keyed Tier-A strategies** — not just GitHub. v1 ships
   ten platform parents plus a curated set of specialized children.
3. **Shape-keyed strategy family** — orthogonal to platforms. Strategies keyed
   on *what the page is* (product page, blog, ecommerce storefront, etc.)
   rather than *where the page is hosted*.

This amendment carries the original design forward unchanged on its lifecycle,
scoring, identity-resolution, deferred-queue, reaper, and CLI surfaces. It
expands the strategy roster, adds a hierarchical specificity-aware dispatch
model, adds the shape-keyed strategy family, adds page-shape classification,
adds a research-intent disambiguation mechanism, adds a new failure mode for
JIT/LLM unavailability, and updates the out-of-scope list.

Where this amendment is silent, the original design applies.

## Strategy roster (v1)

### Tier-A platform-keyed parents (10)

Each strategy is identified by `<platform>Strategy`. `Applies(event)` returns
true on a `(matches bool, specificity int)` pair; the dispatcher picks the
highest-specificity match. Parents are matched at lower specificity than their
children.

- `GitHubStrategy` — `github.com` (renamed from `GitHubOwnerStrategy` in the
  original spec). Handles repo / PR / issue / profile / sponsor as
  parent-internal sub-paths.
- `GoogleStrategy` — `google.com` (catch-all for Google properties not handled
  by a specialized child).
- `XStrategy` — `x.com`, `twitter.com`. Handles user / tweet / thread.
- `LinkedInStrategy` — `linkedin.com`. Handles user / post / org.
- `ArxivStrategy` — `arxiv.org`. Handles paper. No v1 children.
- `WikipediaStrategy` — `*.wikipedia.org` in any language edition. Handles
  article.
- `MediumStrategy` — `medium.com` and Medium-hosted custom domains. Handles
  article generically.
- `SubstackStrategy` — `*.substack.com` and Substack custom domains. Handles
  publication / post / note generically.
- `BeehiivStrategy` — `*.beehiiv.com` and Beehiiv custom domains. Handles
  publication / post.
- `YouTubeStrategy` — `youtube.com`, `youtu.be`. Handles channel / video /
  playlist.

### Tier-A platform-keyed children (13)

The subset where the parent's surface is genuinely too broad to handle without
specialization. Other specializations are deferred to follow-on USes; the
hierarchy supports them but v1 ships only this set.

- **GitHub:** `GistStrategy` (`gist.github.com`), `SecurityAdvisoryStrategy`
  (`*/security/advisories/*` and global advisory database).
- **Google:** `GoogleSearchStrategy` (`google.com/search`),
  `GoogleScholarStrategy` (`scholar.google.com`), `GoogleTrendsStrategy`
  (`trends.google.com`), `GoogleNewsStrategy` (`news.google.com`).
- **Substack:** `SubstackPublicationStrategy` (publication homepage),
  `SubstackPostStrategy` (single post), `SubstackNotesStrategy` (note URLs).
- **Beehiiv:** `BeehiivPublicationStrategy` (publication homepage),
  `BeehiivPostStrategy` (single post).
- **Medium:** `MediumPublicationStrategy` (publication homepage),
  `MediumProfileStrategy` (user profile).

(Total: 13 children. Beyond ship-list count of 10 in earlier discussion — all
listed children meet the "parent surface is genuinely too broad" bar; v1 ships
all of them.)

### Tier-A shape-keyed parents (11)

A second strategy family, orthogonal to platform. Triggers on what a page *is*,
determined by the page-shape classifier (see "Page-shape classification"
below). Shape strategies fire alongside any matching platform strategy
(Q-rework-3 = D from amendment dialogue: union candidates, dedup at
materialization).

- `ProductPageStrategy` — product/SKU/landing-for-a-thing-being-sold pages.
  Lateral: features (topic anchors), target-market signals, pricing model,
  backlinks, keywords, competitive products, third-party reviews (G2,
  Capterra, Product Hunt), seller org canonical.
- `BlogStrategy` — blog index pages (vs. `BlogPostStrategy` for posts).
  Lateral: author roster, recent posts, RSS/Atom feed URL (candidate
  watch-target feeding US-0007), topics, about-page (org or person canonical).
- `BlogPostStrategy` — single blog posts. Lateral: same-author other posts,
  the blog itself (candidate `@blog.*` or `@organization.*`), topics, feed
  URL, backlinks, outbound categorized links (citations, references, "via"
  attributions), comments-from-canonical-people.
- `EcommerceStoreStrategy` — storefront homepages (Shopify, WooCommerce,
  Magento, custom). Lateral: merchant org canonical, top products (each →
  candidate `ProductPageStrategy` target), brand/about page,
  categories/collections, parent platform.
- `BusinessStrategy` — captured object resolves to (or is) an organization
  canonical, and research-intent disambiguation passes (see
  "Research-intent disambiguation" below). Lateral: org's products (each →
  candidate ProductPageStrategy target), competitors via
  SEO/keyword/category overlap, press mentions, leadership, funding round
  info.
- `LandingPageStrategy` — campaign landing pages (distinct from product pages
  by goal: conversion, not transaction). Lateral: the campaign behind it,
  A/B test variations findable via SEO surface, the org canonical, the
  call-to-action target.
- `PricingPageStrategy` — `/pricing` pages specifically. Lateral: competitive
  pricing benchmarks (other tools in same band), the org's product pages,
  pricing change history (via Wayback or ibr).
- `AboutPageStrategy` — `/about`, `/team`, `/company` pages of orgs. Lateral:
  org's leadership (canonical persons), founding date, parent/subsidiary
  relationships, press mentions, investors.
- `DocsPageStrategy` — software documentation pages. Lateral: the project the
  docs are for (canonical), the docs platform (GitBook, Read-the-Docs,
  Mintlify, Docusaurus — each is an org canonical), the docs version, the
  source repo if linked.
- `JobPostingStrategy` — non-LinkedIn job postings. Lateral: hiring org
  canonical, similar postings at other orgs, the recruiter contact, role-level
  (IC vs. management) and seniority signals.
- `ResearchPaperStrategy` — academic papers on hosts other than arXiv
  (Semantic Scholar, ResearchGate, publisher direct). Distinct from
  ArxivStrategy because the host doesn't expose arXiv-like APIs. Lateral:
  same-author other papers, citations, co-author list (candidate person
  canonicals), institutional affiliation, related papers via Semantic Scholar
  API.
- `EventPageStrategy` — conferences, webinars, meetups. Lateral: organizing
  org, speakers (candidate canonicals), sponsors, past editions, the event
  series if recurring.

### Tier-B fallback (1)

- `JITStrategy` — fires when **no** Tier-A strategy (platform-keyed or
  shape-keyed) claims the captured object. Asks the LLM "what lateral paths
  exist for this source's object shape?", executes proposed sub-paths via
  existing fetcher infrastructure (ibr; host APIs if any), returns candidates
  the same way Tier-A does. The LLM proposal becomes a recipe cached by
  `(source_domain, page_type)` — same caching shape as Tier-A ibr recipes.

### Total v1 strategy count

10 platform parents + 13 platform children + 11 shape parents + 1 JIT = **35
strategies**.

## Hierarchical specificity-aware dispatch

The strategy registry stays flat (per-strategy `Register()` calls), but each
strategy's `Applies(event)` now returns:

```go
type AppliesResult struct {
    Matches     bool
    Specificity int  // higher = more specific
}
```

The dispatcher:

1. Calls `Applies()` on every registered strategy.
2. Collects all matching strategies (Matches = true).
3. **Within the platform-keyed family**: keeps only the highest-specificity
   match. Children shadow parents.
4. **Within the shape-keyed family**: keeps all matching strategies (a page
   can be both a product page and a landing page — both fire).
5. Across families: union the results from step 3 and step 4. Both can fire
   for the same capture.
6. **Tier B (`JITStrategy`)** runs only if both step 3 and step 4 returned
   empty (zero Tier-A claimed the capture).

Rationale for asymmetric handling of step 3 vs. step 4: platform strategies
are designed to be mutually-exclusive specializations within a platform tree
(GistStrategy makes sense only when GitHubStrategy would also match — the
child is the right answer). Shape strategies are not mutually-exclusive; they
classify the page on independent dimensions (a page can simultaneously be a
landing page and a pricing page; both are legitimate lateral surfaces).

Specificity scoring within platform family:

- Domain match exact: +1
- Subdomain match: +1
- Path prefix match: +1 per matching path segment
- Path pattern match (regex/glob): +1 per constrained segment

So `GistStrategy.Applies("https://gist.github.com/jadb/abc123")` returns
`(true, specificity=2)` (subdomain + domain), and
`GitHubStrategy.Applies("https://gist.github.com/jadb/abc123")` returns
`(true, specificity=1)`. Gist wins.

Specificity scoring within shape family is constant (1) — the classifier's
decision is binary "is this this shape, yes/no", and multiple matches all
fire.

## Page-shape classification

Q-rework-7 = C+B: heuristic-first, LLM-fallback, cached as a recipe.

### Heuristic layer (zero LLM cost)

Pattern-match obvious cases. Examples:

- URL contains `/pricing` (case-insensitive) → likely `PricingPageStrategy`.
- URL ends in `/about`, `/team`, `/company` → likely `AboutPageStrategy`.
- HTML `<head>` contains `<link rel="alternate" type="application/rss+xml">`
  with a feed URL → likely `BlogStrategy` or `BlogPostStrategy` (post if URL
  is `/<year>/<slug>`-shaped, index otherwise).
- HTML contains `Shopify.shop` JS variable, or `myshopify.com` in script
  src, or `cdn.shopify.com` asset URLs → likely `EcommerceStoreStrategy`
  (storefront homepage) or `ProductPageStrategy` (product page; URL pattern
  `/products/<slug>`).
- HTML `<head>` contains Open Graph type `og:type=article` → likely
  `BlogPostStrategy`.
- HTML contains `application/ld+json` schema with `@type: Product` → likely
  `ProductPageStrategy`.
- HTML contains `application/ld+json` schema with `@type: JobPosting` →
  `JobPostingStrategy`.
- HTML contains `application/ld+json` schema with `@type: Event` →
  `EventPageStrategy`.
- HTML contains `application/ld+json` schema with `@type: Article` AND the
  publisher schema declares it's academic → `ResearchPaperStrategy`.
- Substack heuristic: `*.substack.com` in canonical URL OR Substack-specific
  meta tags → already handled by `SubstackStrategy` platform match; shape
  classifier doesn't run separately for these.

The heuristic layer maintains a simple ordered ruleset; first-rule-wins.
Heuristic verdicts are confident enough to skip the LLM call.

### LLM-fallback layer

For pages where the heuristic layer returns no match (or returns
"ambiguous"), the LLM classifies. Prompt: "Given this page's URL, title,
meta tags, and first ~2KB of body text, which of {ProductPage, Blog,
BlogPost, EcommerceStore, Business, LandingPage, PricingPage, AboutPage,
DocsPage, JobPosting, ResearchPaper, EventPage, none-of-these} apply? A page
may match multiple."

LLM verdict is recorded as a **page-shape recipe** keyed by
`(source_domain, url_pattern)` where `url_pattern` is derived from the URL
by replacing path segments that look like IDs/slugs with `*`. So
`mycompany.com/products/widget-pro-x12` becomes
`mycompany.com/products/*`. Future captures matching the pattern reuse the
verdict without an LLM call.

### Recipe lifecycle (page-shape)

Same failure-driven refresh as the original spec's ibr recipes (Section 4
mode 2):

- Re-evaluated when ibr extraction using the recipe returns empty or
  structurally suspicious output for a sub-path that depends on the recipe.
- No time-based TTL.

### Configuration

```yaml
lateral:
  page_shape:
    classifier:
      heuristic_first: true
      llm_fallback: true
    recipe:
      cache_key:
        - source_domain
        - url_pattern
      refresh_on_failure: true
      pattern_normalize:
        replace_ids_with_wildcard: true
```

## Research-intent disambiguation

Q-rework-9.1 = D: layered. Applies to shape strategies where research-intent
vs. personal-browsing matters. Currently scoped to `BusinessStrategy`,
`JobPostingStrategy`, `EventPageStrategy`, `ResearchPaperStrategy`. Other
shape strategies fire unconditionally (a captured product page is *always*
of interest for lateral; the user wouldn't have captured it otherwise).

### Layer A — baseline fire

Strategy fires for every matching capture in v1. Cap gate, threshold T, and
cold-cycle handle noise. This is the cold-start posture; produces labeled
data for layer C.

### Layer B — interest-registry boost

If active context contains an interest entry whose tags overlap with the
strategy's research-intent tags (e.g. `BusinessStrategy` looks for tags like
`competitive-analysis`, `market-research`, `vendor-evaluation` in any
`@interest.*` entry that's currently active), the strategy boosts its
confidence and the user's threshold T is implicitly relaxed for this scan.

Concretely: layer B applies a `+0.15` additive boost to the candidate's
final score. Configurable per strategy.

### Layer C — learned per-active-context-shape suppression

After ≥20 labeled candidates have run through the lifecycle for a given
strategy + active-context-shape pair, compute the bucket promotion rate. If
the rate is below a configurable threshold (default `0.005`, i.e. 0.5%), the
strategy auto-suppresses for that active-context-shape: skips the scan, emits
`lateral.skipped.learned_low_promotion`. Suppression is per-strategy and
per-active-context-shape; it does not affect the strategy's behavior in other
contexts.

Active-context-shape is fingerprinted by the same mechanism used for triage
caching (Q2.4 = A in original spec): a hash over the active context's tag
set + namespace distribution + interest registry entries.

Suppression decisions are recomputed on each `ctxt lateral suggest-weights`
run (Tier 3-B from original spec) and on a slow background cadence (default
24h).

### Configuration

```yaml
lateral:
  research_intent:
    enabled_for:
      - BusinessStrategy
      - JobPostingStrategy
      - EventPageStrategy
      - ResearchPaperStrategy
    layer_b_interest_boost: 0.15
    layer_c_min_labeled: 20
    layer_c_promotion_threshold: 0.005
    layer_c_recompute_cadence: 24h
```

### Reusability

The same layered detection applies to any future strategy where
research-vs-consume disambiguation is the relevant question. The mechanism
is one strategy property (`enabled_for` list) plus a small evaluator at
dispatcher time. Not a per-strategy reimplementation.

## Failure mode 7 — JIT proposal failure / LLM unavailable

A new failure mode added to the original spec's Section 4. JIT depends on
the LLM in two places: at recipe-authoring time (first capture from a new
source-page-type) and at fallback-classification time when the heuristic
layer returns no match.

### Knob 1 — LLM unavailable, no cached recipe yet

Q-rework-1 = B: defer scan to persistent queue. LLM is treated as a
load-bearing dependency in the same shape as eva (mode 5) and identity
resolver (mode 3). Re-try when LLM is back. Same persistent queue, 24h
retention; expired deferrals drop with `lateral.skipped.jit_unavailable`.

### Knob 2 — LLM unavailable, cached recipe exists

Use the cached recipe even if the cache is stale. A stale recipe is better
than no lateral output. The recipe will refresh on its next failure
(failure-driven lifecycle). Logs `lateral.recipe.using_during_llm_outage`
for ops visibility.

### Knob 3 — LLM returns garbage proposal

LLM responds but the proposal fails validation (no sub-paths proposed,
sub-paths reference unfetchable URLs, etc.). Skip the scan with
`lateral.scan.jit_proposal_failed`. Do not retry within the same scan.
Ambient retry on next parent re-capture handles transient LLM hallucination.

### Knob 4 — LLM rate limiting

If the LLM service has its own rate limiting (e.g. per-API-key TPM/RPM
limits), reuse the same dynamic-floor mechanism as GitHub rate limiting
(mode 1, dynamic [5%, 90%] clamp driven by trailing-4h actual call rate).
The trailing-4h window measures LLM calls, not API calls.

## Bus event additions

Add to the original spec's bus event catalog:

- `ctxt.lateral.scan.skipped` reasons extended: `jit_unavailable`,
  `learned_low_promotion`.
- `ctxt.lateral.scan.jit_proposal_failed` (re-instated; was removed during
  self-review of original spec).
- `ctxt.lateral.recipe.using_during_llm_outage`.
- `ctxt.lateral.classifier.heuristic_match` (heuristic layer matched).
- `ctxt.lateral.classifier.llm_classification` (LLM fallback ran).
- `ctxt.lateral.classifier.recipe_hit` (cached page-shape recipe used).
- `ctxt.lateral.research_intent.boost_applied` (layer B boost applied).
- `ctxt.lateral.research_intent.suppressed` (layer C auto-suppression
  active for this context-shape).

## Configuration additions

On top of the original spec's `lateral:` block:

```yaml
lateral:
  strategies:
    # platform-keyed parents
    GitHubStrategy:
      enabled: true
    GoogleStrategy:
      enabled: true
    XStrategy:
      enabled: true
    LinkedInStrategy:
      enabled: true
    ArxivStrategy:
      enabled: true
    WikipediaStrategy:
      enabled: true
    MediumStrategy:
      enabled: true
    SubstackStrategy:
      enabled: true
    BeehiivStrategy:
      enabled: true
    YouTubeStrategy:
      enabled: true
    # platform-keyed children (specificity-shadowed parents above)
    GistStrategy: { enabled: true }
    SecurityAdvisoryStrategy: { enabled: true }
    GoogleSearchStrategy: { enabled: true }
    GoogleScholarStrategy: { enabled: true }
    GoogleTrendsStrategy: { enabled: true }
    GoogleNewsStrategy: { enabled: true }
    SubstackPublicationStrategy: { enabled: true }
    SubstackPostStrategy: { enabled: true }
    SubstackNotesStrategy: { enabled: true }
    BeehiivPublicationStrategy: { enabled: true }
    BeehiivPostStrategy: { enabled: true }
    MediumPublicationStrategy: { enabled: true }
    MediumProfileStrategy: { enabled: true }
    # shape-keyed
    ProductPageStrategy: { enabled: true }
    BlogStrategy: { enabled: true }
    BlogPostStrategy: { enabled: true }
    EcommerceStoreStrategy: { enabled: true }
    BusinessStrategy: { enabled: true }
    LandingPageStrategy: { enabled: true }
    PricingPageStrategy: { enabled: true }
    AboutPageStrategy: { enabled: true }
    DocsPageStrategy: { enabled: true }
    JobPostingStrategy: { enabled: true }
    ResearchPaperStrategy: { enabled: true }
    EventPageStrategy: { enabled: true }
    # tier-B fallback
    JITStrategy:
      enabled: true

  page_shape:
    classifier:
      heuristic_first: true
      llm_fallback: true
    recipe:
      cache_key: [source_domain, url_pattern]
      refresh_on_failure: true
      pattern_normalize:
        replace_ids_with_wildcard: true

  research_intent:
    enabled_for:
      - BusinessStrategy
      - JobPostingStrategy
      - EventPageStrategy
      - ResearchPaperStrategy
    layer_b_interest_boost: 0.15
    layer_c_min_labeled: 20
    layer_c_promotion_threshold: 0.005
    layer_c_recompute_cadence: 24h

  jit:
    llm_rate_limit:
      trailing_window_hours: 4
      floor_pct_min: 0.05
      floor_pct_max: 0.90
    proposal_validation:
      min_subpaths: 1
      max_subpaths: 10
```

## Updated out-of-scope (replaces original)

- Online weight learning. Manual `suggest-weights` only.
- Cross-user / federated lateral signals.
- `--lateral-supplement` flag on Tier-A strategies. Tier A stays sealed;
  cannot be supplemented by JIT.
- Multi-machine federated queue coordination (Redis adapter exists; the
  inter-instance coordination of which instance handles which deferred event
  is out of scope; a single ctxt instance owns the queue).
- Web UI / dashboard for suggestions.

Removed from out-of-scope (now in v1):

- ~~Strategies beyond GitHub~~ — 34 additional strategies in v1.
- ~~JIT (LLM-proposed) strategies for unstructured sources (Tier B)~~ —
  `JITStrategy` is in v1.
- ~~Lateral on capture sources other than `code.github.*`~~ — lateral
  subscribes to `ctxt.ingest.object.persisted` from any capture pipeline.

## Acceptance criteria additions

In addition to the original spec's criteria:

- [ ] Strategy registry supports `Applies()` returning `(matches,
      specificity)` tuple.
- [ ] Dispatcher: highest-specificity match wins within platform family;
      all matching shape strategies fire; Tier B fires only if zero Tier-A
      claim.
- [ ] All 10 platform parents implemented and integration-tested with at
      least one cassette per platform.
- [ ] All 13 platform children implemented; cassettes verify they shadow
      the parent.
- [ ] All 11 shape strategies implemented; integration-tested against
      fixture HTML.
- [ ] Page-shape classifier: heuristic layer matches obvious cases; LLM
      fallback handles ambiguous; verdicts cached as page-shape recipes.
- [ ] Page-shape recipes refresh on failure (no time-based TTL).
- [ ] `JITStrategy` implemented; integration-tested with a non-platform
      source URL (e.g. a random news site); recipe persisted on first
      capture; reused on second capture.
- [ ] Research-intent layered detection: layer A always fires for
      `enabled_for` strategies; layer B applies boost when active
      interest matches; layer C suppresses after ≥20 labeled candidates
      below threshold.
- [ ] Failure mode 7 (JIT proposal failure / LLM unavailable): defer to
      queue when no cached recipe; use cached recipe during outage if
      available; skip on garbage proposal.
- [ ] LLM rate limiting reuses the GitHub-rate-limit dynamic-floor
      mechanism applied to LLM calls.
- [ ] All new bus events emitted per "Bus event additions" section.

## Test strategy additions

In addition to the original spec's test strategy:

### Cassette tests

- One cassette per platform-keyed parent (10) and per platform-keyed child
  (13) capturing a representative URL of that platform's primary surface.
- Fixture HTML pages for each shape-keyed strategy (11) capturing a
  representative shape (one product page, one blog post, one job posting,
  etc.).
- LLM proposal cassettes for `JITStrategy`: first-time-source recipe
  authoring, cached-recipe hit, recipe-staleness-recovery cycle.

### Integration tests

- End-to-end: capture a non-GitHub URL → JIT runs (no Tier-A claims) →
  recipe authored → second capture from same source-page-type uses cached
  recipe.
- End-to-end: capture a Shopify product page → both `ShopifyStrategy`
  (platform; specialized child of e-commerce parent if added) AND
  `ProductPageStrategy` (shape) AND `EcommerceStoreStrategy` (shape, if
  storefront context inferred) fire → candidates merged → deduped at
  materialization.
- End-to-end: research-intent. Capture a company's about page with NO
  active interest → `BusinessStrategy` fires (layer A) but few candidates
  promote → after 20 labeled candidates with no promotions, layer C
  suppresses. Activate `@interest.competitive-research` → suppression is
  bypassed via layer B boost; promotions resume.

### Property-based tests

Add to original spec's property tests:

- "Children always shadow parents in platform family": for every
  platform-child pair, registering both → only child fires for child-URLs.
- "Shape strategies are independent": registering N shape strategies →
  capture matching M of them → exactly M fire (not more, not fewer).
- "JIT fires only when no Tier-A claims": for every URL where any Tier-A
  matches, JIT does not fire.

## Documentation deliverables additions

In addition to the original spec's deliverables:

- ADR documenting the platform-vs-shape strategy family split, the
  specificity-aware dispatch model, and the research-intent layered
  detection.
- Per-platform documentation page in `docs/architecture/lateral/` (one per
  platform parent) describing supported sub-paths, identity-resolution
  targets, and known limitations.
- Per-shape-strategy documentation page in `docs/architecture/lateral/`
  describing what classifies as that shape, lateral surface, and known
  limitations.
- Update to `docs/manual/workflows/lateral-discovery.md` covering Tier B
  experience for users on long-tail sources.
