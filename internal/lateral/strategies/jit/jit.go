// Package jit implements the catch-all FamilyJIT strategy. It fires only when
// no Tier-A platform/shape strategy claims a CapturedEvent (see Registry.Dispatch).
//
// The strategy's Probe runs a Pipeline that asks a Proposer for sub-paths,
// dispatches them through a Fetcher, and emits candidates + failure/outage
// events. All collaborators are host-wired at construction time:
//   - Proposer  → kit/ai/llm.Completer adapter   (host)
//   - Cache     → MemoryProposalCache            (jit.NewMemoryProposalCache)
//   - Fetcher   → ibr adapter                    (host, T-0252)
//   - Publisher → kit bus                        (host, T-0252)
//   - Outage    → kit/core/breaker adapter       (host, T-0252)
//   - PageType  → pageshape classifier adapter   (host, T-0252)
package jit

import (
	"context"
	"net/url"

	kitdomain "hop.top/kit/go/runtime/domain"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// StrategyID is the registry-stable identifier for the JIT strategy.
const StrategyID = "jit"

// PageTypeClassifier abstracts the page-shape signal JIT feeds the proposer.
// Production wiring adapts pageshape.LLMClassifier (picking a representative
// label from the multi-label result); tests use a fake. Decoupled so jit
// stays free of pageshape imports — the daemon owns the adapter.
type PageTypeClassifier interface {
	Classify(ctx context.Context, sourceURL string) (string, error)
}

// noPageType is the safe-default classifier when none is wired. Always
// returns the empty string, which the proposer treats as "no page-type
// signal" — the LLM still gets the source domain.
type noPageType struct{}

func (noPageType) Classify(_ context.Context, _ string) (string, error) { return "", nil }

// Strategy is the FamilyJIT catch-all. It is registered last and consulted
// only when zero Tier-A strategies match the inbound event.
type Strategy struct {
	pipeline   *Pipeline
	classifier PageTypeClassifier
}

// New constructs a Strategy with all collaborators wired. proposer + executor
// are required (Probe will nil-panic without them); pub and outage are
// optional (nil → events suppressed / no outage). classifier is optional too;
// nil means "no page-type signal."
func New(proposer *CachedProposer, executor *Executor, pub kitdomain.EventPublisher, outage OutageDetector, classifier PageTypeClassifier) *Strategy {
	if classifier == nil {
		classifier = noPageType{}
	}
	return &Strategy{
		pipeline:   NewPipeline(proposer, executor, pub, outage),
		classifier: classifier,
	}
}

func (s *Strategy) ID() string                     { return StrategyID }
func (s *Strategy) Family() lateral.StrategyFamily { return lateral.FamilyJIT }

// Applies always matches at specificity 0. Registry gating ensures JIT only
// fires when no platform/shape strategy claimed the event.
func (s *Strategy) Applies(_ context.Context, _ lateral.CapturedEvent) lateral.AppliesResult {
	return lateral.AppliesResult{Matches: true, Specificity: 0}
}

// Probe runs the JIT pipeline once for ev. Derives the source domain from
// ev.SourceURL and the page type from the classifier; both are passed to
// the proposer.
//
// Probe returns the executor's candidate slice. Failure / outage events are
// emitted as side-effects via the wired publisher (see Pipeline.RunOnce).
// A proposer error short-circuits Probe with that error; per-path fetch
// failures do not (surviving candidates are returned).
func (s *Strategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	domain := domainOf(ev.SourceURL)
	pageType, _ := s.classifier.Classify(ctx, ev.SourceURL)
	return s.pipeline.RunOnce(ctx, ev.ObjectID, ev.SourceURL, domain, pageType)
}

// Preconditions: JIT runs from the captured event alone, no parent record fields required.
func (s *Strategy) Preconditions() []string { return nil }

// domainOf extracts the host from a URL. Returns "" if the URL is
// unparseable — the proposer treats empty domain as "no signal" and the
// pipeline's executor will return zero candidates because resolveSamehost
// fails on an empty base host.
func domainOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}
