// Package blob builds the blob store storage.blob configures and resolves
// the blob:// refs content above storage.blob.threshold is stored as.
package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrNoStore is returned when content is a blob:// ref and there is no
// blob store to read it from.
var ErrNoStore = errors.New("blob: content is a blob:// ref but no blob store is configured")

// Content returns the content a field stands for: the blob's bytes when
// content is a blob:// ref, content itself otherwise.
func Content(ctx context.Context, store storage.BlobStore, content string) (string, error) {
	got, _, err := read(ctx, store, content, -1)
	return got, err
}

// Preview is Content capped at max bytes, cut back to a rune boundary.
// truncated reports whether the blob held more than was returned; inline
// content is returned whole, whatever its size. max <= 0 means no cap.
func Preview(ctx context.Context, store storage.BlobStore, content string, max int64) (preview string, truncated bool, err error) {
	return read(ctx, store, content, max)
}

func read(ctx context.Context, store storage.BlobStore, content string, max int64) (string, bool, error) {
	key, ok := BlobKey(content)
	if !ok {
		return content, false, nil
	}
	if store == nil {
		return "", false, fmt.Errorf("resolve %s: %w", content, ErrNoStore)
	}
	rc, _, err := store.Get(ctx, key)
	if err != nil {
		return "", false, fmt.Errorf("resolve %s: %w", content, err)
	}
	defer rc.Close()

	var r io.Reader = rc
	if max > 0 {
		// One byte past the cap tells a blob of exactly max bytes from
		// a longer one.
		r = io.LimitReader(rc, max+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", false, fmt.Errorf("resolve %s: read: %w", content, err)
	}
	if max <= 0 || int64(len(data)) <= max {
		return string(data), false, nil
	}
	data = data[:max]
	// Drop a rune the cap split; at most utf8.UTFMax-1 trailing bytes.
	for i := 0; i < utf8.UTFMax-1 && len(data) > 0 && !utf8.Valid(data); i++ {
		data = data[:len(data)-1]
	}
	return string(data), true, nil
}
