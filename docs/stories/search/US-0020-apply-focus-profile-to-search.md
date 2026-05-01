# US-0020: Apply Focus Profile to Search

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to apply a focus profile to my searches so that results are filtered and ranked through the lens of my current role or project context.

---

## Context

The same query ("authentication decisions") means different things to a security engineer versus a backend engineer. Focus profiles encode role- or project-specific boosts and exclusions: which entities matter, which tags are relevant, which object types to prioritize. Applying a profile shifts ranking server-side — it is not a post-processing step on the client. This ensures that the same query with different profiles produces reliably different, contextually appropriate result sets. Profiles are stored in the server; the client passes only the profile name.

---

## Acceptance Criteria

- [ ] User can apply a profile: `ctxt find "query" --profile engineering`
- [ ] Without `--profile`, server returns unfiltered results
- [ ] Profile filter and boost are applied server-side, not client-side; `rank.explain` includes `profile_boost` contribution
- [ ] Results with `--profile engineering` differ from results without profile on the same query
- [ ] Switching profiles (`--profile security` vs. `--profile engineering`) yields different result ordering for the same query
- [ ] Profile-excluded objects are absent from or ranked lower in filtered results
- [ ] Server records the profile used in the search history entry
- [ ] Unknown profile name returns 400 with a profile-not-found error
- [ ] Same query + profile via CLI, REST (`?profile=engineering`), and gRPC returns identical filtered result sets

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload

- [ ] `ctxt find "query" --profile engineering` sends `profile=engineering` in request payload;
      server receives the field
- [ ] `ctxt find "query"` without `--profile` sends no profile field (or `profile=null`); server
      applies no profile filter
- [ ] `ctxt find "query" --profile engineering --limit 10` sends both `profile` and `limit` in
      payload; server receives both

### Server-Side Receipt and Storage

- [ ] Server receives `profile` param; result relevance scores differ from no-profile baseline
      (boost/filter applied server-side, not client-side)
- [ ] Server applies profile's entity/tag boosts to rank scores; `rank.explain` in response
      includes `profile_boost` contribution
- [ ] Server applies profile's exclusion filters; objects outside profile scope deprioritized
      or absent
- [ ] Server records `profile` used in search history entry (verifiable via history endpoint
      or DB row)

### Flags Coverage

- [ ] `--profile <name>` — name present in payload; server returns profile-filtered result set
- [ ] `--limit <n>` — present in payload; result count ≤ n
- [ ] Unknown profile name → server returns 400 with profile-not-found error
- [ ] No `--profile` flag → server returns unfiltered results; no profile error

### Profile Filter Correctness

- [ ] Results with `--profile engineering` differ from results without profile on same query
- [ ] Profile-relevant objects rank higher with profile than without
- [ ] Profile-excluded objects absent from or ranked lower in filtered results
- [ ] Switching profiles (`--profile security` vs. `--profile engineering`) yields different
      result ordering for the same query

### Error Handling

- [ ] Profile config missing or corrupt → server returns 400 with descriptive error
- [ ] Profile references non-existent tags/entities → server returns results with warning

### Interface Parity

- [ ] Same query + profile via CLI, REST (`?profile=engineering`), and gRPC → identical
      filtered result sets

---

## Related Stories

- [US-0016](./US-0016-natural-language-search.md) — Natural Language Search (`--profile` flag supported on all search commands)
- [US-0017](./US-0017-structured-rsql-query.md) — Structured RSQL Query (profile combines with RSQL)
- [US-0019](./US-0019-federated-registry-search.md) — Federated Registry Search (profile applied after federation merge)
- [US-0021](./US-0021-search-with-result-explanation.md) — Search with Result Explanation (`rank.explain.profile_boost` populated here)
- [US-0030](../admin/US-0030-set-up-focus-profiles.md) — Set Up Focus Profiles (admin story that creates profiles)
- [US-0053](./US-0053-cross-profile-search-aggregation.md) — Cross-Profile Search Aggregation (multi-profile variant)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)

---

## E2E Tests

- `test/integration/us0020_focus_profile_test.go::TestUS0020_FocusProfileRestrictsSearchScope`
- `test/integration/us0020_focus_profile_test.go::TestUS0020_FocusProfileAllObjectsWhenNoProfile`
- `test/integration/us0020_focus_profile_test.go::TestUS0020_FocusProfileViaHTTPSearchQuery`
- `test/integration/us0020_focus_profile_test.go::TestUS0020_FocusProfileNoMatchReturnsEmpty`
