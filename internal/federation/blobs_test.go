package federation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const pushBlobKey = "feedbeef0123456789"

// blobBackedObject is an object stored the way externalize_content
// leaves it: content in the source blob store, refs in the row.
func blobBackedObject(t *testing.T, src storage.BlobStore, id, content string) storage.KnowledgeObject {
	t.Helper()
	if err := src.Put(context.Background(), pushBlobKey, strings.NewReader(content), storage.BlobMeta{Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	o := makeObject(id, "hash-"+id)
	o.RawContent = "blob://" + pushBlobKey
	o.TextContent = "blob://" + pushBlobKey
	o.Metadata = map[string]any{"blob_key": pushBlobKey}
	return o
}

// targetBlobStore is the store a data dir holding target keeps its
// blobs in by default: storage.blob.local.path = <data dir>/blobs.
func targetBlobStore(t *testing.T, target string) storage.BlobStore {
	t.Helper()
	bs, err := blob.New(config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{
		Path: filepath.Join(filepath.Dir(target), config.BlobDirName),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return bs
}

// A local push copies each pushed object's blob into the target's store,
// so the target resolves its refs without the source.
func TestLocalPusher_CopiesBlobsToTargetStore(t *testing.T) {
	ctx := context.Background()
	src := storageutil.NewTestBlobStore(t)
	target := filepath.Join(t.TempDir(), config.DBFileName)
	body := strings.Repeat("federated blob ", 30)
	obj := blobBackedObject(t, src, "fb-1", body)

	if err := NewLocalPusher("blob-fed", target, nil, src).Push(ctx, []storage.KnowledgeObject{obj}, nil, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := src.Delete(ctx, pushBlobKey); err != nil {
		t.Fatal(err)
	}

	tbs := targetBlobStore(t, target)
	drv, err := storageutil.NewDriver("sqlite", target, tbs)
	if err != nil {
		t.Fatal(err)
	}
	defer drv.Close(ctx)
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := drv.Objects().Get(ctx, "fb-1")
	if err != nil {
		t.Fatal(err)
	}
	content, err := blob.Content(ctx, tbs, got.RawContent)
	if err != nil {
		t.Fatalf("target cannot resolve the pushed ref: %v", err)
	}
	if content != body {
		t.Errorf("target content = %.40q, want the source blob", content)
	}
}

// A ref whose blob is gone at the source fails the batch: no row lands
// without its content and the watermark stays put for the retry.
func TestLocalPusher_MissingSourceBlobFailsPush(t *testing.T) {
	ctx := context.Background()
	src := storageutil.NewTestBlobStore(t)
	target := filepath.Join(t.TempDir(), config.DBFileName)
	obj := blobBackedObject(t, src, "fb-lost", "soon gone")
	if err := src.Delete(ctx, pushBlobKey); err != nil {
		t.Fatal(err)
	}

	if err := NewLocalPusher("lost-fed", target, nil, src).Push(ctx, []storage.KnowledgeObject{obj}, nil, nil); err == nil {
		t.Fatal("push of a ref with no blob: want error")
	}
	drv, err := storageutil.NewDriver("sqlite", target, targetBlobStore(t, target))
	if err != nil {
		t.Fatal(err)
	}
	defer drv.Close(ctx)
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := drv.Objects().Get(ctx, "fb-lost"); err == nil {
		t.Error("object landed without its blob")
	}
	if wm, _ := drv.Watermarks().GetWatermark(ctx, "lost-fed"); wm != 0 {
		t.Errorf("watermark advanced on a failed push: %d", wm)
	}
}

// The push wire format has no blob channel: a remote push sends the
// content inline and the receiver re-externalizes it by its own
// threshold.
func TestRemotePusher_SendsResolvedContent(t *testing.T) {
	var got FederationPushRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if strings.Contains(string(data), "blob://") {
			t.Errorf("push body carries a blob ref")
		}
		_ = json.Unmarshal(data, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	src := storageutil.NewTestBlobStore(t)
	body := strings.Repeat("remote blob ", 30)
	obj := blobBackedObject(t, src, "rb-1", body)
	srcDB, _ := newTestDB(t)
	p := NewRemotePusherWithWatermarks("rb-fed", srv.URL, "", srcDB.Watermarks(), src)
	if err := p.Push(context.Background(), []storage.KnowledgeObject{obj}, nil, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(got.Objects) != 1 || got.Objects[0].RawContent != body || got.Objects[0].TextContent != body {
		t.Fatalf("pushed objects = %+v, want one with the blob's content inline", got.Objects)
	}
	if obj.RawContent != "blob://"+pushBlobKey {
		t.Error("Push rewrote the caller's object")
	}
}

// Without a blob store a ref cannot be sent as content: fail, don't send it.
func TestRemotePusher_RefWithoutStoreFails(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	obj := makeObject("rb-2", "hash-rb-2")
	obj.RawContent = "blob://" + pushBlobKey
	if err := NewRemotePusher("rb-fed", srv.URL, "").Push(context.Background(), []storage.KnowledgeObject{obj}, nil, nil); err == nil {
		t.Fatal("push of a ref with no blob store: want error")
	}
	if called {
		t.Error("refs were sent to the receiver")
	}
}

// A file target's blob setting names the store a local push copies
// into: the blob lands there, not beside the target database.
func TestLocalPusher_CopiesBlobsToConfiguredTargetStore(t *testing.T) {
	ctx := context.Background()
	srcDB, _ := newTestDB(t)
	target := filepath.Join(t.TempDir(), config.DBFileName)
	custom := &config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: filepath.Join(t.TempDir(), "replica-blobs")}}
	obj := blobBackedObject(t, srcDB.Blobs(), "fb-custom", strings.Repeat("custom store ", 30))

	p := pusherFor(config.FederationEntry{Name: "custom-fed", URL: "file://" + target, Blob: custom}, srcDB)
	if err := p.Push(ctx, []storage.KnowledgeObject{obj}, nil, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}

	customStore, err := blob.New(*custom)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := customStore.Exists(ctx, pushBlobKey); err != nil || !ok {
		t.Errorf("blob not in the configured target store: exists=%v err=%v", ok, err)
	}
	if ok, _ := targetBlobStore(t, target).Exists(ctx, pushBlobKey); ok {
		t.Error("blob copied beside the target database despite the blob setting")
	}
}
