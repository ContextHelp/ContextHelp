package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeJob(id string) *storage.Job {
	now := time.Now().Truncate(time.Second)
	return &storage.Job{
		ID:         id,
		Type:       "ingest:text",
		Status:     storage.JobPending,
		Payload:    "content for " + id,
		Pipeline:   "text.short",
		Source:     "cli",
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestEnqueueAndGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	job := makeJob("job-1")
	if err := d.Jobs().Create(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := d.Jobs().Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "job-1" {
		t.Errorf("ID: got %q", got.ID)
	}
	if got.Status != storage.JobPending {
		t.Errorf("Status: got %q", got.Status)
	}
}

func TestAcquireNext(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// Create two jobs with slightly different timestamps.
	j1 := makeJob("job-1")
	j1.CreatedAt = time.Now().Add(-2 * time.Second).Truncate(time.Second)
	d.Jobs().Create(ctx, j1)

	j2 := makeJob("job-2")
	j2.CreatedAt = time.Now().Add(-1 * time.Second).Truncate(time.Second)
	d.Jobs().Create(ctx, j2)

	acquired, err := d.Jobs().AcquireNext(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acquired == nil {
		t.Fatal("expected job, got nil")
	}
	if acquired.ID != "job-1" {
		t.Errorf("expected job-1, got %q", acquired.ID)
	}
	if acquired.Status != storage.JobRunning {
		t.Errorf("status: got %q, want running", acquired.Status)
	}
}

func TestAcquireNextEmpty(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	acquired, err := d.Jobs().AcquireNext(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acquired != nil {
		t.Errorf("expected nil, got %v", acquired)
	}
}

func TestJobComplete(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().AcquireNext(ctx)

	if err := d.Jobs().Complete(ctx, "job-1", "obj-result"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, _ := d.Jobs().Get(ctx, "job-1")
	if got.Status != storage.JobCompleted {
		t.Errorf("status: got %q", got.Status)
	}
	if got.ResultID != "obj-result" {
		t.Errorf("result_id: got %q", got.ResultID)
	}
}

func TestJobFail(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().AcquireNext(ctx)

	if err := d.Jobs().Fail(ctx, "job-1", "something broke"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	got, _ := d.Jobs().Get(ctx, "job-1")
	if got.Status != storage.JobFailed {
		t.Errorf("status: got %q", got.Status)
	}
	if got.Error != "something broke" {
		t.Errorf("error: got %q", got.Error)
	}
}

func TestJobRetry(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().AcquireNext(ctx)
	d.Jobs().Fail(ctx, "job-1", "error")

	if err := d.Jobs().Retry(ctx, "job-1"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	got, _ := d.Jobs().Get(ctx, "job-1")
	if got.Status != storage.JobPending {
		t.Errorf("status: got %q", got.Status)
	}
	if got.RetryCount != 1 {
		t.Errorf("retry_count: got %d, want 1", got.RetryCount)
	}
}

func TestJobList(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().Create(ctx, makeJob("job-2"))
	j3 := makeJob("job-3")
	d.Jobs().Create(ctx, j3)
	d.Jobs().AcquireNext(ctx) // makes one running

	jobs, total, err := d.Jobs().List(ctx, storage.JobFilter{Status: storage.JobPending})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Errorf("total pending: got %d, want 2", total)
	}
	if len(jobs) != 2 {
		t.Errorf("count: got %d", len(jobs))
	}
}

func TestRecoverStale(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().AcquireNext(ctx)

	// Recover with 0 timeout means all running jobs are stale.
	n, err := d.Jobs().RecoverStale(ctx, 0)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 1 {
		t.Errorf("recovered: got %d, want 1", n)
	}

	got, _ := d.Jobs().Get(ctx, "job-1")
	if got.Status != storage.JobPending {
		t.Errorf("status: got %q, want pending", got.Status)
	}
}

func TestMarshalJSON_Nil(t *testing.T) {
	result := marshalJSON(nil)
	assert.Equal(t, "null", result)
}

func TestMarshalJSON_NestedMap(t *testing.T) {
	input := map[string]any{"a": map[string]int{"b": 1}}
	result := marshalJSON(input)

	// Verify valid JSON by unmarshalling.
	var parsed map[string]any
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
	assert.Contains(t, parsed, "a")
}

func TestMarshalJSON_RoundTrip(t *testing.T) {
	input := map[string]any{
		"name":  "test",
		"count": float64(42),
		"nested": map[string]any{
			"inner": "value",
		},
	}

	marshalled := marshalJSON(input)

	var roundTripped map[string]any
	err := json.Unmarshal([]byte(marshalled), &roundTripped)
	require.NoError(t, err)
	assert.Equal(t, input, roundTripped)
}

// TestJobIdempotencyKeyRoundTrip: the key persists with the row and comes
// back on Get.
func TestJobIdempotencyKeyRoundTrip(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	job := makeJob("job-idem-1")
	job.IdempotencyKey = "key-abc"
	require.NoError(t, d.Jobs().Create(ctx, job))

	got, err := d.Jobs().Get(ctx, "job-idem-1")
	require.NoError(t, err)
	assert.Equal(t, "key-abc", got.IdempotencyKey)
}

// TestGetByIdempotencyKey: hit returns the row, miss returns (nil, nil).
func TestGetByIdempotencyKey(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	job := makeJob("job-idem-2")
	job.IdempotencyKey = "key-def"
	require.NoError(t, d.Jobs().Create(ctx, job))

	got, err := d.Jobs().GetByIdempotencyKey(ctx, "key-def")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "job-idem-2", got.ID)

	miss, err := d.Jobs().GetByIdempotencyKey(ctx, "no-such-key")
	require.NoError(t, err)
	assert.Nil(t, miss)
}

// TestIdempotencyKeyUniqueConstraint: a second insert with the same non-empty
// key is rejected by the partial unique index — the race-window guard behind
// the lookup-then-insert dedupe in service.Analyze.
func TestIdempotencyKeyUniqueConstraint(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	a := makeJob("job-idem-3a")
	a.IdempotencyKey = "key-race"
	require.NoError(t, d.Jobs().Create(ctx, a))

	b := makeJob("job-idem-3b")
	b.IdempotencyKey = "key-race"
	assert.Error(t, d.Jobs().Create(ctx, b), "duplicate idempotency key must be rejected")
}

// TestIdempotencyKeyEmptyNotUnique: rows without a key (the default) are
// exempt from the unique index — every legacy enqueue writes ”.
func TestIdempotencyKeyEmptyNotUnique(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, d.Jobs().Create(ctx, makeJob("job-idem-4a")))
	require.NoError(t, d.Jobs().Create(ctx, makeJob("job-idem-4b")))

	got, err := d.Jobs().GetByIdempotencyKey(ctx, "")
	require.NoError(t, err)
	assert.Nil(t, got, "empty key must never match")
}
