package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIClient_DeletePipeline_ForwardsNoteHeader confirms a non-empty
// note populates X-Ctxt-Note and an empty note omits it. The server
// just records what it sees.
func TestAPIClient_DeletePipeline_ForwardsNoteHeader(t *testing.T) {
	cases := []struct {
		name string
		note string
		want string
	}{
		{name: "note set", note: "rotating fixture", want: "rotating fixture"},
		{name: "note empty", note: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(HeaderCtxtNote)
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(ts.Close)

			c := NewAPIClient(ts.URL)
			require.NoError(t, c.DeletePipeline("p-1", tc.note))
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestAPIClient_PolicyDenied_ReturnsSentinel confirms a 409 +
// POLICY_DENIED response wraps ErrPolicyDenied so the CLI runner can
// map to exit code 4 without parsing the error message.
func TestAPIClient_PolicyDenied_ReturnsSentinel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"POLICY_DENIED","message":"policy \"delete-pipeline-requires-note\" denied: deleting a pipeline requires --note explaining why","details":{}}}`))
	}))
	t.Cleanup(ts.Close)

	c := NewAPIClient(ts.URL)
	err := c.DeletePipeline("p-1", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPolicyDenied), "expected ErrPolicyDenied wrap, got %v", err)
	assert.True(t, strings.Contains(err.Error(), "delete-pipeline-requires-note"))
	assert.Equal(t, 4, ExitCodeFor(err))
}

// TestAPIClient_OtherErrors_NotPolicyDenied confirms generic errors
// don't map to ErrPolicyDenied. A non-409 stays out of the policy
// path.
func TestAPIClient_OtherErrors_NotPolicyDenied(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"db dropped","details":{}}}`))
	}))
	t.Cleanup(ts.Close)

	c := NewAPIClient(ts.URL)
	err := c.ArchivePipeline("p-1", "any note")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrPolicyDenied))
	assert.Equal(t, 1, ExitCodeFor(err))
}
