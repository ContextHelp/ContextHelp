package google

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// SearchStrategy claims captures of google.com/search and emits the
// query as a candidate plus the top organic results when the daemon
// client supplies them.
type SearchStrategy struct {
	Client    GoogleClient
	ResultCap int // maximum number of result-URL candidates; default 5
}

func NewSearch(c GoogleClient) *SearchStrategy { return &SearchStrategy{Client: c, ResultCap: 5} }

func (*SearchStrategy) ID() string                    { return IDSearch }
func (*SearchStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*SearchStrategy) Preconditions() []string       { return nil }

// Applies returns specificity 3 on google.com/search; 0 otherwise.
func (*SearchStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return lateral.AppliesResult{}
	}
	if (host == "google.com" || strings.HasSuffix(host, ".google.com")) && strings.HasPrefix(u.Path, "/search") {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	return lateral.AppliesResult{}
}

func (s *SearchStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	q := u.Query().Get("q")
	if q == "" {
		return nil, nil
	}
	out := []lateral.Candidate{{
		URL:           "https://www.google.com/search?q=" + url.QueryEscape(q),
		CandidateType: CandidateTypeQuery,
		Strategy:      IDSearch,
		IdentityKey:   identitykey.Build("google", identitykey.EntitySearch, q),
		Preview:       map[string]any{"query": q},
	}}
	if s.Client != nil {
		limit := s.ResultCap
		if limit <= 0 {
			limit = 5
		}
		urls, err := s.Client.SearchTopResults(ctx, q, limit)
		if err == nil {
			for _, ru := range urls {
				// Skip URLs that don't yield host-backed id parts;
				// otherwise the result identity key collapses to `q|`
				// and dedups against every other invalid result.
				hbParts := identitykey.HostBackedIDParts(ru)
				if hbParts == nil {
					continue
				}
				out = append(out, lateral.Candidate{
					URL:           ru,
					CandidateType: CandidateTypeResult,
					Strategy:      IDSearch,
					IdentityKey:   identitykey.Build("google", identitykey.EntitySearch, q+"|"+strings.Join(hbParts, "/")),
					Preview:       map[string]any{"query": q, "result_url": ru},
				})
			}
		}
	}
	return out, nil
}
