# context.help cloud

**context.help cloud** is the hosted offering for ContextHelp.

It exists to remove infrastructure work (deployment, upgrades, availability) and
to provide enterprise/team administration (org access management, SSO/SCIM,
billing), while keeping core node functionality self-hostable.

## What the cloud provides (non-OSS)

- Multi-tenant admin UI for orgs and nodes
- Identity lifecycle (SSO/SCIM) and team onboarding
- Billing/credits and marketplace for paid registries and extensions
- Managed node operations (hosting, upgrades, backups, monitoring)

## What stays in the node (OSS)

- Policy enforcement (permissions/RBAC, entitlements, quotas)
- Registry protocol (thin sync + just-in-time pulls)
- Audit + metering event emission
- Plugins runtime + permission sandboxing

## Related docs

- `docs/cloud/node-enrollment.md`
- `docs/cloud/admin-access.md`
- `docs/cloud/billing-and-credits.md`
- `docs/cloud/domains.md`
- `docs/api/node-admin-api.md`
- `docs/decisions/ADR-031-nodes-and-cloud-boundary.md`
