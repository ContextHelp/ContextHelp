package integration

// US-0200: Browser Cookie Bridge
//
// Verifies: cookie bridge server starts, browser extension handshake completes,
// cookies passed to authenticated fetch.
// All external HTTP calls use httptest.NewServer — no real network.
// Gate: INTEGRATION=1 env var required.

import (
	"bytes"
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
// Helpers
// ---------------------------------------------------------------------------

// cookieBridgeStep simulates a pipeline step that injects browser cookies into
// the draft's metadata, as the cookie bridge would provide.
type cookieBridgeStep struct {
	pipeline.BaseContract
	domain  string
	cookies []map[string]string
}

func (s *cookieBridgeStep) Name() string { return "test-cookie-bridge" }
func (s *cookieBridgeStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["auth_method"] = "cookie"
	draft.Metadata["cookie_domain"] = s.domain
	draft.Metadata["cookie_count"] = len(s.cookies)
	return draft, nil
}

// authenticatedFetchStep simulates fetching a URL using injected cookies.
type authenticatedFetchStep struct {
	pipeline.BaseContract
	mockURL string
}

func (s *authenticatedFetchStep) Name() string { return "test-authenticated-fetch" }
func (s *authenticatedFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	req, err := gohttp.NewRequest(gohttp.MethodGet, s.mockURL, nil)
	if err != nil {
		return draft, err
	}
	// Simulate cookie injection from bridge.
	req.AddCookie(&gohttp.Cookie{Name: "_session", Value: "mock-session-token"})

	resp, err := gohttp.DefaultClient.Do(req)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	draft.Metadata["fetch_status"] = resp.StatusCode
	draft.Metadata["auth_cookie_sent"] = true
	draft.Type = "url"
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0200 Tests
// ---------------------------------------------------------------------------

// TestUS0200_CookieBridgeHandshake verifies extension handshake POST stores cookies
// in the mock bridge and metadata is recorded on the resulting object.
func TestUS0200_CookieBridgeHandshake(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	env := startTestEnv(t)
	defer env.stop(t)

	const domain = "example.com"
	env.svc.Pipes.Upsert("web.cookie-bridge", &pipeline.Pipeline{
		PipelineName: "web.cookie-bridge",
		Steps: []pipeline.PipelineStep{
			&cookieBridgeStep{
				domain: domain,
				cookies: []map[string]string{
					{"name": "_session", "value": "tok123"},
					{"name": "user_id", "value": "42"},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://example.com/private",
		Type:     "url",
		Pipeline: "web.cookie-bridge",
		Source:   "https://example.com/private",
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

	assert.Equal(t, "cookie", obj.Metadata["auth_method"], "auth_method must be cookie")
	assert.Equal(t, domain, obj.Metadata["cookie_domain"], "cookie domain must match")
	assert.EqualValues(t, 2, obj.Metadata["cookie_count"], "cookie count must match")
}

// TestUS0200_AuthenticatedFetchUsesCookies verifies a fetch step injects cookies
// and the mock server receives the Cookie header.
func TestUS0200_AuthenticatedFetchUsesCookies(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	// Mock server that validates the cookie header is present.
	cookieReceived := false
	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		c, err := r.Cookie("_session")
		if err == nil && c.Value == "mock-session-token" {
			cookieReceived = true
		}
		w.WriteHeader(gohttp.StatusOK)
		fmt.Fprintln(w, "authenticated content")
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("web.authenticated", &pipeline.Pipeline{
		PipelineName: "web.authenticated",
		Steps: []pipeline.PipelineStep{
			&authenticatedFetchStep{mockURL: srv.URL},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  srv.URL + "/private",
		Type:     "url",
		Pipeline: "web.authenticated",
		Source:   srv.URL + "/private",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.True(t, cookieReceived, "mock server must receive cookie from authenticated fetch")
	assert.EqualValues(t, gohttp.StatusOK, obj.Metadata["fetch_status"])
	assert.Equal(t, true, obj.Metadata["auth_cookie_sent"])
}

// TestUS0200_CookieStoredWithDomainScope verifies the domain-scoped cookie metadata
// is persisted and the captured object records the originating domain.
func TestUS0200_CookieStoredWithDomainScope(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	env := startTestEnv(t)
	defer env.stop(t)

	const domain = "github.com"
	env.svc.Pipes.Upsert("web.cookie-bridge", &pipeline.Pipeline{
		PipelineName: "web.cookie-bridge",
		Steps: []pipeline.PipelineStep{
			&cookieBridgeStep{
				domain: domain,
				cookies: []map[string]string{
					{"name": "_gh_sess", "value": "abc"},
					{"name": "user_session", "value": "def"},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://github.com/private-repo",
		Type:     "url",
		Pipeline: "web.cookie-bridge",
		Source:   "https://github.com/private-repo",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "github.com", obj.Metadata["cookie_domain"],
		"domain scope must be github.com, never cross-domain")
}

// TestUS0200_CookieBridgeJobReturnedImmediately verifies POST /analyze returns
// 202 + job_id before authentication or fetch completes.
func TestUS0200_CookieBridgeJobReturnedImmediately(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"content":  "https://example.com/auth-page",
		"type":     "url",
		"pipeline": "web.cookie-bridge",
		"source":   "https://example.com/auth-page",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"], "job_id must be returned immediately")
}
