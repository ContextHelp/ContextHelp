# US-0114: Configure Domain Scraper Rules

**System Types:** ctxt, dpkms (self-hosted)
**Personas:** [Platform Integrators](../../personas/platform-integrators.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As a platform integrator, I want to define and update domain-specific scraper rules so article extraction works reliably on problematic sites without recompiling the application.

---

## Context

Generic readability works well for many pages, but some sites expose article bodies in unstable or nonstandard markup. The result is thin captures, missing text, or noisy page chrome in both browser capture and feed-linked article enrichment. A config-driven rule layer lets operators patch these domains quickly with CSS selectors while keeping readability as the fallback path.

This workflow is operational rather than user-facing. Maintainers and integrators need a safe way to add, validate, ship, and observe selector rules across `web.page`, `web.authenticated`, and feed enrichment so extraction quality improves without forking code for each site.

---

## Acceptance Criteria

- [ ] Operator can define exact-domain scraper rules in configuration using `domain -> CSS selector` mapping
- [ ] Domain matching normalizes `www.` so the same rule applies to `example.com` and `www.example.com`
- [ ] The configured rules are reused by browser page capture, authenticated web fetch, and feed-linked article enrichment
- [ ] When a rule matches and extraction succeeds, object metadata records `scraper_strategy=selector`, matched rule domain, and selector used
- [ ] When selector extraction returns empty output or fails, the system falls back to readability and records the fallback in metadata
- [ ] Invalid rule configuration fails with clear validation errors at startup or reload time
- [ ] Operators can update rules without recompiling the application
- [ ] Logs or metrics expose selector hits, readability fallbacks, and extraction failures by domain

---

## Implementation Notes

### Example config

```yaml
scraper_rules:
  arstechnica.com: "div.post-content"
  github.com: "article.entry-content"
  theverge.com: "h2.inline:nth-child(2),h2.duet--article--dangerously-set-cms-markup,figure.w-full,div.duet--article--article-body-component"
```

### Runtime behavior

1. Normalize the request domain
2. Resolve the configured selector rule for that domain
3. If the effective URL remains same-site after redirects, try selector extraction first
4. If selector extraction is empty or fails, fall back to readability
5. Record the extraction path in metadata and logs

### Affected pipelines

- `web.page`
- `web.authenticated`
- feed item linked-article enrichment within `feed.sync`

---

## E2E Test Checklist

- [ ] Add a scraper rule for a mocked domain with awkward HTML structure
- [ ] Capture a page from that domain and confirm selector extraction is used
- [ ] Remove the selector or force it to return empty output and confirm readability fallback still succeeds
- [ ] Sync a feed whose linked article uses the same mocked domain and confirm the rule is reused there
- [ ] Break the config intentionally and confirm startup or reload returns a clear validation error
- [ ] Inspect logs or metrics and confirm rule hits and fallbacks are visible by domain

---

## Related Stories

- [US-0207](../capture/US-0207-web-tab-capture.md) — Web Tab Capture
- [US-0209](../capture/US-0209-authenticated-web-fetch.md) — Authenticated Web Fetch
- [US-0007](../ingestion/US-0007-feed-ingestion-and-sync.md) — Feed Ingestion and Sync

---

## E2E Tests

- planned: `test/integration/us0114_scraper_rules_test.go::TestScraperRules_AddRule`
- planned: `test/integration/us0114_scraper_rules_test.go::TestScraperRules_RuleAppliedOnCapture`
- planned: `test/integration/us0114_scraper_rules_test.go::TestScraperRules_FallsBackToReadability`
- planned: `test/integration/us0114_scraper_rules_test.go::TestScraperRules_ListAndRemove`
