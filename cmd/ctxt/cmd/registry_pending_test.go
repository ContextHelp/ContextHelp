package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// pendingFromSync syncs the untrusted test registry "sync-cli" (one
// entity, ai.bert v1) and returns the queued entity's ID.
func pendingFromSync(t *testing.T) (*testDB, string) {
	t.Helper()
	srv, _ := syncTestRegistry(t, true)
	db := setupTestDB(t)
	withRegistry(t, db, "sync-cli", srv.URL, "")
	if out, err := db.exec("registry", "sync", "sync-cli"); err != nil {
		t.Fatalf("registry sync: %v\n%s", err, out)
	}
	return db, storage.PendingEntityID("sync-cli", "ai.bert", "v1")
}

// TestRegistryPendingLists: the queue lists what an untrusted
// registry's sync held back, as a table and as JSON.
func TestRegistryPendingLists(t *testing.T) {
	db, id := pendingFromSync(t)

	out, err := db.exec("registry", "pending")
	if err != nil {
		t.Fatalf("registry pending: %v\n%s", err, out)
	}
	for _, w := range []string{id, "sync-cli", "ai.bert", "unsigned"} {
		if !strings.Contains(out, w) {
			t.Errorf("pending output lacks %q:\n%s", w, out)
		}
	}

	out, err = db.exec("registry", "pending", "--registry", "sync-cli", "--format", "json")
	if err != nil {
		t.Fatalf("registry pending --format json: %v\n%s", err, out)
	}
	var doc struct {
		Total   int `json:"total"`
		Pending []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"pending"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("pending JSON: %v\n%s", err, out)
	}
	if doc.Total != 1 || doc.Pending[0].ID != id || doc.Pending[0].Slug != "ai.bert" {
		t.Errorf("pending JSON = %+v", doc)
	}

	out, err = db.exec("registry", "pending", "--registry", "other")
	if err != nil || !strings.Contains(out, "No entities waiting for approval") {
		t.Errorf("other registry: %v\n%s", err, out)
	}
}

// TestRegistryApproveRejectGatedLikeDelete: approve and reject carry
// the same kit confirmation gate as registry delete (destructive tier
// plus typed token). The gate itself is kit's, installed by Execute and
// proven in confirm_gate_test.go; this package's tree runs ungated.
func TestRegistryApproveRejectGatedLikeDelete(t *testing.T) {
	want := registryRemoveCmd.Annotations["kit/destructive-token"]
	if want == "" {
		t.Fatal("registry delete carries no kit/destructive-token; nothing to be consistent with")
	}
	for _, c := range []*cobra.Command{registryApproveCmd, registryRejectCmd} {
		if got := c.Annotations["kit/destructive-token"]; got != want {
			t.Errorf("%s kit/destructive-token = %q, want %q", c.CommandPath(), got, want)
		}
		if se := c.Annotations["kit/side-effect"]; !strings.HasPrefix(se, "destructive") {
			t.Errorf("%s kit/side-effect = %q, want destructive", c.CommandPath(), se)
		}
	}
	if se := registryPendingCmd.Annotations["kit/side-effect"]; se != "read" {
		t.Errorf("registry pending kit/side-effect = %q, want read", se)
	}
}

// TestRegistryApproveDryRunThenWrites: approve writes nothing under
// --dry-run; a real approve writes the entity and empties the queue.
func TestRegistryApproveDryRunThenWrites(t *testing.T) {
	db, id := pendingFromSync(t)
	ctx := context.Background()
	written := func() bool {
		e, err := db.Driver.Entities().Get(ctx, "ai.bert")
		return err == nil && e != nil
	}

	out, err := db.exec("registry", "approve", id, "--dry-run")
	if err != nil || !strings.Contains(out, "Would be approved: 1") {
		t.Fatalf("approve --dry-run: %v\n%s", err, out)
	}
	if written() {
		t.Fatal("entity written by approve --dry-run")
	}

	out, err = db.exec("registry", "approve", id, "--confirm=yes")
	if err != nil {
		t.Fatalf("approve --confirm=yes: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Entities approved: 1") {
		t.Errorf("approve output:\n%s", out)
	}
	if !written() {
		t.Fatal("approved entity not written")
	}
	if left, _ := db.Driver.PendingEntities().List(ctx, storage.PendingEntityFilter{}); len(left) != 0 {
		t.Errorf("queue after approve = %+v", left)
	}

	if out, err := db.exec("registry", "approve", "--all", "--confirm=yes"); err == nil {
		t.Errorf("approve --all without --registry succeeded:\n%s", out)
	}
}

// TestRegistryApproveAllOfRegistry approves a registry's whole queue.
func TestRegistryApproveAllOfRegistry(t *testing.T) {
	db, _ := pendingFromSync(t)

	out, err := db.exec("registry", "approve", "--all", "--registry", "sync-cli", "--confirm=yes", "--format", "json")
	if err != nil {
		t.Fatalf("approve --all: %v\n%s", err, out)
	}
	var doc struct {
		Total    int `json:"total"`
		Approved []struct {
			Slug string `json:"slug"`
		} `json:"approved"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.Total != 1 || doc.Approved[0].Slug != "ai.bert" {
		t.Errorf("approve --all JSON = %+v (%v):\n%s", doc, err, out)
	}
}

// TestRegistryRejectDrops: reject drops the queued entity unwritten; an
// unknown ID fails.
func TestRegistryRejectDrops(t *testing.T) {
	db, id := pendingFromSync(t)
	ctx := context.Background()

	out, err := db.exec("registry", "reject", id, "--confirm=yes")
	if err != nil || !strings.Contains(out, "Entities rejected: 1") {
		t.Fatalf("reject: %v\n%s", err, out)
	}
	if left, _ := db.Driver.PendingEntities().List(ctx, storage.PendingEntityFilter{}); len(left) != 0 {
		t.Errorf("queue after reject = %+v", left)
	}
	if e, err := db.Driver.Entities().Get(ctx, "ai.bert"); err == nil && e != nil {
		t.Error("rejected entity written")
	}
	if out, err := db.exec("registry", "reject", id, "--confirm=yes"); err == nil {
		t.Errorf("reject of an ID no longer queued succeeded:\n%s", out)
	}
}
