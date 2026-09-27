package dpkmsclient

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Find modes.
const (
	FindModeFTS    = "fts"
	FindModeVector = "vector"
	FindModeHybrid = "hybrid"
)

// FindRequest is the body of POST /api/v1/find. It mirrors the request
// the dpkms handler decodes, field for field on the wire; a test holds the
// two in step. The caller resolves its profile and search strategy and
// sends the values (ADR-077 §6).
type FindRequest struct {
	// Query is the search text. Required.
	Query string `json:"query"`
	// Mode is FindModeFTS, FindModeVector or FindModeHybrid; "" means
	// hybrid.
	Mode string `json:"mode,omitempty"`
	// Limit caps the result list; 0 means dpkms's default (10).
	Limit int `json:"limit,omitempty"`
	// Profile restricts the search and facet counts to objects owned by
	// that profile; empty means no profile filter.
	Profile string `json:"profile,omitempty"`
	// Filter narrows the search to objects matching metadata facets.
	Filter FindFilter `json:"filter,omitzero"`
	// Search holds the resolved retrieval knobs; nil fields take dpkms's
	// built-in defaults.
	Search FindSearch `json:"search,omitzero"`
	// Explain asks for a per-result score breakdown (hybrid mode only).
	Explain bool `json:"explain,omitempty"`
	// Facets asks for metadata type counts over the filtered objects.
	Facets bool `json:"facets,omitempty"`
}

// FindFilter is find's metadata facet filter. Since and Until are dates
// as YYYY-MM-DD.
type FindFilter struct {
	MetaType   string `json:"meta_type,omitempty"`
	Topic      string `json:"topic,omitempty"`
	Person     string `json:"person,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	Since      string `json:"since,omitempty"`
	Until      string `json:"until,omitempty"`
}

// FindSearch is the resolved search strategy. Nil fields take dpkms's
// built-in defaults, which equal ctxt's config defaults.
type FindSearch struct {
	RRFK                   *int     `json:"rrf_k,omitempty"`
	FTSWeight              *float64 `json:"fts_weight,omitempty"`
	VectorWeight           *float64 `json:"vector_weight,omitempty"`
	FTSPool                *int     `json:"fts_pool,omitempty"`
	VectorPool             *int     `json:"vector_pool,omitempty"`
	MinScore               *float64 `json:"min_score,omitempty"`
	FallbackToFTS          *bool    `json:"fallback_to_fts,omitempty"`
	MentionBoostPerMention *float64 `json:"mention_boost_per_mention,omitempty"`
	MaxMentionBoost        *float64 `json:"max_mention_boost,omitempty"`
	DirectBacklinkBoost    *float64 `json:"direct_backlink_boost,omitempty"`
	HopBacklinkBoost       *float64 `json:"hop_backlink_boost,omitempty"`
}

// FindResponse is the answer to POST /api/v1/find.
type FindResponse struct {
	Query string `json:"query"`
	// Mode is the mode that ran.
	Mode string `json:"mode"`
	// Objects are the results, best first.
	Objects []*storage.KnowledgeObject `json:"objects"`
	// Total is len(Objects).
	Total       int             `json:"total"`
	Diagnostics FindDiagnostics `json:"diagnostics"`
	// Explain has one entry per object, in Objects order, when explain
	// was requested in hybrid mode.
	Explain []FindExplanation `json:"explain,omitempty"`
	// Facets maps metadata type to object count, when requested.
	Facets map[string]int `json:"facets,omitempty"`
}

// FindDiagnostics describes what the search saw before and after the
// reranker threshold. Zero-valued, with no Semantic report, for fts.
type FindDiagnostics struct {
	CandidateCount         int                   `json:"candidate_count"`
	BelowThresholdCount    int                   `json:"below_threshold_count"`
	TopBelowThresholdScore float64               `json:"top_below_threshold_score,omitempty"`
	Threshold              float64               `json:"threshold"`
	StalenessWarning       *FindStalenessWarning `json:"staleness_warning,omitempty"`
	Semantic               *FindSemanticReport   `json:"semantic,omitempty"`
}

// FindStalenessWarning counts results pending a pipeline upgrade.
type FindStalenessWarning struct {
	Count  int    `json:"count"`
	Reason string `json:"reason"`
}

// FindSemanticReport says whether the semantic leg ran and, if not, why.
// Notice is the one-line operator message for a non-ok Status.
type FindSemanticReport struct {
	Status  string `json:"status"`
	ModelID string `json:"model_id,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Notice  string `json:"notice,omitempty"`
}

// OK reports whether the semantic leg ran.
func (r FindSemanticReport) OK() bool { return r.Status == "ok" }

// FindExplanation is the score breakdown of one result.
type FindExplanation struct {
	ID           string                       `json:"id"`
	Breakdown    FindScoreBreakdown           `json:"score_breakdown"`
	DocumentView pluginapi.DocumentProjection `json:"document_view"`
}

// FindScoreBreakdown is each signal's contribution to a result's score.
type FindScoreBreakdown struct {
	FTS            float64 `json:"fts"`
	Vector         float64 `json:"vector"`
	MentionBoost   float64 `json:"mention_boost"`
	GraphRelevance float64 `json:"graph_relevance"`
	WordOverlap    float64 `json:"word_overlap"`
	Total          float64 `json:"total"`
}

// Find runs req with POST /api/v1/find. dpkms embeds the query with its
// own configured provider. A malformed request, or a semantic leg that
// cannot run while req turns fallback_to_fts off, is a USAGE error
// (400 / 422); the *RemoteError it wraps carries the dpkms code
// (INVALID_REQUEST, SEMANTIC_UNAVAILABLE).
func (c *Client) Find(ctx context.Context, req FindRequest) (*FindResponse, error) {
	var out FindResponse
	if err := c.Post(ctx, "/api/v1/find", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
