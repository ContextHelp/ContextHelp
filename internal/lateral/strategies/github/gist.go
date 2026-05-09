package github

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// GistStrategy is the child platform-keyed strategy for gist.github.com.
// It shadows GitHubStrategy at higher specificity so the dispatcher
// picks GistStrategy for any captured gist URL — gist surfaces have
// different sub-paths than github.com proper.
//
// Lateral surface for /<owner>/<gist_id>:
//
//   - owner_profile — login on github.com (always)
//   - owner_gist    — other gists by login
//
// Skeleton mode emits owner_profile only.
type GistStrategy struct {
	deps Dependencies
}

// NewGistStrategy constructs a GistStrategy with the given dependencies.
func NewGistStrategy(deps Dependencies) *GistStrategy {
	return &GistStrategy{deps: deps}
}

// ID implements lateral.LateralStrategy.
func (s *GistStrategy) ID() string { return StrategyIDGist }

// Family implements lateral.LateralStrategy.
func (s *GistStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }

// Preconditions implements lateral.LateralStrategy.
func (s *GistStrategy) Preconditions() []string { return nil }

// Applies returns Matches=true for gist.github.com URLs (and only those —
// the parent GitHubStrategy is responsible for plain github.com).
// Specificity is SpecificityChild (= 2) so it shadows the parent in the
// dispatch step.
func (s *GistStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host, ok := hostOf(ev.SourceURL)
	if !ok {
		return lateral.AppliesResult{}
	}
	if !isGistHost(host) {
		return lateral.AppliesResult{}
	}
	return lateral.AppliesResult{Matches: true, Specificity: SpecificityChild}
}

// isGistHost reports whether host is the gist subdomain. Returns true for
// "gist.github.com" exactly (not for any deeper subdomain prefix).
func isGistHost(host string) bool {
	return strings.EqualFold(host, "gist.github.com")
}

// Probe emits gist-flavoured candidates from a captured gist URL. Path
// shape is /<owner>/<gist_id> (or /<owner> for the owner-gists list);
// other shapes route to no-op.
func (s *GistStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	owner, gistID, ok := parseGistPath(ev.SourceURL)
	if !ok || owner == "" {
		return nil, nil
	}
	out := []lateral.Candidate{ownerProfileCandidate(owner, s.ID())}

	if s.deps.APIClient == nil {
		return out, nil
	}

	// Other gists by the owner — exclude the captured gist if any.
	if gists, err := s.deps.APIClient.ListOwnerGists(ctx, owner); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_owner_gists", err)
	} else {
		for _, g := range gists {
			if g.ID == gistID && gistID != "" {
				continue
			}
			out = append(out, gistCandidate(g, TypeOwnerGist, s.ID()))
		}
	}

	return out, nil
}

// parseGistPath extracts (owner, gist_id) from a gist URL. Returns
// ok=false on parse failure or non-gist host.
func parseGistPath(raw string) (owner, gistID string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	if !isGistHost(strings.ToLower(u.Hostname())) {
		return "", "", false
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return "", "", false
	}
	parts := strings.Split(p, "/")
	switch len(parts) {
	case 1:
		return parts[0], "", true
	default:
		return parts[0], parts[1], true
	}
}

// gistCandidate builds a candidate for a gist.
func gistCandidate(g GistSummary, candidateType, strategyID string) lateral.Candidate {
	url := g.URL
	if url == "" {
		url = gistURL(g.Owner, g.ID)
	}
	return lateral.Candidate{
		URL:           url,
		CandidateType: candidateType,
		Strategy:      strategyID,
		Preview: map[string]any{
			"id":          g.ID,
			"owner":       g.Owner,
			"description": g.Description,
			"files":       g.Files,
		},
	}
}
