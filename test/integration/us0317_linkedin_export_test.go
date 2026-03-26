package integration

// US-0317: LinkedIn data export import (CSV files).

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/linkedin"
)

// TestUS0317_LinkedInParsePosts verifies Posts.csv parsing from fixture.
func TestUS0317_LinkedInParsePosts(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("linkedin", "Posts.csv"))
	require.NoError(t, err)

	posts, err := linkedin.ParsePosts(string(data))
	require.NoError(t, err)
	require.Len(t, posts, 2, "fixture contains 2 posts")
}

// TestUS0317_LinkedInPostFields verifies all expected PostRecord fields.
func TestUS0317_LinkedInPostFields(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("linkedin", "Posts.csv"))
	require.NoError(t, err)

	posts, err := linkedin.ParsePosts(string(data))
	require.NoError(t, err)
	require.NotEmpty(t, posts)

	first := posts[0]
	assert.NotEmpty(t, first.ShareCommentary, "ShareCommentary must be set")
	assert.NotEmpty(t, first.SharedURL, "SharedURL must be set")
	assert.NotEmpty(t, first.ShareMediaCategory, "ShareMediaCategory must be set")
	assert.False(t, first.Date.IsZero(), "Date must be parsed")
}

// TestUS0317_LinkedInPostContent verifies content extraction.
func TestUS0317_LinkedInPostContent(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("linkedin", "Posts.csv"))
	require.NoError(t, err)

	posts, err := linkedin.ParsePosts(string(data))
	require.NoError(t, err)
	require.NotEmpty(t, posts)

	assert.Contains(t, posts[0].ShareCommentary, "open source")
	assert.Equal(t, "https://github.com/example/project", posts[0].SharedURL)
	assert.Equal(t, "ARTICLE", posts[0].ShareMediaCategory)
}

// TestUS0317_LinkedInParseArticles verifies Articles.csv parsing.
func TestUS0317_LinkedInParseArticles(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("linkedin", "Articles.csv"))
	require.NoError(t, err)

	articles, err := linkedin.ParseArticles(string(data))
	require.NoError(t, err)
	require.Len(t, articles, 1, "fixture contains 1 article")

	assert.Equal(t, "Building Better APIs", articles[0].Title)
	assert.NotEmpty(t, articles[0].URL)
	assert.NotEmpty(t, articles[0].Summary)
}

// TestUS0317_LinkedInArticleFields verifies ArticleRecord fields.
func TestUS0317_LinkedInArticleFields(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("linkedin", "Articles.csv"))
	require.NoError(t, err)

	articles, err := linkedin.ParseArticles(string(data))
	require.NoError(t, err)
	require.NotEmpty(t, articles)

	a := articles[0]
	assert.NotEmpty(t, a.Title)
	assert.NotEmpty(t, a.URL)
	assert.False(t, a.PublishedAt.IsZero(), "PublishedAt must be parsed")
}

// TestUS0317_LinkedInFilterPostsSince verifies FilterPostsSince helper.
func TestUS0317_LinkedInFilterPostsSince(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	posts := []linkedin.PostRecord{
		{Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), ShareCommentary: "recent"},
		{Date: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), ShareCommentary: "old"},
	}

	filtered := linkedin.FilterPostsSince(posts, &cutoff)
	require.Len(t, filtered, 1)
	assert.Equal(t, "recent", filtered[0].ShareCommentary)
}

// TestUS0317_LinkedInRenderPost verifies RenderPost output.
func TestUS0317_LinkedInRenderPost(t *testing.T) {
	t.Parallel()

	post := linkedin.PostRecord{
		Date:               time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		ShareCommentary:    "Excited to share our new project!",
		SharedURL:          "https://github.com/example",
		ShareMediaCategory: "ARTICLE",
	}
	rendered := linkedin.RenderPost(post)
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Excited to share")
	assert.Contains(t, rendered, "https://github.com/example")
}

// TestUS0317_LinkedInRenderArticle verifies RenderArticle output.
func TestUS0317_LinkedInRenderArticle(t *testing.T) {
	t.Parallel()

	article := linkedin.ArticleRecord{
		PublishedAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		Title:       "Building Better APIs",
		URL:         "https://linkedin.com/pulse/building-better-apis",
		Summary:     "How to design clean APIs.",
	}
	rendered := linkedin.RenderArticle(article)
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Building Better APIs")
}
