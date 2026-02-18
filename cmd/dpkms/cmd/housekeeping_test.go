package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestHousekeepingPruneWithBeforeFlag(t *testing.T) {
	// Pipe "n" to stdin so fmt.Scanln doesn't hang
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	_, err := w.WriteString("n\n")
	require.NoError(t, err)
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	out, err := executeCommand("housekeeping", "prune", "--before", "2024-01-01")
	require.NoError(t, err, "prune with --before should succeed")
	assert.Contains(t, out, "2024-01-01", "output should contain the date")
	assert.Contains(t, out, "Scanning database", "output should show scanning step")
	assert.Contains(t, out, "Proceed with deletion", "output should prompt for confirmation")
	assert.Contains(t, out, "Pruning cancelled", "output should confirm cancellation")
}

func TestHousekeepingPruneConfirmYes(t *testing.T) {
	// Pipe "y" to stdin to confirm deletion
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	_, err := w.WriteString("y\n")
	require.NoError(t, err)
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	out, err := executeCommand("housekeeping", "prune", "--before", "2024-01-01")
	require.NoError(t, err, "prune with --before and confirmation should succeed")
	assert.Contains(t, out, "2024-01-01", "output should contain the date")
	assert.Contains(t, out, "Pruning completed successfully", "output should confirm completion")
	assert.Contains(t, out, "Deleted", "output should report deleted items")
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
