package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// sendJSON issues method on path with a raw body and returns the status
// and decoded error code (empty on a non-error answer).
func sendJSON(t *testing.T, ts *testServerBundle, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ownerCtx(), method, ts.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var env ErrorEnvelope
	_ = json.NewDecoder(resp.Body).Decode(&env)
	return resp.StatusCode, env.Error.Code
}

// A malformed triage body is refused before the item is looked up; an
// empty one still means "detect a pipeline".
func TestTriageInbox_MalformedBody(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	code, errCode := sendJSON(t, ts, http.MethodPost, "/api/v1/inbox/i-missing/triage", `{"pipeline":`)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "INVALID_REQUEST", errCode)

	code, _ = sendJSON(t, ts, http.MethodPost, "/api/v1/inbox/i-missing/triage", "")
	assert.Equal(t, http.StatusNotFound, code, "an empty body is not a malformed one")
}

// A watch patch that does not decode is refused whole: no field of it is
// applied, where a type error used to apply the fields before it.
func TestUpdateWatch_MalformedBody(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	ctx := ownerCtx()
	watches := ts.svc.Store(ownerCtx()).Watches()
	require.NoError(t, watches.CreateWatch(ctx, &storage.WatchConfig{
		ID: "w-1", Path: t.TempDir(), Mode: "generic", Status: "active",
		IncludePatterns: []string{"*.md"}, DebounceMS: 500,
	}))

	code, errCode := sendJSON(t, ts, http.MethodPatch, "/api/v1/watches/w-1",
		`{"include_patterns":["*.txt"],"debounce_ms":"fast"}`)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "INVALID_REQUEST", errCode)

	got, err := watches.GetWatch(ctx, "w-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"*.md"}, got.IncludePatterns, "a refused patch changed the watch")
	assert.Equal(t, 500, got.DebounceMS)

	code, _ = sendJSON(t, ts, http.MethodPatch, "/api/v1/watches/w-1", "")
	assert.Equal(t, http.StatusOK, code, "an empty patch is a no-op, not an error")
}
