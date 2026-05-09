package github

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Strategy IDs. Exported because daemon wiring + tests reference them.
const (
	StrategyIDGitHub           = "github"
	StrategyIDGist             = "github.gist"
	StrategyIDSecurityAdvisory = "github.advisory"
)

// PreviewKeyIdentityKey is the well-known key under which github strategies
// surface a candidate's canonical identity key (e.g. "@github.user.jadb").
// Substrate adapters lift this into identity.Candidate.IdentityKey before
// invoking the resolver.
const PreviewKeyIdentityKey = "identity_key"

// Specificity scores for the github family. Children must score strictly
// higher than the parent so the platform-family dispatch picks the most
// specific match. See lateral.Registry.Dispatch for the rule.
const (
	SpecificityParent = 1
	SpecificityChild  = 2
)

// Candidate types emitted by github strategies. Listed here so threshold/
// cap_k config can target them by name without scattering string literals.
const (
	TypeSiblingRepo  = "sibling_repo"
	TypeOwnerProfile = "owner_profile"
	TypePinnedRepo   = "pinned_repo"
	TypeStarredRepo  = "starred_repo"
	TypeSponsorPage  = "sponsor_page"
	TypeAuthorPR     = "author_other_pr"
	TypeReviewer     = "pr_reviewer"
	TypeAuthorIssue  = "author_other_issue"
	TypeRepoIssue    = "repo_issue"
	TypeIssueLabel   = "issue_label"
	TypeOwnedRepo    = "owned_repo"
	TypeSponsored    = "sponsored_profile"
	TypeContribOrg   = "contribution_org"
	TypeSimilarSpons = "similar_sponsor"
	TypeOwnerGist    = "owner_gist"
	TypeRelatedGist  = "related_gist"
	TypeAdvisoryRepo = "advisory_repo"
	TypeSimilarAdv   = "similar_advisory"
)

// GitHubStrategy is the parent platform-keyed strategy for github.com. It
// dispatches sub-path probes (repo / PR / issue / profile / sponsor) based
// on URL shape. The Probe pipeline is wired in later tasks; the skeleton
// here ships ID/Family/Applies so the registry has something to bind.
//
// Children (GistStrategy, SecurityAdvisoryStrategy) live in this same
// package and report higher specificity so the dispatcher prefers them
// when their narrower match holds.
type GitHubStrategy struct {
	deps Dependencies
}

// Dependencies bundles the collaborators a fully-wired GitHubStrategy
// needs at probe time. The skeleton only stores them; later sub-path
// tasks consume them. Zero value is valid for Applies-only use.
type Dependencies struct {
	Fetcher   Fetcher
	APIClient APIClient
}

// NewGitHubStrategy constructs a strategy with the given dependencies.
// Pass a zero Dependencies for skeleton-only registration.
func NewGitHubStrategy(deps Dependencies) *GitHubStrategy {
	return &GitHubStrategy{deps: deps}
}

// ID implements lateral.LateralStrategy.
func (s *GitHubStrategy) ID() string { return StrategyIDGitHub }

// Family implements lateral.LateralStrategy. github strategies are platform-
// keyed (mutually-exclusive specializations within the github tree).
func (s *GitHubStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }

// Preconditions implements lateral.LateralStrategy. github strategies need
// the parent record's namespace fields populated; v1 ships an empty list
// (substrate gates on the canonical record's existence, not on specific
// field presence).
func (s *GitHubStrategy) Preconditions() []string { return nil }

// Applies returns Matches=true when the captured event's SourceURL is on
// github.com (case-insensitive) and is NOT on the gist subdomain (which
// GistStrategy claims at higher specificity) and is NOT an advisory page
// (which SecurityAdvisoryStrategy claims). Declining advisory URLs at
// the parent lets the registry fall through to JIT when the advisory
// child is disabled — without it, the parent claims the URL, probes
// produce no candidates (classifier excludes /advisories/*), and JIT
// never fires. Specificity is fixed at SpecificityParent.
func (s *GitHubStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	if !isGitHubURL(ev.SourceURL) {
		return lateral.AppliesResult{}
	}
	if isAdvisoryURL(ev.SourceURL) {
		return lateral.AppliesResult{}
	}
	return lateral.AppliesResult{Matches: true, Specificity: SpecificityParent}
}

// isGitHubURL reports whether u is on github.com proper (excluding the
// gist subdomain). www.github.com is treated as github.com.
func isGitHubURL(raw string) bool {
	host, ok := hostOf(raw)
	if !ok {
		return false
	}
	host = strings.ToLower(host)
	if host == "github.com" || host == "www.github.com" {
		return true
	}
	return false
}

// hostOf parses raw and returns its lowercase host without port. Returns
// ok=false on parse failure or empty host.
func hostOf(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	h := u.Hostname()
	if h == "" {
		return "", false
	}
	return strings.ToLower(h), true
}

// Probe implements lateral.LateralStrategy. The skeleton returns an empty
// candidate set; sub-path probes (T-0255..T-0259) fan out the real work
// through the package-internal pipeline.
func (s *GitHubStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, ac lateral.ActiveContext) ([]lateral.Candidate, error) {
	pt := classifyPath(ev.SourceURL)
	switch pt {
	case pageTypeRepo:
		return s.probeRepo(ctx, ev, ac)
	case pageTypePullRequest:
		return s.probePR(ctx, ev, ac)
	case pageTypeIssue:
		return s.probeIssue(ctx, ev, ac)
	case pageTypeProfile:
		return s.probeProfile(ctx, ev, ac)
	case pageTypeSponsor:
		return s.probeSponsor(ctx, ev, ac)
	default:
		return nil, nil
	}
}

// pageType is the classifier output for github.com URL paths. The
// classifier is path-structural; pageshape is not consulted because the
// github surface is fully specified by URL shape.
type pageType int

const (
	pageTypeUnknown pageType = iota
	pageTypeRepo
	pageTypePullRequest
	pageTypeIssue
	pageTypeProfile
	pageTypeSponsor
)

// classifyPath returns the pageType implied by raw's path. Reserved
// top-level segments (orgs, settings, marketplace, etc.) classify as
// pageTypeUnknown so the strategy doesn't probe them.
func classifyPath(raw string) pageType {
	u, err := url.Parse(raw)
	if err != nil {
		return pageTypeUnknown
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return pageTypeUnknown
	}
	parts := strings.Split(p, "/")
	if len(parts) == 0 {
		return pageTypeUnknown
	}
	// /sponsors/<login>
	if parts[0] == "sponsors" && len(parts) >= 2 {
		return pageTypeSponsor
	}
	// Reserved top-level segments that are not user/org profiles or repos.
	if isReservedTopLevel(parts[0]) {
		return pageTypeUnknown
	}
	switch len(parts) {
	case 1:
		// /<login> — profile
		return pageTypeProfile
	case 2:
		// /<owner>/<repo>
		return pageTypeRepo
	default:
		// /<owner>/<repo>/<verb>/...
		switch parts[2] {
		case "pull", "pulls":
			return pageTypePullRequest
		case "issues":
			return pageTypeIssue
		default:
			return pageTypeRepo
		}
	}
}

// isReservedTopLevel reports whether seg is a known github.com top-level
// path that is not a user/org login or a repo namespace.
func isReservedTopLevel(seg string) bool {
	switch strings.ToLower(seg) {
	case "settings", "marketplace", "explore", "topics", "trending",
		"collections", "events", "notifications", "search", "login",
		"join", "logout", "pricing", "features", "enterprise", "about",
		"site", "security", "orgs", "organizations", "new", "codespaces",
		"discussions", "pulls", "issues", "watching", "stars",
		"home", "dashboard", "advisories":
		return true
	}
	return false
}
