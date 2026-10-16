package dpkmsclient_test

import (
	"context"
	"testing"
	"time"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// jobsFixture is a protected instance holding one job per state.
func jobsFixture(t *testing.T) *dpkmstest.Server {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()
	at, _ := time.Parse(time.RFC3339, "2026-04-12T09:00:00Z")
	for i, j := range []struct {
		id     string
		status storage.JobStatus
		err    string
	}{
		{"j-pending", storage.JobPending, ""},
		{"j-failed", storage.JobFailed, "boom"},
		{"j-done", storage.JobCompleted, ""},
	} {
		created := at.Add(time.Duration(i) * time.Minute)
		if err := driver.Jobs().Create(ctx, &storage.Job{
			ID: j.id, Type: "ingest:text", Status: j.status, Pipeline: "text.short",
			Error:      j.err,
			MaxRetries: 3, CreatedAt: created, UpdatedAt: created,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return dpkmstest.Start(t, driver, dpkmstest.WithKeys())
}

func jobStatus(t *testing.T, srv *dpkmstest.Server, id string) storage.JobStatus {
	t.Helper()
	job, err := srv.Driver.Jobs().Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return job.Status
}

func TestListJobs(t *testing.T) {
	srv := jobsFixture(t)
	c := roleClient(t, srv, dpkmstest.RoleAdmin)
	ctx := context.Background()

	jobs, total, err := c.ListJobs(ctx, dpkmsclient.JobListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(jobs) != 3 {
		t.Fatalf("total=%d len=%d", total, len(jobs))
	}

	jobs, total, err = c.ListJobs(ctx, dpkmsclient.JobListRequest{Status: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(jobs) != 1 || jobs[0].ID != "j-failed" || jobs[0].Error != "boom" {
		t.Fatalf("failed filter: total=%d jobs=%+v", total, jobs)
	}

	jobs, total, err = c.ListJobs(ctx, dpkmsclient.JobListRequest{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(jobs) != 1 {
		t.Fatalf("page: total=%d len=%d", total, len(jobs))
	}
}

func TestGetJob(t *testing.T) {
	srv := jobsFixture(t)
	c := roleClient(t, srv, dpkmstest.RoleAdmin)
	ctx := context.Background()

	job, err := c.GetJob(ctx, "j-failed")
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "j-failed" || job.Status != storage.JobFailed || job.MaxRetries != 3 {
		t.Errorf("job = %+v", job)
	}
	_, err = c.GetJob(ctx, "missing")
	if e := envelope(t, err); e.ExitCode != output.ExitNotFound {
		t.Errorf("missing: exit %d", e.ExitCode)
	}
}

func TestRetryJob(t *testing.T) {
	srv := jobsFixture(t)
	ctx := context.Background()

	// A reader lacks write:jobs; a writer holds it but jobs are
	// owner-only (ADR-080 §6), so both are refused and only the owner
	// retries.
	for _, role := range []string{dpkmstest.RoleReader, dpkmstest.RoleWriter} {
		_, err := roleClient(t, srv, role).RetryJob(ctx, "j-failed")
		if e := envelope(t, err); e.ExitCode != output.ExitUnauthorized {
			t.Errorf("%s retry: exit %d", role, e.ExitCode)
		}
	}
	job, err := roleClient(t, srv, dpkmstest.RoleAdmin).RetryJob(ctx, "j-failed")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != storage.JobPending || job.RetryCount != 1 {
		t.Errorf("retried job = %+v", job)
	}
	if got := jobStatus(t, srv, "j-failed"); got != storage.JobPending {
		t.Errorf("stored status %q", got)
	}
}

// Cancel needs cancel:jobs, which only the admin role holds; a finished
// job is a CONFLICT and an unknown one NOT_FOUND.
func TestCancelJob(t *testing.T) {
	srv := jobsFixture(t)
	ctx := context.Background()

	for _, role := range []string{dpkmstest.RoleReader, dpkmstest.RoleWriter} {
		_, err := roleClient(t, srv, role).CancelJob(ctx, "j-pending")
		if e := envelope(t, err); e.ExitCode != output.ExitUnauthorized {
			t.Errorf("%s cancel: exit %d", role, e.ExitCode)
		}
	}
	if got := jobStatus(t, srv, "j-pending"); got != storage.JobPending {
		t.Fatalf("refused cancel changed the job: %q", got)
	}

	admin := roleClient(t, srv, dpkmstest.RoleAdmin)
	job, err := admin.CancelJob(ctx, "j-pending")
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "j-pending" || job.Status != storage.JobCancelled {
		t.Errorf("cancelled job = %+v", job)
	}
	if got := jobStatus(t, srv, "j-pending"); got != storage.JobCancelled {
		t.Errorf("stored status %q", got)
	}

	_, err = admin.CancelJob(ctx, "j-done")
	if e := envelope(t, err); e.ExitCode != output.ExitConflict {
		t.Errorf("finished: exit %d", e.ExitCode)
	}
	_, err = admin.CancelJob(ctx, "missing")
	if e := envelope(t, err); e.ExitCode != output.ExitNotFound {
		t.Errorf("missing: exit %d", e.ExitCode)
	}
}
