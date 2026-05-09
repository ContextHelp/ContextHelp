package observe

import (
	"sort"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/eval"
)

// QualityWeights configure the relative contribution of offline
// precision and production conversion rate to the composite quality
// score. Weights are normalised inside Compute — callers don't need
// to ensure they sum to 1.
//
// DefaultQualityWeights mirrors the brief's 0.5 / 0.5 weighting.
// Operators tune via the daemon's quality config block once that
// lands; for now Compute is invoked with explicit weights.
type QualityWeights struct {
	Precision  float64
	Conversion float64
}

// DefaultQualityWeights returns the canonical 0.5 / 0.5 weights.
func DefaultQualityWeights() QualityWeights {
	return QualityWeights{Precision: 0.5, Conversion: 0.5}
}

// Quality is one strategy's composite quality readout. Score is the
// weighted average of Precision + Conversion. Sample carries through
// the precision Sample for context (small samples = low confidence).
type Quality struct {
	StrategyID string  `json:"strategy_id"`
	Precision  float64 `json:"precision"`
	Conversion float64 `json:"conversion"`
	Score      float64 `json:"score"`
	Sample     int     `json:"sample"`
}

// Compute joins an eval.Report (offline precision/recall) with a
// snapshot of conversion rates (from eval.ConversionAggregator) into
// a per-strategy Quality readout. Strategies present in only one of
// the inputs still get a Quality entry — the missing axis contributes
// 0 to the weighted average.
//
// The intent: the quality score is a ranking signal for which
// strategies need tuning. A high precision + low conversion strategy
// is doing the right thing (correctly claiming events) but isn't
// delivering useful candidates; a low precision + high conversion
// strategy is happenstance — it's claiming events it shouldn't and
// some of those happen to convert. Both deserve attention; the
// composite makes them equally visible.
func Compute(metrics eval.Report, conversion map[string]eval.ConversionRate, w QualityWeights) []Quality {
	wp, wc := w.Precision, w.Conversion
	denom := wp + wc
	if denom == 0 {
		// Avoid div-by-zero; default to even weighting when the
		// caller passed nonsense.
		wp, wc = 0.5, 0.5
		denom = 1
	}

	ids := map[string]struct{}{}
	for id := range metrics.Strategies {
		ids[id] = struct{}{}
	}
	for id := range conversion {
		ids[id] = struct{}{}
	}

	out := make([]Quality, 0, len(ids))
	for id := range ids {
		m := metrics.Strategies[id]
		c := conversion[id]
		score := (wp*m.Precision + wc*c.Rate) / denom
		out = append(out, Quality{
			StrategyID: id,
			Precision:  m.Precision,
			Conversion: c.Rate,
			Score:      score,
			Sample:     m.Sample,
		})
	}
	// Stable order so dashboards / CI diffs don't flap on map iteration.
	sort.Slice(out, func(i, j int) bool {
		return out[i].StrategyID < out[j].StrategyID
	})
	return out
}
