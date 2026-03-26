package integration

// Shared helpers for search story integration tests (US-0016 – US-0061).

import (
	"encoding/json"
	"io"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// nowTrunc returns the current time truncated to the second, matching
// the SQLite storage precision used for object timestamps.
func nowTrunc() time.Time {
	return time.Now().Truncate(time.Second)
}

// searchResponse is the JSON envelope returned by GET /api/v1/search.
type searchResponse struct {
	Data  []storage.KnowledgeObject `json:"data"`
	Total int                       `json:"total"`
}

// doGet performs an HTTP GET and fails the test on transport error.
func doGet(t *testing.T, url string) *gohttp.Response {
	t.Helper()
	resp, err := gohttp.Get(url) //nolint:noctx
	require.NoError(t, err)
	return resp
}

// decodeJSON decodes JSON from r into dst; fails the test on error.
func decodeJSON(t *testing.T, r io.Reader, dst any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(r).Decode(dst))
}

// containsID returns true when objs contains an object with the given ID.
func containsID(objs []storage.KnowledgeObject, id string) bool {
	for _, o := range objs {
		if o.ID == id {
			return true
		}
	}
	return false
}

// containsIDPtr returns true when objs contains an object with the given ID.
func containsIDPtr(objs []*storage.KnowledgeObject, id string) bool {
	for _, o := range objs {
		if o != nil && o.ID == id {
			return true
		}
	}
	return false
}
