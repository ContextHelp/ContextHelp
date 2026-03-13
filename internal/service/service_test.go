package service

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := pipeline.DefaultRegistry()
	engine := search.NewEngine(driver)
	return New(driver, q, pipes, engine, "")
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
		ToType: "entity", ToID: "@ui.layout",
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
	objs := []*storage.KnowledgeObject{
		{
			ID:        "o-abc123",
			Type:      "decision",
			Summaries: []string{"Defer infrastructure refactor"},
			Source:    "engineering-meeting.pdf",
			Mentions:  []string{"@team.alice"},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "o-def456",
			Type:      "note",
			Summaries: []string{"Market timing analysis"},
			Source:    "slack-#engineering",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
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
	// The first citation (o-abc123) has @team.alice mention.
	var found bool
	for _, c := range result.Citations {
		for _, ent := range c.Entities {
			if ent == "@team.alice" {
				found = true
			}
		}
	}
	assert.True(t, found, "expected @team.alice in enriched entities")
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
