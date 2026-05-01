# US-0055: Search History and Recommendations

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to view my past queries and receive proactive search recommendations so that I can revisit useful searches and discover related knowledge I haven't thought to look for.

---

## Context

Users often repeat searches or forget what they searched last session. Every search query is persisted server-side in a history table with its profile, strategies used, result count, and timestamp. Users can list and filter their history and clear it. Beyond history replay, the server can derive recommendations from patterns in the history — frequent entities, recent queries with high result counts, high-click objects — and surface suggested searches before the user types. Recommendations are personalized: two users with different histories get different suggestions.

---

## Acceptance Criteria

- [ ] Every search query is persisted to the history table with `query`, `profile`, `strategies_used`, `result_count`, and `timestamp`
- [ ] Repeated identical queries create multiple history entries (not deduplicated)
- [ ] User can list history: `ctxt search history`; server returns entries ordered by timestamp descending
- [ ] User can limit history entries: `--limit 20`
- [ ] User can filter history by profile: `--profile engineering`
- [ ] User can clear history: `ctxt search history clear`; subsequent list returns empty
- [ ] User can get recommendations: `ctxt search recommend`; server derives suggestions from history
- [ ] Each recommendation includes `query` (suggested search text) and `reason` (why suggested)
- [ ] With no history, server returns empty recommendations list (not 500)
- [ ] Same history and recommendation requests via CLI and REST return identical entries and suggestions

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload (History)

- [ ] `ctxt search history` sends `GET /search/history` (or equivalent); server receives
      request and returns stored history
- [ ] `ctxt search history --limit 20` sends `limit=20` in request; server receives it and
      returns ≤ 20 entries
- [ ] `ctxt search history --profile engineering` sends `profile=engineering` in request;
      server filters history to queries run with that profile
- [ ] `ctxt find "query"` (any search) triggers server to persist a history record containing
      `query`, `profile`, `strategies_used`, `result_count`, `timestamp`

### Server-Side Receipt and Storage

- [ ] Server persists each search query to history table with `query`, `profile`,
      `strategies_used`, `result_count`, `timestamp`; row verifiable via DB query
- [ ] Server receives history list request with `limit`; returns ≤ limit entries ordered by
      `timestamp` descending
- [ ] Server receives `profile` filter on history request; returns only history entries for
      that profile
- [ ] Repeated identical query creates multiple history entries (not deduplicated)

### CLI → Server Payload (Recommendations)

- [ ] `ctxt search recommend` sends `GET /search/recommendations` (or equivalent); server
      receives request and returns recommendations based on history
- [ ] `ctxt search recommend --limit 5` sends `limit=5`; server returns ≤ 5 recommendations
- [ ] Recommendations endpoint receives no query — server derives suggestions from history

### Server-Side Recommendations

- [ ] Server generates recommendations based on search history (recent queries, frequent
      entities, high-click results)
- [ ] Each recommendation in response includes `query` (suggested search text) and
      `reason` (why suggested)
- [ ] Recommendations differ between users with different histories (not static)
- [ ] Sufficient history required; with no history, server returns empty list (not 500)

### Flags Coverage

- [ ] `--limit <n>` on history — present in payload; entry count ≤ n
- [ ] `--profile <name>` on history — present in payload; server filters history to profile
- [ ] `--limit <n>` on recommend — present in payload; recommendation count ≤ n
- [ ] `ctxt search history clear` sends delete request; history table cleared; subsequent
      list returns empty

### Error Handling

- [ ] History table empty → `GET /search/history` returns `{"entries": [], "total": 0}` with 200
- [ ] Recommendation engine unavailable → graceful degradation; returns empty list with info
      message, not 500

### Interface Parity

- [ ] History and recommendations via CLI and REST → identical entries and recommendations

---

## Related Stories

- [US-0016](./US-0016-natural-language-search.md) — Natural Language Search (every search creates a history entry)
- [US-0017](./US-0017-structured-rsql-query.md) — Structured RSQL Query (RSQL searches also recorded in history)
- [US-0020](./US-0020-apply-focus-profile-to-search.md) — Apply Focus Profile to Search (profile recorded per history entry)
- [US-0054](./US-0054-saved-search-and-alerts.md) — Saved Search and Alerts (complementary: persisted named queries vs. anonymous history)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)

---

## E2E Tests

- `test/integration/us0055_search_history_test.go::TestUS0055_ResurfacingQueueActsAsRecommendation`
- `test/integration/us0055_search_history_test.go::TestUS0055_RepeatedSearchReturnsConsistentResults`
- `test/integration/us0055_search_history_test.go::TestUS0055_DismissingResurfacingEntryHidesItFromUnseenList`
- `test/integration/us0055_search_history_test.go::TestUS0055_EmptyResurfacingQueueReturnsNoError`
