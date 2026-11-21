package mcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
)

// SearchTool searches the user's knowledge objects. The default mode is
// hybrid: full-text and semantic (embedding) retrieval fused with RRF and
// reranked, every result scored; fts and vector run one leg. Per ADR-068
// the tool takes free text, not RSQL.
//
// The tool validates arguments and shapes the result; the query itself
// runs in ToolContext.SearchHandler, which the dpkms server wires to
// service.Service.Find.
type SearchTool struct {
	tc *ToolContext
}

// SearchModes are the search tool's modes; the first is the default.
var SearchModes = []string{"hybrid", "fts", "vector"}

// SearchRequest is the search tool's validated arguments.
type SearchRequest struct {
	Query string
	// TopK caps the result count (1..100).
	TopK int
	// Mode is one of SearchModes.
	Mode string
	// Profile is the caller's focus profile, a lens: it never narrows results.
	Profile string
	// MinScore sets the hybrid threshold when set; hybrid only.
	MinScore *float64
	// Tagged is the tag selection in the internal/tagfilter grammar,
	// canonical form: "a,b" any, "+a,+b" all, "+a,b" a required, b optional.
	Tagged string
	// Since and Until bound the created time: an RFC 3339 timestamp or a
	// YYYY-MM-DD date.
	Since, Until string
	// Metadata facet filters. MentionedSince and MentionedUntil are
	// YYYY-MM-DD dates compared with dates mentioned in the content.
	MetaType, Topic, Person, SourceType string
	MentionedSince, MentionedUntil      string
}

// SearchResult is what a SearchHandler returns.
type SearchResult struct {
	// ExecutedMode is what ran: hybrid, fts, vector, or fts_only /
	// fts_fallback when the semantic leg could not run.
	ExecutedMode string
	// Results are the ranked hits, JSON-encodable.
	Results []any
	// Diagnostics describes a hybrid or vector search (candidate counts,
	// threshold, the semantic leg's status); nil otherwise.
	Diagnostics any
}

func (t *SearchTool) Name() string { return "search" }

func (t *SearchTool) Description() string {
	return "Search the user's knowledge graph. Default mode \"hybrid\" combines full-text and semantic " +
		"(embedding) search, so it matches both exact keywords — a person's name, project or company, " +
		"topic, date, file path, a phrase the user used — and related wording. Other modes: \"fts\" " +
		"(keywords only) and \"vector\" (meaning only). " +
		"Returns ranked results with id, type, title, snippet and, for hybrid and vector, a score " +
		"(hybrid adds a per-signal score_breakdown). When no embedding model is available, hybrid and " +
		"vector answer from full-text search and say so in executed_mode and diagnostics.semantic."
}

func (t *SearchTool) InputSchema() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	date := func(desc string) map[string]any {
		return map[string]any{"type": "string", "format": "date", "description": desc + " YYYY-MM-DD."}
	}
	instant := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc + " RFC 3339 timestamp or YYYY-MM-DD."}
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": str("The search query: keywords or a phrase."),
			"top_k": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return. Default: 10.",
				"default":     10,
				"minimum":     1,
				"maximum":     100,
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        SearchModes,
				"default":     SearchModes[0],
				"description": "hybrid (default), fts or vector.",
			},
			"profile": str("Only return objects of this profile."),
			"min_score": map[string]any{
				"type":        "number",
				"minimum":     0,
				"description": "hybrid only: drop results scoring below this. Default: 0.",
			},
			"tagged": map[string]any{
				"description": "Only objects with these tags. A comma-separated string or an array of strings, " +
					"combined in order. Plain tags are optional: with no + tag one listed tag must match; " +
					"a leading + requires a tag (\"+a,+b,+c\" needs all three; \"+a,b,+c\" needs a and c, b only ranks). Results matching " +
					"more of the listed tags rank first, then by relevance. Tags may hold spaces, " +
					"commas separate them, case matters. An unknown tag matches nothing.",
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
			"since":           instant("Only objects created at or after this; a date is the start of that UTC day."),
			"until":           instant("Only objects created before this; a date takes that whole UTC day."),
			"meta_type":       str("Only objects whose metadata type matches (e.g. task, observation)."),
			"topic":           str("Only objects tagged with this topic."),
			"person":          str("Only objects mentioning this person."),
			"source_type":     str("Only objects from this source type."),
			"mentioned_since": date("Only objects mentioning a date on or after this."),
			"mentioned_until": date("Only objects mentioning a date on or before this."),
		},
		"required": []string{"query"},
	}
}

func (t *SearchTool) Invoke(ctx context.Context, args map[string]any) (any, error) {
	req, err := parseSearchArgs(args)
	if err != nil {
		return nil, InvalidParams(err)
	}
	if t.tc == nil || t.tc.SearchHandler == nil {
		return nil, errors.New("search handler not configured")
	}
	res, err := t.tc.SearchHandler(ctx, req)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"query":         req.Query,
		"top_k":         req.TopK,
		"mode":          req.Mode,
		"executed_mode": res.ExecutedMode,
		"count":         len(res.Results),
		"results":       res.Results,
	}
	if res.Diagnostics != nil {
		out["diagnostics"] = res.Diagnostics
	}
	return out, nil
}

// parseSearchArgs validates the tool arguments. top_k is clamped to its
// range, as it always was; every other malformed argument is an error.
func parseSearchArgs(args map[string]any) (SearchRequest, error) {
	req := SearchRequest{TopK: 10, Mode: SearchModes[0]}
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return req, errors.New("missing or empty 'query'")
	}
	req.Query = query
	if v, ok := args["top_k"]; ok {
		switch n := v.(type) {
		case float64: // JSON numbers decode as float64 in Go
			req.TopK = int(n)
		case int:
			req.TopK = n
		}
	}
	req.TopK = min(max(req.TopK, 1), 100)

	var err error
	if req.Mode, err = stringArg(args, "mode", SearchModes[0]); err != nil {
		return req, err
	}
	if !slices.Contains(SearchModes, req.Mode) {
		return req, fmt.Errorf("'mode' must be one of %v, got %q", SearchModes, req.Mode)
	}
	for name, dst := range map[string]*string{
		"profile": &req.Profile, "meta_type": &req.MetaType, "topic": &req.Topic,
		"person": &req.Person, "source_type": &req.SourceType,
	} {
		if *dst, err = stringArg(args, name, ""); err != nil {
			return req, err
		}
	}
	if v, ok := args["min_score"]; ok {
		n, isNum := v.(float64)
		if !isNum || math.IsNaN(n) || math.IsInf(n, 0) {
			return req, fmt.Errorf("'min_score' must be a number, got %v", v)
		}
		if req.Mode != SearchModes[0] {
			return req, fmt.Errorf("'min_score' applies to mode hybrid only, not %q", req.Mode)
		}
		req.MinScore = &n
	}
	if req.Tagged, err = taggedArg(args); err != nil {
		return req, err
	}
	if req.Since, err = instantArg(args, "since"); err != nil {
		return req, err
	}
	if req.Until, err = instantArg(args, "until"); err != nil {
		return req, err
	}
	if req.MentionedSince, err = dateArg(args, "mentioned_since"); err != nil {
		return req, err
	}
	if req.MentionedUntil, err = dateArg(args, "mentioned_until"); err != nil {
		return req, err
	}
	return req, nil
}

// taggedArg reads the tagged argument, a string or an array of strings in
// the tagfilter grammar, and returns it in canonical form.
func taggedArg(args map[string]any) (string, error) {
	var vals []string
	switch v := args["tagged"].(type) {
	case nil:
		return "", nil
	case string:
		vals = []string{v}
	case []any:
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return "", fmt.Errorf("'tagged' items must be strings, got %T", e)
			}
			vals = append(vals, s)
		}
	default:
		return "", fmt.Errorf("'tagged' must be a string or an array of strings, got %T", v)
	}
	f, err := tagfilter.Parse(vals)
	if err != nil {
		return "", fmt.Errorf("'tagged': %w", err)
	}
	return f.String(), nil
}

// instantArg reads an RFC 3339 timestamp or YYYY-MM-DD date argument.
func instantArg(args map[string]any, name string) (string, error) {
	s, err := stringArg(args, name, "")
	if err != nil || s == "" {
		return "", err
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		if _, err := time.Parse(time.DateOnly, s); err != nil {
			return "", fmt.Errorf("'%s' must be an RFC 3339 timestamp or YYYY-MM-DD, got %q", name, s)
		}
	}
	return s, nil
}

func stringArg(args map[string]any, name, def string) (string, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return def, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("'%s' must be a string, got %T", name, v)
	}
	if s == "" {
		return def, nil
	}
	return s, nil
}

func dateArg(args map[string]any, name string) (string, error) {
	s, err := stringArg(args, name, "")
	if err != nil || s == "" {
		return "", err
	}
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return "", fmt.Errorf("'%s' must be a YYYY-MM-DD date, got %q", name, s)
	}
	return s, nil
}
