package substack

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// ParentStrategy is the catch-all SubstackStrategy. It matches any
// captured URL the customdomain detector tags as substack-hosted
// (canonical or custom domain). Specificity 1; children shadow with 3.
type ParentStrategy struct {
	Client SubstackClient
}

func NewParent(c SubstackClient) *ParentStrategy { return &ParentStrategy{Client: c} }

func (*ParentStrategy) ID() string                    { return IDParent }
func (*ParentStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*ParentStrategy) Preconditions() []string       { return nil }

func (*ParentStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))
	if d.Platform != customdomain.PlatformSubstack {
		return lateral.AppliesResult{}
	}
	if d.CustomHost {
		return lateral.AppliesResult{Matches: true, Specificity: 1}
	}
	return lateral.AppliesResult{Matches: true, Specificity: 2}
}

func (s *ParentStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, _, err := parseURL(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(u.Hostname())
	slug := canonicalSlug(ctx, host, s.Client)
	root := canonicalRoot(slug)

	out := []lateral.Candidate{
		{
			URL:           root,
			CandidateType: CandidateTypePublication,
			Strategy:      IDParent,
			Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("substack", identitykey.EntityPublication, slug)),
		},
		{
			URL:           root + "/feed",
			CandidateType: CandidateTypeFeed,
			Strategy:      IDParent,
			Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("substack", "feed", slug)),
		},
	}
	return out, nil
}
