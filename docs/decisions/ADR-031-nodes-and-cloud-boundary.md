# ADR-031 – Nodes and context.help cloud Boundary

> **Status:** Accepted
> **Date:** 2026-01-27
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt

---

## Context

ContextHelp is self-hostable, but many users (and most teams) prefer a hosted
offering. We want **context.help cloud** to provide enterprise/team operations
(multi-tenant admin UI, SSO/SCIM, billing, marketplace) without moving core
enforcement into the cloud.

Key constraints:

- Nodes must remain useful offline and without any cloud dependency
- Policy enforcement must happen at the node boundary
- One org must be able to manage many nodes
- The cloud must be a client of stable node admin APIs, not a forked runtime

---

## Decision

We standardize a boundary between:

- **Node (OSS):** executes pipelines, stores data, enforces policy, speaks
  registry protocols, emits audit and metering
- **context.help cloud (non-OSS):** org lifecycle, multi-node admin UI,
  SSO/SCIM connectors, billing/credits, marketplace, and managed hosting

Nodes expose a minimal **Node Admin API** to support:

- node enrollment / attachment
- access management (RBAC)
- policy configuration (entitlements, quotas, export gating)
- audit and metering export

The cloud is treated as an admin client. It may host nodes, but it does not
replace node-local enforcement.

---

## Consequences

### Positive

- Clean licensing boundary: OSS enforcement + protocols, cloud UI/ops/commerce
- Self-host remains first-class
- Cloud can evolve independently as long as it stays within the admin API

### Negative

- Requires careful API versioning and capability discovery
- Cloud needs robust enrollment + trust management

---

## Related

- `docs/api/node-admin-api.md`
- `docs/cloud/README.md`

## Alternatives considered

- **Cloud-enforced policy (rejected):** makes self-hosting second-class and
  creates a vendor dependency for enforcement.
- **No node admin API (rejected):** forces the cloud to fork the node runtime or
  rely on ad-hoc mechanisms.
