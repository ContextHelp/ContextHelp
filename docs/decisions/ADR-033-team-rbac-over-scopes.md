# ADR-033 – Team RBAC as an Additive Layer Over Scopes

> **Status:** Accepted
> **Date:** 2026-01-27
> **Author:** @jadb
> **Applies to:** dPKMS

---

## Context

ADR-023 chose a capability/scopes model and explicitly rejected coarse RBAC for
API tokens. Separately, teams need human-oriented access management (roles,
groups, memberships).

We want:

- a team-friendly RBAC layer in OSS
- no regression for fine-grained token scopes
- a single enforcement mechanism at runtime

---

## Decision

We add **team RBAC** as a convenience layer that compiles down to the existing
scope/policy enforcement.

- Tokens/agents continue to use explicit scopes
- Humans can be assigned roles via groups and bindings
- The authorization engine continues to enforce a unified action/resource model

## Alternatives considered

- **RBAC-only enforcement (rejected):** too coarse for agents and least-privilege
  token scoping.
- **Keep humans on raw scopes only (rejected):** poor UX for teams and cloud
  administration.

---

## Consequences

### Positive

- Teams get a familiar admin model without weakening least-privilege
- Cloud admin UI can manage RBAC via a stable node admin API

### Negative

- Adds additional schema and migration surface
- Requires clear docs to avoid role/scope confusion

---

## Related

- `docs/decisions/ADR-023-authentication-authorization-model.md`
- `docs/api/node-admin-api.md`
