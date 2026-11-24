package providertest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A recorded response that embeds the recorder's home path (Ollama's
// /api/show prints the model blob path) must not reach the cassette.
func TestOllamaClientRecordScrubsHomePath(t *testing.T) {
	home := realHomeForTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"modelfile":"FROM ` + home + `/.ollama/models/blobs/sha256-x"}`))
	}))
	defer srv.Close()
	t.Setenv("XRR_MODE", "record")
	t.Setenv("XRR_OLLAMA_UPSTREAM", srv.URL)

	dir := t.TempDir()
	client, _ := OllamaClient(t, dir)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:11434/api/show", strings.NewReader(`{"model":"m"}`))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	files, _ := filepath.Glob(filepath.Join(dir, "http-*.resp.yaml"))
	if len(files) != 1 {
		t.Fatalf("resp files = %v", files)
	}
	b, _ := os.ReadFile(files[0])
	if strings.Contains(string(b), home) || !strings.Contains(string(b), "<home>/.ollama") {
		t.Errorf("cassette keeps the home path:\n%s", b)
	}
}
