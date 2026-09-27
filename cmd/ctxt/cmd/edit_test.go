package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
	"hop.top/kit/go/console/output"
)

// pointStorageElsewhere rewrites the test config's storage path to a
// fresh location the instance never uses, and returns it. A command that
// still opened the local store would create a database there.
func pointStorageElsewhere(t *testing.T, db *testDB) string {
	t.Helper()
	local := filepath.Join(t.TempDir(), "local", "ctxt.db")
	body := fmt.Sprintf("storage:\n  type: sqlite\n  path: %s\n", local)
	if err := os.WriteFile(db.ConfigPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return local
}

func assertNoLocalStore(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a local database exists at %s (stat: %v); the command opened the store", path, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the local data directory %s was created", filepath.Dir(path))
	}
}

func seedEditObject(t *testing.T, db *testDB) {
	t.Helper()
	seedObj(t, db, &storage.KnowledgeObject{
		ID: "obj_123", Summaries: []string{"Old title", "second"},
		Tags:     []storage.Tag{{Label: "old", Source: "auto"}},
		Mentions: []uri.URI{{Scheme: "ctxt", Namespace: "entity", ID: "acme/api"}},
	})
}

func storedObj(t *testing.T, db *testDB, id string) *storage.KnowledgeObject {
	t.Helper()
	obj, err := db.Driver.Objects().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return obj
}

// TestEditEachFlagOverAPI: every edit flag reaches the instance and lands
// on the stored object.
func TestEditEachFlagOverAPI(t *testing.T) {
	cases := []struct {
		flag, value string
		check       func(t *testing.T, obj *storage.KnowledgeObject)
	}{
		{"--title", "New title", func(t *testing.T, obj *storage.KnowledgeObject) {
			if !slices.Equal(obj.Summaries, []string{"New title"}) {
				t.Errorf("summaries = %v", obj.Summaries)
			}
		}},
		{"--summary", "New summary", func(t *testing.T, obj *storage.KnowledgeObject) {
			if !slices.Equal(obj.Summaries, []string{"New summary", "second"}) {
				t.Errorf("summaries = %v", obj.Summaries)
			}
		}},
		{"--tags", "ux, onboarding,critical", func(t *testing.T, obj *storage.KnowledgeObject) {
			var labels []string
			for _, tg := range obj.Tags {
				labels = append(labels, tg.Label)
				if tg.Source != "manual" {
					t.Errorf("tag %s source = %q", tg.Label, tg.Source)
				}
			}
			if !slices.Equal(labels, []string{"ux", "onboarding", "critical"}) {
				t.Errorf("tags = %v", labels)
			}
		}},
		{"--mention", "@ui.best-practice @ux.onboarding", func(t *testing.T, obj *storage.KnowledgeObject) {
			var ms []string
			for i := range obj.Mentions {
				ms = append(ms, obj.Mentions[i].String())
			}
			if !slices.Equal(ms, []string{"ctxt://entity/ui/best-practice", "ctxt://entity/ux/onboarding"}) {
				t.Errorf("mentions = %v", ms)
			}
		}},
		{"--subtype", "article", func(t *testing.T, obj *storage.KnowledgeObject) {
			if obj.Subtype != "article" {
				t.Errorf("subtype = %q", obj.Subtype)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			db := setupTestDB(t)
			seedEditObject(t, db)
			out, err := db.exec("edit", "obj_123", tc.flag, tc.value)
			if err != nil {
				t.Fatalf("edit %s: %v", tc.flag, err)
			}
			if !strings.Contains(out, "Updated obj_123") {
				t.Errorf("output = %q", out)
			}
			tc.check(t, storedObj(t, db, "obj_123"))
		})
	}
}

func TestEditMultipleFields(t *testing.T) {
	db := setupTestDB(t)
	seedEditObject(t, db)
	out, err := db.exec("edit", "obj_123", "--title", "New title", "--tags", "ux,design", "--subtype", "article")
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	for _, want := range []string{"Updated obj_123", "title: New title", "tags: ux,design", "subtype: article"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	obj := storedObj(t, db, "obj_123")
	if obj.Subtype != "article" || len(obj.Tags) != 2 || obj.Summaries[0] != "New title" {
		t.Errorf("stored = %+v", obj)
	}
}

// TestEditNeverOpensLocalStore: edit acts on the instance and creates no
// database at the config's storage path.
func TestEditNeverOpensLocalStore(t *testing.T) {
	db := setupTestDB(t)
	seedEditObject(t, db)
	local := pointStorageElsewhere(t, db)
	if _, err := db.exec("edit", "obj_123", "--subtype", "article"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	assertNoLocalStore(t, local)
	if got := storedObj(t, db, "obj_123").Subtype; got != "article" {
		t.Errorf("instance subtype = %q", got)
	}
}

// TestEditRoles: a writer edits; a reader and a request without a token
// get exit 5 and change nothing.
func TestEditRoles(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedEditObject(t, db)

	for _, role := range []string{dpkmstest.RoleReader, "none"} {
		db.useRole(t, role)
		_, err := db.exec("edit", "obj_123", "--subtype", "denied")
		if exitOf(err) != output.ExitUnauthorized {
			t.Errorf("%s: exit %d (%v), want 5", role, exitOf(err), err)
		}
	}
	db.useRole(t, dpkmstest.RoleWriter)
	if _, err := db.exec("edit", "obj_123", "--subtype", "article"); err != nil {
		t.Fatalf("writer: %v", err)
	}
	if got := storedObj(t, db, "obj_123").Subtype; got != "article" {
		t.Errorf("subtype = %q", got)
	}
}

func TestEditErrors(t *testing.T) {
	db := setupTestDB(t)
	seedEditObject(t, db)
	cases := []struct {
		name string
		args []string
		exit int
	}{
		{"no field", []string{"edit", "obj_123"}, output.ExitUsage},
		{"title with summary", []string{"edit", "obj_123", "--title", "a", "--summary", "b"}, output.ExitUsage},
		{"bad mention", []string{"edit", "obj_123", "--mention", "@"}, output.ExitUsage},
		{"missing object", []string{"edit", "obj_missing", "--title", "x"}, output.ExitNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.exec(tc.args...)
			if exitOf(err) != tc.exit {
				t.Errorf("exit %d (%v), want %d", exitOf(err), err, tc.exit)
			}
		})
	}
	if got := storedObj(t, db, "obj_123").Summaries; !slices.Equal(got, []string{"Old title", "second"}) {
		t.Errorf("a failed edit changed the object: %v", got)
	}
}

func TestEditMissingIDError(t *testing.T) {
	_, err := executeCommand("edit", "--title", "New title")
	if err == nil {
		t.Error("edit without positional id should fail")
	}
}

func TestEditHelp(t *testing.T) {
	out, err := executeCommand("edit", "--help")
	if err != nil {
		t.Fatalf("edit --help should succeed: %v", err)
	}
	for _, flag := range []string{"--title", "--summary", "--tags", "--mention", "--subtype"} {
		if !strings.Contains(out, flag) {
			t.Errorf("edit help should list flag %s", flag)
		}
	}
	// Bound but never applied before; removed rather than ported.
	for _, flag := range []string{"--hints", "--decisions"} {
		if strings.Contains(out, flag+" ") {
			t.Errorf("edit help still lists the dead flag %s", flag)
		}
	}
}
