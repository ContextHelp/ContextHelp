---
status: paper
adr: ADR-068, ADR-066
task: T-0497-impl
---

# US-0220: Local MCP During Network Loss (ctxd-side)

**System Types:** ctxt
**Personas:** [AI Agents](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## User Goal

As an AI agent on the user's machine, I want to query live local state (active session, recently-captured-but-not-yet-enqueued events, daemon health) even when remote dpkms is unreachable — so the user gets useful answers during network outages, while traveling, or when the household NAS hosting dpkms is offline for maintenance.

---

## Context

[ADR-068](../../decisions/ADR-068-mcp-read-surface.md) defines **two** MCP servers, not one. The dpkms-side server ([US-0219](US-0219-mcp-agent-integration.md)) is authoritative but unreachable when dpkms is remote and the network is down. The ctxd-side local server fills the gap with 5 tools that surface state dpkms cannot see when remote.

This is necessitated by the constraint from [ADR-066](../../decisions/ADR-066-ambient-capture-substrate.md): **dpkms is often deployed remote**. Local-machine state — buffered events awaiting enqueue, the live session in the cutter, fingerprint dedup state — lives in `ctxd` because it cannot live anywhere else.

The two servers compose: agents on the user's machine attach to **both**. dpkms-side is asked first for durable knowledge; ctxd-side answers questions about the right-now, in-flight state. No proxy, no double-counting (each server owns distinct data).

---

## Acceptance Criteria

### Server

- [ ] MCP server mounted in `ctxd` at `http://127.0.0.1:8744/mcp` (port configurable via `ambient.mcp.port`)
- [ ] Streamable-HTTP transport per MCP spec 2025-03-26
- [ ] stdio fallback: `ctxd mcp serve` spins a fresh stdio server reading the same buffer (file lock allows concurrent readers)
- [ ] Server starts when `ctxd` starts; stops cleanly on shutdown
- [ ] `127.0.0.1` bind only (no LAN exposure by default)

### 5 read-only tools

| Tool | Args | Returns |
|---|---|---|
| `current_session` | (no args) | active session metadata, or `null` |
| `recent_local` | limit=20, source? | most recent ambient events (incl. buffered) |
| `pending_enqueue` | (no args) | events captured locally but not yet at dpkms |
| `sources` | (no args) | active ambient sources + last-event timestamps |
| `health` | (no args) | daemon status, buffer size, retention state |

- [ ] `current_session()` returns the cutter's active session payload (id, started_at, app_mix-so-far, event_count-so-far) or null when no session is open
- [ ] `recent_local(limit, source?)` reads from the local buffer; **does not proxy to dpkms** (avoids double-counting with dpkms-side `recent()`)
- [ ] `pending_enqueue()` surfaces buffer count + age of oldest pending event + dpkms-reachability status
- [ ] `sources()` lists registered ambient sources, their state, last-event timestamp per source
- [ ] `health()` returns: daemon uptime, buffer size + percentage of cap, dpkms endpoint + last-success timestamp + last-failure if any, ctxd version

### Discovery

- [ ] `ctxd` advertises its MCP endpoint via `~/.local/state/ctxt/ambient/mcp.json` on start
- [ ] File contents: `{"endpoint": "http://127.0.0.1:8744/mcp", "version": "...", "started_at": "..."}`
- [ ] File deleted on clean shutdown

### Install matrix

- [ ] `ctxt mcp install <client>` registers **both** endpoints per [US-0219](US-0219-mcp-agent-integration.md): `ctxt-graph` (dpkms) AND `ctxt-local` (ctxd)
- [ ] `ctxt-local` skipped if discovery file absent (ctxd not running) — install command warns but continues
- [ ] Re-run after starting ctxd registers `ctxt-local` retroactively

### Server-level instructions

- [ ] `instructions` string emphasizes: this server surfaces *live, not-yet-enqueued* state, complementing the authoritative dpkms-side surface

### Bus events

- [ ] `ctxt.mcp.tool.invoked` per tool call (note: `ctxt.*` namespace, not `dpkms.*`, since this is the local server)
- [ ] `ctxt.mcp.tool.completed` / `…failed`
- [ ] `ctxt.mcp.session.opened` / `…closed` for client connections

### Read-only enforcement

- [ ] No tool mutates. Local state changes happen via the substrate (sources emit events; cutter cuts; runner enqueues). MCP is read-only by hard guarantee.

### Failure modes

- [ ] dpkms-down: `pending_enqueue()` accurately reports backlog
- [ ] Buffer at cap: `health()` reports under-pressure state; `pending_enqueue()` shows growing oldest-age
- [ ] Source crashed: `sources()` reports the source as `failed` with last-event timestamp
- [ ] Daemon restarts: discovery file recreated; clients reconnect (MCP handles reconnection)

---

## Implementation Notes

> See [ADR-068 §Implementation Notes](../../decisions/ADR-068-mcp-read-surface.md) for the full file structure.

### Architecture

```
internal/ambient/mcp/
├── server.go              — ctxd-side MCP server (separate from dpkms's)
├── tools/
│   ├── current_session.go
│   ├── recent_local.go
│   ├── pending_enqueue.go
│   ├── sources.go
│   └── health.go
└── discovery.go           — write/delete ~/.local/state/ctxt/ambient/mcp.json
```

The ctxd-side server **does not proxy** to dpkms. Each server owns distinct data; agents merge results client-side if needed.

### Tool implementations

Each tool reads in-memory state from `ctxd`:

```go
func (t *CurrentSessionTool) Invoke(ctx context.Context, args map[string]any) (any, error) {
    snap := t.cutter.Snapshot()                           // atomic read of cutter state
    if snap == nil { return nil, nil }
    return map[string]any{
        "id":         snap.ID,
        "started_at": snap.StartedAt,
        "app_mix":    snap.AppMix,
        "event_count": snap.EventCount,
    }, nil
}
```

### Read-only enforcement

`internal/ambient/mcp/server.go` imports only read-side accessors from runner / cutter / buffer. Mutation methods are not in scope (compile-time guarantee).

---

## E2E Checklist

- [ ] Start ctxd (no dpkms running); verify ctxd MCP starts on :8744
- [ ] `ctxt mcp install claude-code`; verify both endpoints registered
- [ ] Restart Claude Code; verify both servers in tool list
- [ ] Invoke `current_session()` while ctxd has no active session; verify null
- [ ] Trigger an ambient capture (copy something); verify session opens; `current_session()` returns metadata
- [ ] Take dpkms offline; capture 10 events locally; invoke `pending_enqueue()`; verify count=10 + dpkms-down state
- [ ] Bring dpkms back; verify replay; `pending_enqueue()` returns 0
- [ ] Invoke `recent_local(limit=5)`; verify last 5 captures (including buffered ones not yet at dpkms)
- [ ] Invoke `sources()`; verify all enabled sources listed with last-event timestamps
- [ ] Invoke `health()`; verify daemon uptime, buffer size, dpkms endpoint
- [ ] Crash a source (simulated); verify `sources()` reports it as `failed`
- [ ] Restart ctxd; verify discovery file recreated; client reconnects
- [ ] Verify `ctxt-local` endpoint absent in client config when ctxd is not running at install time
- [ ] All bus events fire (note `ctxt.*` namespace)

---

## Related Stories

- [US-0219](US-0219-mcp-agent-integration.md) — dpkms-side authoritative MCP server (sibling)
- [US-0216](US-0216-work-sessions.md) — Sessions whose live state `current_session()` reads
- [US-0211](US-0211-passive-clipboard-watcher.md), [US-0213](US-0213-file-watch-source.md), [US-0214](US-0214-browser-history-source.md), [US-0215](US-0215-foreground-window-source.md) — Sources whose state `sources()` reads

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-068 ctxd-side surface — see `tlc track show ambient-capture`, task **T-0497**.

---

## E2E Tests

- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_ServerStartsWithCtxd`
- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_DiscoveryFile`
- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_CurrentSession`
- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_RecentLocal`
- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_PendingEnqueueDuringNetworkLoss`
- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_Sources`
- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_Health`
- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_NoProxyToDpkms`
- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_SourceCrashedReported`
- planned: `test/integration/us0220_local_mcp_test.go::TestLocalMCP_BothServersRegisteredOnInstall`
