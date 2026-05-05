---
status: paper
adr: ADR-068
task: T-0497-impl
---

# US-0219: MCP Agent Integration (dpkms-side)

**System Types:** dpkms, ctxt
**Personas:** [AI Agents](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an AI agent (Claude Code, Claude Desktop, Cursor, Codex, opencode, custom MCP client), I want to query the user's ctxt knowledge graph natively via MCP — so when the user asks "what was I working on this afternoon?" or "summarize yesterday's Q3 meeting", I can call `search()`, `recent()`, `session()`, `compose()` directly instead of asking the user to shell out and paste results.

---

## Context

Per [ADR-038](../../decisions/ADR-038-supermemory-not-adopted.md), ctxt committed to an MCP server exposing `ingest()`, `search()`, and `compose()` tools when agent integration ships (US-0037 through US-0041). [ADR-068](../../decisions/ADR-068-mcp-read-surface.md) now delivers the **read surface** (writes still go through `ctxt analyze`; an MCP `ingest()` tool is deferred).

The substantive work — FTS, vector search, entity resolution, profile scoping, RSQL — is **already done** in dpkms. ADR-068 is mostly about which tools to expose, where to mount them (in-process on the existing dpkms HTTP listener at `/api/v1/mcp/`), and how profile/auth contexts thread through.

This story covers the **dpkms-side** authoritative MCP server (10 tools spanning the full graph). The **ctxd-side** local MCP server (5 tools surfacing live local state) is covered in [US-0220](US-0220-local-mcp-during-network-loss.md) — they compose: agents on the user's machine attach to both.

This story closes US-0037 (agent discovers query schema) via the `schema()` tool. It does NOT cover RSQL construction (US-0038) or write tools — those stay deferred per ADR-068's phasing.

---

## Acceptance Criteria

### Server

- [ ] MCP server mounted at `http://<dpkms-host>/api/v1/mcp/` as a sibling of REST routes (in-process; reuses storage drivers, FTS, vector, RSQL, entity resolver)
- [ ] Streamable-HTTP transport per MCP spec 2025-03-26 (NOT SSE; deprecated)
- [ ] stdio fallback: `ctxt mcp serve` spins a fresh stdio server reading the same SQLite (WAL allows concurrent readers)
- [ ] `mcp.auto_start = true` default; configurable to `false` to disable

### 10 read-only tools

All wrap existing service-layer queries. Profile defaults to `profile.default`; per-call `profile?` overrides.

| Tool | Args | Returns |
|---|---|---|
| `search` | query, top_k=10, since?, until?, profile?, kinds? | hybrid FTS+vector results |
| `list` | kind, filter?, limit=20, profile? | rows of `kind` |
| `get` | id, profile? | one KnowledgeObject |
| `entity` | slug, profile? | entity + facts + backlinks |
| `recent` | since='today', limit=20, kind?, profile? | newest-first feed |
| `sessions` | since?, until?, limit=20, profile? | session list |
| `session` | id, profile? | session + items |
| `compose` | template, scope?, profile? | rendered document |
| `mentions` | target, depth=1, profile? | objects referencing target |
| `schema` | (no args) | storage taxonomy |

- [ ] Each tool's JSON schema validates via MCP Inspector
- [ ] `compose` calls into the existing `ctxt compose` runtime (no new template engine)
- [ ] `sessions` / `session` return empty / null until ADR-067 sessions storage ships; substrate ready

### Server-level instructions

- [ ] `instructions` string passed to MCP client at connect-time emphasizes: **CALL THESE TOOLS FIRST** when user asks about themselves, their work, their recent context

### Install matrix (`ctxt mcp install <client>`)

- [ ] `claude-code`: `claude mcp add --transport http -s user ctxt-graph http://127.0.0.1:8080/api/v1/mcp/`
- [ ] `claude-desktop`: writes to `~/Library/Application Support/Claude/claude_desktop_config.json` (stdio entry; Claude Desktop limitation)
- [ ] `cursor`: writes to `~/.cursor/mcp.json` (URL entry)
- [ ] `codex`: `codex mcp add ctxt-graph --url ...`
- [ ] `opencode`: writes to `~/.config/opencode/opencode.json` (top-level `mcp` key)
- [ ] `mcp-json`: writes `./mcp.json` for any framework consuming it (Cline, Continue, Zed, custom); `--http` flag for URL entry, default stdio
- [ ] All idempotent (re-run replaces with current URL; no duplicates)
- [ ] `ctxt mcp uninstall <client>` reverses; missing entry treated as success
- [ ] `ctxt mcp status` reports endpoint URL, running status, installed clients

### Auth + profile

- [ ] Default `127.0.0.1` bind (no auth)
- [ ] `mcp.host = "0.0.0.0"` requires `mcp.auth_token_env` set; tool calls without `Authorization: Bearer` return 401
- [ ] Per-call `profile?` argument; falls back to `profile.default`
- [ ] Profile mismatches return empty results, not errors (no information leakage)

### Bus events

- [ ] `dpkms.mcp.tool.invoked` per tool call (with sanitized args)
- [ ] `dpkms.mcp.tool.completed` (with duration + result count)
- [ ] `dpkms.mcp.tool.failed` (with error class)
- [ ] `dpkms.mcp.session.opened` / `…closed` for client connect/disconnect
- [ ] kit/policy CEL rules can subscribe to gate sensitive tools

### MCP protocol conformance

- [ ] Passes the upstream `mcp-test` harness against the official Go SDK
- [ ] Tool result media types valid; pagination works for tools that return many rows

### Read-only enforcement

- [ ] No tool mutates. Hard guarantee at compile time — server imports only read-side service interfaces; mutation methods are not in scope (matches OpenChronicle's discipline per `~/.p/sandbox/OpenChronicle/docs/mcp.md` line 433)

---

## Implementation Notes

> See [ADR-068 §Implementation Notes](../../decisions/ADR-068-mcp-read-surface.md) for the full file structure.
>
> Diagrams: [`068-topology.mmd`](../../diagrams/ambient/068-topology.mmd), [`068-tool-dispatch.mmd`](../../diagrams/ambient/068-tool-dispatch.mmd), [`068-tool-surface.mmd`](../../diagrams/ambient/068-tool-surface.mmd).

### Tool implementation pattern

```go
type SearchTool struct {
    storage storage.StorageDriver
    profile profile.Resolver
    bus     bus.Bus
}

func (t *SearchTool) Name() string             { return "search" }
func (t *SearchTool) Description() string      { return "..." }
func (t *SearchTool) Schema() mcp.ToolSchema   { /* JSON schema */ }
func (t *SearchTool) Invoke(ctx context.Context, args map[string]any) (any, error) {
    // 1. Parse args, resolve profile
    // 2. Emit dpkms.mcp.tool.invoked
    // 3. Call storage / service layer
    // 4. Format response
    // 5. Emit dpkms.mcp.tool.completed
}
```

### Phasing

- Phase A: server scaffold + `search`, `list`, `get`, `entity`, `recent`, `schema`, `compose`. Single profile (default). Streamable-HTTP only.
- Phase B: install matrix for the 6 clients above; user-facing docs.
- Phase C: `sessions`, `session` populated against the session store (depends on US-0216 / ADR-067).

### MCP SDK choice

Use the official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk` or current upstream). If not yet stable for streamable-HTTP, fall back to a thin custom implementation of the MCP spec 2025-03-26 (~600 LoC).

---

## E2E Checklist

- [ ] Start dpkms; verify `/api/v1/mcp/` reachable
- [ ] Run `ctxt mcp install claude-code`; restart Claude Code; verify ctxt-graph appears in tool list
- [ ] Repeat for cursor, codex, opencode (with their respective install paths)
- [ ] Invoke `search(query="test")` from each client; verify results match `ctxt find "test"`
- [ ] Invoke `schema()`; verify storage taxonomy returned
- [ ] Invoke `entity("project.q3")`; verify entity + backlinks
- [ ] Invoke `recent(since="today")`; verify newest-first feed
- [ ] Invoke `sessions(since="today")`; verify empty (until US-0216 lands) or list (after)
- [ ] Invoke `compose(template="meeting-recap", scope="object:obj_xyz")`; verify rendered document
- [ ] Invoke `mentions(target="@person.alice")`; verify graph traversal
- [ ] kit/policy CEL veto: rule on `dpkms.mcp.tool.invoked` for tool=`mentions`; verify error returned to client
- [ ] Public bind: `mcp.host=0.0.0.0` + token; verify 401 without bearer; 200 with
- [ ] Profile scoping: pass `profile="other"` not in config; verify empty results (no leakage)
- [ ] Idempotency: re-run `ctxt mcp install claude-code`; verify no duplicate entries
- [ ] `ctxt mcp uninstall claude-code`; verify removed from config
- [ ] All bus events fire per spec

---

## Related Stories

- [US-0037](../agents/US-0037-agent-discovers-query-schema.md) — Agent discovers query schema (closed by `schema()` tool)
- [US-0038](../agents/US-0038-agent-constructs-rsql-query.md) — Agent constructs RSQL query (deferred; named tools cover most cases)
- [US-0039](../agents/US-0039-agent-ingests-content-and-waits.md) — Agent ingests content (still uses `ctxt analyze`; not MCP)
- [US-0220](US-0220-local-mcp-during-network-loss.md) — Local MCP (sibling; ctxd-side server)
- [US-0216](US-0216-work-sessions.md) — Sessions populate `sessions()`/`session()` tools

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-068 dpkms-side surface — see `tlc track show ambient-capture`, task **T-0497** (ADR) and substrate work in Phase 5.

---

## E2E Tests

- planned: `test/integration/us0219_mcp_test.go::TestMCP_ServerStartup`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_StreamableHTTPTransport`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_StdioFallback`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_AllToolsListed`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_SearchTool`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_SchemaTool`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_EntityTool`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_ComposeTool`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_SessionsTool`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_ProfileScoping`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_PublicBindRequiresAuth`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_BusEventsEmitted`
- planned: `test/integration/us0219_mcp_test.go::TestMCP_CELVeto`
- planned: `test/integration/us0219_mcp_install_test.go::TestMCPInstall_ClaudeCode`
- planned: `test/integration/us0219_mcp_install_test.go::TestMCPInstall_Cursor`
- planned: `test/integration/us0219_mcp_install_test.go::TestMCPInstall_Idempotent`
- planned: `test/integration/us0219_mcp_install_test.go::TestMCPUninstall`
