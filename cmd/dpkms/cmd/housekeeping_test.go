package cmd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHousekeepingVacuum(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.run("housekeeping", "vacuum")
	if err != nil {
		t.Fatalf("housekeeping vacuum should succeed: %v", err)
	}
	if !strings.Contains(out, "Running VACUUM on database") {
		t.Error("output should describe vacuum operation")
	}
	if !strings.Contains(out, "Vacuum completed") {
		t.Error("output should confirm success")
	}
}

func TestHousekeepingReindex(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.run("housekeeping", "reindex")
	if err != nil {
		t.Fatalf("housekeeping reindex should succeed: %v", err)
	}
	if !strings.Contains(out, "Rebuilding search indexes") {
		t.Error("output should describe reindex operation")
	}
	if !strings.Contains(out, "Reindexing completed") {
		t.Error("output should confirm success")
	}
}

func TestHousekeepingCompact(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.run("housekeeping", "compact")
	if err != nil {
		t.Fatalf("housekeeping compact should succeed: %v", err)
	}
	if !strings.Contains(out, "Compacting database") {
		t.Error("output should describe compact operation")
	}
	if !strings.Contains(out, "Compaction completed") {
		t.Error("output should confirm success")
	}
}

func TestHousekeepingPruneMissingBeforeError(t *testing.T) {
	_, err := executeCommand("housekeeping", "prune")
	if err == nil {
		t.Error("prune without --before should fail (required flag)")
	}
}

func TestHousekeepingPruneNoObjects(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.run("housekeeping", "prune", "--before", "2024-01-01")
	require.NoError(t, err, "prune with no matching objects should succeed")
	assert.Contains(t, out, "No objects found before 2024-01-01")
}

func TestHousekeepingPruneWithConfirmNo(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Seed an old object
	obj := &storage.KnowledgeObject{
		ID:        "obj_old_1",
		Type:      "text",
		CreatedAt: time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	// Pipe "n" to stdin
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	_, err := w.WriteString("n\n")
	require.NoError(t, err)
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	out, err := db.run("housekeeping", "prune", "--before", "2024-01-01")
	require.NoError(t, err)
	assert.Contains(t, out, "Found 1 objects before 2024-01-01")
	assert.Contains(t, out, "Pruning cancelled")
}

func TestHousekeepingPruneWithConfirmYes(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_old_2",
		Type:      "text",
		CreatedAt: time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	// Pipe "y" to stdin
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	_, err := w.WriteString("y\n")
	require.NoError(t, err)
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	out, err := db.run("housekeeping", "prune", "--before", "2024-01-01")
	require.NoError(t, err)
	assert.Contains(t, out, "Pruned 1 objects")
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
