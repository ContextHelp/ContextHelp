package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func seedExportObject(t *testing.T, db *testDB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:        "obj_exp001",
		Type:      "url",
		Source:    "https://example.com/post",
		Summaries: []string{"An exportable knowledge object."},
		Tags:      []storage.Tag{{Label: "export"}, {Label: "test"}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed export object: %v", err)
	}
}

// TestExport_JSONFallback verifies ctxt export with no --format returns JSON.
func TestExport_JSONFallback(t *testing.T) {
	db := setupTestDB(t)
	seedExportObject(t, db)

	// No --format requested: kit v0.5 defaults --format to "table", which
	// export treats as "no generator plugin" and falls back to JSON.
	out, err := db.exec("export", "obj_exp001")
	if err != nil {
		t.Fatalf("export JSON should succeed: %v", err)
	}
	if !strings.Contains(out, `"id"`) {
		t.Error("JSON output should contain id field")
	}
}

// TestExport_UnknownFormat verifies ctxt export --format <unknown> returns an error.
func TestExport_UnknownFormat(t *testing.T) {
	db := setupTestDB(t)
	seedExportObject(t, db)

	_, err := db.exec("export", "obj_exp001", "--format", "no-such-format")
	if err == nil {
		t.Error("unknown format should produce an error")
	}
}

// TestExport_Help verifies the export command has --format and --dest flags.
func TestExport_Help(t *testing.T) {
	out, err := executeCommand("export", "--help")
	if err != nil {
		t.Fatalf("export --help should succeed: %v", err)
	}
	if !strings.Contains(out, "--format") {
		t.Error("export help should list --format flag")
	}
	if !strings.Contains(out, "--dest") {
		t.Error("export help should list --dest flag")
	}
}
