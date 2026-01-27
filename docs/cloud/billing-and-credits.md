# Billing and Credits (Cloud)

context.help cloud monetizes via:

- Subscriptions (access to paid registries/extensions)
- Credits (metered access to expensive operations)

## Design constraint

Billing logic must not be a hard dependency of OSS nodes.

Nodes should:

- enforce policy at request boundaries
- call a pluggable entitlement provider
- emit metering events

Cloud should:

- translate entitlements + usage into invoices/credits
- handle payouts and marketplace economics

See `docs/policy/entitlements-and-metering.md` and `docs/decisions/ADR-032-entitlements-metering-as-policy.md`.
