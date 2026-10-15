# Workflow: Gate Principals by Namespace

## Goal

Restrict what namespaces a principal can read. By default every principal reads every namespace. Once you create a grant, the principal reads only entities whose namespace matches one of its globs. If you revoke the last glob, every gated read returns 403. If you delete the grant entirely with `ungate`, the principal goes back to unrestricted.

## Prerequisites

- Shell access to the server host running the dpkms instance.
- The `dpkms` binary in your PATH.
- The instance database accessible at its configured path (same requirement as `dpkms session`).
- The exact principal name the caller authenticates as. For static tokens, this is the `principal` field in `server.auth.static.tokens` (case-sensitive). The instance owner bypasses the gate; granting to it has no effect.

## Quick start

Gate `phone` to two namespaces and verify:

```bash
dpkms entitlement grant phone ai.* devops.k8s
dpkms entitlement list
```

A running instance picks up the change on the principal's next request. No restart required.

## Unrestricted, gated, denied

Every principal is in one of three states:

| State | Condition | Effect on reads |
|---|---|---|
| Unrestricted | No grant exists | Reads every namespace |
| Gated | Grant exists with at least one namespace glob | Reads only matching namespaces |
| Denied | Grant exists but has no namespaces, or grant is past its expiry | Every gated read returns 403 ENTITLEMENT_REQUIRED |

The default state for every principal is unrestricted. Creating a grant (via `grant`) moves the principal to gated from that point on. Deleting the grant (via `ungate`) returns it to unrestricted. Revoking the last namespace leaves an empty grant in place, which is deny-all, not unrestricted.

## Grant namespaces

`dpkms entitlement grant <principal> <namespace>...` adds namespaces to the principal's grant. If no grant exists, it creates one and gates the principal immediately.

Namespace globs accept three forms:

- Exact: `devops.k8s` matches only `devops.k8s`.
- Suffix wildcard: `ai.*` matches `ai.bert`, `ai.models`, but not `ai` itself.
- Everything: `*` matches all namespaces.

Namespaces already granted are left as they are; re-granting is a no-op.

```bash
dpkms entitlement grant phone ai.* devops.k8s
dpkms entitlement grant partner finance.* --expires 2026-12-31
dpkms entitlement grant partner finance.* --expires never
```

`--expires` accepts a duration (`720h`), a date (`2026-12-31`, meaning end of that UTC day), or an RFC 3339 timestamp. `--expires never` removes an existing expiry. Without `--expires`, a new grant never expires and an existing grant keeps its current expiry.

## Revoke namespaces

`dpkms entitlement revoke <principal> <namespace>...` removes globs from the principal's grant. The glob must match exactly as it was granted: revoking `ai.*` removes `ai.*`, not `ai.models`.

```bash
dpkms entitlement revoke phone devops.k8s
```

Revoking a namespace that is not granted, or revoking from a principal with no grant, changes nothing and reports `No change`. Revoking the last namespace leaves an empty grant in place, which denies all gated reads. To lift gating entirely, use `ungate` instead.

## Ungate a principal

`dpkms entitlement ungate <principal>` deletes the grant and returns the principal to unrestricted access. This is destructive: the command prints the namespaces it dropped so you can recreate them with `grant` if needed.

Off a terminal, pass `--confirm=yes`. In a terminal, the command prompts for confirmation.

```bash
dpkms entitlement ungate phone --confirm=yes
```

Ungating a principal with no grant changes nothing.

## List grants

```bash
dpkms entitlement list
dpkms entitlement list --format json
```

`list` shows every grant with the principal name, namespaces, expiry (marked `(expired)` when past), and the timestamp of the last change. `--format json` returns:

```json
{"entitlements": [...], "total": N}
```

This output covers only grants given to callers of this instance (inbound). Entitlements this instance holds on remote registries are separate; list them with `ctxt registry entitlements` and refresh them with `ctxt registry sync`. Registry sync never overwrites a same-named inbound grant, and `dpkms entitlement` commands never touch registry rows.

## Preview a change

`grant`, `revoke`, and `ungate` all accept `--dry-run`. The command prints `[dry-run] no writes will occur` and describes what would change, then exits without writing anything.

```bash
dpkms entitlement grant phone ai.* devops.k8s --dry-run
dpkms entitlement revoke phone devops.k8s --dry-run
dpkms entitlement ungate phone --dry-run
```

With `--format json`, each change reports `action`, `dry_run`, `no_op`, and `result` (containing `principal`, `added`, `removed`, `unchanged`, and `entitlement`).

## Grants after the upgrade

Before this release, inbound grants and remote registry entitlements shared one table. A row could not identify which it was.

On first open after upgrading, a migration runs automatically: SQLite migration 051 (Postgres migration 32). It splits the table by inspecting each row's name:

- A row named `owner` or starting with `prn_` becomes an inbound grant only.
- Any other row (a registry name, or a static-token principal such as `phone`) is kept as a registry entitlement and also copied as an inbound grant.

No principal's access changes after the migration. The copy of a registry name as an inbound grant only matters if a principal authenticates with that exact name. To remove a stray copy:

1. Run `dpkms entitlement list` and identify rows whose names are registry names, not principals.
2. Ungate each:

```bash
dpkms entitlement ungate acme-registry --confirm=yes
```

The registry's own entitlement row stays. Only the copied inbound grant is removed.

## Validation checklist

After any change, confirm the following:

- `dpkms entitlement list` shows the principal with the expected namespaces and expiry.
- A request by that principal for a granted namespace succeeds.
- A request for a namespace outside the grant returns 403 ENTITLEMENT_REQUIRED.
- After `ungate`, the principal no longer appears in `dpkms entitlement list`.

## Common failure modes

### A principal is denied everything

Its grant is empty or past its expiry. Run `dpkms entitlement list` and check the principal's row. To fix:

- Add a namespace: `dpkms entitlement grant <principal> <namespace>`
- Extend or remove the expiry: `dpkms entitlement grant <principal> <namespace> --expires never`
- Remove the grant entirely and return to unrestricted: `dpkms entitlement ungate <principal> --confirm=yes`

### Grant has no effect

Two causes:

1. The principal is the instance owner (admin role). The owner bypasses the gate. Granting to it does nothing.
2. The name passed to `grant` does not match what the caller authenticates as. The `principal` field in `server.auth.static.tokens` is case-sensitive. Run `dpkms entitlement list` and compare the row name against the token config exactly.

### ungate refused

Off a terminal, the command requires `--confirm=yes`. Without it, the command exits with an error. Add the flag and rerun.

### revoke reports No change

The glob must match exactly as it was granted. `ai.*` is a different string from `ai.models`. Run `dpkms entitlement list`, copy the glob from the output, and pass that exact string to `revoke`.

### Namespace validation errors

- `namespace must not be empty`: you passed an empty string as a namespace.
- `syntax error in pattern`: the namespace is not a valid glob, such as `ai.[`. Correct the pattern and rerun.
- `requires at least 2 arg(s)`: `grant` and `revoke` need a principal and at least one namespace.

## For operators

Grants live in the instance database table `registry_entitlements` with `kind = inbound`. Registry entitlement rows use `kind = outbound`. Back up the database to preserve all grants.

Quotas per principal are tracked separately in the metering system and are not affected by entitlement commands.

Related pages:

- [Sign in to the web UI](./web-ui-sign-in.md)
- [Config and permissions](../reference/config-and-permissions.md)
