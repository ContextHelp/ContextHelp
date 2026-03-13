package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const linkedinPostsFixture = `Date,ShareCommentary,ShareMediaCategory,SharedUrl
2023-03-15 10:30:00 UTC,This is a test post from the pipeline step.,ARTICLE,https://example.com
`

const linkedinArticlesFixture = `Title,Url,Description,PublishedAt
My Step Test Article,https://linkedin.com/pulse/test,Summary text here.,2023-05-01
`

func TestLinkedInPostsParser_Run(t *testing.T) {
	step := NewLinkedInPostsParser()
	if step.Name() != "linkedin_posts_parser" {
		t.Errorf("Name() = %q, want linkedin_posts_parser", step.Name())
	}

	draft := &storage.KnowledgeObject{RawContent: linkedinPostsFixture}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	raw, ok := out.Metadata["linkedin_posts"]
	if !ok {
		t.Fatal("metadata key linkedin_posts not set")
	}
	posts, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("linkedin_posts has unexpected type %T", raw)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}

	p := posts[0]
	if p["share_commentary"] != "This is a test post from the pipeline step." {
		t.Errorf("share_commentary = %v", p["share_commentary"])
	}
	if p["shared_url"] != "https://example.com" {
		t.Errorf("shared_url = %v", p["shared_url"])
	}
	if p["share_media_category"] != "ARTICLE" {
		t.Errorf("share_media_category = %v", p["share_media_category"])
	}
	if _, ok := p["dedupe_key"]; !ok {
		t.Error("dedupe_key not set")
	}
}

func TestLinkedInPostsParser_EmptyContent(t *testing.T) {
	step := NewLinkedInPostsParser()
	draft := &storage.KnowledgeObject{RawContent: ""}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	posts := out.Metadata["linkedin_posts"].([]map[string]any)
	if len(posts) != 0 {
		t.Errorf("expected 0 posts for empty content, got %d", len(posts))
	}
}

func TestLinkedInArticlesParser_Run(t *testing.T) {
	step := NewLinkedInArticlesParser()
	if step.Name() != "linkedin_articles_parser" {
		t.Errorf("Name() = %q, want linkedin_articles_parser", step.Name())
	}

	draft := &storage.KnowledgeObject{RawContent: linkedinArticlesFixture}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	raw, ok := out.Metadata["linkedin_articles"]
	if !ok {
		t.Fatal("metadata key linkedin_articles not set")
	}
	articles, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("linkedin_articles has unexpected type %T", raw)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}

	a := articles[0]
	if a["title"] != "My Step Test Article" {
		t.Errorf("title = %v", a["title"])
	}
	if a["url"] != "https://linkedin.com/pulse/test" {
		t.Errorf("url = %v", a["url"])
	}
	if a["summary"] != "Summary text here." {
		t.Errorf("summary = %v", a["summary"])
	}
	if _, ok := a["dedupe_key"]; !ok {
		t.Error("dedupe_key not set")
	}
}

func TestLinkedInArticlesParser_EmptyContent(t *testing.T) {
	step := NewLinkedInArticlesParser()
	draft := &storage.KnowledgeObject{RawContent: ""}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	articles := out.Metadata["linkedin_articles"].([]map[string]any)
	if len(articles) != 0 {
		t.Errorf("expected 0 articles for empty content, got %d", len(articles))
	}
}

func TestLinkedInPostsParser_Contract(t *testing.T) {
	step := NewLinkedInPostsParser()
	contract := step.Contract()
	if len(contract.Requires) == 0 {
		t.Error("expected non-empty Requires")
	}
	if len(contract.Produces) == 0 {
		t.Error("expected non-empty Produces")
	}
}

func TestLinkedInArticlesParser_Contract(t *testing.T) {
	step := NewLinkedInArticlesParser()
	contract := step.Contract()
	if len(contract.Requires) == 0 {
		t.Error("expected non-empty Requires")
	}
	if len(contract.Produces) == 0 {
		t.Error("expected non-empty Produces")
	}
}
