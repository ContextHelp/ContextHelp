package beehiiv

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// PublicationStrategy claims publication root, /archive, /about pages.
// Specificity 3.
type PublicationStrategy struct {
	Client BeehiivClient
}

func NewPublication(c BeehiivClient) *PublicationStrategy { return &PublicationStrategy{Client: c} }

func (*PublicationStrategy) ID() string                    { return IDPublication }
func (*PublicationStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*PublicationStrategy) Preconditions() []string       { return nil }

func (*PublicationStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))
	if d.Platform != customdomain.PlatformBeehiiv {
		return lateral.AppliesResult{}
	}
	_, parts, err := parseURL(ev.SourceURL)
	if err != nil {
		return lateral.AppliesResult{}
	}
	if len(parts) == 0 || (len(parts) == 1 && (parts[0] == "archive" || parts[0] == "about" || parts[0] == "subscribe")) {
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
	idKey := identitykey.Build("beehiiv", identitykey.EntityPublication, slug)
	return []lateral.Candidate{
		{
			URL:           root,
			CandidateType: CandidateTypePublication,
			Strategy:      IDPublication,
			Preview:       identitykey.Set(map[string]any{"slug": slug}, idKey),
		},
		{
			URL:           root + "/archive",
			CandidateType: CandidateTypeArchive,
			Strategy:      IDPublication,
			Preview:       identitykey.Set(map[string]any{"slug": slug, "facet": "archive"}, idKey),
		},
		{
			URL:           root + "/about",
			CandidateType: CandidateTypePublication,
			Strategy:      IDPublication,
			Preview:       identitykey.Set(map[string]any{"slug": slug, "facet": "about"}, idKey),
		},
		{
			URL:           root + "/feed",
			CandidateType: CandidateTypeFeed,
			Strategy:      IDPublication,
			Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("beehiiv", "feed", slug)),
		},
	}, nil
}
