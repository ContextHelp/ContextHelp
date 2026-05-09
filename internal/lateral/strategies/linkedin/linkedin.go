// Package linkedin implements the LinkedIn lateral platform strategy.
// It claims captures hosted on linkedin.com and emits sub-path probe
// candidates for users (/in/<slug>), companies (/company/<slug>),
// schools (/school/<slug>), and posts (/posts/<...>, /pulse/<...>).
//
// LinkedIn aggressively blocks unauthenticated scraping, so probes here
// represent the canonical surfaces a daemon-side LinkedInClient would
// resolve via API access (or via the user's logged-in browser when
// integrated with capture). The strategy itself does no fetching.
package linkedin

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

const ID = "LinkedInStrategy"

const (
	CandidateTypeProfile = "linkedin_profile"
	CandidateTypeOrg     = "linkedin_org"
	CandidateTypeSchool  = "linkedin_school"
	CandidateTypePost    = "linkedin_post"
	CandidateTypeArticle = "linkedin_article"
	CandidateTypeRecent  = "linkedin_recent"
)

// Strategy implements lateral.LateralStrategy for LinkedIn. It is
// purely URL-structural; identity keys are slug-keyed.
type Strategy struct{}

// New constructs a Strategy.
func New() *Strategy { return &Strategy{} }

func (*Strategy) ID() string                    { return ID }
func (*Strategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*Strategy) Preconditions() []string       { return nil }

// Applies matches linkedin.com (apex + any subdomain like
// www.linkedin.com or business.linkedin.com).
func (*Strategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	if host == "" {
		return lateral.AppliesResult{}
	}
	if host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com") {
		spec := 1
		if host != "linkedin.com" {
			spec++
		}
		return lateral.AppliesResult{Matches: true, Specificity: spec}
	}
	return lateral.AppliesResult{}
}

func (s *Strategy) Probe(_ context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	parts := pathSegments(u.Path)
	if len(parts) == 0 {
		return nil, nil
	}

	apex := "https://www.linkedin.com"
	var out []lateral.Candidate

	switch parts[0] {
	case "in":
		if len(parts) < 2 {
			return nil, nil
		}
		slug := parts[1]
		out = append(out,
			lateral.Candidate{
				URL:           apex + "/in/" + slug,
				CandidateType: CandidateTypeProfile,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("linkedin", identitykey.EntityProfile, slug)),
			},
			lateral.Candidate{
				URL:           apex + "/in/" + slug + "/recent-activity/all/",
				CandidateType: CandidateTypeRecent,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("linkedin", identitykey.EntityProfile, slug+"/recent")),
			},
		)
	case "company":
		if len(parts) < 2 {
			return nil, nil
		}
		slug := parts[1]
		out = append(out,
			lateral.Candidate{
				URL:           apex + "/company/" + slug,
				CandidateType: CandidateTypeOrg,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("linkedin", identitykey.EntityOrg, slug)),
			},
			lateral.Candidate{
				URL:           apex + "/company/" + slug + "/people/",
				CandidateType: CandidateTypeOrg,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"slug": slug, "facet": "people"}, identitykey.Build("linkedin", identitykey.EntityOrg, slug+"/people")),
			},
			lateral.Candidate{
				URL:           apex + "/company/" + slug + "/posts/",
				CandidateType: CandidateTypeOrg,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"slug": slug, "facet": "posts"}, identitykey.Build("linkedin", identitykey.EntityOrg, slug+"/posts")),
			},
		)
	case "school":
		if len(parts) < 2 {
			return nil, nil
		}
		slug := parts[1]
		out = append(out, lateral.Candidate{
			URL:           apex + "/school/" + slug,
			CandidateType: CandidateTypeSchool,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("linkedin", identitykey.EntityOrg, "school_"+slug)),
		})
	case "posts":
		// /posts/<author-slug>_<activity-id>.
		if len(parts) < 2 {
			return nil, nil
		}
		raw := parts[1]
		author := raw
		if i := strings.LastIndex(raw, "_"); i > 0 {
			author = raw[:i]
		}
		// author may itself be activity-id-like; rather than guess,
		// attribute the post to author and emit a profile probe.
		out = append(out,
			lateral.Candidate{
				URL:           apex + "/posts/" + raw,
				CandidateType: CandidateTypePost,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"id": raw}, identitykey.Build("linkedin", identitykey.EntityPost, raw)),
			},
			lateral.Candidate{
				URL:           apex + "/in/" + author,
				CandidateType: CandidateTypeProfile,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"slug": author}, identitykey.Build("linkedin", identitykey.EntityProfile, author)),
			},
		)
	case "pulse":
		if len(parts) < 2 {
			return nil, nil
		}
		slug := parts[1]
		out = append(out, lateral.Candidate{
			URL:           apex + "/pulse/" + slug,
			CandidateType: CandidateTypeArticle,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"slug": slug}, identitykey.Build("linkedin", identitykey.EntityArticle, slug)),
		})
	}

	return out, nil
}

func hostOf(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func pathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}
