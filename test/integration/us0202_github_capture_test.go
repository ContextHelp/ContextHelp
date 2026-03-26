package integration

// US-0202: GitHub Repository and PR Capture
//
// Verifies: mock GitHub API, capture repo/issue/PR by URL,
// title/body/labels stored on resulting objects.
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
// Mock GitHub API payloads
// ---------------------------------------------------------------------------

type mockGitHubRepo struct {
	FullName    string   `json:"full_name"`
	Description string   `json:"description"`
	Language    string   `json:"language"`
	Topics      []string `json:"topics"`
	StarCount   int      `json:"stargazers_count"`
}

type mockGitHubIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type mockGitHubPR struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// ---------------------------------------------------------------------------
// Pipeline steps
// ---------------------------------------------------------------------------

// githubRepoFetchStep calls the mock GitHub repos API.
type githubRepoFetchStep struct {
	pipeline.BaseContract
	apiBaseURL string
	owner, repo string
}

func (s *githubRepoFetchStep) Name() string { return "test-github-repo-fetch" }
func (s *githubRepoFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", s.apiBaseURL, s.owner, s.repo)
	resp, err := gohttp.Get(url)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var r mockGitHubRepo
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Type = "code.repo"
	draft.TextContent = r.Description
	draft.Metadata["repo_full_name"] = r.FullName
	draft.Metadata["description"] = r.Description
	draft.Metadata["language"] = r.Language
	draft.Metadata["topics"] = r.Topics
	draft.Metadata["stars"] = r.StarCount
	return draft, nil
}

// githubIssueFetchStep calls the mock GitHub issues API.
type githubIssueFetchStep struct {
	pipeline.BaseContract
	apiBaseURL       string
	owner, repo      string
	issueNumber      int
}

func (s *githubIssueFetchStep) Name() string { return "test-github-issue-fetch" }
func (s *githubIssueFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d", s.apiBaseURL, s.owner, s.repo, s.issueNumber)
	resp, err := gohttp.Get(url)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var issue mockGitHubIssue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Type = "code.issue"
	draft.TextContent = issue.Body
	draft.Metadata["issue_number"] = issue.Number
	draft.Metadata["title"] = issue.Title
	draft.Metadata["body"] = issue.Body
	draft.Metadata["state"] = issue.State
	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.Name
	}
	draft.Metadata["labels"] = labels
	return draft, nil
}

// githubPRFetchStep calls the mock GitHub pulls API.
type githubPRFetchStep struct {
	pipeline.BaseContract
	apiBaseURL  string
	owner, repo string
	prNumber    int
}

func (s *githubPRFetchStep) Name() string { return "test-github-pr-fetch" }
func (s *githubPRFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", s.apiBaseURL, s.owner, s.repo, s.prNumber)
	resp, err := gohttp.Get(url)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var pr mockGitHubPR
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Type = "code.pr"
	draft.TextContent = pr.Body
	draft.Metadata["pr_number"] = pr.Number
	draft.Metadata["title"] = pr.Title
	draft.Metadata["body"] = pr.Body
	draft.Metadata["state"] = pr.State
	draft.Metadata["additions"] = pr.Additions
	draft.Metadata["deletions"] = pr.Deletions
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0202 Tests
// ---------------------------------------------------------------------------

// TestUS0202_RepoTitleDescriptionStored verifies repo full_name and description
// are stored on the captured KnowledgeObject.
func TestUS0202_RepoTitleDescriptionStored(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const owner = "golang"
	const repo = "go"
	const wantDesc = "The Go programming language"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockGitHubRepo{
			FullName:    fmt.Sprintf("%s/%s", owner, repo),
			Description: wantDesc,
			Language:    "Go",
			Topics:      []string{"go", "programming-language"},
			StarCount:   120000,
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("code.github.repo", &pipeline.Pipeline{
		PipelineName: "code.github.repo",
		Steps: []pipeline.PipelineStep{
			&githubRepoFetchStep{apiBaseURL: srv.URL, owner: owner, repo: repo},
		},
	})

	sourceURL := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "code.github.repo",
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

	assert.Equal(t, "code.repo", obj.Type)
	assert.Equal(t, "golang/go", obj.Metadata["repo_full_name"])
	assert.Equal(t, wantDesc, obj.Metadata["description"])
}

// TestUS0202_IssueTitleBodyLabelsStored verifies issue title, body, and labels
// are stored on the captured KnowledgeObject.
func TestUS0202_IssueTitleBodyLabelsStored(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const owner = "golang"
	const repo = "go"
	const issueNum = 456

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockGitHubIssue{
			Number: issueNum,
			Title:  "Improve error messages in net/http",
			Body:   "The error messages in net/http are sometimes misleading.",
			State:  "open",
			Labels: []struct {
				Name string `json:"name"`
			}{
				{Name: "enhancement"},
				{Name: "NeedsDecision"},
			},
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("code.github.issue", &pipeline.Pipeline{
		PipelineName: "code.github.issue",
		Steps: []pipeline.PipelineStep{
			&githubIssueFetchStep{apiBaseURL: srv.URL, owner: owner, repo: repo, issueNumber: issueNum},
		},
	})

	sourceURL := fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, issueNum)
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "code.github.issue",
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

	assert.Equal(t, "code.issue", obj.Type)
	assert.Equal(t, "Improve error messages in net/http", obj.Metadata["title"])
	assert.Equal(t, "The error messages in net/http are sometimes misleading.", obj.Metadata["body"])
	// JSON decode round-trips slices as []any.
	labelsRaw, _ := obj.Metadata["labels"].([]any)
	labels := make([]string, len(labelsRaw))
	for i, l := range labelsRaw {
		labels[i], _ = l.(string)
	}
	assert.Contains(t, labels, "enhancement")
	assert.Contains(t, labels, "NeedsDecision")
}

// TestUS0202_PRTitleBodyStored verifies PR title and body are stored.
func TestUS0202_PRTitleBodyStored(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const owner = "golang"
	const repo = "go"
	const prNum = 123

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockGitHubPR{
			Number:    prNum,
			Title:     "net/http: add structured logging support",
			Body:      "This PR adds slog integration to net/http server.",
			State:     "open",
			Additions: 342,
			Deletions: 89,
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("code.github.pr", &pipeline.Pipeline{
		PipelineName: "code.github.pr",
		Steps: []pipeline.PipelineStep{
			&githubPRFetchStep{apiBaseURL: srv.URL, owner: owner, repo: repo, prNumber: prNum},
		},
	})

	sourceURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, prNum)
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "code.github.pr",
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

	assert.Equal(t, "code.pr", obj.Type)
	assert.Equal(t, "net/http: add structured logging support", obj.Metadata["title"])
	assert.Equal(t, "This PR adds slog integration to net/http server.", obj.Metadata["body"])
	assert.EqualValues(t, 342, obj.Metadata["additions"])
	assert.EqualValues(t, 89, obj.Metadata["deletions"])
}
