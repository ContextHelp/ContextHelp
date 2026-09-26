package service

import (
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/ranking"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SearchMode reports which retrieval legs a hybrid search actually ran.
type SearchMode string

const (
	// SearchModeHybrid: FTS and vector legs both ran.
	SearchModeHybrid SearchMode = "hybrid"
	// SearchModeFTSOnly: no default embedding model; FTS leg only
	// (fallback_to_fts).
	SearchModeFTSOnly SearchMode = "fts_only"
	// SearchModeFTSFallback: the default model's vector leg could not run
	// (provider error, dimension mismatch, missing index, no coverage) and
	// search degraded to FTS (fallback_to_fts). SearchTrace.VectorError
	// holds the reason.
	SearchModeFTSFallback SearchMode = "fts_fallback"
)

// TraceLegs names the retrieval legs a candidate surfaced from.
type TraceLegs string

const (
	TraceLegsFTS    TraceLegs = "fts"
	TraceLegsVector TraceLegs = "vector"
	TraceLegsBoth   TraceLegs = "both"
)

// TraceStage is the furthest pipeline stage a scored candidate reached.
type TraceStage string

const (
	// TraceStageReturned: candidate is in the result list.
	TraceStageReturned TraceStage = "returned"
	// TraceStageCutLimit: candidate cleared the threshold but ranked past
	// the result limit.
	TraceStageCutLimit TraceStage = "cut_limit"
	// TraceStageCutThreshold: candidate's total score fell strictly below
	// the MinScore threshold.
	TraceStageCutThreshold TraceStage = "cut_threshold"
)

// SearchTrace records every candidate a hybrid search scored and why each
// was kept or dropped. Populated only by HybridSearchExplainFilteredWithTrace.
type SearchTrace struct {
	// Query is the caller's query text.
	Query string `json:"query"`
	// FTSQuery is the alias-expanded text sent to the FTS leg.
	FTSQuery string `json:"fts_query"`
	// Mode reports which legs ran.
	Mode SearchMode `json:"mode"`
	// VectorError is the vector-leg error when Mode is fts_fallback.
	VectorError string `json:"vector_error,omitempty"`
	// VectorModel is the default embedding model whose index the vector
	// leg read; empty when there is no default model.
	VectorModel string `json:"vector_model,omitempty"`
	// SemanticStatus is the vector leg's retrieval.SemanticStatus (ok, or
	// why the leg contributed no hits).
	SemanticStatus retrieval.SemanticStatus `json:"semantic_status,omitempty"`
	// FTSPool is the effective FTS candidate pool size.
	FTSPool int `json:"fts_pool"`
	// VectorPool is the effective vector candidate pool size; 0 when the
	// vector leg did not run (no default embedding model).
	VectorPool int `json:"vector_pool"`
	// RRFK is the effective Reciprocal Rank Fusion k constant.
	RRFK int `json:"rrf_k"`
	// FTSWeight and VectorWeight scale each leg's RRF contribution.
	FTSWeight    float64 `json:"fts_weight"`
	VectorWeight float64 `json:"vector_weight"`
	// Weights are the effective reranker weights.
	Weights TraceRerankWeights `json:"weights"`
	// Threshold is the MinScore cut applied to total scores.
	Threshold float64 `json:"threshold"`
	// Limit is the result-count cap; 0 means unlimited.
	Limit int `json:"limit"`
	// Counts summarises legs and stages.
	Counts SearchTraceCounts `json:"counts"`
	// Candidates holds every scored candidate in final rank order.
	Candidates []TraceCandidate `json:"candidates"`
}

// TraceRerankWeights mirrors the reranker weights in effect for a search.
type TraceRerankWeights struct {
	MentionBoost    float64 `json:"mention_boost"`
	MaxMentionBoost float64 `json:"max_mention_boost"`
	DirectBacklink  float64 `json:"direct_backlink"`
	HopBacklink     float64 `json:"hop_backlink"`
	WordOverlap     float64 `json:"word_overlap"`
}

// SearchTraceCounts tallies candidates per leg and per stage.
type SearchTraceCounts struct {
	// FTSHits and VectorHits count unique objects each leg returned.
	FTSHits    int `json:"fts_hits"`
	VectorHits int `json:"vector_hits"`
	// Candidates is the unique merged candidate count.
	Candidates int `json:"candidates"`
	Both       int `json:"both"`
	FTSOnly    int `json:"fts_only"`
	VectorOnly int `json:"vector_only"`
	// Returned + CutLimit + CutThreshold == Candidates.
	Returned     int `json:"returned"`
	CutLimit     int `json:"cut_limit"`
	CutThreshold int `json:"cut_threshold"`
}

// TraceCandidate is one scored candidate with its per-leg provenance.
type TraceCandidate struct {
	ID string `json:"id"`
	// Object is the candidate itself, for in-process consumers. Omitted
	// from JSON to keep the trace compact; join on ID when needed.
	Object *storage.KnowledgeObject `json:"-"`
	Legs   TraceLegs                `json:"legs"`
	// FTSRank and VectorRank are 1-based positions in each leg's result
	// list; 0 when absent from that leg.
	FTSRank    int `json:"fts_rank,omitempty"`
	VectorRank int `json:"vector_rank,omitempty"`
	// FTSRaw is the driver's raw full-text score (SQLite: bm25, lower is
	// better; Postgres: ts_rank_cd, higher is better). Nil when absent.
	FTSRaw *float64 `json:"fts_raw,omitempty"`
	// VectorRaw is the cosine similarity (1 - cosine distance) of the
	// object's closest chunk under the default model. Nil when absent from
	// the vector leg.
	VectorRaw *float64 `json:"vector_raw,omitempty"`
	// RRF is the fused leg score before reranking (FTS + Vector).
	RRF float64 `json:"rrf"`
	// Breakdown is the same per-signal breakdown results report.
	Breakdown ScoreBreakdown `json:"score_breakdown"`
	// Rank is the 1-based position in the full reranked order.
	Rank  int        `json:"rank"`
	Stage TraceStage `json:"stage"`
}

// searchTraceInput carries the effective parameters and intermediate
// state of one hybrid search into buildSearchTrace.
type searchTraceInput struct {
	query, ftsQuery  string
	semantic         retrieval.SemanticReport // vector-leg report; non-ok tolerated via fallback_to_fts
	ftsPool, vecPool int
	rrf              config.RRFConfig
	k                int
	weights          ranking.WeightConfig
	threshold        float64
	limit            int // caller's filter.Limit
	kept             int // results actually returned
	scored           []ranking.Result
	ftsLeg, vecLeg   []*storage.KnowledgeObject
}

// buildSearchTrace assembles the trace header and per-candidate rows.
func buildSearchTrace(in searchTraceInput) *SearchTrace {
	tr := &SearchTrace{
		Query:          in.query,
		FTSQuery:       in.ftsQuery,
		Mode:           SearchModeHybrid,
		VectorModel:    in.semantic.ModelID,
		SemanticStatus: in.semantic.Status,
		FTSPool:        in.ftsPool,
		VectorPool:     in.vecPool,
		RRFK:           in.k,
		FTSWeight:      in.rrf.FTSWeight,
		VectorWeight:   in.rrf.VectorWeight,
		Weights: TraceRerankWeights{
			MentionBoost:    in.weights.MentionBoost,
			MaxMentionBoost: in.weights.MaxMentionBoost,
			DirectBacklink:  in.weights.DirectBacklink,
			HopBacklink:     in.weights.HopBacklink,
			WordOverlap:     in.weights.WordOverlapWeight,
		},
		Threshold: in.threshold,
		Limit:     max(in.limit, 0),
	}
	switch st := in.semantic.Status; {
	case st == retrieval.SemanticNoDefaultModel:
		tr.Mode = SearchModeFTSOnly
		tr.VectorPool = 0
	case st != retrieval.SemanticOK:
		tr.Mode = SearchModeFTSFallback
		tr.VectorError = in.semantic.Detail
	}
	fillTraceCandidates(tr, in.scored, in.threshold, in.kept,
		recordLegHits(in.ftsLeg, "fts_score"),
		recordLegHits(in.vecLeg, "score"))
	return tr
}

// legHit captures a candidate's position and raw score in one leg.
type legHit struct {
	rank int
	raw  *float64
}

// recordLegHits maps each object ID to its first (best) position in a leg
// result list, reading the raw score from Metadata[scoreKey].
func recordLegHits(results []*storage.KnowledgeObject, scoreKey string) map[string]legHit {
	hits := make(map[string]legHit, len(results))
	for i, obj := range results {
		if _, seen := hits[obj.ID]; seen {
			continue
		}
		h := legHit{rank: i + 1}
		if v, ok := obj.Metadata[scoreKey].(float64); ok {
			h.raw = &v
		}
		hits[obj.ID] = h
	}
	return hits
}

// breakdownOf converts a reranker result into the public score breakdown.
func breakdownOf(r ranking.Result) ScoreBreakdown {
	return ScoreBreakdown{
		FTS:            r.FTS,
		Vector:         r.Vector,
		MentionBoost:   r.MentionBoost,
		GraphRelevance: r.GraphRelevance,
		WordOverlap:    r.WordOverlap,
		Total:          r.Total,
	}
}

// fillTraceCandidates classifies every scored result (sorted descending by
// total) into a stage using the same rules as the result list: below
// threshold → cut_threshold; above threshold, past limit → cut_limit.
// limit is the number of above-threshold results actually returned.
func fillTraceCandidates(tr *SearchTrace, scored []ranking.Result, threshold float64, limit int, fts, vec map[string]legHit) {
	tr.Candidates = make([]TraceCandidate, len(scored))
	c := &tr.Counts
	c.FTSHits = len(fts)
	c.VectorHits = len(vec)
	c.Candidates = len(scored)
	kept := 0
	for i, r := range scored {
		id := r.Object.ID
		tc := TraceCandidate{
			ID:        id,
			Object:    r.Object,
			RRF:       r.FTS + r.Vector,
			Breakdown: breakdownOf(r),
			Rank:      i + 1,
		}
		fh, inFTS := fts[id]
		vh, inVec := vec[id]
		if inFTS {
			tc.FTSRank, tc.FTSRaw = fh.rank, fh.raw
		}
		if inVec {
			tc.VectorRank, tc.VectorRaw = vh.rank, vh.raw
		}
		switch {
		case inFTS && inVec:
			tc.Legs = TraceLegsBoth
			c.Both++
		case inVec:
			tc.Legs = TraceLegsVector
			c.VectorOnly++
		default:
			tc.Legs = TraceLegsFTS
			c.FTSOnly++
		}
		switch {
		case r.Total < threshold:
			tc.Stage = TraceStageCutThreshold
			c.CutThreshold++
		case kept < limit:
			tc.Stage = TraceStageReturned
			c.Returned++
			kept++
		default:
			tc.Stage = TraceStageCutLimit
			c.CutLimit++
		}
		tr.Candidates[i] = tc
	}
}
