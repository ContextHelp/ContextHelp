package cmd

import (
	"strings"
	"testing"
)

func TestHousekeepingVacuum(t *testing.T) {
	out, err := executeCommand("housekeeping", "vacuum")
	if err != nil {
		t.Fatalf("housekeeping vacuum should succeed: %v", err)
	}
	if !strings.Contains(out, "Running VACUUM on database") {
		t.Error("output should describe vacuum operation")
	}
	if !strings.Contains(out, "Vacuum completed successfully") {
		t.Error("output should confirm success")
	}
}

func TestHousekeepingReindex(t *testing.T) {
	out, err := executeCommand("housekeeping", "reindex")
	if err != nil {
		t.Fatalf("housekeeping reindex should succeed: %v", err)
	}
	if !strings.Contains(out, "Rebuilding search indexes") {
		t.Error("output should describe reindex operation")
	}
	if !strings.Contains(out, "Reindexing FTS") {
		t.Error("output should mention FTS reindexing")
	}
	if !strings.Contains(out, "Reindexing vectors") {
		t.Error("output should mention vector reindexing")
	}
	if !strings.Contains(out, "Reindexing graph") {
		t.Error("output should mention graph reindexing")
	}
	if !strings.Contains(out, "Reindexing completed successfully") {
		t.Error("output should confirm success")
	}
}

func TestHousekeepingCompact(t *testing.T) {
	out, err := executeCommand("housekeeping", "compact")
	if err != nil {
		t.Fatalf("housekeeping compact should succeed: %v", err)
	}
	if !strings.Contains(out, "Compacting database") {
		t.Error("output should describe compact operation")
	}
	if !strings.Contains(out, "Compaction completed successfully") {
		t.Error("output should confirm success")
	}
}

func TestHousekeepingPruneMissingBeforeError(t *testing.T) {
	_, err := executeCommand("housekeeping", "prune")
	if err == nil {
		t.Error("prune without --before should fail (required flag)")
	}
}

func TestHousekeepingHelp(t *testing.T) {
	out, err := executeCommand("housekeeping", "--help")
	if err != nil {
		t.Fatalf("housekeeping --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"vacuum", "reindex", "compact", "prune"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("housekeeping help should list subcommand %q", subcmd)
		}
	}
}

func TestServeHelp(t *testing.T) {
	out, err := executeCommand("serve", "--help")
	if err != nil {
		t.Fatalf("serve --help should succeed: %v", err)
	}
	for _, flag := range []string{"--port", "--grpc-port", "--workers", "--public", "--profile"} {
		if !strings.Contains(out, flag) {
			t.Errorf("serve help should list flag %s", flag)
		}
	}
}
