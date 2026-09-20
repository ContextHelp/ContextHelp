package integration

// US-0204: LinkedIn Profile and Post Capture
//
// Verifies: mock LinkedIn scraper, capture profile/post,
// content stored with source attribution.
// Each scraper fetch runs through an xrr cassette
// (testdata/cassettes/us0204_*). In the default replay mode the recorded
// round-trip answers from disk and the fixture server is never contacted.
// See xrr_capture_helpers_test.go for what the cassettes do and do not
// prove.
//
// Re-record: XRR_MODE=record go test -count=1 ./test/integration/ \
//   -run TestUS0204_RecordCassettes
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
	// client carries the xrr record/replay transport. Nil falls back to
	// the default client.
	client *gohttp.Client
}

func (s *linkedInProfileFetchStep) Name() string { return "test-linkedin-profile-fetch" }
func (s *linkedInProfileFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/profile/%s", s.scraperURL, s.username)
	resp, err := captureGet(s.client, url)
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
	// client carries the xrr record/replay transport. Nil falls back to
	// the default client.
	client *gohttp.Client
}

func (s *linkedInPostFetchStep) Name() string { return "test-linkedin-post-fetch" }
func (s *linkedInPostFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/post/%s", s.scraperURL, s.postID)
	resp, err := captureGet(s.client, url)
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
// Fixture handlers
//
// Named so the replaying test and the recorder share one definition of
// each payload. See xrr_capture_helpers_test.go.
// ---------------------------------------------------------------------------

const (
	linkedInUsername      = "jane-doe"
	linkedInProfileURL    = "https://www.linkedin.com/in/jane-doe"
	linkedInPostID        = "7001234567890"
	linkedInPostText      = "Excited to share our latest work on distributed tracing."
	linkedInPostSourceURL = "https://www.linkedin.com/posts/jane-doe_activity-7001234567890"
)

// linkedInJSONHandler serves one JSON body for any path, matching the
// previous inline handlers, which also ignored the request path.
func linkedInJSONHandler(payload any) gohttp.HandlerFunc {
	return func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func linkedInProfileFixture() mockLinkedInProfile {
	return mockLinkedInProfile{
		Username:  linkedInUsername,
		FullName:  "Jane Doe",
		Headline:  "Principal Engineer at Acme Corp",
		About:     "Building distributed systems for 10 years.",
		Location:  "San Francisco, CA",
		Skills:    []string{"Go", "Kubernetes", "Distributed Systems"},
		SourceURL: linkedInProfileURL,
	}
}

func linkedInPostFixture() mockLinkedInPost {
	return mockLinkedInPost{
		PostID:    linkedInPostID,
		Author:    "Jane Doe",
		Text:      linkedInPostText,
		Timestamp: "2026-02-01T09:00:00Z",
		Likes:     147,
		SourceURL: linkedInPostSourceURL,
	}
}

func linkedInCassettes() []captureFixture {
	return []captureFixture{
		{
			Cassette: "us0204_profile_attribution",
			Handler:  linkedInJSONHandler(linkedInProfileFixture()),
			Path:     fmt.Sprintf("/profile/%s", linkedInUsername),
		},
		{
			Cassette: "us0204_post_attribution",
			Handler:  linkedInJSONHandler(linkedInPostFixture()),
			Path:     fmt.Sprintf("/post/%s", linkedInPostID),
		},
	}
}

// TestUS0204_RecordCassettes re-records the US-0204 cassettes. No-op
// unless XRR_MODE=record; needs no Postgres/Redis.
func TestUS0204_RecordCassettes(t *testing.T) {
	recordCaptureFixtures(t, linkedInCassettes())
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

	const username = linkedInUsername
	const sourceURL = linkedInProfileURL

	// Replay answers from the cassette; the fixture server only matters
	// when re-recording.
	srv := httptest.NewServer(linkedInJSONHandler(linkedInProfileFixture()))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.linkedin.profile", &pipeline.Pipeline{
		PipelineName: "social.linkedin.profile",
		Steps: []pipeline.PipelineStep{
			&linkedInProfileFetchStep{
				scraperURL: srv.URL,
				username:   username,
				client:     captureClient(t, "us0204_profile_attribution"),
			},
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

	const postID = linkedInPostID
	const wantText = linkedInPostText
	const sourceURL = linkedInPostSourceURL

	srv := httptest.NewServer(linkedInJSONHandler(linkedInPostFixture()))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("social.linkedin.post", &pipeline.Pipeline{
		PipelineName: "social.linkedin.post",
		Steps: []pipeline.PipelineStep{
			&linkedInPostFetchStep{
				scraperURL: srv.URL,
				postID:     postID,
				client:     captureClient(t, "us0204_post_attribution"),
			},
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
