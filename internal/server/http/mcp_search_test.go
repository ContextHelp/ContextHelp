package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/mcp"
	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

type mcpSearchOut struct {
	Mode         string `json:"mode"`
	ExecutedMode string `json:"executed_mode"`
	Count        int    `json:"count"`
	Results      []struct {
		ID             string          `json:"id"`
		Title          string          `json:"title"`
		Snippet        string          `json:"snippet"`
		ProfileID      string          `json:"profile_id"`
		Score          *float64        `json:"score"`
		ScoreBreakdown json.RawMessage `json:"score_breakdown"`
	} `json:"results"`
	Diagnostics struct {
		Semantic struct {
			Status string `json:"status"`
			Notice string `json:"notice"`
		} `json:"semantic"`
	} `json:"diagnostics"`
}

// mcpSearchServer serves the dpkms router over the profile corpus, its
// semantic leg reaching a fake embedding provider when withModel is set;
// without it there is no default model.
func mcpSearchServer(t *testing.T, withModel bool) *httptest.Server {
	t.Helper()
	f := servicetest.NewHybridFixture(t, storageutil.NewTestDriver(t), withModel)
	f.SeedProfileCorpus(t)
	srv := httptest.NewServer(NewRouterWithConfig(f.Svc, RouterConfig{Semantic: f.Sem}))
	t.Cleanup(srv.Close)
	return srv
}

// callMCPSearch calls the search tool on the dpkms router's MCP endpoint.
func callMCPSearch(t *testing.T, url string, args map[string]any) (mcpSearchOut, *mcp.JSONRPCError) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "search", "arguments": args},
	})
	require.NoError(t, err)
	resp, err := http.Post(url+"/api/v1/mcp/", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	var rpc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *mcp.JSONRPCError `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpc))
	var out mcpSearchOut
	if rpc.Error == nil {
		require.NotEmpty(t, rpc.Result.Content)
		require.NoError(t, json.Unmarshal([]byte(rpc.Result.Content[0].Text), &out))
	}
	return out, rpc.Error
}

func TestMCPSearch_HybridByDefault(t *testing.T) {
	srv := mcpSearchServer(t, true)
	out, rpcErr := callMCPSearch(t, srv.URL, map[string]any{"query": servicetest.ProfileQuery, "profile": "alpha"})
	require.Nil(t, rpcErr)
	assert.Equal(t, "hybrid", out.Mode)
	assert.Equal(t, "hybrid", out.ExecutedMode)
	assert.Equal(t, "ok", out.Diagnostics.Semantic.Status)
	require.Len(t, out.Results, 2)
	ids := []string{out.Results[0].ID, out.Results[1].ID}
	assert.ElementsMatch(t, []string{"alpha-text", "alpha-vec"}, ids)
	for _, r := range out.Results {
		require.NotNil(t, r.Score, r.ID)
		assert.NotEmpty(t, r.ScoreBreakdown, r.ID)
		assert.NotEmpty(t, r.Snippet, r.ID)
		assert.Equal(t, "alpha", r.ProfileID)
	}
}

func TestMCPSearch_ReportsFallback(t *testing.T) {
	srv := mcpSearchServer(t, false)
	out, rpcErr := callMCPSearch(t, srv.URL, map[string]any{"query": servicetest.ProfileQuery, "profile": "alpha"})
	require.Nil(t, rpcErr)
	assert.Equal(t, "fts_only", out.ExecutedMode)
	assert.Equal(t, "no_default_model", out.Diagnostics.Semantic.Status)
	assert.NotEmpty(t, out.Diagnostics.Semantic.Notice)
	require.Len(t, out.Results, 1)
	assert.Equal(t, "alpha-text", out.Results[0].ID)
}

func TestMCPSearch_BadArgumentsAreInvalidParams(t *testing.T) {
	srv := mcpSearchServer(t, true)
	for name, args := range map[string]map[string]any{
		// Per ADR-068 the tool takes free text; RSQL stays on REST and gRPC.
		"negative min_score": {"query": "q", "min_score": -1},
		"rsql mode":          {"query": "type==note", "mode": "rsql"},
		"min_score with fts": {"query": "q", "mode": "fts", "min_score": 0.5},
	} {
		t.Run(name, func(t *testing.T) {
			_, rpcErr := callMCPSearch(t, srv.URL, args)
			require.NotNil(t, rpcErr)
			assert.Equal(t, mcp.ErrInvalidParams, rpcErr.Code, rpcErr.Message)
		})
	}
}

// Vector mode scores each hit by similarity and scopes to the profile;
// fts hits carry no score.
func TestMCPSearch_VectorAndFTSScores(t *testing.T) {
	srv := mcpSearchServer(t, true)
	out, rpcErr := callMCPSearch(t, srv.URL, map[string]any{"query": servicetest.ProfileQuery, "mode": "vector", "profile": "alpha"})
	require.Nil(t, rpcErr)
	assert.Equal(t, "vector", out.ExecutedMode)
	require.Len(t, out.Results, 2)
	for _, r := range out.Results {
		require.NotNil(t, r.Score, r.ID)
		assert.Empty(t, r.ScoreBreakdown, r.ID)
	}

	out, rpcErr = callMCPSearch(t, srv.URL, map[string]any{"query": servicetest.ProfileQuery, "mode": "fts", "profile": "alpha"})
	require.Nil(t, rpcErr)
	assert.Equal(t, "fts", out.ExecutedMode)
	require.Len(t, out.Results, 1)
	assert.Nil(t, out.Results[0].Score)
}
