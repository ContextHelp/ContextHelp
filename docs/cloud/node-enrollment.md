# Node Enrollment (Cloud)

Node enrollment is how a self-hosted or cloud-managed node becomes associated
with an org in **context.help cloud**.

## Goals

- Allow one org to manage many nodes
- Establish trust without weakening local-first operation
- Support revocation and key rotation

## Core concepts

- **Node ID**: stable identifier for a node instance
- **Org ID**: tenant identifier in the cloud
- **Issuer**: OIDC/JWT issuer trusted by the node (cloud or external)

## Enrollment phases (high level)

1. Node generates or loads its long-lived identity key material
2. Cloud issues an enrollment token scoped to an org and allowed claims
3. Node exchanges enrollment token for a signed configuration payload
4. Node stores trust configuration locally and begins emitting status/audit/metering

See `docs/decisions/ADR-031-nodes-and-cloud-boundary.md`.
