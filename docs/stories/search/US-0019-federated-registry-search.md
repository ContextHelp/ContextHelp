# US-0019: Federated Registry Search

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a user, I want to query multiple remote knowledge registries in a single search so that I can find relevant knowledge regardless of which registry it lives in.

---

## Context

Knowledge is often distributed across multiple registries: a team registry, an organization registry, a public shared registry. Querying each separately is tedious and loses cross-registry ranking signals. Federated search fans a query out to multiple named registries (or all configured registries), merges the results via RRF, and returns a unified ranked list with each result annotated with its source registry. Results are cached server-side so repeated federated queries stay within local latency.

---

## Acceptance Criteria

- [ ] User can specify registries: `ctxt find "query" --registries remote-a,remote-b`
- [ ] User can fan out to all configured registries: `ctxt find "query" --federated`
- [ ] Without a registry flag, search queries only the local index
- [ ] Remote registry results are merged with local results via RRF; each result includes `source.registry`
- [ ] Duplicate objects (same ID across registries) are collapsed to a single result with merged score
- [ ] Profile filter is applied to merged results (not per-registry)
- [ ] If one remote registry is unreachable, local and remaining registry results are still returned; the failed registry is noted in response metadata
- [ ] Remote registry responses are cached server-side; second identical query returns within local latency (<2s)
- [ ] Unknown registry name returns 400 or a per-registry error in response metadata
- [ ] Local-only search completes in <2s (P99); federated search completes in <5s (P99) when all registries are reachable

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload

- [ ] `ctxt find "query" --registries remote-a,remote-b` sends `registries=["remote-a","remote-b"]`
      in request payload; server receives both registry names
- [ ] `ctxt find "query" --federated` sends `federated=true` (or all-registries signal) in payload;
      server receives and fans out to all configured registries
- [ ] `ctxt find "query"` without registry flag sends only to local index (server does not fan out)
- [ ] `ctxt find "query" --limit 20` sends `limit=20` in payload; server distributes limit across
      registries and returns ≤ 20 merged results

### Server-Side Receipt and Storage

- [ ] Server receives `registries` list and dispatches sub-queries to each named registry
- [ ] Server receives `federated=true` and dispatches to all configured registries
- [ ] Server merges results from local + remote registries via RRF; response `sources` field
      lists contributing registries per result
- [ ] Remote registry responses cached server-side; second identical query returns within local
      latency (<2s)
- [ ] Server records federated query in search history with registry list (verifiable via DB row)

### Flags Coverage

- [ ] `--registries <list>` — registry names present in payload; server fans out only to listed
      registries
- [ ] `--federated` — present in payload; server fans out to all configured registries
- [ ] `--limit <n>` — present in payload; merged result count ≤ n
- [ ] Unknown registry name → server returns 400 or per-registry error in response metadata

### Federation Correctness

- [ ] Results from remote registries include `source.registry` field identifying origin
- [ ] Results from local and remote registries are merged and ranked together (not appended)
- [ ] Duplicate objects (same ID across registries) collapsed to single result with merged score
- [ ] Profile filter applied to merged results (not per-registry)

### Error Handling

- [ ] One remote registry unreachable → local + remaining registry results still returned;
      response metadata notes failed registry
- [ ] All remote registries unreachable → local results returned with warning
- [ ] Registry timeout (>5s) → registry skipped; timeout noted in response metadata

### Latency

- [ ] Local-only search <2s (P99)
- [ ] With federated registries <5s (P99) when all reachable

### Interface Parity

- [ ] Same federated query via CLI, REST, and gRPC → identical merged result sets

---

## Related Stories

- [US-0016](./US-0016-natural-language-search.md) — Natural Language Search (NLQ queries can be federated)
- [US-0018](./US-0018-multi-strategy-search-execution.md) — Multi-Strategy Search Execution (RRF merge used across registries)
- [US-0020](./US-0020-apply-focus-profile-to-search.md) — Apply Focus Profile to Search (profile applied after federation merge)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)

---

## E2E Tests

- `test/integration/us0019_federated_search_test.go::TestUS0019_FederatedSearchFansOutToRegistries`
- `test/integration/us0019_federated_search_test.go::TestUS0019_FederatedSearchOneRegistryDown`
- `test/integration/us0019_federated_search_test.go::TestUS0019_FederatedSearchNoRegistries`
- `test/integration/us0019_federated_search_test.go::TestUS0019_FederatedSearchDeduplicatesSameID`
