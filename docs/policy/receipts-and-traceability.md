# Receipts and Traceability

Receipts make paid distribution enforceable and debuggable without DRM.

## What receipts are for

- Prove what was delivered (and when)
- Support audits, refunds, and abuse investigation
- Enable deterrence via subscriber-specific watermark IDs

## Where receipts apply

- Registry JIT pulls (full content/chunks)
- Derived outputs (composition/transforms)
- Exports (highest-leak surface)

## Constraints

- Receipts should not mutate canonical content
- Receipts should be verifiable (signed) when trust is enabled

See `docs/decisions/ADR-032-entitlements-metering-as-policy.md`.
