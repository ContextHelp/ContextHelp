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
