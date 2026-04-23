package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seedIndexObject creates a KnowledgeObject with structured metadata topics.
func seedIndexObject(
	t *testing.T, svc *service.Service,
	id string, topics []string, summary string,
) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	topicsAny := make([]any, len(topics))
	for i, tp := range topics {
		topicsAny[i] = tp
	}
	obj := &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: summary,
		Summaries:  []string{summary},
		Status:     "active",
		CreatedAt:  now,
		UpdatedAt:  now,
		Metadata: map[string]any{
			"metadata_status": "complete",
			"enrichment.structured_metadata": map[string]any{
				"type":       "observation",
				"topics":     topicsAny,
				"confidence": 0.9,
			},
		},
	}
	require.NoError(t, svc.Store.Objects().Create(
		context.Background(), obj,
	))
}

// TestUS0408_IndexShowsThreeCategories ingests 10 objects across 3 topics
// and verifies the index shows exactly 3 categories.
func TestUS0408_IndexShowsThreeCategories(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	// auth: 4 objects, caching: 3 objects, observability: 3 objects
	topicGroups := map[string]int{
		"auth":          4,
		"caching":       3,
		"observability": 3,
	}
	objNum := 0
	for topic, count := range topicGroups {
		for i := 0; i < count; i++ {
			objNum++
			seedIndexObject(t, env.svc,
				fmt.Sprintf("idx-obj-%d", objNum),
				[]string{topic},
				fmt.Sprintf("%s note %d", topic, i+1),
			)
		}
	}

	id, err := env.svc.BuildTopicIndex(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	idx, err := env.svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	require.NotNil(t, idx)

	assert.Len(t, idx.Categories, 3)
	assert.Len(t, idx.ObjectIDs, 10)

	catCounts := make(map[string]int)
	for _, cat := range idx.Categories {
		catCounts[cat.Topic] = len(cat.Entries)
	}
	assert.Equal(t, 4, catCounts["auth"])
	assert.Equal(t, 3, catCounts["caching"])
	assert.Equal(t, 3, catCounts["observability"])
}

// TestUS0408_IncrementalUpdate ingests 10 objects, builds index,
// then adds an 11th and verifies incremental update.
func TestUS0408_IncrementalUpdate(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	for i := 0; i < 10; i++ {
		topic := "auth"
		if i >= 5 {
			topic = "caching"
		}
		seedIndexObject(t, env.svc,
			fmt.Sprintf("inc-obj-%d", i),
			[]string{topic},
			fmt.Sprintf("note %d", i),
		)
	}

	_, err := env.svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	idx, err := env.svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	assert.Len(t, idx.ObjectIDs, 10)

	// Add 11th object with a new topic.
	now := time.Now().Truncate(time.Second)
	newObj := &storage.KnowledgeObject{
		ID:         "inc-obj-10",
		Type:       "text",
		RawContent: "monitoring dashboard",
		Summaries:  []string{"monitoring dashboard"},
		Status:     "active",
		CreatedAt:  now,
		UpdatedAt:  now,
		Metadata: map[string]any{
			"enrichment.structured_metadata": map[string]any{
				"topics": []any{"monitoring"},
			},
		},
	}
	require.NoError(t, env.svc.Store.Objects().Create(ctx, newObj))
	require.NoError(t, env.svc.UpdateTopicIndexIncremental(ctx, newObj))

	idx, err = env.svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	assert.Len(t, idx.ObjectIDs, 11)
	assert.Len(t, idx.Categories, 3) // auth, caching, monitoring
}

// TestUS0408_FilterByTopic verifies `ctxt index <topic>` returns
// only entries for that topic.
func TestUS0408_FilterByTopic(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	seedIndexObject(t, env.svc, "ft-1", []string{"auth"}, "OAuth")
	seedIndexObject(t, env.svc, "ft-2", []string{"auth"}, "JWT")
	seedIndexObject(t, env.svc, "ft-3", []string{"caching"}, "Redis")

	_, err := env.svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	entries, err := env.svc.GetTopicEntries(ctx, "auth")
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	entries, err = env.svc.GetTopicEntries(ctx, "caching")
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	entries, err = env.svc.GetTopicEntries(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, entries)
}

// TestUS0408_RefreshRegenerates verifies --refresh rebuilds
// the index from scratch.
func TestUS0408_RefreshRegenerates(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	seedIndexObject(t, env.svc, "ref-1", []string{"auth"}, "OAuth")
	id1, err := env.svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	// Add more objects.
	seedIndexObject(t, env.svc, "ref-2", []string{"caching"}, "Redis")

	// Rebuild (simulates --refresh).
	id2, err := env.svc.BuildTopicIndex(ctx)
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "should reuse same index object")

	idx, err := env.svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	assert.Len(t, idx.Categories, 2)
	assert.Len(t, idx.ObjectIDs, 2)
}
