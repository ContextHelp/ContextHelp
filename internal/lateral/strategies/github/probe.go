package github

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// probeRepo / probePR / probeIssue / probeProfile / probeSponsor are the
// per-sub-path probe entry points dispatched from Strategy.Probe.
//
// Each probe receives the captured event + active context and returns the
// raw candidate set. Cap_k and threshold gating happen in the substrate
// scoring stage; probes return everything they find. Probes degrade
// silently when an underlying APIClient call errors — one failed sub-path
// must not nuke the whole probe — but emit a subpath.failed event via the
// Failures helper so ops keeps visibility.

func (s *GitHubStrategy) probeRepo(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	pu, ok := parseGitHubURL(ev.SourceURL)
	if !ok || pu.Owner == "" || pu.Repo == "" {
		return nil, nil
	}
	if s.deps.APIClient == nil {
		// Skeleton mode: no client wired. Emit only the structural
		// owner-profile candidate (no fetch needed).
		return []lateral.Candidate{ownerProfileCandidate(pu.Owner, s.ID())}, nil
	}

	var out []lateral.Candidate

	// 1. Sibling repos: other repos owned by the same owner, excluding the
	//    captured repo itself.
	if siblings, err := s.deps.APIClient.ListRepoSiblings(ctx, pu.Owner, pu.Repo); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_repo_siblings", err)
	} else {
		for _, r := range siblings {
			out = append(out, repoCandidate(r, TypeSiblingRepo, s.ID()))
		}
	}

	// 2. Owner profile candidate (always emit; no fetch).
	out = append(out, ownerProfileCandidate(pu.Owner, s.ID()))

	// 3. Pinned repos surfaced on owner's profile.
	if pinned, err := s.deps.APIClient.ListOwnerPinned(ctx, pu.Owner); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_owner_pinned", err)
	} else {
		for _, r := range pinned {
			out = append(out, repoCandidate(r, TypePinnedRepo, s.ID()))
		}
	}

	// 4. Sponsor page (if owner has Sponsors enabled).
	if has, err := s.deps.APIClient.HasSponsorPage(ctx, pu.Owner); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "has_sponsor_page", err)
	} else if has {
		out = append(out, sponsorCandidate(pu.Owner, s.ID()))
	}

	// 5. Starred repos: surface things the owner finds notable.
	if starred, err := s.deps.APIClient.ListOwnerStarred(ctx, pu.Owner); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_owner_starred", err)
	} else {
		for _, r := range starred {
			out = append(out, repoCandidate(r, TypeStarredRepo, s.ID()))
		}
	}

	return out, nil
}

// repoCandidate builds a Candidate for a github repo with the given
// candidateType (sibling_repo, pinned_repo, starred_repo, owned_repo,
// advisory_repo). Identity-key is the canonical repo key.
func repoCandidate(r RepoSummary, candidateType, strategyID string) lateral.Candidate {
	url := r.URL
	if url == "" {
		url = repoURL(r.Owner, r.Name)
	}
	preview := map[string]any{
		PreviewKeyIdentityKey: repoIdentityKey(r.Owner, r.Name),
		"owner":               r.Owner,
		"name":                r.Name,
		"description":         r.Description,
		"stars":               r.Stars,
		"language":            r.Language,
		"archived":            r.Archived,
		"fork":                r.Fork,
	}
	return lateral.Candidate{
		URL:           url,
		CandidateType: candidateType,
		Strategy:      strategyID,
		Preview:       preview,
	}
}

// ownerProfileCandidate builds an owner_profile candidate for login.
func ownerProfileCandidate(login, strategyID string) lateral.Candidate {
	return lateral.Candidate{
		URL:           userURL(login),
		CandidateType: TypeOwnerProfile,
		Strategy:      strategyID,
		Preview: map[string]any{
			PreviewKeyIdentityKey: userIdentityKey(login),
			"login":               login,
		},
	}
}

// sponsorCandidate builds a sponsor_page candidate for login.
func sponsorCandidate(login, strategyID string) lateral.Candidate {
	return lateral.Candidate{
		URL:           sponsorURL(login),
		CandidateType: TypeSponsorPage,
		Strategy:      strategyID,
		Preview: map[string]any{
			PreviewKeyIdentityKey: userIdentityKey(login),
			"login":               login,
		},
	}
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
