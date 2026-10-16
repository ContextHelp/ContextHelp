package service

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	registrysync "github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// pendingSvc is a service over the SQLite database at path with one
// registry "pub" at url, trust level trust, full sync mode.
func pendingSvc(t *testing.T, path, url string, trust config.RegistryTrustLevel) (*Service, storage.StorageDriver) {
	t.Helper()
	driver, err := sqlite.New(path, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := driver.Init(ownerCtx()); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	t.Cleanup(func() { driver.Close(ownerCtx()) })
	cfg := config.Config{Registries: []config.RegistryConfig{{
		Name: "pub", URL: url, SyncMode: config.RegistrySyncModeFull, TrustLevel: trust,
	}}}
	return NewWithOptions(driver, jobs.NewQueue(driver.Jobs()), pipeline.DefaultRegistry(),
		search.NewEngine(driver), "", nil, nil, cfg), driver
}

// queuedFromUntrusted syncs an untrusted "pub" serving ai.bert and
// ai.gpt and returns the service and the database path.
func queuedFromUntrusted(t *testing.T) (*Service, string, string) {
	t.Helper()
	url := indexRegistry(t,
		registrysync.EntityIndexEntry{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
		registrysync.EntityIndexEntry{Slug: "ai.gpt", Title: "GPT", Namespace: "ai", VersionHash: "v1"},
	)
	path := filepath.Join(t.TempDir(), "pending.db")
	svc, _ := pendingSvc(t, path, url, config.RegistryTrustLevelUntrusted)
	res, err := svc.SyncRegistryEntities(ownerCtx(), url)
	if err != nil {
		t.Fatalf("SyncRegistryEntities: %v", err)
	}
	if res.PendingApproval != 2 {
		t.Fatalf("PendingApproval = %d, want 2", res.PendingApproval)
	}
	return svc, path, url
}

func idOf(slug string) string { return storage.PendingEntityID("pub", slug, "v1") }

// TestPendingQueue_PersistsAcrossServices: what one service queued, a
// new service over the same database still lists.
func TestPendingQueue_PersistsAcrossServices(t *testing.T) {
	first, path, url := queuedFromUntrusted(t)
	if err := first.store.Close(ownerCtx()); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	svc, _ := pendingSvc(t, path, url, config.RegistryTrustLevelUntrusted)
	got, err := svc.ListPendingEntities(ownerCtx(), "pub")
	if err != nil {
		t.Fatalf("ListPendingEntities: %v", err)
	}
	if len(got) != 2 || got[0].Slug != "ai.bert" || got[1].Slug != "ai.gpt" ||
		got[0].SignatureStatus != storage.SignatureUnsigned || got[0].RegistryURL != url {
		t.Fatalf("queue in a new service = %+v", got)
	}
	if other, _ := svc.ListPendingEntities(ownerCtx(), "other"); len(other) != 0 {
		t.Errorf("registry filter leaked: %+v", other)
	}
}

// TestApprovePending_WritesAndDequeues: approve writes the entity with
// provenance and only the approved one leaves the queue.
func TestApprovePending_WritesAndDequeues(t *testing.T) {
	svc, _, url := queuedFromUntrusted(t)
	ctx := ownerCtx()

	done, err := svc.ApprovePendingEntities(ctx, PendingSelection{IDs: []string{idOf("ai.bert")}})
	if err != nil || len(done) != 1 {
		t.Fatalf("ApprovePendingEntities = %d, %v", len(done), err)
	}
	e, err := svc.store.Entities().Get(ctx, "ai.bert")
	if err != nil {
		t.Fatalf("approved entity not written: %v", err)
	}
	if e.RegistryURL != url || e.Metadata[registrysync.ProvenanceKey] != "approved from pub" {
		t.Errorf("written = %+v", e)
	}
	left, _ := svc.ListPendingEntities(ctx, "")
	if len(left) != 1 || left[0].Slug != "ai.gpt" {
		t.Errorf("queue after approve = %+v, want ai.gpt only", left)
	}
	if ent, err := svc.store.Entities().Get(ctx, "ai.gpt"); err == nil && ent != nil {
		t.Errorf("unapproved ai.gpt written")
	}

	// The registry still serves ai.bert v1: the next sync does not
	// queue what was approved, only what still waits.
	res, err := svc.SyncRegistryEntities(ctx, url)
	if err != nil {
		t.Fatalf("re-sync: %v", err)
	}
	left, _ = svc.ListPendingEntities(ctx, "")
	if res.PendingApproval != 1 || len(left) != 1 || left[0].Slug != "ai.gpt" {
		t.Errorf("after re-sync: pending %d, queue %+v; want ai.gpt only", res.PendingApproval, left)
	}
}

// TestApprovePending_AllOfRegistry approves every queued entity of one
// registry; --all without a registry is refused.
func TestApprovePending_AllOfRegistry(t *testing.T) {
	svc, _, _ := queuedFromUntrusted(t)
	ctx := ownerCtx()

	if _, err := svc.ApprovePendingEntities(ctx, PendingSelection{All: true}); !errors.Is(err, ErrPendingSelection) {
		t.Errorf("all without registry: err = %v, want ErrPendingSelection", err)
	}
	done, err := svc.ApprovePendingEntities(ctx, PendingSelection{All: true, Registry: "pub"})
	if err != nil || len(done) != 2 {
		t.Fatalf("approve all = %d, %v; want 2", len(done), err)
	}
	for _, slug := range []string{"ai.bert", "ai.gpt"} {
		if _, err := svc.store.Entities().Get(ctx, slug); err != nil {
			t.Errorf("%s not written: %v", slug, err)
		}
	}
	if left, _ := svc.ListPendingEntities(ctx, ""); len(left) != 0 {
		t.Errorf("queue after approve all = %+v", left)
	}
}

// TestApprovePending_UnknownIDWritesNothing: one unknown ID fails the
// whole approval before anything is written.
func TestApprovePending_UnknownIDWritesNothing(t *testing.T) {
	svc, _, _ := queuedFromUntrusted(t)
	ctx := ownerCtx()

	_, err := svc.ApprovePendingEntities(ctx, PendingSelection{IDs: []string{idOf("ai.bert"), "nope"}})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if e, err := svc.store.Entities().Get(ctx, "ai.bert"); err == nil && e != nil {
		t.Errorf("ai.bert written despite the failed selection")
	}
	if left, _ := svc.ListPendingEntities(ctx, ""); len(left) != 2 {
		t.Errorf("queue = %d, want 2 untouched", len(left))
	}
}

// TestApprovePending_RegistryNowSandboxed: a registry switched to
// sandboxed after its entities were queued cannot have them approved.
func TestApprovePending_RegistryNowSandboxed(t *testing.T) {
	first, path, url := queuedFromUntrusted(t)
	_ = first.store.Close(ownerCtx())
	svc, _ := pendingSvc(t, path, url, config.RegistryTrustLevelSandboxed)
	ctx := ownerCtx()

	_, err := svc.ApprovePendingEntities(ctx, PendingSelection{IDs: []string{idOf("ai.bert")}})
	if !errors.Is(err, ErrPendingSandboxed) {
		t.Fatalf("err = %v, want ErrPendingSandboxed", err)
	}
	if e, err := svc.store.Entities().Get(ctx, "ai.bert"); err == nil && e != nil {
		t.Errorf("sandboxed registry's entity written on approve")
	}
}

// TestRejectPending_DequeuesWithoutWriting.
func TestRejectPending_DequeuesWithoutWriting(t *testing.T) {
	svc, _, _ := queuedFromUntrusted(t)
	ctx := ownerCtx()

	done, err := svc.RejectPendingEntities(ctx, PendingSelection{IDs: []string{idOf("ai.gpt")}})
	if err != nil || len(done) != 1 {
		t.Fatalf("RejectPendingEntities = %d, %v", len(done), err)
	}
	left, _ := svc.ListPendingEntities(ctx, "")
	if len(left) != 1 || left[0].Slug != "ai.bert" {
		t.Errorf("queue after reject = %+v, want ai.bert only", left)
	}
	if e, err := svc.store.Entities().Get(ctx, "ai.gpt"); err == nil && e != nil {
		t.Errorf("rejected entity written")
	}
	if _, err := svc.RejectPendingEntities(ctx, PendingSelection{IDs: []string{idOf("ai.gpt")}}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("reject twice: err = %v, want ErrNotFound", err)
	}
}

// TestSandboxedSyncQueuesNothing: a sandboxed registry's entities are
// blocked, never queued for approval.
func TestSandboxedSyncQueuesNothing(t *testing.T) {
	url := indexRegistry(t, registrysync.EntityIndexEntry{Slug: "ai.t5", Title: "T5", Namespace: "ai", VersionHash: "v1"})
	svc, _ := pendingSvc(t, filepath.Join(t.TempDir(), "box.db"), url, config.RegistryTrustLevelSandboxed)
	ctx := ownerCtx()

	res, err := svc.SyncRegistryEntities(ctx, url)
	if err != nil || res.Blocked != 1 {
		t.Fatalf("SyncRegistryEntities = %+v, %v; want 1 blocked", res, err)
	}
	if got, _ := svc.ListPendingEntities(ctx, ""); len(got) != 0 {
		t.Errorf("sandboxed registry queued %+v", got)
	}
}
