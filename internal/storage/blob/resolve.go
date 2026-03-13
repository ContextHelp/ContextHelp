package blob

import (
	"context"
	"io"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const blobPrefix = "blob://"

// IsBlobRef returns true if the content is a blob reference.
func IsBlobRef(content string) bool {
	return strings.HasPrefix(content, blobPrefix) && len(content) > len(blobPrefix)
}

// BlobKey extracts the blob key from a blob:// reference.
func BlobKey(content string) (string, bool) {
	if !IsBlobRef(content) {
		return "", false
	}
	return content[len(blobPrefix):], true
}

// Resolve returns a reader for the object's content.
// If RawContent is a blob:// reference, it fetches from the blob store.
// Otherwise, it wraps the inline content in a reader.
func Resolve(ctx context.Context, store storage.BlobStore, obj *storage.KnowledgeObject) (io.ReadCloser, error) {
	key, ok := BlobKey(obj.RawContent)
	if !ok {
		return io.NopCloser(strings.NewReader(obj.RawContent)), nil
	}
	rc, _, err := store.Get(ctx, key)
	return rc, err
}
