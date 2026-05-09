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
	return lateral.Candidate{
		URL:           url,
		CandidateType: candidateType,
		Strategy:      strategyID,
		IdentityKey:   repoIdentityKey(r.Owner, r.Name),
		Preview: map[string]any{
			"owner":       r.Owner,
			"name":        r.Name,
			"description": r.Description,
			"stars":       r.Stars,
			"language":    r.Language,
			"archived":    r.Archived,
			"fork":        r.Fork,
		},
	}
}

// ownerProfileCandidate builds an owner_profile candidate for login.
func ownerProfileCandidate(login, strategyID string) lateral.Candidate {
	return lateral.Candidate{
		URL:           userURL(login),
		CandidateType: TypeOwnerProfile,
		Strategy:      strategyID,
		IdentityKey:   userIdentityKey(login),
		Preview: map[string]any{
			"login": login,
		},
	}
}

// sponsorCandidate builds a sponsor_page candidate for login.
func sponsorCandidate(login, strategyID string) lateral.Candidate {
	return lateral.Candidate{
		URL:           sponsorURL(login),
		CandidateType: TypeSponsorPage,
		Strategy:      strategyID,
		IdentityKey:   userIdentityKey(login),
		Preview: map[string]any{
			"login": login,
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
//
// author_other_pr fires only when ac.AuthorHints["github"] is set
// (T-0308). The capture pipeline's session middleware populates the
// hint from a captured PR's author when it has one; without the hint,
// probePR skips the author-scoped fetch (this matches the v1
// best-effort posture).
func (s *GitHubStrategy) probePR(ctx context.Context, ev lateral.CapturedEvent, ac lateral.ActiveContext) ([]lateral.Candidate, error) {
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
	if author := authorHintFor(ac); author != "" {
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
		IdentityKey:   idKey,
		Preview: map[string]any{
			"login": u.Login,
			"type":  u.Type,
			"name":  u.Name,
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
		IdentityKey:   orgIdentityKey(o.Login),
		Preview: map[string]any{
			"login": o.Login,
			"name":  o.Name,
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

// authorHintFor returns the github author login carried on
// ActiveContext.AuthorHints["github"] when the capture pipeline's
// session middleware has staged one (T-0308). Empty when the platform
// key is absent, the value is empty, or AuthorHints is nil — the
// probes treat that as "skip the author-scoped fetch."
func authorHintFor(ac lateral.ActiveContext) string {
	if ac.AuthorHints == nil {
		return ""
	}
	return ac.AuthorHints["github"]
}

// probeIssue emits candidates from a captured issue URL
// (/<owner>/<repo>/issues/<n> or /<owner>/<repo>/issues). Lateral surface:
//
//   - author_other_issue — other issues by the same author
//   - repo_issue         — repo (parent) candidate (helpful when capture
//                          was the issue thread, not the repo itself)
//   - issue_label        — labels attached to the issue
//
// Number-less URLs (/issues list view) skip label lookup. owner_profile
// is always emitted.
//
// author_other_issue fires only when ac.AuthorHints["github"] is set
// (T-0308); see probePR for the same hint-driven seam.
func (s *GitHubStrategy) probeIssue(ctx context.Context, ev lateral.CapturedEvent, ac lateral.ActiveContext) ([]lateral.Candidate, error) {
	pu, ok := parseGitHubURL(ev.SourceURL)
	if !ok || pu.Owner == "" || pu.Repo == "" {
		return nil, nil
	}
	if s.deps.APIClient == nil {
		return []lateral.Candidate{
			ownerProfileCandidate(pu.Owner, s.ID()),
			repoIssueParentCandidate(pu.Owner, pu.Repo, s.ID()),
		}, nil
	}

	out := []lateral.Candidate{
		ownerProfileCandidate(pu.Owner, s.ID()),
		repoIssueParentCandidate(pu.Owner, pu.Repo, s.ID()),
	}

	// Labels: only when we have a number (issue-level URL).
	if pu.Number > 0 {
		if labels, err := s.deps.APIClient.ListIssueLabels(ctx, pu.Owner, pu.Repo, pu.Number); err != nil {
			recordSubpathFailure(s.ID(), ev.ObjectID, "list_issue_labels", err)
		} else {
			for _, l := range labels {
				out = append(out, labelCandidate(l, pu.Owner, pu.Repo, s.ID()))
			}
		}
	}

	// Author's other issues: same hint mechanism as PR; v1 best-effort.
	if author := authorHintFor(ac); author != "" {
		const limit = 25
		if iss, err := s.deps.APIClient.ListAuthoredIssues(ctx, author, limit); err != nil {
			recordSubpathFailure(s.ID(), ev.ObjectID, "list_authored_issues", err)
		} else {
			for _, i := range iss {
				out = append(out, issueCandidate(i, TypeAuthorIssue, s.ID()))
			}
		}
	}

	return out, nil
}

// repoIssueParentCandidate emits a repo_issue candidate pointing at the
// parent repo of a captured issue.
func repoIssueParentCandidate(owner, repo, strategyID string) lateral.Candidate {
	return lateral.Candidate{
		URL:           repoURL(owner, repo),
		CandidateType: TypeRepoIssue,
		Strategy:      strategyID,
		IdentityKey:   repoIdentityKey(owner, repo),
		Preview: map[string]any{
			"owner": owner,
			"name":  repo,
		},
	}
}

// labelCandidate builds an issue_label candidate.
func labelCandidate(l LabelSummary, owner, repo, strategyID string) lateral.Candidate {
	url := l.URL
	if url == "" {
		url = "https://github.com/" + owner + "/" + repo + "/labels/" + l.Name
	}
	return lateral.Candidate{
		URL:           url,
		CandidateType: TypeIssueLabel,
		Strategy:      strategyID,
		Preview: map[string]any{
			"label":       l.Name,
			"description": l.Description,
			"color":       l.Color,
			"owner":       owner,
			"repo":        repo,
		},
	}
}

// issueCandidate builds an issue cross-reference candidate.
func issueCandidate(i IssueSummary, candidateType, strategyID string) lateral.Candidate {
	url := i.URL
	if url == "" {
		url = repoURL(i.Owner, i.Repo)
	}
	return lateral.Candidate{
		URL:           url,
		CandidateType: candidateType,
		Strategy:      strategyID,
		Preview: map[string]any{
			"owner":  i.Owner,
			"repo":   i.Repo,
			"number": i.Number,
			"title":  i.Title,
			"state":  i.State,
			"author": i.Author,
		},
	}
}

// probeProfile emits candidates from a captured profile URL (/<login>).
// Lateral surface:
//
//   - owned_repo        — repos owned by the profile (proxied via siblings
//                         lookup with empty exclude)
//   - pinned_repo       — pinned items on the profile
//   - sponsor_page      — the profile's sponsors page (if active)
//   - sponsored_profile — profiles this profile sponsors
//   - contribution_org  — organizations this profile publicly contributes to
//
// Profile probes are uniformly cheap (no number-gating). Skeleton mode
// emits the structural sponsor candidate (always speculatively useful)
// without requiring an API call.
func (s *GitHubStrategy) probeProfile(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	pu, ok := parseGitHubURL(ev.SourceURL)
	if !ok || pu.Owner == "" {
		return nil, nil
	}
	login := pu.Owner
	if s.deps.APIClient == nil {
		// Without a client we still emit the speculative sponsor page
		// (cheap, helps cold-start when the page exists).
		return []lateral.Candidate{
			sponsorCandidate(login, s.ID()),
		}, nil
	}

	var out []lateral.Candidate

	// Owned repos: ListRepoSiblings(login, "") returns all of login's repos.
	if owned, err := s.deps.APIClient.ListRepoSiblings(ctx, login, ""); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_owned_repos", err)
	} else {
		for _, r := range owned {
			out = append(out, repoCandidate(r, TypeOwnedRepo, s.ID()))
		}
	}

	// Pinned items.
	if pinned, err := s.deps.APIClient.ListOwnerPinned(ctx, login); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_owner_pinned", err)
	} else {
		for _, r := range pinned {
			out = append(out, repoCandidate(r, TypePinnedRepo, s.ID()))
		}
	}

	// Sponsor page.
	if has, err := s.deps.APIClient.HasSponsorPage(ctx, login); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "has_sponsor_page", err)
	} else if has {
		out = append(out, sponsorCandidate(login, s.ID()))
	}

	// Sponsored profiles (whom this user sponsors).
	if sp, err := s.deps.APIClient.ListSponsored(ctx, login); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_sponsored", err)
	} else {
		for _, u := range sp {
			out = append(out, userCandidate(u, TypeSponsored, s.ID()))
		}
	}

	// Contribution orgs.
	if orgs, err := s.deps.APIClient.ListContributionOrgs(ctx, login); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_contribution_orgs", err)
	} else {
		for _, o := range orgs {
			out = append(out, orgCandidate(o, TypeContribOrg, s.ID()))
		}
	}

	return out, nil
}

// probeSponsor emits candidates from a captured /sponsors/<login> URL.
// Lateral surface:
//
//   - owner_profile   — the sponsored profile itself (always)
//   - similar_sponsor — profiles that sponsor a similar set of recipients
//                       to login (i.e. peers of login as a sponsor)
//
// Skeleton mode emits only owner_profile (no fetch).
func (s *GitHubStrategy) probeSponsor(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	pu, ok := parseGitHubURL(ev.SourceURL)
	if !ok || pu.Login == "" {
		return nil, nil
	}
	login := pu.Login
	out := []lateral.Candidate{ownerProfileCandidate(login, s.ID())}
	if s.deps.APIClient == nil {
		return out, nil
	}
	if peers, err := s.deps.APIClient.ListSimilarSponsors(ctx, login); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_similar_sponsors", err)
	} else {
		for _, u := range peers {
			out = append(out, userCandidate(u, TypeSimilarSpons, s.ID()))
		}
	}
	return out, nil
}
