package service

import (
	"context"
	"errors"
	"testing"

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
