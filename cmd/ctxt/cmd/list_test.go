package cmd

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

func TestList(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	for _, id := range []string{"obj_001", "obj_002", "obj_003"} {
		obj := &storage.KnowledgeObject{
			ID:        id,
			Type:      "text",
			Summaries: []string{"Summary for " + id},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed object: %v", err)
		}
	}

	out, err := db.exec("list")
	if err != nil {
		t.Fatalf("list should succeed: %v", err)
	}
	if !strings.Contains(out, "Knowledge Objects") {
		t.Error("output should contain 'Knowledge Objects'")
	}
	if !strings.Contains(out, "obj_001") {
		t.Error("output should contain object IDs")
	}
}

func TestListWithFilters(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_url_1",
		Type:      "url",
		Tags:      []storage.Tag{{Label: "ux"}},
		Mentions:  []uri.URI{{Scheme: "ctxt", Namespace: "entity", ID: "ui/best-practice"}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("list", "--type", "url")
	if err != nil {
		t.Fatalf("list with filters should succeed: %v", err)
	}
	if !strings.Contains(out, "obj_url_1") {
		t.Error("output should contain filtered object")
	}
}

func TestListWithDisplayFlags(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("list", "--limit", "10", "--sort", "recent", "--dir", "asc")
	if err != nil {
		t.Fatalf("list with display flags should succeed: %v", err)
	}
	if !strings.Contains(out, "Knowledge Objects") {
		t.Error("output should contain header")
	}
}

func TestListHelp(t *testing.T) {
	out, err := executeCommand("list", "--help")
	if err != nil {
		t.Fatalf("list --help should succeed: %v", err)
	}
	for _, flag := range []string{"--type", "--tagged", "--mention", "--pipeline", "--subtype", "--q", "--status", "--meta-type", "--topic", "--person", "--source-type", "--since", "--until", "--mentioned-since", "--mentioned-until", "--facets", "--limit", "--start", "--sort", "--dir", "--changed-after", "--cursor", "--advance"} {
		if !strings.Contains(out, flag) {
			t.Errorf("list help should list flag %s", flag)
		}
	}
}

func TestListChangedAfter(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for _, id := range []string{"chg_001", "chg_002"} {
		obj := &storage.KnowledgeObject{ID: id, Type: "text", Status: "active", CreatedAt: now, UpdatedAt: now}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed object: %v", err)
		}
	}

	out, err := db.exec("list", "--changed-after", "0", "--format", "json")
	if err != nil {
		t.Fatalf("list --changed-after 0: %v\n%s", err, out)
	}
	var first struct {
		Objects []struct {
			ID string `json:"id"`
		} `json:"objects"`
		Next int64 `json:"next_changed_after"`
	}
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if len(first.Objects) != 2 || first.Objects[0].ID != "chg_001" || first.Next == 0 {
		t.Fatalf("first page = %+v", first)
	}

	// Update the old object: it lists again past the cursor.
	o, err := db.Driver.Objects().Get(ctx, "chg_001")
	if err != nil {
		t.Fatal(err)
	}
	o.Tags = []storage.Tag{{Label: "edited"}}
	if err := db.Driver.Objects().Update(ctx, o); err != nil {
		t.Fatal(err)
	}
	out, err = db.exec("list", "--changed-after", strconv.FormatInt(first.Next, 10), "--format", "json")
	if err != nil {
		t.Fatalf("list after cursor: %v\n%s", err, out)
	}
	var next struct {
		Objects []struct {
			ID string `json:"id"`
		} `json:"objects"`
	}
	if err := json.Unmarshal([]byte(out), &next); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if len(next.Objects) != 1 || next.Objects[0].ID != "chg_001" {
		t.Fatalf("after the update = %+v, want [chg_001]", next.Objects)
	}

	// The change page composes with a tag selection.
	out, err = db.exec("list", "--changed-after", "0", "--tagged", "edited", "--format", "json")
	if err != nil {
		t.Fatalf("list --changed-after --tagged: %v\n%s", err, out)
	}
	var tagged struct {
		Objects []struct {
			ID string `json:"id"`
		} `json:"objects"`
	}
	if err := json.Unmarshal([]byte(out), &tagged); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if len(tagged.Objects) != 1 || tagged.Objects[0].ID != "chg_001" {
		t.Fatalf("changes tagged edited = %+v, want [chg_001]", tagged.Objects)
	}
}

func TestListChangedAfterUsage(t *testing.T) {
	db := setupTestDB(t)
	for _, args := range [][]string{
		{"list", "--changed-after", "-1"},
		{"list", "--changed-after", "0", "--sort", "updated"},
		{"list", "--changed-after", "0", "--dir", "asc"},
		{"list", "--changed-after", "0", "--start", "1"},
		{"list", "--changed-after", "0", "--cursor", "c", "--advance"},
		{"list", "--changed-after", "0", "--q", "type==url"},
		{"list", "--changed-after", "0", "--facets"},
		// Tags that rank (2+ tags with none required, a plain tag beside a
		// required one) reorder the page.
		{"list", "--changed-after", "0", "--tagged", "a,b"},
		{"list", "--changed-after", "0", "--tagged", "+a,b"},
		{"list", "--changed-after", "0", "--tagged", "+a,b,+c"},
		{"list", "--cursor", "c", "--advance", "--tagged", "a,b"},
		{"list", "--cursor", "c", "--advance", "--tagged", "+a,b"},
	} {
		if _, err := db.exec(args...); err == nil {
			t.Errorf("%v: want a usage error", args)
		}
	}
}

// A fixed-order listing takes a tag selection whose count cannot reorder:
// one tag, or required tags only.
func TestListFixedOrderAcceptsNonRankingTags(t *testing.T) {
	db := setupTestDB(t)
	for _, args := range [][]string{
		{"list", "--changed-after", "0", "--tagged", "a"},
		{"list", "--changed-after", "0", "--tagged", "+a,+b"},
		{"list", "--cursor", "c", "--advance", "--tagged", "a"},
		{"list", "--cursor", "c2", "--advance", "--tagged", "+a,+b"},
	} {
		if out, err := db.exec(args...); err != nil {
			t.Errorf("%v: %v\n%s", args, err, out)
		}
	}
}
