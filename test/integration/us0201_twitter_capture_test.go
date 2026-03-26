package integration

// US-0201: X/Twitter Profile and Thread Capture
//
// Verifies: mock Twitter API, capture tweet by URL, tweet text/author/timestamp
// stored as object.
// All external HTTP calls use httptest.NewServer — no real network.
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
}

func (s *twitterFetchStep) Name() string { return "test-twitter-fetch" }
func (s *twitterFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/2/tweets/%s?expansions=author_id", s.apiBaseURL, s.tweetID)
	resp, err := gohttp.Get(url)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var payload struct {
		Data     mockTweet       `json:"data"`
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
// US-0201 Tests
// ---------------------------------------------------------------------------

// TestUS0201_TweetTextAuthorTimestampStored verifies that tweet text, author,
// and timestamp are stored on the resulting KnowledgeObject.
func TestUS0201_TweetTextAuthorTimestampStored(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const tweetID = "1234567890"
	const wantText = "Distributed systems require careful coordination."
	const wantAuthorUsername = "karpathy"
	const wantCreatedAt = "2026-02-18T10:00:00.000Z"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": mockTweet{
				ID:        tweetID,
				Text:      wantText,
				AuthorID:  "111",
				CreatedAt: wantCreatedAt,
			},
			"includes": map[string]any{
				"users": []mockTwitterUser{
					{ID: "111", Username: wantAuthorUsername, Name: "Andrej Karpathy"},
				},
			},
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.x.post", &pipeline.Pipeline{
		PipelineName: "social.x.post",
		Steps: []pipeline.PipelineStep{
			&twitterFetchStep{apiBaseURL: srv.URL, tweetID: tweetID},
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

	const tweetID = "9999"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": mockTweet{
				ID:        tweetID,
				Text:      "A test tweet",
				AuthorID:  "42",
				CreatedAt: "2026-01-01T00:00:00.000Z",
			},
			"includes": map[string]any{
				"users": []mockTwitterUser{
					{ID: "42", Username: "testuser", Name: "Test User"},
				},
			},
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.x.post", &pipeline.Pipeline{
		PipelineName: "social.x.post",
		Steps: []pipeline.PipelineStep{
			&twitterFetchStep{apiBaseURL: srv.URL, tweetID: tweetID},
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

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": mockTweet{ID: "1", Text: "async test", AuthorID: "1", CreatedAt: "2026-01-01T00:00:00.000Z"},
			"includes": map[string]any{
				"users": []mockTwitterUser{{ID: "1", Username: "u", Name: "U"}},
			},
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.x.post", &pipeline.Pipeline{
		PipelineName: "social.x.post",
		Steps: []pipeline.PipelineStep{
			&twitterFetchStep{apiBaseURL: srv.URL, tweetID: "1"},
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
