package beehiiv

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// ParentStrategy is the catch-all BeehiivStrategy. Specificity 1 on
// custom domains, 2 on canonical *.beehiiv.com hosts; children shadow
// at 3.
type ParentStrategy struct {
	Client BeehiivClient
}

func NewParent(c BeehiivClient) *ParentStrategy { return &ParentStrategy{Client: c} }

func (*ParentStrategy) ID() string                    { return IDParent }
func (*ParentStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*ParentStrategy) Preconditions() []string       { return nil }

func (*ParentStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))
	if d.Platform != customdomain.PlatformBeehiiv {
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
	idKey := identitykey.Build("beehiiv", identitykey.EntityPublication, slug)
	return []lateral.Candidate{
		{
			URL:           root,
			CandidateType: CandidateTypePublication,
			Strategy:      IDParent,
			IdentityKey:   idKey,
			Preview:       map[string]any{"slug": slug},
		},
		{
			URL:           root + "/feed",
			CandidateType: CandidateTypeFeed,
			Strategy:      IDParent,
			IdentityKey:   identitykey.Build("beehiiv", "feed", slug),
			Preview:       map[string]any{"slug": slug},
		},
	}, nil
}
