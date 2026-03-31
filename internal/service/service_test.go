package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/uri"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := pipeline.DefaultRegistry()
	engine := search.NewEngine(driver)
	return New(driver, q, pipes, engine, "", nil)
}

func TestAnalyzeDuplicateDrop(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// First ingestion.
	req := AnalyzeRequest{Content: "hello duplicate world", Type: "text"}
	jobID1, err := svc.Analyze(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, jobID1)

	// Simulate pipeline completing: create an object with a known hash.
	hash := "sha256:test-hash-drop"
	obj := &storage.KnowledgeObject{
		ID:          "obj-drop-1",
		Type:        "text",
		ContentHash: hash,
		RawContent:  "hello duplicate world",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))

	// Override config to use drop policy and point hash at known object.
	svc.Cfg.Duplicates = config.DuplicatesConfig{
		Policy:              "drop",
		CheckExact:          true,
		SimilarityThreshold: 0.95,
	}

	// Second ingestion with same hash — should return existing object ID, not a new job.
	req2 := AnalyzeRequest{Content: "hello duplicate world", Type: "text", KnownHash: hash}
	result, err := svc.Analyze(ctx, req2)
	require.NoError(t, err)
	assert.Equal(t, "obj-drop-1", result, "drop policy should return existing object ID")

	// Confirm only one object exists.
	objs, total, err := svc.Store.Objects().List(ctx, storage.ObjectFilter{})
	require.NoError(t, err)
	assert.Equal(t, 1, total, "only one object should exist after drop dedup")
	assert.Equal(t, "obj-drop-1", objs[0].ID)
}

// TestAnalyzeDuplicateDropAutoHash tests that Analyze auto-computes the content hash
// when KnownHash is not provided, enabling exact dedup without requiring callers to
// pre-compute the hash. This validates the auto-hash wiring added to the Analyze path.
func TestAnalyzeDuplicateDropAutoHash(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	const content = "auto hash dedup test content"
	const source = ""

	// Compute the hash that Analyze will derive internally.
	expectedHash := storageutil.ContentHash(content, source)

	// Create an object with that hash to simulate a pre-existing ingestion.
	obj := &storage.KnowledgeObject{
		ID:          "obj-auto-hash-1",
		Type:        "text",
		ContentHash: expectedHash,
		RawContent:  content,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))

	// Enable exact dedup with drop policy.
	svc.Cfg.Duplicates = config.DuplicatesConfig{
		Policy:     "drop",
		CheckExact: true,
	}

	// Second analyze with same content but NO KnownHash — should auto-compute and match.
	result, err := svc.Analyze(ctx, AnalyzeRequest{Content: content, Type: "text"})
	require.NoError(t, err)
	assert.Equal(t, "obj-auto-hash-1", result, "auto-hash dedup should return existing object ID")
}

func TestAnalyze(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	jobID, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: "hello world",
		Type:    "text",
		Source:  "test",
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if jobID == "" {
		t.Fatal("expected non-empty job ID")
	}

	// Verify job was enqueued.
	job, err := svc.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != storage.JobPending {
		t.Errorf("status: got %q, want pending", job.Status)
	}
	if job.Payload != "hello world" {
		t.Errorf("payload: got %q", job.Payload)
	}
}

func TestAnalyzeRawFlagSkipsEnrichment(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	id, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: "raw content no AI",
		Type:    "text",
		Source:  "test",
		Raw:     true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, id)

	// Must be stored as a KnowledgeObject directly, not as a job.
	obj, err := svc.Store.Objects().Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, obj)

	assert.Equal(t, "raw", obj.Status, "status must be raw")
	assert.Equal(t, "raw content no AI", obj.RawContent)
	assert.Empty(t, obj.Summaries, "no summaries expected — no AI enrichment")
	assert.Empty(t, obj.Tags, "no tags expected — no AI enrichment")
	assert.Empty(t, obj.Embeddings, "no embeddings expected — no AI enrichment")

	// No job must have been enqueued.
	jobs, total, err := svc.ListJobs(ctx, storage.JobFilter{})
	require.NoError(t, err)
	assert.Equal(t, 0, total, "no jobs enqueued for raw ingestion")
	assert.Empty(t, jobs)

	// Object must appear in status=raw filter.
	objs, count, err := svc.Store.Objects().List(ctx, storage.ObjectFilter{Status: "raw"})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, id, objs[0].ID)
}

func TestGetObject(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:        "obj-1",
		Type:      "article",
		CreatedAt: now,
		UpdatedAt: now,
	}
	svc.Store.Objects().Create(ctx, obj)

	got, err := svc.GetObject(ctx, "obj-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "obj-1" {
		t.Errorf("ID: got %q", got.ID)
	}
}

func TestListObjects(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		obj := &storage.KnowledgeObject{
			ID:        "obj-" + string(rune('a'+i)),
			Type:      "article",
			CreatedAt: now,
			UpdatedAt: now,
		}
		svc.Store.Objects().Create(ctx, obj)
	}

	objs, total, err := svc.ListObjects(ctx, storage.ObjectFilter{Type: "article", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 {
		t.Errorf("total: got %d, want 3", total)
	}
	if len(objs) != 3 {
		t.Errorf("count: got %d", len(objs))
	}
}

func TestUpdateObject(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:        "obj-1",
		Type:      "article",
		CreatedAt: now,
		UpdatedAt: now,
	}
	svc.Store.Objects().Create(ctx, obj)

	obj.Type = "note"
	obj.UpdatedAt = time.Now().Truncate(time.Second)
	if err := svc.UpdateObject(ctx, obj); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := svc.GetObject(ctx, "obj-1")
	if got.Type != "note" {
		t.Errorf("type: got %q, want note", got.Type)
	}
}

func TestDeleteObject(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:        "obj-1",
		Type:      "article",
		CreatedAt: now,
		UpdatedAt: now,
	}
	svc.Store.Objects().Create(ctx, obj)

	if err := svc.DeleteObject(ctx, "obj-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := svc.GetObject(ctx, "obj-1")
	if err == nil {
		t.Error("expected error for deleted object")
	}
}

func TestSearchObjects(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	})
	svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-2", Type: "note", CreatedAt: now, UpdatedAt: now,
	})

	results, total, err := svc.SearchObjects(ctx, "type==article", 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
	if len(results) != 1 {
		t.Errorf("results: got %d", len(results))
	}
}

func TestGetJob(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	jobID, _ := svc.Analyze(ctx, AnalyzeRequest{Content: "test", Type: "text", Source: "cli"})

	got, err := svc.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.ID != jobID {
		t.Errorf("ID: got %q", got.ID)
	}
}

func TestListJobs(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.Analyze(ctx, AnalyzeRequest{Content: "a", Type: "text", Source: "cli"})
	svc.Analyze(ctx, AnalyzeRequest{Content: "b", Type: "text", Source: "cli"})
	svc.Analyze(ctx, AnalyzeRequest{Content: "c", Type: "text", Source: "cli"})

	list, total, err := svc.ListJobs(ctx, storage.JobFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 {
		t.Errorf("total: got %d, want 3", total)
	}
	if len(list) != 3 {
		t.Errorf("count: got %d", len(list))
	}
}

func TestRetryJob(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	jobID, _ := svc.Analyze(ctx, AnalyzeRequest{Content: "test", Type: "text", Source: "cli"})

	// Acquire and fail the job.
	svc.Queue.AcquireNext(ctx)
	svc.Queue.Fail(ctx, jobID, "broke")

	if err := svc.RetryJob(ctx, jobID); err != nil {
		t.Fatalf("retry: %v", err)
	}

	got, _ := svc.GetJob(ctx, jobID)
	if got.Status != storage.JobPending {
		t.Errorf("status: got %q, want pending", got.Status)
	}
}

func TestGetEntity(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	svc.Store.Entities().Upsert(ctx, &storage.Entity{
		Slug: "@ui.layout", Title: "UI Layout", CreatedAt: now, UpdatedAt: now,
	})

	got, err := svc.GetEntity(ctx, "@ui.layout")
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	if got.Slug != "@ui.layout" {
		t.Errorf("slug: got %q", got.Slug)
	}
}

func TestEntityBacklinks(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	})

	svc.Store.Edges().Create(ctx, &storage.Edge{
		ID:       "edge-1",
		FromType: "object", FromID: "obj-1",
		ToType: "entity", ToID: "ctxt://entity/ui/layout",
		EdgeType: "mentions", Weight: 1.0, CreatedAt: now,
	})

	objs, err := svc.EntityBacklinks(ctx, "@ui.layout")
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(objs) != 1 {
		t.Errorf("backlinks: got %d, want 1", len(objs))
	}
	if objs[0].ID != "obj-1" {
		t.Errorf("ID: got %q", objs[0].ID)
	}
}

func seedEntities(t *testing.T, ctx context.Context, svc *Service) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	entities := []*storage.Entity{
		{Slug: "@ui.layout", Title: "UI Layout", Namespace: "ui", CreatedAt: now, UpdatedAt: now},
		{Slug: "@ui.color", Title: "UI Color", Namespace: "ui", CreatedAt: now, UpdatedAt: now},
		{Slug: "@api.auth", Title: "API Auth", Namespace: "api", CreatedAt: now, UpdatedAt: now},
	}
	for _, e := range entities {
		require.NoError(t, svc.Store.Entities().Upsert(ctx, e))
	}
}

func TestListEntities(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	seedEntities(t, ctx, svc)

	got, err := svc.ListEntities(ctx, storage.EntityFilter{})
	require.NoError(t, err)
	assert.Len(t, got, 3)

	// Results are ordered by slug ASC.
	slugs := make([]string, len(got))
	for i, e := range got {
		slugs[i] = e.Slug
	}
	assert.Equal(t, []string{"@api.auth", "@ui.color", "@ui.layout"}, slugs)
}

func TestListEntitiesFilterByNamespace(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	seedEntities(t, ctx, svc)

	got, err := svc.ListEntities(ctx, storage.EntityFilter{Namespace: "ui"})
	require.NoError(t, err)
	assert.Len(t, got, 2)

	for _, e := range got {
		assert.Equal(t, "ui", e.Namespace)
	}
}

func TestListEntitiesWithLimit(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	seedEntities(t, ctx, svc)

	got, err := svc.ListEntities(ctx, storage.EntityFilter{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	// First entity by slug ASC is @api.auth.
	assert.Equal(t, "@api.auth", got[0].Slug)
}

func TestListEntitiesEmpty(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	got, err := svc.ListEntities(ctx, storage.EntityFilter{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ---------------------------------------------------------------------------
// Compose / ComposeWithCitations
// ---------------------------------------------------------------------------

func seedObjectsForCompose(t *testing.T, ctx context.Context, svc *Service) []*storage.KnowledgeObject {
	t.Helper()
	now := time.Now().Truncate(time.Second)

	// Build graph-canonical fixtures; summaries/sections derived from graph nodes.
	obj1 := storageutil.BuildGraphKO("o-abc123", "decision", "Defer infrastructure refactor")
	obj1.Source = "engineering-meeting.pdf"
	obj1.Mentions = []uri.URI{{Scheme: "ctxt", Space: "entity", ID: "team/alice"}}
	obj1.CreatedAt = now
	obj1.UpdatedAt = now

	obj2 := storageutil.BuildGraphKO("o-def456", "note", "Market timing analysis")
	obj2.Source = "slack-#engineering"
	obj2.CreatedAt = now
	obj2.UpdatedAt = now

	objs := []*storage.KnowledgeObject{obj1, obj2}
	for _, obj := range objs {
		require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	}
	return objs
}

func TestCompose_BasicOutput(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	objs := seedObjectsForCompose(t, ctx, svc)

	result, err := svc.Compose(ctx, objs, "brief")
	require.NoError(t, err)
	assert.Contains(t, result, "brief")
	assert.Contains(t, result, "o-abc123")
	assert.Contains(t, result, "o-def456")
}

func TestCompose_EmptyObjects(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	result, err := svc.Compose(ctx, nil, "brief")
	require.NoError(t, err)
	assert.Contains(t, result, "brief")
}

func TestComposeWithCitations_ReturnsCitationResult(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	objs := seedObjectsForCompose(t, ctx, svc)

	result, err := svc.ComposeWithCitations(ctx, objs, "brief")
	require.NoError(t, err)
	assert.NotNil(t, result)
	// Content should include object IDs as inline citations.
	assert.Contains(t, result.Content, "o-abc123")
	assert.Contains(t, result.Content, "o-def456")
}

func TestComposeWithCitations_IncludesReferenceTable(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	objs := seedObjectsForCompose(t, ctx, svc)

	result, err := svc.ComposeWithCitations(ctx, objs, "brief")
	require.NoError(t, err)
	// Reference table must be appended.
	assert.Contains(t, result.Content, "## References")
	assert.Contains(t, result.Content, "| ID | Type | Summary | Source | Created |")
}

func TestComposeWithCitations_CitationsSlice(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	objs := seedObjectsForCompose(t, ctx, svc)

	result, err := svc.ComposeWithCitations(ctx, objs, "brief")
	require.NoError(t, err)
	// At least one citation should be present in the structured slice.
	assert.NotEmpty(t, result.Citations)
}

func TestComposeWithCitations_EntitiesEnriched(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	objs := seedObjectsForCompose(t, ctx, svc)

	result, err := svc.ComposeWithCitations(ctx, objs, "brief")
	require.NoError(t, err)
	// The first citation (o-abc123) has team/alice mention URI.
	var found bool
	for _, c := range result.Citations {
		for _, ent := range c.Entities {
			if ent == "ctxt://entity/team/alice" {
				found = true
			}
		}
	}
	assert.True(t, found, "expected ctxt://entity/team/alice in enriched entities")
}

func TestComposeWithCitations_SourceIDs(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	objs := seedObjectsForCompose(t, ctx, svc)

	result, err := svc.ComposeWithCitations(ctx, objs, "brief")
	require.NoError(t, err)
	assert.Contains(t, result.SourceIDs, "o-abc123")
	assert.Contains(t, result.SourceIDs, "o-def456")
}

func TestComposeWithCitations_CompositionType(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	objs := seedObjectsForCompose(t, ctx, svc)

	result, err := svc.ComposeWithCitations(ctx, objs, "plan")
	require.NoError(t, err)
	assert.Equal(t, "plan", result.Type)
}

func TestRelatedObjects(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Seed three objects.
	for _, id := range []string{"ro-1", "ro-2", "ro-3"} {
		require.NoError(t, svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "note", CreatedAt: now, UpdatedAt: now,
		}))
	}

	// ro-1 and ro-2 both mention "entity-X"; ro-3 mentions "entity-Y" only.
	mkEdge := func(id, from, to string) *storage.Edge {
		return &storage.Edge{
			ID:       id, FromType: "object", FromID: from,
			ToType: "entity", ToID: to, EdgeType: "mentions",
			Weight: 1.0, CreatedAt: now,
		}
	}
	require.NoError(t, svc.Store.Edges().Create(ctx, mkEdge("re1", "ro-1", "entity-X")))
	require.NoError(t, svc.Store.Edges().Create(ctx, mkEdge("re2", "ro-2", "entity-X")))
	require.NoError(t, svc.Store.Edges().Create(ctx, mkEdge("re3", "ro-3", "entity-Y")))

	related, err := svc.RelatedObjects(ctx, "ro-1", 1, 0)
	require.NoError(t, err)

	require.Len(t, related, 1)
	assert.Equal(t, "ro-2", related[0].ID)
}

func TestRelatedObjectsDefaultLimit(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Seed seed + 12 related objects sharing entity-X.
	require.NoError(t, svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "seed", Type: "note", CreatedAt: now, UpdatedAt: now,
	}))
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("peer-%02d", i)
		require.NoError(t, svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "note", CreatedAt: now, UpdatedAt: now,
		}))
		require.NoError(t, svc.Store.Edges().Create(ctx, &storage.Edge{
			ID:       "e-" + id, FromType: "object", FromID: id,
			ToType: "entity", ToID: "entity-X", EdgeType: "mentions",
			Weight: 1.0, CreatedAt: now,
		}))
	}
	require.NoError(t, svc.Store.Edges().Create(ctx, &storage.Edge{
		ID:       "e-seed", FromType: "object", FromID: "seed",
		ToType: "entity", ToID: "entity-X", EdgeType: "mentions",
		Weight: 1.0, CreatedAt: now,
	}))

	// limit=0 triggers the default of 10.
	related, err := svc.RelatedObjects(ctx, "seed", 1, 0)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(related), 10)
}

// ---------------------------------------------------------------------------
// objectSnippet — projection helper
// ---------------------------------------------------------------------------

func TestObjectSnippet_GraphCanonical(t *testing.T) {
	obj := storageutil.BuildGraphKO("ko-1", "note", "graph-derived content", "tag1")
	s := objectSnippet(obj)
	assert.Contains(t, s, "graph-derived content")
}

func TestObjectSnippet_FlatSummary(t *testing.T) {
	obj := &storage.KnowledgeObject{
		ID:        "flat-1",
		Summaries: []string{"summary text"},
	}
	assert.Equal(t, "summary text", objectSnippet(obj))
}

func TestObjectSnippet_FlatRawContent(t *testing.T) {
	obj := &storage.KnowledgeObject{
		ID:         "flat-2",
		RawContent: "raw content here",
	}
	assert.Equal(t, "raw content here", objectSnippet(obj))
}

func TestObjectSnippet_LongRawContentTruncated(t *testing.T) {
	content := make([]byte, 600)
	for i := range content {
		content[i] = 'x'
	}
	obj := &storage.KnowledgeObject{
		ID:         "flat-3",
		RawContent: string(content),
	}
	s := objectSnippet(obj)
	assert.Len(t, s, 503) // 500 + "..."
	assert.True(t, len(s) <= 503)
}

func TestObjectSnippet_Empty(t *testing.T) {
	obj := &storage.KnowledgeObject{ID: "empty-1"}
	assert.Equal(t, "", objectSnippet(obj))
}

// ---------------------------------------------------------------------------
// SearchObjectsNodeAware
// ---------------------------------------------------------------------------

func TestSearchObjectsNodeAware_NoFilter(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	require.NoError(t, svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "na-1", Type: "note", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "na-2", Type: "note", CreatedAt: now, UpdatedAt: now,
	}))

	results, total, err := svc.SearchObjectsNodeAware(ctx, "type==note", 10, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, results, 2)
}

func TestSearchObjectsNodeAware_NodeTypeFilter(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	graphObj := storageutil.BuildGraphKO("graph-obj", "note", "hello graph world", "tag1")
	now := time.Now().Truncate(time.Second)
	graphObj.CreatedAt = now
	graphObj.UpdatedAt = now
	require.NoError(t, svc.Store.Objects().Create(ctx, graphObj))

	flatObj := &storage.KnowledgeObject{
		ID: "flat-obj", Type: "note", TextContent: "hello flat world",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, flatObj))

	filter := &pluginapi.NodeAwareFilter{
		NodeTypes: []string{string(pluginapi.NodeTypeSummary)},
	}
	results, total, err := svc.SearchObjectsNodeAware(ctx, "type==note", 10, 0, filter)
	require.NoError(t, err)
	// Only the graph object has NodeTypeSummary nodes.
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, "graph-obj", results[0].ID)
}

// ---------------------------------------------------------------------------
// HybridResult.DocumentView populated
// ---------------------------------------------------------------------------

func TestHybridSearchExplain_PopulatesDocumentView(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("dv-1", []string{"document view test content"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))

	// Rebuild FTS index so the object is findable.
	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	results, err := svc.HybridSearchExplain(ctx, "document view test", 10, nil, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	r := results[0]
	assert.Equal(t, "dv-1", r.Object.ID)
	// DocumentView is populated via ProjectDocument. Because the object has a Graph
	// (BuildGraphKO path), Sections are non-empty and Body is empty.
	assert.NotEmpty(t, r.DocumentView.Sections, "DocumentView.Sections must be populated from graph nodes")
	assert.Equal(t, "document view test content", r.DocumentView.Sections[0].Content)
}

// ---------------------------------------------------------------------------
// T-0209: source-aware DetectInput for automatic pipeline selection
// ---------------------------------------------------------------------------

// newTestServiceWithDetector returns a service whose registry has an extra
// detector prepended. The detector maps a fixed source prefix to a pipeline name.
func newTestServiceWithDetector(t *testing.T, sourcePfx, pipelineName string) *Service {
	t.Helper()
	svc := newTestService(t)
	svc.Pipes.RegisterDetector(pipeline.DetectorFunc(func(in pipeline.DetectInput) (string, error) {
		if len(in.Source) >= len(sourcePfx) && in.Source[:len(sourcePfx)] == sourcePfx {
			return pipelineName, nil
		}
		return "", pipeline.ErrDelegate
	}))
	svc.Pipes.Upsert(pipelineName, &pipeline.Pipeline{PipelineName: pipelineName})
	return svc
}

// TestAnalyzeUsesSourceForPipelineDetection verifies that when a file-backed
// source is present, Analyze passes it to Detect so extension/URL detectors
// fire correctly. Regression test for T-0209.
func TestAnalyzeUsesSourceForPipelineDetection(t *testing.T) {
	svc := newTestServiceWithDetector(t, "/vault/notes/", "watch.file")
	ctx := context.Background()

	jobID, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: "some text that would normally resolve to text.short",
		Type:    "text",
		Source:  "/vault/notes/meeting.md",
	})
	require.NoError(t, err)
	require.NotEmpty(t, jobID)

	job, err := svc.GetJob(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, "watch.file", job.Pipeline,
		"Analyze must route by source path, not raw content")
}

// TestEnqueueUsesSourceForPipelineDetection verifies that Enqueue also passes
// the real source to Detect. Regression test for T-0209.
func TestEnqueueUsesSourceForPipelineDetection(t *testing.T) {
	svc := newTestServiceWithDetector(t, "https://example.com/", "url.ingest")
	ctx := context.Background()

	jobID, err := svc.Enqueue(ctx, AnalyzeRequest{
		Content: "page body text",
		Type:    "url",
		Source:  "https://example.com/article",
	})
	require.NoError(t, err)
	require.NotEmpty(t, jobID)

	job, err := svc.GetJob(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, "url.ingest", job.Pipeline,
		"Enqueue must route by source URL, not raw content")
}

// TestTriageInboxUsesSourceForPipelineDetection verifies that inbox triage
// passes the stored object's Source to Detect. Regression test for T-0209.
func TestTriageInboxUsesSourceForPipelineDetection(t *testing.T) {
	svc := newTestServiceWithDetector(t, "/drop/", "drop.file")
	ctx := context.Background()

	// Capture an inbox item with a file source.
	obj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{
		Content: "dropped file content",
		Type:    "text",
		Source:  "/drop/report.pdf",
	})
	require.NoError(t, err)

	jobID, err := svc.TriageInbox(ctx, obj.ID, TriageRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, jobID)

	job, err := svc.GetJob(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, "drop.file", job.Pipeline,
		"TriageInbox must route by object.Source, not raw content")
}
