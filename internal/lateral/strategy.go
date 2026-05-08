package lateral

import "context"

// StrategyFamily distinguishes platform-keyed strategies (mutually exclusive
// within a platform tree) from shape-keyed strategies (parallel-firing).
type StrategyFamily int

const (
	FamilyPlatform StrategyFamily = iota
	FamilyShape
	FamilyJIT
)

// AppliesResult is what a strategy returns from Applies(). Matches indicates
// whether the strategy claims the captured event; Specificity orders matching
// strategies within the platform family (children shadow parents).
type AppliesResult struct {
	Matches     bool
	Specificity int
}

// CapturedEvent is the input to a strategy. It carries the parent object's
// ID, namespace, source URL, and the originating capture pipeline. The full
// parent record is read from the canonical store separately.
type CapturedEvent struct {
	ObjectID        string
	Namespace       string
	SourceURL       string
	CapturePipeline string
	PersistedAt     int64 // unix nanos; zero if from .captured fallback
}

// Candidate is a strategy's raw output. Identity resolution + cap gate run
// on the candidate set before any are materialized.
type Candidate struct {
	URL           string
	CandidateType string         // sibling_repo, owner_profile, sponsor_page, etc.
	Strategy      string         // strategy ID that produced this candidate
	Preview       map[string]any // strategy-supplied preview fields (title, description, stars, etc.) shown pre-promotion; not source of truth
}

// ActiveContext is the blended-scoring input passed to Probe(). Strategies
// MAY pre-filter sub-paths based on active context to save fetch budget.
type ActiveContext struct {
	SessionTopic     map[string]float64 // tag -> weight; nil if no live session
	CaptureWindow    map[string]float64 // 30d aggregate; always present once captures exist
	InterestRegistry map[string]float64 // active interests
	Fingerprint      string             // hash for triage caching
}

// LateralStrategy is the per-source-or-shape contract. Implementations live
// under internal/lateral/strategies/<id>/.
type LateralStrategy interface {
	ID() string
	Family() StrategyFamily
	Applies(ctx context.Context, ev CapturedEvent) AppliesResult
	Probe(ctx context.Context, ev CapturedEvent, ac ActiveContext) ([]Candidate, error)
	Preconditions() []string // namespace fields that must be non-nil on parent record
}
