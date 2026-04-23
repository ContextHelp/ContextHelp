//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFacetSearch_MetadataType(t *testing.T) {
	env := startTestEnv(t)
	ctx := context.Background()

	d, ok := env.svc.Store.(*sqlite.Driver)
	require.True(t, ok)

	for _, tc := range []struct {
		id, metaType string
	}{
		{"fs-task-1", "task"},
		{"fs-task-2", "task"},
		{"fs-obs-1", "observation"},
	} {
		obj := storageutil.BuildGraphKO(tc.id, "text", "facet test "+tc.id)
		obj.Metadata = map[string]any{"type": tc.metaType}
		obj.CreatedAt = nowTrunc()
		obj.UpdatedAt = nowTrunc()
		require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	}

	_, _ = d.DB().ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")

	// List with MetadataType filter.
	objs, total, err := env.svc.ListObjects(ctx, storage.ObjectFilter{
		MetadataType: "task",
		Status:       "all",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, objs, 2)
	for _, o := range objs {
		assert.Equal(t, "task", o.Metadata["type"])
	}
}

func TestFacetSearch_Person(t *testing.T) {
	env := startTestEnv(t)
	ctx := context.Background()

	obj := storageutil.BuildGraphKO("fs-person-1", "text", "person facet")
	obj.Metadata = map[string]any{
		"people": []any{"alice-chen", "bob-smith"},
	}
	obj.CreatedAt = nowTrunc()
	obj.UpdatedAt = nowTrunc()
	require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))

	obj2 := storageutil.BuildGraphKO("fs-person-2", "text", "no person")
	obj2.Metadata = map[string]any{}
	obj2.CreatedAt = nowTrunc()
	obj2.UpdatedAt = nowTrunc()
	require.NoError(t, env.svc.Store.Objects().Create(ctx, obj2))

	objs, _, err := env.svc.ListObjects(ctx, storage.ObjectFilter{
		MetadataPerson: "alice-chen",
		Status:         "all",
	})
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, "fs-person-1", objs[0].ID)
}

func TestFacetSearch_SinceUntil(t *testing.T) {
	env := startTestEnv(t)
	ctx := context.Background()

	obj := storageutil.BuildGraphKO("fs-date-1", "text", "date facet")
	obj.Metadata = map[string]any{
		"dates_mentioned": []any{"2026-03-15", "2026-04-10"},
	}
	obj.CreatedAt = nowTrunc()
	obj.UpdatedAt = nowTrunc()
	require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))

	// Since 2026-04-01: object has 2026-04-10 >= 2026-04-01.
	since := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	objs, _, err := env.svc.ListObjects(ctx, storage.ObjectFilter{
		MetadataSince: &since,
		Status:        "all",
	})
	require.NoError(t, err)
	assert.Len(t, objs, 1)

	// Since 2026-05-01: no dates >= 2026-05-01.
	tooLate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	objs, _, err = env.svc.ListObjects(ctx, storage.ObjectFilter{
		MetadataSince: &tooLate,
		Status:        "all",
	})
	require.NoError(t, err)
	assert.Len(t, objs, 0)

	// Until 2026-03-20: object has 2026-03-15 <= 2026-03-20.
	until := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	objs, _, err = env.svc.ListObjects(ctx, storage.ObjectFilter{
		MetadataUntil: &until,
		Status:        "all",
	})
	require.NoError(t, err)
	assert.Len(t, objs, 1)
}

func TestFacetSearch_CombinedFilters(t *testing.T) {
	env := startTestEnv(t)
	ctx := context.Background()

	for _, tc := range []struct {
		id       string
		metaType string
		topic    string
		source   string
	}{
		{"fs-combo-1", "task", "auth", "slack"},
		{"fs-combo-2", "task", "billing", "email"},
		{"fs-combo-3", "observation", "auth", "slack"},
	} {
		obj := storageutil.BuildGraphKO(tc.id, "text", "combined "+tc.id)
		obj.Metadata = map[string]any{
			"type":        tc.metaType,
			"topics":      []any{tc.topic},
			"source_type": tc.source,
		}
		obj.CreatedAt = nowTrunc()
		obj.UpdatedAt = nowTrunc()
		require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	}

	// type=task AND topic=auth AND source_type=slack → only fs-combo-1.
	objs, _, err := env.svc.ListObjects(ctx, storage.ObjectFilter{
		MetadataType:  "task",
		MetadataTopic: "auth",
		SourceType:    "slack",
		Status:        "all",
	})
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, "fs-combo-1", objs[0].ID)
}

func TestFacetSearch_FacetCounts(t *testing.T) {
	env := startTestEnv(t)
	ctx := context.Background()

	for _, tc := range []struct {
		id       string
		metaType string
	}{
		{"fs-fc-1", "task"},
		{"fs-fc-2", "task"},
		{"fs-fc-3", "observation"},
		{"fs-fc-4", ""},
	} {
		obj := storageutil.BuildGraphKO(tc.id, "text", "facet count "+tc.id)
		obj.Metadata = map[string]any{}
		if tc.metaType != "" {
			obj.Metadata["type"] = tc.metaType
		}
		obj.CreatedAt = nowTrunc()
		obj.UpdatedAt = nowTrunc()
		require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	}

	counts, err := env.svc.FacetCounts(ctx, storage.ObjectFilter{Status: "all"})
	require.NoError(t, err)
	assert.Equal(t, 2, counts["task"])
	assert.Equal(t, 1, counts["observation"])
	assert.Equal(t, 1, counts["(none)"])
}

func TestFacetSearch_HybridWithFilter(t *testing.T) {
	env := startTestEnv(t)
	ctx := context.Background()

	d, ok := env.svc.Store.(*sqlite.Driver)
	require.True(t, ok)

	for _, tc := range []struct {
		id       string
		summary  string
		metaType string
	}{
		{"fs-hy-1", "kubernetes scheduling policy", "task"},
		{"fs-hy-2", "kubernetes pod autoscaling", "observation"},
	} {
		obj := storageutil.BuildGraphKO(tc.id, "text", tc.summary)
		obj.Metadata = map[string]any{"type": tc.metaType}
		obj.CreatedAt = nowTrunc()
		obj.UpdatedAt = nowTrunc()
		require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	}

	_, _ = d.DB().ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 1.0, VectorWeight: 0.0},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 0},
		FallbackToFTS: true,
	}

	// Hybrid search for "kubernetes" with MetadataType=task → only fs-hy-1.
	results, err := env.svc.HybridSearchFiltered(ctx, "kubernetes",
		storage.ObjectFilter{Limit: 10, MetadataType: "task"}, nil, cfg)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "fs-hy-1", results[0].ID)
}
