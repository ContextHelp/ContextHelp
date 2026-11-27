package storage_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestNodeID_Roundtrip(t *testing.T) {
	id := storage.NewNodeID("obj-abc", storage.NodeTypeSection, 0)
	parsed, err := storage.ParseNodeID(id)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ObjectID != "obj-abc" {
		t.Errorf("got %q", parsed.ObjectID)
	}
	if parsed.NodeType != storage.NodeTypeSection {
		t.Errorf("got %q", parsed.NodeType)
	}
	if parsed.Ordinal != 0 {
		t.Errorf("got %d", parsed.Ordinal)
	}
}

func TestNodeURI_Format(t *testing.T) {
	u := storage.NodeURI("obj-abc", storage.NodeTypeSection, 0)
	if u != "ctxt:node/obj-abc/section/0" {
		t.Errorf("got %q", u)
	}
}

func TestObjectNodeURI_ParseRoundtrip(t *testing.T) {
	raw := "ctxt:node/obj-abc/tag/2"
	ref, err := storage.ParseNodeURI(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ref.ObjectID != "obj-abc" || ref.NodeType != storage.NodeTypeTag || ref.Ordinal != 2 {
		t.Errorf("unexpected ref %+v", ref)
	}
}

func TestNewNodeID_PanicsOnSlashInObjectID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for objectID with '/'")
		}
	}()
	storage.NewNodeID("org/repo", storage.NodeTypeSection, 0)
}

func TestNewNodeID_PanicsOnEmptyObjectID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty objectID")
		}
	}()
	storage.NewNodeID("", storage.NodeTypeSection, 0)
}

func TestNewNodeID_PanicsOnNegativeOrdinal(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative ordinal")
		}
	}()
	storage.NewNodeID("obj-1", storage.NodeTypeSection, -1)
}

func TestParseNodeID_MalformedInput(t *testing.T) {
	_, err := storage.ParseNodeID("only-two/parts")
	if err == nil {
		t.Fatal("expected error for malformed input")
	}
}

func TestParseNodeURI_MissingPrefix(t *testing.T) {
	_, err := storage.ParseNodeURI("node/obj/section/0")
	if err == nil {
		t.Fatal("expected error for missing ctxt:node/ prefix")
	}
}

func TestFreeOrdinal(t *testing.T) {
	sec := func(ids ...string) *storage.ObjectGraph {
		g := &storage.ObjectGraph{}
		for _, id := range ids {
			g.Nodes = append(g.Nodes, storage.GraphNode{ID: id, NodeType: storage.NodeTypeSection})
		}
		return g
	}
	cases := []struct {
		name string
		g    *storage.ObjectGraph
		typ  string
		from int
		want int
	}{
		{"nil graph", nil, storage.NodeTypeSection, 0, 0},
		{"empty graph", &storage.ObjectGraph{}, storage.NodeTypeSection, 3, 3},
		{"lowest free is a gap, not max+1", sec("o/section/0", "o/section/2", "o/section/90"), storage.NodeTypeSection, 0, 1},
		{"from skips held ordinals", sec("o/section/1", "o/section/2"), storage.NodeTypeSection, 1, 3},
		{"from above the highest", sec("o/section/0"), storage.NodeTypeSection, 5, 5},
		{"another type does not count", sec("o/section/0"), storage.NodeTypeTag, 0, 0},
		{"artifact 0 is reserved", nil, storage.NodeTypeArtifact, 0, 1},
		{"artifact skips the original and held", &storage.ObjectGraph{Nodes: []storage.GraphNode{{ID: "o/artifact/1", NodeType: storage.NodeTypeArtifact}}}, storage.NodeTypeArtifact, 0, 2},
	}
	for _, c := range cases {
		if got := storage.FreeOrdinal(c.g, "o", c.typ, c.from); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}

func TestGraphJSONMarked(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want bool
	}{
		"marked":        {`{"stamped":true}`, true},
		"marked, nodes": {`{"nodes":[{"id":"a","node_type":"tag","order":0}],"stamped":true}`, true},
		"plain":         {`{"nodes":[]}`, false},
		"false":         {`{"stamped":false}`, false},
		"empty":         {``, false},
		"malformed":     {`{"stamped":`, false},
		"box":           {`{"v":"v1","ct":"AAAA"}`, false},
	} {
		if got := storage.GraphJSONMarked([]byte(tc.raw)); got != tc.want {
			t.Errorf("%s: got %v want %v", name, got, tc.want)
		}
	}
}

func TestKeepStoredMark(t *testing.T) {
	plain := &storage.ObjectGraph{Nodes: []storage.GraphNode{{ID: "a"}}}
	if got := storage.KeepStoredMark(plain, true); !got.Stamped || len(got.Nodes) != 1 {
		t.Errorf("stored mark not kept: %+v", got)
	}
	if plain.Stamped {
		t.Error("the caller's graph was changed")
	}
	if got := storage.KeepStoredMark(plain, false); got != plain || got.Stamped {
		t.Errorf("an unmarked store marked the graph: %+v", got)
	}
	marked := &storage.ObjectGraph{Stamped: true}
	if got := storage.KeepStoredMark(marked, false); got != marked || !got.Stamped {
		t.Errorf("a client mark was lost: %+v", got)
	}
	if got := storage.KeepStoredMark(nil, true); got != nil {
		t.Errorf("nil graph: %+v", got)
	}
}
