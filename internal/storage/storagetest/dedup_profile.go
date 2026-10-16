package storagetest

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ProfileDedupConformance pins the rule that content-hash dedup stays
// inside a profile (ADR-080 §4, plan decision D22): the same content
// under two profiles is two objects, GetByContentHash looks a hash up
// in one profile, and Reinforce merges into the row of the merge's
// profile only, reporting sql.ErrNoRows when that profile holds none,
// so a capture never writes its tags onto another profile's object. A
// second object with the hash in the same profile is refused with
// storage.ErrAlreadyExists, the error the dedup callers reinforce on;
// objects with no hash never collide.
func ProfileDedupConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	const hash = "sha256:dedup-profile-shared"
	a := searchFixtureObject("dedup-p1", "note", "the same content")
	a.ContentHash, a.ProfileID, a.ReinforcementCount = hash, "p1", 1
	b := searchFixtureObject("dedup-p2", "note", "the same content")
	b.ContentHash, b.ProfileID, b.ReinforcementCount = hash, "p2", 1
	if err := drv.Objects().Create(ctx, a); err != nil {
		t.Fatalf("create %s: %v", a.ID, err)
	}
	if err := drv.Objects().Create(ctx, b); err != nil {
		t.Fatalf("create %s under another profile: %v (the same content under another profile is its own object)", b.ID, err)
	}
	dup := searchFixtureObject("dedup-p1-again", "note", "the same content")
	dup.ContentHash, dup.ProfileID = hash, "p1"
	if err := drv.Objects().Create(ctx, dup); err == nil {
		t.Errorf("a second object with the same hash in the same profile was created")
	} else if !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("Create(same hash, same profile) = %v, want storage.ErrAlreadyExists", err)
	}
	for _, id := range []string{"dedup-nohash-1", "dedup-nohash-2"} {
		o := searchFixtureObject(id, "note", "no hash")
		o.ContentHash, o.ProfileID = "", "p1"
		if err := drv.Objects().Create(ctx, o); err != nil {
			t.Fatalf("create %s without a hash: %v (only non-empty hashes are unique)", id, err)
		}
	}

	t.Run("lookup by hash and profile", func(t *testing.T) { dedupConfLookups(t, drv, hash, a, b) })
	t.Run("reinforce merges into one profile", func(t *testing.T) { dedupConfReinforce(t, drv, hash, a, b) })
}

// dedupConfLookups checks GetByContentHash answers per profile.
func dedupConfLookups(t *testing.T, drv storage.StorageDriver, hash string, a, b *storage.KnowledgeObject) {
	ctx := context.Background()
	for profile, want := range map[string]string{"p1": a.ID, "p2": b.ID, "p3": ""} {
		got, err := drv.Objects().GetByContentHash(ctx, hash, profile)
		if err != nil {
			t.Fatalf("GetByContentHash(%s): %v", profile, err)
		}
		switch {
		case want == "" && got != nil:
			t.Errorf("GetByContentHash(%s) = %s, want nothing", profile, got.ID)
		case want != "" && (got == nil || got.ID != want):
			t.Errorf("GetByContentHash(%s) = %v, want %s", profile, got, want)
		}
	}
}

// dedupConfReinforce checks Reinforce lands on the merge's profile only
// and reports a profile with no row.
func dedupConfReinforce(t *testing.T, drv storage.StorageDriver, hash string, a, b *storage.KnowledgeObject) {
	ctx := context.Background()
	id, err := drv.Objects().Reinforce(ctx, hash, &storage.KnowledgeObject{
		ProfileID: "p2", Tags: []storage.Tag{{Label: "merged-into-p2"}},
	})
	if err != nil || id != b.ID {
		t.Fatalf("Reinforce(p2) = %q, %v, want %s", id, err, b.ID)
	}
	counts := map[string]int{a.ID: 1, b.ID: 2}
	for _, o := range []*storage.KnowledgeObject{a, b} {
		got, err := drv.Objects().Get(ctx, o.ID)
		if err != nil {
			t.Fatalf("Get(%s): %v", o.ID, err)
		}
		tagged := slices.ContainsFunc(got.Tags, func(tag storage.Tag) bool { return tag.Label == "merged-into-p2" })
		if tagged != (o.ID == b.ID) {
			t.Errorf("%s carries the merged tag: %v, want %v", o.ID, tagged, o.ID == b.ID)
		}
		if got.ReinforcementCount != counts[o.ID] {
			t.Errorf("%s reinforcement count = %d, want %d", o.ID, got.ReinforcementCount, counts[o.ID])
		}
	}
	if _, err := drv.Objects().Reinforce(ctx, hash, &storage.KnowledgeObject{ProfileID: "p3"}); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Reinforce(p3) = %v, want sql.ErrNoRows: no object of that profile carries the hash", err)
	}
}
