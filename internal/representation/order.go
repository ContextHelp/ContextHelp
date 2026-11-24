package representation

import (
	"encoding/json"
	"sort"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Meta is the decoded provenance of a representation node. Numeric keys
// come back from storage as float64 after a JSON round trip; MetaOf accepts
// every numeric form.
type Meta struct {
	Kind           Kind
	StartMS, EndMS *int64
	Speaker        string
	PageNumber     *int
	FrameIndex     *int
	FrameMS        *int64
	ImageIndex     *int
	Provider       string
	Model          string
	Confidence     *float64
	Origin         string
}

// MetaOf decodes a node's provenance. ok is false when the node carries no
// valid representation kind, so it is not a representation node.
func MetaOf(n storage.GraphNode) (Meta, bool) {
	s, _ := n.Metadata[KeyRepresentation].(string)
	k := Kind(s)
	if !k.Valid() {
		return Meta{}, false
	}
	m := Meta{Kind: k}
	m.StartMS = int64Of(n.Metadata, KeyStartMS)
	m.EndMS = int64Of(n.Metadata, KeyEndMS)
	m.FrameMS = int64Of(n.Metadata, KeyFrameMS)
	m.PageNumber = intOf(n.Metadata, KeyPageNumber)
	m.FrameIndex = intOf(n.Metadata, KeyFrameIndex)
	m.ImageIndex = intOf(n.Metadata, KeyImageIndex)
	m.Speaker, _ = n.Metadata[KeySpeaker].(string)
	m.Provider, _ = n.Metadata[KeyProvider].(string)
	m.Model, _ = n.Metadata[KeyModel].(string)
	m.Origin, _ = n.Metadata[KeyOrigin].(string)
	if f, ok := floatOf(n.Metadata[KeyConfidence]); ok {
		m.Confidence = &f
	}
	return m, true
}

func floatOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

func int64Of(m map[string]any, key string) *int64 {
	f, ok := floatOf(m[key])
	if !ok {
		return nil
	}
	v := int64(f)
	return &v
}

func intOf(m map[string]any, key string) *int {
	f, ok := floatOf(m[key])
	if !ok {
		return nil
	}
	v := int(f)
	return &v
}

// Reading tiers of the projection order: summaries first, then the
// content representations, then per-frame representations.
const (
	tierSummary = iota
	tierBody
	tierFrame
)

type ranked struct {
	n    storage.GraphNode
	tier int
	m    Meta
	pos  int
}

// Ordered returns the summary and section nodes of g in the order the index
// projection reads them. Nodes without a representation key come first, in
// graph order. Representation nodes follow: summaries, then everything
// else by node Order, then frame representations by frame_index with
// captions before OCR. Ties keep graph order. Other node types (tags,
// mentions, ...) are not part of the text and are left out.
func Ordered(g *storage.ObjectGraph) []storage.GraphNode {
	if g == nil {
		return nil
	}
	var plain []storage.GraphNode
	var reps []ranked
	for i, n := range g.Nodes {
		if n.NodeType != storage.NodeTypeSummary && n.NodeType != storage.NodeTypeSection {
			continue
		}
		m, ok := MetaOf(n)
		if !ok {
			plain = append(plain, n)
			continue
		}
		r := ranked{n: n, m: m, pos: i, tier: tierBody}
		switch {
		case m.Kind == Summary:
			r.tier = tierSummary
		case m.Kind.isFrame():
			r.tier = tierFrame
		}
		reps = append(reps, r)
	}
	sort.SliceStable(reps, func(i, j int) bool {
		a, b := reps[i], reps[j]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if a.tier == tierFrame {
			if fa, fb := frameKey(a.m), frameKey(b.m); fa != fb {
				return fa < fb
			}
			if a.m.Kind != b.m.Kind {
				return a.m.Kind == FrameCaption
			}
		}
		return a.n.Order < b.n.Order
	})
	out := plain
	for _, r := range reps {
		out = append(out, r.n)
	}
	return out
}

func frameKey(m Meta) int {
	if m.FrameIndex == nil {
		return 0
	}
	return *m.FrameIndex
}
