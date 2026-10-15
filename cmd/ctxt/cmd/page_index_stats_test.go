package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"hop.top/kit/go/console/output"
)

// seedPagesIndexStats stores what the page, index and stats command
// tests read: s-1 (note, topic go, reminder), s-2 (article, inbox,
// topics go and ops), the entity page person.alice built from both, three
// jobs, an entity, two feeds (one active) and resurfacing candidates for
// profiles work (one) and home (two).
func seedPagesIndexStats(t *testing.T, db *testDB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	topics := func(ts ...any) map[string]any {
		return map[string]any{"enrichment.structured_metadata": map[string]any{"topics": ts}}
	}
	for _, o := range []*storage.KnowledgeObject{
		{ID: "s-1", Type: "note", Status: "active", RawContent: "Alice ships Go", Summaries: []string{"Alice ships Go"}, Metadata: topics("go")},
		{ID: "s-2", Type: "article", Status: "inbox", RawContent: "Ops handbook", Summaries: []string{"Ops handbook"}, Metadata: topics("go", "ops")},
		{
			ID: "p-alice-0001", Type: "entity_page", Status: "active", RawContent: "stale alice",
			Metadata: map[string]any{"entity_page": map[string]any{
				"entity_slug": "person.alice", "source_ids": []any{"s-1", "s-2"}, "revision_count": 2, "revision_log": []any{},
			}},
		},
	} {
		o.CreatedAt, o.UpdatedAt = now, now
		if err := db.Driver.Objects().Create(ctx, o); err != nil {
			t.Fatalf("seed %s: %v", o.ID, err)
		}
	}
	if err := db.Driver.Objects().SetReminder(ctx, "s-1", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	seedJob(t, db, "j-1", "analyze", "default", storage.JobPending)
	seedJob(t, db, "j-2", "analyze", "default", storage.JobCompleted)
	seedJob(t, db, "j-3", "analyze", "default", storage.JobCompleted)
	if err := db.Driver.Entities().Upsert(ctx, &storage.Entity{Slug: "ui.cart", Title: "Cart", Namespace: "ui", RegistryURL: "https://registry.test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for id, status := range map[string]string{"f-1": "active", "f-2": "paused"} {
		if err := db.Driver.Feeds().Create(ctx, &storage.Feed{ID: id, URL: "https://" + id + ".example/rss", Status: status, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []*storage.ResurfacingEntry{
		{ID: "rq-1", ObjectID: "s-1", ProfileID: "work", Score: 1},
		{ID: "rq-2", ObjectID: "s-1", ProfileID: "home", Score: 1},
		{ID: "rq-3", ObjectID: "s-2", ProfileID: "home", Score: 1},
	} {
		e.CreatedAt = now
		if err := db.Driver.Resurfacing().Upsert(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
}

// assertStoreNotCreated fails when a command created the store the config
// names.
func assertStoreNotCreated(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a command opened the local store %s: %v", path, err)
	}
}

// page list and show read with a reader token; refresh needs a writer.
// None opens the store the config names.
func TestPageCommandsOverAPI(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	seedPagesIndexStats(t, db)
	db.useRole(t, dpkmstest.RoleAdmin)
	local := detachLocalStore(t, db)

	out, err := db.exec("page", "list")
	if err != nil {
		t.Fatalf("page list: %v", err)
	}
	for _, want := range []string{"Entity Pages (1)", "p-alice-", "person.alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("page list lacks %q:\n%s", want, out)
		}
	}

	out, err = db.exec("page", "list", "--format", "json")
	if err != nil {
		t.Fatalf("page list json: %v", err)
	}
	var pages []storage.KnowledgeObject
	if err := json.Unmarshal([]byte(out), &pages); err != nil || len(pages) != 1 || pages[0].ID != "p-alice-0001" {
		t.Fatalf("page list json = %v (%v):\n%s", pages, err, out)
	}

	out, err = db.exec("page", "show", "person.alice")
	if err != nil {
		t.Fatalf("page show: %v", err)
	}
	for _, want := range []string{"Entity Page: person.alice", "ID:        p-alice-0001", "Sources:   2", "Revisions: 2", "stale alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("page show lacks %q:\n%s", want, out)
		}
	}

	// Entity pages are owner-only (ADR-080 §6): a reader is refused the
	// refresh on scope, a writer on the capability, both the reads; the
	// admin owner refreshes.
	db.useRole(t, dpkmstest.RoleReader)
	_, err = db.exec("page", "refresh", "person.alice")
	assertExit(t, "page refresh as reader", err, output.ExitUnauthorized)
	for _, role := range []string{dpkmstest.RoleReader, dpkmstest.RoleWriter} {
		db.useRole(t, role)
		_, err = db.exec("page", "list")
		assertExit(t, role+" page list", err, output.ExitUnauthorized)
	}
	db.useRole(t, dpkmstest.RoleWriter)
	_, err = db.exec("page", "refresh", "person.alice")
	assertExit(t, "page refresh as writer", err, output.ExitUnauthorized)

	db.useRole(t, dpkmstest.RoleAdmin)
	out, err = db.exec("page", "refresh", "person.alice")
	if err != nil || !strings.Contains(out, "Refreshed entity page: person.alice (ID: p-alice-0001)") {
		t.Fatalf("page refresh as admin: %v\n%s", err, out)
	}
	out, err = db.exec("page", "show", "person.alice")
	if err != nil || !strings.Contains(out, "Compiled from 2 sources.") || !strings.Contains(out, "Ops handbook") {
		t.Fatalf("page show after refresh: %v\n%s", err, out)
	}

	assertStoreNotCreated(t, local)
}

// An unknown slug is NOT_FOUND (3); no token UNAUTHORIZED (5); nothing
// answering PREREQUISITE (70).
func TestPageCommandsErrors(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	seedPagesIndexStats(t, db)

	_, err := db.exec("page", "show", "no.such")
	assertExit(t, "page show no.such", err, output.ExitNotFound)
	_, err = db.exec("page", "refresh", "no.such")
	assertExit(t, "page refresh no.such", err, output.ExitNotFound)

	db.useRole(t, "none")
	for _, args := range [][]string{{"page", "list"}, {"page", "show", "person.alice"}, {"page", "refresh", "person.alice"}} {
		_, err := db.exec(args...)
		assertExit(t, strings.Join(args, " ")+" without a token", err, output.ExitUnauthorized)
	}

	down := setupTestDB(t, dpkmstest.Unreachable())
	_, err = down.exec("page", "list")
	assertExit(t, "page list, nothing answering", err, output.ExitPrerequisite)
}

// index reads for a reader; --refresh needs a writer. Before the first
// rebuild the index is NOT_FOUND with a hint.
func TestIndexCommandOverAPI(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	seedPagesIndexStats(t, db)
	db.useRole(t, dpkmstest.RoleAdmin)
	local := detachLocalStore(t, db)

	_, err := db.exec("index")
	assertExit(t, "index before a rebuild", err, output.ExitNotFound)
	var ke *output.Error
	if errors.As(err, &ke) && !strings.Contains(ke.SuggestedFix, "ctxt index --refresh") {
		t.Errorf("index before a rebuild: fix %q", ke.SuggestedFix)
	}
	_, err = db.exec("index", "go")
	assertExit(t, "index go before a rebuild", err, output.ExitNotFound)

	// The topic index is owner-only (ADR-080 §6): a reader is refused
	// the rebuild on scope, a writer on the capability; the admin owner
	// rebuilds and reads.
	db.useRole(t, dpkmstest.RoleReader)
	_, err = db.exec("index", "--refresh")
	assertExit(t, "index --refresh as reader", err, output.ExitUnauthorized)
	db.useRole(t, dpkmstest.RoleWriter)
	_, err = db.exec("index", "--refresh")
	assertExit(t, "index --refresh as writer", err, output.ExitUnauthorized)
	for _, args := range [][]string{{"index"}, {"index", "go"}} {
		_, err = db.exec(args...)
		assertExit(t, "writer "+strings.Join(args, " "), err, output.ExitUnauthorized)
	}

	db.useRole(t, dpkmstest.RoleAdmin)
	out, err := db.exec("index", "--refresh")
	if err != nil {
		t.Fatalf("index --refresh: %v", err)
	}
	for _, want := range []string{"Topic Index (2 categories, 2 objects)", "## go (2)", "## ops (1)", "[s-2] Ops handbook"} {
		if !strings.Contains(out, want) {
			t.Errorf("index --refresh lacks %q:\n%s", want, out)
		}
	}

	db.useRole(t, dpkmstest.RoleAdmin)
	out, err = db.exec("index", "OP")
	if err != nil || !strings.Contains(out, "Topic: OP (1 entries)") || !strings.Contains(out, "s-2") || strings.Contains(out, "s-1") {
		t.Fatalf("index OP: %v\n%s", err, out)
	}
	out, err = db.exec("index", "rust")
	if err != nil || !strings.Contains(out, `No entries for topic "rust"`) {
		t.Fatalf("index rust: %v\n%s", err, out)
	}
	out, err = db.exec("index", "rust", "--format", "json")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("index rust json: %v\n%s", err, out)
	}
	out, err = db.exec("index", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		Categories []struct {
			Topic string `json:"topic"`
		} `json:"categories"`
		ObjectIDs []string `json:"object_ids"`
	}
	if err := json.Unmarshal([]byte(out), &idx); err != nil || len(idx.Categories) != 2 || len(idx.ObjectIDs) != 2 {
		t.Fatalf("index json = %+v (%v):\n%s", idx, err, out)
	}

	db.useRole(t, "none")
	_, err = db.exec("index")
	assertExit(t, "index without a token", err, output.ExitUnauthorized)

	assertStoreNotCreated(t, local)
}

// stats counts server-side: every object by type, jobs by status, and
// the resurfacing candidates of the active profile, which travels with
// the request. Profiles stay the client's own config.
func TestStatsOverAPI(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	seedPagesIndexStats(t, db)
	db.useRole(t, dpkmstest.RoleAdmin)
	local := detachLocalStore(t, db)
	// The client config: the reader's endpoint plus two profiles, home the
	// default.
	cfg, err := os.ReadFile(db.ClientConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg = append(cfg, []byte("profile:\n  default: home\n  profiles:\n    home: {}\n    work: {}\n")...)
	if err := os.WriteFile(db.ClientConfigPath, cfg, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := db.exec("stats", "--format", "json")
	if err != nil {
		t.Fatalf("stats json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	want := map[string]any{
		"knowledge_objects":      3.0,
		"objects_by_type":        map[string]any{"note": 1.0, "article": 1.0, "entity_page": 1.0},
		"jobs":                   map[string]any{"total": 3.0, "pending": 1.0, "running": 0.0, "completed": 2.0, "failed": 0.0},
		"entities":               1.0,
		"feeds":                  2.0,
		"feeds_active":           1.0,
		"profiles":               2.0,
		"default_profile":        "home",
		"reminders_pending":      1.0,
		"resurfacing_candidates": 2.0,
	}
	for k, v := range want {
		if g, _ := json.Marshal(got[k]); string(g) != mustJSON(t, v) {
			t.Errorf("stats %s = %s, want %s", k, g, mustJSON(t, v))
		}
	}
	if len(got) != len(want) {
		t.Errorf("stats keys = %v", got)
	}

	out, err = db.exec("stats")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	for _, want := range []string{"Knowledge Objects", "3 (", "entity_page: 1", "pending: 1 · running: 0 · completed: 2 · failed: 0",
		"(active: 1)", "(default: home)", "2  candidates"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats lacks %q:\n%s", want, out)
		}
	}

	assertStoreNotCreated(t, local)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// stats and stats --watch fail on the first snapshot: no token is exit
// 5, nothing answering exit 70.
func TestStatsErrors(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithKeys())
	db.useRole(t, "none")
	for _, args := range [][]string{{"stats"}, {"stats", "--watch"}} {
		_, err := db.exec(args...)
		assertExit(t, strings.Join(args, " ")+" without a token", err, output.ExitUnauthorized)
	}
	down := setupTestDB(t, dpkmstest.Unreachable())
	_, err := down.exec("stats")
	assertExit(t, "stats, nothing answering", err, output.ExitPrerequisite)
}

// --server pins the endpoint for page, index and stats.
func TestPageIndexStatsServerFlag(t *testing.T) {
	db := setupTestDB(t)
	seedPagesIndexStats(t, db)
	if err := os.WriteFile(db.ClientConfigPath, []byte("server:\n  url: "+testguard.ClosedServerURL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"page", "show", "person.alice"}, {"page", "list"}, {"page", "refresh", "person.alice"}, {"index", "--refresh"}, {"stats"}} {
		if _, err := db.exec(append(args, "--server", db.Server.URL)...); err != nil {
			t.Errorf("%v --server: %v", args, err)
		}
	}
}
