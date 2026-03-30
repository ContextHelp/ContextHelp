package pluginapi

// NodeHit is a search result pointing to a matching node within an object.
type NodeHit struct {
	ObjectID string  `json:"object_id"`
	NodeRef  string  `json:"node_ref"`  // canonical NodeURI
	NodeType string  `json:"node_type"`
	Snippet  string  `json:"snippet,omitempty"`
	Score    float64 `json:"score,omitempty"`
}

// NodeAwareFilter extends object filtering with node/edge type constraints.
type NodeAwareFilter struct {
	// NodeTypes restricts to objects containing nodes of these types.
	NodeTypes []string `json:"node_types,omitempty"`
	// EdgeTypes restricts to objects containing edges of these types.
	EdgeTypes []string `json:"edge_types,omitempty"`
	// ReturnNodeHits — when true, results include per-node NodeHit entries.
	ReturnNodeHits bool `json:"return_node_hits,omitempty"`
}

// NodeAwareResult wraps a KnowledgeObject result with optional node-level hits.
type NodeAwareResult struct {
	Object       *KnowledgeObject    `json:"object"`
	NodeHits     []NodeHit           `json:"node_hits,omitempty"`
	// DocumentView is the derived DocumentProjection for display surfaces.
	DocumentView *DocumentProjection `json:"document_view,omitempty"`
}
