package cmd

import (
	"encoding/csv"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// rowsCase is one list command and the CSV it must produce.
type rowsCase struct {
	name   string
	args   []string
	header []string
	// cell, when set, must appear in some data row.
	cell string
}

// listRowsCases covers every list command whose rows project through
// kit's formatter registry. The seed (seedPagesIndexStats plus two
// linked objects) gives most of them data rows; the rest assert the
// header, which is CSV's contract for an empty result.
func listRowsCases() []rowsCase {
	return []rowsCase{
		{"feed list", []string{"feed", "list"}, []string{"ID", "URL", "Status", "Last Sync"}, "https://f-1.example/rss"},
		{"entity list", []string{"entity", "list"}, []string{"Slug", "Title", "Namespace"}, "ui.cart"},
		{"registry list", []string{"registry", "list"}, []string{"URL", "Last Fetched"}, ""},
		{"audit list", []string{"audit", "list"}, []string{"ID", "Event", "Object", "Actor", "Time"}, ""},
		{"cursor list", []string{"cursor", "list"}, []string{"Name", "LastSeenAt", "LastObjectID", "Updated"}, ""},
		{"inbox list", []string{"inbox", "list"}, []string{"ID", "Type", "Source", "Note", "Created"}, "s-2"},
		{"inbox list --pending", []string{"inbox", "list", "--pending"}, []string{"ID", "Kind", "Status", "Type", "Source", "Pipeline", "Created"}, "j-1"},
		{"instance list", []string{"instance", "list"}, []string{"Name", "Current", "Kind", "URL", "Token", "PID", "DB", "Uptime"}, ""},
		{"page list", []string{"page", "list"}, []string{"ID", "Entity", "Sources", "Updated"}, "p-alice-0001"},
		{"link list", []string{"link", "list", "obj_a"}, []string{"Depth", "From", "Direction", "Type", "Object", "Context"}, "obj_b"},
		{"remind list", []string{"remind", "list"}, []string{"ID", "Title", "Remind At", "Status"}, "s-1"},
		{"watch status", []string{"watch", "status"}, []string{"Path", "Configured", "Watching", "Files", "Last Enqueue", "Last Error"}, ""},
	}
}

func seedListRows(t *testing.T, db *testDB) {
	t.Helper()
	seedPagesIndexStats(t, db)
	seedTwoObjects(t, db)
	if _, err := db.exec("link", "create", "obj_a", "obj_b", "--type", "extends"); err != nil {
		t.Fatalf("link create: %v", err)
	}
}

// TestListCommandsRenderCSV: --format csv yields a parseable CSV document
// whose first record is the command's header, and nothing else (no
// counts, no "Total:" footer, no empty-state prose).
func TestListCommandsRenderCSV(t *testing.T) {
	db := setupTestDB(t)
	seedListRows(t, db)

	for _, tc := range listRowsCases() {
		t.Run(tc.name, func(t *testing.T) {
			out, err := db.exec(append(tc.args, "--format", "csv")...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			recs, err := csv.NewReader(strings.NewReader(out)).ReadAll()
			if err != nil {
				t.Fatalf("not CSV: %v\n%s", err, out)
			}
			if len(recs) == 0 || !slices.Equal(recs[0], tc.header) {
				t.Fatalf("header = %v, want %v\n%s", firstRecord(recs), tc.header, out)
			}
			for _, r := range recs[1:] {
				if len(r) != len(tc.header) {
					t.Fatalf("record %v has %d fields, header has %d", r, len(r), len(tc.header))
				}
			}
			if tc.cell != "" && !anyCell(recs[1:], tc.cell) {
				t.Errorf("no row holds %q:\n%s", tc.cell, out)
			}
		})
	}
}

// TestListCommandsRenderText: --format text is the row view too, never
// the human table with its title line.
func TestListCommandsRenderText(t *testing.T) {
	db := setupTestDB(t)
	seedListRows(t, db)

	for _, tc := range listRowsCases() {
		if tc.cell == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			out, err := db.exec(append(tc.args, "--format", "text")...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			if !strings.Contains(out, tc.cell) {
				t.Errorf("text lacks %q:\n%s", tc.cell, out)
			}
			for _, prose := range []string{"Entities (", "Entity Pages (", "Inbox (", "Reminders (", "Inbox queue ("} {
				if strings.Contains(out, prose) {
					t.Errorf("text carries the human title %q:\n%s", prose, out)
				}
			}
		})
	}
}

// TestLinkListStructured: link list honors json and yaml like the other
// list commands.
func TestLinkListStructured(t *testing.T) {
	db := setupTestDB(t)
	seedListRows(t, db)

	type doc struct {
		Links []struct {
			ObjectID  string `json:"object_id" yaml:"object_id"`
			Direction string `json:"direction" yaml:"direction"`
		} `json:"links" yaml:"links"`
	}
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			out, err := db.exec("link", "list", "obj_a", "--format", format)
			if err != nil {
				t.Fatalf("link list: %v", err)
			}
			var d doc
			if format == "json" {
				err = json.Unmarshal([]byte(out), &d)
			} else {
				err = yaml.Unmarshal([]byte(out), &d)
			}
			if err != nil {
				t.Fatalf("decode %s: %v\n%s", format, err, out)
			}
			if len(d.Links) == 0 || d.Links[0].ObjectID != "obj_b" {
				t.Errorf("links = %+v\n%s", d.Links, out)
			}
		})
	}
}

func firstRecord(recs [][]string) []string {
	if len(recs) == 0 {
		return nil
	}
	return recs[0]
}

func anyCell(recs [][]string, want string) bool {
	for _, r := range recs {
		if slices.Contains(r, want) {
			return true
		}
	}
	return false
}
