---
title: "ctxt as a pure dpkms API client"
tracks:
  - ctxt-remote-client
tasks:
  # ── phase 0: gate and foundations ────────────────────────────────
  - title: "Owner review: ADR-077 and the remote-client plan's open questions"
    description: |
      Gate for every implementation task.
      - Review ADR-077 (docs/decisions/ADR-077-ctxt-pure-api-client.md), which is Proposed.
      - Review the plan's "Open questions" section: scopes vs a writer role, failover, offline capture, the ADR-075 amendment, profile placement, registry tokens, per-instance history position, `upgrade run` placement, owner metering.
      - Record each answer in the plan's "Resolved decisions" section and flip the ADR status.
      Acceptance: every open question has a recorded answer, and the ADR is Accepted or amended.
      Operator-Impact: none.
      Action: record the owner's answers in the plan and set ADR-077's status.
    effort: XS
    priority: P0
    tags: [phase:0, type:docs, domain:cli]
    blocked-by: []

  - title: "Test substrate: in-process dpkms for ctxt command tests, built-binary launcher for e2e"
    description: |
      Nothing changes behaviour. This is the fixture every later task uses.
      In-process server:
      - Extend `setupTestDB` (cmd/ctxt/cmd/testhelpers_test.go) so it also starts an httptest server around `NewRouterWithConfig` (internal/server/http/server.go:70) on the same temporary SQLite driver, with the service built the way `dpkms serve` builds it (cmd/dpkms/cmd/serve.go:322).
      - Write `server.url` for that server into the test config.
      - Options: static-token auth with admin and reader principals, and an unreachable variant.
      E2E launcher:
      - Add a helper under test/testutil/ that starts the built `dpkms serve` on a free port, never 8080 or 8081.
      - It uses hermetic HOME and XDG dirs and a protected config with static tokens, and reaps the process group on cleanup.
      Test gates:
      - Every new test package has a TestMain calling `testguard.Main` (internal/testguard).
      - Proof test: a request to 127.0.0.1:8080 from a fixture-using test fails the run.
      Acceptance:
      - Existing cmd tests pass unchanged.
      - A sample test seeds through the driver and reads through GET /api/v1/objects/{id}.
      Operator-Impact: none.
      Action: add the in-process dpkms fixture and the e2e launcher, with testguard-backed proof tests.
    effort: S
    priority: P0
    tags: [phase:0, type:test, domain:testing]
    blocked-by: []

  - title: "dpkmsclient: one typed HTTP client with kit exit-class mapping"
    description: |
      A new package, internal/dpkmsclient. It is the only way ctxt talks to dpkms.
      It absorbs:
      - `serverDo` / `serverGet` / `contactError` (cmd/ctxt/cmd/server_endpoint.go:44-88);
      - capture's `postCapture` walk (cmd/ctxt/cmd/capture.go:317-368);
      - idxbridge's endpoint and probe handling (internal/idxbridge/idxbridge.go).
      Behaviour:
      - Sends the bearer token and JSON.
      - Mints an Idempotency-Key per logical submission, reused across a failover walk.
      - Walks to the next endpoint only on a dial or DNS failure, and prints which instance served when it wasn't the primary.
      - A 401 or 403 is terminal.
      Error mapping to kit envelopes:
      | Condition | Class | Exit |
      |---|---|---|
      | dial or DNS failure | PREREQUISITE | 70 |
      | 401, 403 | UNAUTHORIZED | 5 |
      | 404 | NOT_FOUND | 3 |
      | 409, policy veto | CONFLICT | 4 |
      | 400, 422 | USAGE | 2 |
      | 5xx, timeout after connecting | TRANSIENT | 6 |
      The dpkms error body's code and message are carried into the envelope.
      Tests:
      - httptest-backed, table-driven over every status class.
      - Mutation check: make a 403 walk on to the next endpoint, and the test goes red.
      - testguard TestMain.
      Acceptance: the package exists with tests; no command uses it yet.
      Operator-Impact: none.
      Action: implement internal/dpkmsclient with the error-class table and the failover walk.
    effort: M
    priority: P0
    tags: [phase:0, type:feat, domain:cli]
    blocked-by: [0]

  - title: "Named endpoints: --instance and CTXT_INSTANCE select a URL plus token for every HTTP command"
    description: |
      Config:
      - Add an optional unique `name` to `ServerEndpoint` (internal/config/config.go:661), accepted in YAML and mapstructure.
      - Validation rejects duplicate or empty-when-present names.
      Resolver (ADR-077 §3):
      - Order: `--server` > `--instance` / `CTXT_INSTANCE` > current-instance state > `server.urls` > `server.url` > default.
      - A name matches the named entry first, then a running local pidfile instance by name or port, as `http://127.0.0.1:<port>` with `server.token`.
      - An unknown name exits 70, and the SuggestedFix lists the configured names.
      Callers:
      - Replace `clientEndpoints`, `pinnedEndpoint` (cmd/ctxt/cmd/helpers.go:109-142) and `serverEndpoint` (server_endpoint.go:33) with the resolver.
      - Today every HTTP command ignores `--instance`; this fixes it.
      Instance commands (cmd/ctxt/cmd/instance.go):
      - `ctxt instance list` shows named endpoints and running local instances.
      - `ctxt instance use` accepts either.
      - `ctxt instance current` prints URL, source layer and whether a token is attached, never the token itself.
      Scope limit:
      - `resolveStoragePath` is untouched. Direct-store commands given a remote name keep failing with "no running dpkms instance" until their own task lands. Say so in the release-notes row.
      Tests: each precedence layer, a stale state file, port selection, and token never printed.
      Acceptance:
      - `ctxt status --instance <name>` hits the named URL with its token.
      - The old helpers are deleted.
      - Release-notes `none` row.
      Operator-Impact: none.
      Action: add named server.urls entries and route every HTTP command through the new resolver.
    effort: M
    priority: P0
    tags: [phase:0, type:feat, domain:cli]
    blocked-by: [2]

  - title: "dpkms authorization: route scopes, admin/reader bundles, gRPC parity, whoami"
    description: |
      Nothing enforces roles today: `Principal.HasRole` (internal/auth/auth.go:72) has no caller.
      Route table:
      - Every /api/v1 route in internal/server/http/server.go:122-249 declares an ADR-023 scope, using the classes in the plan's "Authorization" section.
      - Mount routes through a route-to-scope table.
      Enforcement:
      - The middleware reads the principal attached by `RequireAuth` (middleware_auth.go:42) and returns 403 with a named missing scope.
      - The static provider's roles expand to fixed bundles: `admin` gets every scope, `reader` gets every `read:*` scope.
      - The gRPC unary and stream interceptors (internal/server/grpc/auth.go:33,49) enforce the same per-method scopes.
      - Private instances with no auth provider grant every scope.
      whoami: add GET /api/v1/whoami, which returns the principal ID and its effective scopes.
      Tests:
      - Walk the chi router and fail when a route has no scope.
      - A reader token gets 403 on POST /analyze over both HTTP and gRPC.
      - An admin token passes.
      - A private instance passes with no token.
      Mutation checks: remove the middleware, or the gRPC check, and the tests go red.
      Acceptance: release-notes `none` row saying reader tokens lose write access.
      Operator-Impact: none.
      Action: add scope enforcement to every HTTP route and gRPC method, plus GET /api/v1/whoami.
    effort: M
    priority: P0
    tags: [phase:0, type:feat, domain:server, domain:auth]
    blocked-by: [0]

  # ── phase 1: stop the silent local writes ────────────────────────
  - title: "Enqueue path without local fallback: analyze, bare content, capture, capture tabs, capture history"
    description: |
      The defect, today:
      - These commands fall back to `localDirectAnalyze` (cmd/ctxt/cmd/write_gate.go:26), wired at analyze.go:230-233, capture_tabs.go:322-325 and capture_history.go:526-529. The fallback also runs after a 401 or 403 (internal/idxbridge/analyze.go:78-87).
      - The user is told "Queued locally" (analyze.go:264, :270), and the reports carry `queued_locally` (capture_tabs.go:150, capture_history.go:167). A remote daemon never sees that database.
      - `capture [source]` walks on after a 401 or 403 (capture.go:357-361).
      Change: route all of them through dpkmsclient.
      Delete:
      - write_gate.go;
      - idxbridge's AnalyzeFallback path;
      - the "Queued locally" branches;
      - the QueuedLocally fields and their renderers (capture_tabs.go:383, capture_history.go:652).
      Keep: capture history advances its position only after a successful handoff.
      Tests, written first and red on the current code:
      - An unreachable endpoint exits 70 and creates no db.sqlite under XDG_DATA_HOME.
      - A 401 exits 5, makes no second request, and writes nothing locally.
      - A dial failure on the primary is served by the secondary and announced on stderr.
      Acceptance: release-notes `none` row.
      Operator-Impact: none.
      Action: move the enqueue commands onto dpkmsclient and delete the local-write fallback.
    effort: M
    priority: P0
    tags: [phase:1, type:fix, domain:cli, domain:capture]
    blocked-by: [1, 3]

  - title: "list --q searches remotely only; delete idxbridge"
    description: |
      Today (cmd/ctxt/cmd/list.go:136-168):
      - `runList` opens the local store before routing (list.go:137), so `--q` fails when there's no local instance even though the remote would answer.
      - After a 401 or 403 it searches the local corpus instead (internal/idxbridge/idxbridge.go:309-316).
      Change:
      - `--q` calls GET /api/v1/search through dpkmsclient, before any store is opened.
      - Delete internal/idxbridge. Its last caller goes here.
      Tests, written first:
      - `--q` with no local database answers from the fixture.
      - A 401 exits 5 with no local search.
      Acceptance: idxbridge is gone, and `go build ./...` passes.
      Operator-Impact: none.
      Action: route list --q through dpkmsclient and remove internal/idxbridge.
    effort: S
    priority: P0
    tags: [phase:1, type:fix, domain:cli, domain:search]
    blocked-by: [5]

  # ── phase 2: read commands onto the API ──────────────────────────
  - title: "dpkms: object read surface (full list filters, facets, related)"
    description: |
      The gap: GET /api/v1/objects honours only type, subtype, limit and offset (internal/server/http/handlers_objects.go:28-37).
      Extend GET /objects to the full `storage.ObjectFilter` that `buildObjectFilter` builds (cmd/ctxt/cmd/helpers.go:348-388):
      - tag, mention, pipeline, status, sort, dir;
      - before, after (dates);
      - meta_type, topic, person, source_type, since, until.
      Add:
      - GET /api/v1/objects/facets, which takes the same filter and returns counts by metadata type (`Service.FacetCounts`, internal/service/service.go:407);
      - GET /api/v1/objects/{id}/related?depth=&limit= (`Service.RelatedObjects`, service.go:493).
      Scope: `read:objects`.
      Tests: per filter parameter; bad dates return 400; unknown parameters return 400.
      Acceptance: handlers are documented in docs/ctxt/api-cli.md.
      Operator-Impact: none.
      Action: widen GET /objects and add the facets and related endpoints.
    effort: M
    priority: P1
    tags: [phase:2, type:feat, domain:server]
    blocked-by: [1, 4]

  - title: "ctxt list, show, export, classify, compose, profile schema evolve over the API"
    description: |
      Move each command off `newService`:
      - list, non-`--q` (list.go:137): GET /objects, plus GET /objects/facets for `--facets`. `--cursor` stays a client-side file (internal/cursor).
      - show (show.go:65, :172): GET /objects/{id} and /related.
      - export (export.go:66): GET /objects/{id}.
      - classify with an `o-` argument (classify.go:111): GET /objects/{id}.
      - compose (compose.go:118): GET /objects with the filter, then compose on the client. `Service.Compose` (internal/service/service.go:1089-1130) is pure formatting.
      - profile schema evolve (profile_schema.go:417): GET /objects?limit=100&sort=created_at&dir=desc, then run the frequency analysis from service/schema_evolution.go:27-45 on the client.
      Delete dead code:
      - the output-generator branches in export (export.go:90-100) and show (show.go:85), plus export's `--dest` flag. `Service.PluginRegistry` (service.go:48) is never assigned, so they can't run.
      Tests, written first: each command answers from the fixture with no local database path configured.
      Acceptance: none of these files calls newService.
      Operator-Impact: none.
      Action: switch the six commands to dpkmsclient and delete their newService calls and dead generator code.
    effort: M
    priority: P1
    tags: [phase:2, type:refactor, domain:cli]
    blocked-by: [3, 7]

  - title: "dpkms: POST /api/v1/find (fts, vector, hybrid; diagnostics, explain, facets)"
    description: |
      Today `runFind` (cmd/ctxt/cmd/find.go:147) does three things locally:
      - builds the semantic source, including query embedding, from the local store (find.go:237);
      - merges the local profile's SearchStrategy and flag knobs (find.go:192-215);
      - calls `SemanticSearchFiltered` / `FindByTextFiltered` / `HybridSearchFilteredWithDiagnostics` / the explain variant.
      New endpoint: POST /api/v1/find.
      - Request: query, mode, the object filter, the resolved knobs (rrf_k, fts_weight, vector_weight, fts_pool, vector_pool, min_score), profile, explain, facets.
      - Response: results, diagnostics, and optionally an explain breakdown and facets.
      - Query embedding runs server-side, against the server's model registry.
      Scope: `read:objects`.
      Tests:
      - Each mode against the fixture.
      - A vector mode whose provider is unavailable returns 422 naming the model.
      - The embedding provider is an xrr cassette, never live Ollama.
      Operator-Impact: none.
      Action: implement POST /api/v1/find with server-side query embedding.
    effort: M
    priority: P1
    tags: [phase:2, type:feat, domain:server, domain:search]
    blocked-by: [1, 4]

  - title: "ctxt find and ctxt://search over /find"
    description: |
      `runFind` stops opening the store (find.go:171).
      - It resolves the profile and knobs locally and sends them in the request (ADR-077 §6).
      - It renders results, diagnostics, `--explain` and `--facets` exactly as today.
      - It deletes `findSemanticSource` and the local embedding resolution on this path.
      `ctxt://search/<q>` (root.go:516-521) follows, because it calls runFind.
      Tests, written first:
      - The JSON output shape is unchanged (golden).
      - Knobs from a profile reach the request body.
      Acceptance: find.go makes no newService call on the non-`--graph` path.
      Operator-Impact: none.
      Action: switch find to POST /api/v1/find and remove its local search path.
    effort: S
    priority: P1
    tags: [phase:2, type:refactor, domain:cli, domain:search]
    blocked-by: [3, 9]

  - title: "ctxt find --graph over GET /api/v1/search/graph"
    description: |
      `runFindGraph` (cmd/ctxt/cmd/find_graph.go) builds the graph in-process from `svc.Store`.
      The dpkms data endpoint GET /api/v1/search/graph is being built on the separate search-graph track and hasn't merged. Record that dependency textually after ingest.
      Change: `--graph` fetches the graph document from that endpoint and keeps the viewer and file export client-side.
      Tests, written first: golden graph JSON from the fixture.
      Acceptance: find.go and find_graph.go make no newService calls.
      Operator-Impact: none.
      Action: fetch --graph data from /api/v1/search/graph once that endpoint has merged.
    effort: S
    priority: P2
    tags: [phase:2, type:refactor, domain:cli, domain:search]
    blocked-by: [10]

  - title: "Entities: search and resolve endpoints; ctxt entity and resolve over the API"
    description: |
      Existing routes: GET /entities, /entities/{slug}, /entities/{slug}/backlinks (internal/server/http/server.go:141-143).
      Add:
      - GET /api/v1/entities?q= for `SearchEntities` (service.go:908);
      - GET /api/v1/entities/resolve?mention= for `Entities().Resolve`.
      Both keep the inbound entitlement gate, as the existing handlers do.
      Move off newService:
      - entity list, show, search, backlink (cmd/ctxt/cmd/entities.go:133,166,201,230);
      - resolve (resolve.go:126, :186).
      Scope: `read:objects`.
      Tests, written first: each command against the fixture, and an unknown mention exits 3.
      Operator-Impact: none.
      Action: add the entity search and resolve endpoints and switch the entity and resolve commands.
    effort: S
    priority: P1
    tags: [phase:2, type:feat, domain:server, domain:cli]
    blocked-by: [1, 3, 4]

  - title: "Pages, topic index and stats endpoints; ctxt page, index, stats over the API"
    description: |
      Add:
      - GET /api/v1/pages?limit= and GET /api/v1/pages/{slug} (`ListEntityPages` / `GetEntityPage`, internal/service/page.go:181-203);
      - POST /api/v1/pages/{slug}/refresh (writes);
      - GET /api/v1/topic-index and GET /api/v1/topic-index/{topic};
      - POST /api/v1/topic-index, which rebuilds the index (internal/service/index.go:42-69);
      - GET /api/v1/stats, which returns object counts by type, job counts by status, and entity, feed, reminder and resurfacing counts computed server-side. Today stats.go:76-84 lists every object to count types.
      Scopes: `read:objects` for reads, `write:objects` for refresh and rebuild.
      Move page, index, stats and `stats --watch` (page.go:98,135,168; index.go:51; stats.go:66) off newService.
      Tests, written first: each command against the fixture, and a reader token gets 403 on refresh and rebuild.
      Operator-Impact: none.
      Action: add the pages, topic-index and stats endpoints and switch the three commands.
    effort: M
    priority: P1
    tags: [phase:2, type:feat, domain:server, domain:cli]
    blocked-by: [1, 3, 4]

  - title: "ctxt audit and doctor over the API"
    description: |
      audit list and audit export (cmd/ctxt/cmd/audit.go:128,184):
      - Use the existing GET /api/v1/audit-log (server.go:237), the same endpoint `ctxt log` uses (log.go:107).
      - Export pages through offset until exhausted.
      - Scope: `admin:audit`.
      doctor (doctor.go:52-80):
      - Add POST /api/v1/lint. It takes the checks, limit, stale_days, duplicate_threshold and profile, runs `lint.New(svc.Store, …)` server-side, and appends the audit record as `logLintReport` does today.
      - Scope: `read:objects`.
      - doctor stays in ctxt, because knowledge-quality lint is brain intent.
      Tests, written first:
      - Filter parity between audit's flags and the handler's parameters (handlers_audit_log.go:13-55).
      - A reader token gets 403 on the audit log.
      - The doctor JSON report shape is unchanged.
      Operator-Impact: none.
      Action: switch audit to /audit-log, add POST /api/v1/lint, and switch doctor.
    effort: S
    priority: P1
    tags: [phase:2, type:feat, domain:server, domain:cli]
    blocked-by: [1, 3, 4]

  - title: "Inbox queue and clear endpoints; ctxt inbox over the API"
    description: |
      Existing routes: GET /inbox, POST /inbox/{id}/triage, POST /inbox/{id}/discard (server.go:201-204).
      Add:
      - GET /api/v1/inbox/queue?pending=&failed=&raw= for `ListInboxQueue` (internal/service/service_inbox.go:135), which serves the `--pending` / `--failed` / `--raw` view;
      - POST /api/v1/inbox/clear for `ClearInbox` (service_inbox.go:231).
      Scopes: `read:inbox`, and `process:inbox` for triage, discard and clear.
      Move inbox list, triage, discard, clear (cmd/ctxt/cmd/inbox.go:149,236,261,283) off newService.
      Tests, written first: each subcommand against the fixture, and scope denials.
      Operator-Impact: none.
      Action: add the inbox queue and clear endpoints and switch ctxt inbox.
    effort: S
    priority: P1
    tags: [phase:2, type:feat, domain:server, domain:cli]
    blocked-by: [1, 3, 4]

  - title: "Object reminders and resurfacing endpoints; ctxt remind and resurface over the API"
    description: |
      Reminders:
      - PUT /api/v1/objects/{id}/reminder {at} and DELETE /api/v1/objects/{id}/reminder, for `SetObjectReminder` / `ClearObjectReminder` (internal/service/reminders.go:11-21).
      - GET /api/v1/object-reminders for `ListPendingReminders`. This is distinct from /system/reminders.
      Resurfacing (today in resurface.go:128,195,219):
      - POST /api/v1/resurfacing/take {profile, limit, min_score} lists entries and marks them resurfaced, as the show path does today at resurface.go:157-160. It's a POST because it writes.
      - POST /api/v1/resurfacing/refresh runs the job. The resurfacing config comes from ctxt in the request body, per ADR-077 §6.
      - POST /api/v1/resurfacing/{id}/dismiss.
      Scope: `write:objects`, and `read:objects` for listing reminders.
      Tests, written first: each subcommand against the fixture.
      Operator-Impact: none.
      Action: add the reminder and resurfacing endpoints and switch ctxt remind and resurface.
    effort: S
    priority: P2
    tags: [phase:2, type:feat, domain:server, domain:cli]
    blocked-by: [1, 3, 4]

  # ── phase 3: writes and operator commands ────────────────────────
  - title: "ctxt edit, delete, reprocess over the API"
    description: |
      edit (cmd/ctxt/cmd/edit.go:98):
      - Widen PATCH /api/v1/objects/{id}, which today applies only type and subtype (handlers_objects.go:69-74), to title, summary, tags, mentions and subtype.
      - Delete the `--hints` and `--decisions` flags. They are bound (edit.go:59,61) and never applied.
      delete (delete.go:150,366): preview and bulk delete by filter use GET /objects with the widened filters, then DELETE /objects/{id} per ID.
      reprocess (reprocess.go:67-100):
      - Today it runs enrichment steps in-process with local providers, then updates the object.
      - Add POST /api/v1/objects/{id}/reprocess {step}. It enqueues a dpkms job, so providers run where dpkms runs and use its configuration.
      Scopes: `write:objects` and `delete:objects`.
      Tests, written first:
      - The edit PATCH body covers each field.
      - A bulk delete preview matches the executed set.
      - reprocess returns a job ID.
      - The provider path uses an xrr cassette.
      Operator-Impact: none.
      Action: widen PATCH, add the reprocess endpoint, and switch edit, delete and reprocess.
    effort: M
    priority: P1
    tags: [phase:3, type:feat, domain:server, domain:cli]
    blocked-by: [3, 7]

  - title: "Edge endpoints; ctxt link over the API"
    description: |
      `ctxt link` writes and reads `svc.Store.Edges()` directly (cmd/ctxt/cmd/link.go:85,184,369).
      Add:
      - POST /api/v1/edges {source, target, type, context};
      - GET /api/v1/objects/{id}/edges?direction=in|out|both&type=&depth=, where depth is 1 to 3 and matches `--follow`;
      - DELETE /api/v1/edges?source=&target=&type=.
      Scopes: `write:objects`, `read:objects`, `delete:objects`.
      Move link create, list (including `--follow` traversal) and delete off newService.
      Tests, written first: round-trip create, list and delete; a depth-3 traversal equals today's output (golden).
      Operator-Impact: none.
      Action: add the edge endpoints and switch ctxt link.
    effort: S
    priority: P1
    tags: [phase:3, type:feat, domain:server, domain:cli]
    blocked-by: [1, 3, 4]

  - title: "POST /api/v1/ingest for adapter objects; ctxt ingest over the API"
    description: |
      `ctxt ingest` runs adapters (cardamum, himalaya, stdin) locally, which is correct: local capture stays in ctxt.
      But its runner writes `ObjectStore` directly, with source-key dedup (internal/ingest/runner.go:87,116; cmd/ctxt/cmd/ingest.go:77-83).
      Add POST /api/v1/ingest:
      - It takes a batch of objects with `source_key` and upserts by source key server-side, with the runner's dedup semantics.
      - It returns created, updated and skipped counts.
      Scope: `write:objects`.
      ctxt ingest, including the continuous `--every` mode, posts batches.
      Tests, written first: re-posting a batch is idempotent, and the counts match the local runner's on the same fixture.
      Operator-Impact: none.
      Action: add POST /api/v1/ingest and switch ctxt ingest to it.
    effort: S
    priority: P2
    tags: [phase:3, type:feat, domain:server, domain:cli, domain:capture]
    blocked-by: [1, 3, 4]

  - title: "Registry endpoints; ctxt registry over the API"
    description: |
      Reads, scope `read:registries`:
      - list: existing GET /api/v1/steps/registries (server.go:167).
      - info: new GET /api/v1/registries/{name}, the cached manifest.
      - usage: new GET /api/v1/registries/usage?name=.
      - capabilities: new GET /api/v1/registries/{name}/capabilities.
      - entitlements: new GET /api/v1/entitlements.
      Admin, scope `sync:registries`:
      - add: existing POST /api/v1/steps/registries/fetch (server.go:165).
      - delete: new DELETE /api/v1/registries/{name}.
      - sync: new POST /api/v1/registries/{name}/sync?dry_run=, for `SyncAllRegistriesWithReconciliation` / `DiffRegistrySync`.
      The commands (cmd/ctxt/cmd/registry.go:349,387,437,470,540,708,861,1047) stay in ctxt, because registry subscription is a knowledge decision.
      Out of scope: registry search, login, logout and submit don't touch the store.
      Tests, written first: each subcommand; a reader token gets 403 on add, delete and sync; `--dry-run` writes nothing.
      Operator-Impact: none.
      Action: add the registry endpoints and switch the eight ctxt registry subcommands.
    effort: M
    priority: P2
    tags: [phase:3, type:feat, domain:server, domain:cli, domain:registry]
    blocked-by: [1, 3, 4]

  - title: "Embedding model reads: GET /api/v1/embeddings/models and /provider"
    description: |
      embeddings list (cmd/ctxt/cmd/embeddings.go:217):
      - Add GET /api/v1/embeddings/models, which returns models with coverage (`ListWithCoverage`).
      - The eva contract shape from ADR-071 stays unchanged.
      embeddings provider [model_id] (embeddings.go:302):
      - Today it resolves the provider from the CLI's own flags, env and config.
      - Add GET /api/v1/embeddings/provider?model_id=, which returns the SERVER's resolution with per-field source layers. It returns api_key_env names only, never keys.
      - Delete the client `--embedding-*` override flags on `provider`. They describe a process that no longer embeds anything.
      Scope: `read:embeddings`.
      Tests, written first: golden JSON for list; the provider explain comes from the server config; no key value appears in the output.
      Operator-Impact: none.
      Action: add the two embedding read endpoints and switch embeddings list and provider.
    effort: S
    priority: P1
    tags: [phase:3, type:feat, domain:server, domain:cli, domain:embeddings]
    blocked-by: [1, 3, 4]

  - title: "Embedding model lifecycle behind admin endpoints: register, set-default, deprecate, purge, migrate"
    description: |
      Add, all with scope `admin:embeddings`:
      - POST /api/v1/embeddings/models: register. The server resolves the backend, model, endpoint, api_key_env and dimension from the body, probes the provider, and ensures the per-model index. Today this is embeddings.go:396.
      - POST /api/v1/embeddings/models/{id}/default?dry_run=.
      - POST /api/v1/embeddings/models/{id}/deprecate?dry_run= (embeddings_lifecycle.go:213,300; plans via PlanSetDefault / PlanDeprecate).
      - DELETE /api/v1/embeddings/models/{id}?dry_run=: purge (embeddings_lifecycle.go:408).
      - POST /api/v1/embeddings/migrations {to}: enqueue the migrate job that embeddings_migrate.go:105-135 enqueues through the jobs table today.
      The commands stay in ctxt (ADR-077 §5).
      - The coverage and recall guards, and the ADR-075 before-hook seams, stay in the service layer. They now run only in the daemon.
      Tests, written first:
      - Each verb returns 403 with a reader token.
      - dry_run writes nothing.
      - A conflicting active migration returns 409 (exit 4).
      - The provider probe uses an xrr cassette.
      - The embeddings journey test (cmd/ctxt/cmd/embeddings_journey_e2e_test.go) passes against the fixture.
      Acceptance: openEmbeddingsBackend and newEmbeddingsRegistry are deleted.
      Operator-Impact: none.
      Action: add the five admin embedding endpoints and switch the lifecycle commands.
    effort: M
    priority: P1
    tags: [phase:3, type:feat, domain:server, domain:cli, domain:embeddings]
    blocked-by: [21]

  - title: "Upgrade: GET /api/v1/upgrade/plan for ctxt; move upgrade run to dpkms"
    description: |
      upgrade plan (cmd/ctxt/cmd/upgrade_dpkms.go:304):
      - It reads the SQLite handle directly (upgradeServiceDB, :426).
      - Add GET /api/v1/upgrade/plan, which returns the same buckets and transitions JSON. Scope: `read:system`.
      upgrade run (:483):
      - It takes a raw SQL `--where` predicate, runs the reingest worker in-process and writes the shadow file.
      - Move it to `dpkms upgrade run` with identical flags and consent guards (ADR-070 §1), beside the existing `dpkms upgrade` group (cmd/dpkms/cmd/upgrade.go).
      - Delete `ctxt upgrade run` outright. There's no alias.
      Tests, written first:
      - ctxt upgrade plan golden JSON from the fixture.
      - dpkms upgrade run: dry-run, the `--filter` / `--where` exclusivity, and the consent refusal.
      Acceptance: release-notes `none` row pointing operators to `dpkms upgrade run`.
      Operator-Impact: none.
      Action: add the upgrade plan endpoint, port upgrade run to the dpkms CLI, and delete it from ctxt.
    effort: M
    priority: P2
    tags: [phase:3, type:refactor, domain:server, domain:cli, domain:upgrade]
    blocked-by: [1, 3, 4]

  # ── phase 4: local capture and interactive surfaces ──────────────
  - title: "ctxt watch: local watchers enqueue over HTTP with ctxt-owned dedup state"
    description: |
      Defects today:
      - runWatchStart (cmd/ctxt/cmd/watch.go:141-219) ingests through a local Service.
      - It persists `dir:<path>` watch rows as active (watch.go:195-208 → internal/watcher/manager.go:79). `dpkms serve` on the same database then starts those watches too (cmd/dpkms/cmd/serve.go:328,547).
      - `watch stop` pauses every active row, including watches created through the server API, and doesn't stop the running process (watch.go:229-240).
      - File records store the job ID as object_id (manager.go:344-346), so deleting a file deletes nothing (manager.go:377-379).
      Change:
      - Split watcher's store dependency: config persistence stays optional, and file records go behind a `FileRecords` interface.
      - Add a JSON state-file implementation at $XDG_STATE_HOME/ctxt/watch/<instance-key>.state. It uses flock plus atomic rename, like internal/ambient/position, and its key comes from the resolved endpoint.
      - The ingester is dpkmsclient. Records keep job_id. On file delete, resolve result_id via GET /jobs/{id}, then DELETE /objects/{id}.
      - The clipboard watcher uses the same client.
      - `watch start` writes a run file (pid, endpoint, dirs).
      - `watch stop` signals that pid.
      - `watch status` reports process liveness, the clipboard setting and per-directory counts and last error from the state file, and never touches dpkms.
      The daemon keeps its DB-backed file records for server-side watches (/api/v1/watches).
      Tests, written first:
      - A restart doesn't re-enqueue unchanged files.
      - Switching instances re-ingests into the new instance.
      - A deleted file deletes the resulting object.
      - No watch row lands in the fixture database.
      - stop terminates start.
      Operator-Impact: none.
      Action: move ctxt watch to local state and HTTP ingest, and fix the job-ID deletion bug.
    effort: L
    priority: P1
    tags: [phase:4, type:fix, domain:cli, domain:capture, domain:watch]
    blocked-by: [5]

  - title: "Shell, TUI and ctxt:// TUI dispatch over the API; job cancel endpoint"
    description: |
      TUI:
      - Implement `types.ServiceAdapter` (internal/tui/types/types.go:27) over dpkmsclient: search as /find in fts mode, get, list, jobs, retry, analyze, and compose on the client.
      - Add POST /api/v1/jobs/{id}/cancel (scope `write:jobs`). Today only retry exists (server.go:133).
      - `tui` (tui.go:27) and `dispatchURIViaTUI` (root.go:534) use the adapter.
      Shell:
      - `shell` (shell.go:53-70) replaces the `sessionSvc` Service cache (helpers.go:39) with a session-scoped client.
      - Its job watcher (internal/repl/session.go:55), which listens on `svc.Bus`, subscribes to GET /api/v1/events (SSE) instead.
      Tests, written first:
      - Adapter conformance against the fixture.
      - The REPL job watcher prints a completion that arrives over SSE.
      - Cancel returns 409 for a finished job.
      Operator-Impact: none.
      Action: add the HTTP TUI adapter, the job-cancel endpoint and the SSE job watcher, and switch shell and tui.
    effort: M
    priority: P2
    tags: [phase:4, type:feat, domain:cli, domain:tui, domain:server]
    blocked-by: [8, 10]

  - title: "Upgrade banner from the API response, not the local shadow file"
    description: |
      The ADR-070 banner (cmd/ctxt/cmd/root.go:126-130) reads upgrade-state.json next to the local pidfiles, so it's blind to a remote instance.
      dpkms:
      - Set an `X-Dpkms-Upgrade` response header, a compact state summary, on every /api/v1 response while an upgrade is in flight.
      - The value comes from the upgrade manager that already feeds /healthz.
      ctxt:
      - dpkmsclient renders the banner on stderr from the first response that carries the header.
      - Remove the shadow-file read from ctxt's pre-run.
      Tests, written first:
      - The fixture with an in-flight upgrade shows the banner.
      - No header, no banner.
      - The banner never goes to stdout.
      Operator-Impact: none.
      Action: add the upgrade response header and render the banner from it.
    effort: S
    priority: P3
    tags: [phase:4, type:feat, domain:cli, domain:server, domain:upgrade]
    blocked-by: [2, 4]

  # ── phase 5: close the direct path ───────────────────────────────
  - title: "Delete ctxt's direct-store path and pin it with a dependency test"
    description: |
      Delete from cmd/ctxt/cmd:
      - `newService`, `sessionSvc`, `resolveStoragePath`, `dbPathForInstance` (helpers.go:36-101,144-201);
      - `embeddingBuildOpts`, `loadDetectors` (:209-259);
      - every remaining import of internal/storage/sqlite, internal/storage/postgres, internal/storageutil and internal/dblock.
      Dependency test: `go list -deps ./cmd/ctxt` contains none of those four packages.
      Config: `ctxt config validate` reports `storage.*` keys in a ctxt-only config file as ignored by ctxt.
      Docs:
      - Update docs/architecture.md (the client/substrate boundary);
      - docs/ctxt/api-cli.md (the endpoint map);
      - docs/environment-variables/core.md (CTXT_INSTANCE now selects an endpoint);
      - the manual pages the earlier tasks didn't already touch.
      - Scrub any leftover text that mentions "queued locally" or pidfile-only instance routing.
      Tests, written first:
      - The dependency test fails before the last import goes.
      - The full cmd suite is green against the fixture.
      Operator-Impact: none.
      Action: remove the remaining direct-store code from ctxt and add the dependency test.
    effort: M
    priority: P1
    tags: [phase:5, type:refactor, domain:cli]
    blocked-by: [5, 6, 8, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 22, 23, 24, 25]

  - title: "Remote black-box suite: built ctxt against a protected built dpkms"
    description: |
      The suite runs under the e2e build tag with the launcher from the test-substrate task.
      Setup:
      - `dpkms serve` with `server.access: protected` and static tokens for an admin principal and a reader principal, on a free port.
      - ctxt configured with a named `server.urls` entry and selected with `--instance`.
      Scenarios:
      - One command per resource group succeeds with the admin token.
      - A reader token exits 5 on a write.
      - A wrong token exits 5.
      - A stopped server exits 70.
      - No db.sqlite appears under ctxt's XDG_DATA_HOME.
      - `ctxt watch start` enqueues a new file into the remote instance.
      Assert exit codes on the built binary, not through `go run`.
      Acceptance: the suite is wired into the Makefile's e2e target.
      Operator-Impact: none.
      Action: add the remote e2e suite and wire it into make.
    effort: M
    priority: P1
    tags: [phase:5, type:test, domain:testing]
    blocked-by: [27]
---

# ctxt as a pure dpkms API client

> **Date:** 2026-09-27
> **Decision record:** [ADR-077 – ctxt Is a Pure dpkms API Client; Instances Resolve to Endpoints](../decisions/ADR-077-ctxt-pure-api-client.md) (Proposed)
> **Status:** design only. Nothing here is implemented yet. The first task is the owner's review of the open questions.
> **Audience:** whoever implements the tasks, and the owner reviewing the open questions

## Goal

ctxt on the operator's laptop works fully against a dpkms instance on another always-on host. That host is reached over a private network, runs `server.access: protected`, and accepts static bearer tokens. The target state:

- ctxt never opens the dpkms store. Every command talks to dpkms through its API, whether dpkms is local or remote.
- There is one code path, and no local or remote mode.
- An unreachable instance or a rejected token fails loudly with the right exit code.
- The direct-database path, the silent local-write fallback and the dead flags are deleted. Nothing has been released, so nothing needs to stay compatible.

## Current state

Every claim below was checked against `origin/next` at `d510142`.

### Three ways ctxt reaches data

1. **HTTP only.** These work remotely.
   - `status` (`cmd/ctxt/cmd/status.go:106`), `log` (`log.go:79`), `upgrade status` (`upgrade_dpkms.go:187`), `feed *` (`feed.go:144-311`) and every `import *` (`import_batch.go:102`, `import_email.go:95`, one `serverEndpoint` call per importer) go through `serverEndpoint` / `serverDo` (`server_endpoint.go:33-72`).
   - `capture [source]` has its own resolver, `captureEndpoints` (`capture.go:289`), and walker, `postCapture` (`capture.go:317-368`).
2. **HTTP, then a local fallback.**
   - `analyze` and bare `ctxt <content>` (`root.go:165` → `RunAnalyze`), `capture tabs` and `capture history` build an `idxbridge` whose `AnalyzeFallback` is `localDirectAnalyze` (`analyze.go:230-233`, `capture_tabs.go:322-325`, `capture_history.go:526-529`).
   - `list --q` builds one whose search `Fallback` is the local service (`list.go:156-160`).
3. **Straight into the store.** Everything else goes through `newService()` (`helpers.go:50`). It:
   - runs migrations (`:72`), even for read-only commands;
   - builds the pipeline registry with embedding options (`:78`);
   - loads detectors (`:88`);
   - writes the default registry manifest (`:96`).
   Where the database comes from:
   - `resolveStoragePath` (`:146`) takes `--instance`, `CTXT_INSTANCE` or the current-instance state, and resolves it only through local pidfiles (`dbPathForInstance`, `:177-201`).
   - Otherwise it falls back to `storage.path`, which defaults to `db.sqlite` in the data directory (`internal/config/config.go:1080`). The file is created and migrated on first use.
   - The embeddings commands open the same service and take its raw `*sql.DB` (`embeddings.go:597-615`).

### Defects found by the audit

Each defect has a task in the plan.

1. **Captures land where no daemon will process them.**
   - When no configured instance answers, analyze, capture tabs and capture history enqueue into the local database. The CLI prints "Queued locally; the job will run when the daemon starts" (`analyze.go:264`, `:270`).
   - With a remote dpkms, that daemon never reads this database.
   - The fallback also runs after a 401 or 403 (`internal/idxbridge/analyze.go:78-87`). A warning names the instance, then the write goes to the local store anyway.
2. **`list --q` needs a local store even when it searches remotely.** It opens the store first (`list.go:137`). After an auth rejection it answers from the local corpus with a warning (`idxbridge.go:309-316`).
3. **HTTP commands ignore `--instance` and `CTXT_INSTANCE`.**
   - `clientEndpoints` (`helpers.go:109-129`) never consults the instance selector.
   - Direct-store commands only accept locally running instances.
   - So no command can select a remote instance by name.
4. **No authorization.** Roles are attached (`internal/auth/static.go:54-59`) but never checked: `Principal.HasRole` (`internal/auth/auth.go:72`) has no caller. Any valid token can install steps, create server-side watches or delete pipelines.
5. **`ctxt watch` corrupts shared state.**
   - `watch start` persists `dir:<path>` rows as active (`watch.go:195-208` → `internal/watcher/manager.go:79`). `dpkms serve` on the same database then starts them too (`cmd/dpkms/cmd/serve.go:328`, `:547`).
   - `watch stop` pauses every active row, including server-API watches, and never stops the running `watch start` process (`watch.go:229-240`).
   - File records store the job ID as `object_id` (`manager.go:344-346`), so deleting a file deletes nothing (`manager.go:377-379`).
6. **Dead code.**
   - `Service.PluginRegistry` (`internal/service/service.go:48`) is never assigned, so the output-generator paths in `export` (`export.go:90-100`) and `show` (`show.go:85`) can't run, and `export --dest` does nothing.
   - `edit --hints` and `--decisions` are bound (`edit.go:59`, `:61`) but never applied.
7. **The ADR-070 banner is local-only.** It reads the shadow file next to local pidfiles (`root.go:126-130`), so an upgrade on a remote instance is invisible.

### Per-command audit and decision

In the endpoint column, **✓** marks an existing route. The scope column uses the classes from [Authorization](#authorization-route-scopes-and-role-bundles).

**Already HTTP.** Each moves onto `dpkmsclient` and the resolver in tasks 2, 3 and 5.

| Command | Today | Endpoint | Scope |
|---|---|---|---|
| `status` | `status.go:106`, `:169` | `GET /healthz` ✓, `GET /api/v1/whoami` (new) | none / any |
| `log` | `log.go:79`, `:107` | `GET /audit-log` ✓ | `admin:audit` |
| `upgrade status` | `upgrade_dpkms.go:187`, `:560` | `GET /healthz` ✓ | none |
| `feed add\|list\|sync\|remove` | `feed.go:144-311` | `/feeds…` ✓ (`server.go:170-174`) | `read:feeds`, `write:feeds` |
| `import *` | `import_batch.go:102-303`, `import_email.go:207`, `import_chrome.go:187` | `/import…`, `/importers/*/run`, `/pipelines/enqueue`, `/analyze` ✓ | `write:objects` |
| `capture [source]` | `capture.go:289`, `:317-368`; the 401/403 walk-on at `:357-361` | `POST /analyze` ✓, `POST /inbox` ✓ | `write:objects`, `write:inbox` |

**Hybrids.** The local fallback is deleted in tasks 5 and 6.

| Command | Today | Endpoint | Scope |
|---|---|---|---|
| `analyze`, bare `ctxt <content>` | `analyze.go:155-161`, `:230-271`; `root.go:165` | `POST /analyze` ✓ | `write:objects` |
| `capture tabs` | `capture_tabs.go:322-350` | `POST /analyze` ✓ | `write:objects` |
| `capture history` | `capture_history.go:526-554` | `POST /analyze` ✓ | `write:objects` |
| `list --q` | `list.go:137`, `:153-168` | `GET /search` ✓ | `read:objects` |

**Store only.** "Today" is the `newService` call, or the call that builds a service.

| Command | Today | Endpoint (existing ✓ / new) | Scope | Decision |
|---|---|---|---|---|
| `list` (filters, `--facets`, `--cursor`) | `list.go:137` | `GET /objects` ✓ (widened filters), `GET /objects/facets` | `read:objects` | ctxt. The cursor stays a client file (`internal/cursor`) |
| `show` | `show.go:65`, `:172` | `GET /objects/{id}` ✓, `GET /objects/{id}/related` | `read:objects` | ctxt. Delete the dead generator branch |
| `export` | `export.go:66` | `GET /objects/{id}` ✓ | `read:objects` | ctxt. Delete the generator lookup and `--dest` |
| `classify o-…` | `classify.go:111` | `GET /objects/{id}` ✓ | `read:objects` | ctxt. Classification stays client-side |
| `compose` | `compose.go:118` | `GET /objects` ✓ (widened) | `read:objects` | ctxt. Composition is client-side formatting (`service.go:1089-1130`) |
| `profile schema evolve` | `profile_schema.go:417` | `GET /objects` ✓ (widened) | `read:objects` | ctxt. The frequency analysis runs on the client (`schema_evolution.go:27-45`) |
| `find` (fts, vector, hybrid, `--explain`, `--facets`) | `find.go:171`, `:237` | `POST /find` | `read:objects` | ctxt sends the resolved profile knobs, and the query is embedded server-side |
| `find --graph` | `find_graph.go` | `GET /search/graph` (being built on the search-graph track) | `read:objects` | ctxt. Viewer and export stay client-side |
| `ctxt://…` | `root.go:504-540` | via `find`, `show` or the TUI adapter | — | follows those commands |
| `entity list\|show\|backlink` | `entities.go:133`, `:166`, `:230` | `/entities…` ✓ (`server.go:141-143`) | `read:objects` | ctxt |
| `entity search` | `entities.go:201` | `GET /entities?q=` | `read:objects` | ctxt |
| `resolve` | `resolve.go:126`, `:186` | `GET /objects/{id}` ✓, `GET /entities/resolve?mention=` | `read:objects` | ctxt |
| `page list\|show\|refresh` | `page.go:98`, `:135`, `:168` | `GET /pages`, `GET /pages/{slug}`, `POST /pages/{slug}/refresh` | `read:objects` / `write:objects` | ctxt |
| `index [topic]`, `--refresh` | `index.go:51` | `GET /topic-index[/{topic}]`, `POST /topic-index` | `read:objects` / `write:objects` | ctxt |
| `stats` (`--watch`) | `stats.go:66` (lists every object to count types, `:76-84`) | `GET /stats` | `read:objects` | ctxt. Counts are computed server-side |
| `inbox list` | `inbox.go:149` | `GET /inbox` ✓, `GET /inbox/queue` | `read:inbox` | ctxt |
| `inbox triage\|discard` | `inbox.go:236`, `:261` | `POST /inbox/{id}/triage\|discard` ✓ | `process:inbox` | ctxt |
| `inbox clear` | `inbox.go:283` | `POST /inbox/clear` | `process:inbox` | ctxt |
| `link create\|list\|delete` | `link.go:85`, `:184`, `:369` | `POST /edges`, `GET /objects/{id}/edges`, `DELETE /edges` | `write:` / `read:` / `delete:objects` | ctxt |
| `edit` | `edit.go:98` | `PATCH /objects/{id}` ✓ (widened past type and subtype, `handlers_objects.go:69-74`) | `write:objects` | ctxt. Delete the dead `--hints` and `--decisions` |
| `delete` | `delete.go:150`, `:366` | `GET /objects` ✓, `DELETE /objects/{id}` ✓ | `delete:objects` | ctxt. Bulk delete runs one request per ID |
| `reprocess` | `reprocess.go:67-100` (local providers) | `POST /objects/{id}/reprocess` | `write:objects` | the step runs as a dpkms job |
| `remind set\|clear\|list` | `remind.go:96`, `:120`, `:136` | `PUT\|DELETE /objects/{id}/reminder`, `GET /object-reminders` | `write:objects` / `read:objects` | ctxt |
| `resurface` / `refresh` / `dismiss` | `resurface.go:128`, `:195`, `:219` | `POST /resurfacing/take`, `/resurfacing/refresh`, `/resurfacing/{id}/dismiss` | `write:objects` | ctxt. Its config travels in the request |
| `ingest` (adapters, `--stdin`, `--every`) | `ingest.go:77-83`, `internal/ingest/runner.go:87`, `:116` | `POST /ingest` | `write:objects` | ctxt. The adapter runs locally; dedup by source key happens server-side |
| `audit list\|export` | `audit.go:128`, `:184` | `GET /audit-log` ✓ | `admin:audit` | ctxt. Same endpoint as `log` |
| `doctor` | `doctor.go:52-80` | `POST /lint` | `read:objects` | ctxt, because knowledge lint is brain intent. The server appends the audit record |
| `registry list` | `registry.go:349` | `GET /steps/registries` ✓ | `read:registries` | ctxt |
| `registry info\|usage\|capabilities\|entitlements` | `registry.go:470`, `:708`, `:861`, `:1047` | `GET /registries/{name}`, `/registries/usage`, `/registries/{name}/capabilities`, `/entitlements` | `read:registries` | ctxt |
| `registry add` | `registry.go:387` | `POST /steps/registries/fetch` ✓ | `sync:registries` (admin) | ctxt. A subscription is a knowledge decision |
| `registry delete\|sync` (`--dry-run`) | `registry.go:437`, `:540` | `DELETE /registries/{name}`, `POST /registries/{name}/sync` | `sync:registries` (admin) | ctxt |
| `embeddings list` | `embeddings.go:217` | `GET /embeddings/models` | `read:embeddings` | ctxt |
| `embeddings provider [id]` | `embeddings.go:302` (the store is opened only with an ID) | `GET /embeddings/provider` | `read:embeddings` | ctxt shows the server's resolution. Delete the client overrides |
| `embeddings register` | `embeddings.go:396` | `POST /embeddings/models` | `admin:embeddings` | ctxt. The provider probe must run server-side |
| `embeddings set-default\|deprecate` | `embeddings_lifecycle.go:213`, `:300` | `POST /embeddings/models/{id}/default\|deprecate` | `admin:embeddings` | ctxt |
| `embeddings purge` | `embeddings_lifecycle.go:408` | `DELETE /embeddings/models/{id}` | `admin:embeddings` | ctxt |
| `embeddings migrate` | `embeddings_migrate.go:105-135` (jobs-table handoff) | `POST /embeddings/migrations` | `admin:embeddings` | ctxt |
| `upgrade plan` | `upgrade_dpkms.go:304`, `:426` | `GET /upgrade/plan` | `read:system` | ctxt |
| `upgrade run` | `upgrade_dpkms.go:483` (raw SQL `--where`, SQLite only, in-process worker) | — | — | **moves to `dpkms upgrade run`**. It is substrate maintenance over the storage schema |
| `watch start` | `watch.go:142`, `:159`, `:175` | `POST /analyze` ✓, `GET /jobs/{id}` ✓, `DELETE /objects/{id}` ✓ | `write:objects`, `delete:objects` | ctxt. Watchers and dedup state are local |
| `watch stop\|status` | `watch.go:222`, `:247` | — | — | ctxt. Local process and state file only |
| `shell` | `shell.go:54`, `internal/repl/session.go:55` | as its subcommands, plus `GET /events` ✓ (SSE) | `read:*` | ctxt |
| `tui`, `ctxt://` in the TUI | `tui.go:27`, `root.go:534` | adapter over the above, plus `POST /jobs/{id}/cancel` | `read:objects`, `write:jobs` | ctxt |
| `instance list\|use\|current` | `instance.go:114-238` (pidfiles only) | — | — | ctxt. Named endpoints plus local instances (task 3) |

These commands touch no store and are out of scope: `cursor`, `config`, `profile` (other than `schema evolve`), `watch enable|disable`, `capture schedule`, `capture browsers`, `lateral`, `registry search|login|logout|submit`, `uri`, `secret`, `key`, and the deprecated `job` / `detector` / `dev` forwarders.

## Design

### Client package

`internal/dpkmsclient` is the only way ctxt reaches dpkms. It replaces:

- `serverDo` / `serverGet` / `contactError` (`server_endpoint.go:44-88`);
- `postCapture` (`capture.go:317-368`);
- all of `internal/idxbridge`.

It provides:

- a bearer token on every request;
- JSON encode and decode;
- an `Idempotency-Key` per logical submission;
- the failover walk (dial or DNS failures only, announced on stderr);
- error bodies decoded into kit envelopes with ADR-077's exit classes;
- typed methods per resource.

Commands never build URLs themselves.

### Endpoint resolution

This is ADR-077 §3. In short: `--server` > `--instance` / `CTXT_INSTANCE` > state file > `server.urls` > `server.url` > `http://127.0.0.1:8080`.

- A name resolves to a named `server.urls` entry first, then to a running local instance by pidfile name or port.
- Steps 1 to 3 give one endpoint. Steps 4 to 6 give an ordered list for the failover walk.
- The resolved endpoint also keys client-side state: the watch state file and, pending an open question, the browser-history position.

Example laptop config:

```yaml
server:
  token: <admin token for home>
  urls:
    - name: home
      url: https://dpkms.example.ts.net:7700
    - name: local
      url: http://127.0.0.1:8080
      token: ""
```

### Authorization: route scopes and role bundles

Every route declares one scope. The static provider's roles expand to bundles:

- `admin` holds every scope.
- `reader` holds every `read:*` scope.
- Private instances grant every scope.

| Class | Routes |
|---|---|
| `read:*` (reader) | every `GET` under `/api/v1` except the audit log; `POST /find`; `POST /lint`; `GET /events`; the MCP mount; `GET /whoami` |
| `write:objects`, `write:inbox`, `write:feeds`, `write:jobs` | analyze, capture, inbox capture, object `PATCH`, reprocess, edges, reminders, resurfacing, pages refresh, topic-index rebuild, ingest, import and importer runs, `pipelines/enqueue`, feed mutations, job retry and cancel, aliases, saved searches, suggestions approve and reject, `system/reminders` dismiss, entity pull |
| `delete:objects`, `process:inbox` | object and edge deletes, clearing search history; inbox triage, discard and clear |
| admin-only (`admin:*`, `sync:registries`) | pipeline create, delete and archive; step install and uninstall; registry fetch, update, delete and sync; `entities/registry-sync`; server-side watch mutations (they read the server's filesystem); the embedding lifecycle; the audit log |
| unchanged | `POST /federation/push` keeps its own credential rule (`server.go:241`) |

gRPC methods get the same scopes through the interceptors in `internal/server/grpc/auth.go:33`, `:49`. Otherwise gRPC would bypass the HTTP checks.

### Brain settings travel with the request

ctxt resolves profiles, search strategy, resurfacing config and lint thresholds from its own config, and sends the values it resolved. Examples: `find` sends `rrf_k`, the weights and pools and `min_score`; `resurfacing/refresh` sends its window and scoring. dpkms applies what arrives, so the brain's configuration has one owner.

### Watch: local watchers, local state

- Watch configs come from ctxt config (`watch.dirs`, `watch.clipboard`), as today.
- File records move to `$XDG_STATE_HOME/ctxt/watch/<instance-key>.state`, a versioned JSON file.
  - Every mutation holds a flock and replaces the file atomically, as `internal/ambient/position` does.
  - A corrupt file is an error and is never rewritten.
- Records hold `job_id`. A file delete resolves `result_id` through `GET /jobs/{id}`, then deletes that object.
- `watch start` writes a run file (pid, endpoint, dirs). `watch stop` signals that pid.
- `watch status` reads the run file and the state file only.
- The daemon keeps its database-backed records for its own server-side watches (`/api/v1/watches`).
- A JSON state file is enough for vault-sized directories (thousands of files, one rewrite per debounced batch). If a real directory outgrows it, the next step is a ctxt-owned SQLite file, never the dpkms store.

### Test substrate

- **Command tests.** They run against an in-process dpkms: `httptest` around the real router and service on a temporary SQLite. Seeding through the driver still works, so existing tests keep their fixtures.
- **E2E.** It starts the built `dpkms serve` on a free port with hermetic HOME and XDG dirs.
- **Guards.**
  - Every test binary goes through `internal/testguard`, which blocks `:8080` and `:8081` and points the default `server.url` at a closed port.
  - External providers (embedding and enrichment models) use recorded `xrr` cassettes.
  - `httptest` is used only for our own dpkms endpoints.

## Missing dpkms endpoints

These are new or widened. Scopes are as in the table above.

| # | Endpoint | Serves |
|---|---|---|
| 1 | `GET /api/v1/objects`: widen the filters to the full `ObjectFilter` | list, compose, delete, schema evolve |
| 2 | `GET /api/v1/objects/facets` | list `--facets`, find `--facets` |
| 3 | `GET /api/v1/objects/{id}/related` | show |
| 4 | `PATCH /api/v1/objects/{id}`: widen to title, summary, tags, mentions, subtype | edit |
| 5 | `POST /api/v1/objects/{id}/reprocess` | reprocess |
| 6–8 | `POST /api/v1/edges`, `GET /api/v1/objects/{id}/edges`, `DELETE /api/v1/edges` | link |
| 9 | `POST /api/v1/find` | find, the TUI search |
| 10 | `GET /api/v1/search/graph` (being built on the search-graph track) | find `--graph` |
| 11 | `GET /api/v1/entities?q=` | entity search |
| 12 | `GET /api/v1/entities/resolve?mention=` | resolve |
| 13–15 | `GET /api/v1/pages`, `GET /api/v1/pages/{slug}`, `POST /api/v1/pages/{slug}/refresh` | page |
| 16–18 | `GET /api/v1/topic-index`, `GET /api/v1/topic-index/{topic}`, `POST /api/v1/topic-index` | index |
| 19 | `GET /api/v1/stats` | stats |
| 20 | `GET /api/v1/inbox/queue` | inbox list `--pending/--failed/--raw` |
| 21 | `POST /api/v1/inbox/clear` | inbox clear |
| 22–24 | `PUT` and `DELETE /api/v1/objects/{id}/reminder`, `GET /api/v1/object-reminders` | remind |
| 25–27 | `POST /api/v1/resurfacing/take`, `…/refresh`, `…/{id}/dismiss` | resurface |
| 28 | `POST /api/v1/ingest` | ingest |
| 29 | `POST /api/v1/lint` | doctor |
| 30–33 | `GET /api/v1/registries/{name}`, `…/usage`, `…/{name}/capabilities`, `GET /api/v1/entitlements` | registry reads |
| 34–35 | `DELETE /api/v1/registries/{name}`, `POST /api/v1/registries/{name}/sync` | registry delete and sync |
| 36–37 | `GET /api/v1/embeddings/models`, `GET /api/v1/embeddings/provider` | embeddings list and provider |
| 38–42 | `POST /api/v1/embeddings/models`, `…/{id}/default`, `…/{id}/deprecate`, `DELETE …/{id}`, `POST /api/v1/embeddings/migrations` | embedding lifecycle |
| 43 | `GET /api/v1/upgrade/plan` | upgrade plan |
| 44 | `POST /api/v1/jobs/{id}/cancel` | TUI |
| 45 | `GET /api/v1/whoami` | status, instance current |
| — | `X-Dpkms-Upgrade` response header (not an endpoint) | the ADR-070 banner |

One command moves to the `dpkms` CLI: `ctxt upgrade run` becomes `dpkms upgrade run`.

## Phases

The order keeps ctxt usable after every merge:

- Each task switches a whole command group, together with the endpoints it needs, in one change.
- Commands not yet switched keep their current path.
- Only the last deletion task needs every command to have moved.

Indices match the frontmatter's 0-based `blocked-by`.

| Phase | Idx | Task |
|---|---|---|
| 0: gate and foundations | 0 | Owner review of ADR-077 and the open questions |
| | 1 | Test substrate: in-process dpkms, built-binary launcher |
| | 2 | `internal/dpkmsclient` with the exit-class mapping |
| | 3 | Named endpoints; `--instance` for every HTTP command |
| | 4 | Route scopes, role bundles, gRPC parity, `whoami` |
| 1: stop silent local writes | 5 | Enqueue path without fallback (analyze, bare, capture, tabs, history) |
| | 6 | `list --q` remote only; delete `idxbridge` |
| 2: reads onto the API | 7 | Object read surface (filters, facets, related) |
| | 8 | list, show, export, classify, compose, schema evolve |
| | 9 | `POST /find` |
| | 10 | find, `ctxt://search` |
| | 11 | find `--graph` (after the search-graph endpoint merges) |
| | 12 | Entities search and resolve; entity and resolve commands |
| | 13 | Pages, topic index, stats |
| | 14 | audit, doctor |
| | 15 | inbox |
| | 16 | remind, resurface |
| 3: writes and operator commands | 17 | edit, delete, reprocess |
| | 18 | link and edges |
| | 19 | ingest |
| | 20 | registry |
| | 21 | embeddings reads |
| | 22 | embeddings lifecycle (admin) |
| | 23 | upgrade plan endpoint; `upgrade run` → `dpkms` |
| 4: local capture and interactive | 24 | `ctxt watch` local state, HTTP ingest |
| | 25 | shell, TUI, job cancel |
| | 26 | Upgrade banner from the response header |
| 5: close | 27 | Delete the direct path; dependency test; docs |
| | 28 | Remote black-box e2e suite |

Tasks 7 to 23 can run in parallel once tasks 1, 3 and 4 have landed, apart from their listed dependencies. Tasks that add routes all edit `internal/server/http/server.go`, so serialize their merges or rebase each onto the last.

## Rules for every task

- **Test first.** The new test fails on the code before the change.
- **Mutation check.** Put the defect back, or remove the new check, and confirm the test goes red.
- **Hermetic tests.**
  - Command tests use the in-process fixture.
  - Every new test package calls `testguard.Main`.
  - Nothing reaches `127.0.0.1:8080` or `:8081`, or a real HOME or XDG directory.
  - External providers use `xrr` cassettes.
- **Delete the dead path in the same change.** That means the command's `newService` call, its local branches and any flag that no longer means anything. There are no aliases or shims.
- **Operator impact.**
  - Every task is ADR-070 `Operator-Impact: none`; no trailer is needed.
  - A task that changes what an operator sees adds a `none` row to the day's `docs/release-notes/` file. Examples: new exit codes, lost reader write access, a moved command.
- **Docs.** Each task updates its own manual or API section. The closing task does the architecture page and the final scrub.

## Related, not in this plan

- **Browser extension cookie bridge** reachability from a remote dpkms.
- **`/ws/bus`** reachability from a remote dpkms. The shell uses SSE on `/api/v1/events` instead.
- **Offline capture buffering** through ADR-066's buffer, which would replay captures once dpkms is reachable again.
- **Per-token explicit scopes** in `server.auth.static.tokens`, for capture-only devices.
- **The `dpkms` CLI's own direct-store commands** (`job`, `detector`, `housekeeping`) are host-local by design and stay that way. `cmd/dpkms/cmd/api_client.go` could later reuse `dpkmsclient`.
- **Dead `capture` flags** `--ambient`, `--input`, `--skip` and `--window` (Track 2, `capture.go:106-108`).
- **Registry route naming.** Knowledge registries are listed and fetched under `/api/v1/steps/registries` (`server.go:165-167`).
- **An unrouted retrieval handler,** `Retrieve` (`internal/server/http/handlers_retrieve.go:30`), is mounted nowhere.

## Open questions

The owner decides these in task 0.

1. **Scopes versus a `writer` role.**
   - ADR-023 (Accepted) chose capability scopes and rejected RBAC. The static provider issues `admin` and `reader` roles.
   - This plan enforces scopes per route, with the two roles as fixed bundles. A capture-only token then needs admin until per-token scopes exist.
   - Recommendation: accept the bundles now, and add per-token `scopes` as the follow-up instead of a third role.
2. **Failover across `server.urls`.**
   - A laptop listing a remote instance and then a local one would, on a dial failure, write to the local instance: a different corpus, but announced.
   - Recommendation: keep the failover, restricted to dial failures and announced on stderr. The alternative is exactly one endpoint per invocation.
3. **Offline capture.**
   - With no fallback, capturing while the remote instance is unreachable exits 70.
   - Recommendation: accept this for now. A local dpkms (ADR-074's hub topology) or the ADR-066 buffer is the later answer.
4. **ADR-075 amendment.**
   - The CLI stops being a hook host. Before-hooks and outbox writes run only in the daemon.
   - `exec` hooks come from the daemon host's config.
   - `ctxt events deliver`, which drains the outbox without a daemon, loses its purpose.
   - Confirm the amendment, and that ADR-075's first increment hosts its `domain.Service` wrappers in the daemon only.
5. **Profile placement.** Profiles stay in ctxt config, and their values travel with each request. Server-side pipelines that read profile config (for example analyze with `--profile`) still read the server's copy. Should profiles eventually be held by dpkms and served through the API?
6. **Registry tokens.**
   - `ctxt registry login` stores tokens in the laptop keychain (`registry.go:803`).
   - Nothing reads that store: `NewTokenStore` has no other caller.
   - Registry sync now runs on the dpkms host. Should registry credentials become dpkms host secrets?
7. **Browser-history position per instance.**
   - The history position file (`internal/ambient/position`) isn't keyed by instance, so switching instances skips history that was already sent elsewhere.
   - Recommendation: key it by the resolved endpoint, as the watch state is keyed.
8. **`upgrade run` placement.** Moving it to `dpkms upgrade run` needs a shell on the dpkms host. The alternative is an admin endpoint that enqueues the reingest job and accepts only `--filter` (no raw SQL).
9. **Owner metering.** On protected instances, the inbound entitlement gate filters the owner's own entity lists and meters each entity read (`handlers_entities.go:30-38`, `:57`; wired at `serve.go:369-371`). Should admin principals bypass the gate?

## Resolved decisions

None yet. Task 0 fills this in.
