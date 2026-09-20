package integration

// US-0201: X/Twitter Profile and Thread Capture
//
// Verifies: mock Twitter API, capture tweet by URL, tweet text/author/timestamp
// stored as object.
// The upstream fetch runs through an xrr cassette
// (testdata/cassettes/us0201_*). In the default replay mode the recorded
// round-trip answers from disk and the fixture server is never contacted.
// See xrr_capture_helpers_test.go for what the cassettes do and do not
// prove.
//
// Re-record: XRR_MODE=record go test -count=1 ./test/integration/ \
//   -run TestUS0201_RecordCassettes
//
// Gate: INTEGRATION=1 env var required.

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock Twitter API payloads
// ---------------------------------------------------------------------------

type mockTweet struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	AuthorID  string `json:"author_id"`
	CreatedAt string `json:"created_at"`
}

type mockTwitterUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// ---------------------------------------------------------------------------
// Pipeline step
// ---------------------------------------------------------------------------

// twitterFetchStep calls the mock Twitter API and populates the draft.
type twitterFetchStep struct {
	pipeline.BaseContract
	apiBaseURL string
	tweetID    string
	// client carries the xrr record/replay transport. Nil falls back to
	// the default client.
	client *gohttp.Client
}

func (s *twitterFetchStep) Name() string { return "test-twitter-fetch" }
func (s *twitterFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/2/tweets/%s?expansions=author_id", s.apiBaseURL, s.tweetID)
	resp, err := captureGet(s.client, url)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var payload struct {
		Data     mockTweet `json:"data"`
		Includes struct {
			Users []mockTwitterUser `json:"users"`
		} `json:"includes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Type = "social.post"
	draft.TextContent = payload.Data.Text
	draft.Metadata["tweet_id"] = payload.Data.ID
	draft.Metadata["tweet_text"] = payload.Data.Text
	draft.Metadata["author_id"] = payload.Data.AuthorID
	draft.Metadata["created_at"] = payload.Data.CreatedAt
	if len(payload.Includes.Users) > 0 {
		draft.Metadata["author_username"] = payload.Includes.Users[0].Username
		draft.Metadata["author_name"] = payload.Includes.Users[0].Name
	}
	return draft, nil
}

// ---------------------------------------------------------------------------
// Fixture handlers
//
// Named so the replaying test and the recorder share one definition of
// each payload. See xrr_capture_helpers_test.go.
// ---------------------------------------------------------------------------

const (
	twitterTweetID       = "1234567890"
	twitterTweetText     = "Distributed systems require careful coordination."
	twitterAuthorHandle  = "karpathy"
	twitterTweetCreated  = "2026-02-18T10:00:00.000Z"
	twitterSourceTweetID = "9999"
	twitterAsyncTweetID  = "1"
)

// twitterTweetHandler serves a v2 tweet lookup response with the author
// expanded into includes.users, matching the shape the step decodes.
func twitterTweetHandler(tweet mockTweet, user mockTwitterUser) gohttp.HandlerFunc {
	return func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": tweet,
			"includes": map[string]any{
				"users": []mockTwitterUser{user},
			},
		})
	}
}

func twitterMainFixture() (mockTweet, mockTwitterUser) {
	return mockTweet{
			ID:        twitterTweetID,
			Text:      twitterTweetText,
			AuthorID:  "111",
			CreatedAt: twitterTweetCreated,
		}, mockTwitterUser{
			ID: "111", Username: twitterAuthorHandle, Name: "Andrej Karpathy",
		}
}

func twitterSourceFixture() (mockTweet, mockTwitterUser) {
	return mockTweet{
			ID:        twitterSourceTweetID,
			Text:      "A test tweet",
			AuthorID:  "42",
			CreatedAt: "2026-01-01T00:00:00.000Z",
		}, mockTwitterUser{
			ID: "42", Username: "testuser", Name: "Test User",
		}
}

func twitterAsyncFixture() (mockTweet, mockTwitterUser) {
	return mockTweet{
		ID:        twitterAsyncTweetID,
		Text:      "async test",
		AuthorID:  "1",
		CreatedAt: "2026-01-01T00:00:00.000Z",
	}, mockTwitterUser{ID: "1", Username: "u", Name: "U"}
}

func twitterTweetPath(id string) string {
	return fmt.Sprintf("/2/tweets/%s?expansions=author_id", id)
}

func twitterCassettes() []captureFixture {
	main, mainUser := twitterMainFixture()
	src, srcUser := twitterSourceFixture()
	async, asyncUser := twitterAsyncFixture()
	return []captureFixture{
		{
			Cassette: "us0201_tweet_metadata",
			Handler:  twitterTweetHandler(main, mainUser),
			Path:     twitterTweetPath(twitterTweetID),
		},
		{
			Cassette: "us0201_tweet_source_url",
			Handler:  twitterTweetHandler(src, srcUser),
			Path:     twitterTweetPath(twitterSourceTweetID),
		},
		{
			Cassette: "us0201_async_capture",
			Handler:  twitterTweetHandler(async, asyncUser),
			Path:     twitterTweetPath(twitterAsyncTweetID),
		},
	}
}

// TestUS0201_RecordCassettes re-records the US-0201 cassettes. No-op
// unless XRR_MODE=record; needs no Postgres/Redis.
func TestUS0201_RecordCassettes(t *testing.T) {
	recordCaptureFixtures(t, twitterCassettes())
}

// ---------------------------------------------------------------------------
// US-0201 Tests
// ---------------------------------------------------------------------------

// TestUS0201_TweetTextAuthorTimestampStored verifies that tweet text, author,
// and timestamp are stored on the resulting KnowledgeObject.
func TestUS0201_TweetTextAuthorTimestampStored(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const tweetID = twitterTweetID
	const wantText = twitterTweetText
	const wantAuthorUsername = twitterAuthorHandle
	const wantCreatedAt = twitterTweetCreated

	// Replay answers from the cassette; the fixture server only matters
	// when re-recording.
	srv := httptest.NewServer(twitterTweetHandler(twitterMainFixture()))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.x.post", &pipeline.Pipeline{
		PipelineName: "social.x.post",
		Steps: []pipeline.PipelineStep{
			&twitterFetchStep{
				apiBaseURL: srv.URL,
				tweetID:    tweetID,
				client:     captureClient(t, "us0201_tweet_metadata"),
			},
		},
	})

	sourceURL := fmt.Sprintf("https://x.com/%s/status/%s", wantAuthorUsername, tweetID)
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "social.x.post",
		Source:   sourceURL,
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "social.post", obj.Type, "type must be social.post")
	assert.Equal(t, wantText, obj.Metadata["tweet_text"], "tweet text must be stored")
	assert.Equal(t, wantAuthorUsername, obj.Metadata["author_username"], "author username must be stored")
	assert.Equal(t, wantCreatedAt, obj.Metadata["created_at"], "timestamp must be stored")
}

// TestUS0201_TweetSourceURLStored verifies the original tweet URL is persisted.
func TestUS0201_TweetSourceURLStored(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const tweetID = twitterSourceTweetID

	srv := httptest.NewServer(twitterTweetHandler(twitterSourceFixture()))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.x.post", &pipeline.Pipeline{
		PipelineName: "social.x.post",
		Steps: []pipeline.PipelineStep{
			&twitterFetchStep{
				apiBaseURL: srv.URL,
				tweetID:    tweetID,
				client:     captureClient(t, "us0201_tweet_source_url"),
			},
		},
	})

	const sourceURL = "https://x.com/testuser/status/9999"
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "social.x.post",
		Source:   sourceURL,
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, sourceURL, obj.Source, "source URL must be preserved")
}

// TestUS0201_CaptureIsAsync verifies POST /analyze returns job_id immediately.
func TestUS0201_CaptureIsAsync(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	srv := httptest.NewServer(twitterTweetHandler(twitterAsyncFixture()))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.x.post", &pipeline.Pipeline{
		PipelineName: "social.x.post",
		Steps: []pipeline.PipelineStep{
			&twitterFetchStep{
				apiBaseURL: srv.URL,
				tweetID:    twitterAsyncTweetID,
				client:     captureClient(t, "us0201_async_capture"),
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://x.com/u/status/1",
		Type:     "url",
		Pipeline: "social.x.post",
		Source:   "https://x.com/u/status/1",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, jobID, "job_id must be returned immediately")
}
