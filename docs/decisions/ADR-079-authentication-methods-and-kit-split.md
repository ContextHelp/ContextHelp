# ADR-079 – Authentication Methods, Principals and the kit/ctxt Split

> **Status:** Accepted (2026-10-14; decisions D1–D15 as recommended in the plan; amended the same day after review, D16–D17)
> **Date:** 2026-10-14
> **Author:** jadb
> **Applies to:** dPKMS, ctxt
> **Extends:** ADR-023 (authentication and authorization model)
> **References:** ADR-031 (nodes and cloud boundary), ADR-033 (team RBAC over scopes), ADR-064 (federation), ADR-074 (same-owner device sync), ADR-077 (ctxt is a pure API client), ADR-078 (no plugin system), ADR-080 (sharing and per-viewer visibility)
> **Plan:** [`docs/plans/2026-10-14-auth-and-sharing.md`](../plans/2026-10-14-auth-and-sharing.md)

---

## Context

dpkms authenticates every inbound request on a `protected` or `public` instance, and nothing on a `private` one (`server.access`, `internal/config/config.go`). What exists today:

| Piece | Where | State |
|---|---|---|
| `auth.Provider` interface: `Authenticate(ctx, Credential) (*Principal, error)` | `internal/auth/auth.go` | Implemented |
| `static` provider: tokens with roles listed in `server.auth.static.tokens` | `internal/auth/static.go` | Implemented. No expiry, no rotation, no revocation except editing the config. |
| `oidc`, `mtls` provider names | `internal/auth/auth.go`, `factory.go` | Reserved; `FromConfig` returns "not yet implemented" |
| Roles `admin`/`writer`/`reader` expanded into 28 `verb:resource` scopes; every route and RPC declares one | `internal/auth/scope.go`, `internal/server/http/routes.go`, `internal/server/grpc/scope.go` | Implemented |
| Web UI sessions: the CLI mints a 60 s login code, the browser exchanges it for a `__Host-` cookie; scopes = token scopes ∩ `UISessionScopes` | `internal/auth/session.go`, `internal/server/http/ui_session.go`, migration `041_ui_sessions.sql` | Implemented |
| Reverse proxy that injects one static token | `docs/manual/operations/reverse-proxy.md` | Documented; everyone behind the proxy is one principal |
| Federation push token | `federation.token`, `handlers_federation.go` | Implemented; must also be a writer static token |

Only one identity backend can be configured (`server.auth.provider`). A person can only sign in to the web UI through a CLI that already holds a token. dpkms cannot tell people apart behind a proxy. Nothing has an expiry. ADR-023's personal access tokens (`ctxt auth token create`) were never built.

Sharing (ADR-080) needs dpkms to know who is calling. The driving case is an owner who shares a contact network with work colleagues and with partners. Those people have no ctxt CLI and no token. They need to sign in from a browser, and the owner needs to grant access, see who has it, and take it away again. Agents need their own scoped, expiring keys.

Constraints:

- **Local-first, swappable providers.** Every method in the core set works with no external identity provider and no outbound network. Cloud and enterprise IdPs are opt-in config.
- **ADR-023 still stands.** Capabilities stay `verb:resource` scopes. Passwords stay rejected. An external JWT issuer is never required.
- **kit is the convention authority** (`hop.top/kit`). Generic mechanisms belong there so other hop-top tools get the same behaviour. ctxt keeps what is specific to a knowledge store.
- **No backward compatibility.** ctxt is unreleased, so config keys can be replaced outright.

kit already ships (`poly-kit/go`, `next`): `transport/authn` (`NewJWT`, `NewJWKS`, `NewOIDC` verifiers that only accept asymmetric algorithms; `APIKeys` over a `kv.Store` with SHA-256 hashing, expiry and soft revoke), `transport/api` (`Auth` middleware, `ClientCertAuth` for mTLS, `ClientAddress` with trusted proxies, `OriginCheck`, `HostCheck`), `cmdsurface.MountOAuth` (state and callback only, no code exchange), `cmdsurface.SignedIssuer` (single-use HMAC URLs), and a hash-chained audit log. kit has no authenticator chain, no identity from proxy headers, no WebAuthn, no OAuth code exchange with PKCE, no session store and no device identity. ctxt imports none of `transport/authn`, `transport/api` or `core/identity` today.

---

## Decision

**dpkms authenticates a request by trying an ordered chain of methods. Each method resolves to a principal in an instance-local principal registry. The core methods need no external service: owner-local, API keys, invite and magic links, passkeys, and trusted proxy identity. mTLS, enterprise OIDC and social login come later as opt-in methods. Authentication only ever produces a principal. What that principal may do (scopes) and see (share policies, ADR-080) is decided after authentication, in dpkms.**

### 1. Principals

A principal is a row in the instance database, never just a config entry.

| Kind | Who | Typical methods | Notes |
|---|---|---|---|
| `owner` | The person the instance belongs to. Exactly one per instance. | owner-local, passkey, API key, proxy | Holds every scope. Bypasses share policies (ADR-080) unless a request opts in to a preview. |
| `person` | A colleague, partner or family member the owner invited | invite link, then passkey or proxy identity; magic link for sign-in again | Roles `[reader]` by default, so every read route is callable, but the viewer (ADR-080) is empty until a grant applies. |
| `device` | An owned device (capture, sync) | API key now; mTLS / device certificate under ADR-074 | Writer scopes. |
| `peer` | A federation target (ADR-064) | API key | Replaces the separate `federation.token` for inbound pushes. The outbound credential this instance presents to its targets stays in `federation.targets[]`. |

**Agents are keys, not principals.** An AI agent or script gets an API key on the principal it acts for, bound to a share policy that narrows that principal's view (ADR-080 §2). It can never hold more scopes or see more data than that principal. Revoking the key ends the agent, and the access log attributes its reads by key. A separate `agent` principal kind with a parent link was considered and dropped: it duplicates what a key with a policy already expresses, and it leaves open whether grants flow through the parent.

On a `private` instance every request is the owner (today's `LocalPrincipal`, whose ID `local` becomes `owner`). On `protected` and `public` instances an unauthenticated request is anonymous and reaches only the routes that are already open.

A principal can have several **identities**, one per credential: a passkey, a proxy login, an OIDC `(issuer, subject)`, a client certificate, or an API key. Every identity, keys included, is a row in `principal_identities`, so revoking a principal cascades to all of its credentials and sessions in one place. An identity is bound to a principal only by an explicit act: the owner creates it, the principal adds it to itself under `write:self` (§5), or the invitee accepts an invite. dpkms never links identities by matching email addresses across methods, because a claimed email is not proof of the same person.

### 2. Methods

Phases refer to the [plan](../plans/2026-10-14-auth-and-sharing.md).

| Method | Tier | Credential | Who it serves | Built on |
|---|---|---|---|---|
| **Owner-local** | Core (exists) | None; loopback bind on `private` | Owner on the host | Existing `private` access class |
| **API keys** (replace static tokens) | Core, Phase 1 | `Authorization: Bearer` or `X-API-Key`; hashed at rest; expiry; revoke; scopes; optional share policy | CLI, agents, devices, peers, scripts | kit `authn.APIKeys` over a ctxt `kv.Store` |
| **Invite links** | Core, Phase 1 | Single-use URL `…/ui/invite#t=<ticket>`; the secret is in the fragment so it never reaches logs, proxies or Referer (as login codes already work) | First sign-in of a `person` | kit `SignedIssuer`-style tickets (proposal K4) |
| **Magic links** | Core, Phase 3 (owner-minted sign-in links from Phase 1) | Single-use, 10-minute ticket. The owner can mint it and copy it, or dpkms can email it when an SMTP provider is configured (opt-in). | Signing in again without a passkey; recovery | Same ticket store as invites |
| **Passkeys (WebAuthn)** | Core, Phase 3 | Discoverable credential; browser ceremony | People in the web UI, owner included | `github.com/go-webauthn/webauthn` (probed: `BeginDiscoverableLogin` works; `http://localhost` origins accepted) through kit proposal K3 |
| **Trusted proxy identity** | Core, Phase 3 | Identity header set by a proxy whose address is in `trusted_proxies`. Presets: `tailscale` (`Tailscale-User-Login`, `Tailscale-User-Name`) and `forward_auth` (`X-Forwarded-User`/`Remote-User`, `X-Forwarded-Email`, as oauth2-proxy and Authelia send them) | Tailnets, existing SSO proxies | kit `ClientAddress` + proposal K2 |
| **Browser sessions** | Core (exists, generalized) | `__Host-` cookie + `X-Ctxt-CSRF` | Every interactive method ends in one | ctxt `ui_sessions`; kit proposal K5 later |
| **mTLS** | Later, opt-in | Client certificate; principal from SAN/CN | Devices, ADR-074 hub transport, service-to-service | kit `ClientCertAuth`, `auth.mtls.*` |
| **Enterprise OIDC** | Later, opt-in | Authorization code + PKCE sign-in for browsers; IdP-issued JWT bearer tokens for machines | Companies that run Okta, Entra ID, Keycloak, Authentik | kit `authn.NewOIDC` + proposal K6 |
| **Social login** | Later, opt-in | Same flow as OIDC (Google is OIDC; GitHub is plain OAuth 2.0 and needs a userinfo call) | Invitees who prefer a social account | Proposal K6 |
| **Passwords** | Rejected | | | ADR-023 rejected them; passkeys cover the same need without stored secrets |

Two kinds of method appear above. **Authenticators** resolve a principal on every request from something the request carries: an API key, a session cookie, a proxy header, a client certificate, an IdP-issued JWT. **Sign-in ceremonies** establish a browser session once: an invite link, a magic link, a passkey ceremony, an OIDC code flow. A ceremony is never tried on an ordinary request. It ends by minting a session, and the `session` authenticator takes over from there.

Every method is optional except owner-local and API keys. An instance enables the authenticators and ceremonies it wants:

```yaml
server:
  access: protected
  public_url: https://dpkms.tail1234.ts.net   # link base, passkey RP ID + origin; required by every link and passkey sign-in
  auth:
    methods: [api_key, session, proxy]        # authenticators; evaluation order in §3
    sign_in: [invite, link, passkey]          # ceremonies allowed to mint a session
    proxy:
      preset: tailscale                 # tailscale | forward_auth | custom
      trusted_proxies: [127.0.0.1/32]   # tailscale serve forwards from loopback
      unknown_identity: deny            # deny | request (owner approves)
    passkey:
      user_verification: preferred
    links:
      invite_ttl: 168h
      magic_link_ttl: 10m
      email: {}                         # absent = never send mail; links are copied by hand
```

`server.auth.provider` and `server.auth.static` are removed. `federation.token` is replaced by a `peer` API key.

### 3. The chain

Each authenticator in `methods` returns `matched`, `no credential` or `refused`:

- **Matched**: stop. The request runs as that principal.
- **No credential**: try the next method.
- **Refused** (a credential was presented and is wrong, expired or revoked): stop with 401. dpkms never falls through to a weaker method once a credential has been presented, so a bad bearer token cannot be rescued by a proxy header.

**Order.** Authenticators that carry an explicit credential (`api_key`, `mtls`, `jwt`) always run before ambient ones (`session`, `proxy`), whatever order `methods` lists them in. A request with both a bearer key and a proxy identity therefore runs as the key's principal, which lets an agent behind a tailnet use its own narrower key. Within each group the config order applies.

**What counts as refused.** A carried credential that is wrong, expired or revoked: a bad or expired bearer key, a bad certificate, an invalid JWT. A session cookie is different. Cookies outlive their sessions routinely (idle timeout, restart, revocation), so an expired or unknown cookie is **no credential**: the chain clears it and moves on, and a proxy identity or a fresh sign-in can take over. Only a cookie that fails its integrity check is refused.

**Ambient identity follows the browser rules.** A proxy identity arrives on every request the browser sends through the proxy, including requests a hostile site triggers, exactly as a cookie does. Requests authenticated by `session` or `proxy` therefore get the same treatment: unsafe methods must pass the Origin check (`Sec-Fetch-Site`, falling back to Origin against Host, as `ui_session.go` does today) and carry the CSRF header, and scopes are `UISessionScopes ∩ principal scopes`. A bearer key on the same request lifts both restrictions, because it ran first.

Proxy headers are read only when the TCP peer is in `trusted_proxies`. They are ignored, and logged as a security event, when anyone else sends them.

`tailscale serve` forwards from loopback, so with the `tailscale` preset every local process on the dpkms host can also set the header, and so claim any bound login, the owner's included. On a single-user host that is the same trust the `private` access class already gives loopback. On a host shared with other users, use a sign-in proxy that authenticates to dpkms with an API key of its own, or (later) a `tailscale` preset that asks the local Tailscale API who the peer is (WhoIs) instead of trusting a header.

**Ceremonies.** Redeeming an invite or magic link, exchanging a login code and finishing a passkey ceremony are the only ways to mint a session. Each is rate limited per client address. A redemption that arrives on a request already carrying a session is refused: the caller signs out first, so a ticket can never bind to a principal other than the one it was issued for.

### 4. Bootstrap and owner recovery

- Every instance database has exactly one `owner` principal, created by the migration that adds the principal registry.
- `dpkms auth key create --principal owner` runs on the dpkms host and prints a new owner API key once; only its hash is stored. It opens the store directly, as the existing `dpkms session list|revoke` commands do. It is both bootstrap and recovery: filesystem access to the host is the root of trust, as ADR-023 Layer 1 already assumes. (`dpkms key` already names the instance signing-key commands, so the new group is `dpkms auth`.)
- A `protected` or `public` instance with no active owner credential (an unexpired, unrevoked API key, passkey or identity bound to `owner`) refuses to start and names that command.
- For infrastructure as code, `server.auth.keys[]` may declare keys **by hash only** (`sha256:<hex>`, principal, roles, optional expiry). Plaintext keys in config are refused.
- `server.public_url` is required whenever `sign_in` contains a link or passkey method: it is the base of every invite and magic link and the passkey origin. Validation refuses the config without it.
- Upgrading an existing `protected` instance: run `dpkms auth key create --principal owner` on the host before the new build starts, then replace the static tokens in CLI configs and proxies with the new key. The old tokens stop working at first start.

### 5. Scopes stay; roles stay bundles

Authentication produces a principal. Authorization is decided afterwards and unchanged in kind. Scopes are the effective capability set and every route declares one. `admin`/`writer`/`reader` remain fixed bundles. ADR-033 team roles are still a convenience that compiles down to scopes. Two additions:

- `admin:principals` lets a principal manage principals, identities, keys and invites. `admin:shares` lets it manage audiences, share policies and ceilings (ADR-080). Both sit in the `admin` bundle, which only `owner` holds by default.
- `write:self` lets a principal manage its own credentials and sessions: create and revoke its own API keys, register and remove its own passkeys, list and end its own sessions. Every role bundle includes it, and a route under it checks that the target belongs to the caller. Without it a `person` could never register the passkey the invite flow asks for.
- A browser session's scopes are `UISessionScopes ∩ principal scopes`, as today, whichever method signed the browser in. Proxy-identified requests get the same intersection (§3).

### 6. kit/ctxt split

The rule: a mechanism that verifies "who is this" and would work the same way in any hop-top service goes to kit. Anything that knows about knowledge objects, audiences, owners or the web UI stays in ctxt.

| Concern | kit | ctxt (dpkms) |
|---|---|---|
| JWT/JWKS/OIDC token verification | Exists: `authn.NewJWT/NewJWKS/NewOIDC` | Configures issuers, maps claims to principals |
| API key minting, hashing, expiry, revoke | Exists: `authn.APIKeys`; K1 adds labels and last-used | Implements `kv.Store` over the instance DB (SQLite and Postgres); the identity row binds a key to its principal and share policy |
| Ordered authenticator chain | **K2** `api.Chain`: carried methods before ambient, expired ambient credential = absent, browser rules on ambient identity | Chooses methods from `server.auth.methods` |
| Identity from trusted proxy headers | **K2** `api.ProxyIdentity` with presets, marked ambient | Maps the login to a principal; `unknown_identity` policy |
| WebAuthn ceremonies | **K3** `authn/passkey` over go-webauthn with a credential-store interface | Stores credentials per principal; UI pages |
| Single-use login tickets (invite, magic link) | **K4** ticket issuer and store interface, built on `SignedIssuer` + `NonceStore` | What an invite grants (audiences); delivery |
| Session cookie + CSRF primitive | **K5** `transport/session` extracted from ctxt's `ui_sessions` design | Session kinds and scope sets |
| OAuth 2.0 / OIDC sign-in (code + PKCE + id_token) | **K6** completes `MountOAuth` | Provider config; allow-lists |
| mTLS principal extraction | Exists: `api.ClientCertAuth` | Device and peer mapping (ADR-074) |
| Principal registry, owner, kinds, identity linking | — | ctxt |
| Scopes, role bundles, route→scope table | Convention: `verb:resource`, RFC 6750 403 | ctxt's scope list |
| Audiences, share policies, masking, read audit | — | ctxt (ADR-080) |
| Audit sink | Exists: hash-chained `security` audit log | ctxt writes auth and read events (ADR-080 §8) |

The proposals to hand to kit (K1–K6) are listed under [Implementation Notes](#proposals-for-kit).

---

## Rationale

- **A principal registry, not a token table.** Sharing needs to know *who* someone is across devices and methods, and to revoke a person in one place. Tokens in config cannot do that: they have no expiry or revocation, and they cannot describe a person who has a passkey on a phone and a tailnet login on a laptop.
- **Passkeys and links rather than passwords.** The people being invited are colleagues and partners who will never install a CLI. Passkeys give them phishing-resistant sign-in with nothing secret stored on the server. Invite and magic links need no mail server: the owner can paste the link into any channel, and email delivery is an opt-in provider. ADR-023 already rejected passwords.
- **Trusted proxy identity is cheap and covers real deployments.** The owner's own instance runs behind `tailscale serve`. Tailscale strips client-supplied `Tailscale-User-*` headers and sets them for tailnet users and for external users the node is shared with. It does not set them for tagged devices or Funnel traffic ([Tailscale Serve docs](https://tailscale.com/kb/1312/serve)). Sharing a node with a partner's tailnet therefore already gives dpkms a verified identity, with no new secret. oauth2-proxy, Authelia and Cloudflare Access fit the same shape.
- **A chain instead of one provider.** Real instances mix callers: the owner's CLI uses a key, colleagues use passkeys, agents use keys, devices may use certificates. kit's serve contract allows one `auth.mode` per service. dpkms has its own server and needs several at once. Stopping on the first refusal keeps the chain from turning into "try everything until something accepts".
- **OIDC is later, not never.** It is how enterprises will want to bring their own identities (ADR-031 cloud SSO). It is not needed for the contact-sharing case. It drags in client registration, secrets and an always-reachable IdP, which is the opposite of local-first. kit already verifies OIDC tokens, so machine-to-machine JWTs can arrive early without the browser sign-in flow.
- **Generic pieces in kit.** Every hop-top service that serves HTTP will need the chain, proxy identity, passkeys, tickets and sessions. Writing them once in kit keeps the same header handling, the same refusal codes and the same cookie hardening everywhere. ctxt already built the session hardening (`__Host-` prefix, per-host cookie name, fragment-delivered codes, CSRF header plus `Sec-Fetch-Site`), so it is the natural seed for K5.

### Alternatives considered

| Alternative | Why not |
|---|---|
| OIDC only (delegate everything to an IdP) | Breaks local-first: no IdP means no sharing. Forces partners onto the owner's IdP. ADR-023 rejected a required external issuer. |
| Keep static tokens and add one token per invitee | No expiry or revocation without a restart. Secrets in config. No browser sign-in for people without a CLI. One person on two devices is two principals. |
| Proxy-only (oauth2-proxy in front of everything) | Works for a tailnet, but every deployment then needs a proxy. Agents and the CLI still need keys. dpkms still needs a principal registry to attach grants to. |
| Passwords + TOTP | Stored secrets, reset flows and phishing. Rejected in ADR-023; passkeys are strictly better for invited users. |
| Self-issued JWTs (kit `core/identity.SignJWT`) as the API credential | Stateless JWTs cannot be revoked without a deny list, and dpkms already holds state. Opaque keys with a DB lookup give revocation for free. JWT verification stays available for federated IdP tokens. |
| Build it all in ctxt | Duplicates work every hop-top service needs. Contradicts kit as convention authority. |

---

## Consequences

### Positive

- Colleagues and partners can sign in from a browser with no CLI and no IdP, which unblocks ADR-080.
- Every credential can expire and be revoked. Every request is attributable to a principal.
- Agents get their own narrower keys instead of sharing the owner's token.
- Federation and devices use the same key machinery. One fewer bespoke token.
- kit gains reusable pieces, and ctxt drops hand-rolled ones.

### Negative

- More attack surface: WebAuthn, ticket redemption and header trust all need careful tests. A misconfigured `trusted_proxies` lets anyone who can reach the port claim any identity. Validation must refuse `0.0.0.0/0`, and the docs must show the risk.
- Passkeys need a stable origin. `server.public_url` becomes required for passkeys, and changing the host name invalidates registered credentials.
- The principal registry is new state. It replicates with ADR-074 device sync only if the sync protocol carries it. Until then each instance has its own principals.
- Six kit proposals create a dependency. Phase 1 needs none of them: ctxt wraps `APIKeys` directly and builds the chain and tickets behind the K2 and K4 shapes.

### Neutral or Considerations

- `private` stays unauthenticated. Sharing requires `protected` (or `public`).
- Static config tokens disappear, so docker-compose examples and the reverse-proxy guide must switch to hashed keys, and every running `protected` instance needs an owner key created on its host before the new build starts (§4).
- Email delivery is a provider role: absent by default, SMTP opt-in, never a cloud default.

---

## Implementation Notes

### Schema sketch (instance DB, SQLite and Postgres)

```sql
CREATE TABLE principals (
  id            TEXT PRIMARY KEY,           -- 'owner' or 'prn_<ulid>'
  kind          TEXT NOT NULL,              -- owner|person|device|peer
  display_name  TEXT NOT NULL,
  roles         TEXT NOT NULL DEFAULT '["reader"]', -- JSON array: admin|writer|reader
  status        TEXT NOT NULL DEFAULT 'active',  -- active|suspended|revoked
  created_at    TIMESTAMP NOT NULL,
  revoked_at    TIMESTAMP
);
CREATE TABLE principal_identities (         -- one row per credential, API keys included
  id            TEXT PRIMARY KEY,
  principal_id  TEXT NOT NULL REFERENCES principals(id),
  method        TEXT NOT NULL,              -- api_key|passkey|proxy|oidc|mtls
  issuer        TEXT NOT NULL DEFAULT '',   -- proxy preset, OIDC issuer, CA fingerprint
  subject       TEXT NOT NULL,              -- kit key id, passkey credential id, tailscale login, OIDC sub, cert SAN
  name          TEXT NOT NULL DEFAULT '',   -- "laptop CLI", "assistant bot", "phone passkey"
  created_at    TIMESTAMP NOT NULL,
  revoked_at    TIMESTAMP,                  -- set together with the kit key record for api_key rows
  last_used_at  TIMESTAMP,                  -- touched at most once a minute
  UNIQUE (method, issuer, subject)
);
-- ADR-080 (Phase 2) adds: share_policy_id TEXT REFERENCES share_policies(id)
--   narrows the principal's view for requests made with this identity; never widens it.
CREATE TABLE passkey_credentials (          -- K3 store interface; each row also has an identity row
  credential_id BLOB PRIMARY KEY,
  principal_id  TEXT NOT NULL REFERENCES principals(id),
  public_key    BLOB NOT NULL,
  sign_count    INTEGER NOT NULL,
  transports    TEXT NOT NULL DEFAULT '[]',
  name          TEXT NOT NULL DEFAULT '',
  created_at    TIMESTAMP NOT NULL,
  last_used_at  TIMESTAMP
);
CREATE TABLE auth_tickets (                 -- invites + magic links (K4)
  hash          TEXT PRIMARY KEY,           -- sha256 of the secret
  kind          TEXT NOT NULL,              -- invite|magic_link
  principal_id  TEXT REFERENCES principals(id), -- null for an invite not yet accepted
  grants        TEXT NOT NULL DEFAULT '{}', -- invite: audiences, roles, expires_at of the membership
  created_by    TEXT NOT NULL REFERENCES principals(id),
  created_at    TIMESTAMP NOT NULL,
  expires_at    TIMESTAMP NOT NULL,
  redeemed_at   TIMESTAMP
);
-- API keys: kit APIKeys records (hash, expiry, scopes, revoked) live in a kv table
-- keyed 'apikey/<id>'. The identity row (method api_key, subject <id>) carries the
-- name, the share-policy binding and last-used. There is no side table.
```

`ui_sessions.principal_id` already exists. `ui_sessions.token_hash` (session dies when its static token is removed) becomes `identity_id REFERENCES principal_identities(id)`: the session dies when the identity or principal that minted it is revoked, whichever method signed the browser in.

### CLI surface (ctxt, over the API; ADR-077)

| Command | Scope |
|---|---|
| `ctxt auth whoami` | `read:system` (exists as `GET /api/v1/whoami`) |
| `ctxt auth key create --name --expires --scopes --share-policy` / `list` / `revoke` | Own keys: `write:self`; others' keys: `admin:principals` |
| `ctxt auth identity list` / `remove` (own passkeys and proxy logins), `ctxt auth session list` / `end` | `write:self` |
| `ctxt principal list|show|suspend|revoke` | `admin:principals` |
| `ctxt principal identity add|list|remove <principal>` (bind a proxy login or OIDC subject by hand) | `admin:principals` |
| `ctxt principal link <principal>` → single-use sign-in link | `admin:principals` |
| `ctxt share invite --audience <a> --expires 7d` → prints the link | `admin:shares` (ADR-080) |
| `dpkms auth key create --principal owner` (host only, direct store) | Host filesystem |

### Proposals for kit

Each is independent. None changes an existing kit behaviour.

- **K1 – `authn.APIKey` metadata.** Add `Name string`, `Labels map[string]string` and `LastUsedAt` (updated at most once a minute, like ctxt's session touch). Adopters that have no identity table of their own bind keys without a side table. Optional for ctxt: its identity row already carries them.
- **K2 – `api.Chain(methods ...Method) AuthFunc` and `api.ProxyIdentity(cfg) Method`.** A `Method` returns `(claims, matched bool, err error)` and declares whether its credential is **carried** (key, certificate, JWT) or **ambient** (cookie, proxy header). The chain runs carried methods before ambient ones whatever the configured order, stops on the first match and on the first refusal, treats an expired or unknown ambient credential as absent (clearing a cookie when it can), and applies `OriginCheck` plus a CSRF header to unsafe requests that only an ambient method authenticated. `ProxyIdentity` reads identity headers only when `PeerAddrFromContext` is in `TrustedProxies`, strips them otherwise, and ships presets `tailscale` and `forward_auth`. Claims carry `Source: "proxy:<preset>"` and `Ambient: true`. Serve config: `services.<svc>.auth.modes: [...]`, a list next to today's single `auth.mode`.
- **K3 – `authn/passkey`.** Wraps go-webauthn: `BeginRegistration/FinishRegistration/BeginLogin/FinishLogin` over a `CredentialStore` interface and a short-lived `CeremonyStore` (challenge state, 5 min TTL, single use). It derives RP ID and origins from a configured public URL. It never decides who the user is: registration takes an already-authorized principal ID.
- **K4 – `authn/ticket`.** Single-use, expiring, hashed-at-rest tickets with a `kind` and an opaque payload: `Issue(ctx, kind, payload, ttl) (secret, error)`, `Redeem(ctx, kind, secret) (payload, error)`. Redeem is atomic, so the same secret can never be used twice. It generalizes `SignedIssuer`/`NonceStore` from "run a command" to "establish an identity".
- **K5 – `transport/session`.** A cookie session primitive taken from ctxt's design: `__Host-` prefix, per-host cookie name, idle and absolute TTLs, rate-limited touch, revocation by owner identity, CSRF header plus `Sec-Fetch-Site`/Origin check (reusing `OriginCheck`), and a store interface. ctxt migrates `internal/auth/session.go` onto it once it lands.
- **K6 – OAuth 2.0 / OIDC sign-in.** Complete `MountOAuth` with the PKCE `code_verifier`, the token exchange, `id_token` verification through `authn.NewOIDC`, and a userinfo fallback for plain-OAuth providers (GitHub). Output is verified claims. Mapping them to a principal stays with the adopter.

Order: K2 and K4 first (Phase 1 already uses their shape), then K3 for passkeys in Phase 3; K5 and K6 can follow. The contact-sharing slice (plan Phase 2) needs no kit change. Until a proposal lands, ctxt implements the piece behind the same interface and switches once kit ships it.

### Testing

- A table-driven chain test: every pair of (credential present/absent/bad) across methods asserts matched, next or refused. It includes: an expired session cookie followed by a valid proxy identity resolves to the proxy principal and clears the cookie; an expired bearer key is 401 even with a valid proxy identity behind it; bearer plus proxy resolves to the bearer principal in every configured order.
- Browser rules on ambient identity: an unsafe request authenticated only by proxy identity, without a passing Origin check and CSRF header, is refused; the same request with a bearer key passes; a proxy-identified request gets `UISessionScopes ∩ principal scopes`.
- Proxy header spoofing: headers from a non-trusted peer are ignored and logged. Validation refuses `0.0.0.0/0` and `::/0`.
- Ticket redemption under concurrency: N parallel redeems of one secret, exactly one succeeds. A redemption on a request that already carries a session is refused. The N+1th redemption or login-code exchange per client address inside the window is 429.
- WebAuthn with a virtual authenticator (go-webauthn test helpers or a Chrome DevTools virtual authenticator in e2e), never a real device.
- Revocation: revoking a principal ends its sessions, keys and identities within one request.
- Tests that start dpkms use `t.TempDir()` data dirs and closed ports, never the live instance.

---

## References

- ADR-023 (layers, scopes, rejected passwords and a required central issuer), ADR-033 (team RBAC compiles to scopes), ADR-031 (cloud SSO is a Node Admin concern), ADR-074 §5–6 (device identity on the `server.auth` seam), ADR-077 (CLI token config), ADR-078 (auth extended through a trusted proxy or OIDC)
- `internal/auth`, `internal/server/http/{routes.go,ui_session.go,middleware_auth.go}`, `internal/server/stack/stack.go`
- kit: `go/transport/authn`, `go/transport/api/{mw_auth.go,mtls.go,mw_clientaddr.go,mw_origin.go}`, `go/transport/cmdsurface/{surface_oauth.go,surface_signed.go}`, `docs/contracts/serve-lifecycle.md`
- [Tailscale Serve identity headers](https://tailscale.com/kb/1312/serve), [WebAuthn Level 3](https://www.w3.org/TR/webauthn-3/), [RFC 7636 PKCE](https://www.rfc-editor.org/rfc/rfc7636), [RFC 6750](https://www.rfc-editor.org/rfc/rfc6750)
- `github.com/go-webauthn/webauthn` v0.18.2
