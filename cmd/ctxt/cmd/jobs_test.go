package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func seedJob(t *testing.T, db *testDB, id, typ, pipeline string, status storage.JobStatus) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	job := &storage.Job{
		ID:         id,
		Type:       typ,
		Status:     status,
		Pipeline:   pipeline,
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Driver.Jobs().Create(context.Background(), job); err != nil {
		t.Fatalf("seed job %s: %v", id, err)
	}
}

func TestJobsList(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_001", "ingest:text", "text.short", storage.JobCompleted)
	seedJob(t, db, "job_002", "ingest:url", "url.generic", storage.JobPending)

	out, err := db.exec("job", "list")
	if err != nil {
		t.Fatalf("job list should succeed: %v", err)
	}
	if !strings.Contains(out, "Jobs") {
		t.Error("output should contain jobs header")
	}
	if !strings.Contains(out, "job_001") {
		t.Error("output should contain job IDs")
	}
}

func TestJobsListWithFilter(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_p1", "ingest:text", "text.short", storage.JobPending)
	seedJob(t, db, "job_c1", "ingest:text", "text.short", storage.JobCompleted)

	out, err := db.exec("job", "list", "--state", "pending")
	if err != nil {
		t.Fatalf("job list with --state should succeed: %v", err)
	}
	if !strings.Contains(out, "job_p1") {
		t.Error("output should contain pending job")
	}
}

func TestJobsStatus(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_s1", "ingest:text", "text.short", storage.JobPending)

	out, err := db.exec("job", "status", "job_s1")
	if err != nil {
		t.Fatalf("job status should succeed: %v", err)
	}
	if !strings.Contains(out, "Job: job_s1") {
		t.Error("output should show job ID")
	}
	if !strings.Contains(out, "Status:") {
		t.Error("output should contain Status field")
	}
	if !strings.Contains(out, "Pipeline:") {
		t.Error("output should contain Pipeline field")
	}
}

func TestJobsStatusNoIDError(t *testing.T) {
	_, err := executeCommand("job", "status")
	if err == nil {
		t.Error("job status without ID should fail")
	}
}

func TestJobsLogs(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_l1", "ingest:text", "text.short", storage.JobPending)

	out, err := db.exec("job", "log", "job_l1")
	if err != nil {
		t.Fatalf("job log should succeed: %v", err)
	}
	if !strings.Contains(out, "Logs for job job_l1") {
		t.Error("output should show job ID in logs header")
	}
}

func TestJobsLogsNoIDError(t *testing.T) {
	_, err := executeCommand("job", "log")
	if err == nil {
		t.Error("job log without ID should fail")
	}
}

func TestJobsRetry(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_r1", "ingest:text", "text.short", storage.JobFailed)

	out, err := db.exec("job", "retry", "job_r1")
	if err != nil {
		t.Fatalf("job retry should succeed: %v", err)
	}
	if !strings.Contains(out, "job_r1") {
		t.Error("output should mention job ID")
	}
	if !strings.Contains(out, "retry") {
		t.Error("output should confirm retry")
	}
}

func TestJobsRetryNoIDError(t *testing.T) {
	_, err := executeCommand("job", "retry")
	if err == nil {
		t.Error("job retry without ID should fail")
	}
}

func TestJobsCancel(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_x1", "ingest:text", "text.short", storage.JobPending)

	out, err := db.exec("job", "cancel", "job_x1")
	if err != nil {
		t.Fatalf("job cancel should succeed: %v", err)
	}
	if !strings.Contains(out, "job_x1") {
		t.Error("output should mention job ID")
	}
	if !strings.Contains(out, "cancelled") {
		t.Error("output should confirm cancellation")
	}
}

func TestJobsCancelNoIDError(t *testing.T) {
	_, err := executeCommand("job", "cancel")
	if err == nil {
		t.Error("job cancel without ID should fail")
	}
}

func TestJobsHelp(t *testing.T) {
	out, err := executeCommand("job", "--help")
	if err != nil {
		t.Fatalf("job --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"list", "status", "log", "retry", "cancel"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("job help should list subcommand %q", subcmd)
		}
	}
}
