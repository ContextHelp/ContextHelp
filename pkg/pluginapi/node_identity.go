package pluginapi

import (
	"fmt"
	"strconv"
	"strings"
)

// Node type constants for ObjectGraph nodes.
const (
	NodeTypeSection       = "section"
	NodeTypeTag           = "tag"
	NodeTypeEntityMention = "entity_mention"
	NodeTypeDecision      = "decision"
	NodeTypeTask          = "task"
	NodeTypeSummary       = "summary"
	NodeTypeCodeBlock     = "code_block"
)

// Edge type constants for ObjectGraph edges.
const (
	EdgeTypeContains    = "contains"
	EdgeTypeReferences  = "references"
	EdgeTypeResolvesTo  = "resolves_to"
	EdgeTypeDerivedFrom = "derives_from"
)

// NodeRef is a parsed reference to a node within an object graph.
type NodeRef struct {
	ObjectID string
	NodeType string
	Ordinal  int
}

// NewNodeID returns a stable opaque identifier for a node.
// Format: "<objectID>/<nodeType>/<ordinal>"
func NewNodeID(objectID, nodeType string, ordinal int) string {
	return fmt.Sprintf("%s/%s/%d", objectID, nodeType, ordinal)
}

// ParseNodeID parses a node ID produced by NewNodeID.
func ParseNodeID(id string) (NodeRef, error) {
	parts := strings.SplitN(id, "/", 3)
	if len(parts) != 3 {
		return NodeRef{}, fmt.Errorf("invalid node id %q", id)
	}
	ord, err := strconv.Atoi(parts[2])
	if err != nil {
		return NodeRef{}, fmt.Errorf("invalid ordinal in node id %q: %w", id, err)
	}
	return NodeRef{ObjectID: parts[0], NodeType: parts[1], Ordinal: ord}, nil
}

// NodeURI returns the canonical URI for a node.
// Format: "ctxt:node/<objectID>/<nodeType>/<ordinal>"
func NodeURI(objectID, nodeType string, ordinal int) string {
	return fmt.Sprintf("ctxt:node/%s/%s/%d", objectID, nodeType, ordinal)
}

// ParseNodeURI parses a node URI produced by NodeURI.
func ParseNodeURI(uri string) (NodeRef, error) {
	const prefix = "ctxt:node/"
	if !strings.HasPrefix(uri, prefix) {
		return NodeRef{}, fmt.Errorf("invalid node uri %q", uri)
	}
	return ParseNodeID(strings.TrimPrefix(uri, prefix))
}
