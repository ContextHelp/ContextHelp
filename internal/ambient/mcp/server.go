// Package mcp is the ctxd-side local MCP read-surface (per ADR-068).
//
// Distinct from the dpkms-side MCP server (internal/mcp/) which is the
// authoritative graph-wide read surface. This server runs INSIDE ctxd
// (the local ambient daemon) and exposes 5 tools that surface live
// local state dpkms cannot see when remote:
//
//	current_session()  — active session metadata, or null
//	recent_local()     — recent ambient events from the local buffer
//	pending_enqueue()  — buffered events awaiting dpkms (network-loss state)
//	sources()          — registered ambient source names + last-event ts
//	health()           — daemon status, buffer size, dpkms reachability
//
// Why two MCP servers (per ADR-068 §Decision item 1): when dpkms is
// remote, the user's machine has state dpkms can't see (in-flight cutter
// session, locally-buffered events, source health). Agents on the user's
// machine attach to BOTH servers and merge results client-side; agents
// elsewhere attach only to dpkms.
//
// Reuses the wire-shape primitives (JSON-RPC dispatch, Tool interface,
// JSONRPCRequest/Response) from internal/mcp/. This package wraps that
// dispatcher with ctxd-specific tool implementations.
package mcp

import (
	"context"
	"errors"
	"net/http"

	dpkmsmcp "github.com/ideacrafterslabs/ctxt/internal/mcp"
)

// Default port per ADR-068 §Decision item 2.
const DefaultPort = 8744

// LocalSnapshot is what ctxd's runner / cutter / buffer expose to the
// MCP tools at read time. The daemon owner (cmd/ctxd) constructs one
// at startup and updates it as state changes.
type LocalSnapshot struct {
	// CurrentSession returns the active session metadata, or nil.
	CurrentSession func(ctx context.Context) (any, error)
	// RecentLocal returns the most-recent N ambient events.
	RecentLocal func(ctx context.Context, limit int, source string) ([]any, error)
	// PendingEnqueue returns events buffered awaiting dpkms.
	PendingEnqueue func(ctx context.Context) (any, error)
	// Sources returns registered source names + last-event timestamps.
	Sources func(ctx context.Context) ([]any, error)
	// Health returns daemon status: uptime, buffer size, dpkms endpoint
	// + last-success / last-failure timestamps.
	Health func(ctx context.Context) (any, error)
}

// Server is the ctxd-side MCP server. Wraps the dpkms-side dispatcher
// (internal/mcp.Server) and registers the 5 ctxd-specific tools.
type Server struct {
	inner *dpkmsmcp.Server
}

// New constructs a ctxd-side server with the supplied LocalSnapshot
// providing the tools' data sources.
//
// Uses dpkmsmcp.NewBare() to start with no tools so the dpkms-side
// defaults (schema, search) don't accidentally appear here. The ctxd
// server is local-only and does NOT proxy to dpkms (per ADR-068
// §Decision item 1: each server owns distinct data; agents merge
// results client-side).
func New(snap LocalSnapshot) *Server {
	inner := dpkmsmcp.NewBare()
	inner.Register(&currentSessionTool{snap: &snap})
	inner.Register(&recentLocalTool{snap: &snap})
	inner.Register(&pendingEnqueueTool{snap: &snap})
	inner.Register(&sourcesTool{snap: &snap})
	inner.Register(&healthTool{snap: &snap})
	return &Server{inner: inner}
}

// Handler returns the HTTP handler for the ctxd-side MCP endpoint.
// Mount on a separate listener (default :8744) per ADR-068.
func (s *Server) Handler() http.Handler { return s.inner.Handler() }

// ---------------------------------------------------------------------------
// 5 ctxd-side tools
// ---------------------------------------------------------------------------

type currentSessionTool struct{ snap *LocalSnapshot }

func (t *currentSessionTool) Name() string { return "current_session" }
func (t *currentSessionTool) Description() string {
	return "The active work session if any: id, started_at, app_mix-so-far, event_count-so-far. Returns null when no session is open. First-hop tool for 'what is the user doing right now' questions; complements dpkms-side sessions() which lists historical sessions."
}
func (t *currentSessionTool) InputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *currentSessionTool) Invoke(ctx context.Context, _ map[string]any) (any, error) {
	if t.snap == nil || t.snap.CurrentSession == nil {
		return nil, errors.New("current_session: snapshot not configured")
	}
	return t.snap.CurrentSession(ctx)
}

type recentLocalTool struct{ snap *LocalSnapshot }

func (t *recentLocalTool) Name() string { return "recent_local" }
func (t *recentLocalTool) Description() string {
	return "Most recent ambient events captured on this machine, including events buffered awaiting enqueue (dpkms unreachable). PREFER over dpkms-side recent() for last-30-seconds questions; the dpkms feed has post-pipeline enrichment latency."
}
func (t *recentLocalTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit":  map[string]any{"type": "integer", "default": 20, "minimum": 1, "maximum": 200},
			"source": map[string]any{"type": "string", "description": "Filter by ambient source name (e.g. 'clipboard', 'meeting')"},
		},
	}
}
func (t *recentLocalTool) Invoke(ctx context.Context, args map[string]any) (any, error) {
	limit := 20
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit = int(n)
		case int:
			limit = n
		}
	}
	source, _ := args["source"].(string)
	if t.snap == nil || t.snap.RecentLocal == nil {
		return nil, errors.New("recent_local: snapshot not configured")
	}
	return t.snap.RecentLocal(ctx, limit, source)
}

type pendingEnqueueTool struct{ snap *LocalSnapshot }

func (t *pendingEnqueueTool) Name() string { return "pending_enqueue" }
func (t *pendingEnqueueTool) Description() string {
	return "Events captured locally but NOT YET enqueued to dpkms. Surfaces dpkms-down state: count + age of oldest pending event + reachability status. Useful for 'why isn't my recent capture showing up in search?' UX."
}
func (t *pendingEnqueueTool) InputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *pendingEnqueueTool) Invoke(ctx context.Context, _ map[string]any) (any, error) {
	if t.snap == nil || t.snap.PendingEnqueue == nil {
		return nil, errors.New("pending_enqueue: snapshot not configured")
	}
	return t.snap.PendingEnqueue(ctx)
}

type sourcesTool struct{ snap *LocalSnapshot }

func (t *sourcesTool) Name() string { return "sources" }
func (t *sourcesTool) Description() string {
	return "List of ambient sources active on this machine and their last-event timestamps. Diagnostic — useful for 'is the foreground source actually running?'"
}
func (t *sourcesTool) InputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *sourcesTool) Invoke(ctx context.Context, _ map[string]any) (any, error) {
	if t.snap == nil || t.snap.Sources == nil {
		return nil, errors.New("sources: snapshot not configured")
	}
	return t.snap.Sources(ctx)
}

type healthTool struct{ snap *LocalSnapshot }

func (t *healthTool) Name() string { return "health" }
func (t *healthTool) Description() string {
	return "Daemon status: uptime, buffer size + percentage of cap, dpkms endpoint + last-success timestamp + last-failure if any, ctxd version. Diagnostic."
}
func (t *healthTool) InputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *healthTool) Invoke(ctx context.Context, _ map[string]any) (any, error) {
	if t.snap == nil || t.snap.Health == nil {
		return nil, errors.New("health: snapshot not configured")
	}
	return t.snap.Health(ctx)
}
