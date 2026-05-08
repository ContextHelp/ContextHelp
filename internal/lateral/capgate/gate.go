package capgate

import "sort"

// Scored is a candidate that has been scored and is ready for the cap gate.
// The gate consumes Scored slices, applies threshold + top-K per type, and
// returns the survivors.
type Scored struct {
	URL   string
	Type  string
	Score float64
}

// Config drives the cap gate's per-type thresholds and K caps. Singleton
// candidate types (e.g. owner_profile, sponsor_page) bypass K — they're
// always-or-never gated only by threshold.
type Config struct {
	ThresholdsByType map[string]float64
	CapKByType       map[string]int
	Singletons       map[string]bool
}

// Gate filters Scored candidates by threshold and per-type top-K cap.
type Gate struct{ cfg Config }

// NewGate constructs a Gate with the given config.
func NewGate(cfg Config) *Gate { return &Gate{cfg: cfg} }

// Filter returns the survivors. For each candidate type: drop everything
// below the type's threshold, then keep top-K by score (skipping the K cap
// for singleton types).
func (g *Gate) Filter(in []Scored) []Scored {
	byType := map[string][]Scored{}
	for _, s := range in {
		byType[s.Type] = append(byType[s.Type], s)
	}

	var out []Scored
	for typ, cands := range byType {
		thresh := g.cfg.ThresholdsByType[typ]
		passing := cands[:0]
		for _, c := range cands {
			if c.Score >= thresh {
				passing = append(passing, c)
			}
		}
		sort.SliceStable(passing, func(i, j int) bool { return passing[i].Score > passing[j].Score })
		if !g.cfg.Singletons[typ] {
			if k, ok := g.cfg.CapKByType[typ]; ok && len(passing) > k {
				passing = passing[:k]
			}
		}
		out = append(out, passing...)
	}
	return out
}
