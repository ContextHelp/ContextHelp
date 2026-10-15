# Authentication and sharing: phased plan

> **Date:** 2026-10-14
> **Decision records:** [ADR-079 – Authentication Methods, Principals and the kit/ctxt Split](../decisions/ADR-079-authentication-methods-and-kit-split.md) and [ADR-080 – Sharing: Audiences, Share Policies and Per-Viewer Visibility](../decisions/ADR-080-sharing-and-per-viewer-visibility.md), both Proposed.
> **Status:** design for review. Nothing here is implemented yet.
> **Audience:** the owner reviewing the design, then whoever implements each phase

The goal: an owner shares a contact network with work colleagues (no phone or email) and with partners (no personal notes), from their own instance, with no external identity provider. The first phase that delivers this end to end is Phase 2. Phases 0 and 1 are the minimum it needs.

## Phases at a glance

| Phase | Delivers | Depends on | kit work |
|---|---|---|---|
| 0 | Read seams and today's bugs fixed; the leak-suite harness; no behaviour change for the owner | — | — |
| 1 | Principals, API keys with expiry and revoke, invite links, sessions from any method | 0 | none required (K1 optional) |
| **2** | **Contact sharing end to end:** audiences, share policies, grants, classes, contact rendering, scoped storage, backstop, read audit | 1 | — |
| 3 | Passkeys, trusted proxy identity (Tailscale, forward auth), magic links (email opt-in) | 1 | K2, K3, K4 |
| 4 | Free-text masking: `pii_detect`, masked shadow FTS, `pii_terms`, masked vectors, value canary over spans | 2 | — |
| 5 | Derived data for restricted viewers: per-view entity pages and topic index, recomputed related scores, `entity_facts`, events and export for viewers | 2 | — |
| 6 | Later methods: mTLS for devices, enterprise OIDC, social login; peer principals with share policies | 3 | K5, K6 |

## Phase 0 – Seams and existing bugs

Nothing user-visible changes for the owner. This phase makes Phase 2 a policy change instead of a refactor.

Work:

1. **Status bug.** `FTSSearch` and `ListBySQL` apply `Status` on both drivers. Inbox and discarded objects stop matching search.
2. **`sharing.Viewer` in the context.** Only the `Unrestricted` value exists yet. `RequireAuth` attaches it for every principal. Private instances attach it too.
3. **`storage.ScopedDriver(viewer)` and `storage.System(driver)`.** Every read interface goes through the wrapper, including `Get`, `EdgeStore`, `EntityStore`, `EmbeddingStore`, `ProximityStore`, aliases, pages, saved searches and search history. With `Unrestricted` it passes through. Handlers in `internal/server/{http,grpc}`, `internal/mcp` and `internal/service` read paths take only the scoped driver. Pipeline, jobs and housekeeping call `storage.System`.
4. **Lint rule.** forbidigo (newly enabled in `.golangci.yml`) bans the raw driver accessors in `internal/server/...` and `internal/mcp/...`.
5. **Provenance on mentions.** `entity_resolver`, `jobs/worker` and `service/fanout` write `edges.metadata.source_node` (the mention's graph node ID) on `mentions` and `mentioned_in` edges.
6. **Per-principal columns.** `owner_principal` on `search_history`, `saved_searches` and `resurfacing_queue`, defaulting to `owner`.
7. **Leak-suite harness** (`internal/sharing/leaktest`). It enumerates `apiRoutes` (`internal/server/http/routes.go`) and `methodScopes` (`internal/server/grpc/scope.go`) and requires a case for every read route. A new route with no case fails the build. In this phase it holds only an owner fixture and asserts that cases exist.

Exit criteria:

- Status-bug tests fail on the old code and pass on the new (mutation checked), on SQLite and Postgres.
- `storagetest` conformance covers every scoped method with `Unrestricted`.
- The lint rule fails on a deliberately added raw-driver call in a handler.
- The leak harness lists every route. Removing one case makes it fail.
- The full suite is green, with no change to owner-visible output.

## Phase 1 – Principals, keys, invites (ADR-079 core)

Work:

1. **Migrations.** `principals` (with the single `owner` row), `principal_identities`, `auth_tickets`, `api_key_bindings`, and a `kv` table for kit `authn.APIKeys` records (SQLite and Postgres).
2. **API keys.** kit `authn.APIKeys` over a ctxt `kv.Store`. `ctxt auth key create|list|revoke`, and `dpkms auth key create --principal owner` on the host for bootstrap and recovery.
3. **Remove the static provider.** `server.auth.provider`/`static` are deleted. `server.auth.keys[]` accepts `sha256:` hashes only. `federation.token` becomes a `peer` key. Docs and compose examples change with it, including `docs/manual/operations/reverse-proxy.md`.
4. **Method chain.** `server.auth.methods: [api_key, session]`. Stop on match, stop on refusal (ADR-079 §3). Built behind kit K2's planned interface so the later swap is mechanical.
5. **Invites.** `ctxt share invite` mints an `auth_tickets` row and prints `https://<public_url>/ui/invite#t=<secret>`. Redeeming creates a `person` principal and a browser session. In Phase 1 the invite grants nothing; grants arrive in Phase 2.
6. **Sign-in again without passkeys.** The owner mints a single-use sign-in link for a principal (`ctxt principal link <id>`), with the same ticket store and a 10-minute TTL. This is the stopgap until passkeys in Phase 3.
7. **Sessions.** The `ui_sessions.token_hash` coupling becomes `identity_id`. Revoking a principal or an identity ends its sessions.
8. **Scopes** `admin:principals` and `admin:shares`, held by the owner only.

Exit criteria:

- Chain table test: every combination of present, absent and bad credentials across methods gives the expected match, next or 401.
- Ticket redemption: N concurrent redeems of one secret, exactly one succeeds.
- Revocation: revoking a principal fails its next API call, session request and key use.
- A protected instance with no owner credential refuses to start and names `dpkms auth key create`.
- No plaintext key is accepted in config. Validation refuses it and names the key.

## Phase 2 – Contact sharing end to end (smallest complete slice)

Scope is deliberately narrow. Structured contacts are shared through **field projection and rendering**, not span masking, so no PII detector is needed yet. Free-text notes are excluded by class and by data filter, never masked.

Work:

1. **Migrations.** `audiences`, `audience_members`, `share_policies`, `share_grants`, `objects.classes`, `objects.projected_fts_body_masked`, `embeddings.variant`, `access_log`, `access_log_objects`.
2. **Policy spec and compile** (`internal/sharing`). Validation of the YAML in ADR-080 §2. The RSQL subset: `type`, `subtype`, `tag`, `profile`, `class`, `source`, dates, with `related`/`mention` refused. Compiled to a per-dialect predicate with `search.CompileFor`. Grant union and key intersection follow ADR-080 §2.
3. **Classes.** Config rules (`sharing.classes.rules`) evaluated at ingest. Default rules: `structured_metadata.type == person_note` → `personal`, `profile == personal` → `personal`. `ctxt object classify` for manual changes.
4. **Contacts.**
   - cardamum ingest extracts `TEL` and `ADR`.
   - It keeps `email`, `phone` and `address` in metadata only, never in the content text.
   - It stores the vCard `NOTE` as a separate graph node with class `personal`.
   - A contact renderer builds the text from the visible fields.
   - The masked FTS body and the masked vector are built from the minimum view: no `contact_details`, no `personal`.
   - ctxt is unreleased, so existing contacts are re-ingested; there is no migration of their text.
5. **Scoped storage, restricted mode.** The viewer predicate goes into every object read. Edges, backlinks and entities are filtered by ADR-080 §6. Restricted viewers search the masked FTS and masked vectors. Counts, facets and stats are computed inside the scoped query. Rerank inbound counts and alias expansion run over visible objects. SQLite vector search uses widening rounds.
6. **Owner-only for restricted viewers.** Jobs, inbox, capture/recent, `GET /events`, pipelines, watches, registries admin, entity pages, `topic_index`, export. Any route that a capability does not open returns 403 `NOT_SHARED`, never an empty 200.
7. **Render layer.** Field projection, contact rendering and the derived-field rule (summaries of a contact are dropped for restricted viewers) on REST, gRPC, MCP `search` and searchgraph.
8. **Backstop.** ID ledger plus a canary over projected-out values (the contact's email, phone, address and note hashes). Strip and log `security.visibility_backstop`. Return 500 under `--strict-sharing` and in tests.
9. **Read audit.** `access_log` for non-owner requests. `ctxt share audit --principal|--object`.
10. **CLI and UI.** `ctxt share audience|policy|grant|revoke|invite|audit`, `ctxt --as`, two policy templates (`colleagues-network`, `partners-network`, as in ADR-080 §2), a Sharing page in the web UI, and the shared-viewer banner.

Exit criteria (the contact case, end to end):

- An owner creates both audiences and both policies, invites one colleague and one partner, and each signs in through the invite link.
- **Leak suite green** for the colleague, the partner, an agent key narrower than the partner, an expired grant and a revoked principal, on every route and RPC, on SQLite and Postgres:
  - No hidden object ID, entity slug or edge appears.
  - The colleague's responses contain no email, phone, address or note text (canary values).
  - The partner's responses contain no note text, no `personal` node and no ID of the Sarah note.
  - `total`, facet counts and stats equal the visible count.
- **Differential tests green.** For each restricted principal, adding or removing the hidden fixtures (the Sarah note, a co-mentioning note, a note tagged `work`) does not change any byte of any response, apart from timestamps and request IDs.
- **Mutation checks.** Removing the scoped join from `EdgeStore.ListTo` makes the backstop fire and the leak suite fail. Removing the backstop as well still fails the leak suite.
- **Owner performance.** p95 for owner `find` and `list` within 5% of Phase 0, on a 100k-object fixture.
- **Restricted performance.** Colleague `find` p95 under 2× the owner's on the same fixture.

## Phase 3 – Passkeys, proxy identity, magic links

Work:

- **Passkeys** through kit K3: registration after the invite is redeemed, discoverable sign-in, a credential list and revoke in the UI. `server.public_url` is required.
- **Trusted proxy identity** through kit K2: presets `tailscale` and `forward_auth`, `trusted_proxies` validation (refuses `0.0.0.0/0`), and `unknown_identity: deny|request`. An invite can be bound to an expected proxy login, so a partner on a shared tailnet node signs in with no link at all.
- **Magic links** through kit K4. The owner can mint them, and they can be emailed only when the opt-in SMTP provider is configured.

Exit criteria:

- WebAuthn ceremonies pass with a virtual authenticator (go-webauthn test helpers; Chrome DevTools virtual authenticator in e2e), never a real device.
- Proxy-header spoofing from a non-trusted peer is ignored and logged.
- Bearer and proxy together resolve to the bearer principal.
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
| K1 – `APIKey` name, labels, last-used | Nice to have in Phase 1 | `api_key_bindings` side table |
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

## Risks

| Risk | Mitigation |
|---|---|
| A read path added later skips the scoped driver | Lint rule, route enumeration in the leak harness, backstop |
| Predicate cost on Postgres with large tag sets | Profile in Phase 2; fall back to `object_grants` materialized for objects only |
| SQLite vector recall for narrow viewers | Widening rounds; vec0 metadata pre-filter when one policy dominates |
| PII detector misses | Classes and projection for anything critical; documented recall; canary only checks known values |
| `trusted_proxies` too broad | Validation refuses `0.0.0.0/0`/`::/0`; docs show the impersonation risk; loopback trust stated explicitly |
| Passkeys bound to a host name | `server.public_url` required; changing it documented as invalidating credentials |
| The refactor touches every storage interface | Phase 0 lands it with no behaviour change, behind conformance tests |
| Summaries of contacts disappear for viewers | Accepted in Phase 2 (they derive from content that includes hidden fields); D7 revisits |

## Decisions needed

| # | Decision | Recommendation |
|---|---|---|
| D1 | Remove static config tokens entirely? | **Yes.** DB-backed API keys. Config accepts only `sha256:` hashes, for infrastructure as code. Plaintext keys refused. |
| D2 | Which methods are core, and in which order? | **Phase 1:** API keys, invite links, owner-minted sign-in links, sessions. **Phase 3:** passkeys, trusted proxy (Tailscale, forward auth), emailed magic links. **Later:** mTLS, OIDC, social. Passwords stay rejected. If partners are already on the owner's tailnet, Tailscale proxy identity could move into Phase 1: it is the cheapest method. |
| D3 | Several grants matching one object: union or most restrictive? | **Union** (fields ∪, a category masked only if every matching grant masks it). Exclusions apply per grant. The owner puts a person in one audience for a hard ceiling. |
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
| Storage | `Reinforce` (content-hash dedup) merges across profiles | write-side | noted; not a read leak |
| Storage | `embeddings.text`, `projected_fts_body`, `object_nodes`, `jobs.payload` | raw copies of content | 2 (never served to viewers), 4 (masked variants) |
