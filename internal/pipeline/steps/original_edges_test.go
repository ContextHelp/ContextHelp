package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A representation derives from the original: the edge runs from the
// derived node to the artifact.
func assertDerivesFromArtifact(t *testing.T, g *storage.ObjectGraph, objID string) {
	t.Helper()
	artifactID := storage.NewNodeID(objID, storage.NodeTypeArtifact, 0)
	var n int
	for _, e := range g.Edges {
		if e.EdgeType != storage.EdgeTypeDerivedFrom {
			continue
		}
		n++
		if e.ToID != artifactID {
			t.Errorf("derives_from edge %s -> %s: want target %s", e.FromID, e.ToID, artifactID)
		}
		if e.FromID == artifactID {
			t.Errorf("derives_from edge starts at the artifact: %s", e.ID)
		}
	}
	if n == 0 {
		t.Fatal("no derives_from edge")
	}
}

// withOriginal attaches img as the draft's stored original, as the worker
// does before any step runs.
func withOriginal(t *testing.T, d *storage.KnowledgeObject, img string) *storage.KnowledgeObject {
	t.Helper()
	o := blob.Original{Key: blob.OriginalKey(img), SHA256: blob.OriginalKey(img), ContentType: "image/png", Size: int64(len(img))}
	if err := blob.AttachOriginal(d, o, img); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOCRDerivesFromArtifact(t *testing.T) {
	got, err := NewOCRExtractor().Run(context.Background(), withOriginal(t, &storage.KnowledgeObject{
		ID: "o1", RawContent: "img", ContentType: "image/png",
	}, "img"))
	if err != nil {
		t.Fatal(err)
	}
	assertDerivesFromArtifact(t, got.Graph, "o1")
}

func TestVisionDerivesFromArtifact(t *testing.T) {
	got, err := NewVisionAnalyzer().Run(context.Background(), withOriginal(t, &storage.KnowledgeObject{
		ID: "o2", RawContent: "img", ContentType: "image/png",
	}, "img"))
	if err != nil {
		t.Fatal(err)
	}
	assertDerivesFromArtifact(t, got.Graph, "o2")
}

// With the original already attached, steps reuse its artifact node and
// leave its metadata alone.
func TestOCRKeepsAttachedOriginal(t *testing.T) {
	draft := &storage.KnowledgeObject{ID: "o3", RawContent: "img", ContentType: "image/png"}
	o := blob.Original{Key: blob.OriginalKey("img"), SHA256: blob.OriginalKey("img"), ContentType: "image/png", Size: 3}
	if err := blob.AttachOriginal(draft, o, "img"); err != nil {
		t.Fatal(err)
	}
	got, err := NewOCRExtractor().Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	var artifacts int
	for _, n := range got.Graph.Nodes {
		if n.NodeType == storage.NodeTypeArtifact {
			artifacts++
		}
	}
	if artifacts != 1 {
		t.Fatalf("artifact nodes = %d, want 1", artifacts)
	}
	if back, ok := blob.OriginalOf(got); !ok || back != o {
		t.Fatalf("original changed: %+v %v", back, ok)
	}
}

// Binary content is keyed by its bytes alone, so it shares one blob
// with the original stored before the steps ran, whatever the source.
func TestExternalizeBinaryKeyIsSHA256OfBytes(t *testing.T) {
	store, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := string([]byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe})
	got, err := NewExternalizer(store, 1<<20).Run(context.Background(), &storage.KnowledgeObject{
		RawContent: bin, Source: "/tmp/x.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := blob.OriginalKey(bin); got.Metadata["blob_key"] != want {
		t.Fatalf("blob_key = %v, want %s", got.Metadata["blob_key"], want)
	}
	// Text over the threshold keeps the normalised, source-keyed hash.
	txt := strings.Repeat("Hello  World ", 10)
	got, err = NewExternalizer(store, 16).Run(context.Background(), &storage.KnowledgeObject{
		RawContent: txt, Source: "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := storageutil.ContentHash(txt, "s"); got.Metadata["blob_key"] != want {
		t.Fatalf("text blob_key = %v, want %s", got.Metadata["blob_key"], want)
	}
}

// Fan-out items are artifact nodes too; they must not take the ordinal
// the stored original holds.
func TestStageItemsSkipsOriginalOrdinal(t *testing.T) {
	draft := &storage.KnowledgeObject{ID: "o4"}
	blob.AttachOriginal(draft, blob.Original{Key: blob.OriginalKey("x"), SHA256: blob.OriginalKey("x"), Size: 1}, "x")
	stageItems(draft, []fanOutItem{{Title: "a", Content: "one"}, {Title: "b", Content: "two"}})
	seen := map[string]bool{}
	for _, n := range draft.Graph.Nodes {
		if seen[n.ID] {
			t.Fatalf("duplicate node id %s", n.ID)
		}
		seen[n.ID] = true
	}
	if len(draft.Graph.Nodes) != 3 {
		t.Fatalf("nodes = %d, want original + 2 items", len(draft.Graph.Nodes))
	}
	if _, ok := blob.OriginalOf(draft); !ok {
		t.Fatal("original lost")
	}
}

// Externalizing the content that is still the original's bytes must not
// put the blob again under another content type.
func TestExternalizeBinaryKeepsOriginalContentType(t *testing.T) {
	store, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := string([]byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe})
	o, err := blob.PutOriginal(context.Background(), store, bin, "image/png", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	draft := &storage.KnowledgeObject{ID: "o5", RawContent: bin, ContentType: "application/octet-stream"}
	blob.AttachOriginal(draft, o, bin)
	got, err := ExternalizeBinary(context.Background(), store, draft)
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata["blob_content_type"] != "image/png" {
		t.Fatalf("blob_content_type = %v", got.Metadata["blob_content_type"])
	}
	_, meta, err := store.Get(context.Background(), o.Key)
	if err != nil || meta.ContentType != "image/png" {
		t.Fatalf("stored content type = %q, %v", meta.ContentType, err)
	}
}

// Text externalized over the threshold writes blob_key; the original's
// own key survives it.
func TestExternalizeTextKeepsOriginalKey(t *testing.T) {
	store, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	draft := &storage.KnowledgeObject{ID: "o6", RawContent: strings.Repeat("words ", 50), ContentType: "image/png"}
	o := blob.Original{Key: blob.OriginalKey("img"), SHA256: blob.OriginalKey("img"), Size: 3}
	blob.AttachOriginal(draft, o, "img")
	got, err := NewExternalizer(store, 16).Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata[blob.MetaOriginalBlobKey] != o.Key || got.Metadata["blob_key"] == o.Key {
		t.Fatalf("metadata = %v", got.Metadata)
	}
	if got.Metadata["blob_content_type"] != "text/plain; charset=utf-8" {
		t.Fatalf("text blob content type = %v", got.Metadata["blob_content_type"])
	}
	if keys := blob.Keys(got); len(keys) != 2 {
		t.Fatalf("Keys = %v, want the text blob and the original", keys)
	}
}

// Text externalized from a PDF or office file is text, whatever the
// object's content type.
func TestExternalizeTextTypeIsTextPlain(t *testing.T) {
	store, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for ct, want := range map[string]string{
		"application/pdf": "text/plain; charset=utf-8",
		"":                "text/plain; charset=utf-8",
		"text/markdown":   "text/markdown",
	} {
		got, err := NewExternalizer(store, 16).Run(context.Background(), &storage.KnowledgeObject{
			RawContent: strings.Repeat("words ", 50), ContentType: ct,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Metadata["blob_content_type"] != want {
			t.Errorf("content type %q: blob_content_type = %v, want %s", ct, got.Metadata["blob_content_type"], want)
		}
	}
}
