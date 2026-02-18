package cmd

import (
	"strings"
	"testing"
)

func TestJobsList(t *testing.T) {
	out, err := executeCommand("jobs", "list")
	if err != nil {
		t.Fatalf("jobs list should succeed: %v", err)
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
	out, err := executeCommand("jobs", "status", "job_12345678")
	if err != nil {
		t.Fatalf("jobs status should succeed: %v", err)
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
	_, err := executeCommand("jobs", "status")
	if err == nil {
		t.Error("jobs status without ID should fail")
	}
}

func TestJobsLogs(t *testing.T) {
	out, err := executeCommand("jobs", "logs", "job_12345678")
	if err != nil {
		t.Fatalf("jobs logs should succeed: %v", err)
	}
	if !strings.Contains(out, "Job Logs: job_12345678") {
		t.Error("output should show job ID")
	}
	if !strings.Contains(out, "Starting pipeline") {
		t.Error("output should contain log entries")
	}
}

func TestJobsLogsNoIDError(t *testing.T) {
	_, err := executeCommand("jobs", "logs")
	if err == nil {
		t.Error("jobs logs without ID should fail")
	}
}

func TestJobsRetry(t *testing.T) {
	out, err := executeCommand("jobs", "retry", "job_12345678")
	if err != nil {
		t.Fatalf("jobs retry should succeed: %v", err)
	}
	if !strings.Contains(out, "Retrying job: job_12345678") {
		t.Error("output should confirm retry")
	}
	if !strings.Contains(out, "queued for execution") {
		t.Error("output should confirm re-queue")
	}
}

func TestJobsRetryNoIDError(t *testing.T) {
	_, err := executeCommand("jobs", "retry")
	if err == nil {
		t.Error("jobs retry without ID should fail")
	}
}

func TestJobsCancel(t *testing.T) {
	out, err := executeCommand("jobs", "cancel", "job_12345678")
	if err != nil {
		t.Fatalf("jobs cancel should succeed: %v", err)
	}
	if !strings.Contains(out, "Cancelling job: job_12345678") {
		t.Error("output should confirm cancellation")
	}
	if !strings.Contains(out, "cancelled") {
		t.Error("output should confirm job cancelled")
	}
}

func TestJobsCancelNoIDError(t *testing.T) {
	_, err := executeCommand("jobs", "cancel")
	if err == nil {
		t.Error("jobs cancel without ID should fail")
	}
}

func TestJobsHelp(t *testing.T) {
	out, err := executeCommand("jobs", "--help")
	if err != nil {
		t.Fatalf("jobs --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"list", "status", "logs", "retry", "cancel"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("jobs help should list subcommand %q", subcmd)
		}
	}
}
