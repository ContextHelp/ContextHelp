package integration

// US-0316: Twitter/X archive import (tweets.js fixture).

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/twitter"
)

// TestUS0316_TwitterParseArchiveFile verifies the tweets.js fixture parses.
func TestUS0316_TwitterParseArchiveFile(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("twitter", "tweets.js"))
	require.NoError(t, err)

	tweets, err := twitter.ParseArchive(string(data))
	require.NoError(t, err)
	require.Len(t, tweets, 2, "fixture contains 2 tweets")
}

// TestUS0316_TwitterTweetFields verifies all expected fields are populated.
func TestUS0316_TwitterTweetFields(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("twitter", "tweets.js"))
	require.NoError(t, err)

	tweets, err := twitter.ParseArchive(string(data))
	require.NoError(t, err)
	require.NotEmpty(t, tweets)

	first := tweets[0]
	assert.NotEmpty(t, first.ID, "ID must be set")
	assert.NotEmpty(t, first.FullText, "FullText must be set")
	assert.False(t, first.CreatedAt.IsZero(), "CreatedAt must be parsed")
	assert.Equal(t, "en", first.Lang, "Lang must be extracted")
}

// TestUS0316_TwitterHashtagsExtracted verifies hashtag extraction.
func TestUS0316_TwitterHashtagsExtracted(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("twitter", "tweets.js"))
	require.NoError(t, err)

	tweets, err := twitter.ParseArchive(string(data))
	require.NoError(t, err)
	require.NotEmpty(t, tweets)

	// First tweet has #golang and #testing hashtags.
	assert.NotEmpty(t, tweets[0].Hashtags)
	assert.Contains(t, tweets[0].Hashtags, "golang")
	assert.Contains(t, tweets[0].Hashtags, "testing")
}

// TestUS0316_TwitterURLsExtracted verifies URL extraction.
func TestUS0316_TwitterURLsExtracted(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("twitter", "tweets.js"))
	require.NoError(t, err)

	tweets, err := twitter.ParseArchive(string(data))
	require.NoError(t, err)
	require.NotEmpty(t, tweets)

	assert.NotEmpty(t, tweets[0].URLs)
	assert.Contains(t, tweets[0].URLs, "https://go.dev")
}

// TestUS0316_TwitterReplyDetected verifies in_reply_to_status_id.
func TestUS0316_TwitterReplyDetected(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("twitter", "tweets.js"))
	require.NoError(t, err)

	tweets, err := twitter.ParseArchive(string(data))
	require.NoError(t, err)
	require.Len(t, tweets, 2)

	// Second tweet is a reply.
	assert.NotEmpty(t, tweets[1].ReplyToStatusID)
	assert.Equal(t, "1234567890123456789", tweets[1].ReplyToStatusID)
}

// TestUS0316_TwitterFavoriteCount verifies numeric field parsing.
func TestUS0316_TwitterFavoriteCount(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(testdataPath("twitter", "tweets.js"))
	require.NoError(t, err)

	tweets, err := twitter.ParseArchive(string(data))
	require.NoError(t, err)
	require.NotEmpty(t, tweets)

	assert.Equal(t, 42, tweets[0].FavoriteCount)
	assert.Equal(t, 5, tweets[0].RetweetCount)
}

// TestUS0316_TwitterFilterSince verifies FilterSince helper.
func TestUS0316_TwitterFilterSince(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	old := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)

	all := []twitter.Tweet{
		{ID: "t1", FullText: "old", CreatedAt: old},
		{ID: "t2", FullText: "recent", CreatedAt: recent},
	}

	filtered := twitter.FilterSince(all, &cutoff)
	require.Len(t, filtered, 1)
	assert.Equal(t, "t2", filtered[0].ID)
}

// TestUS0316_TwitterRenderContent verifies RenderContent output.
func TestUS0316_TwitterRenderContent(t *testing.T) {
	t.Parallel()

	tweet := twitter.Tweet{
		ID:            "123",
		FullText:      "Test tweet #golang",
		CreatedAt:     time.Now(),
		Lang:          "en",
		FavoriteCount: 10,
		RetweetCount:  2,
		Hashtags:      []string{"golang"},
	}
	rendered := twitter.RenderContent(tweet)
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Test tweet #golang")
}
