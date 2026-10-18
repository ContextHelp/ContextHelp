package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// An object pushed with a blob:// ref naming a path outside the blob
// store never reads that file back: GET /api/v1/objects/{id} refuses
// the ref instead of resolving it.
func TestPushedBlobRefOutsideStoreNotRead(t *testing.T) {
	base := t.TempDir()
	const marker = "outside-the-blob-root-7f3a"
	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	blobs, err := local.New(filepath.Join(base, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	driver, err := sqlite.New(filepath.Join(base, "db.sqlite"), blobs)
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
	ts := httptest.NewServer(NewRouter(svc, false, nil))
	defer ts.Close()

	// blobs/../secret.txt once sharded (<k[:2]>/<k[2:4]>/<k>).
	obj := storage.KnowledgeObject{
		ID: "obj-escape", Type: "note", RawContent: "blob://../secret.txt",
		ContentHash: "hash-escape", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	presp, err := http.Post(ts.URL+"/api/v1/federation/push", "application/json", makePushBody(t, []storage.KnowledgeObject{obj}, nil))
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	presp.Body.Close()

	get := func(path string) (int, string) {
		t.Helper()
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), marker) {
			t.Errorf("GET %s returned the file outside the blob store: %s", path, body)
		}
		return resp.StatusCode, string(body)
	}
	if code, body := get("/api/v1/objects/obj-escape"); code == http.StatusOK {
		t.Errorf("GET object: status 200 for a ref outside the store: %s", body)
	}
	get("/api/v1/objects") // a list preview resolves the ref too
}
