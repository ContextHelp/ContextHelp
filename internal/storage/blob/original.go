package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Metadata keys of an original's artifact node.
const (
	MetaBlobKey     = storage.ArtifactMetaBlobKey
	MetaContentType = storage.ArtifactMetaContentType
	MetaSizeBytes   = storage.ArtifactMetaSizeBytes
	MetaSHA256      = storage.ArtifactMetaSHA256
	MetaFileName    = storage.ArtifactMetaFileName

	// MetaOriginalBlobKey is the object-level metadata key naming the
	// original's blob. It is the original's own slot: text externalized
	// later writes metadata.blob_key, which must not hide the original
	// from Keys and CountBlobRefs.
	MetaOriginalBlobKey = "original_blob_key"
)

// Original describes a stored original: the bytes a capture arrived as,
// kept before any step runs. Key is the blob key, the SHA-256 of the
// exact bytes, so identical originals share one blob.
type Original struct {
	Key         string
	SHA256      string
	ContentType string
	FileName    string
	Size        int64
}

// hashChunk is how much of a string OriginalKey copies to hash at once.
const hashChunk = 64 << 10

// OriginalKey is the blob key of content: the hex SHA-256 of its exact
// bytes.
func OriginalKey(content string) string {
	// io.WriteString and []byte(content) both copy the whole string:
	// hash in chunks so the copy stays small.
	h := sha256.New()
	buf := make([]byte, min(hashChunk, len(content)))
	for i := 0; i < len(content); i += hashChunk {
		n := copy(buf, content[i:])
		_, _ = h.Write(buf[:n]) // hash.Hash.Write never fails
	}
	return hex.EncodeToString(h.Sum(nil))
}

// PutOriginal stores content in store under OriginalKey(content). There
// is no size cap. Putting the same bytes again rewrites the same blob.
func PutOriginal(ctx context.Context, store storage.BlobStore, content, contentType, fileName string) (Original, error) {
	if store == nil {
		return Original{}, ErrNoStore
	}
	key := OriginalKey(content)
	o := Original{
		Key: key, SHA256: key, ContentType: contentType,
		FileName: fileName, Size: int64(len(content)),
	}
	err := store.Put(ctx, key, strings.NewReader(content), storage.BlobMeta{
		ContentType: contentType,
		Size:        o.Size,
		ContentHash: key,
	})
	if err != nil {
		return Original{}, fmt.Errorf("put original: %w", err)
	}
	return o, nil
}

// AttachOriginal records o on obj: the artifact node (ordinal 0) with
// the original's metadata, and the object-level original_blob_key that
// delete, update and sweep count references through. content is kept in
// obj.BlobContent (when not empty) so ingest can put the blob back if a
// concurrent delete removes it before the row lands (Ensure).
//
// artifact/0 is the original's alone. An existing node there that is not an
// original (no blob key and not labelled "original") is not overwritten:
// AttachOriginal returns an error and changes nothing.
func AttachOriginal(obj *storage.KnowledgeObject, o Original, content string) error {
	if obj.Graph == nil {
		obj.Graph = &storage.ObjectGraph{}
	}
	id := storage.NewNodeID(obj.ID, storage.NodeTypeArtifact, 0)
	if n := obj.Graph.FindNode(id); n != nil {
		if k, _ := n.Metadata[MetaBlobKey].(string); k == "" && n.Label != "original" {
			return fmt.Errorf("attach original: node %s is not an original", id)
		}
	}
	meta := map[string]any{
		MetaBlobKey:     o.Key,
		MetaContentType: o.ContentType,
		MetaSizeBytes:   o.Size,
		MetaSHA256:      o.SHA256,
		MetaFileName:    o.FileName,
	}
	if n := obj.Graph.FindNode(id); n != nil {
		n.Metadata = meta
		n.Label = "original"
	} else {
		obj.Graph.Nodes = append(obj.Graph.Nodes, storage.GraphNode{
			ID: id, NodeType: storage.NodeTypeArtifact, Label: "original", Metadata: meta,
		})
	}

	if obj.Metadata == nil {
		obj.Metadata = make(map[string]any)
	}
	obj.Metadata[MetaOriginalBlobKey] = o.Key
	if content == "" {
		return nil // bytes not at hand: record the original, put nothing back
	}
	if obj.BlobContent == nil {
		obj.BlobContent = make(map[string]string)
	}
	obj.BlobContent[o.Key] = content
	return nil
}

// OriginalOf returns the original obj's artifact node records, and
// whether it has one. Read the bytes with store.Get(ctx, o.Key).
func OriginalOf(obj *storage.KnowledgeObject) (Original, bool) {
	if obj.ID == "" {
		return Original{}, false
	}
	node := obj.Graph.FindNode(storage.NewNodeID(obj.ID, storage.NodeTypeArtifact, 0))
	if node == nil {
		return Original{}, false
	}
	key, _ := node.Metadata[MetaBlobKey].(string)
	if key == "" {
		return Original{}, false
	}
	o := Original{Key: key}
	o.SHA256, _ = node.Metadata[MetaSHA256].(string)
	o.ContentType, _ = node.Metadata[MetaContentType].(string)
	o.FileName, _ = node.Metadata[MetaFileName].(string)
	switch n := node.Metadata[MetaSizeBytes].(type) {
	case int64:
		o.Size = n
	case int:
		o.Size = int64(n)
	case float64:
		o.Size = int64(n)
	}
	return o, true
}

// WithoutOriginal returns o with the references to its original removed:
// metadata.original_blob_key and the blob_key and sha256 of its artifact
// node. The node stays, with its content type, size and file name, as
// provenance.
// Federation uses it where the wire has no blob channel, so originals
// stay on the source instance. o's own maps are not modified.
func WithoutOriginal(o storage.KnowledgeObject) storage.KnowledgeObject {
	if _, ok := o.Metadata[MetaOriginalBlobKey]; ok {
		o.Metadata = maps.Clone(o.Metadata)
		delete(o.Metadata, MetaOriginalBlobKey)
	}
	if o.Graph != nil && o.ID != "" {
		id := storage.NewNodeID(o.ID, storage.NodeTypeArtifact, 0)
		if n := o.Graph.FindNode(id); n != nil {
			if _, has := n.Metadata[MetaBlobKey]; has {
				g := *o.Graph
				g.Nodes = slices.Clone(o.Graph.Nodes)
				n = g.FindNode(id)
				n.Metadata = maps.Clone(n.Metadata)
				delete(n.Metadata, MetaBlobKey)
				delete(n.Metadata, MetaSHA256) // the same value: the blob's address
				o.Graph = &g
			}
		}
	}
	return o
}
