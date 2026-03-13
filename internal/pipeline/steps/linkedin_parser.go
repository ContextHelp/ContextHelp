package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/importer/linkedin"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// LinkedInPostsParser parses a LinkedIn Posts.csv export from draft.RawContent
// and populates draft.Metadata["linkedin_posts"] with normalised post records.
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
		draft.Metadata["linkedin_posts"] = []map[string]any{}
		return draft, nil
	}

	posts, err := linkedin.ParsePosts(draft.RawContent)
	if err != nil {
		return nil, fmt.Errorf("linkedin_posts_parser: %w", err)
	}

	items := make([]map[string]any, 0, len(posts))
	for _, p := range posts {
		item := map[string]any{
			"share_commentary":     p.ShareCommentary,
			"share_media_category": p.ShareMediaCategory,
			"shared_url":           p.SharedURL,
			"dedupe_key":           linkedin.PostDedupeKey(p),
		}
		if !p.Date.IsZero() {
			item["date"] = p.Date
		}
		items = append(items, item)
	}

	draft.Metadata["linkedin_posts"] = items
	return draft, nil
}

// LinkedInArticlesParser parses a LinkedIn Articles.csv export from draft.RawContent
// and populates draft.Metadata["linkedin_articles"] with normalised article records.
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
		draft.Metadata["linkedin_articles"] = []map[string]any{}
		return draft, nil
	}

	articles, err := linkedin.ParseArticles(draft.RawContent)
	if err != nil {
		return nil, fmt.Errorf("linkedin_articles_parser: %w", err)
	}

	items := make([]map[string]any, 0, len(articles))
	for _, a := range articles {
		item := map[string]any{
			"title":      a.Title,
			"url":        a.URL,
			"summary":    a.Summary,
			"dedupe_key": linkedin.ArticleDedupeKey(a),
		}
		if !a.PublishedAt.IsZero() {
			item["published_at"] = a.PublishedAt
		}
		items = append(items, item)
	}

	draft.Metadata["linkedin_articles"] = items
	return draft, nil
}
