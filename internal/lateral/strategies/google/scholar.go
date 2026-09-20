package google

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// ScholarStrategy handles scholar.google.com captures.
//
// Surfaces:
//
//   - /scholar?q=<query>      query result page → candidate query
//   - /citations?user=<id>    author profile → author candidate
//   - /scholar?cluster=<id>   paper cluster   → paper candidate
type ScholarStrategy struct {
	Client GoogleClient
}

func NewScholar(c GoogleClient) *ScholarStrategy { return &ScholarStrategy{Client: c} }

func (*ScholarStrategy) ID() string                     { return IDScholar }
func (*ScholarStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*ScholarStrategy) Preconditions() []string        { return nil }

func (*ScholarStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	if host == "scholar.google.com" || strings.HasSuffix(host, ".scholar.google.com") {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	return lateral.AppliesResult{}
}

func (s *ScholarStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	apex := "https://scholar.google.com"
	switch {
	case u.Path == "/citations":
		uid := q.Get("user")
		if uid == "" {
			return nil, nil
		}
		return []lateral.Candidate{{
			URL:           apex + "/citations?user=" + uid,
			CandidateType: CandidateTypeAuthor,
			Strategy:      IDScholar,
			IdentityKey:   identitykey.Build("scholar", identitykey.EntityProfile, uid),
			Preview:       map[string]any{"user_id": uid},
		}}, nil
	case u.Path == "/scholar":
		if cluster := q.Get("cluster"); cluster != "" {
			return []lateral.Candidate{{
				URL:           apex + "/scholar?cluster=" + cluster,
				CandidateType: CandidateTypePaper,
				Strategy:      IDScholar,
				IdentityKey:   identitykey.Build("scholar", identitykey.EntityPaper, cluster),
				Preview:       map[string]any{"cluster_id": cluster},
			}}, nil
		}
		if query := q.Get("q"); query != "" {
			out := []lateral.Candidate{{
				URL:           apex + "/scholar?q=" + url.QueryEscape(query),
				CandidateType: CandidateTypeQuery,
				Strategy:      IDScholar,
				IdentityKey:   identitykey.Build("scholar", identitykey.EntitySearch, query),
				Preview:       map[string]any{"query": query},
			}}
			if s.Client != nil {
				if author, err := s.Client.ScholarAuthor(ctx, query); err == nil && author != "" {
					hbParts := identitykey.HostBackedIDParts(author)
					if hbParts == nil {
						return out, nil
					}
					out = append(out, lateral.Candidate{
						URL:           author,
						CandidateType: CandidateTypeAuthor,
						Strategy:      IDScholar,
						IdentityKey:   identitykey.Build("scholar", identitykey.EntityProfile, hbParts...),
						Preview:       map[string]any{"author_url": author},
					})
				}
			}
			return out, nil
		}
	}
	return nil, nil
}
