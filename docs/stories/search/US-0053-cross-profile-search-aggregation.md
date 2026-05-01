# US-0053: Cross-Profile Search Aggregation

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker wearing multiple hats, I want to aggregate search results across several focus profiles in a single query so that I can see what is relevant from each perspective without running separate searches.

---

## Context

A staff engineer may operate across security, backend, and infrastructure profiles simultaneously. Running three separate searches and mentally merging the results is tedious. Cross-profile aggregation runs each profile's scoring independently on the same result set, then merges via RRF. Objects relevant to multiple profiles rank higher than those relevant to only one. Each result includes a `contributing_profiles` field listing which profiles surfaced it, letting the user see which context each result belongs to. Querying all profiles at once is also supported via `--all-profiles`.

---

## Acceptance Criteria

- [ ] User can aggregate across named profiles: `ctxt find "query" --profiles engineering,security`
- [ ] User can aggregate across all configured profiles: `ctxt find "query" --all-profiles`
- [ ] Each profile's boost/filter is applied independently; results are then merged via RRF
- [ ] Each result includes a `contributing_profiles` field listing which profiles surfaced it
- [ ] An object matched by multiple profiles ranks higher than one matched by a single profile
- [ ] Result ordering differs from any single-profile search on the same query
- [ ] Without profiles flag, search uses only the single active profile (not cross-profile)
- [ ] Unknown profile name returns 400 with a profile-not-found error (fail-fast)
- [ ] If one profile config is corrupt, remaining profiles still aggregate; error noted in response metadata
- [ ] Same cross-profile query via CLI, REST, and gRPC returns identical aggregated result sets

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload

- [ ] `ctxt find "query" --profiles engineering,security` sends `profiles=["engineering",
      "security"]` in request payload; server receives both profile names
- [ ] `ctxt find "query" --all-profiles` sends `profiles=all` (or equivalent) in payload;
      server aggregates across all configured profiles
- [ ] `ctxt find "query"` (no profiles flag) sends single active profile or no-profile context;
      not a cross-profile query
- [ ] `ctxt find "query" --profiles engineering,security --limit 20` sends both `profiles` and
      `limit` in payload; server receives both

### Server-Side Receipt and Storage

- [ ] Server receives `profiles` list; applies each profile's boost/filter to result scoring
      independently, then merges
- [ ] Server merges per-profile result sets via RRF; each result's `rank.explain` includes
      per-profile score contribution
- [ ] Server receives `limit`; merged result count ≤ limit
- [ ] Each result in response includes `contributing_profiles` field listing which profiles
      surfaced it

### Flags Coverage

- [ ] `--profiles <list>` — profile names in payload; server applies all listed profiles in
      merge
- [ ] `--all-profiles` — signal present in payload; server uses all configured profiles
- [ ] `--limit <n>` — present in payload; merged result count ≤ n
- [ ] Unknown profile name → server returns 400 with profile-not-found error (fail-fast)

### Aggregation Correctness

- [ ] Result appearing in multiple profiles ranked higher than result in only one profile
- [ ] Results exclusive to a single profile still appear, but ranked lower than shared results
- [ ] Result ordering differs from any single-profile search on same query (aggregation effect)
- [ ] `contributing_profiles` field correctly lists profiles that matched each result

### Error Handling

- [ ] One profile config corrupt/missing → remaining profiles still aggregate; error noted in
      response metadata
- [ ] All profiles yield empty results → `{"results": [], "total": 0}` with 200

### Interface Parity

- [ ] Same cross-profile query via CLI, REST, and gRPC → identical aggregated result sets

---

## Related Stories

- [US-0020](./US-0020-apply-focus-profile-to-search.md) — Apply Focus Profile to Search (single-profile variant; prerequisite)
- [US-0018](./US-0018-multi-strategy-search-execution.md) — Multi-Strategy Search Execution (RRF merge reused for multi-profile aggregation)
- [US-0030](../admin/US-0030-set-up-focus-profiles.md) — Set Up Focus Profiles (admin story that creates profiles)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)

---

## E2E Tests

- `test/integration/us0053_cross_profile_test.go::TestUS0053_GlobalSearchIncludesAllProfiles`
- `test/integration/us0053_cross_profile_test.go::TestUS0053_PerProfileSearchReturnsIsolatedResults`
- `test/integration/us0053_cross_profile_test.go::TestUS0053_AggregatedSearchViaHTTP`
