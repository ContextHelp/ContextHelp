package browser_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestIntegration_IBRFetcherFullFlow tests the end-to-end flow:
// mock daemon → client → ibr_fetcher step → populated KnowledgeObject.
func TestIntegration_IBRFetcherFullFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			json.NewEncoder(w).Encode(map[string]any{
				"status":        "healthy",
				"uptime":        10,
				"pid":           9999,
				"activeClients": 1,
				"queueDepth":    0,
			})
		case "/command":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			args := body["args"].([]any)
			prompt := args[0].(string)

			assert.Contains(t, prompt, "https://js-heavy-site.example.com")
			assert.Contains(t, prompt, "click the login button")

			json.NewEncoder(w).Encode(map[string]any{
				"extracts": []map[string]any{
					{
						"html":  "<div class='dashboard'><h1>Welcome Back</h1><p>Your projects</p></div>",
						"title": "Dashboard",
					},
				},
				"tokenUsage": map[string]int{
					"prompt":     200,
					"completion": 80,
					"total":      280,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// 1. Client health check.
	client := browser.NewClient(srv.URL, "integration-token")
	health, err := client.Health(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "healthy", health.Status)
	assert.Equal(t, 9999, health.PID)

	// 2. IBR fetcher step with custom instructions.
	fetcher := steps.NewIBRFetcher(client)
	draft := &storage.KnowledgeObject{
		Source: "https://js-heavy-site.example.com/dashboard",
		Metadata: map[string]any{
			"ibr_instructions": []any{
				"click the login button",
				"extract the dashboard content",
			},
		},
	}

	result, err := fetcher.Run(context.Background(), draft)
	require.NoError(t, err)

	// 3. Verify RawContent populated from "html" key in extracts.
	assert.Contains(t, result.RawContent, "Welcome Back")
	assert.Contains(t, result.RawContent, "dashboard")

	// 4. Verify metadata.
	assert.Equal(t, "https://js-heavy-site.example.com/dashboard", result.Metadata["source_url"])

	tokenUsage, ok := result.Metadata["ibr_token_usage"]
	require.True(t, ok, "ibr_token_usage should be in metadata")
	tu, ok := tokenUsage.(browser.TokenUsage)
	require.True(t, ok, "should be TokenUsage type")
	assert.Equal(t, 280, tu.Total)

	_, ok = result.Metadata["ibr_extracts"]
	assert.True(t, ok, "ibr_extracts should be in metadata")
}
