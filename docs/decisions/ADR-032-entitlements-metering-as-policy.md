# ADR-032 – Entitlements and Metering as Policy Predicates

> **Status:** Accepted
> **Date:** 2026-01-27
> **Author:** @jadb
> **Applies to:** dPKMS

---

## Context

We want a registry and plugin ecosystem where publishers can monetize:

- paid registry subscriptions
- credit-metered access (JIT pulls, exports, derivations)
- per-object access labels

This must not require embedding payment logic into OSS nodes.

---

## Decision

Monetization is expressed as **policy checks** at request boundaries.

Nodes:

- enforce authorization using scopes/RBAC + policy rules
- consult a pluggable **EntitlementProvider** for subscription/credit decisions
- emit metering and audit events
- attach signed receipts (when trust is enabled) to high-leak responses

Cloud:

- implements a remote entitlement provider (billing/credits)
- converts metering events into invoices and credit balances

Priority order for gating:

1. subscription entitlement
2. credits/meters/quota
3. per-object access labels

## Alternatives considered

- **Embed billing in core (rejected):** breaks self-hostability and complicates
  OSS deployments.
- **DRM-style enforcement (rejected):** conflicts with sovereignty goals and is
  brittle; we prefer deterrence + receipts + quotas.

---

## Consequences

### Positive

- OSS stays self-hostable and cloud-optional
- Registry monetization works with thin sync + JIT pull
- Metering becomes a first-class anti-scrape lever

### Negative

- Requires a stable policy vocabulary (actions/resources/meters)
- Receipts and metering require careful privacy defaults

---

## Related

- `docs/policy/entitlements-and-metering.md`
- `docs/policy/receipts-and-traceability.md`
- `docs/decisions/ADR-034-registry-index-sync-and-jit-resolution.md`
