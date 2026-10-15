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

- [`docs/configuration-structure.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/configuration-structure.md)
- [`docs/environment-variables/README.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/environment-variables/README.md)

## Runtime controls

### Server settings

```bash
dpkms serve --port 8080 --grpc-port 9090 --workers 4
dpkms serve --public
```

The listener ports can also live in `dpkms.yaml` or the environment. The flag wins over the env var, the env var over the file:

| Listener | Flag | Env var | Config key | Default |
|---|---|---|---|---|
| HTTP | `--port` | `CH_SERVER_PORT` | `server.port` | 8080 |
| gRPC | `--grpc-port` | `CH_GRPC_PORT` | `server.grpc_port` | 9090 |
| Cookie bridge (loopback) | `--cookie-bridge-port` | `CH_COOKIE_BRIDGE_PORT` | `server.cookie_bridge_port` | 9377 |

A port set any of these ways is bound exactly; serve exits non-zero when it is busy. `0` takes an OS-assigned port. Only an unset port falls back from a busy default to a free one.

The other serve settings follow the same order (flag, env var, file, default):

| Setting | Flag | Env var | Config key | Default |
|---|---|---|---|---|
| Job workers | `--workers` | `DPKMS_WORKERS` | `server.workers` | 4 |
| Dev-server CORS | `--dev` | | `server.dev` | false |
| Reminder check interval | `--reminder-interval` | | `server.reminder_interval` | 1m |
| External steps directory | `--steps-path` | | `steps.path` | `~/.config/contexthelp/steps` |
| Default focus profile | `--profile` | `CTXT_PROFILE` | `profile.default` | the only profile, if one is defined |

### Inbound auth, roles and scopes

A private instance (the default: loopback only) needs no credential and grants every scope. A `protected` or `public` instance authenticates every request through an ordered chain of methods, `server.auth.methods`, against the principal registry in its database. Carried credentials (`api_key`, in `Authorization: Bearer` or `X-API-Key`) run before ambient ones (`session`, the web UI cookie) whatever order you list them in. The first match wins and the first refusal ends the request, so a bad key is never rescued by a cookie behind it. An expired or unknown cookie is treated as absent and cleared.

```yaml
server:
  access: protected
  public_url: https://dpkms.example.net   # base of every sign-in link; required unless sign_in is []
  auth:
    methods: [api_key, session]           # default
    sign_in: [invite, link]               # ceremonies that may mint a browser session; default
    keys:                                 # optional: keys declared by hash, for infrastructure as code
      - hash: sha256:3f9c...              # printf %s "$KEY" | sha256sum
        principal: owner
        roles: [reader]                   # narrows the key; default: the principal's roles
        expires: 2027-12-31
```

Keys are created on the dpkms host and stored as hashes:

```bash
dpkms auth key create --principal owner --name "laptop CLI"   # --expires 720h, or a date
```

The key is printed once. Every principal is a row in the instance database: `owner` is seeded by the schema, and every other principal (a person, a device, a federation peer) gets keys of its own, so one can be revoked without the others. A key holds its principal's role scopes, or fewer when it was narrowed at creation; it never reaches past its principal.

A `protected` or `public` instance refuses to start until the owner holds an active key, or a `server.auth.keys` entry names one. The refusal names `dpkms auth key create --principal owner`. `server.auth.keys` takes hashes only, for infrastructure as code: a key written in the clear fails `ctxt config validate` and stops `dpkms serve`, naming the entry. A key declared in config cannot sign a browser in, since there is no identity row to bind the session to; use a created key for `ctxt ui open`. `server.public_url` is the base of every invite and sign-in link, so it is required whenever `sign_in` lists `invite` or `link`, which the default does. Set `sign_in: []` on an instance that mints no links.

Every `/api/v1` route and gRPC method requires one scope. A role is a fixed bundle of scopes:

| Role | Scopes |
|---|---|
| `admin` | every scope except `signout:ui`; also skips the entity entitlement and metering gate |
| `writer` | every `read:*` and `write:*` scope |
| `reader` | every `read:*` scope |

`read:ui` (mint a web UI sign-in link) is a read scope, so every role holds it; a browser session never does, so a session cannot mint another. `signout:ui` (end your own browser session) belongs to browser sessions only: no role holds it.

A principal needs at least one known role; an unknown role on a declared key fails `ctxt config validate` and stops `dpkms serve`. A key without a route's scope gets 403 `INSUFFICIENT_SCOPE` naming the scope. A missing, revoked, expired or invalid key gets 401. `GET /api/v1/whoami` shows the caller's principal, roles and scopes, and how it signed in (`via`: `token`, `session` or `none` on a private instance).

| Scope | Bundles | Routes (under `/api/v1`) and gRPC methods |
|---|---|---|
| `read:objects` | reader, writer, admin | `GET` objects (list, facets, show, related, edges), search, the search graph, entities (list, search, resolve, show, backlinks), aliases, saved searches, search history; `POST /find`; `GET /events`; gRPC `QueryService/*`, `EntityService/*` |
| `read:mcp` | reader, writer, admin | the MCP mount (`/mcp`) |
| `read:inbox` | reader, writer, admin | `GET /inbox`, `inbox/queue` |
| `read:feeds` | reader, writer, admin | `GET /feeds` |
| `read:jobs` | reader, writer, admin | `GET` jobs, `import/{id}`, `importers/runs/{id}`, `capture/recent`; gRPC `JobService/*` |
| `read:registries` | reader, writer, admin | `GET /steps/registries` |
| `read:system` | reader, writer, admin | `GET /whoami`, `GET /system/reminders`; gRPC reflection |
| `write:self` | reader, writer, admin; browser sessions | a principal's own keys, identities and sessions (routes arrive with the principal CLI) |
| `read:pipelines` | reader, writer, admin | `GET` pipelines and steps |
| `read:watches` | reader, writer, admin | `GET` watches and watch files |
| `write:objects` | writer, admin | `POST /analyze`, `capture/*`, `import`, `importers/*/run`, `pipelines/enqueue`, `aliases`, `saved-searches`, suggestion approve and reject, `entities/{slug}/pull`; `PATCH /objects/{id}`, `POST /objects/{id}/reprocess`, `POST /edges`; gRPC `AnalyzeService/Analyze` |
| `write:inbox` | writer, admin | `POST /inbox` |
| `write:feeds` | writer, admin | `POST /feeds`, `feeds/sync`, `feeds/{id}/sync` |
| `write:jobs` | writer, admin | `POST /jobs/{id}/retry` |
| `write:system` | writer, admin | `POST /system/reminders/{id}/dismiss` |
| `delete:objects` | admin | `DELETE /objects/{id}` |
| `delete:edges` | admin | `DELETE /edges` |
| `delete:aliases` | admin | `DELETE /aliases/{alias}` |
| `delete:searches` | admin | `DELETE /saved-searches/{name}`, `DELETE /search-history` |
| `delete:feeds` | admin | `DELETE /feeds/{id}` |
| `process:inbox` | admin | `POST /inbox/{id}/triage`, `inbox/{id}/discard`, `inbox/clear` |
| `cancel:jobs` | admin | `POST /jobs/{id}/cancel` (`ctxt job cancel`) |
| `sync:registries` | admin | `POST /steps/registries/fetch`, `steps/registries/{url}/update`, `entities/registry-sync` |
| `admin:pipelines` | admin | `POST /pipelines`, `pipelines/{name}/archive`, `pipelines/{name}/unarchive`; `DELETE /pipelines/{name}` |
| `admin:steps` | admin | `POST /steps/install`; `DELETE /steps/{name}` |
| `admin:watches` | admin | `POST /watches`, `watches/{id}/pause`, `watches/{id}/resume`; `PATCH` and `DELETE /watches/{id}` |
| `admin:audit` | admin | `GET /audit-log` |
| `admin:principals` | admin | principals, their identities, keys and invites (routes arrive with the principal CLI) |
| `admin:shares` | admin | audiences, share policies and ceilings (routes arrive with sharing) |
| `read:ui` | reader, writer, admin; never a browser session | `POST /ui/login-codes` (`ctxt ui open`) |
| `signout:ui` | browser sessions only | `DELETE /ui/session` |

`POST /api/v1/federation/push` needs `write:objects` and, on non-private instances, the key of a principal of kind `peer`; any other principal gets 403 `PEER_REQUIRED`. `/health`, `/healthz` and gRPC health are open.

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

### Fetch intranet pages

When dpkms ingests a URL (a captured page, a feed, the links a feed
carries, or a redirect from any of them), it refuses to connect to a
private-network address: loopback, `10/8`, `172.16/12`, `192.168/16`,
unique-local IPv6, link-local (including the cloud metadata address
`169.254.169.254`), CGNAT `100.64/10`, multicast and unspecified
addresses, also in their IPv4-mapped IPv6 form. The job fails with
`fetch refused: <host> is a <range> address` and is not retried. This
stops a capture, or a page it redirects to, from making dpkms reach
the services beside it.

To capture pages from your intranet, allow them by name or by range:

```yaml
fetch:
  allowed_hosts:
    - wiki.corp.example   # exact name, whatever it resolves to
  allowed_cidrs:
    - 10.20.0.0/16        # a range, or a single address
```

`fetch.allow_private: true` turns the block off entirely. Host entries
are exact names: no scheme, port or wildcard. These fetches ignore
`HTTP_PROXY` / `HTTPS_PROXY`. Calls to configured endpoints (providers,
registries, federation peers) are not affected.

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
to sync cookies. `--cookie-bridge-port N` binds exactly `N` instead, and
`0` an OS-assigned port. The extension connects to
`ws://127.0.0.1:9377`, so move the bridge only if the extension is
pointed at the new port too.

Whatever the instance's access class, the bridge accepts only WebSocket
handshakes that come from a browser extension (`chrome-extension://`,
`moz-extension://` or `safari-web-extension://` origin) and name
`127.0.0.1:<port>` or `localhost:<port>` in `Host`.
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
- **Principal**: the principal of the API key that minted the link,
  with its roles. The session ends the moment that key, or the
  principal, is revoked: the store ends it in the same transaction, and
  the next request with the cookie gets `401` and the cookie cleared. A
  key declared in `server.auth.keys` cannot mint a link.
- **Scopes**: the key's scopes narrowed to the web UI's set: `read:objects`,
  `read:inbox`, `read:feeds`, `read:jobs`, `read:registries`,
  `read:system`, `delete:objects`, `write:jobs`, `write:self`, plus
  `signout:ui`, which only sessions hold. Apart from signing itself out,
  a session never holds a scope its key lacks. A reader's session only
  reads and manages its own credentials. A writer's may also retry jobs.
  An admin's gets the whole set and nothing more. It never holds
  `read:ui`, so it cannot mint sign-in links. Every other route
  (pipelines, steps, registry changes, watches, the audit log,
  federation, the MCP mount, login links, other writes) answers `403`
  `INSUFFICIENT_SCOPE` naming the scope. `/ws/bus` keeps its own bus
  token and gRPC keeps API keys only; a session cookie opens neither.
  `GET /api/v1/whoami` with the cookie shows the session's scopes and
  times.
- **CSRF**: a request carrying the cookie must come from the instance's
  own pages. Writes need `Sec-Fetch-Site: same-origin` or a matching
  `Origin` (`403` `CROSS_ORIGIN_REQUEST`) and the header `X-Ctxt-CSRF: 1`
  (`403` `CSRF_HEADER_REQUIRED`). Reads sent by another site or another
  port on the host are refused. A request that also carries
  `Authorization` or `X-API-Key` is decided by that key first: a valid
  key lifts these rules, an invalid one is refused whatever the cookie.

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

### Pipeline overrides

`pipelines.overrides.<pipeline>` in `dpkms.yaml` changes one built-in pipeline. `dpkms config validate` checks every override against the pipelines and the installed steps, and `dpkms serve` refuses to start on the same errors.

| Key | Value |
|---|---|
| `providers.<role>` | Backend for this pipeline only: `llm`, `ocr`, `vision`, `transcription`, `diarization`, `video`, `document`. `embedding` is ignored: each embedding model's registry entry decides its provider. |
| `skip_steps` | Step names to drop. Each must be in the pipeline (`dedup` counts when `duplicates.check_similar` adds it). |
| `insert_steps[].step` | Step to insert: a built-in step, or one installed with `dpkms pipeline step install`. |
| `insert_steps[].before`, `insert_steps[].after` | A step in the pipeline once `skip_steps` and earlier inserts apply, named exactly once. Set one or neither; neither appends. |
| `insert_steps[].on_error` | `fail` (default): the object fails and nothing is stored. `continue`: log the failure and pass the object on unchanged. Installed steps only. |
| `insert_steps[].timeout` | Duration that overrides the step's SKILL.md timeout (default 30s). Installed steps only. |
| `insert_steps[].isolation` | `container` runs the step in a container. It never lowers what the step or its registry requires. Installed steps only. |
| `insert_steps[].config` | Map written to the file `DPKMS_STEP_CONFIG_PATH` names and checked against the step's `config_schema`. `config show` prints it, so keep credentials out. Installed steps only. |

Install a step before you name it here, then restart `dpkms serve`. Walkthrough: [Add a custom step (PII masking)](../admin-extensibility/custom-steps.md).

### Registry update policy

```bash
dpkms pipeline step registry autoupdate <registry-url> --enable
dpkms pipeline step registry autoupdate <registry-url> --disable
```

## Permission and isolation policy references

- No plugin system: [`docs/decisions/ADR-078-no-plugin-system.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/decisions/ADR-078-no-plugin-system.md)
- External step protocol and isolation: [`docs/decisions/ADR-058-external-step-execution-protocol.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/decisions/ADR-058-external-step-execution-protocol.md)

## Operational safeguards

1. Always run `ctxt config validate` (and `dpkms config validate` for `dpkms.yaml`) before rollout.
2. Use explicit registry auto-update policies.
3. Prefer staged pipeline changes plus test enqueue runs.
4. Track runtime flags used in production for reproducibility.

## Story alignment

- Config/admin: `US-0027`, `US-0030`, `US-0031`
- Step/pipeline permissions: `US-0029`, `US-0112`
