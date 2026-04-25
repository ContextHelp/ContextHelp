package cmd

import (
	"strings"
	"testing"
)

func TestJobDeprecated(t *testing.T) {
	_, err := executeCommand("job")
	if err == nil {
		t.Fatal("job should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms job") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestJobListDeprecated(t *testing.T) {
	_, err := executeCommand("job", "list")
	if err == nil {
		t.Fatal("job list should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms job list") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestJobStatusDeprecated(t *testing.T) {
	_, err := executeCommand("job", "status", "job_123")
	if err == nil {
		t.Fatal("job status should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms job status") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestJobRetryDeprecated(t *testing.T) {
	_, err := executeCommand("job", "retry", "job_123")
	if err == nil {
		t.Fatal("job retry should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms job retry") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}

func TestJobCancelDeprecated(t *testing.T) {
	_, err := executeCommand("job", "cancel", "job_123")
	if err == nil {
		t.Fatal("job cancel should return error (moved to dpkms)")
	}
	if !strings.Contains(err.Error(), "moved to dpkms job cancel") {
		t.Errorf("error should mention dpkms, got: %v", err)
	}
}
