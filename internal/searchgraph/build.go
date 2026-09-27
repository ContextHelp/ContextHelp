package searchgraph

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Default caps.
//
// The default search pools are 50 FTS + 50 vector candidates, so a trace
// holds at most ~100 objects. DefaultMaxNodes keeps every such candidate
// plus up to ~150 entities: past a few hundred nodes a force-directed view
// stops being readable. DefaultMaxEdges bounds the quadratic relations
// (co_mention, similar) that can explode on popular entities, keeping
// layout cost and document size (~0.5 MB) in check while leaving room for
// every matched, link and mention edge of a default-sized trace.
const (
	DefaultMaxNodes = 250
	DefaultMaxEdges = 1500
	// DefaultSimilarThreshold is the minimum cosine similarity for a
	// similar edge.
	DefaultSimilarThreshold = 0.8
)

// ErrSimilarUnsupported is returned when similar edges are requested but
// the source cannot read stored embeddings.
var ErrSimilarUnsupported = errors.New("searchgraph: similar edges not supported: embedding store cannot read embeddings by id")

// ErrEntityVisibilityRequired is returned when Options.RequireEntityVisibility
// is set without an Options.EntityVisible predicate: a caller serving
// principals must state its entity policy, so a forgotten predicate fails
// the build instead of exposing every entity.
var ErrEntityVisibilityRequired = errors.New("searchgraph: entity visibility predicate required")

// EdgeLister lists stored edges leaving a node (storage.EdgeStore).
type EdgeLister interface {
	ListFrom(ctx context.Context, fromType, fromID string) ([]*storage.Edge, error)
}

// EntityGetter looks up an entity by slug (storage.EntityStore).
type EntityGetter interface {
	Get(ctx context.Context, slug string) (*storage.Entity, error)
}

// Source is the stored-relationship access the builder needs.
type Source struct {
	Edges    EdgeLister
	Entities EntityGetter
	// Embeddings is optional; required only for similar edges. It reads
	// the per-model rows of the trace's vector model.
	Embeddings storage.EmbeddingReader
}

// SourceFrom adapts a storage driver. Embeddings is set when the driver's
// embedding store implements storage.EmbeddingReader.
func SourceFrom(d storage.StorageDriver) Source {
	src := Source{Edges: d.Edges(), Entities: d.Entities()}
	if emb := d.Embeddings(); emb != nil {
		if r, ok := emb.(storage.EmbeddingReader); ok {
			src.Embeddings = r
		}
	}
	return src
}

// EntityVisibility reports whether an entity may appear in a document —
// the caller's access policy (dpkms: the principal's namespace
// entitlements). ent is the stored entity, nil when the store holds no
// record for slug. It is called once per distinct mentioned slug.
type EntityVisibility func(ctx context.Context, slug string, ent *storage.Entity) bool

// Options tunes a build. Zero values select defaults.
type Options struct {
	// MaxNodes caps nodes, the query node included (<= 0: DefaultMaxNodes).
	MaxNodes int
	// MaxEdges caps edges (<= 0: DefaultMaxEdges).
	MaxEdges int
	// Similar enables similar edges (cosine over stored embeddings of the
	// trace's vector model, the default model the search read).
	Similar bool
	// SimilarThreshold is the minimum cosine for a similar edge
	// (<= 0: DefaultSimilarThreshold).
	SimilarThreshold float64
	// EntityVisible hides entities from the document (nil: every entity
	// is visible). The predicate runs before anything is derived from
	// mentions, so a hidden entity is as absent as one never mentioned:
	// no node, no mentions edge, no share of a co_mention weight, a
	// mention count, a count or the node cap.
	EntityVisible EntityVisibility
	// RequireEntityVisibility makes EntityVisible mandatory: Build fails
	// with ErrEntityVisibilityRequired when it is nil. Servers set it so
	// the ungated default (nil: all visible, what the local CLI wants)
	// can never be reached by accident.
	RequireEntityVisibility bool
	// Now stamps generated_at (nil: time.Now).
	Now func() time.Time
}

func (o Options) withDefaults() Options {
	if o.MaxNodes <= 0 {
		o.MaxNodes = DefaultMaxNodes
	}
	if o.MaxEdges <= 0 {
		o.MaxEdges = DefaultMaxEdges
	}
	if o.SimilarThreshold <= 0 {
		o.SimilarThreshold = DefaultSimilarThreshold
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// Build turns a search trace into a graph document.
//
// Depth is 1: nodes are the query, the trace candidates and the entities
// those candidates mention; no other object is ever pulled in, and an edge
// is emitted only when both endpoints are nodes.
//
// Truncation keeps, in order: the query; objects by stage (returned,
// cut_limit, cut_threshold) then rank; entities by mention count
// (descending) then slug. Edges are kept in relation order matched, stored
// links, mentions, co_mention, similar; within a relation matched and
// mentions follow object order, links sort by (source, target, relation),
// and derived relations by weight (descending) then (source, target).
// Emitted order equals keep order, so output is deterministic.
//
// Store round trips: one ListFrom per kept object and one entity Get per
// kept entity (the stores offer no batch variants), both bounded by
// MaxNodes, plus one EmbeddingsByID call when Similar is set and the trace
// names a vector model. With EntityVisible set, the entity Gets cover
// every distinct slug the kept objects mention instead, since visibility
// is decided before the node cap ranks entities.
func Build(ctx context.Context, tr *service.SearchTrace, src Source, opts Options) (*Document, error) {
	if tr == nil {
		return nil, errors.New("searchgraph: nil trace")
	}
	if src.Edges == nil || src.Entities == nil {
		return nil, errors.New("searchgraph: source needs edges and entities")
	}
	if opts.RequireEntityVisibility && opts.EntityVisible == nil {
		return nil, ErrEntityVisibilityRequired
	}
	opts = opts.withDefaults()
	if opts.Similar && src.Embeddings == nil {
		return nil, ErrSimilarUnsupported
	}

	b := &builder{tr: tr, src: src, opts: opts}
	b.selectObjects()
	if err := b.loadEdges(ctx); err != nil {
		return nil, err
	}
	if err := b.selectEntities(ctx); err != nil {
		return nil, err
	}
	edges, err := b.edges(ctx)
	if err != nil {
		return nil, err
	}
	if len(edges) > opts.MaxEdges {
		edges = edges[:opts.MaxEdges]
		b.truncated = true
	}
	return b.document(edges), nil
}

// builder holds intermediate state for one Build.
type builder struct {
	tr   *service.SearchTrace
	src  Source
	opts Options

	objects   []*service.TraceCandidate // kept, in priority order
	kept      map[string]bool           // kept object ids
	mentions  map[string][]string       // object id -> sorted unique slugs
	links     []Edge                    // collapsed stored links, sorted
	entities  []entityNode              // kept, in priority order
	keptEnt   map[string]bool           // kept entity slugs
	truncated bool

	stored  map[string]*storage.Entity // looked-up entities; nil value: no record
	visible map[string]bool            // EntityVisible verdicts by slug
}

type entityNode struct {
	slug  string
	label string
	count int
}

// stagePriority orders stages for truncation; unknown stages sort last.
func stagePriority(s service.TraceStage) int {
	switch s {
	case service.TraceStageReturned:
		return 0
	case service.TraceStageCutLimit:
		return 1
	case service.TraceStageCutThreshold:
		return 2
	default:
		return 3
	}
}

// selectObjects orders candidates by (stage, rank, id), drops duplicate
// ids and keeps as many as the node cap allows after the query node.
func (b *builder) selectObjects() {
	cands := make([]*service.TraceCandidate, 0, len(b.tr.Candidates))
	seen := make(map[string]bool, len(b.tr.Candidates))
	for i := range b.tr.Candidates {
		c := &b.tr.Candidates[i]
		if c.ID == "" || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		cands = append(cands, c)
	}
	slices.SortStableFunc(cands, func(x, y *service.TraceCandidate) int {
		return cmp.Or(
			cmp.Compare(stagePriority(x.Stage), stagePriority(y.Stage)),
			cmp.Compare(x.Rank, y.Rank),
			cmp.Compare(x.ID, y.ID),
		)
	})
	budget := b.opts.MaxNodes - 1
	if len(cands) > budget {
		cands = cands[:budget]
		b.truncated = true
	}
	b.objects = cands
	b.kept = make(map[string]bool, len(cands))
	for _, c := range cands {
		b.kept[c.ID] = true
	}
}

// loadEdges reads each kept object's outgoing stored edges once, keeping
// entity mentions and collapsed object links whose target is kept.
func (b *builder) loadEdges(ctx context.Context) error {
	b.mentions = make(map[string][]string, len(b.objects))
	links := make(map[linkKey]Edge)
	for _, c := range b.objects {
		stored, err := b.src.Edges.ListFrom(ctx, "object", c.ID)
		if err != nil {
			return fmt.Errorf("searchgraph: list edges from %s: %w", c.ID, err)
		}
		var slugs []string
		for _, e := range stored {
			switch {
			case e.ToType == "entity" && e.EdgeType == RelMentions && e.ToID != "":
				slugs = append(slugs, e.ToID)
			case e.ToType == "object" && b.kept[e.ToID]:
				if l, ok := collapseLink(c.ID, e.ToID, e.EdgeType); ok {
					links[linkKey{l.Source, l.Target, l.Relation}] = l
				}
			}
		}
		slices.Sort(slugs)
		slugs, err = b.visibleOnly(ctx, slices.Compact(slugs))
		if err != nil {
			return err
		}
		b.mentions[c.ID] = slugs
	}
	b.links = make([]Edge, 0, len(links))
	for _, l := range links {
		b.links = append(b.links, l)
	}
	slices.SortFunc(b.links, func(x, y Edge) int {
		return cmp.Or(
			cmp.Compare(x.Source, y.Source),
			cmp.Compare(x.Target, y.Target),
			cmp.Compare(x.Relation, y.Relation),
		)
	})
	return nil
}

// selectEntities ranks mentioned entities by mention count, keeps as many
// as the remaining node budget allows and resolves their display names.
func (b *builder) selectEntities(ctx context.Context) error {
	counts := make(map[string]int)
	for _, c := range b.objects {
		for _, s := range b.mentions[c.ID] {
			counts[s]++
		}
	}
	ents := make([]entityNode, 0, len(counts))
	for s, n := range counts {
		ents = append(ents, entityNode{slug: s, count: n})
	}
	slices.SortFunc(ents, func(x, y entityNode) int {
		return cmp.Or(cmp.Compare(y.count, x.count), cmp.Compare(x.slug, y.slug))
	})
	budget := b.opts.MaxNodes - 1 - len(b.objects)
	if len(ents) > budget {
		ents = ents[:max(budget, 0)]
		b.truncated = true
	}
	b.keptEnt = make(map[string]bool, len(ents))
	for i := range ents {
		label, err := b.entityLabel(ctx, ents[i].slug)
		if err != nil {
			return err
		}
		ents[i].label = label
		b.keptEnt[ents[i].slug] = true
	}
	b.entities = ents
	return nil
}

// visibleOnly drops the slugs EntityVisible hides, in place. Every
// relation, count and cap is derived from the mentions it leaves, so a
// hidden entity cannot surface anywhere in the document. Without a
// predicate every slug is visible and no entity is looked up here.
func (b *builder) visibleOnly(ctx context.Context, slugs []string) ([]string, error) {
	if b.opts.EntityVisible == nil {
		return slugs, nil
	}
	if b.visible == nil {
		b.visible = make(map[string]bool)
	}
	out := slugs[:0]
	for _, s := range slugs {
		ok, seen := b.visible[s]
		if !seen {
			ent, err := b.entity(ctx, s)
			if err != nil {
				return nil, err
			}
			ok = b.opts.EntityVisible(ctx, s, ent)
			b.visible[s] = ok
		}
		if ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// entity looks slug up once per build; a slug without a stored record
// yields nil. Any other store error fails the build.
func (b *builder) entity(ctx context.Context, slug string) (*storage.Entity, error) {
	if ent, ok := b.stored[slug]; ok {
		return ent, nil
	}
	ent, err := b.src.Entities.Get(ctx, slug)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		ent = nil
	case err != nil:
		return nil, fmt.Errorf("searchgraph: get entity %s: %w", slug, err)
	}
	if b.stored == nil {
		b.stored = make(map[string]*storage.Entity)
	}
	b.stored[slug] = ent
	return ent, nil
}

func (b *builder) entityLabel(ctx context.Context, slug string) (string, error) {
	ent, err := b.entity(ctx, slug)
	if err != nil {
		return "", err
	}
	if ent != nil {
		if l := cleanLabel(ent.Title); l != "" {
			return l, nil
		}
	}
	return cleanLabel(slug), nil
}

// edges assembles every relation in keep order.
func (b *builder) edges(ctx context.Context) ([]Edge, error) {
	out := make([]Edge, 0, 2*len(b.objects)+len(b.links))
	for _, c := range b.objects {
		out = append(out, Edge{
			Source:   QueryNodeID,
			Target:   ObjectNodePrefix + c.ID,
			Relation: RelMatched,
			Directed: true,
			Metadata: EdgeMetadata{Derivation: DerivationDerived, Weight: ptr(c.Breakdown.Total)},
		})
	}
	out = append(out, b.links...)
	for _, c := range b.objects {
		for _, s := range b.mentions[c.ID] {
			if !b.keptEnt[s] {
				continue
			}
			out = append(out, Edge{
				Source:   ObjectNodePrefix + c.ID,
				Target:   EntityNodePrefix + s,
				Relation: RelMentions,
				Directed: true,
				Metadata: EdgeMetadata{Derivation: DerivationStored},
			})
		}
	}
	out = append(out, b.coMentions()...)
	if b.opts.Similar {
		sim, err := b.similar(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, sim...)
	}
	return out, nil
}

// document assembles the final JGF document.
func (b *builder) document(edges []Edge) *Document {
	tr := b.tr
	nodes := make(map[string]Node, 1+len(b.objects)+len(b.entities))
	nodes[QueryNodeID] = Node{Label: tr.Query, Metadata: NodeMetadata{Kind: KindQuery}}
	for _, c := range b.objects {
		nodes[ObjectNodePrefix+c.ID] = objectNode(c)
	}
	for _, e := range b.entities {
		nodes[EntityNodePrefix+e.slug] = Node{
			Label:    e.label,
			Metadata: NodeMetadata{Kind: KindEntity, Slug: e.slug, MentionCount: e.count},
		}
	}
	if edges == nil {
		edges = []Edge{}
	}
	tc := tr.Counts
	return &Document{Graph: Graph{
		ID:       GraphID,
		Type:     GraphType,
		Label:    tr.Query,
		Directed: true,
		Metadata: GraphMetadata{
			Vocabulary:     Vocabulary,
			GeneratedAt:    b.opts.Now().UTC().Format(time.RFC3339),
			Query:          tr.Query,
			FTSQuery:       tr.FTSQuery,
			Mode:           string(tr.Mode),
			VectorError:    tr.VectorError,
			VectorModel:    tr.VectorModel,
			SemanticStatus: string(tr.SemanticStatus),
			FTSPool:        tr.FTSPool,
			VectorPool:     tr.VectorPool,
			RRFK:           tr.RRFK,
			Limit:          tr.Limit,
			FTSWeight:      tr.FTSWeight,
			VectorWeight:   tr.VectorWeight,
			Threshold:      tr.Threshold,
			Weights: Weights{
				MentionBoost:    tr.Weights.MentionBoost,
				MaxMentionBoost: tr.Weights.MaxMentionBoost,
				DirectBacklink:  tr.Weights.DirectBacklink,
				HopBacklink:     tr.Weights.HopBacklink,
				WordOverlap:     tr.Weights.WordOverlap,
			},
			Counts: Counts{
				FTSHits:      tc.FTSHits,
				VectorHits:   tc.VectorHits,
				Candidates:   tc.Candidates,
				Both:         tc.Both,
				FTSOnly:      tc.FTSOnly,
				VectorOnly:   tc.VectorOnly,
				Returned:     tc.Returned,
				CutLimit:     tc.CutLimit,
				CutThreshold: tc.CutThreshold,
				Nodes:        len(nodes),
				Edges:        len(edges),
				Entities:     len(b.entities),
			},
			Truncated: b.truncated,
			Caps:      Caps{MaxNodes: b.opts.MaxNodes, MaxEdges: b.opts.MaxEdges},
		},
		Nodes: nodes,
		Edges: edges,
	}}
}

func objectNode(c *service.TraceCandidate) Node {
	bd := c.Breakdown
	md := NodeMetadata{
		Kind:       KindObject,
		ObjectID:   c.ID,
		Stage:      string(c.Stage),
		Legs:       string(c.Legs),
		Rank:       c.Rank,
		FTSRank:    c.FTSRank,
		VectorRank: c.VectorRank,
		FTSRaw:     c.FTSRaw,
		VectorRaw:  c.VectorRaw,
		RRF:        ptr(c.RRF),
		Score: &Score{
			FTS:            bd.FTS,
			Vector:         bd.Vector,
			MentionBoost:   bd.MentionBoost,
			GraphRelevance: bd.GraphRelevance,
			WordOverlap:    bd.WordOverlap,
			Total:          bd.Total,
		},
	}
	if o := c.Object; o != nil {
		md.ObjectType = o.Type
		md.Source = o.Source
		if !o.CreatedAt.IsZero() {
			md.CreatedAt = o.CreatedAt.UTC().Format(time.RFC3339)
		}
	}
	return Node{Label: objectLabel(c.ID, c.Object), Metadata: md}
}

func ptr[T any](v T) *T { return &v }
