# Browser Authentication for the dpkms Web UI — Proposal

**Date:** 2026-09-26
**Plan:** P412
**Status:** Accepted 2026-09-26: option A, phases 0, 1 and 1b. See [Decisions](#decisions). Not yet implemented.
**Related:** P410 (web-ui), P411 (UI adapter contract; this answers its
live-stream auth question), ADR-023 (auth model), ADR-032 (entitlements)

---

## Problem

dpkms serves the React SPA at `/ui/*`. The SPA sends no credentials. On a
`private` instance that is fine, because nothing is authenticated. On a
`protected` or `public` instance every `/api/v1` call the SPA makes gets a
401. So the UI cannot be used on any remote instance. The next browser
feature, a search-graph viewer at `/ui/searchgraph/` backed by an API
endpoint, is blocked by the same gap.

We need a way for a browser to hold a credential for a remote dpkms. It
must not weaken the CLI/API auth model, and it must work for SSE, which
P411 Part B builds on.

---

## Current state (verified in code)

### Inbound auth

| Fact | Where |
|---|---|
| Three access classes: `private` (loopback, no auth), `protected`, `public` (auth mandatory). `server.public` / `--public` is deprecated shorthand for `public`. | `internal/config/config.go:534-540`, `:599-607` |
| Serve resolves the class and provider before binding. A non-private instance without credentials refuses to start. | `cmd/dpkms/cmd/serve.go:149`, `resolveInboundAuth` `:658-678` |
| Only `private` binds `127.0.0.1`. Non-private binds `0.0.0.0`. | `serve.go:400-406` |
| Provider seam: `Provider.Authenticate(Credential) → Principal{ID, Name, Provider, Roles, Meta}`. | `internal/auth/auth.go:56-104` |
| **Only `static` is implemented** (token → principal table). `oidc` and `mtls` are reserved names that fail at startup. | `internal/auth/factory.go:17-28` |
| `RequireAuth` wraps all of `/api/v1`: REST, SSE `/events`, federation push, and the MCP mount. It reads credentials from `Authorization: Bearer` or `X-API-Key` only. No cookies. | `internal/server/http/middleware_auth.go:26-74`, `server.go:112-119` |
| **dpkms has no TLS listener.** It serves plain HTTP (`httpSrv.Serve`). `r.TLS` is always nil, so "mTLS setups" do not exist today. Remote TLS must come from a reverse proxy. | `serve.go:526` |
| No trusted-proxy handling (`X-Forwarded-*`, real-IP). | grep: none |
| The principal is attached to both ctx views, `auth` and the kit policy engine. Roles reach CEL as `principal.Role` (first role only). `Principal.HasRole` has no callers. | `internal/auth/policy.go:17-31`, `auth.go:72` |
| Non-private instances disable the kit policy env-principal fallback. | `serve.go:309-317` |
| The entitlement/metering gate only exists on authenticated instances and only covers the entity surface. It is keyed by `Principal.ID`. | `serve.go:365-377`, `internal/server/http/entitlement.go:14-19` |
| **Profiles are not an authorization boundary.** The caller asserts the profile with `?profile=` (or a body field), and nothing binds a principal to a profile. | `handlers_search.go:20`, `handlers_inbox.go:26` |
| `/ui/*` static assets are served outside `/api/v1` and are unauthenticated on every class. | `server.go:91-110` |
| `/healthz` is redacted for anonymous callers on `public`. | `server.go:84-88` |

### CLI → dpkms

- The client resolves endpoints in this order: `server.urls[]` (each entry
  `{url, token}`) > `server.url` > the default. `server.token` is the
  fallback token. Tokens come only from config, never from flags.
  (`cmd/ctxt/cmd/helpers.go:103-142`)
- The CLI sends `Authorization: Bearer <token>`
  (`cmd/ctxt/cmd/server_endpoint.go:63-64`). The same token is also the
  server-side `server.auth.static.tokens[]` entry. There is no login flow,
  no refresh and no expiry.

### Web UI

- `web/ui/src/api.ts:5-18`: plain `fetch('/api/v1/...')` with no auth
  header and no 401 handling. The default `credentials: 'same-origin'`
  means same-origin **cookies would be sent automatically**.
- The SPA makes mutating calls: `DELETE /objects/{id}` and
  `POST /jobs/{id}/retry`. Its source has no `dangerouslySetInnerHTML` or
  markdown-HTML sinks. `EventSource` is not used yet. Because
  `EventSource` cannot set headers, a bearer header cannot authenticate
  SSE from a browser.

### CORS and `--dev`

- `CORS` only acts when `Host` is localhost. It always allows **any**
  `chrome-extension://`, `moz-extension://` or `safari-web-extension://`
  origin. It allows `localhost:5173` only with `--dev`
  (`middleware_cors.go:9-19, 34-86`). Remote hosts get no CORS headers,
  which is correct for a same-origin SPA.
- Vite's dev proxy forwards `/api` to `VITE_API_URL`
  (`web/ui/vite.config.ts`).

### Existing weaknesses (code reading, not probed)

1. **DNS rebinding on private instances.** Nothing checks `Host`, so a
   hostile page that rebinds its name to `127.0.0.1` can read the whole
   unauthenticated API as same-origin.
2. **Localhost CSRF on private instances.** Handlers decode JSON whatever
   the `Content-Type` (`handlers_inbox.go:28`). A cross-site
   `fetch(..., {mode:'no-cors', body: '{…}'})` sends a text/plain "simple"
   POST, which needs no preflight, so writes such as `POST /inbox` and
   `POST /capture/*` are reachable from any web page.
3. No `frame-ancestors`, `X-Frame-Options`, `nosniff` or `Referrer-Policy`
   on `/ui/*`, so the UI can be framed (clickjacking).
4. Cookies do not isolate by port. Two dpkms instances on one host
   (e.g. `:8080` and `:8081`) share a cookie jar and count as the **same
   site**, so `SameSite` alone does not separate them.

The ephemeral graph viewer already solves 1 and 3 in the CLI:
`cmd/ctxt/cmd/find_graph_viewer.go:34-38, 72-122` uses a Host allowlist
(its comment names DNS rebinding), strict CSP with
`frame-ancestors 'none'`, `X-Frame-Options: DENY`, `no-referrer`,
`nosniff`, and a 256-bit capability token in the path.

### Building blocks in kit (`hop.top/kit v0.5.0-alpha.15`)

| Available | Not available |
|---|---|
| `transport/api.Auth(AuthFunc)` middleware, `Claims{sub,tenant,scopes}`, `IdentityOf/ScopesOf` (`go/transport/api/mw_auth.go`) | cookie/session store |
| `core/identity`: Ed25519 keypair, `SignJWT` / `VerifyJWT` (`go/core/identity/jwt.go`) | CSRF middleware |
| `runtime/policy` principal binding (already used) | OIDC client / RP, device-code flow, PKCE |

The repo has no session, cookie-auth, OIDC or device-flow code either. The
cookie-bridge WebSocket is unrelated: it syncs browser-extension
cookies for scraping (`internal/server/ws/cookie_bridge.go`).

### ADR alignment

ADR-023 Layer 2 already lists "short-lived session tokens" and
`mode: none | token | session | mtls`. It rejects mandatory external JWT
issuers and passwords (alternatives 2 and 3). Any option must therefore
keep `private` auth-free and treat an external IdP as optional.

---

## Threat model

| Deployment | Who reaches the port | Main threats |
|---|---|---|
| **Localhost / private** | the local user, every local process, every web page (via the browser) | DNS rebinding, cross-site simple POST (CSRF), cross-port instance confusion, malicious extension (all extension origins allowed) |
| **Remote single-user** (VPS, home server; `protected`/`public`) | internet or LAN | credential theft (XSS, logs, URL leakage), CSRF once cookies exist, clickjacking, brute force (the security emitter already alerts on auth failures) |
| **Multi-user / tenant** (several principals on one instance) | several humans and agents | cross-principal data access. **Profiles are caller-asserted**, so no option here gives tenant isolation. That needs a separate principal→profile binding (see open questions). |
| **Behind a reverse proxy** (TLS, SSO) | the proxy, plus anyone who can reach dpkms directly (it binds `0.0.0.0`) | forged identity headers if dpkms trusts them without a proof. A **private instance behind a same-host proxy exposes the API with no auth**, because the loopback bind looks "safe". |

Browser-specific attack classes:

- **CSRF.** This only appears once the browser holds an ambient
  credential (cookie, client cert, proxy SSO cookie). Bearer-in-JS is
  immune.
- **XSS → token theft.** The UI renders ingested third-party content
  (captured pages, feeds). React escapes it today, but one rendering sink
  would expose any credential readable by JS. HttpOnly cookies limit the
  damage to "act as the user while the tab is open".
- **Token in URL.** Tokens in a URL leak through proxy and access logs,
  browser history, `Referer` and screen shares. This matters for SSE
  `?token=` and for login links.
- **Clickjacking.** Framing the UI to trick clicks on delete/retry.
- **CORS.** A permissive `Access-Control-Allow-Origin` combined with
  credentials would hand the API to other origins. Today remote hosts get
  no CORS, which is the right default. Keep it that way.

---

## Options

### A — CLI-minted one-time login link → HttpOnly session cookie

**UX.** Run `ctxt ui open [--server URL] [--profile P] [--no-browser]`.
The CLI calls `POST /api/v1/ui/login-codes` with its configured bearer
token. The server returns a single-use code (256-bit, TTL 60 s, bound to
the principal). The CLI opens `https://host/ui/auth#code=…` via
`internal/browser/launch`. The page POSTs the code to `/ui/auth/session`,
receives a cookie, strips the fragment with `history.replaceState`, and
shows "Signed in as `<principal>` on `<instance>`". On a private instance,
`ctxt ui open` just opens `/ui/`.

**Security.**

- The long-lived static token never enters the browser.
- The cookie is `HttpOnly; Secure; SameSite=Strict; Path=/`, with an
  instance-scoped name such as `__Host-dpkms_<instance>`, which fixes the
  port collision.
- Sessions have idle and absolute TTLs, a server-side revocation list and
  a logout endpoint.
- The code travels in the **fragment**, which is never sent to the
  server, never logged by proxies and absent from `Referer`. It is also
  single-use and short-lived.
- Login-CSRF (an attacker makes you sign in as them) is bounded by the
  explicit "Signed in as…" confirm step.
- `Secure` means a remote plain-HTTP deployment cannot keep a session.
  That is intended: it forces TLS at the proxy. Browsers treat
  `http://localhost` as secure, so local dev still works.

**CSRF.** Defence in depth for any cookie-authenticated request:

1. `SameSite=Strict`.
2. `Sec-Fetch-Site` must be `same-origin`, or, when that header is absent,
   `Origin` must match the instance origin. The origin includes the port,
   which covers cross-port attacks.
3. Mutations must carry a custom header (`X-Ctxt-CSRF: 1`), which forces a
   preflight that CORS refuses.

No double-submit token is needed.

**Profiles / entitlements.** The session resolves to the **same
`Principal`** as the minting token (ID and Roles, with
`Meta{"via":"session"}`), so entitlements, quotas, metering, policy CEL
and security events apply unchanged. `--profile` only sets the UI's
default profile (UX). It is not authz.

**Fit with P411.** Cookies work with `EventSource`, which settles P411's
open "live-stream auth model" question for the first-party SPA. Server-side
external adapters keep using bearer. `/capabilities` should report
`"auth": {"schemes": ["bearer","session"], "session_login": "cli"}`
instead of a single string.

**Effort: M** (about 700–1000 LOC with tests).

| Area | Change |
|---|---|
| `internal/auth/session.go` (new) | `SessionStore` (mint code, exchange, lookup, revoke, TTLs) and a composite provider: scheme `session` → store, otherwise the configured provider. The provider seam stays intact. |
| `internal/server/http/middleware_auth.go` | `credentialFromRequest` also reads the session cookie. New `RequireSameOrigin` CSRF middleware applied when scheme = session. |
| `internal/server/http/server.go` | Mount `POST /api/v1/ui/login-codes` (bearer only), `POST /ui/auth/session` (outside `/api/v1`, code → cookie), `GET /api/v1/ui/session` (whoami), `DELETE /api/v1/ui/session` (logout). Refuse session credentials on federation push, MCP and `/ws/bus`. Add security headers on `/ui/*`. |
| `cmd/dpkms/cmd/serve.go`, `internal/config` | Wrap the provider when non-private. Add `server.ui.session.{idle_ttl,max_ttl}`. |
| `cmd/ctxt/cmd/ui_open.go` (new) | Uses `serverEndpoint`/`serverDo` and `launch.Opener`. `--no-browser` prints the URL. |
| `web/ui/src/api.ts`, new `/auth` route | CSRF header, 401 → "Run `ctxt ui open`" screen, exchange page. The search-graph viewer uses the same `fetch` defaults. |

### B — Paste a bearer token into the UI

**UX.** On a 401 the SPA shows a token field, stores the token, and sends
`Authorization: Bearer` on every call.

**Storage trade-offs.**

| Store | Survives reload | XSS-readable | Notes |
|---|---|---|---|
| JS memory | no | yes (while open) | re-paste every reload |
| `sessionStorage` | per tab | yes | lost on new tab |
| `localStorage` | yes | yes | persists the **long-lived, full-power, CLI-grade** token on disk in the browser profile |

**Security.** A single XSS exfiltrates a static token that also works for
CLI, gRPC and MCP, with no expiry, until the operator edits config.
`EventSource` cannot send the header, so SSE needs `?token=` in the URL,
which leaks. No CSRF, because nothing is ambient. Users will paste tokens
into shared machines.

**Profiles / entitlements.** Unchanged (same principal).

**Fit with P411.** Poor. It breaks browser SSE unless the token goes in
the URL.

**Effort: S** (`api.ts`, a sign-in component; no server change).

**B′ (variant): paste once, exchange for a cookie.** The pasted token is
POSTed once to `/ui/auth/session` and never stored in JS. This reuses A's
server half. It is useful where no CLI exists (a phone, a borrowed
laptop). Effort **S on top of A**.

### C — Delegate to OIDC or a reverse proxy

| Variant | How | Code | Security notes |
|---|---|---|---|
| **C1 Proxy injects bearer** | oauth2-proxy, Cloudflare Access, Authelia or Tailscale Serve authenticate the human, then add `Authorization: Bearer <static token>` upstream | **none; works today**. Docs only. | One principal for everyone behind the proxy (fine for single-user). dpkms must be reachable only through the proxy (firewall or bind). The proxy's own SSO cookie is usually `SameSite=Lax`, so dpkms still needs A's Origin checks for mutations. |
| **C2 Trusted identity assertion** | new `proxy` provider maps a **verifiable** assertion (e.g. `Cf-Access-Jwt-Assertion` checked against the IdP JWKS, or a shared-secret-signed header) → `Principal` | **M** | Never trust bare `X-Forwarded-User`: dpkms binds `0.0.0.0` on non-private instances, so anyone who reaches the port can forge it. Gives per-user principals. |
| **C3 Native OIDC** | implement the reserved `oidc` provider: auth-code + PKCE for the browser (the session cookie from A becomes the RP session), JWT verification for API bearers | **L–XL** | Needs an IdP. This is optional per ADR-023. Adds discovery, JWKS rotation, refresh and logout, plus claims → principal/roles mapping. |

**Profiles / entitlements.** Principal ID = the OIDC `sub` or asserted
user. Entitlement rows must be keyed to it, so operators need a mapping
table. **CSRF**: same as A for any cookie. **Fit with P411**: transparent,
since the browser holds a cookie and the stream works.

### D — mTLS only

**UX.** Install a client certificate in every browser and device, then
answer the certificate-picker prompt. This is poor on mobile and in
managed browsers.

**Security.** There is no bearer secret in JS or the URL, and the
credential cannot be exfiltrated by XSS (it lives in the key store).
Browsers still attach client certs to cross-site requests, so **CSRF
still applies**, and A's Origin checks are still needed.

**Premise correction.** dpkms has **no TLS listener** and the `mtls`
provider is reserved-only. Implementing D means either:

- (a) adding a TLS listener with a client CA to dpkms and a provider
  mapping cert SAN/CN → Principal, or
- (b) terminating at a proxy that forwards the verified cert, which is the
  C2 trust problem again.

**Effort: L.** It aligns with the device-sync identity analysis
(`docs/analysis/2026-08-25-device-sync-identity-transport.md`), which
plans mTLS on device certs behind the same `server.auth` seam. Browser
certs could reuse that PKI later.

**Profiles / entitlements.** Principal = the cert identity, and
entitlements are keyed to it. **Fit with P411**: transparent.

### Baseline — SSH tunnel to a private instance

`ssh -L 8080:127.0.0.1:8080 host` gives the remote UI with no code and no
auth. It works today for single-user setups. The catch: the instance must
stay `private`, so other CLI clients must tunnel too.

---

## Comparison

| | A login link → cookie | B paste token | C1 proxy injects | C2 proxy assertion | C3 native OIDC | D mTLS |
|---|---|---|---|---|---|---|
| UX | one command, zero typing | copy/paste a secret | SSO page | SSO page | SSO page | cert install per device |
| Needs the CLI | yes (B′ removes this) | no | no | no | no | no |
| XSS blast radius | session while the tab is open | **full static token, no expiry** | proxy session | proxy session | session | none (key store) |
| Token in URL | code in fragment, single-use, 60 s | `?token=` for SSE | no | no | auth code (standard) | no |
| CSRF exposure | yes, mitigated (Strict + Origin + header) | none | yes (dpkms must check Origin) | yes | yes | yes |
| Browser SSE | yes | only via URL token | yes | yes | yes | yes |
| Per-user principals | yes (per token) | yes | **no** (one token) | yes | yes | yes |
| External dependency | none | none | proxy + IdP | proxy + IdP | IdP | PKI |
| ADR-023 fit | Layer 2 "session" | Layer 2 token | ops pattern | new provider | Layer 3 (optional) | `mtls` mode |
| Effort | **M** | S | none (docs) | M | L–XL | L |

---

## Recommendation

**Adopt A as the first-party browser path. Document C1 now. Keep C2, C3
and D as later providers behind the existing seam. Reject B as specified
(offer B′ instead).**

Why: A is the only option that works with no external dependency, keeps
the long-lived CLI token out of the browser, makes SSE work (P411), and
reuses the existing `Provider → Principal → entitlement/policy/metering`
pipeline unchanged. The CSRF work it requires is also what C and D need,
so it is not wasted if the owner later adds OIDC or mTLS.

### Phased plan

| Phase | Scope | Effort | Unblocks |
|---|---|---|---|
| **0: Hardening (any option)** | Host allowlist on every class (anti-rebinding; configurable `server.allowed_hosts` for proxies). Origin / `Sec-Fetch-Site` check on mutating requests to private instances. Security headers on `/ui/*` (CSP `frame-ancestors 'none'`, XFO DENY, nosniff, `no-referrer`), reusing the `find_graph_viewer.go` pattern. `api.ts` 401 handling. | S | Closes the existing private-instance holes |
| **1: Session cookie (A)** | Session store and composite provider, login-code and exchange endpoints, CSRF middleware, `ctxt ui open`, SPA auth route. Session credentials refused on federation, MCP and `/ws/bus`. | M | Remote UI and the search-graph viewer |
| **1b: Docs (C1)** | Operator guide: proxy injects bearer; firewall dpkms to the proxy; warn against a private instance behind a proxy. | S | SSO-fronted single-user deployments today |
| **2: B′ paste-to-cookie** | Sign-in form that exchanges a pasted token for a cookie; token never stored. | S | Devices without the CLI |
| **3: P411 alignment** | `/capabilities.auth` object; the stream accepts session or bearer. | S | P411 Phase 2 |
| **Later** | C2 verifiable proxy assertion → C3 native OIDC → D mTLS (with device-sync PKI), each one more `server.auth` provider. | M / L–XL / L | Multi-user SSO, enterprise |

---

## Decisions

Recorded 2026-09-26.

| # | Question | Decision |
|---|---|---|
| — | Browser path | **A** (CLI-minted login link → session cookie), with phase 0 hardening and 1b proxy docs. |
| 1 | Session scope | **Reduced "ui" scope**: read and search plus the SPA's own mutations; no pipeline, step, registry, federation or admin routes. Needs a scope → route table (since 2026-09-27: the route-scope model's table, see "One scope system"). |
| 2 | Session persistence | **Persisted** in the storage driver, listable and revocable; idle 12 h, max 7 d. |
| 3 | Revocation coupling | **Yes**: a session ends when the static token that minted it is removed from config (token hash stored per session). |
| 5 | Private-instance hardening | **On by default**; extra hostnames via `server.allowed_hosts`. |
| — | Registry list in the "ui" scope | **Allowed**: `GET /steps/registries` (the web UI Registry page); registry writes stay token-only. |
| — | Watches in the "ui" scope | **Token-only**: `GET /watches` exposes host filesystem paths. |
| — | One scope system (2026-09-27) | **The "ui" scope is expressed in the route-scope model** (ADR-023 `verb:resource` scopes, role bundles, `GET /api/v1/whoami`). No separate route table: every `/api/v1` route declares one scope, and a session's effective scopes are its minting principal's **intersected** with a fixed ui set (`read:objects`, `read:inbox`, `read:feeds`, `read:jobs`, `read:registries`, `read:system`, `delete:objects`, `write:jobs`, `signout:ui`); a reader token yields a read-only session, an admin token no more than the set. To express the set, `read:mcp` (MCP mount), `read:pipelines` (pipeline and step reads), `read:watches`, `delete:aliases` and `delete:searches` split from coarser scopes (role reach unchanged); `read:ui` (mint login codes) is a read scope, so every role holds it, and it is never in the ui set, so a session cannot mint; `signout:ui` (sign out) sits only in the ui set, in no role bundle, and a session holds it whatever its token. The reader role stays `read:*` only; `delete:aliases` and `delete:searches` stay admin-only. Tests pin the bundles, the ui set, the routes it reaches, and that it never holds `read:ui`. One whoami: `GET /api/v1/whoami` adds `via` and the session; `/api/v1/ui/session` keeps only `DELETE` (sign out). |
| 4, 6, 7, 8 | B′, tenant isolation, extension CORS, plain-HTTP remote | Open. |

## Open questions for the owner

Questions 1, 2, 3 and 5 are decided above.

1. **Session scope.** Should a browser session carry the minting
   principal's **full** rights, or a reduced "ui" scope (read plus the
   SPA's mutations, no pipeline/step/registry admin)? A reduced scope
   needs a scope → route table.
2. **Session persistence.** In-memory (a restart logs everyone out;
   simplest) or stored in the storage driver (survives restarts; enables
   `dpkms session list/revoke`)? Proposed TTLs: idle 12 h, max 7 d.
3. **Revocation coupling.** When a static token is removed from config,
   should sessions it minted die immediately? That means storing a
   token-hash per session and re-checking it on each request.
4. **B′ in scope for phase 1 or later?** It matters for phone access.
5. **Private-instance hardening (phase 0).** A Host allowlist and Origin
   check on private instances can break unusual local setups (custom
   hostnames, other local tools POSTing without `Origin`). Should they be
   on by default, with an opt-out?
6. **Tenant isolation.** Profiles are caller-asserted today, so
   multi-user instances have no per-profile isolation under **any** option
   here. Should a principal → allowed-profiles binding (P411 assumes one
   exists: "the token's scope pins which `profile_id`s") be designed as
   its own proposal?
7. **Extension CORS.** Every extension origin is allowed on localhost.
   Should it be pinned to the ctxt extension's IDs?
8. **Plain-HTTP remote.** Should serve refuse or warn when a non-private
   instance is reached over plain HTTP (no proxy TLS)? A's `Secure` cookie
   would silently fail there.

---

**Status: Accepted** (option A, phases 0, 1, 1b). Not yet implemented.
