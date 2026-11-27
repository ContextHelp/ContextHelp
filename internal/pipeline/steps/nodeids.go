package steps

import "github.com/ideacrafterslabs/ctxt/internal/storage"

// ownNode reports whether nodes holds a node of the type and label that
// step made, or that nobody stamped: the node a step would add is already
// there. A node another step made does not count.
func ownNode(nodes []storage.GraphNode, nodeType, label, step string) bool {
	for _, n := range nodes {
		if n.NodeType == nodeType && n.Label == label && (n.ProducedBy() == "" || n.ProducedBy() == step) {
			return true
		}
	}
	return false
}

// freeNodeID is the ID of the lowest free ordinal of nodeType at or above want
// (the step's own running index for the type): never one past the highest, and
// never one that moves because the graph holds other nodes. A node a step
// rebuilds at reanalysis lands on the ID it had at ingest, and the step never
// lands on, nor skips because of, a node that is not its own.
func freeNodeID(g *storage.ObjectGraph, objectID, nodeType string, want int) string {
	return storage.NewNodeID(objectID, nodeType, storage.FreeOrdinal(g, objectID, nodeType, want))
}
