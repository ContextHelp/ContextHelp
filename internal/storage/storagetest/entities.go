package storagetest

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ThinEntityConformance pins the thin-entity round trip every driver must
// honour: a row stored by UpsertThin (registry thin sync, mentions) reads
// back through Get, List and Resolve with empty metadata, and a later full
// Upsert over it replaces it. A thin row whose stored metadata is not a JSON
// object fails every entity read, so any search graph that mentions it fails.
func ThinEntityConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	entities := drv.Entities()

	thin := &storage.Entity{
		Slug:        "thin.mentioned",
		Title:       "Mentioned",
		Namespace:   "thin",
		Aliases:     []string{"thin-alias"},
		VersionHash: "v1",
		RegistryURL: "https://registry.example/thin",
		CreatedAt:   fixtureTime(),
		UpdatedAt:   fixtureTime(),
	}
	if err := entities.UpsertThin(ctx, thin); err != nil {
		t.Fatalf("UpsertThin: %v", err)
	}

	t.Run("Get", func(t *testing.T) {
		got, err := entities.Get(ctx, thin.Slug)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		assertThinEntity(t, got)
	})

	t.Run("List", func(t *testing.T) {
		got, err := entities.List(ctx, storage.EntityFilter{ContentStatus: storage.ContentStatusThin})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("List(thin) returned %d entities, want 1", len(got))
		}
		assertThinEntity(t, got[0])
	})

	t.Run("ResolveByAlias", func(t *testing.T) {
		got, err := entities.Resolve(ctx, "thin-alias")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		assertThinEntity(t, got)
	})

	t.Run("FullUpsertOverThin", func(t *testing.T) {
		full := *thin
		full.Description = "Now pulled in full"
		full.Metadata = map[string]any{"source": "pull"}
		full.ContentStatus = storage.ContentStatusFull
		if err := entities.Upsert(ctx, &full); err != nil {
			t.Fatalf("Upsert over thin: %v", err)
		}
		got, err := entities.Get(ctx, thin.Slug)
		if err != nil {
			t.Fatalf("Get after full Upsert: %v", err)
		}
		if got.ContentStatus != storage.ContentStatusFull {
			t.Errorf("ContentStatus = %q, want %q", got.ContentStatus, storage.ContentStatusFull)
		}
		if got.Description != full.Description {
			t.Errorf("Description = %q, want %q", got.Description, full.Description)
		}
		if got.Metadata["source"] != "pull" || len(got.Metadata) != 1 {
			t.Errorf("Metadata = %v, want map[source:pull]", got.Metadata)
		}

		// A thin re-sync never clobbers the full record.
		if err := entities.UpsertThin(ctx, thin); err != nil {
			t.Fatalf("UpsertThin over full: %v", err)
		}
		got, err = entities.Get(ctx, thin.Slug)
		if err != nil {
			t.Fatalf("Get after thin re-sync: %v", err)
		}
		if got.ContentStatus != storage.ContentStatusFull || got.Metadata["source"] != "pull" {
			t.Errorf("thin re-sync clobbered full record: status=%q metadata=%v", got.ContentStatus, got.Metadata)
		}
	})
}

func assertThinEntity(t *testing.T, got *storage.Entity) {
	t.Helper()
	if got.Slug != "thin.mentioned" || got.Title != "Mentioned" || got.Namespace != "thin" {
		t.Errorf("entity = %s/%q/%s, want thin.mentioned/\"Mentioned\"/thin", got.Slug, got.Title, got.Namespace)
	}
	if got.ContentStatus != storage.ContentStatusThin {
		t.Errorf("ContentStatus = %q, want %q", got.ContentStatus, storage.ContentStatusThin)
	}
	if len(got.Metadata) != 0 {
		t.Errorf("Metadata = %v, want empty", got.Metadata)
	}
	if got.VersionHash != "v1" || got.RegistryURL != "https://registry.example/thin" {
		t.Errorf("provenance = %q/%q, want v1/https://registry.example/thin", got.VersionHash, got.RegistryURL)
	}
}

// EntityQueryConformance pins EntityFilter.Query and EntityStore.Resolve
// across drivers.
//
// Query is a case-insensitive (ASCII) substring match over the slug, the
// title and every alias, applied in the store before Limit, so a match
// sorted after any number of non-matching entities is still found. LIKE
// metacharacters in the query match literally. Resolve finds an entity by
// exact slug, else by exact alias, and reports storage.ErrNotFound for
// anything else, however the mention is spelled.
func EntityQueryConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	es := drv.Entities()
	seed := []*storage.Entity{
		{Slug: "aa.filler-1", Title: "Filler one", Namespace: "aa"},
		{Slug: "aa.filler-2", Title: "Filler two", Namespace: "aa"},
		{Slug: "ui.checkout-flow", Title: "Checkout Flow", Namespace: "ui"},
		{Slug: "ui.cart", Title: "Shopping Cart", Namespace: "ui", Aliases: []string{"Basket", "trolley"}},
		{Slug: "ops.rate_limit", Title: "100% uptime", Namespace: "ops"},
		{Slug: "zz.late-match", Title: "Zebra CHECKOUT page", Namespace: "zz"},
	}
	for _, e := range seed {
		e.CreatedAt, e.UpdatedAt = fixtureTime(), fixtureTime()
		if err := es.Upsert(ctx, e); err != nil {
			t.Fatalf("seed %s: %v", e.Slug, err)
		}
	}
	// An entity stored without aliases must not break the alias match.
	if err := es.UpsertThin(ctx, &storage.Entity{
		Slug: "zz.thin", Title: "Thin stub", Namespace: "zz",
		CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
	}); err != nil {
		t.Fatalf("seed thin: %v", err)
	}

	slugs := func(es []*storage.Entity) []string {
		out := make([]string, 0, len(es))
		for _, e := range es {
			out = append(out, e.Slug)
		}
		return out
	}

	queries := []struct {
		name   string
		filter storage.EntityFilter
		want   []string
	}{
		{"slug substring", storage.EntityFilter{Query: "cart"}, []string{"ui.cart"}},
		{"title substring, any case", storage.EntityFilter{Query: "checkout"}, []string{"ui.checkout-flow", "zz.late-match"}},
		{"upper-case query", storage.EntityFilter{Query: "SHOPPING"}, []string{"ui.cart"}},
		{"alias substring, any case", storage.EntityFilter{Query: "basK"}, []string{"ui.cart"}},
		{"second alias", storage.EntityFilter{Query: "trolley"}, []string{"ui.cart"}},
		{"percent is literal", storage.EntityFilter{Query: "100%"}, []string{"ops.rate_limit"}},
		{"lone percent is literal", storage.EntityFilter{Query: "%"}, []string{"ops.rate_limit"}},
		{"underscore is literal", storage.EntityFilter{Query: "_"}, []string{"ops.rate_limit"}},
		{"no match", storage.EntityFilter{Query: "nothing-like-this"}, nil},
		{"query with namespace", storage.EntityFilter{Query: "checkout", Namespace: "zz"}, []string{"zz.late-match"}},
		{"limit applies after the match", storage.EntityFilter{Query: "checkout", Limit: 1}, []string{"ui.checkout-flow"}},
		{"offset applies after the match", storage.EntityFilter{Query: "checkout", Limit: 1, Offset: 1}, []string{"zz.late-match"}},
		{"match past non-matching rows", storage.EntityFilter{Query: "zebra", Limit: 1}, []string{"zz.late-match"}},
		{"thin stub by title", storage.EntityFilter{Query: "stub"}, []string{"zz.thin"}},
	}
	for _, q := range queries {
		t.Run("query/"+q.name, func(t *testing.T) {
			got, err := es.List(ctx, q.filter)
			if err != nil {
				t.Fatalf("List(%+v): %v", q.filter, err)
			}
			if g := slugs(got); !equalStrings(g, q.want) {
				t.Fatalf("List(%+v) = %v; want %v", q.filter, g, q.want)
			}
		})
	}

	t.Run("empty query lists everything", func(t *testing.T) {
		got, err := es.List(ctx, storage.EntityFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(seed)+1 {
			t.Fatalf("List() = %v; want all %d entities", slugs(got), len(seed)+1)
		}
	})

	resolves := []struct {
		mention, want string
	}{
		{"ui.cart", "ui.cart"},
		{"Basket", "ui.cart"},
		{"trolley", "ui.cart"},
	}
	for _, r := range resolves {
		t.Run("resolve/"+r.mention, func(t *testing.T) {
			got, err := es.Resolve(ctx, r.mention)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", r.mention, err)
			}
			if got.Slug != r.want {
				t.Fatalf("Resolve(%q) = %s; want %s", r.mention, got.Slug, r.want)
			}
		})
	}
	// Resolve is exact: a substring, a different case, or a mention that
	// needs quoting in any encoding names nothing.
	for _, mention := range []string{"cart", "basket", "no.such", `quo"te`, "back\\slash", "ctl\x7f", "tab\tbed", ""} {
		t.Run("resolve miss/"+mention, func(t *testing.T) {
			got, err := es.Resolve(ctx, mention)
			if !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("Resolve(%q) = %v, %v; want storage.ErrNotFound", mention, got, err)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
