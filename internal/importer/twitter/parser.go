// Package twitter provides a parser for Twitter/X archive exports.
//
// Twitter data archives export tweets as a JavaScript file (tweets.js or
// data/tweets.js) with the form:
//
//	window.YTD.tweets.part0 = [ { "tweet": { ... } }, ... ]
//
// This parser strips the JS assignment prefix and decodes the JSON array.
package twitter

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Tweet is a normalised representation of a single tweet from the archive.
type Tweet struct {
	// ID is the tweet's unique identifier string (stable across re-imports).
	ID string

	// FullText is the full tweet text (may include truncated suffix in old exports).
	FullText string

	// CreatedAt is the tweet creation timestamp.
	CreatedAt time.Time

	// Lang is the declared language code (e.g. "en").
	Lang string

	// ReplyToStatusID is set when the tweet is a reply.
	ReplyToStatusID string

	// RetweetedStatusID is set when the tweet is a native retweet.
	RetweetedStatusID string

	// QuotedStatusID is set when the tweet quotes another tweet.
	QuotedStatusID string

	// URLs contains expanded URLs extracted from entities.
	URLs []string

	// MediaURLs contains media URL strings extracted from extended_entities.
	MediaURLs []string

	// Hashtags contains hashtag text values.
	Hashtags []string

	// FavoriteCount is the number of likes at export time.
	FavoriteCount int

	// RetweetCount is the number of retweets at export time.
	RetweetCount int
}

// ParseArchive parses the raw content of a tweets.js file and returns
// normalised Tweet records.  The content may include the JavaScript
// assignment prefix (window.YTD.tweets.part0 = ...) which is stripped
// automatically.
func ParseArchive(content string) ([]Tweet, error) {
	jsonData, err := stripJSPrefix(content)
	if err != nil {
		return nil, fmt.Errorf("twitter parser: %w", err)
	}

	// The outer array contains objects that wrap each tweet under a "tweet" key.
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return nil, fmt.Errorf("twitter parser: decode archive array: %w", err)
	}

	out := make([]Tweet, 0, len(raw))
	for _, wrapper := range raw {
		tweetData, ok := wrapper["tweet"]
		if !ok {
			// Older exports may omit the "tweet" wrapper; try the object itself.
			// Re-marshal wrapper values into a single object for decoding.
			combined := make(map[string]json.RawMessage, len(wrapper))
			for k, v := range wrapper {
				combined[k] = v
			}
			b, _ := json.Marshal(combined)
			tweetData = b
		}

		t, err := decodeTweet(tweetData)
		if err != nil {
			// Skip malformed entries but continue processing the rest.
			continue
		}
		out = append(out, t)
	}

	return out, nil
}

// stripJSPrefix removes the leading JS assignment and returns raw JSON bytes.
func stripJSPrefix(content string) ([]byte, error) {
	s := strings.TrimSpace(content)

	// Locate the first '[' which starts the JSON array.
	idx := strings.Index(s, "[")
	if idx < 0 {
		return nil, fmt.Errorf("no JSON array found in content")
	}
	s = s[idx:]

	// Strip trailing semicolon if present.
	s = strings.TrimRight(s, " \t\r\n;")

	return []byte(s), nil
}

func decodeTweet(data json.RawMessage) (Tweet, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Tweet{}, err
	}

	t := Tweet{}
	t.ID = rawString(raw["id"])
	if t.ID == "" {
		t.ID = rawString(raw["id_str"])
	}
	if t.ID == "" {
		return Tweet{}, fmt.Errorf("tweet has no id")
	}

	t.FullText = rawString(raw["full_text"])
	if t.FullText == "" {
		t.FullText = rawString(raw["text"])
	}

	t.Lang = rawString(raw["lang"])

	t.ReplyToStatusID = rawString(raw["in_reply_to_status_id_str"])
	if t.ReplyToStatusID == "" {
		t.ReplyToStatusID = rawString(raw["in_reply_to_status_id"])
	}

	// Parse created_at (Twitter uses "Mon Jan 02 15:04:05 +0000 2006" format).
	if createdStr := rawString(raw["created_at"]); createdStr != "" {
		if ts, err := parseTwitterTime(createdStr); err == nil {
			t.CreatedAt = ts
		}
	}

	t.FavoriteCount = rawInt(raw["favorite_count"])
	t.RetweetCount = rawInt(raw["retweet_count"])

	// Extract retweet / quote status IDs from the nested objects.
	if rt := rawNestedID(raw["retweeted_status"]); rt != "" {
		t.RetweetedStatusID = rt
	}
	if qt := rawNestedID(raw["quoted_status"]); qt != "" {
		t.QuotedStatusID = qt
	}

	// Extract entities.
	if entitiesData, ok := raw["entities"]; ok {
		t.URLs, t.Hashtags = extractEntities(entitiesData)
	}

	// Extended entities (media).
	if extData, ok := raw["extended_entities"]; ok {
		t.MediaURLs = extractMedia(extData)
	}

	return t, nil
}

func parseTwitterTime(raw string) (time.Time, error) {
	// Twitter archive format: "Mon Jan 02 15:04:05 +0000 2006"
	return time.Parse("Mon Jan 02 15:04:05 -0700 2006", raw)
}

func rawString(v json.RawMessage) string {
	if len(v) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return s
}

func rawInt(v json.RawMessage) int {
	if len(v) == 0 {
		return 0
	}
	var n int
	if err := json.Unmarshal(v, &n); err != nil {
		// Some fields are quoted strings.
		var s string
		if err2 := json.Unmarshal(v, &s); err2 == nil {
			var n2 int
			_, _ = fmt.Sscanf(s, "%d", &n2)
			return n2
		}
		return 0
	}
	return n
}

func rawNestedID(v json.RawMessage) string {
	if len(v) == 0 {
		return ""
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(v, &nested); err != nil {
		return ""
	}
	id := rawString(nested["id_str"])
	if id == "" {
		id = rawString(nested["id"])
	}
	return id
}

func extractEntities(data json.RawMessage) (urls []string, hashtags []string) {
	var entities struct {
		URLs []struct {
			ExpandedURL string `json:"expanded_url"`
			URL         string `json:"url"`
		} `json:"urls"`
		Hashtags []struct {
			Text string `json:"text"`
		} `json:"hashtags"`
	}
	if err := json.Unmarshal(data, &entities); err != nil {
		return nil, nil
	}
	for _, u := range entities.URLs {
		eu := strings.TrimSpace(u.ExpandedURL)
		if eu == "" {
			eu = strings.TrimSpace(u.URL)
		}
		if eu != "" {
			urls = append(urls, eu)
		}
	}
	for _, h := range entities.Hashtags {
		if tag := strings.TrimSpace(h.Text); tag != "" {
			hashtags = append(hashtags, tag)
		}
	}
	return urls, hashtags
}

func extractMedia(data json.RawMessage) []string {
	var extended struct {
		Media []struct {
			MediaURLHTTPS string `json:"media_url_https"`
			MediaURL      string `json:"media_url"`
		} `json:"media"`
	}
	if err := json.Unmarshal(data, &extended); err != nil {
		return nil
	}
	out := make([]string, 0, len(extended.Media))
	for _, m := range extended.Media {
		u := strings.TrimSpace(m.MediaURLHTTPS)
		if u == "" {
			u = strings.TrimSpace(m.MediaURL)
		}
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}

// RenderContent converts a Tweet to a human-readable markdown string suitable
// for ingestion into dPKMS.
func RenderContent(t Tweet) string {
	var b strings.Builder

	title := t.FullText
	if len(title) > 80 {
		title = title[:80] + "…"
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	b.WriteString("Provider: Twitter/X\n")
	b.WriteString("Tweet ID: ")
	b.WriteString(t.ID)
	b.WriteString("\n")

	if !t.CreatedAt.IsZero() {
		b.WriteString("Created: ")
		b.WriteString(t.CreatedAt.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if t.Lang != "" {
		b.WriteString("Language: ")
		b.WriteString(t.Lang)
		b.WriteString("\n")
	}
	if t.ReplyToStatusID != "" {
		b.WriteString("Reply To: ")
		b.WriteString(t.ReplyToStatusID)
		b.WriteString("\n")
	}
	if t.RetweetedStatusID != "" {
		b.WriteString("Retweet Of: ")
		b.WriteString(t.RetweetedStatusID)
		b.WriteString("\n")
	}
	if t.QuotedStatusID != "" {
		b.WriteString("Quote Of: ")
		b.WriteString(t.QuotedStatusID)
		b.WriteString("\n")
	}
	if t.FavoriteCount > 0 {
		b.WriteString(fmt.Sprintf("Likes: %d\n", t.FavoriteCount))
	}
	if t.RetweetCount > 0 {
		b.WriteString(fmt.Sprintf("Retweets: %d\n", t.RetweetCount))
	}
	if len(t.Hashtags) > 0 {
		b.WriteString("Hashtags: #")
		b.WriteString(strings.Join(t.Hashtags, " #"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(t.FullText)
	b.WriteString("\n")

	if len(t.URLs) > 0 {
		b.WriteString("\nLinks:\n")
		for _, u := range t.URLs {
			b.WriteString("- ")
			b.WriteString(u)
			b.WriteString("\n")
		}
	}
	if len(t.MediaURLs) > 0 {
		b.WriteString("\nMedia:\n")
		for _, u := range t.MediaURLs {
			b.WriteString("- ")
			b.WriteString(u)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// DedupeKey returns the stable deduplication key for a tweet.
// Format: "twitter:<tweet_id>"
func DedupeKey(t Tweet) string {
	return "twitter:" + t.ID
}

// FilterSince returns only tweets created at or after the given time.
// If since is nil all tweets are returned.
func FilterSince(tweets []Tweet, since *time.Time) []Tweet {
	if since == nil {
		return tweets
	}
	out := make([]Tweet, 0, len(tweets))
	for _, t := range tweets {
		if !t.CreatedAt.IsZero() && t.CreatedAt.Before(*since) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// TweetURL returns the canonical URL for a tweet given a username.
func TweetURL(username, tweetID string) string {
	if username == "" {
		return "https://twitter.com/i/web/status/" + tweetID
	}
	return "https://twitter.com/" + username + "/status/" + tweetID
}
