// Package google implements the GoogleStrategy parent and its
// children: Search, Scholar, Trends, News.
//
// # Family layout
//
//	GoogleStrategy        — catch-all on google.com, .google.com, *.gle
//	GoogleSearchStrategy  — google.com/search
//	GoogleScholarStrategy — scholar.google.com
//	GoogleTrendsStrategy  — trends.google.com
//	GoogleNewsStrategy    — news.google.com
//
// All five live in this package because they share host-parsing
// helpers, the parent-vs-child specificity calibration, and a single
// GoogleClient interface (different methods per child).
//
// # Specificity calibration
//
//   - Parent (apex google.com): 1
//   - Parent (any *.google.com subdomain not handled by a child): 2
//   - Search child (google.com/search): 3
//   - Scholar / Trends / News children (their dedicated subdomains): 3
//
// So the dispatcher's "highest-specificity wins within platform family"
// always picks the child when one applies.
package google

import (
	"context"
	"net/url"
	"strings"
)

const (
	IDParent  = "GoogleStrategy"
	IDSearch  = "GoogleSearchStrategy"
	IDScholar = "GoogleScholarStrategy"
	IDTrends  = "GoogleTrendsStrategy"
	IDNews    = "GoogleNewsStrategy"
)

const (
	CandidateTypeQuery   = "google_query"
	CandidateTypeResult  = "google_result"
	CandidateTypeAuthor  = "scholar_author"
	CandidateTypePaper   = "scholar_paper"
	CandidateTypeTrend   = "trends_topic"
	CandidateTypeArticle = "news_article"
	CandidateTypeTopic   = "news_topic"
	CandidateTypeGeneric = "google_generic"
)

// GoogleClient is the daemon-side fetcher interface. Concrete adapters
// implement only the methods their child needs; nil values are
// tolerated by the strategies (they degrade to URL-only candidates).
type GoogleClient interface {
	// SearchTopResults returns the top organic results for query.
	SearchTopResults(ctx context.Context, query string, limit int) ([]string, error)
	// ScholarAuthor returns the author landing page URL for a paper id.
	ScholarAuthor(ctx context.Context, paperID string) (authorURL string, err error)
	// TrendsRelated returns related queries for a trend.
	TrendsRelated(ctx context.Context, topic string) ([]string, error)
	// NewsTopicArticles returns top article URLs for a news topic id.
	NewsTopicArticles(ctx context.Context, topicID string, limit int) ([]string, error)
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

// isGoogleHost reports whether host is *.google.com (any subdomain) or
// google.com itself. The .gle short-domain is also accepted.
func isGoogleHost(host string) bool {
	return host == "google.com" || strings.HasSuffix(host, ".google.com") || strings.HasSuffix(host, ".gle")
}
