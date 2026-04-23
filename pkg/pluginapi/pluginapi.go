// Package pluginapi is the stable public API surface for ctxt plugins.
//
// External plugin modules import only this package. It has no imports from
// internal/ so third-party plugins can depend on it without violating Go's
// internal/ visibility rules.
package pluginapi

import (
	"context"
	"encoding/json"
	"time"

	"hop.top/uri"
)

// ─── Event bus ───────────────────────────────────────────────────────────────

// Event is a CloudEvents v1.0 event carried on the Bus.
type Event struct {
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	SpecVersion     string          `json:"specversion"`
	Type            string          `json:"type"`
	DataContentType string          `json:"datacontenttype,omitempty"`
	Time            time.Time       `json:"time"`
	Data            json.RawMessage `json:"data,omitempty"`
}

// Bus is the interface plugins may use to publish or subscribe to events.
type Bus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(eventType string, handler func(ctx context.Context, e Event) error)
	Close() error
}

// ─── Storage types ───────────────────────────────────────────────────────────

// Alias represents a human-readable name that resolves to a knowledge object ID.
type Alias struct {
	Alias     string    `json:"alias"`
	ObjectID  string    `json:"object_id"`
	Scope     string    `json:"scope"`   // "global" | "profile"
	Profile   string    `json:"profile"` // empty for global
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AliasFilter restricts alias listing results.
type AliasFilter struct {
	ObjectID string
	Scope    string
	Profile  string
}

// AliasStore is the persistence interface for aliases.
type AliasStore interface {
	Create(ctx context.Context, a *Alias) error
	Resolve(ctx context.Context, alias, profile string) (string, error)
	List(ctx context.Context, filter AliasFilter) ([]*Alias, error)
	Delete(ctx context.Context, alias, scope, profile string) error
}

// StorageDriver is the narrow view of the storage backend exposed to plugins.
// Plugins receive this through Deps.Store. Only the sub-stores that plugins
// are permitted to access are included here.
type StorageDriver interface {
	Aliases() AliasStore
}

// ─── KnowledgeObject ─────────────────────────────────────────────────────────

// KnowledgeObject is the central data structure representing an ingested piece
// of knowledge. It is the type passed through pipeline steps.
type KnowledgeObject struct {
	ID                 string         `json:"id"`
	Type               string         `json:"type"`
	Subtype            string         `json:"subtype,omitempty"`
	RawContent         string         `json:"raw_content"`
	TextContent        string         `json:"text_content,omitempty"`
	ContentType        string         `json:"content_type,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	Summaries          []string       `json:"summaries,omitempty"`
	Sections           []Section      `json:"sections,omitempty"`
	Tags               []Tag          `json:"tags,omitempty"`
	Mentions           []uri.URI      `json:"mentions,omitempty"`
	Decisions          []Decision     `json:"decisions,omitempty"`
	Tasks              []Task         `json:"tasks,omitempty"`
	Embeddings         []float32      `json:"embeddings,omitempty"`
	Pipeline           string         `json:"pipeline,omitempty"`
	Source             string         `json:"source,omitempty"`
	RegistryInfluences []string       `json:"registry_influences,omitempty"`
	Plugins            map[string]any `json:"plugins,omitempty"`
	ContentHash        string         `json:"content_hash,omitempty"`
	SourceKey          string         `json:"source_key,omitempty"` // external dedup key (Slack ts, tweet ID, etc.)
	Status             string         `json:"status,omitempty"`
	InboxNote          string         `json:"inbox_note,omitempty"`
	ReinforcementCount int            `json:"reinforcement_count,omitempty"`
	LastReinforcedAt   *time.Time     `json:"last_reinforced_at,omitempty"`
	RemindAt           *time.Time     `json:"remind_at,omitempty"`
	RemindedAt         *time.Time     `json:"reminded_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	FTSIndexed         bool           `json:"fts_indexed"`
	VectorIndexed      bool           `json:"vector_indexed"`
	ProfileID          string         `json:"profile_id,omitempty"` // owning profile; empty = global
	AttachmentIDs      []string       `json:"attachment_ids,omitempty"`
	Graph              *ObjectGraph   `json:"graph,omitempty"`
}

// Draft is an alias for KnowledgeObject being progressively enriched.
type Draft = KnowledgeObject

// Section represents a structural section of a knowledge object.
type Section struct {
	Title    string         `json:"title"`
	Content  string         `json:"content"`
	Order    int            `json:"order"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Tag represents a label attached to a knowledge object.
type Tag struct {
	Label  string  `json:"label"`
	Weight float64 `json:"weight,omitempty"`
	Source string  `json:"source,omitempty"`
}

// Decision represents an extracted decision from content.
type Decision struct {
	Title  string `json:"title"`
	Status string `json:"status"`
	Impact string `json:"impact"`
}

// Task represents an extracted task from content.
type Task struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

// ─── Graph-canonical types ────────────────────────────────────────────────────

// GraphNode is a typed node within an object's intra-object graph.
type GraphNode struct {
	ID       string         `json:"id"`              // NewNodeID(objectID, nodeType, ordinal)
	NodeType GraphNodeType  `json:"node_type"`       // NodeType* constant
	Label    string         `json:"label,omitempty"` // human-readable label
	Content  string         `json:"content,omitempty"`
	Order    int            `json:"order"`           // ordinal position; zero is valid
	Metadata map[string]any `json:"metadata,omitempty"`
}

// GraphEdge is a directed edge between two nodes within the same object graph.
// Inter-object edges MUST go to the edges table (ADR-049); this is intra-object only.
type GraphEdge struct {
	ID       string  `json:"id"`
	FromID   string  `json:"from_id"`
	ToID     string  `json:"to_id"`
	EdgeType GraphEdgeType `json:"edge_type"` // EdgeType* constant
	Weight   float64 `json:"weight,omitempty"`
}

// ObjectGraph holds typed nodes and edges for a single KnowledgeObject.
// Write source of truth for graph-canonical KOs.
type ObjectGraph struct {
	Nodes []GraphNode `json:"nodes,omitempty"`
	Edges []GraphEdge `json:"edges,omitempty"`
}

// FindNode returns the node with the given ID, or nil.
func (g *ObjectGraph) FindNode(id string) *GraphNode {
	if g == nil {
		return nil
	}
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

// DocumentProjection is a derived human-facing view of a KnowledgeObject.
// Derived by projection.ProjectDocument; not stored directly.
type DocumentProjection struct {
	Title    string    `json:"title,omitempty"`
	Body     string    `json:"body,omitempty"`
	Sections []Section `json:"sections,omitempty"`
}

// IndexProjection is a derived search-index view of a KnowledgeObject.
// Derived by projection.ProjectIndex; not stored directly.
type IndexProjection struct {
	FTSBody string `json:"fts_body,omitempty"`
	// Tags includes weight and source metadata — richer than ADR pseudocode []string.
	Tags          []Tag    `json:"tags,omitempty"`
	Mentions      []string `json:"mentions,omitempty"`
	EmbeddingText string   `json:"embedding_text,omitempty"`
}

// ─── Pipeline step contract ───────────────────────────────────────────────────

// StepContract declares what a pipeline step requires and produces.
type StepContract struct {
	Requires     []string // KnowledgeObject fields the step reads
	Produces     []string // KnowledgeObject fields the step writes
	Capabilities []string // subsystem requirements: "ocr", "vision", "llm", etc.
}

// PipelineStep is a single transformation that enriches a KnowledgeObject.
// Plugins that contribute pipeline steps implement this interface.
type PipelineStep interface {
	Name() string
	Contract() StepContract
	Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error)
}

// ─── Plugin contract ─────────────────────────────────────────────────────────

// Deps carries shared dependencies injected at plugin initialisation.
type Deps struct {
	Bus   Bus
	Store StorageDriver
}

// Plugin is the interface every ctxt plugin must satisfy.
type Plugin interface {
	// Name returns the unique plugin identifier (matches config key).
	Name() string
	// Version returns the plugin's semver string.
	Version() string
	// Init is called once at startup with the plugin's config block and shared deps.
	Init(ctx context.Context, cfg map[string]interface{}, deps Deps) error
	// PipelineSteps returns zero or more pipeline steps to register.
	PipelineSteps() []PipelineStep
	// Close is called on graceful shutdown.
	Close(ctx context.Context) error
}

// PostIngestHook is implemented by plugins that want to run after an object is created.
type PostIngestHook interface {
	Plugin
	PostIngest(ctx context.Context, obj *KnowledgeObject) error
}

// ─── Output generator contract ────────────────────────────────────────────────

// OutputOptions carries rendering preferences passed to an OutputGenerator.
type OutputOptions struct {
	// Destination is an optional filesystem path hint (e.g. vault directory).
	// Generators that write files use this as a base path.
	// Empty means the caller handles writing the returned bytes itself.
	Destination string
	// Extra holds generator-specific settings sourced from plugin config.
	Extra map[string]any
}

// OutputGenerator is implemented by plugins that can render a KnowledgeObject
// to a specific output format (e.g. Obsidian Markdown, Notion export).
type OutputGenerator interface {
	Plugin
	// Name returns the format identifier matched against --format (e.g. "obsidian-md").
	GeneratorName() string
	// Accepts reports whether this generator can handle obj.
	Accepts(obj KnowledgeObject) bool
	// Generate renders obj and returns the formatted bytes.
	Generate(ctx context.Context, obj KnowledgeObject, opts OutputOptions) ([]byte, error)
}

// AliasResolver is implemented by plugins that can resolve aliases to object IDs.
type AliasResolver interface {
	Plugin
	// ResolveID resolves an alias (or passes through an ID unchanged).
	// Returns the canonical object ID or the input unchanged if not an alias.
	ResolveID(ctx context.Context, idOrAlias, profile string) (string, error)
}
