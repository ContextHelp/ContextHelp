# Threat Model

Threats to ctxt and dpkms, what mitigates each one today, and what does not.

## Protected assets

- **Knowledge data**: objects, blobs, entities, edges, search history, the audit log — in the dpkms store (SQLite file or PostgreSQL) and the blob store under the data directory (or S3).
- **Credentials**: provider API keys (secrets backend or env), `server.auth` tokens, `server.token` on clients, `federation.token`, registry credentials (OS keychain), S3 keys.
- **Configuration**: `ctxt.yaml`, `dpkms.yaml`, `policy/ctxt.yaml`, pipeline overrides.

## Threats

### 1. Unauthorized access to the API

**Vectors**: a remote caller reaching dpkms; a web page in the user's browser calling `127.0.0.1` (DNS rebinding, CSRF); a stolen token.

**Mitigations**:
- `server.access: private` (default) binds `127.0.0.1` only.
- `protected` / `public` require static tokens (32+ characters, no `REPLACE_ME`, constant-time compare) with roles that expand to scopes; every `/api/v1` route and gRPC method needs one scope.
- Host header check (always on `private`; on `protected` / `public` when `server.allowed_hosts` is set), cross-origin guard and JSON-only request bodies.
- `/healthz` details hidden from anonymous callers on public instances.

**Gaps**: no TLS in dpkms; no rate limiting; no per-user accounts; a `private` instance behind a reverse proxy authenticates nobody.

### 2. Unauthorized access to files

**Vectors**: readable database, blob or config files; backups copied off the machine.

**Mitigations**: `ctxt config doctor` warns on group- or world-readable config and `--fix` sets `0600`. `ctxt config backup` writes a signed config bundle (`.zip` + `.sig`).

**Gaps**: no encryption at rest for the database or blobs; `dpkms backup` archives are not encrypted. Use OS disk encryption.

### 3. Credential exposure

**Vectors**: secrets in config files, shell history, logs, `ctxt config show` output, version control.

**Mitigations**:
- `ctxt config validate` / `doctor` flag plaintext-looking secrets and print only a 4-character hint; validation rejects `${VAR}` values and placeholder or short tokens.
- `dpkms secret set` refuses a secret as a positional argument.
- Registry credentials live in the OS keychain and are never sent over plain `http` to a non-loopback host.

**Gaps**: logs are not sanitized (`-V` included); `ctxt config show --format json|yaml` prints tokens. See [What output shows secrets](../secrets-validation-and-log-sanitization.md#what-output-shows-secrets).

### 4. Malicious registry data

**Vectors**: a compromised or spoofed registry serving entities or step manifests.

**Mitigations**:
- Ed25519 signature check on sync responses when the registry declares a public key; `registries_global.require_signatures: true` refuses unsigned registries.
- Entity integrity guard rejects malformed entities before write.
- Trust levels: only `trust_level: trusted` registries write entities, in a single sync and in `--reconcile` alike. `untrusted` (the default) registries' entities wait in the approval queue until an admin approves or rejects them (`ctxt registry pending|approve|reject`; the API routes need `sync:registries`); `sandboxed` registries' entities are blocked and never queued.

**Gaps**: signatures are optional by default.

### 5. Untrusted code

**Vectors**: plugins, installed pipeline steps.

**State**: no plugin is loaded at runtime, so plugins cannot run in a deployed dpkms. `dpkms pipeline step install` records steps from a step registry; no pipeline executes installed external steps today. When either lands, it runs in-process or as a plain subprocess: there is no sandbox ([ADR-027](../../decisions/ADR-027-plugin-isolation-sandboxing.md) is design).

### 6. Data exfiltration

**Vectors**: provider calls, registry sync, federation push, URL capture.

**Mitigations**: nothing leaves the machine unless configured (provider keys/endpoints, `registries`, `federations`, S3 / PostgreSQL). `--offline` refuses outbound HTTP for one invocation (loopback stays reachable). Captured URLs, feeds and every redirect they follow refuse private-network addresses (SSRF guard, [controls.md](controls.md#outbound-fetch-guard)).

**Gaps**: a browser-rendered fetch (`url.authenticated`, `url.interactive`) is vetted before the browser runs, but the browser's own DNS lookups, redirects and subresources are not checked.

### 7. Denial of service

**Mitigations**: bounded worker pool (`server.workers`); `server.quotas` caps entity resolve / content pull / taxonomy sync per principal per month; body limits on `/api/v1/find` and the web UI sign-in exchange.

**Gaps**: no rate limiting and no general request-size limit.

### 8. Policy bypass on destructive operations

**Mitigations**: kit CEL rules in `policy/ctxt.yaml`, loaded by `dpkms serve` (default rules require `--note` on pipeline archive/remove); destructive CLI commands need `--confirm-token`; delete needs the `delete:objects` scope (admin role).

## Summary

| Threat | Main mitigation | Main gap |
|--------|-----------------|----------|
| API access | loopback default, static tokens + scopes, Host/origin guards | no TLS, no rate limit |
| File access | config permission check | no encryption at rest |
| Credential exposure | config secret scan, secrets backend, keychain | logs not sanitized |
| Registry data | Ed25519 signatures, trust levels | signatures optional |
| Untrusted code | nothing runs it today | no sandbox |
| Exfiltration | opt-in egress, `--offline` | URL fetch reaches private addresses |
| DoS | worker pool, quotas | no rate limit, no body limit |

## Related

- [boundaries.md](boundaries.md), [controls.md](controls.md)
- [../secret-management.md](../secret-management.md), [../validation.md](../validation.md)
