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

// probePR emits candidates from a captured PR URL (/<owner>/<repo>/pull/<n>
// or /<owner>/<repo>/pulls). The lateral surface is:
//
//   - author_other_pr — other PRs by the same author across github
//   - sibling_repo   — repos owned by the PR repo's owner (siblings)
//   - pr_reviewer    — reviewers requested + acted on this PR
//
// Each sub-path fetch degrades silently on API error; subpath.failed
// goes to the recorder. Author/reviewer lookup requires the API client;
// skeleton mode emits only the structural sibling owner_profile.
func (s *GitHubStrategy) probePR(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	pu, ok := parseGitHubURL(ev.SourceURL)
	if !ok || pu.Owner == "" || pu.Repo == "" {
		return nil, nil
	}
	if s.deps.APIClient == nil {
		return []lateral.Candidate{ownerProfileCandidate(pu.Owner, s.ID())}, nil
	}

	var out []lateral.Candidate

	// Owner profile is always emitted (cheap, structural).
	out = append(out, ownerProfileCandidate(pu.Owner, s.ID()))

	// Sibling repos: same as repo probe, lighter limit applied by scoring.
	if siblings, err := s.deps.APIClient.ListRepoSiblings(ctx, pu.Owner, pu.Repo); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_repo_siblings", err)
	} else {
		for _, r := range siblings {
			out = append(out, repoCandidate(r, TypeSiblingRepo, s.ID()))
		}
	}

	// PR reviewers: fetch only when we have a number (PR-level URL).
	if pu.Number > 0 {
		if revs, err := s.deps.APIClient.ListPRReviewers(ctx, pu.Owner, pu.Repo, pu.Number); err != nil {
			recordSubpathFailure(s.ID(), ev.ObjectID, "list_pr_reviewers", err)
		} else {
			for _, r := range revs {
				out = append(out, userCandidate(r, TypeReviewer, s.ID()))
			}
		}
	}

	// Author's other PRs: use the captured PR's author signal in the
	// event's preview if available; otherwise fall back to fetching from
	// the PR endpoint. v1 reads author from the event's preview when the
	// daemon has populated it; without that, we skip author-based
	// candidates rather than incur an extra fetch.
	//
	// The CapturedEvent type doesn't carry preview today (substrate is a
	// thin envelope). Author lookup therefore happens via the API on the
	// PR endpoint inside ListAuthoredPRs only when an explicit author
	// hint comes through the active context. v1 keeps this path
	// best-effort.
	if author := authorHintFor(ev); author != "" {
		const limit = 25
		if prs, err := s.deps.APIClient.ListAuthoredPRs(ctx, author, limit); err != nil {
			recordSubpathFailure(s.ID(), ev.ObjectID, "list_authored_prs", err)
		} else {
			for _, p := range prs {
				out = append(out, prCandidate(p, TypeAuthorPR, s.ID()))
			}
		}
	}

	return out, nil
}

// userCandidate builds a github user/org candidate.
func userCandidate(u UserSummary, candidateType, strategyID string) lateral.Candidate {
	url := u.URL
	if url == "" {
		url = userURL(u.Login)
	}
	idKey := userIdentityKey(u.Login)
	if u.Type == "Organization" {
		idKey = orgIdentityKey(u.Login)
	}
	return lateral.Candidate{
		URL:           url,
		CandidateType: candidateType,
		Strategy:      strategyID,
		Preview: map[string]any{
			PreviewKeyIdentityKey: idKey,
			"login":               u.Login,
			"type":                u.Type,
			"name":                u.Name,
		},
	}
}

// orgCandidate builds an organization candidate.
func orgCandidate(o OrgSummary, candidateType, strategyID string) lateral.Candidate {
	url := o.URL
	if url == "" {
		url = userURL(o.Login)
	}
	return lateral.Candidate{
		URL:           url,
		CandidateType: candidateType,
		Strategy:      strategyID,
		Preview: map[string]any{
			PreviewKeyIdentityKey: orgIdentityKey(o.Login),
			"login":               o.Login,
			"name":                o.Name,
		},
	}
}

// prCandidate builds a candidate for a PR cross-reference.
func prCandidate(p PullRequestSummary, candidateType, strategyID string) lateral.Candidate {
	url := p.URL
	if url == "" {
		url = repoURL(p.Owner, p.Repo) // best-effort fallback
	}
	return lateral.Candidate{
		URL:           url,
		CandidateType: candidateType,
		Strategy:      strategyID,
		Preview: map[string]any{
			"owner":  p.Owner,
			"repo":   p.Repo,
			"number": p.Number,
			"title":  p.Title,
			"state":  p.State,
			"author": p.Author,
		},
	}
}

// authorHintFor returns the author login carried via the captured event's
// active-context fingerprint, when the daemon has staged one. v1 reads
// from a well-known key on ActiveContext — but ActiveContext doesn't
// surface arbitrary string values, so v1 always returns empty. T-0267
// integration test exercises the full flow when the daemon is wired to
// stage author hints out-of-band; v1 keeps probePR best-effort without
// it.
func authorHintFor(_ lateral.CapturedEvent) string { return "" }

func (s *GitHubStrategy) probeIssue(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}

func (s *GitHubStrategy) probeProfile(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}

func (s *GitHubStrategy) probeSponsor(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}
