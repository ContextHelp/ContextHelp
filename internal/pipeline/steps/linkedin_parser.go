package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/importer/linkedin"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// LinkedInPostsParser parses a LinkedIn Posts.csv export from draft.RawContent
// and populates draft.Metadata["feed_items"] with one item per post: its
// rendered text as content and its dedupe key as guid and source (a post
// has no URL of its own; SharedURL is what it links to).
type LinkedInPostsParser struct {
	pipeline.BaseContract
}

// NewLinkedInPostsParser creates a LinkedInPostsParser.
func NewLinkedInPostsParser() *LinkedInPostsParser {
	return &LinkedInPostsParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *LinkedInPostsParser) Name() string { return "linkedin_posts_parser" }

func (s *LinkedInPostsParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	if draft.RawContent == "" {
		draft.Metadata["feed_items"] = []map[string]any{}
		return draft, nil
	}

	posts, err := linkedin.ParsePosts(draft.RawContent)
	if err != nil {
		return nil, fmt.Errorf("linkedin_posts_parser: %w", err)
	}

	items := make([]map[string]any, 0, len(posts))
	for _, p := range posts {
		key := linkedin.PostDedupeKey(p)
		item := map[string]any{
			"title":                p.ShareCommentary,
			"content":              linkedin.RenderPost(p),
			"link":                 p.SharedURL,
			"source":               key,
			"guid":                 key,
			"share_commentary":     p.ShareCommentary,
			"share_media_category": p.ShareMediaCategory,
			"shared_url":           p.SharedURL,
			"dedupe_key":           key,
		}
		if !p.Date.IsZero() {
			item["date"] = p.Date
		}
		items = append(items, item)
	}

	draft.Metadata["feed_items"] = items
	return draft, nil
}

// LinkedInArticlesParser parses a LinkedIn Articles.csv export from draft.RawContent
// and populates draft.Metadata["feed_items"] with one item per article: its
// rendered text as content, its URL as link and source (the dedupe key when
// it has none), and its dedupe key as guid.
type LinkedInArticlesParser struct {
	pipeline.BaseContract
}

// NewLinkedInArticlesParser creates a LinkedInArticlesParser.
func NewLinkedInArticlesParser() *LinkedInArticlesParser {
	return &LinkedInArticlesParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *LinkedInArticlesParser) Name() string { return "linkedin_articles_parser" }

func (s *LinkedInArticlesParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	if draft.RawContent == "" {
		draft.Metadata["feed_items"] = []map[string]any{}
		return draft, nil
	}

	articles, err := linkedin.ParseArticles(draft.RawContent)
	if err != nil {
		return nil, fmt.Errorf("linkedin_articles_parser: %w", err)
	}

	items := make([]map[string]any, 0, len(articles))
	for _, a := range articles {
		key := linkedin.ArticleDedupeKey(a)
		source := a.URL
		if source == "" {
			source = key
		}
		item := map[string]any{
			"title":      a.Title,
			"content":    linkedin.RenderArticle(a),
			"link":       a.URL,
			"source":     source,
			"guid":       key,
			"url":        a.URL,
			"summary":    a.Summary,
			"dedupe_key": key,
		}
		if !a.PublishedAt.IsZero() {
			item["published_at"] = a.PublishedAt
		}
		items = append(items, item)
	}

	draft.Metadata["feed_items"] = items
	return draft, nil
}
