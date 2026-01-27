# Entitlements and Metering

This document defines the OSS-facing **policy seam** used to support paid
registries and extensions.

## Goals

- Express monetization as policy checks (not storage modes)
- Support subscription -> credits -> per-object access labels
- Keep nodes self-hostable without any cloud dependency

## Recommended model

1. Subscription entitlement gates access to a registry/feed
2. Credits meter expensive operations (JIT pulls, exports, derivations)
3. Per-object gating is expressed as access labels/classes, not bespoke ACLs

## Entitlement provider (conceptual)

- `CheckEntitlement(principal, action, resource, context)`
- `Consume(meter, amount, context)`

See `docs/decisions/ADR-032-entitlements-metering-as-policy.md`.
