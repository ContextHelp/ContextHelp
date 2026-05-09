package jit

import (
	"context"

	kitdomain "hop.top/kit/go/runtime/domain"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Pipeline composes the JIT collaborators (proposer, executor) with the
// failure emitter into one runnable unit. RunOnce is what the strategy's
// Probe will call (T-0251); the helper exists in its own type so T-0298
// can integration-test the failure paths and T-0249 has a stable seam to
// hook recipe-served-during-outage events.
type Pipeline struct {
	proposer Proposer
	executor *Executor
	pub      kitdomain.EventPublisher
}

// NewPipeline wires the three collaborators. pub is optional (nil-safe);
// the failure emitters silently no-op when no publisher is configured.
func NewPipeline(proposer Proposer, executor *Executor, pub kitdomain.EventPublisher) *Pipeline {
	return &Pipeline{proposer: proposer, executor: executor, pub: pub}
}

// RunOnce executes one JIT scan: ask proposer for sub-paths, execute them
// against the source URL, emit candidates + failure events.
//
// Failure handling:
//   - proposer error → ctxt.lateral.scan.failed (Mechanism=jit_proposal),
//     RunOnce returns (nil, err) — the caller decides whether to surface
//     this as a strategy error or swallow.
//   - empty proposer result → returns (nil, nil) silently. Empty is not a
//     failure event (the LLM may have legitimately had no opinion).
//   - per-path fetch failures → ctxt.lateral.subpath.failed once per
//     failure; surviving candidates are still returned.
//
// objectID identifies the captured parent object — required for the
// scan.failed and subpath.failed payloads (object_id field in schema).
// pageType is the page-shape label the proposer is keyed on (passed through
// to candidate.Preview).
func (p *Pipeline) RunOnce(ctx context.Context, objectID, sourceURL, domain, pageType string) ([]lateral.Candidate, error) {
	subpaths, err := p.proposer.Propose(ctx, domain, pageType)
	if err != nil {
		_ = EmitProposalFailure(ctx, p.pub, objectID, sourceURL, err)
		return nil, err
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
