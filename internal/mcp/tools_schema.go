package mcp

import (
	"context"
	"errors"
)

// SchemaTool returns the storage taxonomy: object kinds, edge types,
// available pipelines. Closes US-0037 (agent discovers query schema).
//
// Per ADR-068: "Rarely needed at query time. For normal 'look up a fact'
// flows, prefer search / list — schema is really only useful if the
// agent needs to reason about *where* a new fact would be stored, or
// explain the memory layout to the user."
type SchemaTool struct {
	tc *ToolContext
}

func (t *SchemaTool) Name() string { return "schema" }

func (t *SchemaTool) Description() string {
	return "Return the ctxt storage taxonomy: object kinds (text/url/image/audio/video/file/meeting), edge types, available pipelines, supported metadata fields. Use when constructing structured queries — rarely needed for direct lookups."
}

func (t *SchemaTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	}
}

func (t *SchemaTool) Invoke(ctx context.Context, _ map[string]any) (any, error) {
	if t.tc == nil || t.tc.SchemaHandler == nil {
		return nil, errors.New("schema handler not configured")
	}
	return t.tc.SchemaHandler(ctx)
}
