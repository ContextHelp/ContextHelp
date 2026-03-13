package twitter

import (
	"strings"
	"testing"
	"time"
)

// minimalTweetsJS is a golden fixture representing the tweets.js format used
// in Twitter/X archive exports.
const minimalTweetsJS = `window.YTD.tweets.part0 = [
  {
    "tweet" : {
      "id" : "1234567890123456789",
      "id_str" : "1234567890123456789",
      "full_text" : "Hello world! This is a test tweet. https://t.co/abc123",
      "lang" : "en",
      "created_at" : "Mon Jan 02 15:04:05 +0000 2023",
      "favorite_count" : "42",
      "retweet_count" : "7",
      "entities" : {
        "urls" : [
          {
            "url" : "https://t.co/abc123",
            "expanded_url" : "https://example.com/article"
          }
        ],
        "hashtags" : []
      }
    }
  },
  {
    "tweet" : {
      "id_str" : "9876543210987654321",
      "full_text" : "#golang is great! #programming",
      "lang" : "en",
      "created_at" : "Tue Feb 14 10:00:00 +0000 2023",
      "favorite_count" : "0",
      "retweet_count" : "0",
      "in_reply_to_status_id_str" : "1111111111111111111",
      "entities" : {
        "urls" : [],
        "hashtags" : [
          {"text" : "golang"},
          {"text" : "programming"}
        ]
      }
    }
  }
]`

// tweetWithMedia exercises the extended_entities path.
const tweetWithMedia = `window.YTD.tweets.part0 = [
  {
    "tweet" : {
      "id_str" : "111",
      "full_text" : "Check out this photo!",
      "created_at" : "Wed Mar 01 12:00:00 +0000 2023",
      "favorite_count" : "0",
      "retweet_count" : "0",
      "entities" : {"urls":[],"hashtags":[]},
      "extended_entities" : {
        "media" : [
          {
            "media_url_https" : "https://pbs.twimg.com/media/abc.jpg"
          }
        ]
      }
    }
  }
]`

// rawJSONArray exercises parsing without the JS prefix.
const rawJSONArray = `[
  {
    "tweet" : {
      "id_str" : "555",
      "full_text" : "No prefix tweet",
      "created_at" : "Thu Apr 06 08:00:00 +0000 2023",
      "favorite_count" : "1",
      "retweet_count" : "0",
      "entities" : {"urls":[],"hashtags":[]}
    }
  }
]`

func TestParseArchive_BasicTweets(t *testing.T) {
	tweets, err := ParseArchive(minimalTweetsJS)
	if err != nil {
		t.Fatalf("ParseArchive: unexpected error: %v", err)
	}
	if len(tweets) != 2 {
		t.Fatalf("expected 2 tweets, got %d", len(tweets))
	}

	// First tweet
	tw := tweets[0]
	if tw.ID != "1234567890123456789" {
		t.Errorf("tweet[0].ID = %q, want 1234567890123456789", tw.ID)
	}
	if !strings.HasPrefix(tw.FullText, "Hello world!") {
		t.Errorf("tweet[0].FullText = %q, want prefix 'Hello world!'", tw.FullText)
	}
	if tw.Lang != "en" {
		t.Errorf("tweet[0].Lang = %q, want en", tw.Lang)
	}
	if tw.FavoriteCount != 42 {
		t.Errorf("tweet[0].FavoriteCount = %d, want 42", tw.FavoriteCount)
	}
	if tw.RetweetCount != 7 {
		t.Errorf("tweet[0].RetweetCount = %d, want 7", tw.RetweetCount)
	}
	if len(tw.URLs) != 1 || tw.URLs[0] != "https://example.com/article" {
		t.Errorf("tweet[0].URLs = %v, want [https://example.com/article]", tw.URLs)
	}

	// Second tweet
	tw2 := tweets[1]
	if tw2.ID != "9876543210987654321" {
		t.Errorf("tweet[1].ID = %q, want 9876543210987654321", tw2.ID)
	}
	if tw2.ReplyToStatusID != "1111111111111111111" {
		t.Errorf("tweet[1].ReplyToStatusID = %q, want 1111111111111111111", tw2.ReplyToStatusID)
	}
	if len(tw2.Hashtags) != 2 {
		t.Errorf("tweet[1].Hashtags = %v, want 2 hashtags", tw2.Hashtags)
	}
}

func TestParseArchive_CreatedAt(t *testing.T) {
	tweets, err := ParseArchive(minimalTweetsJS)
	if err != nil {
		t.Fatalf("ParseArchive: %v", err)
	}
	if tweets[0].CreatedAt.IsZero() {
		t.Error("tweet[0].CreatedAt should not be zero")
	}
	// 2023-01-02 in UTC
	want := time.Date(2023, 1, 2, 15, 4, 5, 0, time.UTC)
	if !tweets[0].CreatedAt.Equal(want) {
		t.Errorf("tweet[0].CreatedAt = %v, want %v", tweets[0].CreatedAt, want)
	}
}

func TestParseArchive_MediaTweet(t *testing.T) {
	tweets, err := ParseArchive(tweetWithMedia)
	if err != nil {
		t.Fatalf("ParseArchive: %v", err)
	}
	if len(tweets) != 1 {
		t.Fatalf("expected 1 tweet, got %d", len(tweets))
	}
	tw := tweets[0]
	if len(tw.MediaURLs) != 1 || tw.MediaURLs[0] != "https://pbs.twimg.com/media/abc.jpg" {
		t.Errorf("tweet.MediaURLs = %v, want one media URL", tw.MediaURLs)
	}
}

func TestParseArchive_RawJSONNoPrefx(t *testing.T) {
	tweets, err := ParseArchive(rawJSONArray)
	if err != nil {
		t.Fatalf("ParseArchive: %v", err)
	}
	if len(tweets) != 1 {
		t.Fatalf("expected 1 tweet, got %d", len(tweets))
	}
	if tweets[0].ID != "555" {
		t.Errorf("tweet.ID = %q, want 555", tweets[0].ID)
	}
}

func TestParseArchive_Empty(t *testing.T) {
	tweets, err := ParseArchive("window.YTD.tweets.part0 = []")
	if err != nil {
		t.Fatalf("ParseArchive: %v", err)
	}
	if len(tweets) != 0 {
		t.Errorf("expected 0 tweets, got %d", len(tweets))
	}
}

func TestParseArchive_InvalidContent(t *testing.T) {
	_, err := ParseArchive("not valid content at all")
	if err == nil {
		t.Error("expected error for invalid content, got nil")
	}
}

func TestRenderContent_Basic(t *testing.T) {
	tw := Tweet{
		ID:        "123",
		FullText:  "Hello, world!",
		Lang:      "en",
		CreatedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		Hashtags:  []string{"hello"},
		URLs:      []string{"https://example.com"},
	}
	rendered := RenderContent(tw)
	if !strings.Contains(rendered, "Hello, world!") {
		t.Error("rendered content should contain tweet text")
	}
	if !strings.Contains(rendered, "Tweet ID: 123") {
		t.Error("rendered content should contain tweet ID")
	}
	if !strings.Contains(rendered, "Provider: Twitter/X") {
		t.Error("rendered content should contain provider")
	}
	if !strings.Contains(rendered, "#hello") {
		t.Error("rendered content should contain hashtags")
	}
	if !strings.Contains(rendered, "https://example.com") {
		t.Error("rendered content should contain URLs")
	}
}

func TestRenderContent_TruncatesLongTitle(t *testing.T) {
	longText := strings.Repeat("a", 100)
	tw := Tweet{ID: "1", FullText: longText}
	rendered := RenderContent(tw)
	// Title line should be truncated.
	lines := strings.Split(rendered, "\n")
	if len(lines[0]) > 85 { // "# " + 80 chars + "…"
		t.Errorf("title line too long: %d chars", len(lines[0]))
	}
}

func TestDedupeKey(t *testing.T) {
	tw := Tweet{ID: "123456"}
	key := DedupeKey(tw)
	if key != "twitter:123456" {
		t.Errorf("DedupeKey = %q, want twitter:123456", key)
	}
}

func TestFilterSince(t *testing.T) {
	t1 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC)

	tweets := []Tweet{
		{ID: "1", CreatedAt: t1},
		{ID: "2", CreatedAt: t2},
		{ID: "3", CreatedAt: t3},
	}

	cutoff := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	filtered := FilterSince(tweets, &cutoff)
	if len(filtered) != 2 {
		t.Errorf("FilterSince: got %d tweets, want 2", len(filtered))
	}
	if filtered[0].ID != "2" || filtered[1].ID != "3" {
		t.Errorf("FilterSince: wrong tweets: %v", filtered)
	}

	// Nil since = all tweets.
	all := FilterSince(tweets, nil)
	if len(all) != 3 {
		t.Errorf("FilterSince(nil): got %d, want 3", len(all))
	}
}

func TestTweetURL(t *testing.T) {
	url := TweetURL("jack", "123")
	if url != "https://twitter.com/jack/status/123" {
		t.Errorf("TweetURL = %q", url)
	}
	urlNoUser := TweetURL("", "123")
	if !strings.Contains(urlNoUser, "123") {
		t.Errorf("TweetURL (no user) = %q", urlNoUser)
	}
}
