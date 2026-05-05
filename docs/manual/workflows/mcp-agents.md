# Workflow: Agent-Native MCP Read-Surface

## Goal

Expose the ctxt knowledge graph and live local state to AI agents (Claude Code, Claude Desktop, Cursor, Codex, opencode, custom MCP clients) as a Model Context Protocol surface. After install, the user can ask their agent things like "what was I doing this afternoon?", "summarize yesterday's meeting on Q3", "who is Alice in this project?" and get answers grounded in their personal knowledge graph.

## Scope

- Installing the MCP servers in the user's preferred client
- The 10 dpkms-side tools (authoritative, full graph) and 5 ctxd-side tools (local-only, live state)
- Read-only by design (writes still go through `ctxt analyze`)
- Profile resolution per tool call
- Local vs. remote dpkms behavior
- Streamable-HTTP transport, stdio fallback

## Primary stories

- `US-0219` (MCP agent integration)
- `US-0220` (local MCP during network loss)
- `US-0037`–`US-0041` (agent integration: schema discovery, RSQL construction — `schema()` tool closes US-0037)

## Prerequisites

1. dpkms running with the MCP server enabled (default: `mcp.auto_start = true`).
2. `ctxd` running for the local-side MCP (only matters if you're on the same machine as where you're running the agent and want live ambient state).
3. Your agent client installed and updated to a version that supports streamable-HTTP MCP (claude-code recent, claude-desktop recent, cursor recent, codex recent, opencode recent).

## Install matrix

The simplest path — `ctxt mcp install` writes the right config for your client. Idempotent (safe to re-run); reversible via `ctxt mcp uninstall`.

```bash
# Claude Code (CLI)
ctxt mcp install claude-code

# Claude Desktop (macOS .app)
ctxt mcp install claude-desktop

# Cursor
ctxt mcp install cursor

# Codex CLI
ctxt mcp install codex

# opencode
ctxt mcp install opencode

# Any framework that consumes mcpServers JSON (Cline, Continue, Zed, custom)
ctxt mcp install mcp-json --output ./mcp.json
```

Each install registers **two endpoints**:

- `ctxt-graph` — dpkms-side, authoritative; 10 tools
- `ctxt-local` — ctxd-side, local-only; 5 tools (skipped if `ctxd` not detected)

## Procedure

### Step 1: Verify the install

```bash
ctxt mcp status
```

Sample output:

```
dpkms MCP:  http://127.0.0.1:8080/api/v1/mcp/  (running, 10 tools)
ctxd MCP:   http://127.0.0.1:8744/mcp           (running, 5 tools)

Installed clients:
  claude-code    OK  endpoints: ctxt-graph, ctxt-local
  cursor         OK  endpoints: ctxt-graph, ctxt-local
```

Restart your agent client to pick up the new MCP servers.

### Step 2: Tool reference (dpkms-side)

All 10 tools are read-only. They wrap existing service-layer queries (FTS5 + vector + entity resolver + RSQL) — no new query engine. Profile defaults to the configured default; pass `profile?` per call to override.

| Tool | Args | Returns | When the agent should use it |
|---|---|---|---|
| `search` | query, top_k=10, since?, until?, profile?, kinds? | hybrid FTS+vector results | Specific keywords (person, project, error msg, file path) |
| `list` | kind, filter?, limit=20, profile? | rows of `kind` matching filter | Browsing by type (objects, entities, watches, etc.) |
| `get` | id, profile? | one KnowledgeObject | After search/list points at it |
| `entity` | slug, profile? | entity + facts + backlinks | Resolving `@person.alice` or `@project.q3` |
| `recent` | since='today', limit=20, kind?, profile? | newest-first feed | "What's new / what has the user been up to?" |
| `sessions` | since?, until?, limit=20, profile? | sessions list with metadata | "Show recent work sessions" |
| `session` | id, profile? | session + every KO captured during it | After sessions points at one |
| `compose` | template, scope?, profile? | rendered document | Synthesis: "summarize my last X" |
| `mentions` | target, depth=1, profile? | objects referencing target | Graph traversal from an entity or object |
| `schema` | (no args) | storage taxonomy: kinds, edge types, etc. | Agent constructing structured queries |

### Step 3: Tool reference (ctxd-side, local-only)

These surface state dpkms cannot see when remote.

| Tool | Args | Returns | When the agent should use it |
|---|---|---|---|
| `current_session` | (no args) | active session metadata, or `null` | "What am I working on right now?" |
| `recent_local` | limit=20, source? | most recent ambient events (incl. buffered) | "What did I just capture?" — beats `recent` for last-30-seconds questions |
| `pending_enqueue` | (no args) | events captured locally but not yet at dpkms | "Is dpkms reachable? What's stuck?" |
| `sources` | (no args) | active ambient sources + last-event timestamps | Diagnostics |
| `health` | (no args) | daemon status, buffer size, retention state | Diagnostics |

### Step 4: Server-level instructions (what agents see)

Both servers pass an `instructions` string to the MCP client at connect-time. Excerpt from the dpkms-side server:

> ctxt is the user's personal knowledge graph — captures, decisions, mentions, sessions, projects, people, recent activity. **CALL THESE TOOLS FIRST** whenever the user asks about themselves, their work, their recent context: *"what was I doing this afternoon?" / "what did I decide about X?" / "summarize my last session on Y" / "who is Alice?"* — prefer this graph over replying "I don't know."

The ctxd-side server's instructions emphasize that it surfaces *live, not-yet-enqueued* state and complements the authoritative dpkms-side surface.

### Step 5: Profile scoping

Pass `profile?` per tool call:

```
search(query="auth bug", profile="work")
session(id="sess_a1b2c3d4e5f6", profile="research")
```

Without `profile?`, the daemon's configured default (`profile.default` viper key) applies. Profile mismatches return empty results, not errors (no information leakage about which profiles exist).

### Step 6: Bus events for observability

Every tool invocation emits:

```
dpkms.mcp.tool.invoked       (with tool name + sanitized args)
dpkms.mcp.tool.completed     (with duration + result count)
dpkms.mcp.tool.failed        (with error class)
```

Or `ctxt.mcp.*` for the ctxd-side server. kit/runtime/policy CEL rules can subscribe to gate sensitive tools — e.g. "agents in profile=work cannot call `mentions(target='alice@home')`".

### Step 7: Public bind + auth (advanced, deferred)

Default: `127.0.0.1` only. To expose to LAN or via a tunnel:

```yaml
mcp:
  host: 0.0.0.0
  auth_token_env: CTXT_MCP_TOKEN
```

Then clients pass `Authorization: Bearer $CTXT_MCP_TOKEN`. **ChatGPT-Desktop tunnel scenarios** (per OpenChronicle's docs) are out of scope for v1 — document but don't ship a tunnel command.

## Common patterns

### "Agent: what was I working on at 2pm today?"

Agent flow:
1. Call `current_session()` — if active and started before 2pm, return its metadata + first 5 items
2. Else call `sessions(since="today")`, find the one whose time-range covers 2pm
3. Call `session(id=...)` to get the items
4. Synthesize natural-language answer from session metadata + KO contents

### "Agent: summarize yesterday's Q3 planning meeting"

```
search(query="Q3 planning", kinds=["meeting"], since="yesterday")
→ get(id=<top result>)
→ compose(template="meeting-recap", scope="object:<id>")
```

### "Agent: anything captured locally that hasn't reached dpkms?"

```
pending_enqueue()
→ if non-empty, surface to user with reason (network down? policy hold?)
```

This uses the **ctxd-side** MCP — agents on remote-dpkms deployments only see this when running on the user's machine.

## Outputs to validate

- Both endpoints reachable: `curl -X POST http://127.0.0.1:8080/api/v1/mcp/ -d '{"method":"tools/list"}'`
- Tool schemas valid (use the [MCP Inspector](https://github.com/modelcontextprotocol/inspector) for interactive testing)
- Bus events fire for every tool call (`ctxt watch --topic 'dpkms.mcp.*'`)
- Profile scoping respected (queries with `profile=work` don't leak personal-profile data)

## Common failure modes

### "Agent client can't connect"

- Streamable-HTTP transport requires a recent client SDK; check version
- Use stdio fallback: `ctxt mcp install claude-code --transport stdio`
- Check `mcp.host` binding (default localhost; agent must run on same host)

### "Tools list is empty"

- Agent client may need a manual refresh after `ctxt mcp install`
- Restart the client (Claude Desktop requires Cmd+Q + relaunch)

### "ctxd MCP returns 'no active session'"

- Sessions only exist when `ctxd` is running and a foreground signal is active
- One-shot `ctxt analyze` commands don't create sessions

### "Agent loops calling search() with the same query"

- It's hitting profile mismatch and getting empty results
- Pass `profile?` explicitly; or fix the default

## Privacy posture

- Read-only by design. No tool can mutate. Agents that want to ingest go through the existing `ctxt analyze` enqueue path (separate concern).
- All tool calls bus-emit. CEL rules can audit, gate, or veto.
- Default `127.0.0.1` bind. Public exposure requires explicit `--public` + token.
- `ctxd`-side server lives on the user's machine; never surfaces dpkms state directly (no proxy).

## Related references

- [`ambient-capture.md`](ambient-capture.md) — produces the events agents query
- [`sessions.md`](sessions.md) — work-unit grouping that `sessions`/`session`/`current_session` tools query
- [`meeting-capture.md`](meeting-capture.md) — meetings show up in `search` and `recent`
- [`../../decisions/ADR-068-mcp-read-surface.md`](../../decisions/ADR-068-mcp-read-surface.md)
- [`../../decisions/ADR-038-supermemory-not-adopted.md`](../../decisions/ADR-038-supermemory-not-adopted.md) — the original commitment to ship `ingest()`/`search()`/`compose()` MCP tools (closed by ADR-068)
- [`../../diagrams/ambient/068-topology.mmd`](../../diagrams/ambient/068-topology.mmd)
- [`../../diagrams/ambient/068-tool-dispatch.mmd`](../../diagrams/ambient/068-tool-dispatch.mmd)
- [`../../diagrams/ambient/068-tool-surface.mmd`](../../diagrams/ambient/068-tool-surface.mmd)
- [MCP Specification](https://modelcontextprotocol.io)
