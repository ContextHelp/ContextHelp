package ingest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// graphAdapter emits one object with a graph whose node ids are unset
// and a note node classed personal, as the contact adapter does, and
// one plain object.
type graphAdapter struct{}

func (graphAdapter) Name() string { return "graphed" }

func (graphAdapter) Fetch(context.Context) ([]Object, error) {
	note := storage.GraphNode{NodeType: storage.NodeTypeSection, Label: "note", Content: "private", Order: 1}
	storage.SetNodeClasses(&note, []string{storage.ClassPersonal})
	return []Object{{
		ID: "one", Type: "contact", Content: "Sarah Lee", Metadata: map[string]any{"name": "Sarah Lee"},
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{NodeType: storage.NodeTypeSection, Label: "card", Content: "Sarah Lee", Order: 0},
			note,
		}},
	}, {ID: "two", Type: "contact", Content: "Bob"}}, nil
}

// graphRun runs graphAdapter through a runner whose classifier tags
// Sarah vip, and returns the store and the two stored objects.
func graphRun(t *testing.T) (storage.StorageDriver, *Runner, *storage.KnowledgeObject, *storage.KnowledgeObject) {
	t.Helper()
	drv := storageutil.NewTestDriver(t)
	ctx := context.Background()
	r := NewRunner(drv.Objects(), WithClassifier(func(o *storage.KnowledgeObject) []string {
		if o.Type == "contact" && o.Metadata["name"] == "Sarah Lee" {
			return []string{"vip", "vip"}
		}
		return nil
	}))
	res, err := r.Run(ctx, graphAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 {
		t.Fatalf("created %d, want 2", res.Created)
	}
	objs, _, err := drv.Objects().List(ctx, storage.ObjectFilter{Status: storage.ObjectStatusAll})
	if err != nil {
		t.Fatal(err)
	}
	var sarah, bob *storage.KnowledgeObject
	for _, o := range objs {
		switch o.RawContent {
		case "Sarah Lee":
			sarah = o
		case "Bob":
			bob = o
		}
	}
	if sarah == nil || bob == nil {
		t.Fatalf("stored %d objects, want Sarah and Bob", len(objs))
	}
	return drv, r, sarah, bob
}

// The runner runs the classifier over every stored object and
// normalizes what it returns.
func TestRunner_StoresClasses(t *testing.T) {
	_, _, sarah, bob := graphRun(t)
	if !slices.Equal(sarah.Classes, []string{"vip"}) {
		t.Errorf("sarah classes = %v, want vip", sarah.Classes)
	}
	if bob.Classes != nil {
		t.Errorf("bob classes = %v, want none", bob.Classes)
	}
}

// The runner stores the adapter's graph with node ids minted from the
// object id and keeps the note's class; an object without a graph gets
// none.
func TestRunner_StoresGraphWithMintedNodeIDs(t *testing.T) {
	_, _, sarah, bob := graphRun(t)
	if sarah.Graph == nil || len(sarah.Graph.Nodes) != 2 {
		t.Fatalf("graph = %+v", sarah.Graph)
	}
	for i, n := range sarah.Graph.Nodes {
		if want := storage.NewNodeID(sarah.ID, storage.NodeTypeSection, i); n.ID != want {
			t.Errorf("node %d id = %q, want %q", i, n.ID, want)
		}
		if !strings.HasPrefix(n.ID, sarah.ID+"/") {
			t.Errorf("node id %q does not carry the object id", n.ID)
		}
	}
	if !slices.Equal(storage.NodeClasses(sarah.Graph.Nodes[1]), []string{storage.ClassPersonal}) {
		t.Errorf("note classes = %v", storage.NodeClasses(sarah.Graph.Nodes[1]))
	}
	if bob.Graph != nil {
		t.Errorf("bob graph = %+v, want none", bob.Graph)
	}
}

// A second run dedups on the source key and stores nothing new.
func TestRunner_GraphObjectsDedup(t *testing.T) {
	drv, r, _, _ := graphRun(t)
	ctx := context.Background()
	if _, err := r.Run(ctx, graphAdapter{}); err != nil {
		t.Fatal(err)
	}
	if again, _, _ := drv.Objects().List(ctx, storage.ObjectFilter{Status: storage.ObjectStatusAll}); len(again) != 2 {
		t.Errorf("second run stored %d objects, want 2", len(again))
	}
}
