package cmd

import (
	"context"
	"encoding/json"
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

func TestHousekeepingRunDryRun(t *testing.T) {
	db := setupTestDB(t)

	// Only the summary lands on stdout. The per-step narration
	// ("[dry-run] ...", "Checkpointing WAL...") is diagnostic and goes
	// to stderr, so it cannot interleave with the structured document
	// a --format json caller parses; the harness captures stdout only.
	out, err := db.run("housekeeping", "run", "--dry-run")
	require.NoError(t, err, "housekeeping run --dry-run should succeed")
	assert.Contains(t, out, "Housekeeping Summary")
	assert.NotContains(t, out, "Checkpointing WAL",
		"per-step narration belongs on stderr, not stdout")
}

func TestHousekeepingRunDryRunJSON(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.run("housekeeping", "run", "--dry-run", "--format", "json")
	require.NoError(t, err, "housekeeping run --dry-run --format json should succeed")

	// stdout must be exactly one parseable JSON document: no progress
	// log, no human summary banner.
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got),
		"stdout should be a single JSON document, got: %q", out)

	// A dry run answers with a PLAN, not with the counters a completed
	// pass reports. This assertion used to require db_size_mb,
	// reclaimed_kb and the rest — the summary fields — which described
	// work that by definition had not happened: "jobs_pruned: 0" reads
	// identically whether the pass pruned nothing or was never going to
	// run at all. The plan states dry_run outright so a reviewer can
	// refuse a document that turned out to be a result, and lists the
	// actions so it can approve them individually.
	assert.Equal(t, true, got["dry_run"],
		"a preview must declare itself one in the payload, not only in a banner")

	actions, ok := got["actions"].([]any)
	require.True(t, ok, "the plan must carry an actions array, got: %v", got["actions"])
	assert.Len(t, actions, 6,
		"the full pass documents six mutating steps; the plan must name each one")

	for _, action := range actions {
		entry, ok := action.(map[string]any)
		require.True(t, ok, "each action must be an object, got: %v", action)
		for _, key := range []string{"kind", "target", "count", "reversible"} {
			assert.Contains(t, entry, key,
				"an action a reviewer cannot read is not reviewable")
		}
	}

	// The summary fields must be ABSENT: emitting both shapes would let
	// a caller read a projection as a result.
	for _, key := range []string{"db_size_mb", "reclaimed_kb", "jobs_pruned"} {
		assert.NotContains(t, got, key,
			"a preview must not report the counters of a run that did not happen")
	}
}

func TestHousekeepingRejectsUnknownFormat(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.run("housekeeping", "run", "--dry-run", "--format", "bogus-format")
	require.Error(t, err, "an unknown --format must not succeed")
	// The message has to name the rejected value AND the accepted set,
	// so a caller can correct itself without reading the docs.
	assert.Contains(t, err.Error(), "bogus-format")
	for _, valid := range []string{"json", "table", "yaml"} {
		assert.Contains(t, err.Error(), valid)
	}
}

func TestHousekeepingHelp(t *testing.T) {
	out, err := executeCommand("housekeeping", "--help")
	if err != nil {
		t.Fatalf("housekeeping --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"vacuum", "reindex", "compact", "prune", "run"} {
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
