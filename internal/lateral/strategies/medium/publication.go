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

func (*PublicationStrategy) ID() string                     { return IDPublication }
func (*PublicationStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*PublicationStrategy) Preconditions() []string        { return nil }

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
		// resolved indicates whether slug is a real Medium publication
		// slug (suitable for medium.com/<slug> canonical URLs). When
		// false, we emit the custom-domain root URL instead of forcing
		// the host into the medium.com/ path (which produces invalid
		// URLs like medium.com/newsletter.example.com).
		var resolved bool
		if s.Client != nil {
			if r, err := s.Client.ResolvePublication(ctx, host); err == nil && r != "" {
				slug = r
				resolved = true
			}
		}
		if !resolved {
			// Degraded mode: keep the custom-domain root as the
			// canonical surface and key the identity by host.
			root := "https://" + host + "/"
			pubKey := identitykey.Build("medium", identitykey.EntityPublication, host)
			return []lateral.Candidate{
				{
					URL:           root,
					CandidateType: CandidateTypePublication,
					Strategy:      IDPublication,
					IdentityKey:   pubKey,
					Preview:       map[string]any{"host": host},
				},
				{
					URL:           root + "archive",
					CandidateType: CandidateTypePublication,
					Strategy:      IDPublication,
					// Reuse the publication identity key; differentiate
					// the facet via Preview so the resolver still dedups
					// to the same entity.
					IdentityKey: pubKey,
					Preview:     map[string]any{"host": host, "facet": "archive"},
				},
				{
					URL:           root + "feed",
					CandidateType: CandidateTypeFeed,
					Strategy:      IDPublication,
					IdentityKey:   identitykey.Build("medium", "feed", host),
					Preview:       map[string]any{"host": host},
				},
			}, nil
		}
	default:
		return nil, nil
	}

	pubKey := identitykey.Build("medium", identitykey.EntityPublication, slug)
	out := []lateral.Candidate{
		{
			URL:           "https://medium.com/" + slug,
			CandidateType: CandidateTypePublication,
			Strategy:      IDPublication,
			IdentityKey:   pubKey,
			Preview:       map[string]any{"slug": slug},
		},
		{
			URL:           "https://medium.com/" + slug + "/archive",
			CandidateType: CandidateTypePublication,
			Strategy:      IDPublication,
			// Reuse the publication identity key so the resolver
			// collapses landing + archive onto the same entity; the
			// facet is differentiated via Preview.
			IdentityKey: pubKey,
			Preview:     map[string]any{"slug": slug, "facet": "archive"},
		},
		{
			URL:           "https://medium.com/feed/" + slug,
			CandidateType: CandidateTypeFeed,
			Strategy:      IDPublication,
			IdentityKey:   identitykey.Build("medium", "feed", slug),
			Preview:       map[string]any{"slug": slug},
		},
	}
	return out, nil
}
