package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// linkGoldenGraph is the link fixture of the golden listing: a chain
// obj_1 extends obj_2 supports obj_3 related-to obj_4 contradicts obj_5
// derived-from obj_6, plus obj_7 supersedes obj_3 with a context. From
// obj_1, a depth-3 walk reaches obj_4 and obj_7 but stops before obj_5.
var linkGoldenGraph = [][]string{
	{"obj_1", "obj_2", "--type", "extends"},
	{"obj_2", "obj_3", "--type", "supports"},
	{"obj_3", "obj_4", "--type", "related-to"},
	{"obj_4", "obj_5", "--type", "contradicts"},
	{"obj_5", "obj_6", "--type", "derived-from"},
	{"obj_7", "obj_3", "--type", "supersedes", "--context", "newer research"},
}

// linkGoldenListings are the listings the golden file records, in order.
var linkGoldenListings = [][]string{
	{"obj_1", "--follow", "3"},
	{"obj_3", "--follow", "3"},
	{"obj_3"},
	{"obj_3", "--type", "supported-by"},
	{"obj_7", "--follow", "2", "--type", "supersedes"},
	{"obj_6", "--follow", "1"},
}

// TestLinkListGolden: link list output, with and without --follow, for a
// fixed graph. The golden was recorded from the local-store
// implementation, so the API-backed listing prints exactly what it did.
func TestLinkListGolden(t *testing.T) {
	db := setupTestDB(t)
	for i := 1; i <= 7; i++ {
		seedObj(t, db, &storage.KnowledgeObject{ID: fmt.Sprintf("obj_%d", i)})
	}
	for _, l := range linkGoldenGraph {
		if _, err := db.exec(append([]string{"link", "create"}, l...)...); err != nil {
			t.Fatalf("link create %v: %v", l, err)
		}
	}

	var got strings.Builder
	for _, args := range linkGoldenListings {
		out, err := db.exec(append([]string{"link", "list"}, args...)...)
		if err != nil {
			t.Fatalf("link list %v: %v", args, err)
		}
		got.WriteString("$ ctxt link list " + strings.Join(args, " ") + "\n" + out)
	}

	golden := filepath.Join("testdata", "link_list.golden")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Errorf("link list output differs from %s\n--- got\n%s--- want\n%s", golden, got.String(), want)
	}
}
