package mcp

import (
	"context"
	"errors"
)

// SearchTool wraps the existing dpkms FTS + vector hybrid search. Per
// ADR-068: "Hybrid full-text + semantic search across the user's
// knowledge graph. Best when you have specific keywords."
//
// In Phase A this delegates to the SearchHandler in ToolContext, which
// the dpkms server wires to its existing service.Service.SearchObjects
// (or equivalent). The MCP layer adds nothing query-side; it's a thin
// wire-shape adapter.
type SearchTool struct {
	tc *ToolContext
}

func (t *SearchTool) Name() string { return "search" }

func (t *SearchTool) Description() string {
	return "Hybrid full-text + semantic search across the user's knowledge graph. Best when you have specific keywords — a person's name, project / company name, topic, date, file path, or a phrase the user might have used. Returns ranked results with snippets."
}

func (t *SearchTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query. Supports keywords, phrases.",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return. Default: 10.",
				"default":     10,
				"minimum":     1,
				"maximum":     100,
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchTool) Invoke(ctx context.Context, args map[string]any) (any, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, errors.New("missing or empty 'query'")
	}
	topK := 10
	if v, ok := args["top_k"]; ok {
		switch n := v.(type) {
		case float64: // JSON numbers decode as float64 in Go
			topK = int(n)
		case int:
			topK = n
		}
	}
	if topK < 1 {
		topK = 1
	}
	if topK > 100 {
		topK = 100
	}

	if t.tc == nil || t.tc.SearchHandler == nil {
		return nil, errors.New("search handler not configured")
	}
	results, err := t.tc.SearchHandler(ctx, query, topK)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"query":   query,
		"top_k":   topK,
		"count":   len(results),
		"results": results,
	}, nil
}
