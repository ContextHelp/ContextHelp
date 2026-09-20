package google

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// TrendsStrategy handles trends.google.com captures.
//
// Surfaces:
//
//   - /trends/explore?q=<topic>   topic page → trend candidate
//   - /trends/trendingsearches    rising-now → trend candidate
type TrendsStrategy struct {
	Client     GoogleClient
	RelatedCap int // maximum number of related-topic candidates; default 10
}

func NewTrends(c GoogleClient) *TrendsStrategy {
	return &TrendsStrategy{Client: c, RelatedCap: 10}
}

func (*TrendsStrategy) ID() string                     { return IDTrends }
func (*TrendsStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*TrendsStrategy) Preconditions() []string        { return nil }

func (*TrendsStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	if host == "trends.google.com" || strings.HasSuffix(host, ".trends.google.com") {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	return lateral.AppliesResult{}
}

func (s *TrendsStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	apex := "https://trends.google.com"
	q := u.Query()
	if topic := q.Get("q"); topic != "" {
		out := []lateral.Candidate{{
			URL:           apex + "/trends/explore?q=" + url.QueryEscape(topic),
			CandidateType: CandidateTypeTrend,
			Strategy:      IDTrends,
			IdentityKey:   identitykey.Build("trends", identitykey.EntityTrend, topic),
			Preview:       map[string]any{"topic": topic},
		}}
		if s.Client != nil {
			limit := s.RelatedCap
			if limit <= 0 {
				limit = 10
			}
			if related, err := s.Client.TrendsRelated(ctx, topic); err == nil {
				added := 0
				for _, r := range related {
					if added >= limit {
						break
					}
					if strings.TrimSpace(r) == "" {
						continue
					}
					out = append(out, lateral.Candidate{
						URL:           apex + "/trends/explore?q=" + url.QueryEscape(r),
						CandidateType: CandidateTypeTrend,
						Strategy:      IDTrends,
						IdentityKey:   identitykey.Build("trends", identitykey.EntityTrend, r),
						Preview:       map[string]any{"topic": r, "related_to": topic},
					})
					added++
				}
			}
		}
		return out, nil
	}
	return nil, nil
}
