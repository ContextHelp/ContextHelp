package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seedBlobRefObject stores an object whose content lives in the blob
// store, as externalize_content leaves it.
func seedBlobRefObject(t *testing.T, ts *testServerBundle, id, content string) {
	t.Helper()
	ctx := context.Background()
	key := "k" + id
	if err := ts.svc.Store.Blobs().Put(ctx, key, strings.NewReader(content), storage.BlobMeta{Size: int64(len(content))}); err != nil {
		t.Fatalf("put blob: %v", err)
	}
	now := time.Now().Truncate(time.Second)
	if err := ts.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: id, Type: "text", RawContent: "blob://" + key, CreatedAt: now, UpdatedAt: now,
		Metadata: map[string]any{"blob_key": key},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
}

// Clients read content, never the blob:// ref it is stored as.
func TestObjectReadsResolveBlobRefs(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	body := strings.Repeat("externalized body ", 40)
	seedBlobRefObject(t, ts, "obj-blob", body)

	resp, err := http.Get(ts.URL + "/api/v1/objects/obj-blob")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status: got %d, want 200", resp.StatusCode)
	}
	var obj storage.KnowledgeObject
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if obj.RawContent != body || obj.TextContent != body {
		t.Errorf("get: raw_content %.40q text_content %.40q, want the blob's content", obj.RawContent, obj.TextContent)
	}

	lresp, err := http.Get(ts.URL + "/api/v1/objects")
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer lresp.Body.Close()
	var list struct {
		Data []storage.KnowledgeObject `json:"data"`
	}
	if err := json.NewDecoder(lresp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Data) != 1 || strings.HasPrefix(list.Data[0].RawContent, "blob://") || list.Data[0].RawContent == "" {
		t.Errorf("list: want one object with content, got %+v", list.Data)
	}
}

// A blob that cannot be read is a server error, not a missing object.
func TestGetObjectLostBlobIs500(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	seedBlobRefObject(t, ts, "obj-lost", "gone soon")
	if err := ts.svc.Store.Blobs().Delete(context.Background(), "kobj-lost"); err != nil {
		t.Fatalf("delete blob: %v", err)
	}

	resp, err := http.Get(ts.URL + "/api/v1/objects/obj-lost")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", resp.StatusCode)
	}
}
