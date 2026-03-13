package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const twitterFixture = `window.YTD.tweets.part0 = [
  {
    "tweet" : {
      "id_str" : "111222333",
      "full_text" : "Step pipeline test tweet #test",
      "lang" : "en",
      "created_at" : "Mon Jan 02 15:04:05 +0000 2023",
      "favorite_count" : "5",
      "retweet_count" : "2",
      "entities" : {
        "urls" : [],
        "hashtags" : [{"text":"test"}]
      }
    }
  }
]`

func TestTwitterArchiveParser_Run(t *testing.T) {
	step := NewTwitterArchiveParser()
	if step.Name() != "twitter_archive_parser" {
		t.Errorf("Name() = %q, want twitter_archive_parser", step.Name())
	}

	draft := &storage.KnowledgeObject{RawContent: twitterFixture}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	raw, ok := out.Metadata["twitter_tweets"]
	if !ok {
		t.Fatal("metadata key twitter_tweets not set")
	}
	tweets, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("twitter_tweets has unexpected type %T", raw)
	}
	if len(tweets) != 1 {
		t.Fatalf("expected 1 tweet, got %d", len(tweets))
	}

	tw := tweets[0]
	if tw["id"] != "111222333" {
		t.Errorf("tweet id = %v, want 111222333", tw["id"])
	}
	if hashtags, ok := tw["hashtags"].([]string); !ok || len(hashtags) != 1 {
		t.Errorf("tweet hashtags = %v", tw["hashtags"])
	}
}

func TestTwitterArchiveParser_EmptyContent(t *testing.T) {
	step := NewTwitterArchiveParser()
	draft := &storage.KnowledgeObject{RawContent: ""}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	tweets := out.Metadata["twitter_tweets"].([]map[string]any)
	if len(tweets) != 0 {
		t.Errorf("expected 0 tweets for empty content, got %d", len(tweets))
	}
}

func TestTwitterArchiveParser_Contract(t *testing.T) {
	step := NewTwitterArchiveParser()
	contract := step.Contract()
	if len(contract.Requires) == 0 {
		t.Error("expected non-empty Requires")
	}
	if len(contract.Produces) == 0 {
		t.Error("expected non-empty Produces")
	}
}
