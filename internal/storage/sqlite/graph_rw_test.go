//go:build fts5

package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestObjectStore_CreateAndGet_GraphRoundtrip(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	ko := &pluginapi.KnowledgeObject{
		ID:        "obj-rw-1",
		Type:      "note",
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID("obj-rw-1", pluginapi.NodeTypeSection, 0),
					NodeType: pluginapi.NodeTypeSection,
					Label:    "Intro",
					Content:  "hello",
					Order:    0,
				},
			},
		},
	}

	if err := drv.Objects().Create(ctx, ko); err != nil {
		t.Fatal(err)
	}
	got, err := drv.Objects().Get(ctx, "obj-rw-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Graph == nil {
		t.Fatal("Graph not persisted")
	}
	if len(got.Graph.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(got.Graph.Nodes))
	}
	if got.Graph.Nodes[0].Content != "hello" {
		t.Errorf("node content mismatch: %q", got.Graph.Nodes[0].Content)
	}
	if got.Graph.Nodes[0].Label != "Intro" {
		t.Errorf("node label mismatch: %q", got.Graph.Nodes[0].Label)
	}
}

func TestObjectStore_Update_GraphRoundtrip(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	ko := &pluginapi.KnowledgeObject{
		ID: "obj-rw-2", Type: "note", Status: "active",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := drv.Objects().Create(ctx, ko); err != nil {
		t.Fatal(err)
	}

	ko.Graph = &pluginapi.ObjectGraph{
		Nodes: []pluginapi.GraphNode{
			{
				ID:       pluginapi.NewNodeID("obj-rw-2", pluginapi.NodeTypeTag, 0),
				NodeType: pluginapi.NodeTypeTag,
				Label:    "go",
				Content:  "go",
			},
		},
	}
	if err := drv.Objects().Update(ctx, ko); err != nil {
		t.Fatal(err)
	}
	got, err := drv.Objects().Get(ctx, "obj-rw-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Graph == nil || len(got.Graph.Nodes) == 0 {
		t.Fatal("updated graph not persisted")
	}
	if got.Graph.Nodes[0].Label != "go" {
		t.Errorf("updated node label: %q", got.Graph.Nodes[0].Label)
	}
}

func TestObjectStore_ObjectNodes_UpsertOnCreate(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	ko := &pluginapi.KnowledgeObject{
		ID: "obj-rw-3", Type: "note", Status: "active",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{ID: pluginapi.NewNodeID("obj-rw-3", pluginapi.NodeTypeTag, 0),
					NodeType: pluginapi.NodeTypeTag, Label: "go", Content: "go"},
				{ID: pluginapi.NewNodeID("obj-rw-3", pluginapi.NodeTypeTag, 1),
					NodeType: pluginapi.NodeTypeTag, Label: "sqlite", Content: "sqlite"},
			},
		},
	}
	if err := drv.Objects().Create(ctx, ko); err != nil {
		t.Fatal(err)
	}

	// Verify object_nodes rows were inserted
	rows, err := drv.DB().QueryContext(ctx,
		`SELECT node_type FROM object_nodes WHERE object_id = ? ORDER BY ordinal`, "obj-rw-3")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var nt string
		if err := rows.Scan(&nt); err != nil {
			t.Fatal(err)
		}
		types = append(types, nt)
	}
	if len(types) != 2 {
		t.Fatalf("want 2 object_nodes rows, got %d", len(types))
	}
}
