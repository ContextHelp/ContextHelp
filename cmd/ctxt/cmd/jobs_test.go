package cmd

import (
	"strings"
	"testing"
)

func TestJobsList(t *testing.T) {
	out, err := executeCommand("job", "list")
	if err != nil {
		t.Fatalf("job list should succeed: %v", err)
	}
	if !strings.Contains(out, "Listing jobs") {
		t.Error("output should contain listing header")
	}
	if !strings.Contains(out, "job_12345678") {
		t.Error("output should contain sample job IDs")
	}
	if !strings.Contains(out, "Completed") {
		t.Error("output should show job states")
	}
}

func TestJobsStatus(t *testing.T) {
	out, err := executeCommand("job", "status", "job_12345678")
	if err != nil {
		t.Fatalf("job status should succeed: %v", err)
	}
	if !strings.Contains(out, "Job Status: job_12345678") {
		t.Error("output should show job ID")
	}
	if !strings.Contains(out, "State:") {
		t.Error("output should contain State field")
	}
	if !strings.Contains(out, "Pipeline:") {
		t.Error("output should contain Pipeline field")
	}
	if !strings.Contains(out, "Progress:") {
		t.Error("output should contain Progress field")
	}
}

func TestJobsStatusNoIDError(t *testing.T) {
	_, err := executeCommand("job", "status")
	if err == nil {
		t.Error("job status without ID should fail")
	}
}

func TestJobsLogs(t *testing.T) {
	out, err := executeCommand("job", "log", "job_12345678")
	if err != nil {
		t.Fatalf("job log should succeed: %v", err)
	}
	if !strings.Contains(out, "Job Logs: job_12345678") {
		t.Error("output should show job ID")
	}
	if !strings.Contains(out, "Starting pipeline") {
		t.Error("output should contain log entries")
	}
}

func TestJobsLogsNoIDError(t *testing.T) {
	_, err := executeCommand("job", "log")
	if err == nil {
		t.Error("job log without ID should fail")
	}
}

func TestJobsRetry(t *testing.T) {
	out, err := executeCommand("job", "retry", "job_12345678")
	if err != nil {
		t.Fatalf("job retry should succeed: %v", err)
	}
	if !strings.Contains(out, "Retrying job: job_12345678") {
		t.Error("output should confirm retry")
	}
	if !strings.Contains(out, "queued for execution") {
		t.Error("output should confirm re-queue")
	}
}

func TestJobsRetryNoIDError(t *testing.T) {
	_, err := executeCommand("job", "retry")
	if err == nil {
		t.Error("job retry without ID should fail")
	}
}

func TestJobsCancel(t *testing.T) {
	out, err := executeCommand("job", "cancel", "job_12345678")
	if err != nil {
		t.Fatalf("job cancel should succeed: %v", err)
	}
	if !strings.Contains(out, "Cancelling job: job_12345678") {
		t.Error("output should confirm cancellation")
	}
	if !strings.Contains(out, "cancelled") {
		t.Error("output should confirm job cancelled")
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
