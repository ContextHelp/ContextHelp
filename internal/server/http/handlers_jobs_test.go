package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
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
