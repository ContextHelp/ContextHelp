package service

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// An imported node on the ID a step would take stays, and every tag still
// has a node of its own.
func TestReanalyzeKeepsAForeignNodeOnAStepNodeID(t *testing.T) {
	e := newRealEnv(t)
	e.createStored(t, "imp1", "text.long@v0", &storage.ObjectGraph{Stamped: true, Nodes: []storage.GraphNode{
		{ID: "imp1/tag/0", NodeType: storage.NodeTypeTag, Label: "imported"},
	}})
	for range 2 {
		_, _, err := e.svc.ReanalyzeObject(ownerCtx(), "imp1")
		require.NoError(t, err)
		got := e.get(t, "imp1")
		n := got.Graph.FindNode("imp1/tag/0")
		require.NotNil(t, n)
		assert.Equal(t, "imported", n.Label)
		assert.Empty(t, n.ProducedBy())
		labels := map[string]bool{}
		seen := map[string]bool{}
		for _, nd := range got.Graph.Nodes {
			assert.False(t, seen[nd.ID], "duplicate node ID %s", nd.ID)
			seen[nd.ID] = true
			if nd.NodeType == storage.NodeTypeTag {
				labels[nd.Label] = true
			}
		}
		require.NotEmpty(t, got.Tags)
		for _, tag := range got.Tags {
			assert.True(t, labels[tag.Label], "tag %q has no node", tag.Label)
		}
	}
}

func stepNodeIDs(g *storage.ObjectGraph) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.ProducedBy() != "" {
			out = append(out, n.ID)
		}
	}
	slices.Sort(out)
	return out
}

// Reanalysing an object with unchanged content gives its step nodes the
// IDs they had at ingest, whatever foreign nodes sit at high ordinals.
func TestReanalyzeKeepsStepNodeIDsStable(t *testing.T) {
	for _, name := range []string{"text.long", "doc.markdown", "doc.code"} {
		t.Run(name, func(t *testing.T) {
			e := newRealEnv(t)
			fx := invFixtures[name](t, e)
			obj := e.ingestJob(t, name, fx.source, fx.payload, false)
			id := obj.ID
			o := e.get(t, id)
			want := stepNodeIDs(o.Graph)
			require.NotEmpty(t, want)
			for _, typ := range []string{storage.NodeTypeSection, storage.NodeTypeTag, storage.NodeTypeDecision, storage.NodeTypeSummary, storage.NodeTypeTask} {
				o.Graph.Nodes = append(o.Graph.Nodes, storage.GraphNode{ID: id + "/" + typ + "/90", NodeType: typ, Label: "foreign " + typ, Order: 90})
			}
			e.update(t, o)
			for run := 1; run <= 2; run++ {
				_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
				require.NoError(t, err)
				got := e.get(t, id)
				assert.Equal(t, want, stepNodeIDs(got.Graph), "run %d: step node IDs moved", run)
				for _, typ := range []string{storage.NodeTypeSection, storage.NodeTypeTag, storage.NodeTypeDecision, storage.NodeTypeSummary, storage.NodeTypeTask} {
					assert.NotNil(t, got.Graph.FindNode(id+"/"+typ+"/90"), "foreign %s dropped", typ)
				}
			}
		})
	}
}

func idsOf(g *storage.ObjectGraph) []string {
	var out []string
	for _, n := range g.Nodes {
		out = append(out, n.ID)
	}
	slices.Sort(out)
	return out
}

func assertUniqueIDs(t *testing.T, g *storage.ObjectGraph) {
	t.Helper()
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		assert.False(t, seen[n.ID], "duplicate node ID %s", n.ID)
		seen[n.ID] = true
	}
}

// A node the steps do not own inside the range they use moves the later
// nodes of that type up. The result is stable from the next run on, the
// foreign node stays, and no ID is held twice.
func TestReanalyzeWithAForeignNodeInsideTheStepRange(t *testing.T) {
	e := newRealEnv(t)
	e.createStored(t, "rng1", "text.long@v0", &storage.ObjectGraph{Stamped: true, Nodes: []storage.GraphNode{
		{ID: "rng1/tag/0", NodeType: storage.NodeTypeTag, Label: "imported"},
		{ID: "rng1/section/1", NodeType: storage.NodeTypeSection, Label: "foreign section", Order: 1},
	}})
	var first []string
	for run := 1; run <= 3; run++ {
		_, _, err := e.svc.ReanalyzeObject(ownerCtx(), "rng1")
		require.NoError(t, err)
		got := e.get(t, "rng1")
		assertUniqueIDs(t, got.Graph)
		for _, id := range []string{"rng1/tag/0", "rng1/section/1"} {
			n := got.Graph.FindNode(id)
			require.NotNil(t, n, "foreign %s dropped", id)
			assert.Empty(t, n.ProducedBy())
		}
		assert.NotNil(t, got.Graph.FindNode("rng1/tag/1"), "the tags did not move up past the foreign tag")
		assert.NotNil(t, got.Graph.FindNode("rng1/section/2"), "\"Details\" did not move up past the foreign section")
		if run == 1 {
			first = idsOf(got.Graph)
			continue
		}
		assert.Equal(t, first, idsOf(got.Graph), "run %d differs from run 1", run)
	}
}

func stepOrders(g *storage.ObjectGraph) map[string]int {
	out := map[string]int{}
	for _, n := range g.Nodes {
		if n.ProducedBy() != "" {
			out[n.ID] = n.Order
		}
	}
	return out
}

// Order is reproduced like the ID: a foreign node at a high Order does not
// move the Order of a rebuilt step node.
func TestReanalyzeKeepsStepNodeOrders(t *testing.T) {
	for _, name := range []string{"text.long", "image.ocr"} {
		t.Run(name, func(t *testing.T) {
			e := newRealEnv(t)
			var id string
			if name == "image.ocr" {
				id = invIngest(t, e, name, invFixtures[name](t, e))
			} else {
				id = e.ingestJob(t, name, "src", invText, false).ID
			}
			o := e.get(t, id)
			want := stepOrders(o.Graph)
			require.NotEmpty(t, want)
			for _, typ := range []string{storage.NodeTypeSection, storage.NodeTypeSummary, storage.NodeTypeTag, storage.NodeTypeDecision} {
				o.Graph.Nodes = append(o.Graph.Nodes, storage.GraphNode{ID: id + "/" + typ + "/90", NodeType: typ, Label: "foreign", Order: 90})
			}
			e.update(t, o)
			for run := 1; run <= 2; run++ {
				_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
				require.NoError(t, err)
				assert.Equal(t, want, stepOrders(e.get(t, id).Graph), "run %d: Order moved", run)
			}
		})
	}
}
