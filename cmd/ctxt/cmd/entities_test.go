package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"hop.top/kit/go/console/output"
)

// seedEntityCommands stores what the entity command tests read: two
// "checkout" entities, one entity with an alias, and an object that
// mentions it.
func seedEntityCommands(t *testing.T, db *testDB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for _, e := range []*storage.Entity{
		{Slug: "ui.checkout-flow", Title: "Checkout Flow", Namespace: "ui"},
		{Slug: "ui.cart", Title: "Shopping Cart", Namespace: "ui", Description: "Where items wait.", Aliases: []string{"basket"}},
		{Slug: "zz.late", Title: "Late CHECKOUT page", Namespace: "zz"},
	} {
		e.CreatedAt, e.UpdatedAt = now, now
		if err := db.Driver.Entities().Upsert(ctx, e); err != nil {
			t.Fatalf("seed %s: %v", e.Slug, err)
		}
	}
	if err := db.Driver.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj_cartnote", Type: "note", TextContent: "cart page", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	if err := db.Driver.Edges().Create(ctx, &storage.Edge{
		ID: "edge_cart", FromType: "object", FromID: "obj_cartnote", ToType: "entity", ToID: "ctxt://entity/ui/cart",
		EdgeType: "mentions", Weight: 1, CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed edge: %v", err)
	}
}

// detachLocalStore points the ctxt config's storage path at a database
// file that does not exist yet, so any command that still opened the
// store would create it. Returns the path.
func detachLocalStore(t *testing.T, db *testDB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	if err := os.WriteFile(db.ConfigPath, []byte("storage:\n  type: sqlite\n  path: "+path+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func jsonSlugs(t *testing.T, out string) []string {
	t.Helper()
	var es []storage.Entity
	if err := json.Unmarshal([]byte(out), &es); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	slugs := make([]string, 0, len(es))
	for _, e := range es {
		slugs = append(slugs, e.Slug)
	}
	return slugs
}

// Every entity leaf answers from the in-process dpkms with a reader token
// and never opens the store the config names.
func TestEntityCommandsOverAPI(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedEntityCommands(t, db)
	db.useRole(t, dpkmstest.RoleReader)
	local := detachLocalStore(t, db)

	out, err := db.exec("entity", "list")
	if err != nil {
		t.Fatalf("entity list: %v", err)
	}
	for _, want := range []string{"Entities (3)", "ui.cart", "ui.checkout-flow", "zz.late"} {
		if !strings.Contains(out, want) {
			t.Errorf("entity list lacks %q:\n%s", want, out)
		}
	}

	out, err = db.exec("entity", "list", "--namespace", "zz", "--format", "json")
	if err != nil {
		t.Fatalf("entity list --namespace: %v", err)
	}
	if got := jsonSlugs(t, out); strings.Join(got, ",") != "zz.late" {
		t.Errorf("entity list --namespace zz = %v", got)
	}

	out, err = db.exec("entity", "list", "--limit", "1", "--format", "json")
	if err != nil {
		t.Fatalf("entity list --limit: %v", err)
	}
	if got := jsonSlugs(t, out); strings.Join(got, ",") != "ui.cart" {
		t.Errorf("entity list --limit 1 = %v", got)
	}

	out, err = db.exec("entity", "search", "checkout", "--format", "json")
	if err != nil {
		t.Fatalf("entity search: %v", err)
	}
	if got := jsonSlugs(t, out); strings.Join(got, ",") != "ui.checkout-flow,zz.late" {
		t.Errorf("entity search checkout = %v", got)
	}

	out, err = db.exec("entity", "search", "BASKET")
	if err != nil {
		t.Fatalf("entity search by alias: %v", err)
	}
	if !strings.Contains(out, `Entity search: "BASKET" (1 results)`) || !strings.Contains(out, "ui.cart") {
		t.Errorf("entity search BASKET:\n%s", out)
	}

	out, err = db.exec("entity", "show", "ui.cart")
	if err != nil {
		t.Fatalf("entity show: %v", err)
	}
	for _, want := range []string{"Entity: ui.cart", "Shopping Cart", "Where items wait.", "- basket"} {
		if !strings.Contains(out, want) {
			t.Errorf("entity show lacks %q:\n%s", want, out)
		}
	}

	out, err = db.exec("entity", "backlink", "ui.cart")
	if err != nil {
		t.Fatalf("entity backlink: %v", err)
	}
	if !strings.Contains(out, "Backlinks for ui.cart (1 objects)") || !strings.Contains(out, "obj_cartnote") {
		t.Errorf("entity backlink:\n%s", out)
	}

	if _, err := os.Stat(local); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("entity commands opened the local store %s: %v", local, err)
	}
}

// A slug that names nothing is NOT_FOUND (exit 3); a missing token is
// UNAUTHORIZED (exit 5); nothing answering is PREREQUISITE (exit 70).
func TestEntityCommandsErrors(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedEntityCommands(t, db)

	_, err := db.exec("entity", "show", "no.such")
	assertExit(t, "entity show no.such", err, output.ExitNotFound)

	db.useRole(t, "none")
	for _, args := range [][]string{
		{"entity", "list"}, {"entity", "search", "cart"}, {"entity", "show", "ui.cart"}, {"entity", "backlink", "ui.cart"},
	} {
		_, err := db.exec(args...)
		assertExit(t, strings.Join(args, " ")+" without a token", err, output.ExitUnauthorized)
	}

	down := setupTestDB(t, dpkmstest.Unreachable())
	_, err = down.exec("entity", "list")
	assertExit(t, "entity list, nothing answering", err, output.ExitPrerequisite)
}

// --server pins the endpoint for the entity leaves too.
func TestEntityCommandsServerFlag(t *testing.T) {
	db := setupTestDB(t)
	seedEntityCommands(t, db)
	db.useRole(t, dpkmstest.RoleAdmin)
	// Route the config at a closed port; only --server reaches the data.
	if err := os.WriteFile(db.ClientConfigPath, []byte("server:\n  url: "+testguard.ClosedServerURL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := db.exec("entity", "show", "ui.cart", "--server", db.Server.URL)
	if err != nil || !strings.Contains(out, "Entity: ui.cart") {
		t.Fatalf("entity show --server: %v\n%s", err, out)
	}
}

func assertExit(t *testing.T, what string, err error, want int) {
	t.Helper()
	var ke *output.Error
	if !errors.As(err, &ke) || ke.ExitCode != want {
		t.Errorf("%s: err = %v; want exit %d", what, err, want)
	}
}

func TestEntitiesShowNoSlugError(t *testing.T) {
	_, err := executeCommand("entity", "show")
	if err == nil {
		t.Error("entity show without slug should fail")
	}
}

func TestEntitiesSearchNoQueryError(t *testing.T) {
	_, err := executeCommand("entity", "search")
	if err == nil {
		t.Error("entity search without query should fail")
	}
}

func TestEntitiesBacklinksNoSlugError(t *testing.T) {
	_, err := executeCommand("entity", "backlink")
	if err == nil {
		t.Error("entity backlink without slug should fail")
	}
}

func TestEntitiesHelp(t *testing.T) {
	out, err := executeCommand("entity", "--help")
	if err != nil {
		t.Fatalf("entity --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"list", "show", "search", "backlink"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("entity help should list subcommand %q", subcmd)
		}
	}
}
