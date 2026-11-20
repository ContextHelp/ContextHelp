package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func seedTaggedObject(t *testing.T, svc *Service, id string, tags []storage.Tag) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, svc.store.Objects().Create(ownerCtx(), &storage.KnowledgeObject{
		ID:         id,
		Type:       "note",
		Status:     storage.ObjectStatusActive,
		RawContent: "body",
		Summaries:  []string{"s"},
		Tags:       tags,
		CreatedAt:  now,
		UpdatedAt:  now,
	}))
}

// A tag patch keeps source and weight of labels it retains, marks new
// labels manual, and drops the rest.
func TestPatchObjectTagsKeepProvenance(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()
	seedTaggedObject(t, svc, "obj-tags", []storage.Tag{
		{Label: "hinted", Weight: 1, Source: "user"},
		{Label: "auto-a", Weight: 0.44, Source: "auto"},
		{Label: "auto-b", Weight: 0.9, Source: "auto"},
	})

	in := []string{" hinted ", "auto-a", "fresh", ""}
	got, err := svc.PatchObject(ctx, "obj-tags", ObjectPatch{Tags: &in})
	require.NoError(t, err)

	want := []storage.Tag{
		{Label: "hinted", Weight: 1, Source: "user"},
		{Label: "auto-a", Weight: 0.44, Source: "auto"},
		{Label: "fresh", Source: "manual"},
	}
	require.Equal(t, want, got.Tags)

	stored, err := svc.store.Objects().Get(ctx, "obj-tags")
	require.NoError(t, err)
	require.Equal(t, want, stored.Tags)
}

// An empty list still clears the tags.
func TestPatchObjectTagsEmptyClears(t *testing.T) {
	svc := newTestService(t)
	seedTaggedObject(t, svc, "obj-clear", []storage.Tag{{Label: "a", Weight: 1, Source: "auto"}})

	in := []string{}
	got, err := svc.PatchObject(ownerCtx(), "obj-clear", ObjectPatch{Tags: &in})
	require.NoError(t, err)
	require.Empty(t, got.Tags)
}
