// Package pageshape adapts the lateral/pageshape multi-label
// classifier stack into jit.PageTypeClassifier (single-string output).
//
// pageshape ships two layers:
//
//   - Heuristic: URL/HTML pattern matching, cheap, returns []Label.
//   - LLMClassifier: composes an LLM + RecipeCache, returns []Label
//     and caches verdicts by (domain, normalized-path) tuple.
//
// jit's PageTypeClassifier wants a single string ("BlogPost",
// "AboutPage", ...). This adapter:
//
//  1. Tries the Heuristic first (no I/O, no LLM cost).
//  2. Falls back to the LLM stack when the heuristic returns nothing.
//  3. Picks the first label from the result and returns its string
//     form. Tie-break is "first label wins" — pageshape doesn't expose
//     confidence scores. Callers wanting deterministic ordering across
//     equivalent labels should sort the heuristic output upstream.
//
// HTML fetching: jit calls Classify with just a URL. The heuristic
// works URL-only (some labels match purely on path patterns); the LLM
// stack accepts an Input{URL, HTML} and works URL-only too. We never
// fetch HTML in this adapter — the daemon-side fetcher concerns are
// owned by the strategy pipelines, not the classifier.
package pageshape

import (
	"context"

	lateralpageshape "github.com/ideacrafterslabs/ctxt/internal/lateral/pageshape"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// HeuristicLayer is the subset of *pageshape.Heuristic the adapter
// uses. The real type satisfies it; tests inject a stub.
type HeuristicLayer interface {
	Classify(in lateralpageshape.Input) []lateralpageshape.Label
}

// LLMLayer is the subset of *pageshape.LLMClassifier the adapter uses.
// pageshape.LLMClassifier satisfies it.
type LLMLayer interface {
	Classify(ctx context.Context, in lateralpageshape.Input) ([]lateralpageshape.Label, error)
}

// Classifier composes a heuristic + LLM stack into a single string-
// returning classifier suitable for jit. Either layer may be nil
// (skipped). When both are nil, Classify always returns ("", nil) —
// jit's noPageType behaviour.
type Classifier struct {
	heuristic HeuristicLayer
	llm       LLMLayer
}

// New wires a Classifier. Pass nil for either layer to skip it.
// Typical production wiring:
//
//	heuristic := pageshape.NewHeuristic()
//	llmClassifier := pageshape.NewLLMClassifier(llm, cache)
//	classifier := New(heuristic, llmClassifier)
func New(heuristic HeuristicLayer, llm LLMLayer) *Classifier {
	return &Classifier{heuristic: heuristic, llm: llm}
}

// jit.PageTypeClassifier contract.
var _ jit.PageTypeClassifier = (*Classifier)(nil)

// Classify returns the string form of the first label produced by the
// heuristic (preferred) or the LLM (fallback). Returns ("", nil) when
// neither layer produces a result.
//
// Errors propagate from the LLM layer; the heuristic is infallible.
// Per the spec, JIT treats classifier errors as "no signal" — the
// proposer still runs with the empty page-type. Callers wanting
// stricter behaviour can wrap.
func (c *Classifier) Classify(ctx context.Context, sourceURL string) (string, error) {
	in := lateralpageshape.Input{URL: sourceURL}

	if c.heuristic != nil {
		if labels := c.heuristic.Classify(in); len(labels) > 0 {
			return string(labels[0]), nil
		}
	}

	if c.llm != nil {
		labels, err := c.llm.Classify(ctx, in)
		if err != nil {
			return "", err
		}
		if len(labels) > 0 {
			return string(labels[0]), nil
		}
	}

	return "", nil
}
