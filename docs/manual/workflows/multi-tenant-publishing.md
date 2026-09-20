# Workflow: Multi-Tenant Publishing

## Goal

Founder publishes per-role private registries; per-role employees subscribe selectively; data
sovereignty preserved (registries influence selection, never receive user content). Role-scoped
"what's new since last check" via named cursors.

## Scope

- Founder runs N private registries (`client-ops`, `client-finance`, ...) on her dpkms.
- Each role has an aps profile + ctxt focus profile bound to one registry.
- Employees on shared dpkms read merged content; standalone-dpkms employees pull via
  `ctxt registry sync` over the network.
- Per-role cursor (`ctxt list --cursor <name>`) advances independently.
- Powers showcase scenario 1 (client-bubble dashboard).

## Primary stories

- `US-0030` set-up-focus-profiles
- `US-0318` configure-federation-targets
- `US-0319-fed` async-push-local-merged
- `US-0409` named-cursor (`ctxt list --cursor <name>`)

## Prerequisites

1. Founder dpkms running: `dpkms serve`
2. Federation enabled (`US-0318` config) so role profiles share content where intended.
3. Registry endpoints reachable (file, git, or HTTP) — see `dpkms/registries.md`.
4. aps profile per role + ctxt focus profile per role (`US-0030`).

## Worked example: company X, 4 employees

| Role | aps profile | ctxt focus profile | Subscribed registry | Host |
|---|---|---|---|---|
| Founder | `founder` | `all` | publishes both | shared dpkms |
| Sales | `sales` | `client-ops` | `client-ops` | shared dpkms |
| Support | `support` | `client-ops` | `client-ops` | shared dpkms |
| Billing | `billing` | `client-finance` | `client-finance` | shared dpkms |
| Standalone | `field` | `client-ops` | `client-ops` | own dpkms (network) |

Founder captures `@client.acme` notes; sales+support+field see ops-relevant entries; billing sees
finance entries; nobody sees the wrong slice.

## Procedure

### Step 1: Define + publish registries (founder)

```bash
# Pseudocode — exact registry-publish surface lives in dpkms/registries.md
ctxt registry add client-ops    file:///srv/registries/client-ops.json
ctxt registry add client-finance file:///srv/registries/client-finance.json
ctxt registry sync client-ops
ctxt registry sync client-finance
```

Each registry declares scope (entity prefixes, allowed mentions, weights). Registries are
read-only semantic overlays: they never receive content. Content distribution is via federation.

### Step 2: Per-role focus profile (each employee, once)

```bash
# Pseudocode — see US-0030
ctxt profile create sales    --registries client-ops
ctxt profile create support  --registries client-ops
ctxt profile create billing  --registries client-finance
```

Profile pins a registry set; subsequent queries with `--profile <name>` apply that lens.

### Step 3: Standalone-dpkms employee subscribes over network

```bash
ctxt registry add client-ops https://founder.example.com/registries/client-ops
ctxt registry sync client-ops
```

Network registry uses the same `ctxt registry sync` surface; auth via `ctxt registry auth-set`.

### Step 4: Founder captures with mention + routing intent

```bash
# Mention identifies the client; tags + pipeline drive registry-side weighting
ctxt analyze "Acme renewal call: support ticket spike"        \
  --type text --mentions "@client.acme" --hints "#support"
ctxt analyze "Acme PO #1742 invoice clearance"                \
  --type text --mentions "@client.acme" --hints "#billing"
```

Capture stays local on founder's dpkms. Federation push (`US-0319-fed`) propagates to the
shared dpkms. Each role's `--profile` filters which entries surface based on registry scope.

### Step 5: Employees query "what's new" with named cursor

```bash
# Sales: only ops-side @client.acme entries since their last check
ctxt list --cursor sales-acme --profile sales \
  --mention @client.acme --advance

# Support: same registry, separate cursor — independent advance
ctxt list --cursor support-acme --profile support \
  --mention @client.acme --advance

# Billing: only finance-side entries since their last check
ctxt list --cursor billing-acme --profile billing \
  --mention @client.acme --advance
```

Cursor file is local per-employee state. `--advance` records the most-recent timestamp seen so
the next call returns only newer items.

### Step 6: Revoke an employee

```bash
ctxt registry remove client-ops   # employee-side
# Optional: founder rotates registry token via ctxt registry auth-set
```

Removing the registry subscription cuts the lens; cursor file can be deleted or left dormant.

## Acceptance / verification

- Sales sees only entries scoped by `client-ops`; never sees `client-finance` items.
- Support's cursor advances independently of sales' cursor for the same `@client.acme` query.
- Standalone-dpkms employee receives the same `client-ops` slice over network sync.
- Founder can attach the same capture to multiple registries (multi-mention or multi-tag).
- Capture without registry-targeted hints stays local-only (sovereign default).
- Revoking subscription = next `ctxt registry sync` returns nothing for that role.
- No registry endpoint receives raw user content (privacy invariant — `decentralization.md`).

## Common failure modes

### Wrong registry subscribed

- Verify with `ctxt registry list`; confirm role's `--profile` pins the intended registry set.
- Re-create profile: `ctxt profile rm <role> && ctxt profile create <role> --registries ...`.

### Capture not propagating to shared dpkms

- Confirm `US-0318` federation target configured on founder's dpkms.
- Inspect federation queue: `dpkms federation status` (pseudocode — see `US-0319-fed`).
- Re-run capture with explicit `--profile founder` to ensure correct origin profile.

### Cursor drift across employees

- Cursors are per-name local state; ensure each role uses a unique `--cursor` value.
- Reset with `ctxt cursor reset <name>`; re-advance from a known timestamp.

### Network registry auth failure

- Re-issue token: `ctxt registry auth-set <name>`.
- Confirm registry HTTP endpoint reachable; check firewall + cert.
- Use `ctxt registry capabilities <name>` to verify protocol compatibility.

## Related references

- [`../../dpkms/decentralization.md`](../../dpkms/decentralization.md)
- [`../../dpkms/registries.md`](../../dpkms/registries.md)
- [`../../dpkms/registry-syncing-and-retrieval.md`](../../dpkms/registry-syncing-and-retrieval.md)
- [`../../dpkms/mentions.md`](../../dpkms/mentions.md)
- `../../stories/admin/US-0030-set-up-focus-profiles.md`
- `../../stories/federation/US-0318-configure-federation-targets.md`
