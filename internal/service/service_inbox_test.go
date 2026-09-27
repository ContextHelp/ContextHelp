package service

import (
	"context"
	"errors"
	"fmt"
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

type failingJobStore struct {
	storage.JobStore
}

func (s *failingJobStore) Create(ctx context.Context, job *storage.Job) error {
	return errors.New("enqueue failed")
}

func TestTriageInboxAtomic(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	// Wrap the real job store with one that always fails Create.
	failStore := &failingJobStore{JobStore: driver.Jobs()}
	q := jobs.NewQueue(failStore)
	pipes := pipeline.DefaultRegistry()
	engine := search.NewEngine(driver)
	svc := New(driver, q, pipes, engine, "", nil)

	ctx := context.Background()

	// 1. Capture an item.
	obj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{
		Content: "test content",
		Type:    "text",
	})
	require.NoError(t, err)
	assert.Equal(t, "inbox", obj.Status)

	// 2. Attempt triage. This should fail because of our failingJobStore.
	jobID, err := svc.TriageInbox(ctx, obj.ID, TriageRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enqueue failed")
	assert.Empty(t, jobID)

	// 3. Verify the object is still in "inbox" status (compensating logic fired).
	got, err := svc.Store.Objects().Get(ctx, obj.ID)
	require.NoError(t, err)
	assert.Equal(t, "inbox", got.Status, "object status should have been reverted to inbox")
}

// ClearInbox discards every inbox item, however many pages they span,
// and leaves objects in any other status alone.
func TestClearInboxClearsEveryPageAndOnlyInbox(t *testing.T) {
	old := clearInboxPage
	clearInboxPage = 2
	t.Cleanup(func() { clearInboxPage = old })

	driver := storageutil.NewTestDriver(t)
	svc := New(driver, jobs.NewQueue(driver.Jobs()), pipeline.DefaultRegistry(), search.NewEngine(driver), "", nil)
	ctx := context.Background()

	var inbox []string
	for i := range 5 {
		obj, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{Content: fmt.Sprintf("item %d", i), Type: "text"})
		require.NoError(t, err)
		inbox = append(inbox, obj.ID)
	}
	now := time.Now().Truncate(time.Second)
	for id, status := range map[string]string{"keep-active": "active", "keep-raw": "raw"} {
		require.NoError(t, driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "text", Status: status, CreatedAt: now, UpdatedAt: now,
		}))
	}

	n, err := svc.ClearInbox(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	for _, id := range inbox {
		got, err := driver.Objects().Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, "discarded", got.Status, id)
	}
	for id, status := range map[string]string{"keep-active": "active", "keep-raw": "raw"} {
		got, err := driver.Objects().Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, status, got.Status, id)
	}

	n, err = svc.ClearInbox(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
}

// Triage and discard act on inbox items only: an object in any other
// status, or no object at all, is storage.ErrNotFound, and nothing is
// written or enqueued.
func TestTriageDiscardRefuseNonInboxObjects(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	created := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	for id, status := range map[string]string{"o-active": "active", "o-discarded": "discarded", "o-raw": "raw"} {
		require.NoError(t, svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "text", Status: status, RawContent: id, CreatedAt: created, UpdatedAt: created,
		}))
	}
	jobCount := func() int {
		_, n, err := svc.Store.Jobs().List(ctx, storage.JobFilter{Limit: 100})
		require.NoError(t, err)
		return n
	}
	jobsBefore := jobCount()

	for id, status := range map[string]string{"o-active": "active", "o-discarded": "discarded", "o-raw": "raw", "o-missing": ""} {
		jobID, err := svc.TriageInbox(ctx, id, TriageRequest{})
		require.ErrorIs(t, err, storage.ErrNotFound, "triage %s", id)
		assert.Empty(t, jobID, "triage %s", id)
		require.ErrorIs(t, svc.DiscardInbox(ctx, id), storage.ErrNotFound, "discard %s", id)
		if status == "" {
			continue
		}
		got, err := svc.Store.Objects().Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, status, got.Status, id)
		assert.True(t, got.UpdatedAt.Equal(created), "%s updated_at moved to %s", id, got.UpdatedAt)
	}
	assert.Equal(t, jobsBefore, jobCount(), "a refused triage enqueued a job")
}

// A real inbox item still triages (active, one job) and discards.
func TestTriageDiscardInboxItems(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{Content: "to triage", Type: "text"})
	require.NoError(t, err)
	b, err := svc.CaptureToInbox(ctx, InboxCaptureRequest{Content: "to discard", Type: "text"})
	require.NoError(t, err)

	jobID, err := svc.TriageInbox(ctx, a.ID, TriageRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, jobID)
	job, err := svc.Store.Jobs().Get(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, "to triage", job.Payload)
	require.NoError(t, svc.DiscardInbox(ctx, b.ID))

	got, err := svc.Store.Objects().Get(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", got.Status)
	got, err = svc.Store.Objects().Get(ctx, b.ID)
	require.NoError(t, err)
	assert.Equal(t, "discarded", got.Status)

	// Once out of the inbox, neither can be processed again.
	_, err = svc.TriageInbox(ctx, a.ID, TriageRequest{})
	require.ErrorIs(t, err, storage.ErrNotFound)
	require.ErrorIs(t, svc.DiscardInbox(ctx, b.ID), storage.ErrNotFound)
}
