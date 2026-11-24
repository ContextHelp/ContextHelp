package representation

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrNoObjectID is returned by Emit for a draft that has no ID yet; node IDs
// derive from the object ID, so steps skip graph emission until it is set.
var ErrNoObjectID = errors.New("representation: draft has no object id")

// Anchor locates a representation within the original. Unset fields are
// absent from node metadata; zero is a valid value for every numeric field,
// so each is a pointer.
type Anchor struct {
	StartMS, EndMS *int64
	Speaker        string
	PageNumber     *int
	FrameIndex     *int
	FrameMS        *int64
	ImageIndex     *int
}

// WithSpan sets the time span within the original, in milliseconds.
func (a Anchor) WithSpan(startMS, endMS int64) Anchor {
	a.StartMS, a.EndMS = &startMS, &endMS
	return a
}

// WithSpeaker sets the diarized speaker label.
func (a Anchor) WithSpeaker(s string) Anchor { a.Speaker = s; return a }

// WithPage sets the page within a document.
func (a Anchor) WithPage(n int) Anchor { a.PageNumber = &n; return a }

// WithFrame sets the selected video frame and its timestamp.
func (a Anchor) WithFrame(index int, ms int64) Anchor {
	a.FrameIndex, a.FrameMS = &index, &ms
	return a
}

// WithFrameIndex sets the selected frame's index when its timestamp is not
// known; frame_ms is then absent rather than a misleading 0.
func (a Anchor) WithFrameIndex(index int) Anchor {
	a.FrameIndex = &index
	return a
}

// WithImage sets the image index within a document.
func (a Anchor) WithImage(n int) Anchor { a.ImageIndex = &n; return a }

// Spec describes one representation node to emit.
type Spec struct {
	Kind    Kind
	Label   string
	Content string
	Anchor  Anchor

	// Provider and Model name the backend and model that produced the text.
	Provider, Model string
	// Confidence is 0-1, nil when the producer reports none.
	Confidence *float64
	// Origin is OriginSidecar for imported subtitles; empty otherwise.
	Origin string

	// DerivesFrom is the node the text derives from, normally the artifact
	// that represents the original. Empty means FindOriginal on the graph;
	// no edge is written when there is none yet.
	DerivesFrom string
	// Extra holds producer-specific keys that are not part of the
	// convention (for example vision labels). Reserved keys are refused.
	Extra map[string]any
}

func (s Spec) validate() error {
	if !s.Kind.Valid() {
		return fmt.Errorf("representation: unknown kind %q", s.Kind)
	}
	if strings.TrimSpace(s.Content) == "" {
		// A producer that returned nothing has no representation; nothing
		// stands in for it (ADR-076 §5).
		return fmt.Errorf("representation: %s has no content", s.Kind)
	}
	if s.Confidence != nil && (*s.Confidence < 0 || *s.Confidence > 1) {
		return fmt.Errorf("representation: confidence %v outside 0-1", *s.Confidence)
	}
	if s.Origin != "" && s.Origin != OriginSidecar {
		return fmt.Errorf("representation: unknown origin %q", s.Origin)
	}
	if err := s.Anchor.validate(); err != nil {
		return err
	}
	if s.Kind.isFrame() && s.Anchor.FrameIndex == nil {
		return fmt.Errorf("representation: %s needs a frame_index", s.Kind)
	}
	for k := range s.Extra {
		if reserved[k] {
			return fmt.Errorf("representation: extra key %q is reserved", k)
		}
	}
	return nil
}

// validate refuses a negative position and a span that ends before it starts.
func (a Anchor) validate() error {
	for name, v := range map[string]*int64{"start_ms": a.StartMS, "end_ms": a.EndMS, "frame_ms": a.FrameMS} {
		if v != nil && *v < 0 {
			return fmt.Errorf("representation: %s is negative", name)
		}
	}
	for name, v := range map[string]*int{"page_number": a.PageNumber, "frame_index": a.FrameIndex, "image_index": a.ImageIndex} {
		if v != nil && *v < 0 {
			return fmt.Errorf("representation: %s is negative", name)
		}
	}
	if a.StartMS != nil && a.EndMS != nil && *a.EndMS < *a.StartMS {
		return fmt.Errorf("representation: end_ms %d before start_ms %d", *a.EndMS, *a.StartMS)
	}
	return nil
}

func (s Spec) metadata() map[string]any {
	m := map[string]any{KeyRepresentation: string(s.Kind)}
	a := s.Anchor
	if a.StartMS != nil {
		m[KeyStartMS] = *a.StartMS
	}
	if a.EndMS != nil {
		m[KeyEndMS] = *a.EndMS
	}
	if a.Speaker != "" {
		m[KeySpeaker] = a.Speaker
	}
	if a.PageNumber != nil {
		m[KeyPageNumber] = *a.PageNumber
	}
	if a.FrameIndex != nil {
		m[KeyFrameIndex] = *a.FrameIndex
	}
	if a.FrameMS != nil {
		m[KeyFrameMS] = *a.FrameMS
	}
	if a.ImageIndex != nil {
		m[KeyImageIndex] = *a.ImageIndex
	}
	if s.Provider != "" {
		m[KeyProvider] = s.Provider
	}
	if s.Model != "" {
		m[KeyModel] = s.Model
	}
	if s.Confidence != nil {
		m[KeyConfidence] = *s.Confidence
	}
	if s.Origin != "" {
		m[KeyOrigin] = s.Origin
	}
	for k, v := range s.Extra {
		m[k] = v
	}
	return m
}

// Emit appends one representation node for spec to the draft's graph, with
// the next free ordinal for its node type, and a derives_from edge from the
// node to its source when there is one. It returns the node. A refused spec
// leaves the graph untouched.
func Emit(draft *storage.KnowledgeObject, spec Spec) (storage.GraphNode, error) {
	if draft == nil || draft.ID == "" {
		return storage.GraphNode{}, ErrNoObjectID
	}
	if err := spec.validate(); err != nil {
		return storage.GraphNode{}, err
	}
	if draft.Graph == nil {
		draft.Graph = &storage.ObjectGraph{}
	}
	g := draft.Graph
	// Emit is idempotent: a node with the same kind, anchor and content
	// (speaker too when both name one; an unlabelled node is filled in by diarization later) is returned as is.
	if n, ok := findSame(g, spec); ok {
		return n, nil
	}
	source := spec.DerivesFrom
	if source == "" {
		source = FindOriginal(g)
	} else if g.FindNode(source) == nil {
		return storage.GraphNode{}, fmt.Errorf("representation: source node %q not in graph", source)
	}

	nodeType := spec.Kind.NodeType()
	ordinal := storage.NextOrdinal(g, nodeType)
	n := storage.GraphNode{
		ID:       storage.NewNodeID(draft.ID, nodeType, ordinal),
		NodeType: nodeType,
		Label:    spec.Label,
		Content:  spec.Content,
		Order:    nextOrder(g, nodeType),
		Metadata: spec.metadata(),
	}
	g.Nodes = append(g.Nodes, n)
	if source != "" {
		g.Edges = append(g.Edges, storage.GraphEdge{
			ID:       fmt.Sprintf("%s->%s", n.ID, source),
			FromID:   n.ID,
			ToID:     source,
			EdgeType: storage.EdgeTypeDerivedFrom,
		})
	}
	return n, nil
}

func nextOrder(g *storage.ObjectGraph, nodeType string) int {
	next := 0
	for _, n := range g.Nodes {
		if n.NodeType == nodeType && n.Order+1 > next {
			next = n.Order + 1
		}
	}
	return next
}

// FindOriginal returns the ID of the artifact node that stands for the
// stored original: artifact ordinal 0, which blob.AttachOriginal owns, when
// it carries a blob key. It returns "" when the graph has none, and never a
// later artifact (a kept frame, a fan-out item).
func FindOriginal(g *storage.ObjectGraph) string {
	if g == nil {
		return ""
	}
	for _, n := range g.Nodes {
		if n.NodeType != storage.NodeTypeArtifact {
			continue
		}
		ref, err := storage.ParseNodeID(n.ID)
		if err != nil || ref.Ordinal != 0 {
			continue
		}
		if k, _ := n.Metadata[storage.ArtifactMetaBlobKey].(string); k != "" {
			return n.ID
		}
	}
	return ""
}

// Has reports whether g holds a representation node of any of kinds, or of
// any kind when none are given.
func Has(g *storage.ObjectGraph, kinds ...Kind) bool {
	if g == nil {
		return false
	}
	for _, n := range g.Nodes {
		m, ok := MetaOf(n)
		if !ok {
			continue
		}
		if len(kinds) == 0 {
			return true
		}
		for _, k := range kinds {
			if m.Kind == k {
				return true
			}
		}
	}
	return false
}

// ModelOf returns the model a provider reports through an optional
// Model() string method, or "" when it has none.
func ModelOf(provider any) string {
	if m, ok := provider.(interface{ Model() string }); ok {
		return m.Model()
	}
	return ""
}

// ConfidenceOf returns a pointer to c when it is a usable 0-1 confidence,
// nil when the producer reported none or an out-of-range value. Zero counts
// as none: providers report 0 for "no confidence", so a true 0.0 is dropped.
func ConfidenceOf(c float64) *float64 {
	if c <= 0 || c > 1 {
		return nil
	}
	return &c
}

func findSame(g *storage.ObjectGraph, s Spec) (storage.GraphNode, bool) {
	for _, n := range g.Nodes {
		m, ok := MetaOf(n)
		if !ok || m.Kind != s.Kind || n.Content != s.Content {
			continue
		}
		a := s.Anchor
		if a.Speaker != "" && m.Speaker != "" && a.Speaker != m.Speaker {
			continue
		}
		if eq64(m.StartMS, a.StartMS) && eq64(m.EndMS, a.EndMS) && eq64(m.FrameMS, a.FrameMS) &&
			eqInt(m.PageNumber, a.PageNumber) && eqInt(m.FrameIndex, a.FrameIndex) && eqInt(m.ImageIndex, a.ImageIndex) {
			return n, true
		}
	}
	return storage.GraphNode{}, false
}

func eq64(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
func eqInt(a, b *int) bool  { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// HasContent reports whether g holds a representation node of a content
// kind, that is any kind but summary. A summary is derived from content and
// does not stand for the object's text.
func HasContent(g *storage.ObjectGraph) bool {
	for _, k := range kinds {
		if k != Summary && Has(g, k) {
			return true
		}
	}
	return false
}
