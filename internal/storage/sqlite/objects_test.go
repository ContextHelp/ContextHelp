package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	uri "hop.top/cite/scheme"
)

// makeObject builds a graph-canonical KnowledgeObject for tests.
// Flat fields (Tags, Mentions, RawContent) are preserved for backward compat.
// Graph is populated with a summary node, a section node, and a tag node.
func makeObject(id, typ string) *storage.KnowledgeObject {
	now := time.Now().Truncate(time.Second)
	content := "test content for " + id
	return &storage.KnowledgeObject{
		ID:         id,
		Type:       typ,
		Subtype:    "short",
		RawContent: content,
		Tags:       []storage.Tag{{Label: "design", Weight: 1.0}},
		Mentions:   []uri.URI{{Scheme: "ctxt", Namespace: "entity", ID: "ui/layout"}},
		Summaries:  []string{content},
		Sections: []storage.Section{{
			Title:   "Body",
			Content: content,
			Order:   0,
		}},
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSummary, 0),
					NodeType: pluginapi.NodeTypeSummary,
					Label:    "Summary",
					Content:  content,
					Order:    0,
				},
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSection, 0),
					NodeType: pluginapi.NodeTypeSection,
					Label:    "Body",
					Content:  content,
					Order:    0,
				},
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeTag, 0),
					NodeType: pluginapi.NodeTypeTag,
					Label:    "design",
					Content:  "design",
					Order:    0,
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestCreateAndGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("obj-1", "article")
	if err := d.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := d.Objects().Get(ctx, "obj-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.ID != "obj-1" {
		t.Errorf("ID: got %q", got.ID)
	}
	if got.Type != "article" {
		t.Errorf("Type: got %q", got.Type)
	}
	if got.RawContent != obj.RawContent {
		t.Errorf("RawContent: got %q", got.RawContent)
	}
	if len(got.Tags) != 1 || got.Tags[0].Label != "design" {
		t.Errorf("Tags: got %v", got.Tags)
	}
}

func TestList(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		obj := makeObject(
			"obj-"+string(rune('a'+i)),
			"article",
		)
		if err := d.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	objs, total, err := d.Objects().List(ctx, storage.ObjectFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objs) != 2 {
		t.Errorf("count: got %d, want 2", len(objs))
	}
	if total != 3 {
		t.Errorf("total: got %d, want 3", total)
	}
}

func TestListFilterByType(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Objects().Create(ctx, makeObject("obj-1", "article"))
	d.Objects().Create(ctx, makeObject("obj-2", "note"))
	d.Objects().Create(ctx, makeObject("obj-3", "article"))

	objs, total, err := d.Objects().List(ctx, storage.ObjectFilter{Type: "article"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
	if len(objs) != 2 {
		t.Errorf("count: got %d, want 2", len(objs))
	}
}

func TestUpdate(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("obj-1", "article")
	d.Objects().Create(ctx, obj)

	obj.Tags = []storage.Tag{{Label: "updated", Weight: 2.0}}
	obj.UpdatedAt = time.Now().Truncate(time.Second)
	if err := d.Objects().Update(ctx, obj); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := d.Objects().Get(ctx, "obj-1")
	if len(got.Tags) != 1 || got.Tags[0].Label != "updated" {
		t.Errorf("Tags after update: got %v", got.Tags)
	}
}

func TestDelete(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("obj-1", "article")
	d.Objects().Create(ctx, obj)

	if err := d.Objects().Delete(ctx, "obj-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := d.Objects().Get(ctx, "obj-1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	err := d.Objects().Delete(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent delete")
	}
}

func TestListBySQL_SimpleWhere(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, d.Objects().Create(ctx, makeObject("art-1", "article")))
	require.NoError(t, d.Objects().Create(ctx, makeObject("art-2", "article")))
	require.NoError(t, d.Objects().Create(ctx, makeObject("note-1", "note")))

	objs, total, err := d.Objects().ListBySQL(ctx, "type = ?", []any{"article"}, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, objs, 2)
	for _, o := range objs {
		assert.Equal(t, "article", o.Type)
	}
}

func TestListBySQL_Empty(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	objs, total, err := d.Objects().ListBySQL(ctx, "type = ?", []any{"nonexistent"}, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Len(t, objs, 0)
}

func TestListBySQL_Pagination(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		obj := makeObject(fmt.Sprintf("page-%d", i), "article")
		// Stagger creation times so ordering is deterministic.
		obj.CreatedAt = time.Now().Add(time.Duration(i) * time.Second).Truncate(time.Second)
		obj.UpdatedAt = obj.CreatedAt
		require.NoError(t, d.Objects().Create(ctx, obj))
	}

	objs, total, err := d.Objects().ListBySQL(ctx, "", nil, 2, 1)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, objs, 2)
}

func TestMergeTags(t *testing.T) {
	t.Run("basic merge", func(t *testing.T) {
		existing := []storage.Tag{{Label: "go", Weight: 1.0}}
		incoming := []storage.Tag{{Label: "rust", Weight: 0.8}}
		result := mergeTags(existing, incoming)
		assert.Len(t, result, 2)
		assert.Equal(t, "go", result[0].Label)
		assert.Equal(t, "rust", result[1].Label)
	})

	t.Run("dedup by label keeps first", func(t *testing.T) {
		existing := []storage.Tag{{Label: "go", Weight: 1.0, Source: "manual"}}
		incoming := []storage.Tag{{Label: "go", Weight: 0.5, Source: "auto"}}
		result := mergeTags(existing, incoming)
		assert.Len(t, result, 1)
		assert.Equal(t, 1.0, result[0].Weight)
		assert.Equal(t, "manual", result[0].Source)
	})

	t.Run("empty existing", func(t *testing.T) {
		result := mergeTags(nil, []storage.Tag{{Label: "new"}})
		assert.Len(t, result, 1)
	})

	t.Run("empty incoming", func(t *testing.T) {
		result := mergeTags([]storage.Tag{{Label: "old"}}, nil)
		assert.Len(t, result, 1)
	})

	t.Run("both empty", func(t *testing.T) {
		result := mergeTags(nil, nil)
		assert.Empty(t, result)
	})
}

func TestMergeStrings(t *testing.T) {
	t.Run("basic merge", func(t *testing.T) {
		result := mergeStrings([]string{"a", "b"}, []string{"c"})
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("dedup", func(t *testing.T) {
		result := mergeStrings([]string{"a", "b"}, []string{"b", "c"})
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("all duplicates", func(t *testing.T) {
		result := mergeStrings([]string{"a", "b"}, []string{"a", "b"})
		assert.Equal(t, []string{"a", "b"}, result)
	})

	t.Run("empty existing", func(t *testing.T) {
		result := mergeStrings(nil, []string{"a"})
		assert.Equal(t, []string{"a"}, result)
	})

	t.Run("empty incoming", func(t *testing.T) {
		result := mergeStrings([]string{"a"}, nil)
		assert.Equal(t, []string{"a"}, result)
	})

	t.Run("both empty", func(t *testing.T) {
		result := mergeStrings(nil, nil)
		assert.Empty(t, result)
	})
}

func TestGetByContentHashEmptyStore(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	got, err := d.Objects().GetByContentHash(ctx, "any-hash")
	assert.NoError(t, err)
	assert.Nil(t, got, "empty store must return nil, nil — not an error")
}

func TestGetByContentHash(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("hash-1", "article")
	obj.ContentHash = "abc123"
	require.NoError(t, d.Objects().Create(ctx, obj))

	t.Run("found", func(t *testing.T) {
		got, err := d.Objects().GetByContentHash(ctx, "abc123")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "hash-1", got.ID)
	})

	t.Run("not found returns nil nil", func(t *testing.T) {
		got, err := d.Objects().GetByContentHash(ctx, "nonexistent")
		assert.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("empty hash returns nil", func(t *testing.T) {
		got, err := d.Objects().GetByContentHash(ctx, "")
		assert.NoError(t, err)
		assert.Nil(t, got)
	})
}

func TestReinforce(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("reinf-1", "article")
	obj.ContentHash = "reinf-hash"
	obj.ReinforcementCount = 1
	obj.Tags = []storage.Tag{{Label: "original", Weight: 1.0}}
	obj.Mentions = []uri.URI{{Scheme: "ctxt", Namespace: "entity", ID: "alice"}}
	require.NoError(t, d.Objects().Create(ctx, obj))

	t.Run("increments count and merges", func(t *testing.T) {
		merge := &storage.KnowledgeObject{
			Tags:     []storage.Tag{{Label: "new-tag", Weight: 0.5}},
			Mentions: []uri.URI{{Scheme: "ctxt", Namespace: "entity", ID: "bob"}},
		}
		id, err := d.Objects().Reinforce(ctx, "reinf-hash", merge)
		require.NoError(t, err)
		assert.Equal(t, "reinf-1", id)

		got, err := d.Objects().Get(ctx, "reinf-1")
		require.NoError(t, err)
		assert.Equal(t, 2, got.ReinforcementCount)
		assert.Len(t, got.Tags, 2)
		assert.Len(t, got.Mentions, 2)
		assert.NotNil(t, got.LastReinforcedAt)
	})

	t.Run("empty hash errors", func(t *testing.T) {
		_, err := d.Objects().Reinforce(ctx, "", &storage.KnowledgeObject{})
		assert.Error(t, err)
	})
}

func TestListBySQL_JSONExtract(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj1 := makeObject("meta-1", "article")
	obj1.Metadata = map[string]any{"key": "val"}
	require.NoError(t, d.Objects().Create(ctx, obj1))

	obj2 := makeObject("meta-2", "article")
	obj2.Metadata = map[string]any{"key": "other"}
	require.NoError(t, d.Objects().Create(ctx, obj2))

	obj3 := makeObject("meta-3", "article")
	obj3.Metadata = map[string]any{"different": "field"}
	require.NoError(t, d.Objects().Create(ctx, obj3))

	objs, total, err := d.Objects().ListBySQL(ctx, "json_extract(metadata, '$.key') = ?", []any{"val"}, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, objs, 1)
	assert.Equal(t, "meta-1", objs[0].ID)
}

func TestVectorSearch_ReturnsRankedResults(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// Object A has embedding close to query direction.
	objA := makeObject("vec-a", "item")
	objA.Embeddings = []float32{1, 0, 0}
	require.NoError(t, d.Objects().Create(ctx, objA))

	// Object B has embedding orthogonal to query.
	objB := makeObject("vec-b", "item")
	objB.Embeddings = []float32{0, 1, 0}
	require.NoError(t, d.Objects().Create(ctx, objB))

	// Object C has embedding in opposite direction.
	objC := makeObject("vec-c", "item")
	objC.Embeddings = []float32{-1, 0, 0}
	require.NoError(t, d.Objects().Create(ctx, objC))

	// Query vector aligns with A.
	query := []float32{1, 0, 0}
	results, err := d.Objects().VectorSearch(ctx, query, storage.ObjectFilter{Limit: 3})
	require.NoError(t, err)
	require.Len(t, results, 3)

	// A should rank first (cosine = 1.0).
	assert.Equal(t, "vec-a", results[0].ID)
	// C should rank last (cosine = -1.0).
	assert.Equal(t, "vec-c", results[2].ID)

	// Scores should be attached in metadata.
	assert.InDelta(t, 1.0, results[0].Metadata["score"].(float64), 0.001)
}

func TestVectorSearch_FiltersByType(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	objA := makeObject("cat-v", "category")
	objA.Embeddings = []float32{1, 0, 0}
	require.NoError(t, d.Objects().Create(ctx, objA))

	objB := makeObject("item-v", "item")
	objB.Embeddings = []float32{1, 0, 0}
	require.NoError(t, d.Objects().Create(ctx, objB))

	results, err := d.Objects().VectorSearch(ctx, []float32{1, 0, 0}, storage.ObjectFilter{Type: "category", Limit: 10})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "cat-v", results[0].ID)
}

func TestVectorSearch_EmptyVectorError(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.Objects().VectorSearch(ctx, nil, storage.ObjectFilter{})
	assert.Error(t, err)
}

func TestVectorSearch_RespectsLimit(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		obj := makeObject(fmt.Sprintf("lim-%d", i), "item")
		obj.Embeddings = []float32{1, 0, 0}
		require.NoError(t, d.Objects().Create(ctx, obj))
	}

	results, err := d.Objects().VectorSearch(ctx, []float32{1, 0, 0}, storage.ObjectFilter{Type: "item", Limit: 2})
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// makeFTSObject builds an object whose projected_fts_body contains the given text.
// Uses a Graph with a summary node so that ProjectIndex derives the FTS body from
// the graph (matching the canonical projection path post-T-0195).
func makeFTSObject(id, typ, searchableText string) *storage.KnowledgeObject {
	now := time.Now().Truncate(time.Second)
	return &storage.KnowledgeObject{
		ID:        id,
		Type:      typ,
		CreatedAt: now,
		UpdatedAt: now,
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSummary, 0),
					NodeType: pluginapi.NodeTypeSummary,
					Label:    "Summary",
					Content:  searchableText,
					Order:    0,
				},
			},
		},
	}
}

func TestFTSSearch(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// Objects whose graph summary nodes contain distinct search terms.
	// ProjectIndex uses Graph nodes for FTSBody, so the projected_fts_body
	// column (and thus objects_fts) will contain these terms after Create.
	obj1 := makeFTSObject("fts-1", "article", "authentication best practices guide")
	require.NoError(t, d.Objects().Create(ctx, obj1))

	obj2 := makeFTSObject("fts-2", "article", "database indexing strategies")
	require.NoError(t, d.Objects().Create(ctx, obj2))

	// Rebuild FTS content table index from projected_fts_body column.
	_, err := d.db.ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)

	results, err := d.Objects().FTSSearch(ctx, "authentication", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "fts-1", results[0].ID)

	results, err = d.Objects().FTSSearch(ctx, "kubernetes", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestFTSSearch_HyphenatedQuerySanitised exercises the T-0565 hyphen-crash
// regression: before the fix, `credit-eligible` reached SQLite FTS5 raw,
// where `-` was parsed as NOT / column qualifier and produced
// "no such column: eligible". The service now passes user input through
// search.SafeFTSQuery before MATCH; this test calls FTSSearch with the
// already-sanitised expression and confirms FTS5 accepts it cleanly.
func TestFTSSearch_HyphenatedQuerySanitised(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeFTSObject("fts-hy-1", "article", "documents that are credit eligible")
	require.NoError(t, d.Objects().Create(ctx, obj))

	_, err := d.db.ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)

	// The sanitised form a real client now sends.
	results, err := d.Objects().FTSSearch(ctx, `"credit" "eligible"`, storage.ObjectFilter{Limit: 10})
	require.NoError(t, err, "sanitised FTS expression must not crash MATCH")
	require.Len(t, results, 1)
	assert.Equal(t, "fts-hy-1", results[0].ID)
}

func TestCosineSimilarity(t *testing.T) {
	assert.InDelta(t, 1.0, cosineSimilarity([]float32{1, 0, 0}, []float32{1, 0, 0}), 0.0001)
	assert.InDelta(t, 0.0, cosineSimilarity([]float32{1, 0, 0}, []float32{0, 1, 0}), 0.0001)
	assert.InDelta(t, -1.0, cosineSimilarity([]float32{1, 0, 0}, []float32{-1, 0, 0}), 0.0001)
	assert.InDelta(t, 0.0, cosineSimilarity([]float32{0, 0, 0}, []float32{1, 0, 0}), 0.0001)
}

func TestMetadataFacetConditionsSQLite(t *testing.T) {
	t.Run("empty filter yields no conditions", func(t *testing.T) {
		conds, args := metadataFacetConditionsSQLite(storage.ObjectFilter{})
		assert.Empty(t, conds)
		assert.Empty(t, args)
	})

	t.Run("all fields set", func(t *testing.T) {
		since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		until := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
		conds, args := metadataFacetConditionsSQLite(storage.ObjectFilter{
			MetadataType:   "observation",
			SourceType:     "slack",
			MetadataTopic:  "auth",
			MetadataPerson: "alice",
			MetadataSince:  &since,
			MetadataUntil:  &until,
		})
		assert.Len(t, conds, 6)
		assert.Len(t, args, 6)
		assert.Contains(t, conds[0], "json_extract(metadata, '$.type')")
		assert.Contains(t, conds[1], "json_extract(metadata, '$.source_type')")
		assert.Contains(t, conds[2], "$.topics")
		assert.Contains(t, conds[3], "$.people")
		assert.Contains(t, conds[4], "$.dates_mentioned")
		assert.Contains(t, conds[5], "$.dates_mentioned")
	})
}

func TestMatchesMetadataFacets(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"type":            "observation",
			"source_type":     "slack",
			"topics":          []any{"auth", "security"},
			"people":          []any{"alice", "bob"},
			"dates_mentioned": []any{"2026-03-15", "2026-04-10"},
		},
	}

	t.Run("no filter matches all", func(t *testing.T) {
		assert.True(t, matchesMetadataFacets(obj, storage.ObjectFilter{}))
	})

	t.Run("matching type", func(t *testing.T) {
		assert.True(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataType: "observation"}))
	})

	t.Run("non-matching type", func(t *testing.T) {
		assert.False(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataType: "task"}))
	})

	t.Run("matching topic", func(t *testing.T) {
		assert.True(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataTopic: "auth"}))
	})

	t.Run("non-matching topic", func(t *testing.T) {
		assert.False(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataTopic: "crypto"}))
	})

	t.Run("matching person", func(t *testing.T) {
		assert.True(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataPerson: "alice"}))
	})

	t.Run("non-matching person", func(t *testing.T) {
		assert.False(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataPerson: "charlie"}))
	})

	t.Run("matching source_type", func(t *testing.T) {
		assert.True(t, matchesMetadataFacets(obj, storage.ObjectFilter{SourceType: "slack"}))
	})

	t.Run("non-matching source_type", func(t *testing.T) {
		assert.False(t, matchesMetadataFacets(obj, storage.ObjectFilter{SourceType: "email"}))
	})

	t.Run("since filter matches", func(t *testing.T) {
		since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		assert.True(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataSince: &since}))
	})

	t.Run("since filter too late", func(t *testing.T) {
		since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		assert.False(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataSince: &since}))
	})

	t.Run("until filter matches", func(t *testing.T) {
		until := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
		assert.True(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataUntil: &until}))
	})

	t.Run("until filter too early", func(t *testing.T) {
		until := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		assert.False(t, matchesMetadataFacets(obj, storage.ObjectFilter{MetadataUntil: &until}))
	})

	t.Run("combined filters intersect", func(t *testing.T) {
		assert.True(t, matchesMetadataFacets(obj, storage.ObjectFilter{
			MetadataType:  "observation",
			MetadataTopic: "auth",
			SourceType:    "slack",
		}))
		assert.False(t, matchesMetadataFacets(obj, storage.ObjectFilter{
			MetadataType:  "observation",
			MetadataTopic: "crypto",
			SourceType:    "slack",
		}))
	})

	t.Run("nil metadata rejects non-empty filter", func(t *testing.T) {
		nilObj := &storage.KnowledgeObject{}
		assert.True(t, matchesMetadataFacets(nilObj, storage.ObjectFilter{}))
		assert.False(t, matchesMetadataFacets(nilObj, storage.ObjectFilter{MetadataType: "task"}))
	})
}
