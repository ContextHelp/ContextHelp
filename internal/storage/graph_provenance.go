package storage

import (
	"encoding/json"
	"maps"
)

// ProducedByKey is the Metadata key, on a graph node or edge, that names
// the pipeline step that added it. A node or edge without it was not added
// by a step: an importer, a client, a person or the attached original.
const ProducedByKey = "produced_by"

// ProducedBy returns the name of the step that added the node, or "".
func (n GraphNode) ProducedBy() string {
	s, _ := n.Metadata[ProducedByKey].(string)
	return s
}

// ProducedBy returns the name of the step that added the edge, or "".
func (e GraphEdge) ProducedBy() string {
	s, _ := e.Metadata[ProducedByKey].(string)
	return s
}

// WithoutProducedBy returns meta without ProducedByKey, as a copy (never the
// stored map); nil when nothing else is left. For views that must not carry step names.
func WithoutProducedBy(meta map[string]any) map[string]any {
	out := maps.Clone(meta)
	delete(out, ProducedByKey)
	if len(out) == 0 {
		return nil
	}
	return out
}

// GraphJSONMarked reports whether the stored graph JSON carries the
// Stamped mark.
func GraphJSONMarked(raw []byte) bool {
	var g struct {
		Stamped bool `json:"stamped"`
	}
	return len(raw) > 0 && json.Unmarshal(raw, &g) == nil && g.Stamped
}

// KeepStoredMark returns the graph an update writes: g, or a copy of g
// marked Stamped when the stored graph is marked. The mark belongs to the
// store: a client that builds its own graph does not turn a marked graph
// back into one that predates stamping.
func KeepStoredMark(g *ObjectGraph, storedMarked bool) *ObjectGraph {
	if g == nil || g.Stamped || !storedMarked {
		return g
	}
	c := *g
	c.Stamped = true
	return &c
}
