package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func pendingFixture(registry, slug, version string, seen time.Time) *storage.PendingEntity {
	e := &storage.Entity{
		Slug: slug, Title: "Title " + slug, Namespace: "ai", VersionHash: version,
		RegistryURL: "https://" + registry + ".example", CreatedAt: seen, UpdatedAt: seen,
	}
	return &storage.PendingEntity{
		ID:           storage.PendingEntityID(registry, slug, version),
		RegistryName: registry, RegistryURL: e.RegistryURL,
		Slug: slug, Namespace: e.Namespace, Title: e.Title, Version: version,
		Entity: e, SignatureStatus: storage.SignatureUnsigned,
		FirstSeenAt: seen, LastSeenAt: seen,
	}
}

// PendingEntityConformance pins the approval queue contract every driver
// must honour: a queued entity round-trips with its payload; queuing it
// again keeps first_seen and moves last_seen; a new version from the same
// registry replaces the old one; listing filters by registry; Get and
// Delete of an unknown ID wrap ErrNotFound; DeleteEntity drops every
// version of one registry's slug.
func PendingEntityConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	q := drv.PendingEntities()
	pendingRoundTrip(t, q)
	ids := pendingVersionsAndList(t, q)
	pendingDeletes(t, q, ids)
}

// pendingRoundTrip queues a/ai.bert v1 twice: the payload round-trips,
// and the second queueing keeps first_seen and moves last_seen.
func pendingRoundTrip(t *testing.T, q storage.PendingEntityStore) {
	t.Helper()
	ctx := context.Background()
	t0 := fixtureTime()
	t1 := t0.Add(time.Hour)

	a := pendingFixture("a", "ai.bert", "v1", t0)
	if err := q.Upsert(ctx, a); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := q.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RegistryName != "a" || got.Slug != "ai.bert" || got.Version != "v1" ||
		got.SignatureStatus != storage.SignatureUnsigned || got.Entity == nil ||
		got.Entity.Title != "Title ai.bert" || got.Entity.RegistryURL != "https://a.example" ||
		!got.FirstSeenAt.Equal(t0) || !got.LastSeenAt.Equal(t0) {
		t.Fatalf("Get = %+v (entity %+v)", got, got.Entity)
	}

	again := pendingFixture("a", "ai.bert", "v1", t1)
	again.SignatureStatus = storage.SignatureVerified
	if err := q.Upsert(ctx, again); err != nil {
		t.Fatalf("Upsert again: %v", err)
	}
	got, err = q.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get after re-queue: %v", err)
	}
	if !got.FirstSeenAt.Equal(t0) || !got.LastSeenAt.Equal(t1) || got.SignatureStatus != storage.SignatureVerified {
		t.Errorf("re-queued: first %v last %v sig %q, want first %v last %v verified",
			got.FirstSeenAt, got.LastSeenAt, got.SignatureStatus, t0, t1)
	}
}

// pendingVersionsAndList queues a/ai.bert v2 (replacing v1), b/ai.bert
// and b/ai.gpt, and checks listing order and the registry filter. It
// returns the three queued IDs in list order.
func pendingVersionsAndList(t *testing.T, q storage.PendingEntityStore) []string {
	t.Helper()
	ctx := context.Background()
	t1 := fixtureTime().Add(time.Hour)
	v2 := pendingFixture("a", "ai.bert", "v2", t1)
	b := pendingFixture("b", "ai.bert", "v1", t1)
	c := pendingFixture("b", "ai.gpt", "v1", t1)
	for _, p := range []*storage.PendingEntity{v2, b, c} {
		if err := q.Upsert(ctx, p); err != nil {
			t.Fatalf("Upsert %s/%s: %v", p.RegistryName, p.Slug, err)
		}
	}
	v1ID := storage.PendingEntityID("a", "ai.bert", "v1")
	if _, err := q.Get(ctx, v1ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("v1 after v2 queued: err = %v, want ErrNotFound (superseded)", err)
	}

	want := []string{v2.ID, b.ID, c.ID}
	assertPendingList(t, q, "", want)
	assertPendingList(t, q, "b", want[1:])
	assertPendingList(t, q, "zzz", []string{})
	return want
}

// pendingDeletes removes b/ai.bert by ID (twice: the second wraps
// ErrNotFound) and every version of a/ai.bert, leaving b/ai.gpt.
func pendingDeletes(t *testing.T, q storage.PendingEntityStore, ids []string) {
	t.Helper()
	ctx := context.Background()
	if err := q.Delete(ctx, ids[1]); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := q.Delete(ctx, ids[1]); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Delete twice: err = %v, want ErrNotFound", err)
	}
	if n, err := q.DeleteEntity(ctx, "a", "ai.bert"); err != nil || n != 1 {
		t.Errorf("DeleteEntity(a, ai.bert) = %d, %v; want 1", n, err)
	}
	assertPendingList(t, q, "", ids[2:])
}

// assertPendingList checks the IDs List returns for registry, in order;
// an empty listing must be a non-nil slice.
func assertPendingList(t *testing.T, q storage.PendingEntityStore, registry string, want []string) {
	t.Helper()
	got, err := q.List(context.Background(), storage.PendingEntityFilter{RegistryName: registry})
	if err != nil {
		t.Fatalf("List %q: %v", registry, err)
	}
	if got == nil {
		t.Errorf("List %q = nil, want a non-nil slice", registry)
	}
	ids := pendingIDs(got)
	if len(ids) != len(want) {
		t.Errorf("List %q = %v, want %v", registry, ids, want)
		return
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("List %q = %v, want %v (registry, slug order)", registry, ids, want)
			return
		}
	}
}

func pendingIDs(ps []*storage.PendingEntity) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}
