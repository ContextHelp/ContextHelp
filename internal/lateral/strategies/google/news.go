package google

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// NewsStrategy handles news.google.com captures.
//
// Surfaces:
//
//   - /articles/<id>          article         → article candidate
//   - /topics/<id>            topic feed      → topic candidate (top articles via client)
type NewsStrategy struct {
	Client      GoogleClient
	ArticleCap  int // top articles emitted from a topic capture; default 5
}

func NewNews(c GoogleClient) *NewsStrategy { return &NewsStrategy{Client: c, ArticleCap: 5} }

func (*NewsStrategy) ID() string                    { return IDNews }
func (*NewsStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*NewsStrategy) Preconditions() []string       { return nil }

func (*NewsStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	if host == "news.google.com" || strings.HasSuffix(host, ".news.google.com") {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	return lateral.AppliesResult{}
}

func (s *NewsStrategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	parts := pathSegments(u.Path)
	if len(parts) < 2 {
		return nil, nil
	}
	apex := "https://news.google.com"
	switch parts[0] {
	case "articles":
		id := parts[1]
		return []lateral.Candidate{{
			URL:           apex + "/articles/" + id,
			CandidateType: CandidateTypeArticle,
			Strategy:      IDNews,
			Preview:       identitykey.Set(map[string]any{"article_id": id}, identitykey.Build("news.google", identitykey.EntityArticle, id)),
		}}, nil
	case "topics":
		id := parts[1]
		out := []lateral.Candidate{{
			URL:           apex + "/topics/" + id,
			CandidateType: CandidateTypeTopic,
			Strategy:      IDNews,
			Preview:       identitykey.Set(map[string]any{"topic_id": id}, identitykey.Build("news.google", identitykey.EntityTopic, id)),
		}}
		if s.Client != nil {
			limit := s.ArticleCap
			if limit <= 0 {
				limit = 5
			}
			if articles, err := s.Client.NewsTopicArticles(ctx, id, limit); err == nil {
				for _, au := range articles {
					out = append(out, lateral.Candidate{
						URL:           au,
						CandidateType: CandidateTypeArticle,
						Strategy:      IDNews,
						Preview:       identitykey.Set(map[string]any{"topic_id": id, "article_url": au}, identitykey.Build("news.google", identitykey.EntityArticle, id+"|"+identitykey.HostBackedID(au))),
					})
				}
			}
		}
		return out, nil
	}
	return nil, nil
}
