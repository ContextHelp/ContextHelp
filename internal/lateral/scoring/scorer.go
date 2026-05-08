package scoring

import (
	"context"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Eva is the subset of the eva library that scoring needs: a single Cosine
// similarity call between two tag-weight maps.
type Eva interface {
	Cosine(ctx context.Context, a, b map[string]float64) (float64, error)
}

// Scorer blends per-signal sub-scores into a single 0..1 score using the
// configured Weights. Missing signals are excluded and their weight is
// redistributed proportionally across the survivors.
type Scorer struct {
	eva     Eva
	weights Weights
}

// ScoreResult is the auditable output of one scoring pass: the blended
// score, which signals contributed, and the weights actually applied after
// redistribution.
type ScoreResult struct {
	Score          float64
	SignalsUsed    []string
	WeightsApplied map[string]float64
}

// NewScorer wires a Scorer to its eva backend and weight configuration.
func NewScorer(eva Eva, w Weights) *Scorer {
	return &Scorer{eva: eva, weights: w}
}

// Score returns the blended score for c against ac. Returns an error only if
// eva itself errors; missing signals are not errors.
func (s *Scorer) Score(ctx context.Context, c lateral.Candidate, ac lateral.ActiveContext) (ScoreResult, error) {
	candVec := candidateVector(c)
	subscores := map[string]float64{}
	if ac.SessionTopic != nil {
		v, err := s.eva.Cosine(ctx, candVec, ac.SessionTopic)
		if err != nil {
			return ScoreResult{}, err
		}
		subscores["session_topic"] = v
	}
	if ac.CaptureWindow != nil {
		v, err := s.eva.Cosine(ctx, candVec, ac.CaptureWindow)
		if err != nil {
			return ScoreResult{}, err
		}
		subscores["capture_window"] = v
	}
	if ac.InterestRegistry != nil {
		v, err := s.eva.Cosine(ctx, candVec, ac.InterestRegistry)
		if err != nil {
			return ScoreResult{}, err
		}
		subscores["interest_registry"] = v
	}
	weights := s.redistribute(subscores)
	var total float64
	for k, v := range subscores {
		total += v * weights[k]
	}
	signals := make([]string, 0, len(subscores))
	for k := range subscores {
		signals = append(signals, k)
	}
	return ScoreResult{Score: total, SignalsUsed: signals, WeightsApplied: weights}, nil
}

// redistribute rescales weights for surviving signals so they sum to 1.0.
func (s *Scorer) redistribute(present map[string]float64) map[string]float64 {
	base := map[string]float64{
		"session_topic":     s.weights.SessionTopic,
		"capture_window":    s.weights.CaptureWindow,
		"interest_registry": s.weights.InterestRegistry,
	}
	var sum float64
	for k := range present {
		sum += base[k]
	}
	out := map[string]float64{}
	for k := range present {
		out[k] = base[k] / sum
	}
	return out
}

// candidateVector turns a Candidate's preview into a tag-weight map. Currently
// extracts the "topics" field if present; richer extraction lands as later
// strategies surface more preview fields.
func candidateVector(c lateral.Candidate) map[string]float64 {
	vec := map[string]float64{}
	if topics, ok := c.Preview["topics"].([]string); ok {
		for _, t := range topics {
			vec[t] = 1.0
		}
	}
	return vec
}
