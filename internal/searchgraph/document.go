// Package searchgraph turns a hybrid search trace into a graph document in
// JSON Graph Format (JGF) v2.1, single-graph form: {"graph": {...}}.
//
// The document is a shared contract between this producer and its
// consumers (viewers, exporters). Everything ctxt-specific lives under
// "metadata" objects, keys are snake_case, numbers are JSON numbers and
// absent values are omitted rather than null.
//
// Node ids carry a kind prefix so kinds never collide:
//
//	query            the single query node
//	obj:<object id>  a search candidate
//	ent:<slug>       an entity mentioned by a candidate
//
// Edge relations:
//
//	relation      source -> target   directed  derivation  weight
//	matched       query  -> object   true      derived     score.total
//	mentions      object -> entity   true      stored      -
//	<link type>   object -> object   true*     stored      -
//	co_mention    object -> object   false     derived     shared entity count
//	similar       object -> object   false     derived     cosine similarity
//
// similar compares stored vectors of metadata.vector_model, the default
// embedding model the search read; an object's similarity to another is
// that of their closest chunk pair.
//
// Link types are the forward names only (extends, contradicts, supersedes,
// supports, related-to, derived-from); *related-to is undirected. Stored
// forward/inverse pairs collapse to one forward edge. Undirected edges
// always have source < target.
//
// Every string (labels included) is untrusted text: consumers must escape
// it before rendering as HTML. The encoder here deliberately does not.
package searchgraph

// Document vocabulary constants.
const (
	// Vocabulary identifies this document shape; bumped on breaking change.
	Vocabulary = "ctxt.search-graph/v1"
	// GraphID is the fixed graph.id.
	GraphID = "search-graph"
	// GraphType is the fixed graph.type.
	GraphType = "ctxt.search-graph"

	// QueryNodeID is the id of the single query node.
	QueryNodeID = "query"
	// ObjectNodePrefix prefixes object node ids.
	ObjectNodePrefix = "obj:"
	// EntityNodePrefix prefixes entity node ids.
	EntityNodePrefix = "ent:"
)

// Node kinds (node.metadata.kind).
const (
	KindQuery  = "query"
	KindObject = "object"
	KindEntity = "entity"
)

// Edge relations beyond the stored link types.
const (
	RelMatched   = "matched"
	RelMentions  = "mentions"
	RelCoMention = "co_mention"
	RelSimilar   = "similar"
)

// Edge derivations (edge.metadata.derivation).
const (
	DerivationStored  = "stored"
	DerivationDerived = "derived"
)

// Document is the top-level JGF single-graph document.
type Document struct {
	Graph Graph `json:"graph"`
}

// Graph is the JGF graph object.
type Graph struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Label    string          `json:"label"`
	Directed bool            `json:"directed"`
	Metadata GraphMetadata   `json:"metadata"`
	Nodes    map[string]Node `json:"nodes"`
	Edges    []Edge          `json:"edges"`
}

// GraphMetadata carries the search parameters and graph summary.
type GraphMetadata struct {
	Vocabulary string `json:"vocabulary"`
	// GeneratedAt is RFC 3339 UTC.
	GeneratedAt string `json:"generated_at"`
	Query       string `json:"query"`
	FTSQuery    string `json:"fts_query"`
	// Mode is hybrid | fts_only | fts_fallback.
	Mode string `json:"mode"`
	// VectorError is set only when the vector leg failed.
	VectorError string `json:"vector_error,omitempty"`
	// VectorModel is the default embedding model the vector leg read (and
	// similar edges compare); omitted when there is no default model.
	VectorModel string `json:"vector_model,omitempty"`
	// SemanticStatus is the vector leg's status: ok, or why it
	// contributed no hits (no_default_model, provider_error, ...).
	SemanticStatus string  `json:"semantic_status,omitempty"`
	FTSPool        int     `json:"fts_pool"`
	VectorPool     int     `json:"vector_pool"`
	RRFK           int     `json:"rrf_k"`
	Limit          int     `json:"limit"`
	FTSWeight      float64 `json:"fts_weight"`
	VectorWeight   float64 `json:"vector_weight"`
	Threshold      float64 `json:"threshold"`
	Weights        Weights `json:"weights"`
	Counts         Counts  `json:"counts"`
	// Truncated is true when a node or edge cap dropped anything.
	Truncated bool `json:"truncated"`
	Caps      Caps `json:"caps"`
}

// Weights mirrors the reranker weights in effect for the search.
type Weights struct {
	MentionBoost    float64 `json:"mention_boost"`
	MaxMentionBoost float64 `json:"max_mention_boost"`
	DirectBacklink  float64 `json:"direct_backlink"`
	HopBacklink     float64 `json:"hop_backlink"`
	WordOverlap     float64 `json:"word_overlap"`
}

// Counts holds the trace tallies (pre-truncation, as the search saw them)
// plus the emitted graph sizes (post-truncation).
type Counts struct {
	FTSHits      int `json:"fts_hits"`
	VectorHits   int `json:"vector_hits"`
	Candidates   int `json:"candidates"`
	Both         int `json:"both"`
	FTSOnly      int `json:"fts_only"`
	VectorOnly   int `json:"vector_only"`
	Returned     int `json:"returned"`
	CutLimit     int `json:"cut_limit"`
	CutThreshold int `json:"cut_threshold"`
	// Nodes, Edges and Entities count what the document contains.
	Nodes    int `json:"nodes"`
	Edges    int `json:"edges"`
	Entities int `json:"entities"`
}

// Caps records the effective node and edge caps.
type Caps struct {
	MaxNodes int `json:"max_nodes"`
	MaxEdges int `json:"max_edges"`
}

// Node is a JGF node. The map key in Graph.Nodes is its id.
type Node struct {
	Label    string       `json:"label"`
	Metadata NodeMetadata `json:"metadata"`
}

// NodeMetadata is shared by all node kinds; fields that do not apply to a
// kind stay empty and are omitted.
type NodeMetadata struct {
	// Kind is query | object | entity.
	Kind string `json:"kind"`

	// Object nodes.
	ObjectID string `json:"object_id,omitempty"`
	// Stage is returned | cut_limit | cut_threshold.
	Stage string `json:"stage,omitempty"`
	// Legs is fts | vector | both.
	Legs string `json:"legs,omitempty"`
	// Rank is the final 1-based rank.
	Rank int `json:"rank,omitempty"`
	// FTSRank and VectorRank are 1-based; omitted when absent from a leg.
	FTSRank    int `json:"fts_rank,omitempty"`
	VectorRank int `json:"vector_rank,omitempty"`
	// FTSRaw is driver-specific (SQLite bm25, lower is better; Postgres
	// ts_rank_cd). VectorRaw is cosine similarity under vector_model.
	FTSRaw    *float64 `json:"fts_raw,omitempty"`
	VectorRaw *float64 `json:"vector_raw,omitempty"`
	// RRF is the fused leg score before reranking.
	RRF        *float64 `json:"rrf,omitempty"`
	Score      *Score   `json:"score,omitempty"`
	ObjectType string   `json:"object_type,omitempty"`
	Source     string   `json:"source,omitempty"`
	// CreatedAt is RFC 3339 UTC.
	CreatedAt string `json:"created_at,omitempty"`

	// Entity nodes.
	Slug string `json:"slug,omitempty"`
	// MentionCount counts object nodes in this graph that mention the entity.
	MentionCount int `json:"mention_count,omitempty"`
}

// Score is the per-signal score breakdown of an object candidate.
type Score struct {
	FTS            float64 `json:"fts"`
	Vector         float64 `json:"vector"`
	MentionBoost   float64 `json:"mention_boost"`
	GraphRelevance float64 `json:"graph_relevance"`
	WordOverlap    float64 `json:"word_overlap"`
	Total          float64 `json:"total"`
}

// Edge is a JGF edge.
type Edge struct {
	Source   string       `json:"source"`
	Target   string       `json:"target"`
	Relation string       `json:"relation"`
	Directed bool         `json:"directed"`
	Metadata EdgeMetadata `json:"metadata"`
}

// EdgeMetadata carries the derivation and optional weight of an edge.
type EdgeMetadata struct {
	// Derivation is stored | derived.
	Derivation string   `json:"derivation"`
	Weight     *float64 `json:"weight,omitempty"`
}
