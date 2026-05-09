package github

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// SecurityAdvisoryStrategy is the child platform-keyed strategy for
// github advisory pages. It matches two surfaces at higher specificity
// than the GitHub parent so the dispatcher prefers it for advisory
// captures:
//
//  1. Per-repo advisories: /<owner>/<repo>/security/advisories[/<ghsa>]
//  2. Global database:    /advisories[/<ghsa>]
//
// Lateral surface:
//
//   - advisory_repo    — the affected repo (per-repo surface)
//   - similar_advisory — recent advisories in the same ecosystem/severity
//                        (when the global advisory DB is the capture
//                        source, similar = recent)
//   - owner_profile    — repo owner (per-repo surface)
type SecurityAdvisoryStrategy struct {
	deps Dependencies
}

// NewSecurityAdvisoryStrategy constructs a strategy with the given
// dependencies.
func NewSecurityAdvisoryStrategy(deps Dependencies) *SecurityAdvisoryStrategy {
	return &SecurityAdvisoryStrategy{deps: deps}
}

// ID implements lateral.LateralStrategy.
func (s *SecurityAdvisoryStrategy) ID() string { return StrategyIDSecurityAdvisory }

// Family implements lateral.LateralStrategy.
func (s *SecurityAdvisoryStrategy) Family() lateral.StrategyFamily {
	return lateral.FamilyPlatform
}

// Preconditions implements lateral.LateralStrategy.
func (s *SecurityAdvisoryStrategy) Preconditions() []string { return nil }

// Applies returns Matches=true for either advisory surface; specificity
// is SpecificityChild (= 2) so the parent GitHubStrategy is shadowed.
//
// Per-repo advisory paths: /<owner>/<repo>/security/advisories... — the
// 3rd path segment is "security" and the 4th is "advisories".
//
// Global advisory db: hostname is github.com (or www.github.com) and the
// path begins with /advisories.
func (s *SecurityAdvisoryStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	if !isAdvisoryURL(ev.SourceURL) {
		return lateral.AppliesResult{}
	}
	return lateral.AppliesResult{Matches: true, Specificity: SpecificityChild}
}

// isAdvisoryURL reports whether raw matches one of the two advisory
// surfaces.
func isAdvisoryURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return false
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return false
	}
	parts := strings.Split(p, "/")
	// /advisories[/<ghsa>]
	if parts[0] == "advisories" {
		return true
	}
	// /<owner>/<repo>/security/advisories[/<ghsa>]
	if len(parts) >= 4 && parts[2] == "security" && parts[3] == "advisories" {
		return true
	}
	return false
}

// Probe emits advisory-flavoured candidates.
func (s *SecurityAdvisoryStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	surface, owner, repo, ghsa := parseAdvisoryURL(ev.SourceURL)
	if surface == advisoryNone {
		return nil, nil
	}

	var out []lateral.Candidate

	if surface == advisoryRepo {
		out = append(out,
			ownerProfileCandidate(owner, s.ID()),
			advisoryRepoCandidate(owner, repo, s.ID()),
		)
	}

	if s.deps.APIClient == nil {
		return out, nil
	}

	// Similar advisories: query the global advisory db.
	const limit = 10
	if advs, err := s.deps.APIClient.ListGlobalAdvisories(ctx, "", "", limit); err != nil {
		recordSubpathFailure(s.ID(), ev.ObjectID, "list_global_advisories", err)
	} else {
		for _, a := range advs {
			if a.GHSAID == ghsa && ghsa != "" {
				continue
			}
			out = append(out, advisoryCandidate(a, TypeSimilarAdv, s.ID()))
		}
	}

	return out, nil
}

// advisorySurface tags which advisory surface a captured URL came from.
type advisorySurface int

const (
	advisoryNone advisorySurface = iota
	advisoryRepo
	advisoryGlobal
)

// parseAdvisoryURL extracts the surface and (owner,repo,ghsa) fields
// where applicable. ghsa may be empty (list-view captures).
func parseAdvisoryURL(raw string) (advisorySurface, string, string, string) {
	u, err := url.Parse(raw)
	if err != nil {
		return advisoryNone, "", "", ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return advisoryNone, "", "", ""
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return advisoryNone, "", "", ""
	}
	parts := strings.Split(p, "/")
	if parts[0] == "advisories" {
		ghsa := ""
		if len(parts) >= 2 {
			ghsa = parts[1]
		}
		return advisoryGlobal, "", "", ghsa
	}
	if len(parts) >= 4 && parts[2] == "security" && parts[3] == "advisories" {
		ghsa := ""
		if len(parts) >= 5 {
			ghsa = parts[4]
		}
		return advisoryRepo, parts[0], parts[1], ghsa
	}
	return advisoryNone, "", "", ""
}

// advisoryRepoCandidate builds a candidate for the affected repo from
// a per-repo advisory page.
func advisoryRepoCandidate(owner, repo, strategyID string) lateral.Candidate {
	return lateral.Candidate{
		URL:           repoURL(owner, repo),
		CandidateType: TypeAdvisoryRepo,
		Strategy:      strategyID,
		Preview: map[string]any{
			PreviewKeyIdentityKey: repoIdentityKey(owner, repo),
			"owner":               owner,
			"name":                repo,
		},
	}
}

// advisoryCandidate builds a similar_advisory candidate.
func advisoryCandidate(a AdvisorySummary, candidateType, strategyID string) lateral.Candidate {
	url := a.URL
	if url == "" && a.GHSAID != "" {
		url = "https://github.com/advisories/" + a.GHSAID
	}
	return lateral.Candidate{
		URL:           url,
		CandidateType: candidateType,
		Strategy:      strategyID,
		Preview: map[string]any{
			"ghsa_id":      a.GHSAID,
			"summary":      a.Summary,
			"severity":     a.Severity,
			"ecosystem":    a.Ecosystem,
			"package_name": a.PackageName,
		},
	}
}
