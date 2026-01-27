# Node Admin API

The Node Admin API is the stable surface used by **context.help cloud** (and any
admin client) to manage a node.

This is intentionally minimal: the cloud provides the UI and org lifecycle;
the node remains the enforcement point.

## Required capabilities

- Node enrollment + capabilities discovery
- RBAC management (teams): users/groups/roles/bindings
- Policy configuration: entitlements, quotas, export gating
- Audit export: security-relevant actions
- Metering export: usage events for credits/billing

See `docs/decisions/ADR-031-nodes-and-cloud-boundary.md`.

---

## Base URL

The Node Admin API is served by the node HTTP server and is versioned under:

- `/admin/v1`

This is separate from the user-facing REST API.

## Authentication

Nodes MAY run in local-only mode. If the admin API is exposed beyond localhost,
it MUST require authentication.

Supported patterns (implementation-specific):

- `Authorization: Bearer <token>` (PAT or admin token)
- mTLS (optional)

## Conventions

- JSON only (`application/json`)
- Timestamps are ISO-8601 UTC
- IDs are opaque strings
- Errors follow the core REST error envelope (see `docs/api/api-rest.md`)

---

## Endpoints

### Capabilities and status

- `GET /admin/v1/capabilities`
  - Returns node version, supported features, and admin API version.

- `GET /admin/v1/status`
  - Returns health plus node metadata used by cloud fleet views.

Example `GET /admin/v1/capabilities` response:

```json
{
  "admin_api_version": "v1",
  "node_id": "node_01H...",
  "node_version": "0.1.0",
  "features": {
    "rbac": true,
    "entitlements": true,
    "metering": true,
    "audit": true,
    "registry_jit": true
  }
}
```

---

### Enrollment (cloud attachment)

Nodes attach to context.help cloud via an enrollment flow.

- `POST /admin/v1/enrollment/attach`
  - Input: cloud enrollment token + org identifier (or opaque attachment id)
  - Output: persisted attachment configuration (issuer, endpoints, org id)

- `POST /admin/v1/enrollment/detach`
  - Detaches the node from the cloud (does not delete local data).

- `POST /admin/v1/enrollment/rotate`
  - Rotates node identity / cloud trust credentials.

---

### RBAC (teams)

RBAC is an OSS feature used for team access management.

Objects:

- Users
- Groups
- Roles
- Bindings

Endpoints:

- `GET /admin/v1/rbac/users`
- `POST /admin/v1/rbac/users`
- `GET /admin/v1/rbac/groups`
- `POST /admin/v1/rbac/groups`
- `GET /admin/v1/rbac/roles`
- `POST /admin/v1/rbac/roles`
- `GET /admin/v1/rbac/bindings`
- `POST /admin/v1/rbac/bindings`
- `DELETE /admin/v1/rbac/bindings/{id}`

RBAC compiles down to the node's underlying scope/policy enforcement
(see `docs/decisions/ADR-033-team-rbac-over-scopes.md`).

---

### Policy configuration

Policy controls are enforced at request boundaries (registry access, exports,
JIT pulls, plugins).

- `GET /admin/v1/policy`
- `PUT /admin/v1/policy`

Policy includes (at minimum):

- entitlement requirements (subscription gates)
- meters and quotas (credits, rate limits)
- export gating rules
- registry-specific overrides

---

### Audit export

- `GET /admin/v1/audit/events?since=<iso>&limit=<n>`
  - Returns an append-only stream of security-relevant events.

Example event:

```json
{
  "id": "evt_01H...",
  "time": "2026-01-27T12:00:00Z",
  "type": "registry.jit_resolve",
  "principal_id": "user_01H...",
  "resource": {
    "kind": "registry_object",
    "id": "uxpatterns:uxp-001"
  },
  "decision": "allow",
  "reason": "entitled:subscription",
  "receipt_id": "rcpt_01H..."
}
```

---

### Metering export

- `GET /admin/v1/metering/events?since=<iso>&limit=<n>`
  - Returns usage events for credits/billing.

Example event:

```json
{
  "id": "use_01H...",
  "time": "2026-01-27T12:00:00Z",
  "principal_id": "user_01H...",
  "meter": "registry.jit_bytes",
  "amount": 24512,
  "resource": {
    "kind": "registry",
    "id": "uxpatterns"
  },
  "context": {
    "org_id": "org_01H...",
    "node_id": "node_01H..."
  }
}
```

---

## Relationship to receipts

Receipts are attached to high-leak responses (JIT pulls, exports, derivations)
and referenced by `receipt_id` in audit logs.

See `docs/policy/receipts-and-traceability.md`.
