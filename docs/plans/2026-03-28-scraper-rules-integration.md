# Implementation Plan: Miniflux-Style Scraper Rules for ctxt

**Date:** 2026-03-28
**Status:** Draft
**Applies to:** `ctxt`
**Related:** browser extension capture, authenticated web fetch, RSS feed ingestion

---

## Goal

Add a reusable article extraction layer to `ctxt` that combines:

- per-domain CSS selector rules, modeled after Miniflux's scraper rules
- generic readability fallback when no rule matches or a rule fails
- optional authenticated fetching using the existing browser cookie bridge

The result should improve two paths:

1. Web capture and authenticated page fetch
2. Feed item enrichment when feeds only provide excerpts or poor-quality HTML

---

## Current State

### What Miniflux already does well

Miniflux maintains a curated `domain -> CSS selector` map and applies those selectors only when the fetched page remains on the same site after redirects. Otherwise it falls back to readability-style extraction.

This is a strong fit for `ctxt` because it improves extraction quality on sites where generic article parsing fails.

### What ctxt already has

- Feed sync pipeline that fetches and parses RSS/Atom/JSON feeds
- Deduplication for feed items by GUID
- A browser cookie bridge and a shared `ClientBridge` abstraction for HTTP requests
- Capture endpoints for full page, selection, and element capture
- Planning and config references for a readability-based extraction flow

### What ctxt is missing

- No implemented article extraction step in the runtime
- No selector rule store or rule loader
- No bridge from browser cookie cache into real article fetches
- No feed-item page fetch/enrichment after feed parsing

---

## Design Summary

Introduce a shared extraction subsystem with this decision order:

1. Fetch the target page with `ClientBridge`
2. Verify the response is HTML/XHTML
3. Resolve the effective URL after redirects
4. If the effective URL is same-site and a selector rule exists for the domain, extract matching nodes with `goquery`
5. If custom-rule extraction yields empty or unusable output, fall back to readability extraction
6. Persist extraction metadata for debugging and ranking

This subsystem should be reusable from both web capture and feed enrichment pipelines.

---

## Rule Model

### Rule format

Store rules outside code in a small config file rather than hardcoding a Go map.

Recommended format:

```yaml
scraper_rules:
  arstechnica.com: "div.post-content"
  blog.cloudflare.com: "div.post-content"
  github.com: "article.entry-content"
  theverge.com: "h2.inline:nth-child(2),h2.duet--article--dangerously-set-cms-markup,figure.w-full,div.duet--article--article-body-component"
```

### Matching behavior

- Normalize domains by stripping `www.`
- Match exact domains first
- Optional future enhancement: wildcard support like `*.substack.com`

### Extraction behavior

- Preserve `<base href>` when present for relative links
- Record which rule was used in metadata
- Fall back to readability if selector extraction is empty

---

## Proposed Packages and Files

### New files

- `hops/main/internal/extract/scraper/rules.go`
- `hops/main/internal/extract/scraper/loader.go`
- `hops/main/internal/extract/scraper/extractor.go`
- `hops/main/internal/extract/scraper/extractor_test.go`
- `hops/main/internal/pipeline/steps/article_scraper.go`
- `hops/main/internal/pipeline/steps/article_scraper_test.go`
- `hops/main/internal/pipeline/steps/feed_item_fetcher.go`
- `hops/main/internal/pipeline/steps/feed_item_fetcher_test.go`
- `hops/main/internal/pipeline/builtins/web_page.go`
- `hops/main/internal/pipeline/builtins/web_authenticated.go`

### Existing files likely to change

- `hops/main/internal/pipeline/steps/client_bridge.go`
- `hops/main/internal/server/ws/cookie_bridge.go`
- `hops/main/internal/server/http/handlers_capture.go`
- `hops/main/internal/pipeline/builtins/feed_sync.go`
- `hops/main/config/config.plugins.yaml`
- `hops/main/docs/stories/capture/US-0207-web-tab-capture.md`
- `hops/main/docs/stories/capture/US-0209-authenticated-web-fetch.md`

---

## Phase Plan

## Phase 1: Extraction Core
**Type:** API / Infrastructure
**Estimated:** 3-4 hours
**Files:** `internal/extract/scraper/*`

**Tasks**:
- [ ] Create a small scraper package with a rules loader and domain matcher
- [ ] Implement HTML content-type gating
- [ ] Implement selector-based extraction with `goquery`
- [ ] Implement readability fallback when selector extraction is unavailable or empty
- [ ] Return extraction metadata: effective URL, base URL, rule used, fallback used
- [ ] Add fixtures and tests for selector extraction, base URL handling, and fallback behavior

**Verification Criteria**:
- [ ] Exact-domain selector lookup works with and without `www`
- [ ] Selector extraction returns concatenated HTML for matched nodes
- [ ] Empty selector result triggers readability fallback
- [ ] Relative `<base>` is ignored and absolute `<base>` is preserved
- [ ] Non-HTML content types are rejected cleanly

**Exit Criteria**:
- Shared extractor exists and is test-covered without touching capture or feed pipelines yet.

---

## Phase 2: Web Capture Integration
**Type:** API
**Estimated:** 2-3 hours
**Files:** `internal/pipeline/steps/article_scraper.go`, `internal/pipeline/builtins/web_page.go`, `internal/server/http/handlers_capture.go`

**Tasks**:
- [ ] Add a pipeline step that calls the shared extractor for page URLs or prefetched HTML
- [ ] Define a built-in `web.page` pipeline that uses the new extraction step
- [ ] Update capture defaults so full-page capture can target `web.page` instead of generic URL analysis
- [ ] Store extraction metadata on the resulting object
- [ ] Ensure selection and element capture keep their current behavior

**Verification Criteria**:
- [ ] Full-page capture routes through the new extraction step
- [ ] Page captures store extracted body text and source URL
- [ ] Extraction metadata records selector-rule usage or readability fallback
- [ ] Existing selection and element capture tests still pass

**Exit Criteria**:
- A normal browser-captured page can be cleaned through the new extraction path.

---

## Phase 3: Authenticated Fetch and Cookie Integration
**Type:** Integration
**Estimated:** 3-4 hours
**Files:** `internal/server/ws/cookie_bridge.go`, `internal/pipeline/steps/client_bridge.go`, `internal/pipeline/builtins/web_authenticated.go`, `internal/pipeline/steps/article_scraper.go`

**Tasks**:
- [ ] Add conversion from cached browser cookies into a real `http.CookieJar`
- [ ] Provide a lookup path from URL domain to cached cookies
- [ ] Add a `web.authenticated` pipeline variant that fetches via `ClientBridge` with cookies when present
- [ ] Record auth method and fallback decisions in metadata
- [ ] Keep unauthenticated behavior as fallback when no cookie exists

**Verification Criteria**:
- [ ] Requests for matching domains send cookies
- [ ] Requests without cached cookies still proceed without auth
- [ ] Metadata records `auth_method`, `auth_fallback`, and related details
- [ ] Authenticated extraction still honors selector rules before readability fallback

**Exit Criteria**:
- The extractor can operate on authenticated pages using the existing browser cookie bridge.

---

## Phase 4: Feed Item Enrichment
**Type:** Integration
**Estimated:** 4-5 hours
**Files:** `internal/pipeline/steps/feed_item_fetcher.go`, `internal/pipeline/builtins/feed_sync.go`, feed-related tests

**Tasks**:
- [ ] Add a post-parse step that inspects feed items and chooses candidates for article fetch
- [ ] Fetch `item.link` when the feed body is empty, too short, or clearly excerpt-only
- [ ] Reuse the shared extractor to obtain full article content
- [ ] Preserve original feed fields alongside enriched article content
- [ ] Keep GUID-based deduplication behavior unchanged

**Verification Criteria**:
- [ ] Feed items with rich `content:encoded` can skip enrichment
- [ ] Feed items with poor summaries fetch and store fuller content
- [ ] Same feed item is not re-enriched repeatedly after deduplication
- [ ] Extraction failures degrade gracefully to original feed content

**Exit Criteria**:
- `feed.sync` can produce materially better content for excerpt-only feeds without regressing deduplication.

---

## Phase 5: Config, Documentation, and Operational Hardening
**Type:** Testing / Documentation
**Estimated:** 2-3 hours
**Files:** config, docs, tests

**Tasks**:
- [ ] Add config surface for scraper rules and extractor behavior
- [ ] Add docs for rule authoring and troubleshooting failed extraction
- [ ] Add test fixtures for a few real-world domains from the initial ruleset
- [ ] Add metrics/log fields for rule hits, readability fallbacks, auth usage, and extraction failures
- [ ] Document how to update the ruleset safely

**Verification Criteria**:
- [ ] Rules can be changed without recompiling the application
- [ ] Logs clearly show which rule or fallback path was used
- [ ] Docs describe rule syntax, limitations, and debugging workflow

**Exit Criteria**:
- The feature is operable, debuggable, and documented for future rule maintenance.

---

## Recommended Rollout Order

1. Phase 1 first, in isolation
2. Phase 2 next, to improve browser page capture immediately
3. Phase 3 after that, to make authenticated capture real
4. Phase 4 last, because feed enrichment is the most behaviorally invasive part
5. Phase 5 before release

This order gives useful value early while containing risk.

---

## Key Decisions

### Decision 1: External rules file over hardcoded Go map

Use external config for the ruleset. `ctxt` needs to evolve faster than a compiled-in map allows, and selector maintenance is operational work, not core logic.

### Decision 2: Shared extractor package

Do not implement one extractor for web capture and another for feeds. The extraction policy should live in one package and be reused.

### Decision 3: Selector-first, readability-second

Follow the Miniflux strategy. Domain-specific selectors are high precision when maintained well. Readability remains the safety net.

### Decision 4: Graceful degradation

If a selector rule breaks or auth cookies are missing, `ctxt` should still capture usable content rather than fail hard whenever possible.

---

## Risks

- CSS selectors are brittle and require maintenance as sites change
- Feed enrichment can increase network usage and sync latency
- Cookie-derived authenticated fetches may encounter anti-bot defenses
- Some sites provide poor HTML even after authenticated fetch, reducing the value of selector rules

---

## Mitigations

- Keep rules external and easy to patch
- Record selector hit/fallback metrics to find broken rules quickly
- Make feed enrichment conditional instead of mandatory
- Cap fetch timeouts and body sizes
- Preserve original feed content when extraction fails

---

## Testing Strategy

### Unit tests

- Rule lookup and normalization
- Selector extraction
- Base URL extraction
- Cookie cache to cookie jar conversion
- Feed item enrichment decision logic

### Integration tests

- Full-page capture on a mocked HTML page with matching selector
- Full-page capture with readability fallback
- Authenticated fetch with synced cookies
- Feed sync with excerpt-only item upgraded to enriched article content

### Regression tests

- Existing feed sync behavior remains stable for feeds with good embedded content
- Existing selection and element capture flows remain unchanged

---

## Exit Criteria

- `ctxt` has a real article extraction path in runtime code
- Full-page capture uses selector rules plus readability fallback
- Authenticated web fetch reuses browser cookies when available
- Feed sync can optionally enrich excerpt-only items from their linked article pages
- Rule maintenance is configuration-driven and documented

---

## First Implementation Slice

If this work should be split into the smallest useful slice, start with:

1. Phase 1 extraction core
2. Phase 2 `web.page` integration

That yields immediate user value without changing feed-sync semantics yet.
