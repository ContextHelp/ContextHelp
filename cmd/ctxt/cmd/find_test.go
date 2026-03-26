package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

func rebuildFTSForTest(t *testing.T, db *testDB) {
	t.Helper()
	d, ok := db.Driver.(*sqlite.Driver)
	if !ok {
		t.Fatal("driver must be *sqlite.Driver")
	}
	if _, err := d.DB().ExecContext(context.Background(), "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')"); err != nil {
		t.Fatalf("fts rebuild: %v", err)
	}
}

func TestFind(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_find_1",
		Type:      "text",
		Summaries: []string{"authentication best practices for web apps"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	rebuildFTSForTest(t, db)

	out, err := db.exec("find", "authentication")
	if err != nil {
		t.Fatalf("find should succeed: %v", err)
	}
	if !strings.Contains(out, "Search [") {
		t.Error("output should contain search header")
	}
	if !strings.Contains(out, "obj_find_1") {
		t.Error("output should contain matching object")
	}
}

func TestFindWithLimit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	for i := 0; i < 3; i++ {
		obj := &storage.KnowledgeObject{
			ID:         "obj_onboard_" + string(rune('a'+i)),
			Type:       "text",
			Summaries:  []string{"onboarding flow design patterns"},
			RawContent: "onboarding details",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed object: %v", err)
		}
	}

	rebuildFTSForTest(t, db)

	out, err := db.exec("find", "onboarding", "--limit", "2")
	if err != nil {
		t.Fatalf("find with --limit should succeed: %v", err)
	}
	if !strings.Contains(out, "Search [") {
		t.Error("output should contain search header")
	}
}

func TestFindNoResults(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("find", "nonexistent_query_xyz")
	if err != nil {
		t.Fatalf("find with no results should succeed: %v", err)
	}
	if !strings.Contains(out, "0 results") {
		t.Error("output should indicate zero results")
	}
}

func TestFindNoQueryError(t *testing.T) {
	_, err := executeCommand("find")
	if err == nil {
		t.Error("find without query should fail")
	}
}

func TestFindHelp(t *testing.T) {
	out, err := executeCommand("find", "--help")
	if err != nil {
		t.Fatalf("find --help should succeed: %v", err)
	}
	if !strings.Contains(out, "--limit") {
		t.Error("find help should list --limit flag")
	}
	if !strings.Contains(out, "search") {
		t.Error("find help should describe search functionality")
	}
}

func TestFindCmd_HybridFlag(t *testing.T) {
	f := findCmd.Flags().Lookup("hybrid")
	if f == nil {
		t.Fatal("--hybrid flag must be registered")
	}
}

func TestFindCmd_FlagOverrides(t *testing.T) {
	for _, name := range []string{"rrf-k", "fts-weight", "vector-weight", "fts-pool", "vector-pool", "min-score"} {
		if f := findCmd.Flags().Lookup(name); f == nil {
			t.Errorf("--%s flag must be registered", name)
		}
	}
}

// TestFindDidYouMean seeds an object with summary "authentication best practices",
// then runs a query whose first word is "auth" but returns no direct results.
// The prefix query "auth *" should match the seeded object and surface a hint.
func TestFindDidYouMean(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_dym_1",
		Type:      "text",
		Summaries: []string{"authentication best practices"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	rebuildFTSForTest(t, db)

	// "auth noresult_suffix_xyz" → 0 direct results; prefix "auth *" hits obj.
	out, err := db.exec("find", "auth noresult_suffix_xyz")
	if err != nil {
		t.Fatalf("find with did-you-mean should succeed: %v", err)
	}
	if !strings.Contains(out, "0 results") {
		t.Error("output should indicate zero results")
	}
	if !strings.Contains(out, "Did you mean") {
		t.Error("output should contain 'Did you mean' suggestions")
	}
}

// TestFindNoResultsNoDYM verifies no "Did you mean" block when prefix also has no matches.
func TestFindNoResultsNoDYM(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("find", "zzznomatch")
	if err != nil {
		t.Fatalf("find with no results should succeed: %v", err)
	}
	if !strings.Contains(out, "0 results") {
		t.Error("output should indicate zero results")
	}
	if strings.Contains(out, "Did you mean") {
		t.Error("should not print 'Did you mean' when there are no prefix matches")
	}
}
