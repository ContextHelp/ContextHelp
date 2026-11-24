package blob_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func newStore(t *testing.T) storage.BlobStore {
	t.Helper()
	s, err := local.New(t.TempDir())
	if err != nil {
		t.Fatalf("local store: %v", err)
	}
	return s
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80}

func TestPutOriginalKeyIsSHA256OfExactBytes(t *testing.T) {
	store := newStore(t)
	got, err := blob.PutOriginal(context.Background(), store, string(pngBytes), "image/png", "a.png")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	want := sum(pngBytes)
	if got.Key != want || got.SHA256 != want {
		t.Fatalf("key/sha = %s/%s, want %s", got.Key, got.SHA256, want)
	}
	if got.Size != int64(len(pngBytes)) || got.ContentType != "image/png" || got.FileName != "a.png" {
		t.Fatalf("original = %+v", got)
	}
	rc, _, err := store.Get(context.Background(), want)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if !bytes.Equal(data, pngBytes) {
		t.Fatalf("stored bytes differ: %v", data)
	}
}

// Same bytes from two sources share one blob; different bytes do not.
func TestPutOriginalDedupsByContent(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	a, err := blob.PutOriginal(ctx, store, string(pngBytes), "image/png", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	b, err := blob.PutOriginal(ctx, store, string(pngBytes), "image/png", "copy.png")
	if err != nil {
		t.Fatal(err)
	}
	if a.Key != b.Key {
		t.Fatalf("same bytes, keys %s vs %s", a.Key, b.Key)
	}
	items, err := store.List(ctx, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("list = %v, %v; want one blob", items, err)
	}
	c, err := blob.PutOriginal(ctx, store, string(pngBytes)+"x", "image/png", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Key == a.Key {
		t.Fatal("different bytes, same key")
	}
}

// Owner decision: no size cap.
func TestPutOriginalHasNoSizeCap(t *testing.T) {
	store := newStore(t)
	big := bytes.Repeat([]byte{0x00, 0xff, 0x7f}, 3<<20) // ~9 MiB
	got, err := blob.PutOriginal(context.Background(), store, string(big), "video/mp4", "v.mp4")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got.Size != int64(len(big)) {
		t.Fatalf("size = %d, want %d", got.Size, len(big))
	}
}

func TestPutOriginalNoStore(t *testing.T) {
	if _, err := blob.PutOriginal(context.Background(), nil, "x", "", ""); err == nil {
		t.Fatal("want error for nil store")
	}
}

func TestAttachOriginalRecordsArtifactNode(t *testing.T) {
	draft := &storage.KnowledgeObject{ID: "obj-1"}
	o := blob.Original{
		Key: sum(pngBytes), SHA256: sum(pngBytes), ContentType: "image/png",
		FileName: "a.png", Size: int64(len(pngBytes)),
	}
	blob.AttachOriginal(draft, o, string(pngBytes))

	node := draft.Graph.FindNode(storage.NewNodeID("obj-1", storage.NodeTypeArtifact, 0))
	if node == nil {
		t.Fatal("no artifact node 0")
	}
	for k, want := range map[string]any{
		"blob_key": o.Key, "content_type": "image/png", "size_bytes": o.Size,
		"sha256": o.SHA256, "file_name": "a.png",
	} {
		if node.Metadata[k] != want {
			t.Errorf("node metadata %s = %v, want %v", k, node.Metadata[k], want)
		}
	}
	// delete, update and sweep find the blob through metadata.blob_key
	keys := blob.Keys(draft)
	if len(keys) != 1 || keys[0] != o.Key {
		t.Fatalf("Keys = %v, want [%s]", keys, o.Key)
	}
	// Ensure can put it back after a racing delete
	if draft.BlobContent[o.Key] != string(pngBytes) {
		t.Fatal("BlobContent not kept for Ensure")
	}
}

// The original has a slot of its own: externalizing text later writes
// metadata.blob_key, which must not hide the original from Keys.
func TestKeysSeesOriginalBesideTextBlobKey(t *testing.T) {
	draft := &storage.KnowledgeObject{ID: "obj-k"}
	o := blob.Original{Key: sum(pngBytes), SHA256: sum(pngBytes), Size: 12}
	blob.AttachOriginal(draft, o, string(pngBytes))
	draft.Metadata["blob_key"] = sum([]byte("text blob"))
	keys := blob.Keys(draft)
	if len(keys) != 2 {
		t.Fatalf("Keys = %v, want the original and the text blob", keys)
	}
	if draft.Metadata["original_blob_key"] != o.Key {
		t.Fatalf("original_blob_key = %v", draft.Metadata["original_blob_key"])
	}
}

// Ensure puts an original back with its own content type.
func TestEnsurePutsBackOriginalWithItsContentType(t *testing.T) {
	store := newStore(t)
	draft := &storage.KnowledgeObject{ID: "obj-e"}
	o := blob.Original{Key: sum(pngBytes), SHA256: sum(pngBytes), ContentType: "image/png", Size: 12}
	blob.AttachOriginal(draft, o, string(pngBytes))
	draft.Metadata["blob_content_type"] = "text/plain"
	if err := blob.Ensure(context.Background(), store, draft); err != nil {
		t.Fatal(err)
	}
	_, meta, err := store.Get(context.Background(), o.Key)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ContentType != "image/png" {
		t.Fatalf("content type = %q", meta.ContentType)
	}
}

func TestOriginalOfRoundTrip(t *testing.T) {
	draft := &storage.KnowledgeObject{ID: "obj-2"}
	if _, ok := blob.OriginalOf(draft); ok {
		t.Fatal("object with no graph has no original")
	}
	o := blob.Original{
		Key: sum(pngBytes), SHA256: sum(pngBytes), ContentType: "image/png",
		FileName: "a.png", Size: 12,
	}
	blob.AttachOriginal(draft, o, string(pngBytes))
	got, ok := blob.OriginalOf(draft)
	if !ok || got != o {
		t.Fatalf("OriginalOf = %+v, %v; want %+v", got, ok, o)
	}
}

// OriginalOf must survive the JSON round trip a stored graph takes,
// where numbers come back as float64.
func TestOriginalOfAfterJSONRoundTrip(t *testing.T) {
	draft := &storage.KnowledgeObject{ID: "obj-3"}
	o := blob.Original{Key: sum(pngBytes), SHA256: sum(pngBytes), ContentType: "image/png", Size: 12}
	blob.AttachOriginal(draft, o, string(pngBytes))
	node := draft.Graph.FindNode(storage.NewNodeID("obj-3", storage.NodeTypeArtifact, 0))
	node.Metadata["size_bytes"] = float64(12)
	got, ok := blob.OriginalOf(draft)
	if !ok || got.Size != 12 {
		t.Fatalf("OriginalOf = %+v, %v", got, ok)
	}
}

// OriginalKey hashes in chunks; sizes around the chunk boundary must
// still give the plain SHA-256.
func TestOriginalKeyAcrossChunkBoundaries(t *testing.T) {
	for _, n := range []int{0, 1, 64<<10 - 1, 64 << 10, 64<<10 + 1, 3*(64<<10) + 7} {
		b := bytes.Repeat([]byte{0x00, 0xff, 'a'}, n/3+1)[:n]
		if got := blob.OriginalKey(string(b)); got != sum(b) {
			t.Errorf("n=%d: key %s, want %s", n, got, sum(b))
		}
	}
}

// artifact/0 belongs to the original: a node that is neither an earlier
// original nor empty is not overwritten.
func TestAttachOriginalRefusesToOverwriteForeignArtifact(t *testing.T) {
	obj := &storage.KnowledgeObject{ID: "o1", Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{{
		ID: "o1/artifact/0", NodeType: storage.NodeTypeArtifact, Label: "dependency left-pad",
		Metadata: map[string]any{"source": "npm"},
	}}}}
	o := blob.Original{Key: blob.OriginalKey("x"), SHA256: blob.OriginalKey("x"), Size: 1}
	if err := blob.AttachOriginal(obj, o, "x"); err == nil {
		t.Fatal("want an error")
	}
	if obj.Graph.Nodes[0].Label != "dependency left-pad" || obj.Graph.Nodes[0].Metadata["source"] != "npm" {
		t.Errorf("foreign node modified: %+v", obj.Graph.Nodes[0])
	}
	if _, has := obj.Metadata[blob.MetaOriginalBlobKey]; has {
		t.Error("object metadata must stay untouched on refusal")
	}
	// Re-attaching over an original, and over no node, still works.
	obj2 := &storage.KnowledgeObject{ID: "o2"}
	for range 2 {
		if err := blob.AttachOriginal(obj2, o, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if len(obj2.Graph.Nodes) != 1 {
		t.Errorf("nodes: %d", len(obj2.Graph.Nodes))
	}
}
