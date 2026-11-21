package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/contentenc"
)

// enqueueRecorder is an httptest dpkms that accepts POST
// /api/v1/pipelines/enqueue and keeps the decoded request bodies.
func enqueueRecorder(t *testing.T) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/pipelines/enqueue" {
			http.NotFound(w, r)
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":"j-1"}`))
	}))
	t.Cleanup(ts.Close)
	return ts, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
}

// A binary file reaches the server base64-encoded, with the encoding
// named; the server-side decode restores the exact bytes.
func TestPipelineEnqueue_BinaryFileEncoded(t *testing.T) {
	bin := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe, 0x80, '\n'}
	srv, bodies := enqueueRecorder(t)
	db := setupTestDB(t)
	path := filepath.Join(t.TempDir(), "img.png")
	require.NoError(t, os.WriteFile(path, bin, 0o600))

	_, err := db.run("--server-url", srv.URL, "pipeline", "enqueue", path)
	require.NoError(t, err)

	got := bodies()
	require.Len(t, got, 1)
	require.Equal(t, "base64", got[0]["content_encoding"])
	decoded, err := contentenc.Decode(got[0]["content"].(string), "base64")
	require.NoError(t, err)
	require.Equal(t, bin, []byte(decoded))
}

func TestPipelineEnqueue_TextCarriesNoEncoding(t *testing.T) {
	srv, bodies := enqueueRecorder(t)
	db := setupTestDB(t)
	path := filepath.Join(t.TempDir(), "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("héllo"), 0o600))

	_, err := db.run("--server-url", srv.URL, "pipeline", "enqueue", path)
	require.NoError(t, err)

	got := bodies()
	require.Len(t, got, 1)
	require.Equal(t, "héllo", got[0]["content"])
	_, present := got[0]["content_encoding"]
	require.False(t, present)
}
