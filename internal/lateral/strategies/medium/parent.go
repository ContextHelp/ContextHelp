package medium

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// ParentStrategy is the catch-all MediumStrategy. It claims captures
// on medium.com, on Medium-hosted subdomains, and on custom domains
// the customdomain detector tags as Medium-hosted.
//
// Specificity:
//   - 1 on apex/canonical medium.com
//   - 2 on a *.medium.com subdomain (publication-owned)
//   - 1 on a custom domain (children may shadow with 3)
type ParentStrategy struct {
	Client MediumClient
}

func NewParent(c MediumClient) *ParentStrategy { return &ParentStrategy{Client: c} }

func (*ParentStrategy) ID() string                    { return IDParent }
func (*ParentStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*ParentStrategy) Preconditions() []string       { return nil }

func (*ParentStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))
	if d.Platform != customdomain.PlatformMedium {
		return lateral.AppliesResult{}
	}
	switch {
	case d.CustomHost:
		return lateral.AppliesResult{Matches: true, Specificity: 1}
	case d.Host == "medium.com":
		return lateral.AppliesResult{Matches: true, Specificity: 1}
	default:
		return lateral.AppliesResult{Matches: true, Specificity: 2}
	}
}

func (s *ParentStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, parts, err := parseURL(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(u.Hostname())
	author, slug := articleSlugFromPath(parts)

	// Article candidate (when path looks article-shaped).
	var out []lateral.Candidate
	if slug != "" {
		out = append(out, lateral.Candidate{
			URL:           "https://medium.com/p/" + slug,
			CandidateType: CandidateTypeArticle,
			Strategy:      IDParent,
			Preview:       identitykey.Set(map[string]any{"slug": slug, "author": author}, identitykey.Build("medium", identitykey.EntityArticle, slug)),
		})
	}

	// Author candidate when /@username extracted.
	if author != "" {
		out = append(out, lateral.Candidate{
			URL:           "https://medium.com/@" + author,
			CandidateType: CandidateTypeAuthor,
			Strategy:      IDParent,
			Preview:       identitykey.Set(map[string]any{"username": author}, identitykey.Build("medium", identitykey.EntityProfile, author)),
		})
	}

	// Feed candidate (RSS) — Medium exposes /feed/<host_or_handle>.
	feedID := host
	if strings.HasSuffix(host, ".medium.com") {
		feedID = strings.TrimSuffix(host, ".medium.com")
	}
	out = append(out, lateral.Candidate{
		URL:           "https://medium.com/feed/" + feedID,
		CandidateType: CandidateTypeFeed,
		Strategy:      IDParent,
		Preview:       identitykey.Set(map[string]any{"feed_id": feedID}, identitykey.Build("medium", "feed", feedID)),
	})

	return out, nil
}
