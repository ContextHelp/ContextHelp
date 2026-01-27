# ADR-034 – Registry Packaging: Index Sync and JIT Resolution

> **Status:** Accepted
> **Date:** 2026-01-27
> **Author:** @jadb
> **Applies to:** dPKMS

---

## Context

Paid and licensed registries often cannot permit bulk replication of full
content. At the same time, subscribers need retrieval lift (entities, taxonomy,
edges, safe abstracts) without requiring constant remote queries.

---

## Decision

Registries support a default distribution model:

- **Index/schema sync (thin sync):** replicate allowed, low-leak artifacts
- **JIT resolution:** fetch full content on demand, gated by policy/entitlements

Registries declare a copy constraint:

- `localCopy: "none" | "index" | "content"`

Behavior:

- `none`: no sync; multi-source retrieval only
- `index`: index/schema sync allowed; full content must be JIT
- `content`: full sync allowed

## Alternatives considered

- **Full sync only (rejected):** incompatible with licensed/paid registries and
  increases leak risk.
- **Remote-only always (rejected):** higher latency and worse offline/UX; index
  sync provides retrieval lift cheaply.

---

## Consequences

### Positive

- Supports paid registries without forcing DRM
- Makes scraping economically controllable (metering + quotas on JIT)

### Negative

- Requires careful schema/versioning for index artifacts
- Requires receipt semantics for high-leak responses (export/JIT)

---

## Related

- `docs/dpkms/registry-protocol.md`
- `docs/dpkms/registry-syncing-and-retrieval.md`
- `docs/registries/publishing.md`
