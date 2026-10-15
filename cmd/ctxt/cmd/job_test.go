package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seedJobs puts one job per state on the instance.
func seedJobs(t *testing.T, db *testDB) {
	t.Helper()
	seedJob(t, db, "job_pend", "ingest:text", "text.short", storage.JobPending)
	seedJob(t, db, "job_fail", "ingest:url", "url.generic", storage.JobFailed)
	seedJob(t, db, "job_done", "ingest:text", "text.short", storage.JobCompleted)
}

func storedJobStatus(t *testing.T, db *testDB, id string) storage.JobStatus {
	t.Helper()
	job, err := db.Driver.Jobs().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("job %s: %v", id, err)
	}
	return job.Status
}

// TestJobListOverAPI: ctxt job list reads the instance's queue over the
// API and never opens a local store.
func TestJobListOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedJobs(t, db)
	local := pointStorageElsewhere(t, db)

	out, err := db.exec("job", "list")
	if err != nil {
		t.Fatalf("job list: %v", err)
	}
	for _, id := range []string{"job_pend", "job_fail", "job_done", "3 total"} {
		if !strings.Contains(out, id) {
			t.Errorf("output lacks %q:\n%s", id, out)
		}
	}
	assertNoLocalStore(t, local)

	out, err = db.exec("job", "list", "--state", "failed", "--format", "json")
	if err != nil {
		t.Fatalf("job list --state failed: %v", err)
	}
	var got struct {
		Jobs  []storage.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if got.Total != 1 || len(got.Jobs) != 1 || got.Jobs[0].ID != "job_fail" {
		t.Errorf("json = %+v", got)
	}

	out, err = db.exec("job", "list", "--state", "pending", "--format", "csv")
	if err != nil {
		t.Fatalf("job list csv: %v", err)
	}
	if !strings.Contains(out, "job_pend") || strings.Contains(out, "job_fail") || strings.Contains(out, "total") {
		t.Errorf("csv = %q", out)
	}
}

func TestJobListRejectsUnknownState(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("job", "list", "--state", "stuck")
	if exitOf(err) != output.ExitUsage {
		t.Fatalf("exit %d (%v), want usage", exitOf(err), err)
	}
}

func TestJobStatusOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedJobs(t, db)
	local := pointStorageElsewhere(t, db)

	out, err := db.exec("job", "status", "job_fail")
	if err != nil {
		t.Fatalf("job status: %v", err)
	}
	for _, want := range []string{"job_fail", "failed", "url.generic", "Retries:    0/3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	assertNoLocalStore(t, local)

	out, err = db.exec("job", "status", "job_done", "--format", "json")
	if err != nil {
		t.Fatalf("job status json: %v", err)
	}
	var job storage.Job
	if err := json.Unmarshal([]byte(out), &job); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if job.ID != "job_done" || job.Status != storage.JobCompleted {
		t.Errorf("json = %+v", job)
	}

	if _, err := db.exec("job", "status", "job_missing"); exitOf(err) != output.ExitNotFound {
		t.Errorf("missing job: exit %d (%v), want not found", exitOf(err), err)
	}
}

func TestJobRetryOverAPI(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedJobs(t, db)

	db.useRole(t, dpkmstest.RoleReader)
	if _, err := db.exec("job", "retry", "job_fail"); exitOf(err) != output.ExitUnauthorized {
		t.Fatalf("reader retry: exit %d (%v)", exitOf(err), err)
	}

	db.useRole(t, dpkmstest.RoleWriter)
	out, err := db.exec("job", "retry", "job_fail")
	if err != nil {
		t.Fatalf("writer retry: %v", err)
	}
	if !strings.Contains(out, "job_fail") {
		t.Errorf("output = %q", out)
	}
	if got := storedJobStatus(t, db, "job_fail"); got != storage.JobPending {
		t.Errorf("stored status %q, want pending", got)
	}
}

// TestJobCancelOverAPI: cancel needs the admin role; a finished job is a
// conflict (exit 4), an unknown one not found (exit 3).
func TestJobCancelOverAPI(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedJobs(t, db)

	db.useRole(t, dpkmstest.RoleWriter)
	if _, err := db.exec("job", "cancel", "job_pend", "--confirm=yes"); exitOf(err) != output.ExitUnauthorized {
		t.Fatalf("writer cancel: exit %d (%v)", exitOf(err), err)
	}
	if got := storedJobStatus(t, db, "job_pend"); got != storage.JobPending {
		t.Fatalf("refused cancel changed the job: %q", got)
	}

	db.useRole(t, dpkmstest.RoleAdmin)
	out, err := db.exec("job", "cancel", "job_pend", "--confirm=yes", "--format", "json")
	if err != nil {
		t.Fatalf("admin cancel: %v", err)
	}
	var job storage.Job
	if err := json.Unmarshal([]byte(out), &job); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if job.ID != "job_pend" || job.Status != storage.JobCancelled {
		t.Errorf("json = %+v", job)
	}
	if got := storedJobStatus(t, db, "job_pend"); got != storage.JobCancelled {
		t.Errorf("stored status %q, want cancelled", got)
	}

	if _, err := db.exec("job", "cancel", "job_done", "--confirm=yes"); exitOf(err) != output.ExitConflict {
		t.Errorf("finished job: exit %d (%v), want conflict", exitOf(err), err)
	}
	if _, err := db.exec("job", "cancel", "job_missing", "--confirm=yes"); exitOf(err) != output.ExitNotFound {
		t.Errorf("missing job: exit %d (%v), want not found", exitOf(err), err)
	}
}
