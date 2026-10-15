# Authentication and sharing: phased plan

> **Date:** 2026-10-14
> **Decision records:** [ADR-079 – Authentication Methods, Principals and the kit/ctxt Split](../decisions/ADR-079-authentication-methods-and-kit-split.md) and [ADR-080 – Sharing: Audiences, Share Policies and Per-Viewer Visibility](../decisions/ADR-080-sharing-and-per-viewer-visibility.md), both Accepted on 2026-10-14 and amended after review the same day (D16–D23).
> **Status:** accepted. Implementation not started.
> **Audience:** whoever implements each phase

The goal: an owner shares a contact network with work colleagues (no phone or email) and with partners (no personal notes), from their own instance, with no external identity provider. The first phase that delivers this end to end is Phase 2. Phases 0 and 1 are the minimum it needs.

## Phases at a glance

| Phase | Delivers | Depends on | kit work |
|---|---|---|---|
| 0a | Status bug, `Viewer` type, lint rule, mention provenance, per-principal columns, leak harness, bench fixture; small, parallelizable | — | — |
| 0b | `ScopedDriver` passthrough over every read interface; the one large mechanical refactor | 0a | — |
| 1 | Principals, identities, API keys with expiry and revoke, invite links, sessions from any method | 0a | none required (K1 optional) |
| **2a** | **Sharing schema, policy spec, classes, ceiling, contact ingest and renderer** | 1 | — |
| **2b** | **Restricted-mode storage:** predicate, filtered edges and entities, masked index, counts, same-profile dedup | 0b, 2a | — |
| **2c** | **Render layer, backstop, read audit** | 2b | — |
| **2d** | **CLI, policy templates, Sharing page, viewer banner** | 2a (preview and audit after 2c) | — |
| 3 | Passkeys, trusted proxy identity (Tailscale, forward auth), magic links (email opt-in) | 1 | K2, K3, K4 |
| 4 | Free-text masking: `pii_detect`, masked shadow FTS, `pii_terms`, masked vectors, value canary over spans | 2 | — |
| 5 | Derived data for restricted viewers: per-view entity pages and topic index, recomputed related scores, `entity_facts`, events and export for viewers | 2 | — |
| 6 | Later methods: mTLS for devices, enterprise OIDC, social login; peer principals with share policies | 3 | K5, K6 |

## Phase 0 – Seams and existing bugs

Nothing user-visible changes for the owner. This phase makes Phase 2 a policy change instead of a refactor. It is split in two because item 0b.1 touches every storage read interface and is the largest refactor in the plan; everything in 0a is small and can run in parallel.

### Phase 0a

1. **Status bug.** `FTSSearch` and `ListBySQL` apply `Status` on both drivers. Inbox and discarded objects stop matching search.
2. **`sharing.Viewer` in the context.** Only the `Unrestricted` value exists yet. `RequireAuth` attaches it for every principal. Private instances attach it too.
3. **Lint rule.** forbidigo (newly enabled in `.golangci.yml`) bans the raw driver accessors in `internal/server/...` and `internal/mcp/...`. Until 0b lands it bans nothing that exists; it is in place so 0b cannot regress.
4. **Provenance on mentions.** `entity_resolver`, `jobs/worker` and `service/fanout` write `edges.metadata.source_node` (the mention's graph node ID) on `mentions` and `mentioned_in` edges.
5. **Per-principal columns.** `owner_principal` on `search_history`, `saved_searches` and `resurfacing_queue`, defaulting to `owner`.
6. **Leak-suite harness** (`internal/sharing/leaktest`). It enumerates `apiRoutes` (`internal/server/http/routes.go`) and `methodScopes` (`internal/server/grpc/scope.go`) and requires a case for every read route. A new route with no case fails the build. In this phase it holds only an owner fixture and asserts that cases exist.
7. **Bench fixture.** A generated 100k-object corpus (contacts, notes, mentions, tags) and a benchmark for `find` and `list` that records p95. Phase 2's performance exit criteria are measured against it; without it they are numbers nobody can check.

### Phase 0b

1. **`storage.ScopedDriver(viewer)` and `storage.System(driver)`.** Every read interface goes through the wrapper, including `Get`, `EdgeStore`, `EntityStore`, `EmbeddingStore`, `ProximityStore`, aliases, pages, saved searches and search history. With `Unrestricted` it passes through. Handlers in `internal/server/{http,grpc}`, `internal/mcp` and `internal/service` read paths take only the scoped driver. Pipeline, jobs and housekeeping call `storage.System`.

Exit criteria:

- Status-bug tests fail on the old code and pass on the new (mutation checked), on SQLite and Postgres.
- `storagetest` conformance covers every scoped method with `Unrestricted`.
- The lint rule fails on a deliberately added raw-driver call in a handler.
- The leak harness lists every route. Removing one case makes it fail.
- The bench records owner p95 for `find` and `list` on the 100k fixture; that number is the Phase 2 baseline.
- The full suite is green, with no change to owner-visible output.

## Phase 1 – Principals, keys, invites (ADR-079 core)

Work:

1. **Migrations.** `principals` (with the single `owner` row, no `agent` kind, no parent link), `principal_identities` (one row per credential, API keys included: method `api_key`, subject = kit key id, name, last-used), `auth_tickets`, and a `kv` table for kit `authn.APIKeys` records (SQLite from 047, Postgres from 27).
2. **API keys.** kit `authn.APIKeys` over a ctxt `kv.Store`, each key mirrored by an identity row. `ctxt auth key create|list|revoke`, `ctxt auth identity list|remove`, `ctxt auth session list|end`, and `dpkms auth key create --principal owner` on the host for bootstrap and recovery. An agent is a key with a name; the share-policy binding on the identity row arrives in Phase 2.
3. **Remove the static provider.** `server.auth.provider`/`static` are deleted. `server.auth.keys[]` accepts `sha256:` hashes only. `federation.token` becomes a `peer` key. Docs and compose examples change with it, including `docs/manual/operations/reverse-proxy.md`. `server.public_url` becomes required when `sign_in` lists `invite` or `link`.
4. **Method chain.** `server.auth.methods: [api_key, session]`, `server.auth.sign_in: [invite, link]`. Stop on match, stop on refusal; carried credentials before ambient ones; an expired or unknown cookie is absent, not refused; `session`-authenticated unsafe requests keep today's Origin check, CSRF header and `UISessionScopes ∩` (ADR-079 §3). Built behind kit K2's planned interface so the later swap is mechanical.
5. **Invites.** `ctxt share invite` mints an `auth_tickets` row and prints `https://<public_url>/ui/invite#t=<secret>`. Redeeming creates a `person` principal with roles `[reader]` and a browser session. A redemption on a request that already carries a session is refused. Redemption and login-code exchange are rate limited per client address. In Phase 1 the invite grants nothing; grants arrive in Phase 2.
6. **Sign-in again without passkeys.** The owner mints a single-use sign-in link for a principal (`ctxt principal link <id>`), with the same ticket store and a 10-minute TTL. This is the stopgap until passkeys in Phase 3.
7. **Sessions.** The `ui_sessions.token_hash` coupling becomes `identity_id` referencing `principal_identities`. Revoking a principal or any of its identities ends the sessions it minted.
8. **Scopes.** `admin:principals` and `admin:shares` in the `admin` bundle (owner only by default); `write:self` in every bundle, with an owner-of-target check on each route under it.
9. **Deploy note.** Both live instances on m3 (`ctxt-personal`, `ctxt-innoprofit`) run `protected` with static tokens behind `tailscale serve`. Deploying this phase there means: create the owner key on the host with `dpkms auth key create --principal owner` before the new build starts, then rotate the ctxt-m3 passthrough tokens and the tokens in `dpkms.yaml`/`ctxt.yaml`. The release-notes row is marked BREAKING and names the command; the `Operator-Impact:` trailer stays `none`, since the CI check accepts only `none | reindex_auto | reingest_selective | reingest_all` and a config change is none of the data buckets.

Exit criteria:

- Chain table test: every combination of present, absent and bad credentials across methods gives the expected match, next or 401, including expired cookie → next, expired key → 401, bearer plus session → bearer.
- Ticket redemption: N concurrent redeems of one secret, exactly one succeeds; a redeem with an active session is refused; the N+1th redeem per address in the window is 429.
- Revocation: revoking a principal fails its next API call, session request and key use; revoking one identity ends only the sessions it minted.
- `write:self`: a `person` can create and revoke its own key and end its own session, and gets 403 on another principal's.
- A protected instance with no owner credential refuses to start and names `dpkms auth key create`.
- No plaintext key is accepted in config. Validation refuses it and names the key. A config with `sign_in: [invite]` and no `public_url` is refused.

## Phase 2 – Contact sharing end to end (smallest complete slice)

Scope is deliberately narrow. Structured contacts are shared through **field projection and rendering**, not span masking, so no PII detector is needed yet. Free-text notes are excluded by class and by data filter, never masked. The phase is split in four so that 2a and 2d can start as soon as Phase 1 lands, while 2b waits for 0b.

### Phase 2a – Schema, policy, classes, contacts

1. **Migrations.** `audiences`, `audience_members`, `share_policies`, `share_grants`, `principals.deny_classes`, `principal_identities.share_policy_id`, `objects.classes`, `objects.projected_fts_body_masked`, `embeddings.variant`, `access_log`, `access_log_objects`.
2. **Policy spec and compile** (`internal/sharing`). Validation of the YAML in ADR-080 §2. The RSQL subset: `type`, `subtype`, `tag`, `profile`, `class`, `source`, dates, with `related`/`mention` refused. Compiled to a per-dialect predicate with `search.CompileFor`. Grant union, the per-principal ceiling and key intersection follow ADR-080 §2.
3. **Classes.** Config rules (`sharing.classes.rules`) evaluated at ingest. Default rules: `structured_metadata.type == person_note` → `personal`, `profile == personal` → `personal`. `ctxt object classify` for manual changes.
4. **Contacts.**
   - cardamum ingest extracts `TEL` and `ADR`.
   - It keeps `email`, `phone` and `address` in metadata only, never in the content text.
   - It stores the vCard `NOTE` as a separate graph node with class `personal`.
   - A contact renderer builds the text from the visible fields and declares `SearchableMinimum = [title, org, role, tags, city]`.
   - The masked FTS body and the masked vector are built from the searchable minimum only.
   - ctxt is unreleased, so existing contacts are re-ingested; there is no migration of their text.

### Phase 2b – Restricted-mode storage

5. **Scoped storage, restricted mode.** The viewer predicate goes into every object read. Edges, backlinks and entities are filtered by ADR-080 §6. Restricted viewers search the masked FTS and masked vectors. Counts, facets and stats are computed inside the scoped query. Rerank inbound counts and alias expansion run over visible objects. SQLite vector search uses widening rounds. The compiled viewer cache invalidates on grant, policy, membership, ceiling, key and principal changes.
6. **Hidden means absent.** A row outside the predicate is 404 on `GET /objects/{id}` and on writes, and omitted from lists. Jobs, inbox, capture/recent, `GET /events`, pipelines, watches, registries admin, entity pages, `topic_index` and export are capability-closed for restricted viewers and return 403 `NOT_SHARED`, never an empty 200.
7. **Dedup within a profile.** `Reinforce` merges only into an object with the same `profile_id`; otherwise the capture becomes its own object.

### Phase 2c – Render, backstop, audit

8. **Render layer.** Field projection, contact rendering and the derived-field rule (summaries of a contact are dropped for restricted viewers) on REST, gRPC, MCP `search` and searchgraph.
9. **Backstop.** Every route in `routes.go` and `methodScopes` declares its response shape (`objects | entities | mixed | none`). The ID ledger walks the known paths of that shape; the canary holds the released objects' projected-out values in memory for the request and skips values under six runes. Strip and log `security.visibility_backstop`. Return 500 under `--strict-sharing` and in tests.
10. **Read audit.** `access_log` for non-owner requests, written by a buffered batch writer. `ctxt share audit --principal|--object`.

### Phase 2d – CLI and UI

11. `ctxt share audience|policy|grant|revoke|ceiling|invite`, two policy templates (`colleagues-network`, `partners-network`, as in ADR-080 §2), a Sharing page in the web UI, and the shared-viewer banner. `ctxt --as`, `ctxt share preview` and `ctxt share audit` land once 2c is in.

Known gap, documented in the sharing guide: a partner sees email on the card but cannot search by it until `pii_terms` arrives in Phase 4; a colleague cannot match on any projected-out field.

Exit criteria (the contact case, end to end):

- An owner creates both audiences and both policies, invites one colleague and one partner, and each signs in through the invite link.
- **Leak suite green** for the colleague, the partner, an agent key narrower than the partner, a principal in both audiences with `deny_classes: [personal]`, an expired grant and a revoked principal, on every route and RPC, on SQLite and Postgres:
  - No hidden object ID, entity slug or edge appears.
  - The colleague's responses contain no email, phone, address or note text (canary values).
  - The partner's responses contain no note text, no `personal` node and no ID of the Sarah note.
  - `total`, facet counts and stats equal the visible count.
  - `GET /objects/{hidden}` is 404; a capability-closed route is 403.
- **Backstop precision.** The restricted viewer's `whoami`, audience and policy listings and error bodies are untouched; a fixture with the projected-out value `Lee` leaves other text intact.
- **Differential tests green.** For each restricted principal, adding or removing the hidden fixtures (the Sarah note, a co-mentioning note, a note tagged `work`, a same-content capture under another profile) does not change any byte of any response, apart from timestamps and request IDs.
- **Mutation checks.** Removing the scoped join from `EdgeStore.ListTo` makes the backstop fire and the leak suite fail. Removing the backstop as well still fails the leak suite.
- **Owner performance.** p95 for owner `find` and `list` within 5% of the Phase 0a baseline on the 100k fixture.
- **Restricted performance.** Colleague `find` p95 under 2× the owner's on the same fixture.

## Phase 3 – Passkeys, proxy identity, magic links

Work:

- **Passkeys** through kit K3: registration after the invite is redeemed, discoverable sign-in, a credential list and revoke in the UI. `server.public_url` is required.
- **Trusted proxy identity** through kit K2: presets `tailscale` and `forward_auth`, `trusted_proxies` validation (refuses `0.0.0.0/0`), and `unknown_identity: deny|request`. Proxy identity is ambient, so proxy-authenticated requests get the browser rules: Origin check and CSRF header on unsafe methods, `UISessionScopes ∩ principal scopes`, lifted only by a bearer key on the same request. An invite can be bound to an expected proxy login, so a partner on a shared tailnet node signs in with no link at all.
- **Magic links** through kit K4. The owner can mint them, and they can be emailed only when the opt-in SMTP provider is configured.
- `ctxt principal identity add|list|remove` for binding a proxy login by hand; `ctxt auth identity` for a principal's own passkeys.

Exit criteria:

- WebAuthn ceremonies pass with a virtual authenticator (go-webauthn test helpers; Chrome DevTools virtual authenticator in e2e), never a real device.
- Proxy-header spoofing from a non-trusted peer is ignored and logged.
- Bearer and proxy together resolve to the bearer principal in every configured order; an expired cookie followed by a valid proxy identity resolves to the proxy principal and clears the cookie.
- An unsafe request authenticated only by proxy identity, without a passing Origin check, is refused; with a bearer key it passes.
- The chain table test extended to all Phase 3 methods.

## Phase 4 – Free-text masking

Work:

- A `pii_detect` built-in step, a provider role with a local default: rules plus validators for email, phone, IBAN, card numbers and postal addresses, and an optional local NER model. Cloud detectors are opt-in.
- `pii_spans` over every text field and graph node, and `pii_terms` for revealed-category matching.
- The masked FTS body for free text and masked vectors for objects with spans.
- `redact.mask`/`reveal`/`style` in policies.
- The canary extended to span values.
- ADR-058 writable fields gain `pii_spans`.
- The custom-steps guide gains a "never store vs per-viewer" section.

Exit criteria:

- An FTS query for a masked email returns nothing for the colleague and the contact for the partner (revealed).
- A vector query with the masked sentence ranks the object no higher than with a neutral sentence (masked-variant check).
- Detector recall measured on a labelled fixture set and recorded in the docs.

## Phase 5 – Derived data for restricted viewers

Work:

- Entity pages and `topic_index` built per view from visible sources, instead of being owner-only.
- `related`/proximity scores recomputed over visible co-mentions.
- `entity_facts` lands together with the first step that derives entity attributes from content.
- `GET /events` filtered per subscriber for restricted viewers (`capabilities.events`).
- `capabilities.export` for viewers.
- The remaining ADR-068 MCP tools, scoped.

Exit criteria: leak and differential suites extended to every artifact in ADR-080 §6, green.

## Phase 6 – Later methods

- mTLS for devices and the ADR-074 hub transport (kit `ClientCertAuth`).
- Enterprise OIDC sign-in and IdP-issued JWT bearer tokens (kit K6 + `authn.NewOIDC`).
- Social login.
- Peer principals with a share policy, so a filtered view can be pushed to a partner's instance (an ADR-064 extension).

## What goes to kit, and when

| Proposal (ADR-079) | Needed by | Until it lands |
|---|---|---|
| K1 – `APIKey` name, labels, last-used | Not needed: the identity row carries them | — |
| K2 – `api.Chain` + `api.ProxyIdentity` | Phase 3 (chain shape used from Phase 1) | ctxt-local chain behind the same interface |
| K4 – `authn/ticket` | Phase 3 (shape used from Phase 1) | `auth_tickets` in ctxt |
| K3 – `authn/passkey` | Phase 3 | — (Phase 3 waits for it, or ctxt wraps go-webauthn directly and moves it later) |
| K5 – `transport/session` | After Phase 3 | ctxt `internal/auth/session.go` |
| K6 – OAuth/OIDC sign-in | Phase 6 | — |

File K2 and K4 first: they are small and unblock the shape that Phase 1 uses.

## Test strategy

| Layer | What | Where |
|---|---|---|
| Unit | Policy validation, RSQL subset, grant union and key intersection, renderer, chain | `internal/sharing`, `internal/auth` |
| Conformance | Every scoped store method, `Unrestricted` and restricted, both drivers | `internal/storage/storagetest` |
| Leak suite | Every read route and RPC as every fixture principal; hidden IDs, canary values, counts | `internal/sharing/leaktest` |
| Differential | Same requests with and without hidden fixtures; byte-identical responses | `internal/sharing/leaktest` |
| Mutation | Break one join and expect red; drop the backstop and expect red | CI job on the leak suite |
| e2e | Invite → session → shared search in the web UI; passkeys with a virtual authenticator (Phase 3) | `e2e/` |

Isolation rules for every test that starts dpkms or ctxt: data dirs under `t.TempDir()`, closed ports for every endpoint, a temporary `HOME`/`XDG_*`, and `testguard.Main` in packages that can reach the keyring. No test talks to a live instance.

Postgres conformance and the Postgres half of the leak suite run in CI against the workflow's Postgres service and locally only when `POSTGRES_*` is set. Agents implementing a phase do not start containers; they run the SQLite half locally and read the Postgres result from CI.

## Risks

| Risk | Mitigation |
|---|---|
| A read path added later skips the scoped driver | Lint rule, route enumeration in the leak harness, backstop |
| Predicate cost on Postgres with large tag sets | Profile in Phase 2; fall back to `object_grants` materialized for objects only |
| SQLite vector recall for narrow viewers | Widening rounds; vec0 metadata pre-filter when one policy dominates |
| PII detector misses | Classes and projection for anything critical; documented recall; canary only checks known values |
| `trusted_proxies` too broad | Validation refuses `0.0.0.0/0`/`::/0`; docs show the impersonation risk; loopback trust stated explicitly |
| Passkeys bound to a host name | `server.public_url` required; changing it documented as invalidating credentials |
| The refactor touches every storage interface | Phase 0b lands it alone, with no behaviour change, behind conformance tests |
| Summaries of contacts disappear for viewers | Accepted in Phase 2 (they derive from content that includes hidden fields); D7 revisits |
| Backstop strips legitimate IDs (principals, policies, jobs share the key `id`) | Routes declare their response shape; the walker follows known paths only; precision tests in the leak suite |
| Ambient identity (cookie, proxy header) rides along on cross-site requests | Browser rules on every ambient method: Origin check, CSRF header, `UISessionScopes ∩` |
| Expired cookie blocks a proxy or passkey sign-in | Expired or unknown ambient credential is absent, not refused |
| A partner who is also a colleague gets the union | `deny_classes` ceiling per principal |
| Read audit writes stall SQLite reads | Buffered batch writer; loss on crash documented |
| Deploying Phase 1 locks the owner out of a live instance | Deploy note in Phase 1: create the owner key on the host first |

## Decisions

D1–D15 accepted as recommended on 2026-10-14. D16–D23 added by the review of the same day.

| # | Decision | Accepted |
|---|---|---|
| D1 | Remove static config tokens entirely? | **Yes.** DB-backed API keys. Config accepts only `sha256:` hashes, for infrastructure as code. Plaintext keys refused. |
| D2 | Which methods are core, and in which order? | **Phase 1:** API keys, invite links, owner-minted sign-in links, sessions. **Phase 3:** passkeys, trusted proxy (Tailscale, forward auth), emailed magic links. **Later:** mTLS, OIDC, social. Passwords stay rejected. If partners are already on the owner's tailnet, Tailscale proxy identity could move into Phase 1: it is the cheapest method. |
| D3 | Several grants matching one object: union or most restrictive? | **Union** (fields ∪, a category masked only if every matching grant masks it). Exclusions apply per grant. The hard ceiling is per principal (D18), not "one audience per person". |
| D4 | Owner sees raw by default? | **Yes**, with `--as` preview and opt-in masking for their own requests. |
| D5 | How do notes get the `personal` class? | Rules by default: `person_note` from structured metadata, the `personal` profile, the vCard `NOTE`. Policy templates exclude `personal`. Data filters are allowlists, so unclassified notes are shared only when a filter names them. |
| D6 | Masked search: one shadow index or post-filtering? | **One masked shadow index** + `pii_terms` for revealed categories. Post-filtering leaks through ranking and counts. |
| D7 | Regenerate summaries per policy? | **No.** Drop derived fields whose inputs are hidden. Revisit opt-in per-policy summaries after Phase 4. |
| D8 | Read audit scope and retention | **Non-owner requests only**, object IDs kept 90 days (configurable), counts kept with the access log. |
| D9 | Vector search for restricted viewers | **Allowed on the masked variant.** Phase 2 covers contacts (render-based); Phase 4 covers free text. |
| D10 | Entity pages and `topic_index` for viewers | **Owner-only in Phase 2**, built per view in Phase 5. |
| D11 | Contact ingest change (email out of text, `NOTE` as a `personal` node, extract `TEL`/`ADR`) | **Yes**, with re-ingest. No compatibility path (ctxt is unreleased). |
| D12 | Email delivery for links | **None by default.** Links are copied by hand. SMTP is an opt-in provider. No cloud mail default. |
| D13 | Principals and device sync (ADR-074) | **Instance-local for now.** Whether principals and grants replicate is decided when ADR-074 is implemented. |
| D14 | Sharing to a partner's own instance | **Defer** to Phase 6 (a peer principal with a share policy). |
| D15 | ADR numbering | **Keep 079/080.** The unmerged `docs/adr-kit-command-surfaces` branch proposes an ADR-078 that collides with the merged ADR-078 (no plugin system). It must take the next free number when it lands. |
| D16 | Agents as principals or as keys? | **Keys.** An agent is an API key on the principal it acts for, with a narrowing share policy. No `agent` kind, no parent link. Every credential, keys included, is a `principal_identities` row, so revocation cascades uniformly and sessions reference one table. |
| D17 | Chain semantics | **Authenticators and sign-in ceremonies are separate config lists.** Carried credentials (key, certificate, JWT) run before ambient ones (cookie, proxy) whatever the configured order. An expired or unknown ambient credential is absent, not refused. Ambient-only requests get the browser rules (Origin check, CSRF header, `UISessionScopes ∩`). `write:self` lets a principal manage its own credentials and sessions. Ceremonies are rate limited and refused on a request that already has a session. |
| D18 | Hard ceiling per principal | **Yes:** `principals.deny_classes`, ANDed after the grant union, applied to parts in render. |
| D19 | Hidden row: 403 or 404? | **404.** Hidden means absent. 403 `NOT_SHARED` only for capability-closed routes. |
| D20 | Backstop walk | **Keyed on a declared response shape per route**, walking known ID paths. Never on bare field names. Canary holds plaintext per request and skips values under six runes. |
| D21 | Shadow index versus field projection | **Per-type `SearchableMinimum`.** Only those fields enter the masked body and vector; the rest matches only through `pii_terms` (Phase 4). Projection inside the minimum does not hide terms; the docs say to use a class. |
| D22 | Content-hash dedup across profiles | **Merge only within the same profile.** A same-content capture under another profile is its own object. |
| D23 | Read-audit write path | **Buffered batch writer** off the request path; entries lost at a crash are accepted and documented. |

## Appendix A: read-path inventory

State on `feat/over-api` at `ea08d07c`. "Opt" means the profile filter is applied only when the caller passes a profile; "none" means never. No path ties visibility to the principal.

| Surface | Path | Today | Phase that closes it |
|---|---|---|---|
| HTTP | `GET /objects`, `/objects/facets` | none (no `profile` parameter) | 2 |
| HTTP | `GET /objects/{id}` | none | 2 |
| HTTP | `GET /objects/{id}/related`, `/objects/{id}/edges` | none, any status | 2 |
| HTTP | `GET /search` (RSQL) | opt, no status filter | 0 (status), 2 |
| HTTP | `POST /find` (FTS, vector, hybrid, facets) | opt; FTS ignores status; rerank and alias expansion global | 0 (status), 2 |
| HTTP | `GET /search/graph` | opt for objects; entities by namespace entitlement only; mention edges unscoped | 2 |
| HTTP | `GET /entities`, `/entities/resolve`, `/entities/{slug}`, `/entities/{slug}/backlinks` | none (entitlement gate only) | 2 |
| HTTP | `GET /pages`, `/pages/{slug}`, `/topic-index[/{topic}]` | none; multi-source derived objects | 2 (owner-only), 5 (per view) |
| HTTP | `GET /stats` | none | 2 |
| HTTP | `GET /inbox`, `/inbox/queue`, `/jobs`, `/jobs/{id}`, `/capture/recent` | none; job payload is raw content | 2 (owner-only) |
| HTTP | `GET /events` (SSE) | every event to every subscriber | 2 (owner-only), 5 (per subscriber) |
| HTTP | `GET /aliases`, `/saved-searches`, `/search-history` | opt or none | 0 (columns), 2 |
| HTTP | `/mcp` → `search` | opt; snippet = first 240 runes of the body | 2 |
| gRPC | `QueryService.Search`, `ListObjects`, `NodeAwareSearch` | opt | 2 |
| gRPC | `QueryService.GetObject`, `EntityService.*`, `JobService.*` | none | 2 |
| CLI | `ctxt export`, `ctxt compose` | via `GetObject` / `ListObjects` | 2 |
| Background | federation `Syncer.Sync`, resurfacing `job.score`, reminders | all objects | out of scope (owner-trusted) |
| Storage | `Reinforce` (content-hash dedup) merges across profiles | write-side; merged tags reach viewers | 2b (same-profile merge only) |
| Storage | `embeddings.text`, `projected_fts_body`, `object_nodes`, `jobs.payload` | raw copies of content | 2 (never served to viewers), 4 (masked variants) |
