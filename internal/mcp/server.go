// Package mcp is the dpkms-side MCP read-surface (per ADR-068).
//
// Mounts at /api/v1/mcp/ on the existing dpkms HTTP listener as a sibling
// of the REST routes. Uses streamable-HTTP transport per MCP spec
// 2025-03-26. The handler is a thin JSON-RPC 2.0 dispatcher; tool
// implementations live in this package as Tool implementations.
//
// The MCP protocol surface is small enough that a hand-rolled handler
// (~150 LoC of JSON-RPC + tool dispatch) is appropriate vs. depending on
// an evolving upstream Go SDK whose streamable-HTTP support is in flux.
//
// Phase A scope (this file): scaffold + 2 tools (schema, search).
// Phase A extension: list, get, entity, recent, compose.
// Phase C: sessions, session.
// Phase D: ctxd-side server (internal/ambient/mcp/).
//
// Read-only by design. No tool can mutate. Writes go through the existing
// /api/v1/analyze enqueue path. Per ADR-068 §Decision item 1.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// JSONRPCVersion is the MCP wire version. MCP uses JSON-RPC 2.0 with
// extensions (per spec 2025-03-26).
const JSONRPCVersion = "2.0"

// MCPProtocolVersion is the MCP protocol version this server implements.
const MCPProtocolVersion = "2025-03-26"

// Tool is the interface every MCP tool implements.
//
// Each tool is read-only and stateless from the substrate's perspective —
// state lives in the ToolContext (storage handles, profile resolver, etc.).
type Tool interface {
	// Name returns the tool name as exposed to MCP clients (e.g. "search").
	Name() string
	// Description is the human-readable description shown to MCP clients.
	// Should describe WHEN to use this tool, not just what it does.
	Description() string
	// InputSchema returns the JSON Schema for the tool's arguments.
	InputSchema() map[string]any
	// Invoke runs the tool with the supplied arguments and returns the
	// result. Errors are surfaced to the client as JSON-RPC errors.
	Invoke(ctx context.Context, args map[string]any) (any, error)
}

// ToolContext is what tools read from to do their work. The Server
// constructs one and shares it across tool invocations.
//
// Substrate handles stay opaque interfaces here so this package doesn't
// transitively pull in storage / pipeline / etc. dependencies. The caller
// (cmd/dpkms's serve wiring) supplies them.
type ToolContext struct {
	// SearchHandler is called by the search tool. Returns matched
	// objects as opaque JSON-encodable values.
	SearchHandler func(ctx context.Context, query string, topK int) ([]any, error)

	// SchemaHandler returns the storage taxonomy. Static-ish; cached at
	// startup but the handler is supplied so test doubles can inject.
	SchemaHandler func(ctx context.Context) (map[string]any, error)
}

// Server hosts the MCP read-surface. One Server per HTTP listener.
type Server struct {
	tc    ToolContext
	tools map[string]Tool
	mu    sync.RWMutex
}

// New constructs a Server with the supplied ToolContext and the default
// tool set (schema, search). Additional tools can be added via Register.
func New(tc ToolContext) *Server {
	s := &Server{tc: tc, tools: make(map[string]Tool)}
	s.Register(&SchemaTool{tc: &s.tc})
	s.Register(&SearchTool{tc: &s.tc})
	return s
}

// Register adds a tool. Used by Phase A extensions that ship more tools.
func (s *Server) Register(tool Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name()] = tool
}

// Handler returns the HTTP handler for the MCP endpoint. Mount at
// /api/v1/mcp/ (or the ctxd-side /mcp).
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

// JSONRPCRequest is the standard JSON-RPC 2.0 envelope.
type JSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// JSONRPCResponse is the standard JSON-RPC 2.0 response envelope.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError is the JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MCP-defined error codes (per spec 2025-03-26).
const (
	ErrParseError     = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternalError  = -32603
)

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "MCP requires POST", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, nil, ErrParseError, fmt.Sprintf("parse: %v", err))
		return
	}
	if req.JSONRPC != JSONRPCVersion {
		s.writeError(w, req.ID, ErrInvalidRequest, "jsonrpc must be 2.0")
		return
	}

	resp := s.dispatch(r.Context(), &req)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Best-effort: response body is already half-written; client will
		// see truncation. This is a server-side bug, not a wire error.
		return
	}
}

// dispatch routes a JSON-RPC request to the right MCP method handler.
//
// MCP standard methods (per spec 2025-03-26):
//   initialize       — handshake; client sends capabilities, server replies
//   tools/list       — list available tools with their schemas
//   tools/call       — invoke a tool with arguments
//   ping             — keepalive
//
// We implement the minimum to make MCP clients happy + serve our tools.
func (s *Server) dispatch(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "ping":
		return &JSONRPCResponse{JSONRPC: JSONRPCVersion, ID: req.ID, Result: map[string]any{}}
	default:
		return errorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

func (s *Server) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": MCPProtocolVersion,
			"serverInfo": map[string]any{
				"name":    "ctxt-graph",
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			"instructions": ServerInstructions,
		},
	}
}

func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tools := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, map[string]any{
			"name":        t.Name(),
			"description": t.Description(),
			"inputSchema": t.InputSchema(),
		})
	}
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Result:  map[string]any{"tools": tools},
	}
}

func (s *Server) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	name, ok := req.Params["name"].(string)
	if !ok {
		return errorResponse(req.ID, ErrInvalidParams, "tools/call: missing or invalid 'name'")
	}
	args, _ := req.Params["arguments"].(map[string]any)
	if args == nil {
		args = map[string]any{}
	}

	s.mu.RLock()
	tool, found := s.tools[name]
	s.mu.RUnlock()
	if !found {
		return errorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("tool %q not found", name))
	}

	result, err := tool.Invoke(ctx, args)
	if err != nil {
		return errorResponse(req.ID, ErrInternalError, fmt.Sprintf("%s: %v", name, err))
	}
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": jsonString(result)},
			},
			"isError": false,
		},
	}
}

func (s *Server) writeError(w http.ResponseWriter, id any, code int, message string) {
	resp := errorResponse(id, code, message)
	_ = json.NewEncoder(w).Encode(resp)
}

func errorResponse(id any, code int, message string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
}

// jsonString marshals v to compact JSON. Used by tools/call to wrap
// arbitrary tool results as MCP "text" content (per spec, tool results
// are an array of typed content objects; for ctxt's structured returns
// we use type=text + JSON body).
func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error())
	}
	return string(b)
}

// ServerInstructions is the human-readable instruction string sent to
// MCP clients on initialize. Per ADR-068 §Decision item 1, this teaches
// agents to call ctxt tools FIRST when answering personal questions.
const ServerInstructions = `ctxt is the user's personal knowledge graph — captures, decisions, mentions, sessions, projects, people, recent activity. CALL THESE TOOLS FIRST whenever the user asks about themselves, their work, their recent context: "what was I doing this afternoon?" / "what did I decide about X?" / "summarize my last session on Y" / "who is Alice?" — prefer this graph over replying "I don't know."`
