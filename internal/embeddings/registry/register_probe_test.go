package registry_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Register refuses a model_id that cannot be embedded as a literal in
// per-model index DDL, before touching the table.
func TestRegister_RejectsInvalidModelID(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()
	for _, id := range []string{"has space@1", "quote'@1", "-dash@1", strings.Repeat("a", 201)} {
		err := r.Register(ctx, registry.Model{ModelID: id, Provider: registry.ProviderOllama, Dimension: 8}, false)
		if err == nil || !strings.Contains(err.Error(), "invalid embedding model_id") {
			t.Errorf("Register(%q): err = %v, want the model_id validation error", id, err)
		}
		if _, err := r.Get(ctx, id); !errors.Is(err, registry.ErrModelNotFound) {
			t.Errorf("Register(%q) left a row behind (Get err = %v)", id, err)
		}
	}
}

// SQLite's ceiling is sqlite-vec's vec0 column limit.
func TestRegister_SQLiteDimensionCeiling(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()

	err := r.Register(ctx, registry.Model{
		ModelID: "too-wide@1", Provider: registry.ProviderOllama, Dimension: storage.SQLiteVecMaxDimension + 1,
	}, false)
	if err == nil {
		t.Fatalf("Register with dimension %d succeeded on sqlite", storage.SQLiteVecMaxDimension+1)
	}
	for _, want := range []string{"too-wide@1", "8192"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	if err := r.Register(ctx, registry.Model{
		ModelID: "widest@1", Provider: registry.ProviderOllama, Dimension: storage.SQLiteVecMaxDimension,
	}, false); err != nil {
		t.Fatalf("Register at the ceiling (%d): %v", storage.SQLiteVecMaxDimension, err)
	}
}
