package linkedin

import (
	"strings"
	"testing"
	"time"
)

// postsCSV is a golden fixture for the LinkedIn Posts.csv export format.
const postsCSV = `Date,ShareCommentary,ShareMediaCategory,SharedUrl
2023-03-15 10:30:00 UTC,Excited to share my latest thoughts on distributed systems! This is a longer post about the topic.,ARTICLE,https://www.linkedin.com/pulse/thoughts-distributed-systems-jad
2023-04-01 09:00:00 UTC,Happy April! Just launched a new open source project.,IMAGE,
2023-04-15 14:45:00 UTC,,NONE,https://example.com/article
`

// articlesCSV is a golden fixture for the LinkedIn Articles.csv export format.
const articlesCSV = `Title,Url,Description,PublishedAt
Building Resilient Systems,https://www.linkedin.com/pulse/building-resilient-systems,Learn how to build systems that survive failure.,2023-01-10
The Future of AI,https://www.linkedin.com/pulse/future-ai,Exploring AI trends in 2023 and beyond.,2023-02-20 12:00:00 UTC
`

// emptyCSV exercises zero-row parsing.
const emptyCSV = `Date,ShareCommentary,ShareMediaCategory,SharedUrl
`

func TestParsePosts_Basic(t *testing.T) {
	posts, err := ParsePosts(postsCSV)
	if err != nil {
		t.Fatalf("ParsePosts: unexpected error: %v", err)
	}
	// Third row has no commentary but has a SharedUrl, so it is included.
	if len(posts) != 3 {
		t.Fatalf("expected 3 posts, got %d", len(posts))
	}

	p := posts[0]
	if !strings.HasPrefix(p.ShareCommentary, "Excited to share") {
		t.Errorf("post[0].ShareCommentary = %q", p.ShareCommentary)
	}
	if p.ShareMediaCategory != "ARTICLE" {
		t.Errorf("post[0].ShareMediaCategory = %q, want ARTICLE", p.ShareMediaCategory)
	}
	if p.SharedURL != "https://www.linkedin.com/pulse/thoughts-distributed-systems-jad" {
		t.Errorf("post[0].SharedURL = %q", p.SharedURL)
	}
	wantDate := time.Date(2023, 3, 15, 10, 30, 0, 0, time.UTC)
	if !p.Date.Equal(wantDate) {
		t.Errorf("post[0].Date = %v, want %v", p.Date, wantDate)
	}

	p2 := posts[1]
	if p2.ShareCommentary != "Happy April! Just launched a new open source project." {
		t.Errorf("post[1].ShareCommentary = %q", p2.ShareCommentary)
	}
}

func TestParsePosts_Empty(t *testing.T) {
	posts, err := ParsePosts(emptyCSV)
	if err != nil {
		t.Fatalf("ParsePosts: %v", err)
	}
	if len(posts) != 0 {
		t.Errorf("expected 0 posts, got %d", len(posts))
	}
}

func TestParsePosts_InvalidCSV(t *testing.T) {
	_, err := ParsePosts("field1\x00field2\nrow1\x00val1")
	// May or may not error depending on CSV parser; just check no panic.
	_ = err
}

func TestParseArticles_Basic(t *testing.T) {
	articles, err := ParseArticles(articlesCSV)
	if err != nil {
		t.Fatalf("ParseArticles: unexpected error: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 articles, got %d", len(articles))
	}

	a := articles[0]
	if a.Title != "Building Resilient Systems" {
		t.Errorf("article[0].Title = %q", a.Title)
	}
	if a.URL != "https://www.linkedin.com/pulse/building-resilient-systems" {
		t.Errorf("article[0].URL = %q", a.URL)
	}
	if a.Summary != "Learn how to build systems that survive failure." {
		t.Errorf("article[0].Summary = %q", a.Summary)
	}
	if a.PublishedAt.Year() != 2023 || a.PublishedAt.Month() != 1 || a.PublishedAt.Day() != 10 {
		t.Errorf("article[0].PublishedAt = %v", a.PublishedAt)
	}

	a2 := articles[1]
	if a2.Title != "The Future of AI" {
		t.Errorf("article[1].Title = %q", a2.Title)
	}
}

func TestParseArticles_EmptyContent(t *testing.T) {
	articles, err := ParseArticles("Title,Url,Description,PublishedAt\n")
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	if len(articles) != 0 {
		t.Errorf("expected 0 articles, got %d", len(articles))
	}
}

func TestRenderPost_Basic(t *testing.T) {
	p := PostRecord{
		Date:               time.Date(2023, 3, 15, 10, 30, 0, 0, time.UTC),
		ShareCommentary:    "This is my post text",
		SharedURL:          "https://example.com",
		ShareMediaCategory: "ARTICLE",
	}
	rendered := RenderPost(p)

	if !strings.Contains(rendered, "This is my post text") {
		t.Error("rendered should contain post text")
	}
	if !strings.Contains(rendered, "Provider: LinkedIn") {
		t.Error("rendered should contain provider")
	}
	if !strings.Contains(rendered, "https://example.com") {
		t.Error("rendered should contain shared URL")
	}
	if !strings.Contains(rendered, "ARTICLE") {
		t.Error("rendered should contain media category")
	}
}

func TestRenderPost_TruncatesLongTitle(t *testing.T) {
	longText := strings.Repeat("x", 120)
	p := PostRecord{ShareCommentary: longText}
	rendered := RenderPost(p)
	lines := strings.Split(rendered, "\n")
	if len(lines[0]) > 85 {
		t.Errorf("title line too long: %d", len(lines[0]))
	}
}

func TestRenderArticle_Basic(t *testing.T) {
	a := ArticleRecord{
		Title:       "My Great Article",
		URL:         "https://linkedin.com/pulse/my-great-article",
		Summary:     "A summary of the article.",
		PublishedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	rendered := RenderArticle(a)

	if !strings.Contains(rendered, "My Great Article") {
		t.Error("rendered should contain title")
	}
	if !strings.Contains(rendered, "Provider: LinkedIn") {
		t.Error("rendered should contain provider")
	}
	if !strings.Contains(rendered, "https://linkedin.com/pulse/my-great-article") {
		t.Error("rendered should contain URL")
	}
	if !strings.Contains(rendered, "A summary of the article.") {
		t.Error("rendered should contain summary")
	}
}

func TestPostDedupeKey_Stable(t *testing.T) {
	p := PostRecord{
		Date:            time.Date(2023, 3, 15, 10, 30, 0, 0, time.UTC),
		ShareCommentary: "Hello world",
	}
	k1 := PostDedupeKey(p)
	k2 := PostDedupeKey(p)
	if k1 != k2 {
		t.Errorf("PostDedupeKey is not stable: %q vs %q", k1, k2)
	}
	if !strings.HasPrefix(k1, "linkedin:post:") {
		t.Errorf("PostDedupeKey = %q, want prefix linkedin:post:", k1)
	}
}

func TestArticleDedupeKey_URLPreference(t *testing.T) {
	a := ArticleRecord{
		URL:         "https://linkedin.com/pulse/abc",
		Title:       "Some title",
		PublishedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	k := ArticleDedupeKey(a)
	if k != "linkedin:article:https://linkedin.com/pulse/abc" {
		t.Errorf("ArticleDedupeKey = %q", k)
	}

	// Without URL, falls back to date+title.
	a2 := ArticleRecord{Title: "My title", PublishedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)}
	k2 := ArticleDedupeKey(a2)
	if !strings.HasPrefix(k2, "linkedin:article:") {
		t.Errorf("ArticleDedupeKey(no URL) = %q", k2)
	}
}

func TestFilterPostsSince(t *testing.T) {
	t1 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC)

	posts := []PostRecord{
		{Date: t1, ShareCommentary: "Jan"},
		{Date: t2, ShareCommentary: "Jun"},
		{Date: t3, ShareCommentary: "Dec"},
	}

	cutoff := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	filtered := FilterPostsSince(posts, &cutoff)
	if len(filtered) != 2 {
		t.Errorf("FilterPostsSince: got %d, want 2", len(filtered))
	}
	if filtered[0].ShareCommentary != "Jun" {
		t.Errorf("wrong first post: %q", filtered[0].ShareCommentary)
	}

	all := FilterPostsSince(posts, nil)
	if len(all) != 3 {
		t.Errorf("FilterPostsSince(nil): got %d, want 3", len(all))
	}
}

func TestFilterArticlesSince(t *testing.T) {
	t1 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)

	articles := []ArticleRecord{
		{PublishedAt: t1, Title: "Old"},
		{PublishedAt: t2, Title: "New"},
	}

	cutoff := time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC)
	filtered := FilterArticlesSince(articles, &cutoff)
	if len(filtered) != 1 {
		t.Errorf("FilterArticlesSince: got %d, want 1", len(filtered))
	}
	if filtered[0].Title != "New" {
		t.Errorf("wrong article: %q", filtered[0].Title)
	}
}
