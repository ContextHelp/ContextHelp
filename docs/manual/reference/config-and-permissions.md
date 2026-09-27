# Config and Permissions

## Goal

Run predictable, secure deployments by combining validated configuration, explicit runtime controls, and constrained extension permissions.

## Configuration controls

### Validate and inspect config

```bash
ctxt config path
ctxt config show
ctxt config validate
```

Edit config in default editor:

```bash
ctxt config edit
```

### Layering model

Configuration behavior is layered (defaults, system, user, env, flags). For full details:

- [`../../configuration-structure.md`](../../configuration-structure.md)
- [`../../environment-variables/README.md`](../../environment-variables/README.md)

## Runtime controls

### Server settings

```bash
dpkms serve --port 8080 --grpc-port 9090 --workers 4
dpkms serve --public
```

### Inbound auth, roles and scopes

A private instance (the default: loopback only, no `server.auth`) needs no token and grants every scope. A `protected` or `public` instance requires `server.auth`, and each token carries one or more roles:

```yaml
server:
  access: protected
  auth:
    provider: static
    static:
      tokens:
        - token: ${DPKMS_TOKEN_OWNER}
          principal: owner
          roles: [admin]
        - token: ${DPKMS_TOKEN_PHONE}
          principal: phone
          roles: [writer]   # capture-only device
        - token: ${DPKMS_TOKEN_DASHBOARD}
          principal: dashboard
          roles: [reader]
```

Every `/api/v1` route and gRPC method requires one scope. A role is a fixed bundle of scopes:

| Role | Scopes |
|---|---|
| `admin` | every scope; also skips the entity entitlement and metering gate |
| `writer` | every `read:*` and `write:*` scope |
| `reader` | every `read:*` scope |

A token needs at least one known role; an unknown or missing role fails `ctxt config validate` and stops `dpkms serve`. A token without a route's scope gets 403 `INSUFFICIENT_SCOPE` naming the scope; a missing or invalid token gets 401. `GET /api/v1/whoami` shows a token's principal, roles and scopes.

| Scope | Bundles | Routes (under `/api/v1`) and gRPC methods |
|---|---|---|
| `read:objects` | reader, writer, admin | `GET` objects (list, facets, show, related), search, entities (list, show, backlinks), aliases, saved searches, search history, suggestions; `POST /find`; `GET /events`; the MCP mount; gRPC `QueryService/*`, `EntityService/*` |
| `read:inbox` | reader, writer, admin | `GET /inbox` |
| `read:feeds` | reader, writer, admin | `GET /feeds` |
| `read:jobs` | reader, writer, admin | `GET` jobs, `import/{id}`, `importers/runs/{id}`, `capture/recent`; gRPC `JobService/*` |
| `read:registries` | reader, writer, admin | `GET /steps/registries` |
| `read:system` | reader, writer, admin | `GET /whoami`; `GET` pipelines, steps, watches, watch files, system reminders; gRPC reflection |
| `write:objects` | writer, admin | `POST /analyze`, `capture/*`, `import`, `importers/*/run`, `pipelines/enqueue`, `aliases`, `saved-searches`, suggestion approve and reject, `entities/{slug}/pull`; `PATCH /objects/{id}`; gRPC `AnalyzeService/Analyze` |
| `write:inbox` | writer, admin | `POST /inbox` |
| `write:feeds` | writer, admin | `POST /feeds`, `feeds/sync`, `feeds/{id}/sync` |
| `write:jobs` | writer, admin | `POST /jobs/{id}/retry` |
| `write:system` | writer, admin | `POST /system/reminders/{id}/dismiss` |
| `delete:objects` | admin | `DELETE` objects, aliases, saved searches, search history |
| `delete:feeds` | admin | `DELETE /feeds/{id}` |
| `process:inbox` | admin | `POST /inbox/{id}/triage`, `inbox/{id}/discard` |
| `sync:registries` | admin | `POST /steps/registries/fetch`, `steps/registries/{url}/update`, `entities/registry-sync` |
| `admin:pipelines` | admin | `POST /pipelines`, `pipelines/{name}/archive`, `pipelines/{name}/unarchive`; `DELETE /pipelines/{name}` |
| `admin:plugins` | admin | `POST /steps/install`; `DELETE /steps/{name}` |
| `admin:watches` | admin | `POST /watches`, `watches/{id}/pause`, `watches/{id}/resume`; `PATCH` and `DELETE /watches/{id}` |
| `admin:audit` | admin | `GET /audit-log` |

`POST /api/v1/federation/push` needs `write:objects` and, on non-private instances, the `federation.token` credential as well: the federation token must be a static token whose principal holds `writer` or `admin`. `/health`, `/healthz` and gRPC health are open.

### Reach dpkms by another host name

dpkms checks the `Host` header of every HTTP request, so a web page
that points its own DNS name at your machine cannot use the API. A
private instance (the default) answers only to `127.0.0.1:<port>` and
`localhost:<port>`, where `<port>` is the port it bound. Any other name
gets `403` with the error code `HOST_NOT_ALLOWED`.

To reach it by another name, list that name under `server.allowed_hosts`:

```yaml
server:
  allowed_hosts:
    - dpkms.lan          # this host on any port
    - 192.168.1.20:8080  # this host on port 8080 only
```

Entries are host names or IP addresses, with an optional port. They are
not URLs: no scheme, path or wildcard.

Protected and public instances check `Host` only when
`server.allowed_hosts` is set. Set it to the names your clients and
reverse proxy use; `127.0.0.1:<port>` and `localhost:<port>` stay allowed.

### Profile controls

```bash
ctxt profile list
ctxt profile view <name>
ctxt profile set <name>
ctxt profile unset <name>
```

## Pipeline and extension permissions

### Pipeline governance controls

```bash
dpkms pipeline list --include-archived
dpkms pipeline show <name> --raw
dpkms pipeline step list
dpkms pipeline step registry list
```

### Registry update policy

```bash
dpkms pipeline step registry autoupdate <registry-url> --enable
dpkms pipeline step registry autoupdate <registry-url> --disable
```

## Permission and isolation policy references

- Plugin isolation: [`../../plugins/plugin-isolation.md`](../../plugins/plugin-isolation.md)
- Plugin API: [`../../plugins/plugins-api.md`](../../plugins/plugins-api.md)

## Operational safeguards

1. Always run `ctxt config validate` before rollout.
2. Use explicit registry auto-update policies.
3. Prefer staged pipeline changes plus test enqueue runs.
4. Track runtime flags used in production for reproducibility.

## Story alignment

- Config/admin: `US-0027`, `US-0030`, `US-0031`
- Plugin/pipeline permissions: `US-0029`, `US-0042` to `US-0045`, `US-0112`
