package steps

import (
	"errors"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// emitLenient emits spec and, when representation.Emit refuses it, skips the
// one representation and records the reason as `<kind>_error` metadata
// (ADR-076 §5) instead of failing the whole ingest. Emit itself stays strict;
// producers clamp what they can first (see clampSpan).
func emitLenient(draft *storage.KnowledgeObject, spec representation.Spec) {
	_, err := representation.Emit(draft, spec)
	if err == nil || errors.Is(err, representation.ErrNoObjectID) {
		return
	}
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	key := string(spec.Kind) + "_error"
	if prev, _ := draft.Metadata[key].(string); prev != "" {
		draft.Metadata[key] = prev + "; " + err.Error()
		return
	}
	draft.Metadata[key] = err.Error()
}

// clampSpan makes a provider-reported span a valid anchor: negatives become
// 0 and an end before the start becomes the start.
func clampSpan(startMS, endMS int64) (int64, int64) {
	startMS = max(startMS, 0)
	return startMS, max(endMS, startMS)
}

// clampNonNeg makes a provider-reported number a valid index or page.
func clampNonNeg(n int) int { return max(n, 0) }

// applySpeakers copies diarized speaker labels onto existing transcript
// nodes, matching each segment to its node by start and end time.
func applySpeakers(g *storage.ObjectGraph, segs []providers.TranscriptSegment) {
	if g == nil {
		return
	}
	for _, seg := range segs {
		if seg.Speaker == "" {
			continue
		}
		start, end := clampSpan(seg.StartTime.Milliseconds(), seg.EndTime.Milliseconds())
		for i := range g.Nodes {
			m, ok := representation.MetaOf(g.Nodes[i])
			if !ok || m.Kind != representation.Transcript || m.StartMS == nil || m.EndMS == nil ||
				*m.StartMS != start || *m.EndMS != end {
				continue
			}
			g.Nodes[i].Metadata[representation.KeySpeaker] = seg.Speaker
		}
	}
}

// collapseWhitespace is the normalisation text_cleaner applies to RawContent: CRLF to
// LF, runs of blanks to one space, three or more newlines to two, trimmed.
func collapseWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = multiSpace.ReplaceAllString(s, " ")
	s = multiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// ensureRoot returns the ID of the object's root summary node, adding it
// when there is none. summary/0 is the root, and is created when absent,
// unless it carries a representation: a summary representation is derived
// text, not the root the text-splitting steps hang their sections off. Then
// the first non-representation summary is the root, and failing that a new
// root takes the next free summary ordinal.
func ensureRoot(draft *storage.KnowledgeObject, label, content string) string {
	g := draft.Graph
	id0 := storage.NewNodeID(draft.ID, storage.NodeTypeSummary, 0)
	n0 := g.FindNode(id0)
	if n0 == nil {
		g.Nodes = append(g.Nodes, storage.GraphNode{ID: id0, NodeType: storage.NodeTypeSummary, Label: label, Content: content})
		return id0
	}
	isRep := func(n storage.GraphNode) bool { _, ok := representation.MetaOf(n); return ok }
	if !isRep(*n0) {
		return id0
	}
	for _, n := range g.Nodes {
		if n.NodeType == storage.NodeTypeSummary && !isRep(n) {
			return n.ID
		}
	}
	id := storage.NewNodeID(draft.ID, storage.NodeTypeSummary, storage.NextOrdinal(g, storage.NodeTypeSummary))
	g.Nodes = append(g.Nodes, storage.GraphNode{ID: id, NodeType: storage.NodeTypeSummary, Label: label, Content: content})
	return id
}
