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
| `admin` | every scope except `signout:ui`; also skips the entity entitlement and metering gate |
| `writer` | every `read:*` and `write:*` scope |
| `reader` | every `read:*` scope |

`read:ui` (mint a web UI sign-in link) is a read scope, so every role holds it; a browser session never does, so a session cannot mint another. `signout:ui` (end your own browser session) belongs to browser sessions only: no role holds it.

A token needs at least one known role; an unknown or missing role fails `ctxt config validate` and stops `dpkms serve`. A token without a route's scope gets 403 `INSUFFICIENT_SCOPE` naming the scope; a missing or invalid token gets 401. `GET /api/v1/whoami` shows the caller's principal, roles and scopes, and how it signed in (`via`: `token`, `session` or `none` on a private instance).

| Scope | Bundles | Routes (under `/api/v1`) and gRPC methods |
|---|---|---|
| `read:objects` | reader, writer, admin | `GET` objects (list, facets, show, related), search, the search graph, entities (list, search, resolve, show, backlinks), aliases, saved searches, search history, suggestions; `POST /find`; `GET /events`; gRPC `QueryService/*`, `EntityService/*` |
| `read:mcp` | reader, writer, admin | the MCP mount (`/mcp`) |
| `read:inbox` | reader, writer, admin | `GET /inbox`, `inbox/queue` |
| `read:feeds` | reader, writer, admin | `GET /feeds` |
| `read:jobs` | reader, writer, admin | `GET` jobs, `import/{id}`, `importers/runs/{id}`, `capture/recent`; gRPC `JobService/*` |
| `read:registries` | reader, writer, admin | `GET /steps/registries` |
| `read:system` | reader, writer, admin | `GET /whoami`, `GET /system/reminders`; gRPC reflection |
| `read:pipelines` | reader, writer, admin | `GET` pipelines and steps |
| `read:watches` | reader, writer, admin | `GET` watches and watch files |
| `write:objects` | writer, admin | `POST /analyze`, `capture/*`, `import`, `importers/*/run`, `pipelines/enqueue`, `aliases`, `saved-searches`, suggestion approve and reject, `entities/{slug}/pull`; `PATCH /objects/{id}`; gRPC `AnalyzeService/Analyze` |
| `write:inbox` | writer, admin | `POST /inbox` |
| `write:feeds` | writer, admin | `POST /feeds`, `feeds/sync`, `feeds/{id}/sync` |
| `write:jobs` | writer, admin | `POST /jobs/{id}/retry` |
| `write:system` | writer, admin | `POST /system/reminders/{id}/dismiss` |
| `delete:objects` | admin | `DELETE /objects/{id}` |
| `delete:aliases` | admin | `DELETE /aliases/{alias}` |
| `delete:searches` | admin | `DELETE /saved-searches/{name}`, `DELETE /search-history` |
| `delete:feeds` | admin | `DELETE /feeds/{id}` |
| `process:inbox` | admin | `POST /inbox/{id}/triage`, `inbox/{id}/discard`, `inbox/clear` |
| `sync:registries` | admin | `POST /steps/registries/fetch`, `steps/registries/{url}/update`, `entities/registry-sync` |
| `admin:pipelines` | admin | `POST /pipelines`, `pipelines/{name}/archive`, `pipelines/{name}/unarchive`; `DELETE /pipelines/{name}` |
| `admin:plugins` | admin | `POST /steps/install`; `DELETE /steps/{name}` |
| `admin:watches` | admin | `POST /watches`, `watches/{id}/pause`, `watches/{id}/resume`; `PATCH` and `DELETE /watches/{id}` |
| `admin:audit` | admin | `GET /audit-log` |
| `read:ui` | reader, writer, admin; never a browser session | `POST /ui/login-codes` (`ctxt ui open`) |
| `signout:ui` | browser sessions only | `DELETE /ui/session` |

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

To open the web UI of a remote instance in a browser, put it behind a
reverse proxy that signs you in and adds the token:
[Put dpkms behind a reverse proxy](../operations/reverse-proxy.md).

### Send write requests from scripts

A `POST`, `PATCH`, `PUT` or `DELETE` that carries a body must send
`Content-Type: application/json`; anything else gets `415` with the error
code `UNSUPPORTED_MEDIA_TYPE`. `curl -d` sends a form type by default, so
add the header:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/inbox \
  -H 'Content-Type: application/json' \
  -d '{"content":"remember this"}'
```

Web pages from other origins cannot change anything: dpkms answers `403`
`CROSS_ORIGIN_REQUEST` to a write whose `Sec-Fetch-Site` or `Origin`
header names another site, including another port on `localhost`. The
web UI served by the instance, the ctxt CLI and the ctxt browser
extension are not affected.

### Browser-extension cookie bridge

`dpkms serve` also listens on `127.0.0.1:9377` (or a free port, printed
as `Cookie bridge listening on ws://…`) for the ctxt browser extension
to sync cookies. Whatever the instance's access class, the bridge accepts
only WebSocket handshakes that come from a browser extension
(`chrome-extension://`, `moz-extension://` or `safari-web-extension://`
origin) and name `127.0.0.1:<port>` or `localhost:<port>` in `Host`.
Web pages, other local tools and clients that send no `Origin` get `403`
`CROSS_ORIGIN_REQUEST`; other host names get `403` `HOST_NOT_ALLOWED`.
`server.allowed_hosts` does not apply to the bridge.

### Web UI sessions

On a protected or public instance the web UI needs a browser session;
`ctxt ui open` signs a browser in (see
[Sign in to the web UI](../workflows/web-ui-sign-in.md)). A private
instance has no sessions.

```yaml
server:
  ui:
    session:
      idle_ttl: 12h   # ends this long after the last request (default 12h)
      max_ttl: 168h   # ends this long after sign-in (default 7 days)
```

`idle_ttl` may not exceed `max_ttl`; negative values fail the config load.

- **Cookie**: `__Host-dpkms_<16 hex>`, named after the `Host` the browser
  used (two instances on one machine get different cookies), `HttpOnly`,
  `Secure`, `SameSite=Strict`, `Path=/`, expiring with the session.
  Browsers keep it only over HTTPS or on `localhost`.
- **Principal**: the principal of the static token that minted the link,
  with its roles; the session ends when that token leaves
  `server.auth.static.tokens` (every request checks the tokens dpkms
  loaded at start, so a removal takes effect on restart).
- **Scopes**: the token's scopes narrowed to the web UI's set: `read:objects`,
  `read:inbox`, `read:feeds`, `read:jobs`, `read:registries`,
  `read:system`, `delete:objects`, `write:jobs`, plus `signout:ui`, which
  only sessions hold. Apart from signing itself out, a session never
  holds a scope its token lacks: a `reader` token's session only reads,
  a `writer` token's may also retry jobs, an `admin` token's gets the
  whole set and nothing more. It never holds `read:ui`, so it cannot
  mint sign-in links. Every other route (pipelines, steps,
  registry changes, watches, the audit log, federation, the MCP mount,
  login links, other writes) answers `403` `INSUFFICIENT_SCOPE` naming
  the scope. `/ws/bus` keeps its own bus token and gRPC keeps API tokens
  only; a session cookie opens neither. `GET /api/v1/whoami` with the
  cookie shows the session's scopes and times.
- **CSRF**: a request carrying the cookie must come from the instance's
  own pages: writes need `Sec-Fetch-Site: same-origin` or a matching
  `Origin` (`403` `CROSS_ORIGIN_REQUEST`) and the header `X-Ctxt-CSRF: 1`
  (`403` `CSRF_HEADER_REQUIRED`); reads sent by another site or another
  port on the host are refused. Requests with `Authorization` or
  `X-API-Key` are unaffected, and those headers win over the cookie.

`dpkms session list [--all] [--principal P]` and
`dpkms session revoke <id> | --principal P` manage sessions on the server
host.

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
