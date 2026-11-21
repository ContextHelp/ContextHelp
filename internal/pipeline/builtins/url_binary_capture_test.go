package builtins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/pkg/taxonomy"
)

// captureURL runs a url.generic job for the server's /file through the
// real worker and returns the stored object.
func captureURL(t *testing.T, srv *httptest.Server) (storage.StorageDriver, *storage.KnowledgeObject) {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	pipes := ConfiguredRegistryWithOpts(BuildOpts{
		FetchGuard: safefetch.New(safefetch.Options{Policy: safefetch.Policy{AllowPrivate: true}}),
	})
	q := jobs.NewQueue(driver.Jobs())
	now := time.Now().Truncate(time.Second)
	if err := q.Enqueue(context.Background(), &storage.Job{
		ID: "capture", Type: "ingest:url", Status: storage.JobPending, Payload: srv.URL + "/file",
		Pipeline: "url.generic", Source: srv.URL + "/file", MaxRetries: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	drain(t, jobs.NewWorkerPool(q, pipes, driver, 1, nil, config.JobsConfig{
		PollInterval: 20 * time.Millisecond, StaleTimeout: time.Minute, MaxRetries: 1, MaxHops: 5,
	}), driver.Jobs())
	done, err := q.Get(context.Background(), "capture")
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != storage.JobCompleted {
		t.Fatalf("job %s (err=%q)", done.Status, done.Error)
	}
	obj, err := driver.Objects().Get(context.Background(), done.ResultID)
	if err != nil {
		t.Fatal(err)
	}
	return driver, obj
}

var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R', 0xff, 0xfe}

// A captured image keeps the content type the server declared: the
// object and its blob both carry image/png, and its subtype is image.
func TestURLGenericKeepsBinaryContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	driver, obj := captureURL(t, srv)
	if obj.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want image/png", obj.ContentType)
	}
	if got, _ := obj.Metadata["blob_content_type"].(string); got != "image/png" {
		t.Errorf("blob_content_type = %q, want image/png", got)
	}
	if obj.Subtype != "image" {
		t.Errorf("Subtype = %q, want image (not a text subtype)", obj.Subtype)
	}
	body, err := blob.Content(context.Background(), driver.Blobs(), obj.RawContent)
	if err != nil || body != string(pngBytes) {
		t.Errorf("blob content = %q, %v", body, err)
	}
}

// With no usable Content-Type header, the bytes are sniffed for a type;
// the subtype is never a text one.
func TestURLGenericSniffsBinaryWithoutHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil // no header: net/http would sniff otherwise
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	_, obj := captureURL(t, srv)
	if obj.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want sniffed image/png", obj.ContentType)
	}
	if obj.Subtype == taxonomy.SubtypeTextShort || obj.Subtype == taxonomy.SubtypeTextLong {
		t.Errorf("Subtype = %q, binary is not text", obj.Subtype)
	}
}
