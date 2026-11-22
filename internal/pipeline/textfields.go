package pipeline

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// NonTextField names the first text field of obj that holds binary
// (invalid UTF-8 or NUL, see blobref.NeedsBlob), or "" when they all are
// text. Only content has a blob to go to: every other field the store
// writes as text or JSON (text content, type, summaries, sections, tags,
// mentions, graph, decisions, tasks, the source and the rest) must be
// text. With skipTextContent a binary object's own text_content is not
// checked (an externalize step empties it). withMetadata adds every string
// in the metadata, keys included.
func NonTextField(obj *storage.KnowledgeObject, skipTextContent, withMetadata bool) string {
	if !skipTextContent && blobref.NeedsBlob(obj.TextContent) {
		return "text_content"
	}
	if f := nonTextScalar(obj); f != "" {
		return f
	}
	if f := ReinforceNonTextField(obj); f != "" {
		return f
	}
	if f := nonTextLists(obj); f != "" {
		return f
	}
	if f := nonTextSections(obj, withMetadata); f != "" {
		return f
	}
	if f := nonTextGraph(obj.Graph, withMetadata); f != "" {
		return f
	}
	if withMetadata {
		if k := nonTextIn(obj.Metadata); k != "" {
			return "metadata." + k
		}
	}
	return ""
}

// ReinforceNonTextField names the first binary field among those a
// reinforce writes or keys on (tags, mentions, profile), or "". A
// reinforce only bumps the count and merges tags and mentions, so a
// binary field it never writes does not stop it.
func ReinforceNonTextField(obj *storage.KnowledgeObject) string {
	if blobref.NeedsBlob(obj.ProfileID) {
		return "profile_id"
	}
	for i, t := range obj.Tags {
		if blobref.NeedsBlob(t.Label) || blobref.NeedsBlob(t.Source) {
			return fmt.Sprintf("tags[%d]", i)
		}
	}
	for i, m := range obj.Mentions {
		if blobref.NeedsBlob(m.String()) {
			return fmt.Sprintf("mentions[%d]", i)
		}
	}
	return ""
}

// nonTextScalar checks the object's plain string fields.
func nonTextScalar(obj *storage.KnowledgeObject) string {
	fields := []struct{ name, val string }{
		{"type", obj.Type},
		{"subtype", obj.Subtype},
		{"content_type", obj.ContentType},
		{"pipeline", obj.Pipeline},
		{"source", obj.Source},
		{"source", obj.SourceKey},
		{"status", obj.Status},
		{"inbox_note", obj.InboxNote},
		{"session_id", obj.SessionID},
	}
	for _, f := range fields {
		if blobref.NeedsBlob(f.val) {
			return f.name
		}
	}
	return ""
}

// nonTextLists checks the object's string lists, decisions and tasks.
func nonTextLists(obj *storage.KnowledgeObject) string {
	lists := []struct {
		name string
		vals []string
	}{
		{"summaries", obj.Summaries},
		{"registry_influences", obj.RegistryInfluences},
		{"classes", obj.Classes},
	}
	for _, l := range lists {
		for i, s := range l.vals {
			if blobref.NeedsBlob(s) {
				return fmt.Sprintf("%s[%d]", l.name, i)
			}
		}
	}
	for i, d := range obj.Decisions {
		if blobref.NeedsBlob(d.Title) || blobref.NeedsBlob(d.Status) || blobref.NeedsBlob(d.Impact) {
			return fmt.Sprintf("decisions[%d]", i)
		}
	}
	for i, t := range obj.Tasks {
		if blobref.NeedsBlob(t.Title) || blobref.NeedsBlob(t.Status) {
			return fmt.Sprintf("tasks[%d]", i)
		}
	}
	return ""
}

func nonTextSections(obj *storage.KnowledgeObject, withMetadata bool) string {
	for i, sec := range obj.Sections {
		if blobref.NeedsBlob(sec.Title) || blobref.NeedsBlob(sec.Content) {
			return fmt.Sprintf("sections[%d]", i)
		}
		if withMetadata {
			if k := nonTextIn(sec.Metadata); k != "" {
				return fmt.Sprintf("sections[%d].metadata.%s", i, k)
			}
		}
	}
	return ""
}

// nonTextGraph checks node and edge text, and node metadata.
func nonTextGraph(g *storage.ObjectGraph, withMetadata bool) string {
	if g == nil {
		return ""
	}
	for i, n := range g.Nodes {
		if blobref.NeedsBlob(n.ID) || blobref.NeedsBlob(string(n.NodeType)) ||
			blobref.NeedsBlob(n.Label) || blobref.NeedsBlob(n.Content) {
			return fmt.Sprintf("graph.nodes[%d]", i)
		}
		if withMetadata {
			if k := nonTextIn(n.Metadata); k != "" {
				return fmt.Sprintf("graph.nodes[%d].metadata.%s", i, k)
			}
		}
	}
	for i, e := range g.Edges {
		if blobref.NeedsBlob(e.ID) || blobref.NeedsBlob(e.FromID) ||
			blobref.NeedsBlob(e.ToID) || blobref.NeedsBlob(string(e.EdgeType)) {
			return fmt.Sprintf("graph.edges[%d]", i)
		}
	}
	return ""
}

// nonTextIn is the path of the first binary string (or key) in m, or "".
func nonTextIn(m map[string]any) string {
	for k, v := range m {
		if blobref.NeedsBlob(k) {
			return fmt.Sprintf("%q", k)
		}
		if p := nonTextValue(v); p != "" {
			return k + p
		}
	}
	return ""
}

func nonTextValue(v any) string {
	switch t := v.(type) {
	case string:
		if blobref.NeedsBlob(t) {
			return " (string)"
		}
	case []string:
		for i, s := range t {
			if blobref.NeedsBlob(s) {
				return fmt.Sprintf("[%d]", i)
			}
		}
	case []any:
		for i, e := range t {
			if p := nonTextValue(e); p != "" {
				return fmt.Sprintf("[%d]%s", i, p)
			}
		}
	case map[string]any:
		if k := nonTextIn(t); k != "" {
			return "." + k
		}
	case []map[string]any:
		for i, e := range t {
			if k := nonTextIn(e); k != "" {
				return fmt.Sprintf("[%d].%s", i, k)
			}
		}
	case map[string]string:
		for k, s := range t {
			if blobref.NeedsBlob(k) || blobref.NeedsBlob(s) {
				return "." + k
			}
		}
	case []map[string]string:
		for i, e := range t {
			for k, s := range e {
				if blobref.NeedsBlob(k) || blobref.NeedsBlob(s) {
					return fmt.Sprintf("[%d].%s", i, k)
				}
			}
		}
	}
	return ""
}
