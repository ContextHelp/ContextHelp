package github

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// probeRepo / probePR / probeIssue / probeProfile / probeSponsor are the
// per-sub-path probe entry points dispatched from Strategy.Probe. The
// skeleton ships empty stubs; later T-0255..T-0259 commits flesh out
// each one.
//
// Each probe receives the captured event + active context and returns the
// raw candidate set. Cap_k and threshold gating happen in the substrate
// scoring stage; probes return everything they find.

func (s *GitHubStrategy) probeRepo(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}

func (s *GitHubStrategy) probePR(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}

func (s *GitHubStrategy) probeIssue(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}

func (s *GitHubStrategy) probeProfile(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}

func (s *GitHubStrategy) probeSponsor(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}
