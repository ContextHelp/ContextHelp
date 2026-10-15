package storage

// Keys of Edge.Metadata on mentions and mentioned_in edges: where in the
// source object the mention was found. A restricted viewer sees the edge
// only while that field, and that node when there is one, is projected
// and unmasked, so every writer of these edges sets both keys.
const (
	// ProvenanceSourceField is the KnowledgeObject JSON field name the
	// mention was read from. Always set; see MentionSource*.
	ProvenanceSourceField = "source_field"
	// ProvenanceSourceNode is the ID of the entity_mention node that
	// holds the mention, or "" when the object's graph has none for it.
	ProvenanceSourceNode = "source_node"
)

// Values of ProvenanceSourceField on an edge, and of the same key on an
// entity_mention node's Metadata, where the step that creates the node
// records which field the mention came from.
const (
	// MentionSourceRawContent: extracted from RawContent, by the literal
	// @mention scan or the LLM pass over it.
	MentionSourceRawContent = "raw_content"
	// MentionSourceMentions: read from the object's mentions field as the
	// caller asserted it; no text of the object is known to hold it.
	MentionSourceMentions = "mentions"
)

// MentionNode returns the entity_mention node of g labelled with the
// mention's URI string, or nil when g has none.
func (g *ObjectGraph) MentionNode(mention string) *GraphNode {
	if g == nil {
		return nil
	}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.NodeType == NodeTypeEntityMention && n.Label == mention {
			return n
		}
	}
	return nil
}

// MentionProvenance returns the metadata a mentions or mentioned_in edge
// carries for the mention (its URI string) of the object whose graph is
// g: ProvenanceSourceNode names the entity_mention node that holds it and
// ProvenanceSourceField the field that node recorded. Without such a
// node the mention is known only from the object's mentions field, so
// the node is "" and the field is MentionSourceMentions. It never adds a
// node to g.
func MentionProvenance(g *ObjectGraph, mention string) map[string]any {
	node := g.MentionNode(mention)
	if node == nil {
		return map[string]any{
			ProvenanceSourceNode:  "",
			ProvenanceSourceField: MentionSourceMentions,
		}
	}
	field, _ := node.Metadata[ProvenanceSourceField].(string)
	if field == "" {
		field = MentionSourceMentions
	}
	return map[string]any{
		ProvenanceSourceNode:  node.ID,
		ProvenanceSourceField: field,
	}
}
