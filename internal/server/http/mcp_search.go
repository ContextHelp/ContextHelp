package http

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/mcp"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// mcpSnippetRunes caps a search result's snippet.
const mcpSnippetRunes = 240

// mcpSearchHandler runs the MCP search tool through service.Find, the
// engine behind POST /api/v1/find, with sem as its semantic leg. Hybrid
// runs with explain on, so each hit carries its score and breakdown.
func mcpSearchHandler(svc *service.Service, sem retrieval.SemanticSource) func(context.Context, mcp.SearchRequest) (*mcp.SearchResult, error) {
	return func(ctx context.Context, req mcp.SearchRequest) (*mcp.SearchResult, error) {
		res, err := svc.Find(ctx, service.FindRequest{
			Query:   req.Query,
			Mode:    req.Mode,
			Limit:   req.TopK,
			Profile: req.Profile,
			Filter: service.FindFilter{
				MetaType:   req.MetaType,
				Topic:      req.Topic,
				Person:     req.Person,
				SourceType: req.SourceType,
				Since:      req.Since,
				Until:      req.Until,
			},
			Search:  service.FindSearch{MinScore: req.MinScore},
			Explain: req.Mode == service.FindModeHybrid,
		}, sem)
		if errors.Is(err, service.ErrInvalidFind) {
			return nil, mcp.InvalidParams(err)
		}
		if err != nil {
			return nil, err
		}
		executed := mcpExecutedMode(res)
		out := &mcp.SearchResult{ExecutedMode: executed, Results: make([]any, 0, len(res.Objects))}
		if res.Mode != service.FindModeFTS {
			out.Diagnostics = res.Diagnostics
		}
		for i, o := range res.Objects {
			var b *service.ScoreBreakdown
			if i < len(res.Explain) {
				b = &res.Explain[i].Breakdown
			}
			out.Results = append(out.Results, mcpSearchHit(o, executed, b))
		}
		return out, nil
	}
}

// mcpExecutedMode names what ran: the requested mode, or fts_only (no
// default embedding model) / fts_fallback (the semantic leg failed) when
// a vector or hybrid search answered from full-text only.
func mcpExecutedMode(res *service.FindResult) string {
	rep := res.Diagnostics.Semantic
	switch {
	case res.Mode == service.FindModeFTS || rep == nil || rep.OK():
		return res.Mode
	case rep.Status == retrieval.SemanticNoDefaultModel:
		return string(service.SearchModeFTSOnly)
	default:
		return string(service.SearchModeFTSFallback)
	}
}

// mcpSearchHit shapes one result. A hybrid hit's score is its breakdown
// total; a vector hit's is its similarity when the vector leg ran. FTS
// scores are driver-specific and not comparable, so fts hits have none.
func mcpSearchHit(o *storage.KnowledgeObject, executed string, b *service.ScoreBreakdown) map[string]any {
	hit := map[string]any{
		"id":      o.ID,
		"type":    o.Type,
		"subtype": o.Subtype,
		"snippet": truncateRunes(strings.Join(strings.Fields(objectBody(o)), " "), mcpSnippetRunes),
	}
	if t := objectTitle(o); t != "" {
		hit["title"] = t
	}
	if o.ProfileID != "" {
		hit["profile_id"] = o.ProfileID
	}
	switch {
	case b != nil:
		hit["score"] = b.Total
		hit["score_breakdown"] = b
	case executed == service.FindModeVector:
		if v, ok := o.Metadata["score"].(float64); ok {
			hit["score"] = v
		}
	}
	return hit
}

// objectTitle is the metadata title or, failing that, the first summary.
func objectTitle(o *storage.KnowledgeObject) string {
	if t, ok := o.Metadata["title"].(string); ok && strings.TrimSpace(t) != "" {
		return strings.TrimSpace(t)
	}
	if len(o.Summaries) > 0 {
		return strings.TrimSpace(o.Summaries[0])
	}
	return ""
}

// objectBody is the object's text, or its first section when it has none.
func objectBody(o *storage.KnowledgeObject) string {
	if b := projection.BodyText(o); b != "" {
		return b
	}
	for _, sec := range projection.ProjectDocument(o).Sections {
		if sec.Content != "" {
			return sec.Content
		}
	}
	return ""
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
