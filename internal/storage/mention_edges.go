package storage

import (
	"time"

	"github.com/google/uuid"
	uri "hop.top/cite/scheme"
)

// Edge types of the mention relation between an object and an entity.
const (
	// EdgeTypeMentions runs object → entity: the object mentions the entity.
	EdgeTypeMentions = "mentions"
	// EdgeTypeMentionedIn runs entity → object, the inverse of EdgeTypeMentions.
	EdgeTypeMentionedIn = "mentioned_in"
)

// mentionScheme and mentionNamespace are the scheme and namespace of a
// ctxt://entity/<slug> URI, the mention of an entity.
const (
	mentionScheme    = "ctxt"
	mentionNamespace = "entity"
)

// MentionSlug returns the slug (namespace/name) of the entity a mention
// names, the key of its entity row, or "" when u is not a ctxt://entity/
// URI.
func MentionSlug(u uri.URI) string {
	if u.Scheme != mentionScheme || u.Namespace != mentionNamespace {
		return ""
	}
	return u.ID
}

// MentionTarget is the node ID the edges of a mention name the entity
// by: the ToID of its mentions edge and the FromID of its mentioned_in
// edge. It is the mention's canonical ctxt://entity/<slug> URI string,
// never the bare slug: every writer stores it and every reader that
// looks an entity's edges up (EntityBacklinks) expects it.
func MentionTarget(u uri.URI) string { return u.String() }

// MentionEdges returns the edges recording that obj mentions each entity
// in targets: an object → entity mentions edge and, with reverse, the
// entity → object mentioned_in edge, each naming the entity by
// MentionTarget and carrying MentionProvenance for the mention. A target
// that repeats an earlier one adds no edge, and an obj without an ID has
// none. The edges are new rows with fresh IDs: EdgeStore.CreateMissing
// stores them and keeps an edge already stored for the same pair.
func MentionEdges(obj *KnowledgeObject, targets []uri.URI, reverse bool, now time.Time) []*Edge {
	if obj == nil || obj.ID == "" {
		return nil
	}
	seen := make(map[string]bool, len(targets))
	edges := make([]*Edge, 0, 2*len(targets))
	for _, u := range targets {
		target := MentionTarget(u)
		if seen[target] {
			continue
		}
		seen[target] = true
		edges = append(edges, &Edge{
			ID: uuid.NewString(), FromType: EdgeEndpointObject, FromID: obj.ID,
			ToType: EdgeEndpointEntity, ToID: target, EdgeType: EdgeTypeMentions,
			Weight: 1, Metadata: MentionProvenance(obj.Graph, target), CreatedAt: now,
		})
		if reverse {
			edges = append(edges, &Edge{
				ID: uuid.NewString(), FromType: EdgeEndpointEntity, FromID: target,
				ToType: EdgeEndpointObject, ToID: obj.ID, EdgeType: EdgeTypeMentionedIn,
				Weight: 1, Metadata: MentionProvenance(obj.Graph, target), CreatedAt: now,
			})
		}
	}
	return edges
}
