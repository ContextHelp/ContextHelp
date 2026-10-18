package blob

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/blobtest"
)

// openStore answers every Get with content and counts the calls: a
// store with no key check of its own, so a refusal is resolution's.
type openStore struct {
	storage.BlobStore
	gets int
}

func (s *openStore) Get(context.Context, string) (io.ReadCloser, storage.BlobMeta, error) {
	s.gets++
	return io.NopCloser(strings.NewReader("read through the ref")), storage.BlobMeta{}, nil
}

// A blob:// ref whose key is outside the key grammar is refused when
// content is resolved, with ErrInvalidBlobKey, before the store is asked.
func TestResolutionRefusesInvalidKey(t *testing.T) {
	ctx := context.Background()
	obj := &storage.KnowledgeObject{}
	for _, key := range blobtest.InvalidKeys {
		if key == "" {
			continue // "blob://" alone is not a ref
		}
		ref := "blob://" + key
		t.Run(key, func(t *testing.T) {
			s := &openStore{}
			if got, err := Content(ctx, s, ref); !errors.Is(err, storage.ErrInvalidBlobKey) {
				t.Errorf("Content(%q) = %q, %v; want ErrInvalidBlobKey", ref, got, err)
			}
			if got, _, err := Preview(ctx, s, ref, 4); !errors.Is(err, storage.ErrInvalidBlobKey) {
				t.Errorf("Preview(%q) = %q, %v; want ErrInvalidBlobKey", ref, got, err)
			}
			obj.RawContent = ref
			if rc, err := Resolve(ctx, s, obj); !errors.Is(err, storage.ErrInvalidBlobKey) {
				if rc != nil {
					rc.Close()
				}
				t.Errorf("Resolve(%q): err = %v, want ErrInvalidBlobKey", ref, err)
			}
			if s.gets != 0 {
				t.Errorf("store asked %d time(s) for an invalid key", s.gets)
			}
		})
	}
}
