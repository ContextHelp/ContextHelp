package storage

import (
	"fmt"
	"strconv"
	"strings"
)

// GraphNodeType is the type of a node within an ObjectGraph.
type GraphNodeType = string

// GraphEdgeType is the type of an edge within an ObjectGraph.
type GraphEdgeType = string

// Node type constants for ObjectGraph nodes.
const (
	NodeTypeSection       = "section"
	NodeTypeTag           = "tag"
	NodeTypeEntityMention = "entity_mention"
	NodeTypeDecision      = "decision"
	NodeTypeTask          = "task"
	NodeTypeSummary       = "summary"
	NodeTypeCodeBlock     = "code_block"
	NodeTypeOpenQuestion  = "open_question"
	NodeTypeArtifact      = "artifact" // binary/media reference or external package/dependency
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
// Panics (programmer error) if objectID or nodeType is empty, objectID contains '/', or ordinal < 0.
func NewNodeID(objectID, nodeType string, ordinal int) string {
	if objectID == "" || nodeType == "" {
		panic("storage.NewNodeID: objectID and nodeType must be non-empty")
	}
	if strings.Contains(objectID, "/") {
		panic(fmt.Sprintf("storage.NewNodeID: objectID must not contain '/': %q", objectID))
	}
	if ordinal < 0 {
		panic(fmt.Sprintf("storage.NewNodeID: ordinal must be >= 0, got %d", ordinal))
	}
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
	ref := NodeRef{ObjectID: parts[0], NodeType: parts[1], Ordinal: ord}
	if ref.ObjectID == "" || ref.NodeType == "" {
		return NodeRef{}, fmt.Errorf("invalid node id %q: empty objectID or nodeType", id)
	}
	if ref.Ordinal < 0 {
		return NodeRef{}, fmt.Errorf("invalid node id %q: negative ordinal", id)
	}
	return ref, nil
}

// NodeURI returns the canonical URI for a node.
// Format: "ctxt:node/<objectID>/<nodeType>/<ordinal>"
// Panics (programmer error) if objectID or nodeType is empty, objectID contains '/', or ordinal < 0.
func NodeURI(objectID, nodeType string, ordinal int) string {
	if objectID == "" || nodeType == "" {
		panic("storage.NodeURI: objectID and nodeType must be non-empty")
	}
	if strings.Contains(objectID, "/") {
		panic(fmt.Sprintf("storage.NodeURI: objectID must not contain '/': %q", objectID))
	}
	if ordinal < 0 {
		panic(fmt.Sprintf("storage.NodeURI: ordinal must be >= 0, got %d", ordinal))
	}
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

// NextOrdinal is the next free ordinal for nodeType in g: one past the
// highest ordinal any node of that type holds (read from its ID, or its Order
// when the ID is unset). Steps that add nodes of a type other steps also add
// (sections, summaries, artifacts) allocate through it, so IDs do not collide
// whichever step ran first. It is derived from the graph, not
// counted, so gaps cannot cause reuse.
//
// Artifact ordinal 0 is reserved for the stored original (blob.AttachOriginal
// takes it by name), so NextOrdinal never returns 0 for an artifact.
func NextOrdinal(g *ObjectGraph, nodeType string) int {
	next := 0
	if nodeType == NodeTypeArtifact {
		next = 1
	}
	if g == nil {
		return next
	}
	for _, n := range g.Nodes {
		if n.NodeType != nodeType {
			continue
		}
		if o := nodeOrdinal(n); o+1 > next {
			next = o + 1
		}
	}
	return next
}

func nodeOrdinal(n GraphNode) int {
	if ref, err := ParseNodeID(n.ID); err == nil {
		return ref.Ordinal
	}
	return n.Order
}
