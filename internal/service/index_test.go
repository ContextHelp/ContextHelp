package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func newIndexTestService(t *testing.T) *Service {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	return &Service{Store: driver}
}

func seedIndexObj(
	t *testing.T, svc *Service,
	id string, topics []string, summary string,
) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: summary,
		Status:     "active",
		CreatedAt:  now,
		UpdatedAt:  now,
		Metadata: map[string]any{
			"metadata_status": "complete",
			"enrichment.structured_metadata": map[string]any{
				"type":       "observation",
				"topics":     topics,
				"confidence": 0.9,
			},
		},
	}
	if summary != "" {
		obj.Summaries = []string{summary}
	}
	require.NoError(t, svc.Store.Objects().Create(
		context.Background(), obj,
	))
}

func TestBuildTopicIndex_GroupsByTopic(t *testing.T) {
	svc := newIndexTestService(t)
	ctx := context.Background()

	seedIndexObj(t, svc, "obj-1", []string{"auth", "security"}, "OAuth setup")
	seedIndexObj(t, svc, "obj-2", []string{"auth"}, "JWT tokens")
	seedIndexObj(t, svc, "obj-3", []string{"caching"}, "Redis layer")

	id, err := svc.BuildTopicIndex(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	idx, err := svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	require.NotNil(t, idx)

	assert.Len(t, idx.Categories, 3) // auth, caching, security

	topicMap := make(map[string]int)
	for _, cat := range idx.Categories {
		topicMap[cat.Topic] = len(cat.Entries)
	}
	assert.Equal(t, 2, topicMap["auth"])
	assert.Equal(t, 1, topicMap["security"])
	assert.Equal(t, 1, topicMap["caching"])
}

func TestBuildTopicIndex_SkipsObjectsWithoutTopics(t *testing.T) {
	svc := newIndexTestService(t)
	ctx := context.Background()

	seedIndexObj(t, svc, "obj-1", []string{"auth"}, "Has topic")

	// Object without structured metadata.
	now := time.Now().Truncate(time.Second)
	require.NoError(t, svc.Store.Objects().Create(ctx,
		&storage.KnowledgeObject{
			ID: "obj-no-meta", Type: "text", RawContent: "no meta",
			Status: "active", CreatedAt: now, UpdatedAt: now,
		},
	))

	_, err := svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	idx, err := svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	assert.Len(t, idx.Categories, 1)
	assert.Equal(t, "auth", idx.Categories[0].Topic)
}

func TestGetTopicEntries_CaseInsensitive(t *testing.T) {
	svc := newIndexTestService(t)
	ctx := context.Background()

	seedIndexObj(t, svc, "obj-1", []string{"Authentication"}, "SSO")
	_, err := svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	entries, err := svc.GetTopicEntries(ctx, "authentication")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "SSO", entries[0].Summary)
}

func TestGetTopicEntries_PrefixMatch(t *testing.T) {
	svc := newIndexTestService(t)
	ctx := context.Background()

	seedIndexObj(t, svc, "obj-1", []string{"authentication"}, "SSO")
	_, err := svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	entries, err := svc.GetTopicEntries(ctx, "auth")
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestGetTopicIndex_NilWhenNoIndex(t *testing.T) {
	svc := newIndexTestService(t)
	idx, err := svc.GetTopicIndex(context.Background())
	require.NoError(t, err)
	assert.Nil(t, idx)
}

func TestUpdateTopicIndexIncremental(t *testing.T) {
	svc := newIndexTestService(t)
	ctx := context.Background()

	seedIndexObj(t, svc, "obj-1", []string{"auth"}, "OAuth")
	_, err := svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	// Add a new object incrementally.
	now := time.Now().Truncate(time.Second)
	newObj := &storage.KnowledgeObject{
		ID: "obj-2", Type: "text", RawContent: "Redis cache",
		Summaries: []string{"Redis cache"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
		Metadata: map[string]any{
			"enrichment.structured_metadata": map[string]any{
				"topics": []any{"caching"},
			},
		},
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, newObj))
	require.NoError(t, svc.UpdateTopicIndexIncremental(ctx, newObj))

	idx, err := svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	assert.Len(t, idx.Categories, 2) // auth + caching

	topicMap := make(map[string]int)
	for _, cat := range idx.Categories {
		topicMap[cat.Topic] = len(cat.Entries)
	}
	assert.Equal(t, 1, topicMap["auth"])
	assert.Equal(t, 1, topicMap["caching"])
}

func TestUpdateTopicIndexIncremental_Idempotent(t *testing.T) {
	svc := newIndexTestService(t)
	ctx := context.Background()

	seedIndexObj(t, svc, "obj-1", []string{"auth"}, "OAuth")
	_, err := svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	obj, err := svc.Store.Objects().Get(ctx, "obj-1")
	require.NoError(t, err)

	// Incrementally add same object twice — should be no-op.
	require.NoError(t, svc.UpdateTopicIndexIncremental(ctx, obj))
	require.NoError(t, svc.UpdateTopicIndexIncremental(ctx, obj))

	idx, err := svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	assert.Len(t, idx.Categories[0].Entries, 1)
}

func TestBuildTopicIndex_SkipsSelfIndexObject(t *testing.T) {
	svc := newIndexTestService(t)
	ctx := context.Background()

	seedIndexObj(t, svc, "obj-1", []string{"auth"}, "OAuth")
	_, err := svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	// Rebuild — should not include the topic_index object itself.
	_, err = svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	idx, err := svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	assert.Len(t, idx.Categories, 1)
	assert.Len(t, idx.Categories[0].Entries, 1)
}

func TestOneLiner_Truncates(t *testing.T) {
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	obj := &storage.KnowledgeObject{
		Summaries: []string{string(long)},
	}
	result := oneLiner(obj)
	assert.Len(t, result, 120)
	assert.True(t, result[len(result)-3:] == "...")
}
