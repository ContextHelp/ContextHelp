package beehiiv

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// PostStrategy claims /p/<slug> single-post URLs. Specificity 3.
type PostStrategy struct {
	Client BeehiivClient
}

func NewPost(c BeehiivClient) *PostStrategy { return &PostStrategy{Client: c} }

func (*PostStrategy) ID() string                    { return IDPost }
func (*PostStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*PostStrategy) Preconditions() []string       { return nil }

func (*PostStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))
	if d.Platform != customdomain.PlatformBeehiiv {
		return lateral.AppliesResult{}
	}
	_, parts, err := parseURL(ev.SourceURL)
	if err != nil || len(parts) < 2 {
		return lateral.AppliesResult{}
	}
	if parts[0] == "p" && parts[1] != "" {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	return lateral.AppliesResult{}
}

func (s *PostStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, parts, err := parseURL(ev.SourceURL)
	if err != nil || len(parts) < 2 {
		return nil, err
	}
	host := strings.ToLower(u.Hostname())
	slug := canonicalSlug(ctx, host, s.Client)
	root := canonicalRoot(slug)
	postSlug := parts[1]

	postKey := identitykey.Build("beehiiv", identitykey.EntityPost, slug, postSlug)
	pubKey := identitykey.Build("beehiiv", identitykey.EntityPublication, slug)

	return []lateral.Candidate{
		{
			URL:           root + "/p/" + postSlug,
			CandidateType: CandidateTypePost,
			Strategy:      IDPost,
			IdentityKey:   postKey,
			Preview:       map[string]any{"slug": slug, "post": postSlug},
		},
		{
			URL:           root,
			CandidateType: CandidateTypePublication,
			Strategy:      IDPost,
			IdentityKey:   pubKey,
			Preview:       map[string]any{"slug": slug},
		},
		{
			URL:           root + "/archive",
			CandidateType: CandidateTypeArchive,
			Strategy:      IDPost,
			IdentityKey:   pubKey,
			Preview:       map[string]any{"slug": slug, "facet": "archive"},
		},
	}, nil
}
