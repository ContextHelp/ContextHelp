package searchgraph

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/ideacrafterslabs/ctxt/internal/graph"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type linkKey struct{ source, target, relation string }

// collapseLink maps one stored object->object edge to its canonical graph
// edge. Forward link types keep their direction; inverse types (e.g.
// extended-by) flip to the forward name and direction, so a stored
// forward/inverse pair yields the same edge twice and dedupes. Symmetric
// types (related-to) are undirected with source < target. Non-link edge
// types report ok=false.
func collapseLink(fromID, toID, edgeType string) (Edge, bool) {
	lt := graph.LinkType(edgeType)
	if !graph.ValidLinkType(lt) {
		return Edge{}, false
	}
	src, tgt := ObjectNodePrefix+fromID, ObjectNodePrefix+toID
	directed := true
	switch {
	case graph.IsSymmetric(lt):
		directed = false
		if tgt < src {
			src, tgt = tgt, src
		}
	case !slices.Contains(graph.UserLinkTypes, lt):
		inv, err := graph.InverseLinkType(lt)
		if err != nil {
			return Edge{}, false
		}
		lt = inv
		src, tgt = tgt, src
	}
	return Edge{
		Source:   src,
		Target:   tgt,
		Relation: string(lt),
		Directed: directed,
		Metadata: EdgeMetadata{Derivation: DerivationStored},
	}, true
}

type pairKey struct{ a, b string } // node ids, a < b

func orderedPair(x, y string) pairKey {
	if y < x {
		x, y = y, x
	}
	return pairKey{x, y}
}

// coMentions emits one undirected edge per pair of kept objects that share
// at least one mentioned entity, weighted by the shared entity count. All
// stored mentions of the pair count, including entities dropped by the
// node cap, but never entities Options.EntityVisible hides: loadEdges has
// already removed those.
func (b *builder) coMentions() []Edge {
	byEntity := make(map[string][]string)
	for _, c := range b.objects {
		for _, s := range b.mentions[c.ID] {
			byEntity[s] = append(byEntity[s], ObjectNodePrefix+c.ID)
		}
	}
	shared := make(map[pairKey]int)
	for _, objs := range byEntity {
		for i := range objs {
			for j := i + 1; j < len(objs); j++ {
				shared[orderedPair(objs[i], objs[j])]++
			}
		}
	}
	out := make([]Edge, 0, len(shared))
	for p, n := range shared {
		out = append(out, undirectedDerived(p, RelCoMention, float64(n)))
	}
	sortByWeight(out)
	return out
}

// similar emits one undirected edge per pair of kept objects whose stored
// embeddings under the trace's vector model (the default model the search
// read) reach the cosine threshold. A pair's similarity is that of its
// closest chunk pair, matching the store's object-level search hits.
// Objects without a row under that model get no similar edge, and a trace
// without a vector model (no default model) yields none.
func (b *builder) similar(ctx context.Context) ([]Edge, error) {
	model := b.tr.VectorModel
	if model == "" {
		return nil, nil
	}
	ids := make([]string, len(b.objects))
	for i, c := range b.objects {
		ids[i] = c.ID
	}
	vecs, err := b.src.Embeddings.EmbeddingsByID(ctx, model, ids)
	if err != nil {
		return nil, fmt.Errorf("searchgraph: read embeddings (%s): %w", model, err)
	}
	var out []Edge
	for i, x := range ids {
		vx := vecs[x]
		if len(vx) == 0 {
			continue
		}
		for _, y := range ids[i+1:] {
			cos, ok := closestChunks(vx, vecs[y])
			if !ok || cos < b.opts.SimilarThreshold {
				continue
			}
			p := orderedPair(ObjectNodePrefix+x, ObjectNodePrefix+y)
			out = append(out, undirectedDerived(p, RelSimilar, cos))
		}
	}
	sortByWeight(out)
	return out, nil
}

// closestChunks returns the highest cosine similarity over all chunk
// pairs of x and y; ok is false when no pair is comparable (no chunks,
// differing dimensions or zero magnitude).
func closestChunks(x, y []storage.ObjectVector) (float64, bool) {
	best, found := 0.0, false
	for _, cx := range x {
		for _, cy := range y {
			if len(cx.Vector) != len(cy.Vector) {
				continue
			}
			if cos, ok := cosine(cx.Vector, cy.Vector); ok && (!found || cos > best) {
				best, found = cos, true
			}
		}
	}
	return best, found
}

// cosine returns the cosine similarity of equal-length vectors; ok is
// false when either has zero magnitude.
func cosine(x, y []float32) (float64, bool) {
	var dot, nx, ny float64
	for i := range x {
		a, b := float64(x[i]), float64(y[i])
		dot += a * b
		nx += a * a
		ny += b * b
	}
	if nx == 0 || ny == 0 {
		return 0, false
	}
	// Clamp float rounding drift so identical vectors never exceed 1.
	return min(max(dot/(math.Sqrt(nx)*math.Sqrt(ny)), -1), 1), true
}

func undirectedDerived(p pairKey, rel string, w float64) Edge {
	return Edge{
		Source:   p.a,
		Target:   p.b,
		Relation: rel,
		Directed: false,
		Metadata: EdgeMetadata{Derivation: DerivationDerived, Weight: ptr(w)},
	}
}

// sortByWeight orders derived edges by weight (descending), then
// (source, target).
func sortByWeight(es []Edge) {
	slices.SortFunc(es, func(x, y Edge) int {
		return cmp.Or(
			cmp.Compare(*y.Metadata.Weight, *x.Metadata.Weight),
			cmp.Compare(x.Source, y.Source),
			cmp.Compare(x.Target, y.Target),
		)
	})
}
