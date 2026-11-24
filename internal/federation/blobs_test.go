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
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const pushBlobKey = "feedbeef01234567890000000000000000000000000000000000000000000000"

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

// blobLostOnCreate is a target object store that deletes the blobs an
// object refers to as its row lands: a delete or sweep at the target
// that counted no references to a blob the target already held, and
// deletes it, before the pushed row is written.
type blobLostOnCreate struct {
	storage.ObjectStore
	blobs storage.BlobStore
}

func (o blobLostOnCreate) Create(ctx context.Context, obj *storage.KnowledgeObject) error {
	if err := o.ObjectStore.Create(ctx, obj); err != nil {
		return err
	}
	for _, key := range blob.Keys(obj) {
		if err := o.blobs.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// A blob the target already holds is not copied, so its age at the
// target is whatever it was: an unreferenced one may be deleted there
// as the pushed row lands. The push copies it again once the row is
// written.
func TestLocalPusher_RecopiesTargetBlobDeletedAsRowLands(t *testing.T) {
	ctx := context.Background()
	src := storageutil.NewTestBlobStore(t)
	body := strings.Repeat("federated blob ", 30)
	obj := blobBackedObject(t, src, "fb-race", body)
	target := storageutil.NewTestDriver(t)
	dst := target.Blobs()
	if err := dst.Put(ctx, pushBlobKey, strings.NewReader(body), storage.BlobMeta{}); err != nil {
		t.Fatal(err)
	}

	objs := blobLostOnCreate{ObjectStore: target.Objects(), blobs: dst}
	if err := writeObject(ctx, objs, src, dst, &obj); err != nil {
		t.Fatalf("writeObject: %v", err)
	}
	content, err := blob.Content(ctx, dst, obj.RawContent)
	if err != nil {
		t.Fatalf("the pushed row names a blob the target deleted: %v", err)
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

// ocrObject is an object the way the worker leaves a captured image:
// OCR text inline, the bytes only in the source blob store, an artifact
// node and original_blob_key naming them.
func ocrObject(t *testing.T, src storage.BlobStore, id string) (storage.KnowledgeObject, blob.Original) {
	t.Helper()
	bin := string([]byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe})
	o, err := blob.PutOriginal(context.Background(), src, bin, "image/png", "cat.png")
	if err != nil {
		t.Fatal(err)
	}
	obj := makeObject(id, "hash-"+id)
	obj.RawContent = "recognized text"
	blob.AttachOriginal(&obj, o, bin)
	return obj, o
}

// A local push copies the original too, not only the content refs.
func TestLocalPusher_CopiesOriginalToTargetStore(t *testing.T) {
	ctx := context.Background()
	src := storageutil.NewTestBlobStore(t)
	target := filepath.Join(t.TempDir(), config.DBFileName)
	obj, o := ocrObject(t, src, "fo-1")

	if err := NewLocalPusher("orig-fed", target, nil, src).Push(ctx, []storage.KnowledgeObject{obj}, nil, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := src.Delete(ctx, o.Key); err != nil {
		t.Fatal(err)
	}
	tbs := targetBlobStore(t, target)
	if ok, err := tbs.Exists(ctx, o.Key); err != nil || !ok {
		t.Fatalf("original not in the target store: exists=%v err=%v", ok, err)
	}
	drv, err := storageutil.NewDriver("sqlite", target, tbs)
	if err != nil {
		t.Fatal(err)
	}
	defer drv.Close(ctx)
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := drv.Objects().Get(ctx, "fo-1")
	if err != nil {
		t.Fatal(err)
	}
	if back, ok := blob.OriginalOf(got); !ok || back.Key != o.Key {
		t.Fatalf("target object lost its original: %+v %v", back, ok)
	}
}

// An original the source no longer holds does not drop the object: it
// lands without the original, with a warning. A lost content blob still
// fails the push (TestLocalPusher_MissingSourceBlobFailsPush).
func TestLocalPusher_MissingOriginalLandsWithoutIt(t *testing.T) {
	ctx := context.Background()
	src := storageutil.NewTestBlobStore(t)
	target := filepath.Join(t.TempDir(), config.DBFileName)
	obj, o := ocrObject(t, src, "fo-lost")
	if err := src.Delete(ctx, o.Key); err != nil {
		t.Fatal(err)
	}
	if err := NewLocalPusher("orig-lost", target, nil, src).Push(ctx, []storage.KnowledgeObject{obj}, nil, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	drv, err := storageutil.NewDriver("sqlite", target, targetBlobStore(t, target))
	if err != nil {
		t.Fatal(err)
	}
	defer drv.Close(ctx)
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := drv.Objects().Get(ctx, "fo-lost")
	if err != nil {
		t.Fatalf("object not pushed: %v", err)
	}
	if got.RawContent != "recognized text" {
		t.Errorf("content = %q", got.RawContent)
	}
	if _, ok := blob.OriginalOf(got); ok {
		t.Error("target object still names the lost original")
	}
	if _, has := got.Metadata[blob.MetaOriginalBlobKey]; has {
		t.Error("original_blob_key left on the target object")
	}
}

// The wire format has no blob channel: originals stay on the source.
// A remote push strips the original's key from the object and the
// artifact node, and leaves the caller's object alone.
func TestRemotePusher_OriginalsStayOnSource(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	src := storageutil.NewTestBlobStore(t)
	obj, o := ocrObject(t, src, "ro-1")
	srcDB, _ := newTestDB(t)
	p := NewRemotePusherWithWatermarks("ro-fed", srv.URL, "", srcDB.Watermarks(), src)
	if err := p.Push(context.Background(), []storage.KnowledgeObject{obj}, nil, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if body == "" || strings.Contains(body, o.Key) || strings.Contains(body, "original_blob_key") {
		t.Fatalf("push body carries the original's key: %s", body)
	}
	if _, ok := blob.OriginalOf(&obj); !ok || obj.Metadata[blob.MetaOriginalBlobKey] != o.Key {
		t.Error("Push stripped the caller's object")
	}
}

// RawContent that is the original's own ref inlines as the bytes, which
// JSON would mangle: the push sends no content and no key for it.
func TestRemotePusher_BinaryContentIsNotInlined(t *testing.T) {
	var got FederationPushRequest
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		_ = json.Unmarshal(data, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	src := storageutil.NewTestBlobStore(t)
	obj, o := ocrObject(t, src, "rb-bin")
	obj.RawContent = blobref.For(o.Key)
	obj.Metadata["blob_key"] = o.Key
	srcDB, _ := newTestDB(t)
	p := NewRemotePusherWithWatermarks("rb-bin-fed", srv.URL, "", srcDB.Watermarks(), src)
	if err := p.Push(context.Background(), []storage.KnowledgeObject{obj}, nil, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(got.Objects) != 1 || got.Objects[0].RawContent != "" {
		t.Fatalf("RawContent = %q, want empty", got.Objects[0].RawContent)
	}
	if strings.Contains(body, "\\ufffd") || strings.Contains(body, o.Key) || strings.Contains(body, "blob://") {
		t.Fatalf("push body carries the binary or its key: %s", body)
	}
	if len(got.Objects[0].Graph.Nodes) != 1 {
		t.Error("provenance node dropped")
	}
}
