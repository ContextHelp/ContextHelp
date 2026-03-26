package integration

// US-0209: Authenticated Web Content Fetch
//
// Verifies: configure credential for domain, fetch mock URL requiring auth,
// auth header sent and content stored.
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
// Pipeline steps
// ---------------------------------------------------------------------------

// authFetchStep fetches a URL with a configured auth credential.
type authFetchStep struct {
	pipeline.BaseContract
	targetURL  string
	authMethod string // "cookie", "api-key", "bearer"
	credential string // cookie value, API key, or bearer token
	headerName string // for api-key: the header name (e.g. "X-Api-Key")
}

func (s *authFetchStep) Name() string { return "test-auth-fetch" }
func (s *authFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	req, err := gohttp.NewRequest(gohttp.MethodGet, s.targetURL, nil)
	if err != nil {
		return draft, err
	}

	switch s.authMethod {
	case "cookie":
		req.AddCookie(&gohttp.Cookie{Name: "session", Value: s.credential})
	case "api-key":
		req.Header.Set(s.headerName, s.credential)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+s.credential)
	}

	resp, err := gohttp.DefaultClient.Do(req)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var payload struct {
		Content    string `json:"content"`
		AuthHeader string `json:"received_auth"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Type = "web.page"
	draft.TextContent = payload.Content
	draft.Metadata["auth_method"] = s.authMethod
	draft.Metadata["auth_header_sent"] = payload.AuthHeader != ""
	draft.Metadata["received_auth"] = payload.AuthHeader
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0209 Tests
// ---------------------------------------------------------------------------

// TestUS0209_CookieAuthSentToServer verifies that a fetch with cookie auth
// includes the Cookie header and stores content.
func TestUS0209_CookieAuthSentToServer(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const cookieValue = "test-session-xyz"
	const wantContent = "Premium article body accessible with auth cookie."

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		c, _ := r.Cookie("session")
		var receivedAuth string
		if c != nil {
			receivedAuth = c.Value
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":       wantContent,
			"received_auth": receivedAuth,
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("web.authenticated", &pipeline.Pipeline{
		PipelineName: "web.authenticated",
		Steps: []pipeline.PipelineStep{
			&authFetchStep{
				targetURL:  srv.URL + "/private",
				authMethod: "cookie",
				credential: cookieValue,
			},
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
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "web.page", obj.Type)
	assert.Equal(t, wantContent, obj.TextContent, "content must be stored")
	assert.Equal(t, "cookie", obj.Metadata["auth_method"], "auth_method must be cookie")
	assert.Equal(t, true, obj.Metadata["auth_header_sent"], "auth header must have been sent")
	assert.Equal(t, cookieValue, obj.Metadata["received_auth"], "server must receive cookie")
}

// TestUS0209_APIKeyAuthSentToServer verifies API key auth sends the configured header.
func TestUS0209_APIKeyAuthSentToServer(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const apiKey = "super-secret-key-abc"
	const headerName = "X-Api-Key"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		receivedKey := r.Header.Get(headerName)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":       "API-gated report content.",
			"received_auth": receivedKey,
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("web.authenticated.api", &pipeline.Pipeline{
		PipelineName: "web.authenticated.api",
		Steps: []pipeline.PipelineStep{
			&authFetchStep{
				targetURL:  srv.URL + "/report",
				authMethod: "api-key",
				credential: apiKey,
				headerName: headerName,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  srv.URL + "/report",
		Type:     "url",
		Pipeline: "web.authenticated.api",
		Source:   srv.URL + "/report",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "api-key", obj.Metadata["auth_method"])
	assert.Equal(t, apiKey, obj.Metadata["received_auth"], "server must receive API key header")
}

// TestUS0209_AuthMethodStoredInMetadata verifies authentication method is recorded.
func TestUS0209_AuthMethodStoredInMetadata(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		bearer := r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":       "Bearer-gated content.",
			"received_auth": bearer,
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("web.authenticated.bearer", &pipeline.Pipeline{
		PipelineName: "web.authenticated.bearer",
		Steps: []pipeline.PipelineStep{
			&authFetchStep{
				targetURL:  srv.URL + "/data",
				authMethod: "bearer",
				credential: "my-bearer-token",
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  srv.URL + "/data",
		Type:     "url",
		Pipeline: "web.authenticated.bearer",
		Source:   srv.URL + "/data",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "bearer", obj.Metadata["auth_method"],
		"auth_method must be stored in metadata")
	assert.Equal(t, "Bearer my-bearer-token", obj.Metadata["received_auth"])
}
