package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// recordingContext captures the SearchRequest the tool hands its handler.
func recordingContext(got *SearchRequest) ToolContext {
	return ToolContext{SearchHandler: func(_ context.Context, req SearchRequest) (*SearchResult, error) {
		*got = req
		return &SearchResult{ExecutedMode: "fts_only", Results: []any{}, Diagnostics: map[string]any{"semantic": map[string]any{"status": "no_default_model"}}}, nil
	}}
}

func callSearch(t *testing.T, srv *Server, args string) *JSONRPCResponse {
	t.Helper()
	return post(t, srv, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"search","arguments":`+args+`}}`)
}

func TestSearchTool_DefaultsToHybrid(t *testing.T) {
	var got SearchRequest
	srv := New(recordingContext(&got))
	resp := callSearch(t, srv, `{"query":"orbital zephyr"}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if got.Mode != "hybrid" || got.TopK != 10 {
		t.Errorf("request = %+v, want mode hybrid, top_k 10", got)
	}
	text := resp.Result.(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var inner map[string]any
	if err := json.Unmarshal([]byte(text), &inner); err != nil {
		t.Fatalf("inner JSON: %v", err)
	}
	if inner["mode"] != "hybrid" || inner["executed_mode"] != "fts_only" {
		t.Errorf("mode/executed_mode = %v/%v; the fallback must be visible", inner["mode"], inner["executed_mode"])
	}
	if _, ok := inner["diagnostics"]; !ok {
		t.Error("diagnostics missing from the result")
	}
}

func TestSearchTool_PassesFiltersThrough(t *testing.T) {
	var got SearchRequest
	srv := New(recordingContext(&got))
	resp := callSearch(t, srv, `{"query":"q","top_k":500,"mode":"hybrid","profile":"alpha","min_score":0.2,
		"meta_type":"task","topic":"launch","person":"ada","source_type":"meeting","since":"2026-09-01","until":"2026-09-30"}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	switch {
	case got.TopK != 100:
		t.Errorf("top_k = %d, want clamped to 100", got.TopK)
	case got.Mode != "hybrid" || got.Profile != "alpha":
		t.Errorf("mode/profile = %q/%q", got.Mode, got.Profile)
	case got.MinScore == nil || *got.MinScore != 0.2:
		t.Errorf("min_score = %v", got.MinScore)
	case got.MetaType != "task" || got.Topic != "launch" || got.Person != "ada" || got.SourceType != "meeting":
		t.Errorf("facets = %+v", got)
	case got.Since != "2026-09-01" || got.Until != "2026-09-30":
		t.Errorf("since/until = %v/%v", got.Since, got.Until)
	}
}

func TestSearchTool_BadArgumentsAreInvalidParams(t *testing.T) {
	srv := New(fakeContext())
	for name, args := range map[string]string{
		"missing query":        `{}`,
		"unknown mode":         `{"query":"q","mode":"semantic"}`,
		"rsql mode":            `{"query":"type==note","mode":"rsql"}`,
		"mode not a string":    `{"query":"q","mode":3}`,
		"min_score string":     `{"query":"q","min_score":"high"}`,
		"bad since":            `{"query":"q","since":"yesterday"}`,
		"timestamp until":      `{"query":"q","until":"2026-09-30T00:00:00Z"}`,
		"min_score non-hybrid": `{"query":"q","mode":"fts","min_score":0.5}`,
	} {
		t.Run(name, func(t *testing.T) {
			resp := callSearch(t, srv, args)
			if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
				t.Fatalf("error = %+v, want code %d", resp.Error, ErrInvalidParams)
			}
		})
	}
}

// A handler can flag its own caller errors as invalid params.
func TestSearchTool_HandlerInvalidParams(t *testing.T) {
	srv := New(ToolContext{SearchHandler: func(context.Context, SearchRequest) (*SearchResult, error) {
		return nil, InvalidParams(errors.New("limit must not be negative"))
	}})
	resp := callSearch(t, srv, `{"query":"q"}`)
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams || !strings.Contains(resp.Error.Message, "negative") {
		t.Fatalf("error = %+v", resp.Error)
	}
}

func TestSearchTool_DescriptionMatchesModes(t *testing.T) {
	d := (&SearchTool{}).Description()
	for _, m := range SearchModes {
		if !strings.Contains(d, `"`+m+`"`) {
			t.Errorf("description does not mention mode %q", m)
		}
	}
}
