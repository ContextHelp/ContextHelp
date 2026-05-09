package pageshape

import (
	"context"
	"testing"
)

func TestRefresh_OnExtractionFailureInvalidates(t *testing.T) {
	cache := NewMemoryRecipeCache()
	cache.Put(context.Background(), "x.com", "/post/*", []Label{LabelBlogPost})
	NotifyExtractionFailure(context.Background(), cache, "x.com", "/post/*", "empty_result")
	if _, ok := cache.Get(context.Background(), "x.com", "/post/*"); ok {
		t.Fatal("expected recipe invalidated after failure")
	}
}

func TestRefresh_NoOpOnEmptyCache(t *testing.T) {
	cache := NewMemoryRecipeCache()
	// Should not panic when invalidating a key that doesn't exist.
	NotifyExtractionFailure(context.Background(), cache, "x.com", "/missing", "empty_result")
	if _, ok := cache.Get(context.Background(), "x.com", "/missing"); ok {
		t.Fatal("nothing should have been added")
	}
}
