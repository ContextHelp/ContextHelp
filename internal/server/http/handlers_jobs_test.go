package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedJob(t *testing.T, ts *testServerBundle, id string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	ts.svc.Store.Jobs().Create(context.Background(), &storage.Job{
		ID: id, Type: "ingest:text", Status: storage.JobPending,
		Payload: "content", Pipeline: "text.short", Source: "test",
		MaxRetries: 3, CreatedAt: now, UpdatedAt: now,
	})
}

// TestListJobsEmptyCollection ensures empty DB returns [] not null (T-0219).
func TestListJobsEmptyCollection(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/jobs?limit=20")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw["data"]) == "null" {
		t.Errorf("data: got null, want empty array []")
	}
	if string(raw["data"]) != "[]" {
		t.Errorf("data: got %s, want []", raw["data"])
	}
}

func TestListJobs(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	seedJob(t, ts, "job-1")
	seedJob(t, ts, "job-2")
	seedJob(t, ts, "job-3")

	resp, err := http.Get(ts.URL + "/api/v1/jobs")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data  []storage.Job `json:"data"`
		Total int           `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 3 {
		t.Errorf("total: got %d, want 3", body.Total)
	}
}

func TestGetJob(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	seedJob(t, ts, "job-1")

	resp, err := http.Get(ts.URL + "/api/v1/jobs/job-1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var job storage.Job
	json.NewDecoder(resp.Body).Decode(&job)
	if job.ID != "job-1" {
		t.Errorf("ID: got %q", job.ID)
	}
}

func TestRetryJob(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	seedJob(t, ts, "job-1")
	ctx := context.Background()

	// Acquire and fail the job.
	ts.svc.Store.Jobs().AcquireNext(ctx)
	ts.svc.Store.Jobs().Fail(ctx, "job-1", "broke")

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/jobs/job-1/retry", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var job storage.Job
	json.NewDecoder(resp.Body).Decode(&job)
	if job.Status != storage.JobPending {
		t.Errorf("status: got %q, want pending", job.Status)
	}
}

func TestRetryJobNotFound(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/jobs/nonexistent-job/retry", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var env ErrorEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	assert.Equal(t, "INVALID_REQUEST", env.Error.Code)
	assert.Contains(t, env.Error.Message, "not found")
}

func TestRetryJobMaxRetriesExceeded(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	seedJob(t, ts, "job-maxretry")
	ctx := context.Background()

	// MaxRetries is 3. Acquire, fail, retry 3 times to exhaust retries.
	for i := 0; i < 3; i++ {
		ts.svc.Store.Jobs().AcquireNext(ctx)
		ts.svc.Store.Jobs().Fail(ctx, "job-maxretry", "error")
		err := ts.svc.Store.Jobs().Retry(ctx, "job-maxretry")
		require.NoError(t, err, "retry %d should succeed", i+1)
	}

	// Now retry_count == 3, which equals MaxRetries (3).
	// The Queue.Retry check (job.RetryCount >= job.MaxRetries) should fail.
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/jobs/job-maxretry/retry", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var env ErrorEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	assert.Equal(t, "INVALID_REQUEST", env.Error.Code)
	assert.Contains(t, env.Error.Message, "max retries")
}

func TestRetryJobReturnsUpdatedJob(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	seedJob(t, ts, "job-updated")
	ctx := context.Background()

	// Acquire and fail the job so it can be retried.
	ts.svc.Store.Jobs().AcquireNext(ctx)
	ts.svc.Store.Jobs().Fail(ctx, "job-updated", "something broke")

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/jobs/job-updated/retry", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var job storage.Job
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&job))
	assert.Equal(t, "job-updated", job.ID)
	assert.Equal(t, storage.JobPending, job.Status)
	assert.Equal(t, 1, job.RetryCount, "retry count should be incremented to 1")
}
