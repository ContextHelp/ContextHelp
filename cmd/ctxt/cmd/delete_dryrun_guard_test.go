package cmd

import (
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// runDelete sends no DELETE on a dry run even when PreRunE did not answer
// it: with the preview hook removed, --dry-run --confirm=yes must leave
// the store as it was.
func TestRunDeleteDryRunDeletesNothing(t *testing.T) {
	db := setupTestDB(t)
	seedObj(t, db, &storage.KnowledgeObject{ID: "o-guard", Status: "active", Tags: []storage.Tag{{Label: "tmp"}}})

	prev := deleteCmd.PreRunE
	deleteCmd.PreRunE = nil
	t.Cleanup(func() {
		deleteCmd.PreRunE = prev
		// Kit leaves its dry-run mark on the command's context; later
		// tests call previewDelete on deleteCmd directly.
		resetAllFlags(deleteCmd)
	})

	_, err := db.exec("delete", "--id", "o-guard", "--dry-run", "--confirm=yes", "--format", "json")
	if err != nil {
		t.Fatalf("dry-run delete: %v", err)
	}
	if got := storedIDs(t, db); !slices.Contains(got, "o-guard") {
		t.Fatalf("dry run deleted the object; stored = %v", got)
	}
}
