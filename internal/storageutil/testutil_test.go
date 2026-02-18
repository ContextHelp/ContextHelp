package storageutil

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestNewTestDriver(t *testing.T) {
	driver := NewTestDriver(t)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "test-obj",
		Type:      "article",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := driver.Objects().Get(ctx, "test-obj")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "test-obj" {
		t.Errorf("ID: got %q", got.ID)
	}
}
