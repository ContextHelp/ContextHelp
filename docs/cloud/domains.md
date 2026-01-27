# Cloud Domains (context.help cloud)

This document lists the domains owned by **context.help cloud**.

These are **non-OSS** product domains that sit above self-hostable nodes.
Node-owned domains remain documented under `docs/dpkms/domains.md` and
`docs/ctxt/domains.md`.

**Last Updated:** 2026-01-27

---

## 1. Org & Directory Domain

**Responsibilities:**
- Organizations/tenants
- User directory, groups, and admin roles (cloud-side)
- Membership lifecycle driven by SSO/SCIM

**Related docs:**
- `docs/cloud/admin-access.md`

---

## 2. Node Fleet Domain

**Responsibilities:**
- Org -> nodes inventory (many nodes per org)
- Node status, versions, upgrades, and operational metadata

**Related docs:**
- `docs/cloud/README.md`
- `docs/cloud/node-enrollment.md`

---

## 3. Enrollment & Trust Domain

**Responsibilities:**
- Attaching/detaching nodes to an org
- Trust configuration distribution (issuer config, endpoints)
- Key rotation and revocation workflows

**Related docs:**
- `docs/cloud/node-enrollment.md`
- `docs/decisions/ADR-031-nodes-and-cloud-boundary.md`

---

## 4. Admin UX Domain

**Responsibilities:**
- Multi-tenant admin interface
- Node administration via Node Admin API (cloud as an admin client)
- Human-friendly access management UX (RBAC) and policy configuration UX

**Related docs:**
- `docs/api/node-admin-api.md`
- `docs/cloud/admin-access.md`

---

## 5. Billing & Credits Domain

**Responsibilities:**
- Subscription plans and entitlements
- Credit balances and invoicing
- Usage aggregation from metering events

**Related docs:**
- `docs/cloud/billing-and-credits.md`
- `docs/policy/entitlements-and-metering.md`

---

## 6. Marketplace Domain

**Responsibilities:**
- Publisher listings (registries, extensions)
- Discovery and distribution
- Trials, bundles, refunds, and payouts

**Related docs:**
- `docs/marketplace/README.md`
- `docs/registries/publishing.md`
