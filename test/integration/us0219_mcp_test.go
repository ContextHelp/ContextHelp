package integration

// US-0219: MCP Agent Integration (dpkms-side server).
//
// E2E tests for the dpkms-side MCP read-surface (per ADR-068 + T-0533).
// Verifies the JSON-RPC 2.0 wire shape, tools/list + tools/call
// dispatch, and end-to-end happy + failure paths via xrr cassettes.
//
// Cassettes live at testdata/cassettes/us0219_*.yaml. To re-record:
//
//	XRR_MODE=record go test -count=1 ./test/integration/ -run TestMCP
//
// Default mode (replay) reads cassettes from disk and runs without a
// network or running server. CI runs in replay mode so this stays
// deterministic.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/xrr"
	xrrhttp "hop.top/xrr/adapters/http"

	"github.com/ideacrafterslabs/ctxt/internal/mcp"
)

// xrrTransport records/replays HTTP round-trips through the supplied
// xrr session. Pattern matches kit/go/storage/secret/openbao/openbao_test.go.
type xrrTransport struct {
	session *xrr.FileSession
	adapter *xrrhttp.Adapter
	base    http.RoundTripper
}

func (t *xrrTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}
	xrrReq := &xrrhttp.Request{
		Method: req.Method,
		URL:    req.URL.String(),
		Body:   string(body),
	}
	resp, err := t.session.Record(req.Context(), t.adapter, xrrReq, func() (xrr.Response, error) {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		r, err := t.base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		var rb []byte
		if r.Body != nil {
			rb, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}
		return &xrrhttp.Response{Status: r.StatusCode, Body: string(rb)}, nil
	})
	if err != nil {
		return nil, err
	}
	switch v := resp.(type) {
	case *xrrhttp.Response:
		return &http.Response{
			StatusCode: v.Status,
			Body:       io.NopCloser(strings.NewReader(v.Body)),
			Header:     make(http.Header),
		}, nil
	case *xrr.RawResponse:
		// Replay path: convert raw payload back to *http.Response.
		return rawToHTTP(v), nil
	default:
		return nil, errors.New("unexpected response type from xrr")
	}
}

func rawToHTTP(v *xrr.RawResponse) *http.Response {
	status := 200
	body := ""
	if v != nil && v.Payload != nil {
		switch n := v.Payload["status"].(type) {
		case int:
			status = n
		case float64:
			status = int(n)
		}
		if bs, ok := v.Payload["body"].(string); ok {
			body = bs
		}
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// newXRRSession constructs a session in the configured mode. Record mode
// writes cassettes to disk; replay mode reads them. Default: replay.
//
// xrr.NewFileCassette takes a DIRECTORY; one cassette = one subdirectory
// holding request/response YAML pairs per recorded round-trip.
func newXRRSession(t *testing.T, cassetteName string) *xrr.FileSession {
	t.Helper()
	mode := os.Getenv("XRR_MODE")
	if mode == "" {
		mode = "replay"
	}
	baseDir := os.Getenv("XRR_CASSETTE_DIR")
	if baseDir == "" {
		baseDir = filepath.Join("testdata", "cassettes")
	}
	cassetteDir := filepath.Join(baseDir, cassetteName)
	if mode == "record" {
		_ = os.MkdirAll(cassetteDir, 0o755)
	}
	m := xrr.Mode(mode)
	if m == xrr.ModePassthrough {
		return xrr.NewSession(m, nil)
	}
	return xrr.NewSession(m, xrr.NewFileCassette(cassetteDir))
}

// fakeContext returns the test ToolContext used by both record and
// replay paths. Record mode produces deterministic cassettes; replay
// mode never invokes the handlers.
func fakeContext() mcp.ToolContext {
	return mcp.ToolContext{
		SearchHandler: func(_ context.Context, query string, topK int) ([]any, error) {
			if query == "force-error" {
				return nil, errors.New("simulated search failure")
			}
			out := make([]any, 0, topK)
			for i := 0; i < topK && i < 2; i++ {
				out = append(out, map[string]any{
					"id":    "obj_test_" + string(rune('a'+i)),
					"title": "Test result for query: " + query,
					"score": 0.9 - float64(i)*0.1,
				})
			}
			return out, nil
		},
		SchemaHandler: func(_ context.Context) (map[string]any, error) {
			return map[string]any{
				"object_kinds": []string{"text", "url", "image", "audio", "video", "file", "meeting"},
				"edge_types":   []string{"mentions", "supersedes", "references"},
				"pipelines":    []string{"text.short", "text.long", "url.generic", "url.repo", "image.ocr", "audio.transcribe", "video.full"},
			}, nil
		},
	}
}

// startServer spins up the MCP handler on httptest. Returns the URL +
// cleanup. xrr wraps the http.Client used to call it.
func startServer(t *testing.T) (string, func()) {
	t.Helper()
	srv := mcp.New(fakeContext())
	ts := httptest.NewServer(srv.Handler())
	return ts.URL, ts.Close
}

// xrrClient builds an *http.Client whose transport records/replays via xrr.
func xrrClient(t *testing.T, cassetteName string) *http.Client {
	t.Helper()
	session := newXRRSession(t, cassetteName)
	return &http.Client{
		Transport: &xrrTransport{
			session: session,
			adapter: xrrhttp.NewAdapter(),
			base:    http.DefaultTransport,
		},
	}
}

// postRPC sends a JSON-RPC request and returns the parsed response.
func postRPC(t *testing.T, c *http.Client, url, body string) *mcp.JSONRPCResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)

	var jr mcp.JSONRPCResponse
	if err := json.Unmarshal(rb, &jr); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rb)
	}
	return &jr
}

// ---------------------------------------------------------------------------
// US-0219 integration tests with xrr cassettes.
// ---------------------------------------------------------------------------

// TestMCP_ServerStartup is the substrate-level "the server boots and
// responds to initialize" test. No xrr — direct httptest.
func TestMCP_ServerStartup(t *testing.T) {
	t.Parallel()
	url, cleanup := startServer(t)
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

// TestMCP_StreamableHTTPTransport — happy-path xrr cassette: a full
// initialize → tools/list → tools/call(search) round-trip. Cassette
// captures the JSON-RPC wire shape so future runs replay without a server.
func TestMCP_StreamableHTTPTransport(t *testing.T) {
	t.Parallel()
	url, cleanup := startServer(t)
	defer cleanup()
	c := xrrClient(t, "us0219_streamable_happy")

	// 1. initialize
	resp := postRPC(t, c, url, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if resp.Error != nil {
		t.Fatalf("initialize: %+v", resp.Error)
	}

	// 2. tools/list
	resp = postRPC(t, c, url, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if resp.Error != nil {
		t.Fatalf("tools/list: %+v", resp.Error)
	}
	tools := resp.Result.(map[string]any)["tools"].([]any)
	if len(tools) < 2 {
		t.Errorf("expected ≥2 tools (search + schema); got %d", len(tools))
	}

	// 3. tools/call search (happy)
	resp = postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search","arguments":{"query":"q3 planning","top_k":2}}}`)
	if resp.Error != nil {
		t.Fatalf("search: %+v", resp.Error)
	}
}

// TestMCP_StdioFallback is unimplementable from outside without
// fork/exec; left as documented gap. Real coverage will live in
// cmd/ctxt/cmd/mcp_test.go (Phase B).
func TestMCP_StdioFallback(t *testing.T) {
	t.Parallel()
	t.Skip("pending: ctxt mcp serve stdio subcommand (Phase B)")
}

// TestMCP_AllToolsListed verifies the registered tool names match the
// tools/list output. Today: schema + search.
func TestMCP_AllToolsListed(t *testing.T) {
	t.Parallel()
	url, cleanup := startServer(t)
	defer cleanup()
	c := xrrClient(t, "us0219_tools_list")

	resp := postRPC(t, c, url, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := resp.Result.(map[string]any)["tools"].([]any)
	names := make(map[string]bool)
	for _, x := range tools {
		names[x.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"schema", "search"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

// TestMCP_SearchTool — search via xrr-recorded happy path.
func TestMCP_SearchTool(t *testing.T) {
	t.Parallel()
	url, cleanup := startServer(t)
	defer cleanup()
	c := xrrClient(t, "us0219_search_happy")

	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"auth bug","top_k":2}}}`)
	if resp.Error != nil {
		t.Fatalf("search: %+v", resp.Error)
	}
	content := resp.Result.(map[string]any)["content"].([]any)
	if len(content) == 0 {
		t.Fatal("search: empty content")
	}
}

// TestMCP_SchemaTool — schema via xrr-recorded happy path.
func TestMCP_SchemaTool(t *testing.T) {
	t.Parallel()
	url, cleanup := startServer(t)
	defer cleanup()
	c := xrrClient(t, "us0219_schema_happy")

	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"schema","arguments":{}}}`)
	if resp.Error != nil {
		t.Fatalf("schema: %+v", resp.Error)
	}
}

// TestMCP_EntityTool through TestMCP_BusEventsEmitted are skeletons —
// the corresponding tools / wiring don't exist yet (Phase A extensions).
func TestMCP_EntityTool(t *testing.T) {
	t.Parallel()
	t.Skip("pending: entity tool (Phase A extension)")
}

func TestMCP_ComposeTool(t *testing.T) {
	t.Parallel()
	t.Skip("pending: compose tool wiring to existing ctxt compose runtime (Phase A extension)")
}

func TestMCP_SessionsTool(t *testing.T) {
	t.Parallel()
	t.Skip("pending: sessions/session tools (Phase C — depends on dpkms-side session storage migration)")
}

func TestMCP_ProfileScoping(t *testing.T) {
	t.Parallel()
	t.Skip("pending: profile parameter wiring through tool implementations")
}

func TestMCP_PublicBindRequiresAuth(t *testing.T) {
	t.Parallel()
	t.Skip("pending: ADR-023 auth layer wiring on the MCP handler (Phase F)")
}

func TestMCP_BusEventsEmitted(t *testing.T) {
	t.Parallel()
	t.Skip("pending: kit/runtime/bus emission inside Server.dispatch (deferred to Phase E observability work)")
}

func TestMCP_CELVeto(t *testing.T) {
	t.Parallel()
	t.Skip("pending: kit/runtime/policy CEL guard on dpkms.mcp.tool.invoked topic")
}

// ---------------------------------------------------------------------------
// US-0219 failure-path xrr cassette: simulated handler error.
// ---------------------------------------------------------------------------

// TestMCP_SearchToolFailurePath records the failure cassette: a search
// call whose handler returns an error. The MCP server propagates this
// as a JSON-RPC ErrInternalError. This pairs with TestMCP_SearchTool
// (happy path) so the failure cassette is named explicitly and can be
// re-recorded independently.
func TestMCP_SearchToolFailurePath(t *testing.T) {
	t.Parallel()
	url, cleanup := startServer(t)
	defer cleanup()
	c := xrrClient(t, "us0219_search_failure")

	// fakeContext.SearchHandler returns an error when query=="force-error".
	resp := postRPC(t, c, url,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"force-error"}}}`)
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error in failure-path cassette")
	}
	if resp.Error.Code != mcp.ErrInternalError {
		t.Errorf("error.code = %d, want %d (internal)", resp.Error.Code, mcp.ErrInternalError)
	}
	if !strings.Contains(resp.Error.Message, "simulated search failure") {
		t.Errorf("expected error to propagate handler failure; got %q", resp.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// MCP install matrix tests live in a different test target — they
// exercise the cmd/ctxt/cmd/mcp.go install paths which write to user
// config files. Not part of US-0219 wire-shape verification.
// ---------------------------------------------------------------------------
