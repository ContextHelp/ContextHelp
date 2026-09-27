package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
	"hop.top/kit/go/console/output"
)

// Object fixtures for the edit, delete and reprocess commands, which work
// over the API only.

var cmdSeedTime = time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)

// seedObj stores one object through the driver the in-process dpkms
// serves.
func seedObj(t *testing.T, db *testDB, obj *storage.KnowledgeObject) {
	t.Helper()
	if obj.Type == "" {
		obj.Type = "text"
	}
	if obj.CreatedAt.IsZero() {
		obj.CreatedAt, obj.UpdatedAt = cmdSeedTime, cmdSeedTime
	}
	if err := db.Driver.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("seed %s: %v", obj.ID, err)
	}
}

// storedIDs lists every object the instance stores, whatever its status.
func storedIDs(t *testing.T, db *testDB) []string {
	t.Helper()
	objs, _, err := db.Driver.Objects().List(context.Background(), storage.ObjectFilter{Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(objs))
	for _, o := range objs {
		ids = append(ids, o.ID)
	}
	slices.Sort(ids)
	return ids
}

// exitOf returns the kit exit code err carries: 0 for nil, 1 for an
// error without an envelope.
func exitOf(err error) int {
	if err == nil {
		return 0
	}
	var e *output.Error
	if errors.As(err, &e) {
		return e.ExitCode
	}
	return 1
}

// seedDeleteCorpus stores four objects: o-t1 and o-t2 tagged "tmp",
// o-td tagged "tmp" but discarded, o-k tagged "keep" and mentioning
// @project.archived.
func seedDeleteCorpus(t *testing.T, db *testDB) {
	t.Helper()
	seedObj(t, db, &storage.KnowledgeObject{ID: "o-t1", Status: "active", Tags: []storage.Tag{{Label: "tmp"}}})
	seedObj(t, db, &storage.KnowledgeObject{ID: "o-t2", Type: "article", Status: "active", Tags: []storage.Tag{{Label: "tmp"}}})
	seedObj(t, db, &storage.KnowledgeObject{ID: "o-td", Status: "discarded", Tags: []storage.Tag{{Label: "tmp"}}})
	seedObj(t, db, &storage.KnowledgeObject{
		ID: "o-k", Status: "active", Tags: []storage.Tag{{Label: "keep"}},
		Mentions: []uri.URI{{Scheme: "ctxt", Namespace: "entity", ID: "project/archived"}},
	})
}

func TestDeleteByIDOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedDeleteCorpus(t, db)

	out, err := db.exec("delete", "--id", "o-t1", "--confirm=yes")
	if err != nil {
		t.Fatalf("delete --id: %v", err)
	}
	if !strings.Contains(out, "Deleted o-t1") {
		t.Errorf("output = %q", out)
	}
	if got := storedIDs(t, db); !slices.Equal(got, []string{"o-k", "o-t2", "o-td"}) {
		t.Errorf("stored after delete = %v", got)
	}
}

func TestDeleteByFilterOverAPI(t *testing.T) {
	cases := []struct {
		name string
		args []string
		left []string
	}{
		// The filter selects active objects only, as GET /objects does.
		{"tag", []string{"--tagged", "tmp"}, []string{"o-k", "o-td"}},
		{"mention", []string{"--mention", "@project.archived"}, []string{"o-t1", "o-t2", "o-td"}},
		{"type", []string{"--type", "article"}, []string{"o-k", "o-t1", "o-td"}},
		{"all", []string{"--all"}, []string{"o-td"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			seedDeleteCorpus(t, db)
			args := append([]string{"delete", "--confirm=yes"}, tc.args...)
			out, err := db.exec(args...)
			if err != nil {
				t.Fatalf("%v: %v", tc.args, err)
			}
			if !strings.Contains(out, "objects deleted") {
				t.Errorf("output = %q", out)
			}
			if got := storedIDs(t, db); !slices.Equal(got, tc.left) {
				t.Errorf("stored after delete = %v, want %v", got, tc.left)
			}
		})
	}
}

// previewTargets runs a delete preview with --format json and returns the
// object IDs its plan names.
func previewTargets(t *testing.T, db *testDB, args ...string) []string {
	t.Helper()
	out, err := db.exec(append([]string{"delete", "--dry-run", "--format", "json"}, args...)...)
	if err != nil {
		t.Fatalf("preview %v: %v\n%s", args, err, out)
	}
	var plan struct {
		Actions []struct {
			Target string `json:"target"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("decode plan: %v\n%s", err, out)
	}
	ids := []string{}
	for _, a := range plan.Actions {
		ids = append(ids, strings.TrimPrefix(a.Target, "object:"))
	}
	slices.Sort(ids)
	return ids
}

// TestDeletePreviewMatchesExecutedSet: the IDs a preview names are the
// IDs the committed delete removes, for every selector, and the preview
// itself removes nothing.
func TestDeletePreviewMatchesExecutedSet(t *testing.T) {
	for _, args := range [][]string{
		{"--tagged", "tmp"}, {"--mention", "@project.archived"}, {"--all"}, {"--id", "o-t2"}, {"--id", "o-missing"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			db := setupTestDB(t)
			seedDeleteCorpus(t, db)
			before := storedIDs(t, db)

			planned := previewTargets(t, db, args...)
			if got := storedIDs(t, db); !slices.Equal(got, before) {
				t.Fatalf("preview deleted objects: %v -> %v", before, got)
			}
			if _, err := db.exec(append([]string{"delete", "--confirm=yes"}, args...)...); err != nil && exitOf(err) != 3 {
				t.Fatalf("delete %v: %v", args, err)
			}
			var removed []string
			after := storedIDs(t, db)
			for _, id := range before {
				if !slices.Contains(after, id) {
					removed = append(removed, id)
				}
			}
			if removed == nil {
				removed = []string{}
			}
			if !slices.Equal(planned, removed) {
				t.Errorf("preview named %v; delete removed %v", planned, removed)
			}
		})
	}
}

// TestDeleteRefusalNamesTargets: --confirm=no answers with the targets a
// committed run would remove and deletes nothing.
func TestDeleteRefusalNamesTargets(t *testing.T) {
	db := setupTestDB(t)
	seedDeleteCorpus(t, db)
	out, err := db.exec("delete", "--tagged", "tmp", "--confirm=no", "--format", "json")
	if err != nil {
		t.Fatalf("refusal: %v\n%s", err, out)
	}
	var ref struct {
		Targets []string `json:"targets"`
		Matched int      `json:"matched"`
		Applied bool     `json:"applied"`
	}
	if err := json.Unmarshal([]byte(out), &ref); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	slices.Sort(ref.Targets)
	if ref.Applied || ref.Matched != 2 || !slices.Equal(ref.Targets, []string{"o-t1", "o-t2"}) {
		t.Errorf("refusal = %+v", ref)
	}
	if got := storedIDs(t, db); len(got) != 4 {
		t.Errorf("refusal deleted objects: %v", got)
	}
}

// TestDeleteIsAdminOnly: a writer token gets exit 5 and deletes nothing,
// by ID or by filter; a request without a token is exit 5 too; an admin
// deletes.
func TestDeleteIsAdminOnly(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedDeleteCorpus(t, db)

	db.useRole(t, dpkmstest.RoleWriter)
	for _, args := range [][]string{{"--id", "o-t1"}, {"--tagged", "tmp"}} {
		_, err := db.exec(append([]string{"delete", "--confirm=yes"}, args...)...)
		if exitOf(err) != output.ExitUnauthorized {
			t.Errorf("writer delete %v: exit %d (%v), want 5", args, exitOf(err), err)
		}
	}
	// A writer may still preview: it only reads.
	if got := previewTargets(t, db, "--tagged", "tmp"); len(got) != 2 {
		t.Errorf("writer preview = %v", got)
	}
	// A refused delete ends the run: the writer's filter delete sends one
	// DELETE, not one per match.
	var deletes atomic.Int64
	target, err := url.Parse(db.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes.Add(1)
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(counting.Close)
	if _, err := db.exec("delete", "--tagged", "tmp", "--confirm=yes", "--server", counting.URL); exitOf(err) != output.ExitUnauthorized {
		t.Errorf("writer filter delete via proxy: exit %d (%v), want 5", exitOf(err), err)
	}
	if n := deletes.Load(); n != 1 {
		t.Errorf("writer filter delete sent %d DELETE requests; want 1 (stop at the first refusal)", n)
	}

	db.useRole(t, "none")
	if _, err := db.exec("delete", "--id", "o-t1", "--confirm=yes"); exitOf(err) != output.ExitUnauthorized {
		t.Errorf("no token: exit %d (%v), want 5", exitOf(err), err)
	}
	if got := storedIDs(t, db); len(got) != 4 {
		t.Fatalf("unauthorized deletes removed objects: %v", got)
	}

	db.useRole(t, dpkmstest.RoleAdmin)
	if _, err := db.exec("delete", "--id", "o-t1", "--confirm=yes"); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
	if got := storedIDs(t, db); slices.Contains(got, "o-t1") {
		t.Errorf("admin delete left o-t1: %v", got)
	}
}

func TestDeleteMissingIDIsNotFound(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("delete", "--id", "o-missing", "--confirm=yes")
	if exitOf(err) != output.ExitNotFound {
		t.Fatalf("exit %d (%v), want 3", exitOf(err), err)
	}
}

func TestDeleteUnreachableIsPrerequisite(t *testing.T) {
	db := setupTestDB(t, dpkmstest.Unreachable())
	_, err := db.exec("delete", "--id", "o-t1", "--confirm=yes")
	if exitOf(err) != output.ExitPrerequisite {
		t.Fatalf("exit %d (%v), want 70", exitOf(err), err)
	}
}

// TestDeleteNeverOpensLocalStore: with the config's storage path pointing
// somewhere new, delete still acts on the instance and creates no
// database there.
func TestDeleteNeverOpensLocalStore(t *testing.T) {
	db := setupTestDB(t)
	seedDeleteCorpus(t, db)
	local := pointStorageElsewhere(t, db)

	if _, err := db.exec("delete", "--id", "o-t1", "--confirm=yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := db.exec("delete", "--tagged", "tmp", "--dry-run"); err != nil {
		t.Fatalf("preview: %v", err)
	}
	assertNoLocalStore(t, local)
	if got := storedIDs(t, db); slices.Contains(got, "o-t1") {
		t.Errorf("instance still stores o-t1: %v", got)
	}
}

func TestDeleteNoFilterError(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("delete", "--confirm=yes")
	if exitOf(err) != output.ExitUsage {
		t.Errorf("no filter: exit %d (%v), want 2", exitOf(err), err)
	}
}

func TestDeleteHelp(t *testing.T) {
	out, err := executeCommand("delete", "--help")
	if err != nil {
		t.Fatalf("delete --help should succeed: %v", err)
	}
	for _, flag := range []string{"--id", "--tagged", "--mention", "--type", "--all"} {
		if !strings.Contains(out, flag) {
			t.Errorf("delete help should list flag %s", flag)
		}
	}
	// Bound but never applied before; removed rather than ported.
	for _, flag := range []string{"--index", "--hint", "--subtype"} {
		if strings.Contains(out, flag+" ") {
			t.Errorf("delete help still lists the dead flag %s", flag)
		}
	}
}
