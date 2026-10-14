# Security Controls

The controls ctxt and dpkms implement, with their config keys, and the ones they do not.

## Inbound authentication

dpkms has three access classes (`server.access`):

| Class | Binds | Auth |
|-------|-------|------|
| `private` (default) | `127.0.0.1` | none; every local caller holds every scope |
| `protected` | all interfaces | `server.auth` required, or `dpkms serve` refuses to start |
| `public` | all interfaces | as `protected`; `/healthz` details hidden from anonymous callers |

The only provider is `static`:

```yaml
server:
  access: protected
  auth:
    provider: static
    static:
      tokens:
        - token: <32+ random characters>   # literal value; env references are not expanded
          principal: laptop
          roles: [writer]                  # admin | writer | reader
```

- Clients send `Authorization: Bearer <token>` or `X-API-Key: <token>`. Tokens shorter than 32 characters or containing `REPLACE_ME` fail validation; comparison is constant-time.
- Roles expand to scopes: `admin` holds every scope, `writer` every read and write scope, `reader` every read scope. Each `/api/v1` route and gRPC method requires one scope; `401` without a valid token, `403 INSUFFICIENT_SCOPE` without the scope.
- Web UI: `ctxt ui open` mints a single-use sign-in link. The browser session holds the web UI's scopes intersected with its token's, ends after `server.ui.session.idle_ttl` (12h) or `max_ttl` (7 days), and ends when the token is removed. `dpkms session list|revoke` manage sessions.
- Federation push on non-private instances also needs `federation.token`.
- Not implemented: `oidc` and `mtls` providers (reserved names), per-user accounts.

Clients: `ctxt` sends `server.token` (or the matching `server.urls[].token`). Keep tokens out of shared config: load them from a gitignored second `--config` file ([configuration-structure.md](../../configuration-structure.md)).

Reference: [Inbound auth, roles and scopes](../../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes).

## Browser and request guards

- **Host allowlist** (DNS rebinding): a `private` instance rejects any `Host` other than `127.0.0.1` / `localhost` on its port or an entry in `server.allowed_hosts`. `protected` / `public` instances check `Host` only when `server.allowed_hosts` is set.
- **Cross-origin guard** (every instance): state-changing browser requests from another origin get `403 CROSS_ORIGIN_REQUEST`. Requests without `Sec-Fetch-Site` / `Origin` (CLI, federation peers, MCP clients) and browser-extension origins pass; `--dev` / `server.dev` adds the Vite dev server origins.
- **JSON-only bodies** (every instance): a state-changing request with a body not labelled `application/json` gets `415`.

## Secrets

- **Provider API keys**: the secrets backend (`secrets.backend`: `env`, `keyring`, `agefile`, `onepassword`, `ghsecrets`) or the env var of the same name. `dpkms secret set` reads from stdin, `--from-file` or `--prompt`, never a positional argument. See [../secrets-backends.md](../secrets-backends.md).
- **Registry credentials**: OS keychain.
- **Config scan**: `ctxt config validate` and `ctxt config doctor` flag plaintext-looking secrets (printing a 4-character hint) and reject `${VAR}` values; `doctor` warns on loose file permissions and `--fix` sets `0600`.

Not implemented: log sanitization. `ctxt config show --format json|yaml` prints tokens. See [../secrets-validation-and-log-sanitization.md](../secrets-validation-and-log-sanitization.md).

## Policy rules

`dpkms serve` loads kit CEL rules from `$XDG_CONFIG_HOME/contexthelp/policy/ctxt.yaml` (or `CTXT_POLICY_FILE`). The seeded rules require a `--note` on pipeline archive and remove. See [../../ctxt/policies.md](../../ctxt/policies.md).

Destructive CLI commands (`ctxt delete`, `ctxt inbox discard|clear`, ...) also require the `--confirm-token` the command prints.

## Data validation

- Config: decoded strictly and validated (`ctxt config validate`); unknown backends, short tokens, `${VAR}` values fail.
- Registry data: Ed25519 signatures when the registry declares a key, entity integrity guard, trust levels ([boundaries.md](boundaries.md#boundary-2-external-registries)).
- Request bodies: JSON decoding per handler; size limits only on `/api/v1/find` and the web UI sign-in exchange.
- Storage: parameterized SQL on SQLite and PostgreSQL.

## Audit log and alerts

- The audit log is an append-only table in the dpkms database. It records security alerts (`security.auth_failure` and `security.acl_denial` once a principal reaches the threshold below; every `security.quota_exhausted`), duplicate detection, fan-out enrichment and `ctxt doctor` runs. Object creates, edits and deletes are not recorded.
- Read it with `ctxt audit list` / `ctxt audit export` (`admin:audit` scope over HTTP).
- `security.alerts`: reaching `auth_failure_threshold` (3) auth failures or `acl_denial_threshold` (10) ACL denials per principal in 60s writes the alert entry above and, if set, posts to `webhook_url` or sends mail via `smtp`. Each POST or SMTP session ends after `delivery_timeout` (10s), or when serve's shutdown drain (`jobs.drain_timeout`) runs out; `webhook_url` redirects are not followed (a 3xx is logged as a failed delivery).
- The `audit:` config block (`audit_log`, `syslog`, `webhook`) is read by no code; nothing forwards the audit log.

## Encryption

- **At rest**: none. Use OS disk encryption. [ADR-019](../../decisions/ADR-019-encryption-and-privacy.md) is design.
- **In transit**: dpkms serves plain HTTP and gRPC. Terminate TLS at a reverse proxy; use `sslmode=require` in a PostgreSQL DSN. Outbound HTTPS calls verify certificates.
- **Config bundles**: `ctxt config backup` writes a signed bundle (`.zip` + `.sig`). `dpkms backup` archives are not encrypted.

## Untrusted code

No plugin is loaded and no installed external step is executed at runtime, so there is no sandbox to configure. See [boundaries.md](boundaries.md#boundary-4-plugins-and-installed-steps).

## Deployment

- Local: the defaults are local-only (`private`, SQLite, no telemetry). Nothing to set.
- Remote access: `server.access: protected` with static tokens, behind a TLS-terminating reverse proxy ([Put dpkms Behind a Reverse Proxy](../../manual/operations/reverse-proxy.md)), or an SSH tunnel to a `private` instance. Never put a `private` instance behind a proxy: it authenticates nobody.
- Containers: [../../deployment/docker.md](../../deployment/docker.md) (tokens via a mounted secrets file).
- Liveness: `dpkms healthcheck`, `GET /health`, `GET /healthz`.

## Related

- [threat.md](threat.md), [boundaries.md](boundaries.md), [compliance.md](compliance.md)
- [../../dpkms/security.md](../../dpkms/security.md)
