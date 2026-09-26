package steps

import (
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// itemsToEnqueueKey is the metadata key the worker reads once a container
// object is stored: it enqueues one job per entry, taking the entry's
// "content" as payload, "source" as source and "pipeline" as pipeline.
const itemsToEnqueueKey = "items_to_enqueue"

// Item pipelines a fan-out hands its items to: text is embedded as is, a
// bare URL is fetched first.
const (
	itemTextPipeline = "text.long"
	itemURLPipeline  = "url.generic"
)

// fanOutItem is one item a splitter stages for ingestion as an object of
// its own.
type fanOutItem struct {
	Title    string
	Content  string // payload: the item's text, or its URL when it has none
	Source   string
	Pipeline string // empty picks itemPipeline(Content)
}

// itemPipeline picks the content pipeline for an item payload.
func itemPipeline(content string) string {
	c := strings.TrimSpace(content)
	if !strings.ContainsAny(c, " \t\n") && (strings.HasPrefix(c, "http://") || strings.HasPrefix(c, "https://")) {
		return itemURLPipeline
	}
	return itemTextPipeline
}

// stageItems appends items with a payload to the container's
// items_to_enqueue, and names each on the container's graph as an artifact
// node the container contains, keyed by the item's source: item IDs exist
// only once the worker stores them.
func stageItems(draft *storage.KnowledgeObject, items []fanOutItem) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	pending, _ := draft.Metadata[itemsToEnqueueKey].([]map[string]any)
	if pending == nil {
		pending = make([]map[string]any, 0, len(items))
	}
	for _, it := range items {
		if strings.TrimSpace(it.Content) == "" {
			continue
		}
		p := it.Pipeline
		if p == "" {
			p = itemPipeline(it.Content)
		}
		ordinal := len(pending)
		pending = append(pending, map[string]any{
			"title":    it.Title,
			"content":  it.Content,
			"source":   it.Source,
			"pipeline": p,
		})
		if draft.ID == "" {
			continue
		}
		if draft.Graph == nil {
			draft.Graph = &pluginapi.ObjectGraph{}
		}
		nodeID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeArtifact, ordinal)
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       nodeID,
			NodeType: pluginapi.NodeTypeArtifact,
			Label:    it.Title,
			Order:    ordinal,
			Metadata: map[string]any{"source": it.Source, "pipeline": p},
		})
		draft.Graph.Edges = append(draft.Graph.Edges, pluginapi.GraphEdge{
			ID:       fmt.Sprintf("%s->%s", draft.ID, nodeID),
			FromID:   draft.ID,
			ToID:     nodeID,
			EdgeType: pluginapi.EdgeTypeContains,
		})
	}
	draft.Metadata[itemsToEnqueueKey] = pending
}

// firstString returns the first non-empty string value among keys of m.
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, _ := m[k].(string); strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
