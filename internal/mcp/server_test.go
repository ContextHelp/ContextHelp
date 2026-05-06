package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeContext returns a ToolContext that responds with canned data,
// useful for isolating MCP wire behavior from real storage.
func fakeContext() ToolContext {
	return ToolContext{
		SearchHandler: func(_ context.Context, query string, topK int) ([]any, error) {
			if query == "force-error" {
				return nil, errors.New("simulated search failure")
			}
			return []any{
				map[string]any{"id": "obj_1", "title": "first match for " + query, "score": 0.95},
				map[string]any{"id": "obj_2", "title": "second match", "score": 0.81},
			}, nil
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

// post performs a single JSON-RPC POST to the server's handler and
// returns the parsed response.
func post(t *testing.T, srv *Server, body string) *JSONRPCResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Logf("non-200 response: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}
	return &resp
}

func TestServer_InitializeReturnsCapabilities(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	resp := post(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("initialize: unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("initialize: result not a map; got %T", resp.Result)
	}
	if got := result["protocolVersion"]; got != MCPProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", got, MCPProtocolVersion)
	}
	if _, has := result["instructions"]; !has {
		t.Error("initialize: result must include 'instructions' for client-side guidance")
	}
}

func TestServer_ToolsListReturnsBuiltinTools(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	resp := post(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if resp.Error != nil {
		t.Fatalf("tools/list: %+v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) < 2 {
		t.Fatalf("tools/list returned %d tools, want at least 2 (schema + search)", len(tools))
	}
	names := make(map[string]bool)
	for _, t := range tools {
		tm := t.(map[string]any)
		names[tm["name"].(string)] = true
	}
	if !names["search"] || !names["schema"] {
		t.Errorf("missing built-in tools; got %v", names)
	}
}

func TestServer_ToolsCallSearchHappyPath(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search","arguments":{"query":"q3 planning","top_k":5}}}`
	resp := post(t, srv, body)
	if resp.Error != nil {
		t.Fatalf("search: unexpected error: %+v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("search: result.content must be non-empty")
	}
	textBlock := content[0].(map[string]any)
	if textBlock["type"] != "text" {
		t.Errorf("content[0].type = %v, want text", textBlock["type"])
	}
	// The text payload contains a JSON-encoded result; verify shape.
	var inner map[string]any
	if err := json.Unmarshal([]byte(textBlock["text"].(string)), &inner); err != nil {
		t.Fatalf("inner JSON: %v", err)
	}
	if inner["query"] != "q3 planning" {
		t.Errorf("inner.query = %v, want q3 planning", inner["query"])
	}
	if inner["top_k"].(float64) != 5 {
		t.Errorf("inner.top_k = %v, want 5", inner["top_k"])
	}
	if int(inner["count"].(float64)) != 2 {
		t.Errorf("inner.count = %v, want 2", inner["count"])
	}
}

func TestServer_ToolsCallSearchPropagatesError(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	// fakeContext returns an error when query=="force-error".
	body := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search","arguments":{"query":"force-error"}}}`
	resp := post(t, srv, body)
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error for handler failure; got nil")
	}
	if resp.Error.Code != ErrInternalError {
		t.Errorf("error.code = %d, want %d (internal)", resp.Error.Code, ErrInternalError)
	}
	if !strings.Contains(resp.Error.Message, "simulated search failure") {
		t.Errorf("error.message should propagate handler error; got %q", resp.Error.Message)
	}
}

func TestServer_ToolsCallSchemaReturnsTaxonomy(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	body := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"schema","arguments":{}}}`
	resp := post(t, srv, body)
	if resp.Error != nil {
		t.Fatalf("schema: %+v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	textBlock := result["content"].([]any)[0].(map[string]any)
	var taxonomy map[string]any
	_ = json.Unmarshal([]byte(textBlock["text"].(string)), &taxonomy)
	kinds := taxonomy["object_kinds"].([]any)
	if len(kinds) != 7 {
		t.Errorf("object_kinds count = %d, want 7", len(kinds))
	}
}

func TestServer_UnknownToolReturnsMethodNotFound(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	body := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`
	resp := post(t, srv, body)
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if resp.Error.Code != ErrMethodNotFound {
		t.Errorf("error.code = %d, want %d", resp.Error.Code, ErrMethodNotFound)
	}
}

func TestServer_UnknownMethodReturnsMethodNotFound(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	body := `{"jsonrpc":"2.0","id":7,"method":"some/unknown/method"}`
	resp := post(t, srv, body)
	if resp.Error == nil || resp.Error.Code != ErrMethodNotFound {
		t.Errorf("expected method-not-found error; got %+v", resp.Error)
	}
}

func TestServer_BadJSONRPCVersion(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	body := `{"jsonrpc":"1.0","id":8,"method":"initialize"}`
	resp := post(t, srv, body)
	if resp.Error == nil || resp.Error.Code != ErrInvalidRequest {
		t.Errorf("expected invalid-request error; got %+v", resp.Error)
	}
}

func TestServer_NonPostReturns405(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_PingReturnsEmptyResult(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	resp := post(t, srv, `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	if resp.Error != nil {
		t.Fatalf("ping: %+v", resp.Error)
	}
}

func TestServer_RegistersAdditionalTools(t *testing.T) {
	t.Parallel()
	srv := New(fakeContext())
	srv.Register(&fakeTool{name: "extra"})

	resp := post(t, srv, `{"jsonrpc":"2.0","id":10,"method":"tools/list"}`)
	tools := resp.Result.(map[string]any)["tools"].([]any)
	found := false
	for _, t := range tools {
		if t.(map[string]any)["name"] == "extra" {
			found = true
			break
		}
	}
	if !found {
		t.Error("registered tool 'extra' not in tools/list output")
	}
}

// fakeTool is a minimal Tool for registration tests.
type fakeTool struct{ name string }

func (f *fakeTool) Name() string                { return f.name }
func (f *fakeTool) Description() string         { return "fake" }
func (f *fakeTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (f *fakeTool) Invoke(_ context.Context, _ map[string]any) (any, error) {
	return map[string]any{}, nil
}

// readAll consumes an io.Reader; helper for diagnostic logs.
var _ = io.ReadAll
var _ = bytes.NewReader
