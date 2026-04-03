package steps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestIBRFetcher_Name(t *testing.T) {
	f := NewIBRFetcher(nil)
	assert.Equal(t, "ibr_fetcher", f.Name())
}

func TestIBRFetcher_Contract(t *testing.T) {
	f := NewIBRFetcher(nil)
	c := f.Contract()
	assert.Contains(t, c.Requires, "Source")
	assert.Contains(t, c.Produces, "RawContent")
	assert.Contains(t, c.Produces, "Metadata")
	assert.Contains(t, c.Capabilities, "browser")
}

func TestIBRFetcher_Run_FetchesViaIBRDaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		args := body["args"].([]any)
		prompt := args[0].(string)
		assert.Contains(t, prompt, "https://example.com")

		json.NewEncoder(w).Encode(map[string]any{
			"extracts": []map[string]any{
				{"text": "<h1>Hello World</h1><p>Content here</p>"},
			},
			"tokenUsage": map[string]int{"prompt": 100, "completion": 50, "total": 150},
		})
	}))
	defer srv.Close()

	client := browser.NewClient(srv.URL, "test")
	f := NewIBRFetcher(client)

	draft := &storage.KnowledgeObject{
		Source: "https://example.com",
	}

	result, err := f.Run(context.Background(), draft)
	require.NoError(t, err)
	assert.Contains(t, result.RawContent, "Hello World")
	assert.Equal(t, "https://example.com", result.Metadata["source_url"])
}

func TestIBRFetcher_Run_NilClient_ReturnsError(t *testing.T) {
	f := NewIBRFetcher(nil)
	draft := &storage.KnowledgeObject{Source: "https://example.com"}
	_, err := f.Run(context.Background(), draft)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browser not available")
}

func TestIBRFetcher_Run_NoURL_ReturnsError(t *testing.T) {
	client := browser.NewClient("http://localhost:1", "tok")
	f := NewIBRFetcher(client)
	draft := &storage.KnowledgeObject{}
	_, err := f.Run(context.Background(), draft)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no URL")
}
