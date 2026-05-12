package google

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// ParentStrategy is the GoogleStrategy catch-all. It handles google.com
// captures that none of the child strategies (Search, Scholar, Trends,
// News) claim.
type ParentStrategy struct {
	Client GoogleClient
}

// NewParent constructs the catch-all.
func NewParent(c GoogleClient) *ParentStrategy { return &ParentStrategy{Client: c} }

func (*ParentStrategy) ID() string                    { return IDParent }
func (*ParentStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*ParentStrategy) Preconditions() []string       { return nil }

// Applies — apex google.com scores 1, subdomains score 2. Children
// always score higher (3) so dispatcher prefers them within the platform
// family.
func (*ParentStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	if !isGoogleHost(host) {
		return lateral.AppliesResult{}
	}
	if host == "google.com" {
		return lateral.AppliesResult{Matches: true, Specificity: 1}
	}
	return lateral.AppliesResult{Matches: true, Specificity: 2}
}

// Probe emits a minimal generic candidate keyed by host+path so the
// resolver can dedup repeated captures of the same Google surface even
// when no specialised child handles it.
func (*ParentStrategy) Probe(_ context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(u.Hostname())
	path := strings.TrimPrefix(u.Path, "/")
	idParts := []string{host}
	if path != "" {
		for _, seg := range strings.Split(path, "/") {
			if seg != "" {
				idParts = append(idParts, seg)
			}
		}
	}
	return []lateral.Candidate{{
		URL:           ev.SourceURL,
		CandidateType: CandidateTypeGeneric,
		Strategy:      IDParent,
		IdentityKey:   identitykey.Build("google", "page", idParts...),
		Preview:       map[string]any{"host": host, "path": path},
	}}, nil
}
