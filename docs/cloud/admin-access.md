# Admin Access (Cloud)

The cloud model is built around **orgs owning many nodes**.

## Cloud responsibilities

- Org directory (users, groups) and lifecycle (SSO/SCIM)
- Assign who can administer which nodes
- Provide a UI for node administration via node admin APIs

## Node responsibilities (OSS)

- Enforce policy locally for all operations
- Maintain local RBAC bindings and policy configuration
- Emit audit/metering events to the cloud (optional)

## Suggested mental model

- Cloud decides *who may administer a node*
- Node decides *what actions are permitted and enforced*

See `docs/api/node-admin-api.md`.
