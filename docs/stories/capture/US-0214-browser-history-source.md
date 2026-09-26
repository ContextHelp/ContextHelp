---
status: partial
adr: ADR-066
task: T-0502
---

# US-0214: Browser-History Ambient Source

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## User Goal

As a researcher, I want my browser history (URL + title + visit-time) ingested automatically into my knowledge graph so that "what was that article I read three weeks ago?" becomes searchable without me having to remember to bookmark or copy-paste.

---

## Context

ctxt already has 4 one-shot importers for static bookmark exports (`ctxt import chrome|firefox|safari|edge`). They run when the user remembers — which is rarely. The browser-history source closes the gap by polling the browser's SQLite history files periodically (read-only; copy-to-temp to avoid lock contention) and emitting RawEvents for new visits.

This is a sibling ambient source under [ADR-066](../../decisions/ADR-066-ambient-capture-substrate.md). Reuses the parsing helpers from the existing importers (`internal/importer/chrome|firefox|safari|edge`) so this story is mostly substrate plumbing, not new browser parsers.

A future enhancement (browser extension for live tab events) is out of v1 scope; this story uses the SQLite-history poll pattern only.

---

## Acceptance Criteria

- [ ] Source registered as `browserhistory` in the ambient runner
- [ ] Polls browser SQLite history files at configurable interval (default 5 min)
- [ ] Read-only access via copy-to-temp (avoids lock contention with running browser)
- [ ] Detects new visits since last poll (using browser-specific visit-time column)
- [ ] Emits RawEvent per new visit with: URL, page title, visit timestamp, browser identifier
- [ ] Routes to `url.generic` pipeline by default; `url.repo` for github.com/gitlab.com/bitbucket.org URLs
- [ ] Fingerprint = SHA-256 of `(url + visit_timestamp_minute_bucket)`; substrate dedup catches re-emissions across polls
- [ ] Per-browser config (Chrome, Firefox, Safari, Edge, Brave, Arc):

```yaml
ambient:
  sources:
    browserhistory:
      enabled: true
      poll_interval: 5m
      browsers:
        - chrome
        - firefox
        - safari
capture:
  url_filter:          # shared by every browser capture path
    deny:
      - "*://*.bank.example.com/*"
    # localhost, loopback, file: and browser-internal pages are always denied
```

- [ ] kit/policy CEL veto on `ctxt.ambient.event.captured` for `source=browserhistory` can drop sensitive URLs (sensitive bank, healthcare, internal domains) before enqueue
- [ ] Bus events emit per ADR-066 taxonomy
- [ ] Last-poll-timestamp persisted per browser in buffer state (so daemon restart doesn't replay old visits)
- [ ] Bookmark-shaped URLs (search engines: google.com/search, duckduckgo.com/?q=) tagged with `subtype=search-query` so search-engine visits are distinguishable from content visits
- [ ] Cross-platform: macOS / Linux / Windows (browser SQLite paths differ per OS)

### Progress

Scope so far: Chromium family only (Chrome, Brave, Edge, Arc, Chromium, Vivaldi), via the one-shot `ctxt capture history` command and the Chromium History reader. User doc: [capture-history.md](../../capture-history.md). No box is ticked until the command lands and its e2e passes.

| Criterion | Coverage |
|---|---|
| Registered as `browserhistory` in the ambient runner | Open: runner registration |
| Poll at configurable interval (default 5 min) | `ctxt capture history` on a launchd schedule (5 min) as a stopgap; in-daemon polling open until runner registration |
| Read-only copy-to-temp | Chromium History reader |
| Detect new visits since last poll | `ctxt capture history` incremental mode (saved position) |
| RawEvent per visit (URL, title, visit time, browser) | Chromium History reader; one entry per real navigation, subframes dropped, redirects collapsed |
| Route `url.generic` / `url.repo` | Not in the command spec; verify when it lands |
| Fingerprint dedup across polls | Not in the command spec; verify when it lands |
| Per-browser config | Chromium family via `--browser` / `--browser-profile` plus per-browser and per-profile `capture.url_filter`; Firefox and Safari open |
| kit/policy CEL veto | Open |
| Bus events per ADR-066 | Open |
| Position persisted per browser across restarts | `ctxt capture history`: per browser + profile in `browserhistory.state`; backfills never touch it |
| `subtype=search-query` tagging | Not in the command spec; verify when it lands |
| Cross-platform paths | Chromium profile resolver covers macOS / Linux / Windows; scheduling is macOS (launchd) only; Firefox and Safari open |

---

## Implementation Notes

### Architecture

```
internal/ambient/browserhistory/browserhistory.go
  implements ambient.AmbientSource
  -> Start: register tickers per browser
  -> On tick (per browser):
       - Determine SQLite history file path (per OS, per browser)
       - Copy file to OS temp dir (to avoid lock contention)
       - Open read-only; SELECT WHERE visit_time > last_poll_timestamp
       - Emit RawEvent per row
       - Persist new last_poll_timestamp
```

Reuses the existing parsing helpers under `internal/importer/{chrome,firefox,safari,edge}/` — those packages already understand the per-browser SQLite schema. This story extracts the visit-querying logic into a poll-friendly variant.

### Per-OS SQLite paths (reference)

Documented per-browser paths come from the existing importer code; this story consumes them.

### Routing

```
URL matches github.com|gitlab.com|bitbucket.org → url.repo
Otherwise → url.generic
```

Both pipelines already exist (`internal/pipeline/builtins/url_*.go`).

### Privacy

- URL filter denylist runs in the source before emission (cheap, doesn't need policy round-trip); rules and syntax in [ambient.md](../../ambient.md#keep-sites-out-of-browser-capture)
- kit/policy CEL is the structural enforcement (can ALSO check page title patterns)
- Last-poll-timestamp persisted in `$XDG_STATE_HOME/ctxt/ambient/browserhistory.state`

### CLI

```bash
ctxt capture --ambient tail --source browserhistory
ctxt capture --ambient sources                       # shows last-poll per browser
```

---

## E2E Checklist

- [ ] `ctxt capture --ambient` starts with browserhistory source enabled
- [ ] Visit 5 URLs in Chrome; wait poll_interval; verify 5 KnowledgeObjects via `url.generic`
- [ ] Visit a github.com URL; verify routed to `url.repo`
- [ ] Configure denylist for `*.bank.example.com`; visit such a URL; verify dropped before enqueue
- [ ] Stop daemon for 1 hour; visit 10 URLs; restart daemon; verify all 10 picked up exactly once
- [ ] Configure Firefox alongside Chrome; verify both browsers polled; visits don't double-count
- [ ] kit/policy CEL veto on a URL pattern; verify `ctxt.ambient.event.filtered` emits
- [ ] Restart browser while daemon polling; verify no errors (copy-to-temp avoids lock)
- [ ] Search-engine URL (google.com/search?q=...); verify `subtype=search-query`
- [ ] Bus events fire per ADR-066 taxonomy

---

## Related Stories

- [US-0207](US-0207-web-tab-capture.md) — Web tab capture via browser extension (sibling pattern; future)
- [US-0211](US-0211-passive-clipboard-watcher.md) — Sibling clipboard ambient source
- [US-0213](US-0213-file-watch-source.md) — Sibling file-watch ambient source
- [US-0216](US-0216-work-sessions.md) — Sessions group browser visits with surrounding work
- [US-0001](../ingestion/US-0002-url-capture-and-extraction.md) — One-shot URL capture (this is the daemonized variant)
- Existing importers: `ctxt import chrome|firefox|safari|edge` (one-shot bookmark imports; complementary, not superseded)

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-066 Phase 3 (browser-history source) — see `tlc track show ambient-capture`, task **T-0502**.

---

## E2E Tests

- planned: `test/integration/us0214_browserhistory_test.go::TestBrowserHistory_PollsChromeSafely`
- planned: `test/integration/us0214_browserhistory_test.go::TestBrowserHistory_DetectsNewVisits`
- planned: `test/integration/us0214_browserhistory_test.go::TestBrowserHistory_RestartResumesFromBookmark`
- planned: `test/integration/us0214_browserhistory_test.go::TestBrowserHistory_DenylistFiltersURLs`
- planned: `test/integration/us0214_browserhistory_test.go::TestBrowserHistory_RoutesRepoURLs`
- planned: `test/integration/us0214_browserhistory_test.go::TestBrowserHistory_HandlesBrowserLockContention`
- planned: `test/integration/us0214_browserhistory_test.go::TestBrowserHistory_MultiBrowserConcurrent`
