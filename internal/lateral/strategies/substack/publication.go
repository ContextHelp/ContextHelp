package substack

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// PublicationStrategy claims a publication homepage (root path, or
// /archive). Specificity 3.
type PublicationStrategy struct {
	Client SubstackClient
}

func NewPublication(c SubstackClient) *PublicationStrategy { return &PublicationStrategy{Client: c} }

func (*PublicationStrategy) ID() string                     { return IDPublication }
func (*PublicationStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*PublicationStrategy) Preconditions() []string        { return nil }

func (*PublicationStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))
	if d.Platform != customdomain.PlatformSubstack {
		return lateral.AppliesResult{}
	}
	_, parts, err := parseURL(ev.SourceURL)
	if err != nil {
		return lateral.AppliesResult{}
	}
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "archive") {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	return lateral.AppliesResult{}
}

func (s *PublicationStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, _, err := parseURL(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(u.Hostname())
	slug := canonicalSlug(ctx, host, s.Client)
	root := canonicalRoot(slug)
	idKey := identitykey.Build("substack", identitykey.EntityPublication, slug)
	return []lateral.Candidate{
		{
			URL:           root,
			CandidateType: CandidateTypePublication,
			Strategy:      IDPublication,
			IdentityKey:   idKey,
			Preview:       map[string]any{"slug": slug},
		},
		{
			URL:           root + "/archive",
			CandidateType: CandidateTypeArchive,
			Strategy:      IDPublication,
			IdentityKey:   idKey,
			Preview:       map[string]any{"slug": slug, "facet": "archive"},
		},
		{
			URL:           root + "/about",
			CandidateType: CandidateTypePublication,
			Strategy:      IDPublication,
			IdentityKey:   idKey,
			Preview:       map[string]any{"slug": slug, "facet": "about"},
		},
		{
			URL:           root + "/feed",
			CandidateType: CandidateTypeFeed,
			Strategy:      IDPublication,
			IdentityKey:   identitykey.Build("substack", "feed", slug),
			Preview:       map[string]any{"slug": slug},
		},
	}, nil
}
