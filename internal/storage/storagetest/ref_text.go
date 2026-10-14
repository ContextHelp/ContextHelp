package storagetest

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// RefTextConformance pins storage.RefTextStore, the repair of objects
// whose text_content is a blob:// ref (storage copied the ref into an
// empty text_content before projection v3): ListRefText finds exactly
// those rows, and ReplaceRefText swaps the ref for text, flags it a
// prefix when asked, and marks the row's projection stale, so the
// re-projection re-indexes it. pdb reads the stored bodies back.
func RefTextConformance(t *testing.T, drv storage.StorageDriver, pdb ProjectionDB) {
	t.Helper()
	c := refTextCase{drv: drv, pdb: pdb}
	var ok bool
	if c.store, ok = drv.Objects().(storage.RefTextStore); !ok {
		t.Fatalf("%T does not implement storage.RefTextStore", drv.Objects())
	}
	if c.proj, ok = drv.Objects().(storage.ProjectionStore); !ok {
		t.Fatalf("%T does not implement storage.ProjectionStore", drv.Objects())
	}
	c.seed(t, "reftext-a", "blob://reftexta", "blob://reftexta")
	c.seed(t, "reftext-b", "blob://reftextb", "blob://reftextb")
	c.seed(t, "reftext-ok", "blob://reftextok", "a real prefix")
	c.seed(t, "reftext-inline", "inline body", "inline body")

	t.Run("list", c.testList)
	t.Run("replace", c.testReplace)
	t.Run("unflagged", c.testUnflagged)
	t.Run("text changed since listed", c.testChanged)
	t.Run("none left", func(t *testing.T) {
		if got := c.list(t, "", 10); len(got) != 0 {
			t.Errorf("ListRefText after repair = %v, want none", got)
		}
	})
}

type refTextCase struct {
	drv   storage.StorageDriver
	pdb   ProjectionDB
	store storage.RefTextStore
	proj  storage.ProjectionStore
}

// seed stores an object with text as its text_content. Create never
// stores a ref as text; Update stores text as given, which is how rows
// written before v3 look.
func (c refTextCase) seed(t *testing.T, id, raw, text string) {
	t.Helper()
	ctx := context.Background()
	obj := &storage.KnowledgeObject{ID: id, Type: "note", RawContent: raw,
		Metadata: map[string]any{"keep": "me"}, CreatedAt: fixtureTime(), UpdatedAt: fixtureTime()}
	if err := c.drv.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
	obj.TextContent = text
	if err := c.drv.Objects().Update(ctx, obj); err != nil {
		t.Fatalf("Update %s: %v", id, err)
	}
}

func (c refTextCase) list(t *testing.T, after string, limit int) []string {
	t.Helper()
	objs, err := c.store.ListRefText(context.Background(), after, limit)
	if err != nil {
		t.Fatalf("ListRefText: %v", err)
	}
	ids := make([]string, 0, len(objs))
	for _, o := range objs {
		ids = append(ids, o.ID)
		if o.RawContent == "" || !blobref.Is(o.TextContent) {
			t.Errorf("ListRefText %s: raw %q text %q, want the stored fields", o.ID, o.RawContent, o.TextContent)
		}
	}
	return ids
}

func (c refTextCase) get(t *testing.T, id string) *storage.KnowledgeObject {
	t.Helper()
	got, err := c.drv.Objects().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	return got
}

func (c refTextCase) testList(t *testing.T) {
	if got := c.list(t, "", 10); len(got) != 2 || got[0] != "reftext-a" || got[1] != "reftext-b" {
		t.Errorf("ListRefText = %v, want [reftext-a reftext-b]", got)
	}
	if got := c.list(t, "reftext-a", 10); len(got) != 1 || got[0] != "reftext-b" {
		t.Errorf("ListRefText after reftext-a = %v, want [reftext-b]", got)
	}
	if got := c.list(t, "", 1); len(got) != 1 {
		t.Errorf("ListRefText limit 1 = %v", got)
	}
}

func (c refTextCase) testReplace(t *testing.T) {
	ctx := context.Background()
	wrote, err := c.store.ReplaceRefText(ctx, "reftext-a", "blob://reftexta", "wallaby prefix", true)
	if err != nil || !wrote {
		t.Fatalf("ReplaceRefText = %v, %v", wrote, err)
	}
	got := c.get(t, "reftext-a")
	if got.TextContent != "wallaby prefix" {
		t.Errorf("TextContent = %q, want the repaired text", got.TextContent)
	}
	if got.Metadata[blobref.MetaTextTruncated] != true || got.Metadata["keep"] != "me" {
		t.Errorf("Metadata = %v, want the flag set and other keys kept", got.Metadata)
	}
	if !got.UpdatedAt.Equal(fixtureTime()) {
		t.Errorf("UpdatedAt = %v: a repair is not an edit", got.UpdatedAt)
	}
	stale, err := c.proj.ListStaleProjections(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListStaleProjections: %v", err)
	}
	if len(stale) != 1 || stale[0] != "reftext-a" {
		t.Errorf("stale = %v, want [reftext-a]: the repaired row must be re-projected", stale)
	}
	if _, err := c.proj.Reproject(ctx, "reftext-a"); err != nil {
		t.Fatalf("Reproject: %v", err)
	}
	if body := c.pdb.Body(t, "reftext-a"); body != "wallaby prefix" {
		t.Errorf("re-projected body = %q, want the repaired text", body)
	}
	AssertFTS(t, c.drv, "wallaby", "[reftext-a]")
}

func (c refTextCase) testUnflagged(t *testing.T) {
	if _, err := c.store.ReplaceRefText(context.Background(), "reftext-b", "blob://reftextb", "whole text", false); err != nil {
		t.Fatalf("ReplaceRefText: %v", err)
	}
	got := c.get(t, "reftext-b")
	if _, set := got.Metadata[blobref.MetaTextTruncated]; set || got.TextContent != "whole text" {
		t.Errorf("TextContent %q, Metadata %v: want the text, no flag", got.TextContent, got.Metadata)
	}
}

func (c refTextCase) testChanged(t *testing.T) {
	wrote, err := c.store.ReplaceRefText(context.Background(), "reftext-ok", "blob://reftextok", "clobbered", true)
	if err != nil || wrote {
		t.Errorf("ReplaceRefText over non-ref text = %v, %v; want false, nil", wrote, err)
	}
	if got := c.get(t, "reftext-ok"); got.TextContent != "a real prefix" {
		t.Errorf("TextContent = %q: a repair overwrote text that is no longer the ref", got.TextContent)
	}
}
