package integration

// US-0220: Local MCP During Network Loss (ctxd-side server).
//
// E2E tests for the ctxd-side MCP read-surface (per ADR-068 + T-0533).
// Verifies the 5 local-only tools (current_session, recent_local,
// pending_enqueue, sources, health) work end-to-end via xrr cassettes
// — happy paths AND a failure path where the local daemon snapshot
// returns an error.
//
// Cassettes live at testdata/cassettes/us0220_*. Re-record:
//
//	XRR_MODE=record go test -count=1 ./test/integration/ -run TestLocalMCP
//
// Replays without a network or running ctxd; CI mode default.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ambientmcp "github.com/ideacrafterslabs/ctxt/internal/ambient/mcp"
	"github.com/ideacrafterslabs/ctxt/internal/mcp"
)

// fakeSnapshot returns deterministic local-state for cassette recording.
// "force-error" inputs produce errors so the failure path is exercised.
func fakeSnapshot() ambientmcp.LocalSnapshot {
	return ambientmcp.LocalSnapshot{
		CurrentSession: func(_ context.Context) (any, error) {
			return map[string]any{
				"id":          "sess_test_aabbccddeeff",
				"started_at":  "2026-05-06T10:30:00Z",
				"event_count": 23,
				"app_mix": map[string]float64{
					"us.zoom.xos":               0.65,
					"com.apple.Safari":          0.25,
					"com.tinyspeck.slackmacgap": 0.10,
				},
			}, nil
		},
		RecentLocal: func(_ context.Context, limit int, source string) ([]any, error) {
			if source == "force-error" {
				return nil, errors.New("simulated local-buffer read failure")
			}
			out := make([]any, 0, limit)
			for i := 0; i < limit && i < 3; i++ {
				out = append(out, map[string]any{
					"source":      "clipboard",
					"occurred_at": time.Date(2026, 5, 6, 10, 30+i, 0, 0, time.UTC).Format(time.RFC3339),
					"kind":        "url",
					"fingerprint": "fp_" + string(rune('a'+i)),
				})
			}
			return out, nil
		},
		PendingEnqueue: func(_ context.Context) (any, error) {
			return map[string]any{
				"count":              5,
				"oldest_age_seconds": 120,
				"dpkms_reachable":    false,
				"last_failure_at":    "2026-05-06T10:28:30Z",
				"last_failure_error": "dial tcp: connection refused",
			}, nil
		},
		Sources: func(_ context.Context) ([]any, error) {
			return []any{
				map[string]any{"name": "clipboard", "active": true, "last_event_at": "2026-05-06T10:32:00Z"},
				map[string]any{"name": "filewatch", "active": true, "last_event_at": "2026-05-06T10:25:00Z"},
				map[string]any{"name": "browserhistory", "active": true, "last_event_at": "2026-05-06T10:30:00Z"},
				map[string]any{"name": "foreground", "active": true, "last_event_at": "2026-05-06T10:32:15Z"},
			}, nil
		},
		Health: func(_ context.Context) (any, error) {
			return map[string]any{
				"version":               "0.1.0",
				"uptime_seconds":        3600,
				"buffer_count":          5,
				"buffer_capacity":       4096,
				"buffer_pct_full":       0.122,
				"dpkms_endpoint":        "https://dpkms.example.com",
				"dpkms_last_success_at": "2026-05-06T10:00:00Z",
				"dpkms_last_failure_at": "2026-05-06T10:28:30Z",
			}, nil
		},
	}
}

func startLocalServer(t *testing.T) (string, func()) {
	t.Helper()
	srv := ambientmcp.New(fakeSnapshot())
	ts := httptest.NewServer(srv.Handler())
	return ts.URL, ts.Close
}

// TestLocalMCP_ServerStartsWithCtxd verifies the ctxd-side MCP server
// boots and responds to initialize.
func TestLocalMCP_ServerStartsWithCtxd(t *testing.T) {
	t.Parallel()
	url, cleanup := startLocalServer(t)
	defer cleanup()

	resp, err := http.Post(url, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestLocalMCP_DiscoveryFile verifies the daemon writes a discovery
// file (~/.local/state/ctxt/ambient/mcp.json) at startup. Skipped here
// because that's a daemon-driver concern (cmd/ctxd).
func TestLocalMCP_DiscoveryFile(t *testing.T) {
	t.Parallel()
	t.Skip("pending: cmd/ctxd discovery-file write on startup (daemon-driver concern)")
}

// TestLocalMCP_CurrentSession — happy-path cassette: tools/call current_session
// returns active session metadata.
func TestLocalMCP_CurrentSession(t *testing.T) {
	t.Parallel()
	url, cleanup := startLocalServer(t)
	defer cleanup()
	c := xrrClient(t, "us0220_current_session_happy")

	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"current_session","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("current_session: %+v", resp.Error)
	}
}

// TestLocalMCP_RecentLocal — happy-path cassette: recent_local returns
// recent ambient events.
func TestLocalMCP_RecentLocal(t *testing.T) {
	t.Parallel()
	url, cleanup := startLocalServer(t)
	defer cleanup()
	c := xrrClient(t, "us0220_recent_local_happy")

	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"recent_local","arguments":{"limit":3}}}`)
	if resp.Error != nil {
		t.Fatalf("recent_local: %+v", resp.Error)
	}
}

// TestLocalMCP_PendingEnqueueDuringNetworkLoss — happy-path cassette
// for the dpkms-down scenario. The fake snapshot reports
// dpkms_reachable=false so the cassette captures what an agent would
// see when called during a network outage.
func TestLocalMCP_PendingEnqueueDuringNetworkLoss(t *testing.T) {
	t.Parallel()
	url, cleanup := startLocalServer(t)
	defer cleanup()
	c := xrrClient(t, "us0220_pending_enqueue_dpkms_down")

	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pending_enqueue","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("pending_enqueue: %+v", resp.Error)
	}
}

// TestLocalMCP_Sources — happy-path cassette: sources returns the
// registered ambient sources + last-event timestamps.
func TestLocalMCP_Sources(t *testing.T) {
	t.Parallel()
	url, cleanup := startLocalServer(t)
	defer cleanup()
	c := xrrClient(t, "us0220_sources_happy")

	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sources","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("sources: %+v", resp.Error)
	}
}

// TestLocalMCP_Health — happy-path cassette: health returns daemon
// status summary.
func TestLocalMCP_Health(t *testing.T) {
	t.Parallel()
	url, cleanup := startLocalServer(t)
	defer cleanup()
	c := xrrClient(t, "us0220_health_happy")

	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"health","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("health: %+v", resp.Error)
	}
}

// TestLocalMCP_RecentLocalFailure — failure-path cassette: a
// recent_local call where the snapshot returns an error.
func TestLocalMCP_RecentLocalFailure(t *testing.T) {
	t.Parallel()
	url, cleanup := startLocalServer(t)
	defer cleanup()
	c := xrrClient(t, "us0220_recent_local_failure")

	// fakeSnapshot returns an error for source="force-error".
	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"recent_local","arguments":{"source":"force-error"}}}`)
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error in failure-path cassette")
	}
	if resp.Error.Code != mcp.ErrInternalError {
		t.Errorf("error.code = %d, want %d", resp.Error.Code, mcp.ErrInternalError)
	}
	if !strings.Contains(resp.Error.Message, "simulated local-buffer read failure") {
		t.Errorf("expected handler error to propagate; got %q", resp.Error.Message)
	}
}

// TestLocalMCP_NoProxyToDpkms documents the design decision: ctxd-side
// server NEVER proxies to dpkms (per ADR-068). Exercised by the absence
// of a SearchHandler in the LocalSnapshot — the server has no path to
// dpkms's data.
func TestLocalMCP_NoProxyToDpkms(t *testing.T) {
	t.Parallel()
	url, cleanup := startLocalServer(t)
	defer cleanup()
	c := xrrClient(t, "us0220_no_proxy")

	// Try to call "search" — it's a dpkms-side tool. The ctxd-side
	// server doesn't register it, so we expect method-not-found.
	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"x"}}}`)
	if resp.Error == nil {
		t.Fatal("expected error: dpkms-side tool 'search' should not exist on ctxd-side server")
	}
	if resp.Error.Code != mcp.ErrMethodNotFound {
		t.Errorf("error.code = %d, want %d (method-not-found)", resp.Error.Code, mcp.ErrMethodNotFound)
	}
}

// TestLocalMCP_SourceCrashedReported — pending: requires real source
// supervisor wiring to surface "source X failed at T" through the
// Sources tool's output. Deferred until the daemon driver lands.
func TestLocalMCP_SourceCrashedReported(t *testing.T) {
	t.Parallel()
	t.Skip("pending: real source supervisor + lifecycle event wiring in cmd/ctxd")
}

// TestLocalMCP_BothServersRegisteredOnInstall is a CLI-level test for
// `ctxt mcp install` — registers both ctxt-graph (dpkms) and ctxt-local
// (ctxd) endpoints with the user's MCP client config files.
func TestLocalMCP_BothServersRegisteredOnInstall(t *testing.T) {
	t.Parallel()
	t.Skip("pending: cmd/ctxt/cmd/mcp.go install matrix (Phase B)")
}
