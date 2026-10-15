package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestClassifyObject(t *testing.T) {
	svc := newTestService(t)
	ctx := sharing.WithViewer(context.Background(), sharing.Unrestricted)
	now := time.Now().UTC().Truncate(time.Second)
	obj := &storage.KnowledgeObject{ID: "cls-1", Type: "note", RawContent: "a note", Status: storage.ObjectStatusActive, Classes: []string{"financial"}, CreatedAt: now, UpdatedAt: now}
	if err := svc.Store(ctx).Objects().Create(ctx, obj); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ClassifyObject(ctx, obj.ID, []string{"personal", "financial", "personal"}, nil)
	if err != nil {
		t.Fatalf("ClassifyObject(add): %v", err)
	}
	if !slices.Equal(got.Classes, []string{"financial", "personal"}) {
		t.Fatalf("classes after add = %v", got.Classes)
	}
	stored, err := svc.Store(ctx).Objects().Get(ctx, obj.ID)
	if err != nil || !slices.Equal(stored.Classes, []string{"financial", "personal"}) {
		t.Fatalf("stored classes = %v, %v", stored.Classes, err)
	}

	got, err = svc.ClassifyObject(ctx, obj.ID, []string{"derived"}, []string{"financial", "missing"})
	if err != nil {
		t.Fatalf("ClassifyObject(add and remove): %v", err)
	}
	if !slices.Equal(got.Classes, []string{"derived", "personal"}) {
		t.Fatalf("classes after remove = %v", got.Classes)
	}
	got, err = svc.ClassifyObject(ctx, obj.ID, nil, []string{"derived", "personal"})
	if err != nil || got.Classes != nil {
		t.Fatalf("classes after clearing = %v, %v", got.Classes, err)
	}

	for name, call := range map[string]func() error{
		"nothing asked": func() error { _, err := svc.ClassifyObject(ctx, obj.ID, nil, nil); return err },
		"bad class":     func() error { _, err := svc.ClassifyObject(ctx, obj.ID, []string{"Bad Class"}, nil); return err },
		"bad removal":   func() error { _, err := svc.ClassifyObject(ctx, obj.ID, nil, []string{""}); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalidClass) {
			t.Errorf("%s: err = %v, want ErrInvalidClass", name, err)
		}
	}
	if _, err := svc.ClassifyObject(ctx, "cls-missing", []string{"personal"}, nil); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("missing object: err = %v, want ErrNotFound", err)
	}
	if stored, _ := svc.Store(ctx).Objects().Get(ctx, obj.ID); stored.Classes != nil {
		t.Errorf("a refused call wrote %v", stored.Classes)
	}
}
