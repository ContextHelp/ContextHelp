---
status: shipped
---

# US-0017: Structured RSQL Query

**System Types:** ctxt, dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As an agent or power user, I want to issue deterministic, structured queries using RSQL syntax so that I get reproducible, exactly-filtered result sets without NLQ ambiguity.

---

## Context

Agents and maintainers often need precise, repeatable queries: "all decisions created after 2024-01-01 tagged with auth." Natural language search introduces variability — the same intent can produce different results depending on AI model state. RSQL provides a deterministic, SQL-like filter language that maps directly to structured object fields (type, tags, createdAt, etc.), enabling agents to construct queries programmatically and humans to bookmark exact searches.

---

## Acceptance Criteria

- [ ] User can pass RSQL expressions via `--rsql` flag: `ctxt find --rsql "type==decision"`
- [ ] Server routes `query_mode=rsql` to the RSQL parser, not the NLQ normalizer
- [ ] RSQL expressions support equality (`==`), membership (`=in=()`), comparison (`=gt=`, `=lt=`), AND (`;`), and OR (`,`) operators
- [ ] Server returns only results matching the expression; no partial or approximate matches
- [ ] Pagination (`--limit`, `--offset`) works correctly with RSQL queries
- [ ] Profile filter (`--profile`) can be combined with RSQL queries
- [ ] Invalid RSQL expressions return 400 with a descriptive parse error; no partial results
- [ ] Same RSQL query via CLI, REST, and gRPC returns identical result sets

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload

- [ ] `ctxt find --rsql "type==decision"` sends `query_mode=rsql` and `q=type==decision` in
      request to server; server receives both fields
- [ ] `ctxt find --rsql "tags=in=(auth,security)"` sends full RSQL expression in `q` param;
      server receives unmodified expression
- [ ] `ctxt find --rsql "..." --limit 20` sends `limit=20` in request payload; server receives it
- [ ] `ctxt find --rsql "..." --offset 40` sends `offset=40` in request payload; server receives it
- [ ] `ctxt find --rsql "..." --profile ops` sends `profile=ops` in request payload; server
      receives and applies it

### Server-Side Receipt and Storage

- [ ] Server receives `query_mode=rsql` and routes to RSQL parser (not NLQ path)
- [ ] Server parses RSQL expression and applies deterministic filter to result set
- [ ] Server receives `limit` and `offset`; returned `results` array length ≤ limit, pagination
      cursor consistent with offset
- [ ] Server receives `profile`; result set differs from no-profile baseline
- [ ] Query stored in search history (verifiable via history endpoint or DB row)

### Flags Coverage

- [ ] `--rsql <expr>` — expression present in payload as `q`; `query_mode=rsql` also present
- [ ] `--limit <n>` — present in payload; result count ≤ n
- [ ] `--offset <n>` — present in payload; first result matches expected page position
- [ ] `--profile <name>` — present in payload; server applies filter
- [ ] Invalid RSQL expression → server returns 400 with parse error detail

### RSQL Correctness

- [ ] `type==decision` returns only decision objects
- [ ] `type=in=(decision,insight)` returns both types
- [ ] `tags=in=(auth,security)` returns objects tagged with at least one matching tag
- [ ] `createdAt=gt=2024-01-01` returns only objects created after the date
- [ ] Compound expression (`type==decision;createdAt=gt=2024-01-01`) returns intersection
- [ ] OR expression (`type==decision,type==insight`) returns union

### Error Handling

- [ ] Malformed RSQL (unclosed paren) → 400 with error; no partial results
- [ ] Unknown field name in RSQL → 400 with field-not-found error
- [ ] Empty result set returns `{"results": [], "total": 0}` with 200 status

### Interface Parity

- [ ] Same RSQL query via CLI, `GET /search?q=...&query_mode=rsql`, and gRPC → identical
      result sets

---

## Related Stories

- [US-0016](./US-0016-natural-language-search.md) — Natural Language Search (NLQ path; contrast with deterministic RSQL)
- [US-0018](./US-0018-multi-strategy-search-execution.md) — Multi-Strategy Search Execution (RSQL uses metadata strategy)
- [US-0020](./US-0020-apply-focus-profile-to-search.md) — Apply Focus Profile to Search (profile filter combines with RSQL)
- [US-0038](../agents/US-0038-agent-constructs-rsql-query.md) — Agent Constructs RSQL Query (agent-facing counterpart)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)

---

## E2E Tests

- `test/integration/us0017_rsql_query_test.go::TestUS0017_RSQLTypeFilter`
- `test/integration/us0017_rsql_query_test.go::TestUS0017_RSQLMultipleTypes`
- `test/integration/us0017_rsql_query_test.go::TestUS0017_RSQLInvalidExpressionReturnsError`
- `test/integration/us0017_rsql_query_test.go::TestUS0017_RSQLViaHTTPEndpoint`
- `test/integration/us0017_rsql_query_test.go::TestUS0017_RSQLProfileScopeRestrictsResults`
