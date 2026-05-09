package lateral

import "context"

// Hints carries capture-pipeline-supplied attribution signals about the
// captured page. Strategies consume Hints when URL-shape alone doesn't
// identify the publishing platform — most commonly when a page is hosted
// on a publication's own custom domain rather than the platform's
// canonical host.
//
// The zero value disables hint-based detection. Strategies treat zero
// Hints as "no page-side signals available" and fall back to host-only
// matching, preserving pre-Hints behaviour.
//
// Hints lives on lateral.CapturedEvent so the substrate (not the
// per-platform strategy package) owns the type. Strategies that want
// platform-specific hint extraction read CapturedEvent.Hints and adapt
// to their own input shape.
type Hints struct {
	// Generator is the value of <meta name="generator"> when present.
	// Substack and Beehiiv emit recognisable strings here on
	// custom-domain pages.
	Generator string

	// MetaPlatform is an explicit platform identifier when the capture
	// pipeline has already attributed the page (e.g. via JS SDK probes,
	// installed publishing scripts, or a manual override).
	MetaPlatform string

	// CanonicalHost is the host extracted from the page's rel=canonical
	// link. Custom-domain Substack publications point rel=canonical at
	// *.substack.com; same for Beehiiv. Apex/empty when absent.
	CanonicalHost string
}

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
	// Hints are capture-pipeline-supplied page-side signals (meta
	// generator, explicit platform tag, rel=canonical host) that
	// strategies consume to refine platform attribution. Zero value =
	// no signals available; strategies fall back to host-only matching.
	Hints Hints
}

// Candidate is a strategy's raw output. Identity resolution + cap gate run
// on the candidate set before any are materialized.
type Candidate struct {
	URL           string
	CandidateType string         // sibling_repo, owner_profile, sponsor_page, etc.
	Strategy      string         // strategy ID that produced this candidate
	Preview       map[string]any // strategy-supplied preview fields (title, description, stars, etc.) shown pre-promotion; not source of truth
	// IdentityKey is the canonical opaque key the resolver uses to dedup
	// candidates across captures. Strategies emit this directly via
	// identitykey.Build / identitykey.BuildLocalised; the resolver reads
	// the typed field first and falls back to Preview[identity_key] for
	// one minor cycle (T-0307 / T-0309). Empty when the strategy has no
	// canonical id, in which case the resolver falls back to URL match.
	IdentityKey string
}

// ActiveContext is the blended-scoring input passed to Probe(). Strategies
// MAY pre-filter sub-paths based on active context to save fetch budget.
type ActiveContext struct {
	SessionTopic     map[string]float64 // tag -> weight; nil if no live session
	CaptureWindow    map[string]float64 // 30d aggregate; always present once captures exist
	InterestRegistry map[string]float64 // active interests
	Fingerprint      string             // hash for triage caching
	// AuthorHints carries platform-keyed sticky author identifiers
	// resolved by capture-pipeline session middleware. Keys are platform
	// IDs ("github", "x", "linkedin", ...); values are the
	// platform-specific author identifier (github login, x username,
	// etc.). Strategies consult AuthorHints to unlock author-scoped
	// probes (e.g. github author_other_pr / author_other_issue) without
	// having to derive the author from the captured URL.
	//
	// Nil / missing key = no author hint for that platform; strategies
	// MUST treat that as "skip the author-scoped probe."
	AuthorHints map[string]string
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
