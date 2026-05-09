package jit

import (
	"context"

	kitdomain "hop.top/kit/go/runtime/domain"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Pipeline composes the JIT collaborators (proposer, executor) with the
// failure + outage emitters into one runnable unit. RunOnce is what the
// strategy's Probe will call (T-0251); the helper exists in its own type
// so T-0298 can integration-test the failure paths and T-0249 has a stable
// seam to hook recipe-served-during-outage events.
//
// Pipeline calls ProposeWithOutageFallback on the proposer, so the proposer
// MUST be a *CachedProposer (or a future type that exposes the same
// signature). The current implementation uses *CachedProposer directly to
// avoid an extra interface for one method; revisit if a non-cached proposer
// ever needs to feed the pipeline.
type Pipeline struct {
	proposer *CachedProposer
	executor *Executor
	pub      kitdomain.EventPublisher
	outage   OutageDetector
}

// NewPipeline wires the four collaborators. pub is optional (nil-safe).
// outage is optional too; nil means "treat as healthy" — recipe.served
// events never fire.
func NewPipeline(proposer *CachedProposer, executor *Executor, pub kitdomain.EventPublisher, outage OutageDetector) *Pipeline {
	return &Pipeline{proposer: proposer, executor: executor, pub: pub, outage: outage}
}

// RunOnce executes one JIT scan: ask proposer for sub-paths (with outage
// awareness), execute them against the source URL, emit candidates +
// failure + outage events.
//
// Event emission:
//   - proposer error → ctxt.lateral.scan.failed (Mechanism=jit_proposal),
//     RunOnce returns (nil, err) — the caller decides whether to surface
//     this as a strategy error or swallow.
//   - cache hit served during outage → ctxt.lateral.recipe.served
//     (Circumstance=llm_outage); execution proceeds normally with the
//     cached sub-paths.
//   - empty proposer result → returns (nil, nil) silently. Empty is not a
//     failure event (the LLM may have legitimately had no opinion).
//   - per-path fetch failures → ctxt.lateral.subpath.failed once per
//     failure; surviving candidates are still returned.
//
// objectID identifies the captured parent object — required for the
// scan.failed and subpath.failed payloads (object_id field in schema).
// pageType is the page-shape label the proposer is keyed on (passed through
// to candidate.Preview and recipe.served events).
func (p *Pipeline) RunOnce(ctx context.Context, objectID, sourceURL, domain, pageType string) ([]lateral.Candidate, error) {
	subpaths, served, err := p.proposer.ProposeWithOutageFallback(ctx, domain, pageType, p.outage)
	if err != nil {
		_ = EmitProposalFailure(ctx, p.pub, objectID, sourceURL, err)
		return nil, err
	}
	if served {
		_ = EmitRecipeServed(ctx, p.pub, domain, pageType)
	}
	if len(subpaths) == 0 {
		return nil, nil
	}

	results := p.executor.Execute(ctx, sourceURL, subpaths)
	candidates, failures := Emit(results, pageType)
	if len(failures) > 0 {
		_ = EmitFetchFailures(ctx, p.pub, objectID, failures)
	}
	return candidates, nil
}
