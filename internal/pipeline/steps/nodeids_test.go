package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func foreignGraph(id, nodeType string) *storage.ObjectGraph {
	return &storage.ObjectGraph{Nodes: []storage.GraphNode{{ID: id + "/" + nodeType + "/0", NodeType: nodeType, Label: "imported"}}}
}

func nodeIDs(g *storage.ObjectGraph) map[string]int {
	out := map[string]int{}
	for _, n := range g.Nodes {
		out[n.ID]++
	}
	return out
}

func assertNoDuplicateIDs(t *testing.T, g *storage.ObjectGraph) {
	t.Helper()
	for id, n := range nodeIDs(g) {
		if n > 1 {
			t.Errorf("node ID %s held %d times", id, n)
		}
	}
}

// A kept node on the ID a step would take does not stop the step from
// adding its own node, and the step never lands on it.
func TestTaggerAddsItsNodesBesideAForeignOne(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "o", RawContent: "storage storage storage tests tests layer", Graph: foreignGraph("o", storage.NodeTypeTag)}
	out, err := NewTagger().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	assertNoDuplicateIDs(t, out.Graph)
	if n := out.Graph.FindNode("o/tag/0"); n == nil || n.Label != "imported" {
		t.Fatalf("the foreign node was changed: %+v", n)
	}
	for _, tag := range out.Tags {
		if !ownNode(out.Graph.Nodes, storage.NodeTypeTag, tag.Label, "tagger") {
			t.Errorf("tag %q has no node", tag.Label)
		}
	}
	if len(out.Tags) == 0 {
		t.Fatal("no tags")
	}
}

func TestStepsOnAnEmptyGraphKeepTheirIDs(t *testing.T) {
	out, err := NewTagger().Run(context.Background(), &storage.KnowledgeObject{ID: "o", RawContent: "storage storage tests tests layer"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Graph.FindNode("o/tag/0") == nil || out.Graph.FindNode("o/tag/1") == nil {
		t.Errorf("tag IDs moved: %v", nodeIDs(out.Graph))
	}
}

func TestLanguageDetectorAddsItsNodeBesideAForeignOne(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "o", Source: "main.go", Graph: foreignGraph("o", storage.NodeTypeTag)}
	out, err := NewLanguageDetector().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	assertNoDuplicateIDs(t, out.Graph)
	if !ownNode(out.Graph.Nodes, storage.NodeTypeTag, "lang:go", languageDetectorName) || len(out.Graph.Nodes) != 2 {
		t.Errorf("language node missing: %+v", out.Graph.Nodes)
	}
}

func TestEntityExtractorAddsItsNodeBesideAForeignOne(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "o", RawContent: "ask @person.ada", Graph: foreignGraph("o", storage.NodeTypeEntityMention)}
	out, err := NewEntityExtractor().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	assertNoDuplicateIDs(t, out.Graph)
	if len(out.Graph.Nodes) != 2 || out.Graph.FindNode("o/entity_mention/0").Label != "imported" {
		t.Errorf("nodes: %+v", out.Graph.Nodes)
	}
}

// comment_extractor wrote task/0 over a kept task/0: the primary key of
// the node table rejects it.
func TestCommentExtractorAddsItsNodesBesideAForeignOne(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "o", Source: "main.go", RawContent: "package main\n// TODO: fix this\n// FIXME: and that\nfunc F() {}\n", Graph: foreignGraph("o", storage.NodeTypeTask)}
	out, err := NewCommentExtractor().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	assertNoDuplicateIDs(t, out.Graph)
	if len(out.Graph.Nodes) < 2 || out.Graph.FindNode("o/task/0").Label != "imported" {
		t.Errorf("nodes: %+v", out.Graph.Nodes)
	}
}

type extractLLM struct{}

func (extractLLM) Name() string { return "fake" }
func (extractLLM) Generate(context.Context, string) (string, error) {
	return `{"summary":"S.","topics":["t1","t2"],"decisions":["d1"],"open_questions":[],"artifacts":[]}`, nil
}

// graph_extractor numbers Order across every type it emits, from the number
// of nodes steps made before it, as it always did; only the ID ordinal is
// per type.
func TestGraphExtractorOrderRunsAcrossTypes(t *testing.T) {
	g := &storage.ObjectGraph{}
	for i := range 3 {
		g.Nodes = append(g.Nodes, storage.GraphNode{ID: storage.NewNodeID("o", storage.NodeTypeSection, i), NodeType: storage.NodeTypeSection,
			Metadata: map[string]any{storage.ProducedByKey: "sectioner"}})
	}
	g.Nodes = append(g.Nodes, storage.GraphNode{ID: "o/decision/90", NodeType: storage.NodeTypeDecision, Order: 90})
	out, err := NewGraphExtractorWithLLM(extractLLM{}).Run(context.Background(), &storage.KnowledgeObject{ID: "o", RawContent: "some content to extract from", Graph: g})
	if err != nil {
		t.Fatal(err)
	}
	var orders []int
	for _, n := range out.Graph.Nodes[4:] {
		orders = append(orders, n.Order)
	}
	if len(orders) != 4 || orders[0] != 3 || orders[1] != 4 || orders[2] != 5 || orders[3] != 6 {
		t.Errorf("orders = %v, want 3 4 5 6 (a foreign node does not move them)", orders)
	}
}
