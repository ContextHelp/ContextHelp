package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createRecorder is an httptest dpkms that accepts POST /api/v1/pipelines
// and keeps the decoded request bodies.
func createRecorder(t *testing.T) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/pipelines" {
			http.NotFound(w, r)
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"p-1"}`))
	}))
	t.Cleanup(ts.Close)
	return ts, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
}

// A pipeline config without a description, in every supported format,
// creates the pipeline with an empty one. Before, the unchecked string
// assertion on the absent key panicked the CLI.
func TestPipelineCreate_OptionalDescription(t *testing.T) {
	files := map[string]string{
		"p.json": `{"name":"mine","steps":[{"name":"tagger"}]}`,
		"p.yaml": "name: mine\nsteps:\n  - name: tagger\n",
		"p.toml": "name = \"mine\"\n[[steps]]\nname = \"tagger\"\n",
	}
	for file, content := range files {
		t.Run(file, func(t *testing.T) {
			srv, bodies := createRecorder(t)
			db := setupTestDB(t)
			path := filepath.Join(t.TempDir(), file)
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

			out, err := db.run("--server-url", srv.URL, "pipeline", "create", path)
			require.NoError(t, err, out)
			got := bodies()
			require.Len(t, got, 1)
			assert.Equal(t, "mine", got[0]["name"])
		})
	}
}

// A config without a name is refused before anything is sent.
func TestPipelineCreate_MissingNameErrors(t *testing.T) {
	srv, bodies := createRecorder(t)
	db := setupTestDB(t)
	path := filepath.Join(t.TempDir(), "p.yaml")
	require.NoError(t, os.WriteFile(path, []byte("description: no name\n"), 0o600))

	out, err := db.run("--server-url", srv.URL, "pipeline", "create", path)
	require.Error(t, err, out)
	assert.Contains(t, err.Error(), "name must be a string")
	assert.Empty(t, bodies())
}
