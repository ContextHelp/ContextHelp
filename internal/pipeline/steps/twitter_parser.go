package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/importer/twitter"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TwitterArchiveParser parses a Twitter/X tweets.js archive from draft.RawContent
// and populates draft.Metadata["twitter_tweets"] with normalised tweet records.
type TwitterArchiveParser struct {
	pipeline.BaseContract
}

// NewTwitterArchiveParser creates a TwitterArchiveParser.
func NewTwitterArchiveParser() *TwitterArchiveParser {
	return &TwitterArchiveParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *TwitterArchiveParser) Name() string { return "twitter_archive_parser" }

func (s *TwitterArchiveParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	if draft.RawContent == "" {
		draft.Metadata["twitter_tweets"] = []map[string]any{}
		return draft, nil
	}

	tweets, err := twitter.ParseArchive(draft.RawContent)
	if err != nil {
		return nil, fmt.Errorf("twitter_archive_parser: %w", err)
	}

	items := make([]map[string]any, 0, len(tweets))
	for _, tw := range tweets {
		item := map[string]any{
			"id":         tw.ID,
			"full_text":  tw.FullText,
			"lang":       tw.Lang,
			"dedupe_key": twitter.DedupeKey(tw),
		}
		if !tw.CreatedAt.IsZero() {
			item["created_at"] = tw.CreatedAt
		}
		if tw.ReplyToStatusID != "" {
			item["reply_to_status_id"] = tw.ReplyToStatusID
		}
		if tw.RetweetedStatusID != "" {
			item["retweeted_status_id"] = tw.RetweetedStatusID
		}
		if tw.QuotedStatusID != "" {
			item["quoted_status_id"] = tw.QuotedStatusID
		}
		if len(tw.URLs) > 0 {
			item["urls"] = tw.URLs
		}
		if len(tw.MediaURLs) > 0 {
			item["media_urls"] = tw.MediaURLs
		}
		if len(tw.Hashtags) > 0 {
			item["hashtags"] = tw.Hashtags
		}
		if tw.FavoriteCount > 0 {
			item["favorite_count"] = tw.FavoriteCount
		}
		if tw.RetweetCount > 0 {
			item["retweet_count"] = tw.RetweetCount
		}
		items = append(items, item)
	}

	draft.Metadata["twitter_tweets"] = items
	return draft, nil
}
