package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Find modes.
const (
	FindModeFTS    = "fts"
	FindModeVector = "vector"
	FindModeHybrid = "hybrid"
)

// DefaultFindLimit is the result limit of a find request that sets none.
const DefaultFindLimit = 10

// findDateLayout is the layout of FindFilter's Since and Until.
const findDateLayout = "2006-01-02"

// ErrInvalidFind marks a find request dpkms refuses as malformed. The
// wrapping error says which field is wrong.
var ErrInvalidFind = errors.New("invalid find request")

// SemanticUnavailableError is returned when the semantic leg cannot run
// and fallback_to_fts is off, so the search fails instead of degrading to
// full-text. Report says why and names the model when there is one.
type SemanticUnavailableError struct {
	Report retrieval.SemanticReport
}

func (e *SemanticUnavailableError) Error() string {
	return fmt.Sprintf("semantic search unavailable (%s) and fallback_to_fts is false: %s", e.Report.Status, e.Report.Detail)
}

// FindRequest is the body of POST /api/v1/find. The caller resolves its
// profile and search strategy and sends the values (ADR-077 §6); dpkms
// does not read a second copy of that configuration.
type FindRequest struct {
	// Query is the search text. Required.
	Query string `json:"query"`
	// Mode is fts, vector or hybrid; empty means hybrid.
	Mode string `json:"mode,omitempty"`
	// Limit caps the result list; 0 means DefaultFindLimit.
	Limit int `json:"limit,omitempty"`
	// Profile restricts every leg, and the facet counts, to objects
	// owned by that profile; empty means no profile filter, as for the
	// object listing. It scopes a search; it is not access control.
	Profile string `json:"profile,omitempty"`
	// Filter narrows every leg to objects matching its metadata facets.
	Filter FindFilter `json:"filter,omitzero"`
	// Search holds the resolved retrieval knobs. A knob left out takes
	// its built-in default (config.DefaultSearchConfig).
	Search FindSearch `json:"search,omitzero"`
	// Explain adds a per-result score breakdown. It applies to hybrid
	// mode only; other modes ignore it.
	Explain bool `json:"explain,omitempty"`
	// Facets adds metadata type counts over the objects Filter matches.
	Facets bool `json:"facets,omitempty"`
}

// FindFilter is find's metadata facet filter. Since and Until are dates
// (YYYY-MM-DD) compared with an object's dates_mentioned.
type FindFilter struct {
	MetaType   string `json:"meta_type,omitempty"`
	Topic      string `json:"topic,omitempty"`
	Person     string `json:"person,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	Since      string `json:"since,omitempty"`
	Until      string `json:"until,omitempty"`
}

// FindSearch is the resolved search strategy a find request carries. Nil
// fields take the built-in default.
type FindSearch struct {
	// RRFK is the Reciprocal Rank Fusion rank constant.
	RRFK *int `json:"rrf_k,omitempty"`
	// FTSWeight and VectorWeight weight each leg's RRF contribution.
	FTSWeight    *float64 `json:"fts_weight,omitempty"`
	VectorWeight *float64 `json:"vector_weight,omitempty"`
	// FTSPool and VectorPool are each leg's candidate pool size.
	FTSPool    *int `json:"fts_pool,omitempty"`
	VectorPool *int `json:"vector_pool,omitempty"`
	// MinScore drops hybrid results scoring below it.
	MinScore *float64 `json:"min_score,omitempty"`
	// FallbackToFTS, when false, fails a vector or hybrid search whose
	// semantic leg cannot run instead of answering full-text only.
	FallbackToFTS *bool `json:"fallback_to_fts,omitempty"`
	// The reranker's additive signal weights.
	MentionBoostPerMention *float64 `json:"mention_boost_per_mention,omitempty"`
	MaxMentionBoost        *float64 `json:"max_mention_boost,omitempty"`
	DirectBacklinkBoost    *float64 `json:"direct_backlink_boost,omitempty"`
	HopBacklinkBoost       *float64 `json:"hop_backlink_boost,omitempty"`
}

// FindResult is the response of POST /api/v1/find.
type FindResult struct {
	Query string `json:"query"`
	// Mode is the mode that ran.
	Mode string `json:"mode"`
	// Objects are the results, best first. Never null.
	Objects []*storage.KnowledgeObject `json:"objects"`
	// Total is len(Objects).
	Total int `json:"total"`
	// Diagnostics reports candidate counts, the threshold, staleness and
	// the semantic leg. Zero-valued, with no semantic report, for fts.
	Diagnostics SearchDiagnostics `json:"diagnostics"`
	// Explain holds one score breakdown per object, in Objects order,
	// when explain was requested in hybrid mode.
	Explain []FindExplanation `json:"explain,omitempty"`
	// Facets maps metadata type to object count, when requested.
	Facets map[string]int `json:"facets,omitempty"`
}

// FindExplanation is the score breakdown of one find result.
type FindExplanation struct {
	ID           string                       `json:"id"`
	Breakdown    ScoreBreakdown               `json:"score_breakdown"`
	DocumentView pluginapi.DocumentProjection `json:"document_view"`
}

// Find runs a find request. sem reaches the default embedding model's
// index for vector and hybrid modes; its provider embeds the query here,
// where dpkms runs. A malformed request is an ErrInvalidFind; a semantic
// leg that cannot run with fallback_to_fts off is a
// *SemanticUnavailableError.
func (s *Service) Find(ctx context.Context, req FindRequest, sem retrieval.SemanticSource) (*FindResult, error) {
	mode, filter, cfg, err := resolveFind(req)
	if err != nil {
		return nil, err
	}
	res := &FindResult{Query: req.Query, Mode: mode}

	switch {
	case mode == FindModeHybrid && req.Explain:
		env, err := s.HybridSearchExplainFilteredWithDiagnostics(ctx, req.Query, filter, sem, cfg)
		if err != nil {
			return nil, err
		}
		res.Diagnostics = env.Diagnostics
		res.Objects = make([]*storage.KnowledgeObject, len(env.Results))
		res.Explain = make([]FindExplanation, len(env.Results))
		for i, r := range env.Results {
			res.Objects[i] = r.Object
			res.Explain[i] = FindExplanation{ID: r.Object.ID, Breakdown: r.Breakdown, DocumentView: r.DocumentView}
		}
	case mode == FindModeHybrid:
		res.Objects, res.Diagnostics, err = s.HybridSearchFilteredWithDiagnostics(ctx, req.Query, filter, sem, cfg)
	case mode == FindModeVector:
		res.Objects, res.Diagnostics, err = s.SemanticSearchFiltered(ctx, req.Query, filter, sem, cfg)
	default: // fts: diagnostics stay zero, with no semantic report.
		res.Objects, err = s.FindByTextFiltered(ctx, req.Query, filter)
	}
	if err != nil {
		return nil, err
	}
	if res.Objects == nil {
		res.Objects = []*storage.KnowledgeObject{}
	}
	res.Total = len(res.Objects)

	if req.Facets {
		if res.Facets, err = s.FacetCounts(ctx, filter); err != nil {
			return nil, fmt.Errorf("find facets: %w", err)
		}
	}
	return res, nil
}

// resolveFind validates req and resolves its mode, filter and search
// config.
func resolveFind(req FindRequest) (string, storage.ObjectFilter, config.SearchConfig, error) {
	var zero storage.ObjectFilter
	invalid := func(format string, args ...any) (string, storage.ObjectFilter, config.SearchConfig, error) {
		return "", zero, config.SearchConfig{}, fmt.Errorf("%w: %s", ErrInvalidFind, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(req.Query) == "" {
		return invalid("query is required")
	}
	mode := req.Mode
	switch mode {
	case "":
		mode = FindModeHybrid
	case FindModeFTS, FindModeVector, FindModeHybrid:
	default:
		return invalid("mode %q: want fts, vector or hybrid", req.Mode)
	}
	if req.Limit < 0 {
		return invalid("limit must not be negative")
	}
	filter := storage.ObjectFilter{
		Limit:          req.Limit,
		ProfileID:      req.Profile,
		MetadataType:   req.Filter.MetaType,
		MetadataTopic:  req.Filter.Topic,
		MetadataPerson: req.Filter.Person,
		SourceType:     req.Filter.SourceType,
	}
	if filter.Limit == 0 {
		filter.Limit = DefaultFindLimit
	}
	for _, d := range []struct {
		name, value string
		dest        **time.Time
	}{{"filter.since", req.Filter.Since, &filter.MetadataSince}, {"filter.until", req.Filter.Until, &filter.MetadataUntil}} {
		if d.value == "" {
			continue
		}
		t, err := time.Parse(findDateLayout, d.value)
		if err != nil {
			return invalid("%s %q: want a date as YYYY-MM-DD", d.name, d.value)
		}
		*d.dest = &t
	}
	cfg, err := req.Search.apply(config.DefaultSearchConfig())
	if err != nil {
		return invalid("%v", err)
	}
	return mode, filter, cfg, nil
}

// apply overlays the set knobs on base and validates them.
func (k FindSearch) apply(base config.SearchConfig) (config.SearchConfig, error) {
	out := base
	for _, p := range []struct {
		name string
		v    *int
		dest *int
	}{
		{"search.rrf_k", k.RRFK, &out.RRF.K},
		{"search.fts_pool", k.FTSPool, &out.CandidatePool.FTS},
		{"search.vector_pool", k.VectorPool, &out.CandidatePool.Vector},
	} {
		if p.v == nil {
			continue
		}
		if *p.v <= 0 {
			return out, fmt.Errorf("%s must be positive", p.name)
		}
		*p.dest = *p.v
	}
	for _, p := range []struct {
		name string
		v    *float64
		dest *float64
	}{
		{"search.fts_weight", k.FTSWeight, &out.RRF.FTSWeight},
		{"search.vector_weight", k.VectorWeight, &out.RRF.VectorWeight},
		{"search.min_score", k.MinScore, &out.MinScore},
		{"search.mention_boost_per_mention", k.MentionBoostPerMention, &out.Reranker.MentionBoostPerMention},
		{"search.max_mention_boost", k.MaxMentionBoost, &out.Reranker.MaxMentionBoost},
		{"search.direct_backlink_boost", k.DirectBacklinkBoost, &out.Reranker.DirectBacklinkBoost},
		{"search.hop_backlink_boost", k.HopBacklinkBoost, &out.Reranker.HopBacklinkBoost},
	} {
		if p.v == nil {
			continue
		}
		if *p.v < 0 {
			return out, fmt.Errorf("%s must not be negative", p.name)
		}
		*p.dest = *p.v
	}
	if k.FallbackToFTS != nil {
		out.FallbackToFTS = *k.FallbackToFTS
	}
	return out, nil
}
