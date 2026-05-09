package eval

import (
	"fmt"
	"sort"
	"strings"
)

// Metrics aggregates a single strategy's eval scores against the
// labelled fixture set. Precision = (correct positives that hit the
// strategy) / (all events the strategy claimed). Recall = (correct
// positives that hit the strategy) / (all events labelled with the
// strategy).
//
// Sample is the count of events that contributed to the calculation
// (fixtures whose Expect.StrategyID = the strategy ID for recall;
// fixtures the strategy actually dispatched on for precision).
//
// NegativePass is the count of negative fixtures (Expect.Negative=true)
// the strategy correctly ignored, divided by total negative fixtures.
// 1.0 = "never claims a negative"; <1.0 means the strategy is firing
// on URLs operators have labelled out.
type Metrics struct {
	StrategyID   string  `json:"strategy_id"`
	Precision    float64 `json:"precision"`
	Recall       float64 `json:"recall"`
	NegativePass float64 `json:"negative_pass"`
	Sample       int     `json:"sample"`
}

// Report bundles per-strategy metrics plus dataset-level totals. The
// CI workflow consumes the JSON form for baseline comparisons; the
// CLI consumes RenderMetricsTable for human inspection.
type Report struct {
	Strategies map[string]Metrics `json:"strategies"`
	// Total* are dataset-level convenience counts the CI baseline
	// diff consumes to express coverage at a glance.
	TotalFixtures int `json:"total_fixtures"`
	TotalPositive int `json:"total_positive"`
	TotalNegative int `json:"total_negative"`
}

// ComputeMetrics derives precision / recall / negative-pass per
// strategy from a ReplayReport. The set of strategies considered is
// the union of (a) every Expect.StrategyID across fixtures and (b)
// every strategy that actually claimed at least one event.
//
// Definition of "correct claim": for a positive fixture, the event's
// dispatched strategy set contains Expect.StrategyID. The harness
// doesn't currently match individual candidate URLs against
// Expect.CandidateURLs at the metric level — that's a follow-on slice
// once Probe outputs are consistently stable across runs (URL-only
// roster strategies suffice for now). Today the metric measures
// dispatch accuracy, which is the dominant precision/recall axis for
// the substrate.
func ComputeMetrics(report ReplayReport) Report {
	out := Report{Strategies: map[string]Metrics{}}

	// Pass 1: per-strategy counters.
	type counters struct {
		labelled        int // fixtures labelled with this strategy
		correctlyHit    int // fixtures labelled + claimed by it
		claimedTotal    int // fixtures claimed by this strategy (positive + negative + unlabelled)
		claimedNegative int // negative fixtures the strategy wrongly claimed
	}
	c := map[string]*counters{}
	getC := func(id string) *counters {
		if v, ok := c[id]; ok {
			return v
		}
		v := &counters{}
		c[id] = v
		return v
	}

	totalNeg := 0
	totalPos := 0
	negativePassPerStrategy := map[string]int{} // id → count of negatives correctly ignored

	for _, r := range report.Results {
		out.TotalFixtures++
		isNeg := r.Fixture.Expect.Negative
		if isNeg {
			totalNeg++
		} else if r.Fixture.Expect.StrategyID != "" {
			totalPos++
			getC(r.Fixture.Expect.StrategyID).labelled++
		}

		claimed := map[string]bool{}
		for _, sid := range r.Strategies {
			claimed[sid] = true
			getC(sid).claimedTotal++
		}

		if isNeg {
			// Update per-strategy "did this strategy ignore the
			// negative?" tally.
			seen := map[string]bool{}
			for sid := range claimed {
				getC(sid).claimedNegative++
				seen[sid] = true
			}
			// Also count the no-claim case for every strategy that
			// participated in this run; we resolve that below by
			// pivoting against c's keys after the loop.
			_ = seen
		} else {
			if expect := r.Fixture.Expect.StrategyID; expect != "" {
				if claimed[expect] {
					getC(expect).correctlyHit++
				}
			}
		}
	}

	// Pivot negative-pass: a strategy "passes" a negative fixture by
	// not claiming it. Per-strategy pass = (totalNeg - claimedNegative) / totalNeg.
	// Strategies that never appeared at all on any negative fixture
	// trivially pass; we record this so unrecognised strategies don't
	// sink the per-strategy metric.
	for id := range c {
		negativePassPerStrategy[id] = totalNeg - c[id].claimedNegative
	}

	out.TotalPositive = totalPos
	out.TotalNegative = totalNeg

	// Materialise Metrics per strategy.
	for id, ctr := range c {
		m := Metrics{StrategyID: id}
		m.Sample = ctr.labelled
		if ctr.claimedTotal > 0 {
			m.Precision = float64(ctr.correctlyHit) / float64(ctr.claimedTotal)
		}
		if ctr.labelled > 0 {
			m.Recall = float64(ctr.correctlyHit) / float64(ctr.labelled)
		}
		if totalNeg > 0 {
			m.NegativePass = float64(negativePassPerStrategy[id]) / float64(totalNeg)
		} else {
			m.NegativePass = 1.0
		}
		out.Strategies[id] = m
	}

	return out
}

// RenderMetricsTable formats metrics as a fixed-column table. The
// columns are aligned to the longest strategy ID so wider IDs (e.g.
// "SubstackPublicationStrategy") still render cleanly.
func RenderMetricsTable(r Report) string {
	if len(r.Strategies) == 0 {
		return "no strategies in report\n"
	}
	ids := make([]string, 0, len(r.Strategies))
	maxID := len("strategy")
	for id := range r.Strategies {
		ids = append(ids, id)
		if len(id) > maxID {
			maxID = len(id)
		}
	}
	sort.Strings(ids)

	var b strings.Builder
	const colP = 10 // precision width
	const colR = 8  // recall
	const colN = 8  // negative_pass
	const colS = 6  // sample

	fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %-*s  %-*s\n",
		maxID, "strategy",
		colP, "precision",
		colR, "recall",
		colN, "neg_pass",
		colS, "sample")
	fmt.Fprintf(&b, "%s  %s  %s  %s  %s\n",
		strings.Repeat("-", maxID),
		strings.Repeat("-", colP),
		strings.Repeat("-", colR),
		strings.Repeat("-", colN),
		strings.Repeat("-", colS))
	for _, id := range ids {
		m := r.Strategies[id]
		fmt.Fprintf(&b, "%-*s  %-*.2f  %-*.2f  %-*.2f  %-*d\n",
			maxID, id,
			colP, m.Precision,
			colR, m.Recall,
			colN, m.NegativePass,
			colS, m.Sample)
	}
	fmt.Fprintf(&b, "\n%d fixtures (%d positive / %d negative)\n",
		r.TotalFixtures, r.TotalPositive, r.TotalNegative)
	return b.String()
}
