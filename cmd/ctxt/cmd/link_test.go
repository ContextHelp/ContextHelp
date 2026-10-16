package cmd

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"hop.top/kit/go/console/output"
)

func seedTwoObjects(t *testing.T, db *testDB) {
	t.Helper()
	for _, id := range []string{"obj_a", "obj_b"} {
		seedObj(t, db, &storage.KnowledgeObject{ID: id})
	}
}

// storedLinkEdges lists the object-to-object edges leaving obj_a and
// obj_b on the instance as "from type to", sorted.
func storedLinkEdges(t *testing.T, db *testDB) []string {
	t.Helper()
	var out []string
	for _, id := range []string{"obj_a", "obj_b"} {
		edges, err := db.Driver.Edges().ListFrom(context.Background(), "object", id)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range edges {
			if e.ToType == "object" {
				out = append(out, e.FromID+" "+e.EdgeType+" "+e.ToID)
			}
		}
	}
	slices.Sort(out)
	return out
}

// TestLinkCreatesEdgesOverAPI: link create stores the link and its
// inverse on the instance, and never opens a local store.
func TestLinkCreatesEdgesOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)
	local := pointStorageElsewhere(t, db)

	out, err := db.exec("link", "create", "obj_a", "obj_b", "--type", "extends")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if want := "Linked obj_a -[extends]-> obj_b\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := storedLinkEdges(t, db); !slices.Equal(got, []string{"obj_a extends obj_b", "obj_b extended-by obj_a"}) {
		t.Errorf("stored = %v", got)
	}

	if _, err := db.exec("link", "list", "obj_a"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := db.exec("link", "delete", "obj_a", "obj_b"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	assertNoLocalStore(t, local)
}

func TestLinkSymmetricNoDuplicate(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	if _, err := db.exec("link", "create", "obj_a", "obj_b", "--type", "related-to"); err != nil {
		t.Fatalf("link: %v", err)
	}
	if got := storedLinkEdges(t, db); !slices.Equal(got, []string{"obj_a related-to obj_b"}) {
		t.Errorf("symmetric link stored %v; want the forward edge only", got)
	}
}

func TestLinkWithContext(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	if _, err := db.exec("link", "create", "obj_a", "obj_b", "--type", "contradicts", "--context", "newer data"); err != nil {
		t.Fatalf("link with context: %v", err)
	}
	for _, id := range []string{"obj_a", "obj_b"} {
		edges, _ := db.Driver.Edges().ListFrom(context.Background(), "object", id)
		if len(edges) != 1 || edges[0].Metadata["context"] != "newer data" {
			t.Errorf("edges from %s = %+v; want one carrying the context", id, edges)
		}
	}
}

// TestLinkCreateErrors: every refusal exits with its class and stores
// nothing.
func TestLinkCreateErrors(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	cases := []struct {
		name string
		args []string
		exit int
	}{
		{"invalid type", []string{"obj_a", "obj_b", "--type", "imaginary"}, output.ExitUsage},
		{"missing source", []string{"obj_missing", "obj_b", "--type", "extends"}, output.ExitNotFound},
		{"missing target", []string{"obj_a", "obj_missing", "--type", "extends"}, output.ExitNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.exec(append([]string{"link", "create"}, tc.args...)...)
			if exitOf(err) != tc.exit {
				t.Errorf("exit %d (%v), want %d", exitOf(err), err, tc.exit)
			}
		})
	}
	if _, err := db.exec("link", "create", "obj_a", "obj_b"); err == nil {
		t.Error("link create without --type succeeded")
	}
	if got := storedLinkEdges(t, db); len(got) != 0 {
		t.Errorf("refused creates stored %v", got)
	}
}

func TestLinksListsAll(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	if _, err := db.exec("link", "create", "obj_a", "obj_b", "--type", "extends"); err != nil {
		t.Fatal(err)
	}
	out, err := db.exec("link", "list", "obj_a")
	if err != nil {
		t.Fatalf("links: %v", err)
	}
	if want := "  → [extends] obj_b\n  ← [extended-by] obj_b\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestLinksFilterByType(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	for _, typ := range []string{"extends", "supports"} {
		if _, err := db.exec("link", "create", "obj_a", "obj_b", "--type", typ); err != nil {
			t.Fatal(err)
		}
	}
	out, err := db.exec("link", "list", "obj_a", "--type", "extends")
	if err != nil {
		t.Fatalf("links --type: %v", err)
	}
	if want := "  → [extends] obj_b\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestLinksNoLinks(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	out, err := db.exec("link", "list", "obj_a")
	if err != nil {
		t.Fatalf("links: %v", err)
	}
	if !strings.Contains(out, "No links found") {
		t.Errorf("expected no-links message; got %q", out)
	}
	if _, err := db.exec("link", "list", "obj_missing"); exitOf(err) != output.ExitNotFound {
		t.Errorf("unknown object: exit %d (%v), want 3", exitOf(err), err)
	}
}

func TestLinksFollowIsCapped(t *testing.T) {
	db := setupTestDB(t)
	for _, id := range []string{"obj_1", "obj_2", "obj_3", "obj_4", "obj_5"} {
		seedObj(t, db, &storage.KnowledgeObject{ID: id})
	}
	for _, l := range [][2]string{{"obj_1", "obj_2"}, {"obj_2", "obj_3"}, {"obj_3", "obj_4"}, {"obj_4", "obj_5"}} {
		if _, err := db.exec("link", "create", l[0], l[1], "--type", "extends"); err != nil {
			t.Fatal(err)
		}
	}
	// --follow above 3 walks 3 hops, as it always has.
	out, err := db.exec("link", "list", "obj_1", "--follow", "9")
	if err != nil {
		t.Fatalf("links --follow: %v", err)
	}
	if want := "→ [extends] obj_2\n  → [extends] obj_3\n    → [extends] obj_4\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestUnlinkRemovesBothDirections: link delete removes the link and its
// inverse together.
func TestUnlinkRemovesBothDirections(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)
	for _, typ := range []string{"extends", "supports"} {
		if _, err := db.exec("link", "create", "obj_a", "obj_b", "--type", typ); err != nil {
			t.Fatal(err)
		}
	}

	out, err := db.exec("link", "delete", "obj_a", "obj_b")
	if err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if want := "Removed 4 link(s) between obj_a and obj_b\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := storedLinkEdges(t, db); len(got) != 0 {
		t.Errorf("edges left after delete: %v", got)
	}
}

func TestUnlinkNoLinks(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	out, err := db.exec("link", "delete", "obj_a", "obj_b")
	if err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if !strings.Contains(out, "No links found") {
		t.Errorf("expected no-links message; got %q", out)
	}
}

// TestLinkRoles: a writer links but cannot unlink (exit 5, edges stay);
// no token is exit 5; an admin unlinks.
func TestLinkRoles(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	seedTwoObjects(t, db)

	db.useRole(t, dpkmstest.RoleWriter)
	if _, err := db.exec("link", "create", "obj_a", "obj_b", "--type", "extends"); err != nil {
		t.Fatalf("writer create: %v", err)
	}
	if _, err := db.exec("link", "delete", "obj_a", "obj_b"); exitOf(err) != output.ExitUnauthorized {
		t.Errorf("writer delete: exit %d (%v), want 5", exitOf(err), err)
	}
	if got := storedLinkEdges(t, db); len(got) != 2 {
		t.Fatalf("writer delete removed edges: %v", got)
	}

	db.useRole(t, dpkmstest.RoleReader)
	if _, err := db.exec("link", "list", "obj_a"); err != nil {
		t.Errorf("reader list: %v", err)
	}
	if _, err := db.exec("link", "create", "obj_b", "obj_a", "--type", "supports"); exitOf(err) != output.ExitUnauthorized {
		t.Errorf("reader create: exit %d (%v), want 5", exitOf(err), err)
	}

	db.useRole(t, "none")
	for _, args := range [][]string{
		{"link", "create", "obj_a", "obj_b", "--type", "supports"},
		{"link", "list", "obj_a"},
		{"link", "delete", "obj_a", "obj_b"},
	} {
		if _, err := db.exec(args...); exitOf(err) != output.ExitUnauthorized {
			t.Errorf("no token %v: exit %d (%v), want 5", args, exitOf(err), err)
		}
	}

	db.useRole(t, dpkmstest.RoleAdmin)
	if _, err := db.exec("link", "delete", "obj_a", "obj_b"); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
	if got := storedLinkEdges(t, db); len(got) != 0 {
		t.Errorf("admin delete left %v", got)
	}
}

func TestLinkUnreachableIsPrerequisite(t *testing.T) {
	db := setupTestDB(t, dpkmstest.Unreachable())
	for _, args := range [][]string{
		{"link", "create", "obj_a", "obj_b", "--type", "extends"},
		{"link", "list", "obj_a"},
		{"link", "delete", "obj_a", "obj_b"},
	} {
		if _, err := db.exec(args...); exitOf(err) != output.ExitPrerequisite {
			t.Errorf("%v: exit %d (%v), want 70", args, exitOf(err), err)
		}
	}
}

func TestDeleteCascadesLinks(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	if _, err := db.exec("link", "create", "obj_a", "obj_b", "--type", "extends"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.exec("delete", "--id", "obj_a", "--confirm=yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := storedLinkEdges(t, db); len(got) != 0 {
		t.Errorf("edges of a deleted object remain: %v", got)
	}
}

func TestLinkHelp(t *testing.T) {
	out, err := executeCommand("link", "create", "--help")
	if err != nil {
		t.Fatalf("link --help: %v", err)
	}
	for _, kw := range []string{"--type", "extends", "contradicts", "source_id", "--server"} {
		if !strings.Contains(out, kw) {
			t.Errorf("help should mention %q", kw)
		}
	}
}
