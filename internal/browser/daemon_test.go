package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Health_ReturnsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]any{
			"status":        "healthy",
			"uptime":        42,
			"pid":           1234,
			"activeClients": 0,
			"queueDepth":    0,
		})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, token: "test-token", httpClient: srv.Client()}
	health, err := c.Health(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "healthy", health.Status)
	assert.Equal(t, 1234, health.PID)
}

func TestClient_Execute_SendsCommandAndReturnsExtracts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/command", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "task", body["command"])

		w.Write([]byte(`{"extracts":[{"title":"Example"}],"tokenUsage":{"prompt":100,"completion":50,"total":150}}`))
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, token: "test-token", httpClient: srv.Client()}
	result, err := c.Execute(context.Background(), "url: https://example.com\ninstructions:\n  - extract title")
	require.NoError(t, err)
	assert.Len(t, result.Extracts, 1)
	assert.Equal(t, 150, result.TokenUsage.Total)
}

func TestClient_Health_ErrorOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, token: "t", httpClient: srv.Client()}
	_, err := c.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestClient_Execute_ErrorOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, token: "t", httpClient: srv.Client()}
	_, err := c.Execute(context.Background(), "test prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}
