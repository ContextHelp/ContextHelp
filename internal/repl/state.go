package repl

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// defaultQueryHistoryMax is the sliding-window size for query vectors.
const defaultQueryHistoryMax = 5

// queryHistoryWeights are applied from most-recent to oldest.
// Indices align with the tail of QueryHistory (len-1 = most recent).
var queryHistoryWeights = []float64{0.4, 0.3, 0.2, 0.1, 0.05}

// SessionState holds mutable per-session context for the REPL.
// It is not goroutine-safe; access only from the read loop goroutine.
type SessionState struct {
	// LastResults is the ordered result set from the most recent find/list command.
	LastResults []*storage.KnowledgeObject
	// ActiveProfile is the focus profile active for this session.
	ActiveProfile string
	// LastQuery is the raw text of the most recent query.
	LastQuery string
	// QueryHistory holds the last QueryHistoryMax embedding vectors for session-context
	// blending. Index 0 is oldest; last index is most recent.
	QueryHistory [][]float32
	// QueryModelID is the embedding model every QueryHistory vector belongs
	// to. Vectors from different models live in different spaces, so a
	// query under another model starts a fresh history.
	QueryModelID string
	// QueryHistoryMax is the sliding window size. Defaults to defaultQueryHistoryMax.
	QueryHistoryMax int
}

// SetResults replaces LastResults with objs and clears any stale index references.
func (s *SessionState) SetResults(objs []*storage.KnowledgeObject) {
	s.LastResults = objs
}

// ResolveIndex returns the 1-based nth result from LastResults.
// Returns an error if n is out of range.
func (s *SessionState) ResolveIndex(n int) (*storage.KnowledgeObject, error) {
	if n < 1 || n > len(s.LastResults) {
		return nil, fmt.Errorf("no result %d (have %d)", n, len(s.LastResults))
	}
	return s.LastResults[n-1], nil
}

// historyMax returns the effective window size (at least 1).
func (s *SessionState) historyMax() int {
	if s.QueryHistoryMax > 0 {
		return s.QueryHistoryMax
	}
	return defaultQueryHistoryMax
}

// PushQueryVector appends vec, embedded under modelID, to QueryHistory and
// trims to QueryHistoryMax. A modelID other than QueryModelID drops the
// existing history first: vectors of different models are never mixed.
func (s *SessionState) PushQueryVector(modelID string, vec []float32) {
	if modelID != s.QueryModelID {
		s.QueryHistory = nil
		s.QueryModelID = modelID
	}
	s.QueryHistory = append(s.QueryHistory, vec)
	max := s.historyMax()
	if len(s.QueryHistory) > max {
		s.QueryHistory = s.QueryHistory[len(s.QueryHistory)-max:]
	}
}

// SessionContextVector returns a weighted average of the query history vectors,
// biased toward the most recent entry, for a query embedded under modelID.
// Returns nil if fewer than 2 entries exist or the history belongs to a
// different model. The result is NOT normalized — callers blend and
// normalize as needed.
func (s *SessionState) SessionContextVector(modelID string) []float32 {
	if modelID != s.QueryModelID || len(s.QueryHistory) < 2 {
		return nil
	}

	dim := len(s.QueryHistory[0])
	if dim == 0 {
		return nil
	}

	out := make([]float32, dim)
	n := len(s.QueryHistory)

	var totalWeight float64
	for i := 0; i < n; i++ {
		// i=n-1 is most recent → weights[0]; i=0 is oldest → weights[n-1].
		wi := n - 1 - i // index into weights (0 = most recent)
		var w float64
		if wi < len(queryHistoryWeights) {
			w = queryHistoryWeights[wi]
		} else {
			w = queryHistoryWeights[len(queryHistoryWeights)-1]
		}
		vec := s.QueryHistory[i]
		if len(vec) != dim {
			continue
		}
		for j, x := range vec {
			out[j] += float32(w) * x
		}
		totalWeight += w
	}

	if totalWeight == 0 {
		return nil
	}
	// Scale to unit-weight sum.
	scale := float32(1.0 / totalWeight)
	for i := range out {
		out[i] *= scale
	}
	return out
}
