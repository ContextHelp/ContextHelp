package integration

// US-0204: LinkedIn Profile and Post Capture
//
// Verifies: mock LinkedIn scraper, capture profile/post,
// content stored with source attribution.
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
// Mock LinkedIn scraper payloads
// ---------------------------------------------------------------------------

type mockLinkedInProfile struct {
	Username  string   `json:"username"`
	FullName  string   `json:"full_name"`
	Headline  string   `json:"headline"`
	About     string   `json:"about"`
	Location  string   `json:"location"`
	Skills    []string `json:"skills"`
	SourceURL string   `json:"source_url"`
}

type mockLinkedInPost struct {
	PostID    string `json:"post_id"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
	Likes     int    `json:"likes"`
	SourceURL string `json:"source_url"`
}

// ---------------------------------------------------------------------------
// Pipeline steps
// ---------------------------------------------------------------------------

// linkedInProfileFetchStep calls the mock LinkedIn scraper for a profile.
type linkedInProfileFetchStep struct {
	pipeline.BaseContract
	scraperURL string
	username   string
}

func (s *linkedInProfileFetchStep) Name() string { return "test-linkedin-profile-fetch" }
func (s *linkedInProfileFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/profile/%s", s.scraperURL, s.username)
	resp, err := gohttp.Get(url)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var profile mockLinkedInProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Type = "social.profile"
	draft.TextContent = profile.About
	draft.Source = profile.SourceURL
	draft.Metadata["platform"] = "linkedin"
	draft.Metadata["username"] = profile.Username
	draft.Metadata["full_name"] = profile.FullName
	draft.Metadata["headline"] = profile.Headline
	draft.Metadata["about"] = profile.About
	draft.Metadata["location"] = profile.Location
	draft.Metadata["skills"] = profile.Skills
	return draft, nil
}

// linkedInPostFetchStep calls the mock LinkedIn scraper for a post.
type linkedInPostFetchStep struct {
	pipeline.BaseContract
	scraperURL string
	postID     string
}

func (s *linkedInPostFetchStep) Name() string { return "test-linkedin-post-fetch" }
func (s *linkedInPostFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/post/%s", s.scraperURL, s.postID)
	resp, err := gohttp.Get(url)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var post mockLinkedInPost
	if err := json.NewDecoder(resp.Body).Decode(&post); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Type = "social.post"
	draft.TextContent = post.Text
	draft.Source = post.SourceURL
	draft.Metadata["platform"] = "linkedin"
	draft.Metadata["post_id"] = post.PostID
	draft.Metadata["author"] = post.Author
	draft.Metadata["text"] = post.Text
	draft.Metadata["timestamp"] = post.Timestamp
	draft.Metadata["likes"] = post.Likes
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0204 Tests
// ---------------------------------------------------------------------------

// TestUS0204_ProfileContentStoredWithAttribution verifies LinkedIn profile
// content is stored with source attribution.
func TestUS0204_ProfileContentStoredWithAttribution(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const username = "jane-doe"
	const sourceURL = "https://www.linkedin.com/in/jane-doe"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockLinkedInProfile{
			Username:  username,
			FullName:  "Jane Doe",
			Headline:  "Principal Engineer at Acme Corp",
			About:     "Building distributed systems for 10 years.",
			Location:  "San Francisco, CA",
			Skills:    []string{"Go", "Kubernetes", "Distributed Systems"},
			SourceURL: sourceURL,
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.linkedin.profile", &pipeline.Pipeline{
		PipelineName: "social.linkedin.profile",
		Steps: []pipeline.PipelineStep{
			&linkedInProfileFetchStep{scraperURL: srv.URL, username: username},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "social.linkedin.profile",
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

	assert.Equal(t, "social.profile", obj.Type)
	assert.Equal(t, sourceURL, obj.Source, "source attribution must be stored")
	assert.Equal(t, "linkedin", obj.Metadata["platform"])
	assert.Equal(t, "Jane Doe", obj.Metadata["full_name"])
	assert.Equal(t, "Principal Engineer at Acme Corp", obj.Metadata["headline"])
}

// TestUS0204_PostContentStoredWithAttribution verifies LinkedIn post content
// is stored with source attribution.
func TestUS0204_PostContentStoredWithAttribution(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const postID = "7001234567890"
	const wantText = "Excited to share our latest work on distributed tracing."
	const sourceURL = "https://www.linkedin.com/posts/jane-doe_activity-7001234567890"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockLinkedInPost{
			PostID:    postID,
			Author:    "Jane Doe",
			Text:      wantText,
			Timestamp: "2026-02-01T09:00:00Z",
			Likes:     147,
			SourceURL: sourceURL,
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.linkedin.post", &pipeline.Pipeline{
		PipelineName: "social.linkedin.post",
		Steps: []pipeline.PipelineStep{
			&linkedInPostFetchStep{scraperURL: srv.URL, postID: postID},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "social.linkedin.post",
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

	assert.Equal(t, "social.post", obj.Type)
	assert.Equal(t, sourceURL, obj.Source, "source attribution must be stored")
	assert.Equal(t, "linkedin", obj.Metadata["platform"])
	assert.Equal(t, wantText, obj.Metadata["text"])
	assert.Equal(t, "Jane Doe", obj.Metadata["author"])
}
