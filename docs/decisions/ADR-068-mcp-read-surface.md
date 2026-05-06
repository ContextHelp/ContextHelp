# ADR-068 – MCP Read-Surface (Dual: dpkms-Authoritative + ctxd-Local)

> **Status:** Accepted
> **Date:** 2026-05-05
> **Author:** jadb
> **Applies to:** dpkms, ctxd, ctxt CLI
> **Supersedes:** None
> **References:** ADR-023 (auth), ADR-038 (SuperMemory MCP pattern), ADR-052 (HTTP+gRPC listeners), ADR-056 (unified enqueue API), ADR-064 (federation), ADR-065 (pluggable adapters), ADR-066 (ambient capture substrate), ADR-067 (sessions), prior art: OpenChronicle reader MCP (`~/.p/sandbox/OpenChronicle/src/openchronicle/mcp/server.py`, `docs/mcp.md`); SuperMemory MCP server pattern (referenced in ADR-038)

---

## Context

ctxt and dpkms today expose a rich set of read APIs for human and programmatic clients but **zero MCP surface**. Agents that want to query the knowledge graph either shell out to the `ctxt` CLI or talk to the existing HTTP/gRPC endpoints directly — neither flow is what tool-capable agents (Claude Code, Claude Desktop, Cursor, Codex, opencode) discover and consume natively. The MCP standard exists precisely to bridge this gap, and ADR-038 already locked in the commitment: *"When ctxt implements agent integration (US-0037 through US-0041), an MCP server exposing `ingest()`, `search()`, and `compose()` tools would enable the same integration pattern without coupling to a specific AI framework."*

ADR-066 elevated this to imminent work: the ambient-capture daemon produces a steady stream of small KnowledgeObjects, and the user's natural follow-up — "what was I just doing? what did I capture today? compose a summary of yesterday afternoon" — only pays off if agents can ask these questions inside the user's existing chat UI without shelling out.

OpenChronicle (`~/.p/sandbox/OpenChronicle/docs/mcp.md`) demonstrates a working reader MCP server: streamable-HTTP on `127.0.0.1:8742/mcp`, in-daemon (warm process, stable URL), eight read-only tools spanning compressed and raw memory, server-level instructions teaching the client *"this is the user's personal memory; CALL THESE TOOLS FIRST."* It also documents the install workflows for the major clients (Claude Code, Claude Desktop, Codex, opencode, Cursor, ChatGPT-via-tunnel) — a non-trivial UX problem we shouldn't reinvent.

**Existing ctxt/dpkms read surfaces (pre-ADR-068)**:

| Surface | Where | Scope |
|---|---|---|
| HTTP REST `/api/v1/*` | dpkms `internal/server/http/server.go:49–168` | Broad: objects, entities, jobs, search, inbox, watches, feeds, suggestions, audit-log, … |
| gRPC services | dpkms `internal/server/grpc/{query,analyze,jobs,entity}.go` | `QueryService` (Search, ListObjects, GetObject, NodeAwareSearch), `EntityService`, `JobService`, `AnalyzeService` |
| SSE event stream | `GET /api/v1/events` | Real-time updates |
| WebSocket bus | `GET /ws/bus` | kit/runtime/bus topics; `BUS_TOKEN` env-gated |
| CLI | `cmd/ctxt/cmd/` | 65 commands across CAPTURE/KNOWLEDGE/COMPOSE/CURATE/ORGANIZE/INTERACT/INSTANCE |
| SPA `/ui/` | dpkms | Embedded Vue.js dashboard |

The substantive work — FTS, vector search, entity resolution, edge traversal, profile scoping, RSQL — is **already done**. ADR-068 is mostly about which tools to expose, where to mount them, and how profile/auth contexts thread through.

**Critical constraint inherited from ADR-066:** dpkms is often deployed remote. Local-machine state — the ambient buffer (events captured but not yet enqueued because dpkms is unreachable, or because kit/policy held them), the active session in `ctxd`'s cutter, the in-flight session-opened/closed events — is **not visible to dpkms**. Agents running on the user's machine want both kinds of context: durable knowledge (dpkms) and live local state (ctxd). A single dpkms-only MCP surface leaves a real hole.

**The question:** Where does MCP mount, what tools does it expose, how does it compose with the existing HTTP/gRPC/CLI/bus surfaces, and how does the dpkms-remote constraint shape the architecture?

---

## Decision

**Adopt a dual MCP read-surface:**

### 1. dpkms-side authoritative MCP server (`/api/v1/mcp/`)

Mount the MCP server **inside the existing dpkms HTTP listener** (`internal/server/http/server.go`) as a sibling of `/api/v1/*` REST routes. Streamable-HTTP transport per MCP spec 2025-03-26 (SSE deprecated). Localhost-only by default; profile-scoped; reuses the existing auth model (ADR-023) when enabled.

**Rationale for in-process mount (not sidecar, not adapter `Serve`):**
- The HTTP listener is already up; storage drivers, profile resolution, RSQL, FTS indexes, and vector store are already in memory. A sidecar would have to either re-open the same SQLite file (with its own connection pool) or RPC into dpkms — both worse than calling the existing service-layer functions directly.
- ADR-065 adapters' `Serve` capability is for **protocol slots** (one-platform-per-protocol: e.g. Stalwart-as-IMAP, cardamum-as-CardDAV). MCP isn't a protocol slot; it's a multiplexer over many query operations across the whole graph. Forcing it through the adapter substrate would violate the substrate's invariants without buying anything.
- ADR-052 already settled "separate HTTP + gRPC listeners"; MCP-over-streamable-HTTP slots into the HTTP listener naturally.

**Tools (initial set — extensible):**

| Tool | Wraps | Description (the docstring the MCP client sees) |
|------|-------|--------|
| `search(query, top_k=10, since?, until?, profile?, kinds?)` | `QueryService.Search` + node-aware FTS | "Hybrid full-text + semantic search across the user's knowledge graph. Best when you have specific keywords." |
| `list(kind, filter?, limit=20, profile?)` | `QueryService.ListObjects` / `EntityService.ListEntities` | "List objects, entities, mentions, sessions, watches by filter. Use for browsing." |
| `get(id, profile?)` | `QueryService.GetObject` | "Fetch one object by ID. Use after search/list points you at it." |
| `entity(slug, profile?)` | `EntityService.GetEntity` + backlinks | "Resolve an entity (person, project, topic) and return its facts + backlinks." |
| `recent(since='today', limit=20, kind?, profile?)` | `ObjectStore.List(filter)` ordered by `created_at desc` | "Newest-first cross-graph feed. Best for 'what's new / what has the user been up to.'" |
| `sessions(since?, until?, limit=20, profile?)` | new `SessionStore` query | "List recent work sessions (per ADR-067). Returns id, time range, end_reason, app_mix, event_count." |
| `session(id, profile?)` | new `SessionStore.Get` + `ObjectStore.List(session_id=…)` | "Read one session: metadata + every KnowledgeObject captured during it. The grouping unit for ambient capture." |
| `compose(template, scope?, profile?)` | new bridge into existing `ctxt compose` runtime | "Synthesize a document from a saved compose template. Scope can be `--session sess_X`, `--since 2h`, etc." |
| `mentions(target, depth=1, profile?)` | `EdgeStore.RelatedObjectIDs` | "Graph traversal: objects mentioning `target` (entity slug or object id), optionally transitive." |
| `schema()` | static | "Return the storage taxonomy: object types/subtypes, kinds, edge types. Useful for agents constructing queries." |

All tools are **read-only**. Writes (`ingest`, `submit`) are not exposed in v1 per OpenChronicle's principle: "writes are the writer's job alone" (`docs/mcp.md` line 433). Agents trigger ingestion via the existing `ctxt analyze` HTTP endpoint, not MCP. ADR-038's `ingest()` tool is **deferred** — the read surface lands first; write surface is a separate, later ADR.

**Server-level instructions** (passed to MCP client per OC's pattern):

> ctxt is the user's personal knowledge graph — captures, decisions, mentions, sessions, projects, people, recent activity. CALL THESE TOOLS FIRST whenever the user asks about themselves, their work, their recent context: *"what was I doing this afternoon?" / "what did I decide about X?" / "summarize my last session on Y" / "who is Alice?"* — prefer this graph over replying "I don't know." There are two layers: durable graph (dpkms; tools above) and live local state (ctxd; see the local MCP if attached).

### 2. ctxd-side local MCP server (`http://127.0.0.1:<port>/mcp`)

Mount a **second, smaller MCP server inside `ctxd`** (the local ambient daemon from ADR-066). Streamable-HTTP, separate port (default 8744; dpkms keeps 8742-equivalent or whatever the existing HTTP listener uses).

**Why a second server:** the local daemon owns information dpkms cannot see when remote — buffered events not yet enqueued (dpkms-down, network partition, kit/policy hold), the live session the cutter is currently maintaining, the local fingerprint dedup state. Agents on the user's machine that need "right-now-on-this-laptop" context attach to both servers and merge. Agents on a different machine attach only to dpkms.

**Tools (intentionally minimal; mirror dpkms shapes where they overlap):**

| Tool | Source | Description |
|------|--------|--------|
| `current_session()` | cutter live state | "The active session if any: id, started_at, app_mix-so-far, event_count-so-far. Returns `null` when no session is open." |
| `recent_local(limit=20, source?)` | local buffer | "Most recent ambient events captured on this machine. Includes events buffered awaiting enqueue (dpkms unreachable)." |
| `pending_enqueue()` | buffer queue | "Events captured locally that have NOT been enqueued to dpkms. Surfaces dpkms-down state to the agent." |
| `sources()` | runner registry | "List of ambient sources active on this machine and their last-event timestamps." |
| `health()` | substrate | "Daemon status: enqueue endpoint, last successful enqueue, buffer size, retention state." |

The local server is **strictly local**. It does not proxy to dpkms; agents needing dpkms data attach to the dpkms server directly. This avoids confused double-counting (local-buffer + dpkms-stored returning overlapping events) and keeps each server's responsibility clean.

### 3. Transport, install, and discovery

- **Transport:** streamable-HTTP per MCP spec 2025-03-26. SSE NOT supported (deprecated upstream). stdio fallback for clients that need it (`ctxt mcp serve` and `ctxd mcp serve` subcommands; spin a fresh stdio server reading the same SQLite/buffer — SQLite WAL allows concurrent readers).
- **Install command** (port OC's `openchronicle install <client>` UX): `ctxt mcp install <client>` writes the appropriate config for `claude-code`, `claude-desktop`, `codex`, `cursor`, `opencode`, `mcp-json`. Two endpoints registered per `--client` invocation: `ctxt-graph` → dpkms server, `ctxt-local` → ctxd server (skipped if ctxd not detected). Idempotent.
- **Discovery:** ctxd advertises its endpoint via `~/.local/state/ctxt/ambient/mcp.json` (port + auth token if present); `ctxt mcp install` reads this for the local entry.

### 4. Auth, profile, network posture

- **Default network posture:** `127.0.0.1` bind only on both servers. Binding `0.0.0.0` requires explicit `--public` flag and a configured token (per ADR-023's "optional, layered" model).
- **Auth:**
    - Local-only (default): no auth required; OS file/socket permissions are the boundary.
    - Public bind: bearer token from config (`mcp.auth_token`) or `BUS_TOKEN`-style env var. Reuses the existing auth-layer hook from ADR-023 when implemented.
    - Tunnel exposure (ChatGPT-Desktop scenario per OC `docs/mcp.md` lines 296–351): explicitly **out of scope for v1**. Document the trade-off; require auth token; do not ship a tunnel command.
- **Profile resolution:** per-tool `profile?` argument, falling back to the daemon's configured default profile (`profile.default` viper key). MCP context-level profile pinning is rejected for v1 (clients aren't reliable about preserving context across tool calls; per-call argument is explicit).
- **Profile enforcement:** v1 trusts the caller (matches existing HTTP/gRPC behavior — profile_id is semantic-only per the survey). Hard enforcement is deferred to whenever the auth layer (ADR-023) gets enforced enforcement.

### 5. Bus events

Per kit/runtime/bus 4-segment past-tense convention:

| Topic | Emitted when |
|-------|--------------|
| `dpkms.mcp.tool.invoked` | Any MCP tool call lands; payload includes tool name + args (sanitized) |
| `dpkms.mcp.tool.completed` | Tool call returned successfully |
| `dpkms.mcp.tool.failed` | Tool call errored (carries error category) |
| `dpkms.mcp.session.opened` | MCP client connected |
| `dpkms.mcp.session.closed` | MCP client disconnected |
| `ctxt.mcp.tool.invoked` / `…completed` / `…failed` | Same shape, `ctxt.*` namespace, emitted by ctxd-side server |

kit/runtime/policy CEL rules can subscribe to `*.mcp.tool.invoked` to gate sensitive tools (e.g. "agents in profile=work cannot call `mentions(target='alice@home')`"). Privacy guards at the tool-call boundary are the natural place for fine-grained agent-access policy.

---

## Rationale

### Chosen: in-process MCP mounted on existing HTTP listener; second tiny server in ctxd

- **Reuse the existing service layer.** Storage drivers, profile resolution, RSQL parser, FTS, vector store, entity resolver are already loaded. An in-process MCP is a thin tool-dispatch shim over them, not a re-implementation. Estimated ~500 LoC for the dpkms-side server, ~150 for ctxd-side.
- **Stable URL = configure-once for clients.** Per OC `docs/mcp.md` lines 9–14, the in-daemon pattern is explicitly chosen so MCP clients don't need to know how to spawn the server. dpkms is already long-running; piggyback.
- **Two MCP servers handle the dpkms-remote topology cleanly.** When dpkms is local: agents see one machine, two endpoints, unified picture. When dpkms is remote: agents on the user's laptop attach to both; agents elsewhere attach only to dpkms; no architecture invented for the case where some context is local-only.
- **Read-only v1 matches the OC discipline and respects the existing pipeline.** Writes go through the existing `ctxt analyze` enqueue path, where pipelines, plugins, mention extraction, and policy already run. Adding MCP `ingest` would force every write-side concern to be re-implemented at the MCP boundary; better to point agents at the existing endpoint.
- **Bus-emitted tool events make MCP traffic observable AND policy-gateable from day one.** Agents calling tools the user didn't intend (or in a privacy-sensitive context) is a real risk; CEL rules on `*.mcp.tool.invoked` give us the lever to address it without a new auth model.
- **Install-command UX is borrowed wholesale from OC.** Their `openchronicle install <client>` matrix (`docs/mcp.md` lines 208–411) is mature; reinventing it is wasteful. Port the patterns and known-good config shapes for each client.

### Rejected alternatives

1. **Mount MCP as an ADR-065 adapter `Serve` capability.** Rejected: adapters are protocol-slot-bound (one-platform-per-protocol: email, contacts, calendar). MCP isn't a protocol slot — it's a query multiplexer spanning the whole graph. Stuffing it in would either violate the one-per-protocol invariant or invent a synthetic "agent" protocol slot for dubious benefit. The substrate exists for protocol federation; MCP is API exposure. Different layer.

2. **Sidecar process (`ctxt-mcp` binary spawned by dpkms).** Rejected: doubles deployment complexity; either re-opens SQLite (separate connection pool, shared-cache contention) or RPCs back to dpkms (network hop for what should be a function call). The in-process mount has none of these problems.

3. **Single dpkms-only MCP server (no ctxd-side).** Rejected: leaves a real gap when dpkms is remote. Agents on the user's machine would have no view of in-flight ambient capture or the active session. Two minimal servers cost less than one big server + a workaround for the gap.

4. **Single ctxd-side proxy server fronting dpkms.** Rejected: forces every dpkms query through the local machine, including from agents on other machines that have direct dpkms access. Adds latency and a local-daemon dependency where none is needed.

5. **MCP tools that wrap RSQL directly (`query(rsql=...)`).** Rejected for v1: agents are not reliable RSQL constructors yet (US-0037 — agent discovers query schema, US-0038 — agent constructs RSQL — are still open). Named tools (`search`, `list`, `get`, `entity`, `recent`, `session`) are easier for agents to choose correctly. RSQL exposure can come later as an `rsql_query` tool when the schema-discovery story is solid.

6. **Including write tools (`ingest`, `submit`) in v1.** Rejected: writes carry pipeline-selection, mention-extraction, policy-gating, and profile-write-permission concerns the read surface doesn't have. The existing `ctxt analyze` HTTP endpoint already handles all of this; pointing agents there is correct. A future ADR can add `ingest` once the agent-write story is settled.

7. **stdio-only (no HTTP server).** Rejected: stdio means a fresh subprocess per client connection, no warm caches, no shared state, and per-client lifecycle. Streamable-HTTP gives all clients the same warm process. Keep stdio as a fallback for clients that don't speak HTTP yet, not the primary.

8. **SSE transport (kept around for older clients).** Rejected: deprecated by MCP spec 2025-03-26. OC kept it as a config flag for migration; we ship streamable-HTTP only. Clients on old MCP SDKs upgrade or use stdio fallback.

---

## Consequences

### Positive

- **Agents gain native, configure-once access to the knowledge graph.** Claude Code, Claude Desktop, Cursor, Codex, opencode, custom agents — all consume the same surface.
- **Two MCP endpoints (dpkms + ctxd) handle remote-dpkms topology cleanly** without reinventing a proxy or doing without local context.
- **Read-only enforcement via type system.** The MCP server imports only read-side service interfaces; mutation methods are not in scope. Hard guarantee, not convention (matches OC `docs/mcp.md` line 433).
- **Bus events make MCP traffic observable and policy-gateable** from day one. Privacy and access controls compose with existing kit/policy infrastructure.
- **Schema-tool (`schema()`) closes ADR-038 US-0037 (agent discovers query schema).** The named-tool surface closes US-0038 enough that RSQL-construction can wait.
- **Install matrix (claude-code/desktop, cursor, codex, opencode, mcp-json, stdio fallback) port-able from OC** without reinvention; first-day DX matches the de-facto standard.
- **In-process mount means warm storage, no extra connections, no sidecar process.** Operationally simplest possible shape.
- **dpkms gains its first agent-facing surface, fulfilling ADR-038's deferred commitment.** Closes the gap explicitly named in ADR-066 §Implementation Notes (line 251) and ADR-067 §References.

### Negative

- **Two servers to version, document, and test.** Mitigated by minimal ctxd-side surface (5 tools); shared transport library between them.
- **Profile-trust posture inherited from existing HTTP/gRPC.** Until ADR-023 auth enforcement lands, callers self-declare profile and the server trusts them. Not new debt; same debt as REST/gRPC.
- **MCP spec is moving target.** Streamable-HTTP just replaced SSE; tool-result media types are still settling; client implementations vary. We track the spec and ship breaking changes in lockstep with major MCP-SDK upgrades.
- **Tool surface design is a real product decision.** The 10-tool initial set above is informed by OC + ADR-038, but actual usage will expose gaps (e.g. "the agent really wants `mentions_of_person()` not `mentions(target=…)`"). Plan to iterate on the tool list based on agent-call telemetry from `*.mcp.tool.invoked` events.
- **Tunnel scenario (ChatGPT-Desktop) is documented-but-unsupported in v1.** Users who want it can DIY with ngrok/cloudflared at their own risk; we don't ship a wrapper. OC documented the same trade-off (`docs/mcp.md` lines 296–351); we follow.
- **`ctxd mcp install` writes user-config files.** Idempotent and reversible (`ctxt mcp uninstall <client>`), but introduces a file-modification surface in user homes. Per-client gotchas (Claude Desktop requires absolute paths, restart; opencode lives in opencode.json not opencode.jsonc) are documented per OC.

### Neutral / Considerations

- **Compose tool semantics.** `compose(template, scope?)` calls into the existing `ctxt compose` runtime. The template registry, scope DSL (`--session sess_X`, `--since 2h ago`), and output format are reused as-is. No new template engine.
- **Vector search vs. FTS vs. hybrid.** `search()` defaults to hybrid (BM25 + vector reranking) per ADR-022; agents can request strict-FTS or strict-vector via parameter if wanted. Default is the most-useful-mode.
- **Session tools are populated only when ADR-067 lands.** v1 of ADR-068 ships `sessions()` and `session()` tool stubs that return `[]` / `null` until session storage is in place. Substrate for future, no migration cost when sessions arrive.
- **Federation and MCP.** Each federated dpkms instance hosts its own MCP server. Cross-instance graph queries are NOT a v1 concern; agents either attach per-instance or ask the federation hub (a federated read replica per ADR-064). This may produce a useful pattern (the hub instance is the agent's universal endpoint), but we're not designing for it.
- **Audit-log surfacing.** The existing `/api/v1/audit-log` endpoint is read-side; we could expose `audit(since?, kind?)` as a tool. Deferred — power-user feature; not first-day.
- **Tool naming.** Singular (`get`, `entity`, `session`) for single-fetch, plural (`list`, `sessions`) for collections. Matches REST norms; reduces agent confusion.

---

## Implementation Notes

### New files

| Path | Responsibility |
|---|---|
| `internal/mcp/server.go` | MCP server registration; mounts at `/api/v1/mcp/` on dpkms HTTP listener |
| `internal/mcp/tools.go` | Tool definitions + dispatch to service layer |
| `internal/mcp/tools/{search,list,get,entity,recent,sessions,session,compose,mentions,schema}.go` | One file per tool |
| `internal/mcp/instructions.go` | Server-level instructions string |
| `internal/mcp/events.go` | Bus topic builders (kit/bus 4-segment) |
| `internal/mcp/auth.go` | Reuse ADR-023 hook; bearer token validation |
| `internal/ambient/mcp/server.go` | ctxd-side MCP server (separate process) |
| `internal/ambient/mcp/tools/{current_session,recent_local,pending_enqueue,sources,health}.go` | ctxd-side tools |
| `cmd/ctxt/cmd/mcp.go` | `ctxt mcp install <client>` / `ctxt mcp uninstall <client>` / `ctxt mcp serve` (stdio fallback) |
| `cmd/ctxd/main.go` | (Already exists per ADR-066) — wires the ctxd MCP server |
| `docs/mcp.md` | User docs: tools, install matrix, transport, auth, troubleshooting (modeled on OC's) |

### Modified files

| Path | Change |
|---|---|
| `internal/server/http/server.go` | Mount MCP handler at `/api/v1/mcp/` (sibling of REST, before SPA fallback) |
| `cmd/dpkms/cmd/serve.go` | Wire MCP server into the existing serve runtime |

### MCP SDK choice

Use the official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk` or current upstream). If not yet stable for streamable-HTTP, fall back to a thin custom implementation of the MCP spec 2025-03-26 — the protocol is small enough (~600 LoC for the wire format).

### Tool implementation pattern

Each tool is a file in `internal/mcp/tools/` exporting:

```go
type SearchTool struct {
    storage storage.StorageDriver
    profile profile.Resolver
    bus     bus.Bus
}

func (t *SearchTool) Name() string                     { return "search" }
func (t *SearchTool) Description() string              { return "..." }
func (t *SearchTool) Schema() mcp.ToolSchema           { /* JSON schema for args */ }
func (t *SearchTool) Invoke(ctx context.Context, args map[string]any) (any, error) {
    // 1. Parse args, resolve profile
    // 2. Emit dpkms.mcp.tool.invoked
    // 3. Call storage / service layer
    // 4. Format response
    // 5. Emit dpkms.mcp.tool.completed
}
```

`Invoke` is the only method that needs unit-testing per tool; the registration is mechanical.

### Install-command port from OpenChronicle

`ctxt mcp install <client>` matrix (matching OC `docs/mcp.md` lines 208–411):

| Client | Config target | Transport |
|---|---|---|
| `claude-code` | `claude mcp add` CLI | streamable-HTTP |
| `claude-desktop` | `~/Library/Application Support/Claude/claude_desktop_config.json` | stdio (Claude Desktop limitation) |
| `cursor` | `~/.cursor/mcp.json` | streamable-HTTP |
| `codex` | `codex mcp add` CLI | streamable-HTTP |
| `opencode` | `~/.config/opencode/opencode.json` | streamable-HTTP |
| `mcp-json` | `./mcp.json` (stdio default; `--http` flag) | stdio or streamable-HTTP |

All idempotent. `ctxt mcp uninstall <client>` reverses.

### Phasing

- **Phase A — dpkms-side server, core tools:** server scaffold + `search`, `list`, `get`, `entity`, `recent`, `schema`, `compose`. Single profile (default). Streamable-HTTP only. ~1 week of focused work.
- **Phase B — install matrix:** `ctxt mcp install` for the six clients above; docs/mcp.md user-facing guide.
- **Phase C — sessions tools (depends on ADR-067 being implemented):** `sessions`, `session` populated against the session store.
- **Phase D — ctxd-side server:** `current_session`, `recent_local`, `pending_enqueue`, `sources`, `health`. Discovery via `~/.local/state/ctxt/ambient/mcp.json`.
- **Phase E — observability + policy:** bus event emission verified; CEL rule examples documented.
- **Phase F (deferred):** RSQL tool, audit tool, write tools, public-bind + auth-token enforcement, ChatGPT-Desktop tunnel docs.

Phases A+B can land on main without ADR-066/067 being done — they only need the existing storage + service layer. Phase C blocks on ADR-067; Phase D blocks on ADR-066 substrate.

### Migration concerns

- **None for existing users.** MCP is opt-in via `ctxt mcp install <client>`. Default install behavior unchanged. No new mandatory daemons or config.
- **No schema migration.** The MCP server reads the existing schema; nothing on disk changes.
- **Plugin API.** Tools live in `internal/mcp/`, not `pkg/pluginapi/`. Plugins do not register MCP tools in v1 — that's a future extension if real demand emerges.

### Testing implications

- **Per-tool unit tests:** mock storage driver, assert tool dispatches the right query and shapes the response correctly.
- **MCP protocol-conformance tests:** spin the server, run `mcp-test` (upstream SDK testing harness) against it.
- **End-to-end integration test:** start dpkms with seed data, run `ctxt mcp install claude-code`, invoke each tool via a stub MCP client, verify responses.
- **ctxd-side integration test:** start ctxd with simulated buffered events + active session, verify `current_session` and `recent_local` return expected shapes.
- **Bus-event tests:** every tool invocation emits `dpkms.mcp.tool.invoked` (or `ctxt.mcp.tool.invoked`) and a corresponding `…completed`/`…failed`.
- **Install-command tests:** for each client, run `install`, assert config file matches expected shape; run `uninstall`, assert clean removal; re-run `install` (idempotency).
- **Auth tests (Phase F):** with `--public` and a token, calls without token are 401; with the token, 200; profile mismatches are 403.

### Backwards compatibility

- Existing HTTP `/api/v1/*`, gRPC services, SSE, WebSocket-bus, CLI: untouched. MCP is purely additive.
- No new required config keys. `mcp.auto_start = true` (default-on for the dpkms server) and `mcp.host`, `mcp.port` (defaults to existing HTTP listener) sit under the existing config namespace.
- ctxd-side server requires ctxd to be running (per ADR-066); no impact on users without ambient capture.

---

## Diagrams

Source-of-truth `.mmd` files live in [`../diagrams/ambient/`](../diagrams/ambient/). See [`../diagrams/README.md`](../diagrams/README.md) for authoring conventions.

### [Two-server topology](../diagrams/ambient/068-topology.mmd)

When dpkms is local, both servers live on the same machine. When dpkms is remote, the local agent attaches to both endpoints; remote agents attach only to dpkms. No proxy, no double-counting.

### [Tool dispatch sequence](../diagrams/ambient/068-tool-dispatch.mmd)

Every tool invocation passes through bus event emission for observability + CEL policy gating.

### [Tool surface (groupings)](../diagrams/ambient/068-tool-surface.mmd)

10 dpkms-side tools (reading, entities & graph, sessions, synthesis) + 5 ctxd-side local-only tools (current_session, recent_local, pending_enqueue, sources, health).

---

## References

- ADR-023 — authentication / authorization model (the auth hook MCP reuses when public-bind is enabled)
- ADR-038 — SuperMemory not adopted (locks in the MCP server commitment, lines 184–188; ADR-068 fulfills it)
- ADR-052 — separate HTTP + gRPC listeners (MCP slots into the HTTP listener; doesn't compete with gRPC)
- ADR-056 — unified enqueue API (the path agents POST to for ingestion; MCP doesn't replicate it)
- ADR-064 — federation (per-instance MCP servers; cross-instance is not a v1 concern)
- ADR-065 — pluggable adapters (rejected as MCP substrate; rationale in §Rejected #1)
- ADR-066 — ambient capture substrate (the buffer + cutter that ctxd-side MCP exposes)
- ADR-067 — sessions / WorkUnit (the session store dpkms-side `sessions`/`session` tools query)
- US-0037 — agent discovers query schema (closed by `schema()` tool)
- US-0038 — agent constructs RSQL query (deferred; named tools cover most cases; RSQL tool is Phase F)
- US-0039–US-0041 — further agent-integration stories (read-side stories closed by Phase A; write-side deferred)
- OpenChronicle prior art:
  - `~/.p/sandbox/OpenChronicle/docs/mcp.md` — tool docs, install matrix, transport choices, ChatGPT-tunnel caveats
  - `~/.p/sandbox/OpenChronicle/src/openchronicle/mcp/server.py` — FastMCP server pattern
  - `~/.p/sandbox/OpenChronicle/src/openchronicle/mcp/captures.py` — raw-layer tools (analog to ctxd-side)
- MCP spec 2025-03-26 (Streamable-HTTP transport)
- tlc track: `ambient-capture` — `tlc track show ambient-capture` (T-0497 carries this ADR)
