package medium

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// PublicationStrategy claims captures of a Medium publication homepage.
//
// Recognised forms:
//   - https://medium.com/<publication-slug>
//   - https://<publication-slug>.medium.com
//   - any custom-domain Medium publication root path (/)
//
// Specificity: 3 — shadows ParentStrategy on these URLs.
type PublicationStrategy struct {
	Client MediumClient
}

func NewPublication(c MediumClient) *PublicationStrategy { return &PublicationStrategy{Client: c} }

func (*PublicationStrategy) ID() string                    { return IDPublication }
func (*PublicationStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*PublicationStrategy) Preconditions() []string       { return nil }

func (*PublicationStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))
	if d.Platform != customdomain.PlatformMedium {
		return lateral.AppliesResult{}
	}
	u, parts, err := parseURL(ev.SourceURL)
	if err != nil {
		return lateral.AppliesResult{}
	}
	host := strings.ToLower(u.Hostname())

	// Subdomain pub: <slug>.medium.com root path.
	if strings.HasSuffix(host, ".medium.com") && len(parts) == 0 {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	// Path-based pub: medium.com/<slug>, single segment, not @user.
	if host == "medium.com" && len(parts) == 1 && !strings.HasPrefix(parts[0], "@") && parts[0] != "p" {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	// Custom domain root path is a publication homepage.
	if d.CustomHost && len(parts) == 0 {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	return lateral.AppliesResult{}
}

func (s *PublicationStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, parts, err := parseURL(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(u.Hostname())
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))

	var slug string
	switch {
	case strings.HasSuffix(host, ".medium.com"):
		slug = strings.TrimSuffix(host, ".medium.com")
	case host == "medium.com" && len(parts) >= 1:
		slug = parts[0]
	case d.CustomHost:
		slug = host
		if s.Client != nil {
			if resolved, err := s.Client.ResolvePublication(ctx, host); err == nil && resolved != "" {
				slug = resolved
			}
		}
	default:
		return nil, nil
	}

	out := []lateral.Candidate{
		{
			URL:           "https://medium.com/" + slug,
			CandidateType: CandidateTypePublication,
			Strategy:      IDPublication,
			Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("medium", identitykey.EntityPublication, slug)),
		},
		{
			URL:           "https://medium.com/" + slug + "/archive",
			CandidateType: CandidateTypePublication,
			Strategy:      IDPublication,
			Preview:       identitykey.Set(map[string]any{"slug": slug, "facet": "archive"}, identitykey.Build("medium", identitykey.EntityPublication, slug+"/archive")),
		},
		{
			URL:           "https://medium.com/feed/" + slug,
			CandidateType: CandidateTypeFeed,
			Strategy:      IDPublication,
			Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("medium", "feed", slug)),
		},
	}
	return out, nil
}
