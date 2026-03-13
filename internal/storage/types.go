package storage

import "time"

// BlobMeta describes metadata for a stored blob.
type BlobMeta struct {
	ContentType string            `json:"content_type"`
	Size        int64             `json:"size"`
	ContentHash string            `json:"content_hash"`
	Filename    string            `json:"filename,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

// BlobInfo describes a blob in a listing.
type BlobInfo struct {
	Key       string    `json:"key"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KnowledgeObject is the central data structure representing an ingested piece of knowledge.
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
	Mentions           []string       `json:"mentions,omitempty"`
	Decisions          []Decision     `json:"decisions,omitempty"`
	Tasks              []Task         `json:"tasks,omitempty"`
	Embeddings         []float32      `json:"embeddings,omitempty"`
	Pipeline           string         `json:"pipeline,omitempty"`
	Source             string         `json:"source,omitempty"`
	RegistryInfluences []string       `json:"registry_influences,omitempty"`
	Plugins            map[string]any `json:"plugins,omitempty"`
	ContentHash        string         `json:"content_hash,omitempty"`
	Status             string         `json:"status,omitempty"`      // "active" | "inbox" | "discarded"
	InboxNote          string         `json:"inbox_note,omitempty"`
	ReinforcementCount int            `json:"reinforcement_count,omitempty"`
	LastReinforcedAt   *time.Time     `json:"last_reinforced_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	FTSIndexed         bool           `json:"fts_indexed"`
	VectorIndexed      bool           `json:"vector_indexed"`
}

// Draft is a KnowledgeObject being progressively enriched (ADR-053).
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

// Entity represents a named entity in the knowledge graph.
type Entity struct {
	Slug        string         `json:"slug"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Namespace   string         `json:"namespace,omitempty"`
	Aliases     []string       `json:"aliases,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Edge represents a relationship between two nodes in the knowledge graph (ADR-049).
type Edge struct {
	ID        string         `json:"id"`
	FromType  string         `json:"from_type"`
	FromID    string         `json:"from_id"`
	ToType    string         `json:"to_type"`
	ToID      string         `json:"to_id"`
	EdgeType  string         `json:"edge_type"`
	Weight    float64        `json:"weight"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// ObjectFilter specifies criteria for listing knowledge objects.
type ObjectFilter struct {
	Type     string
	Subtype  string
	Tag      string
	Mention  string
	Pipeline string
	After    *time.Time
	Before   *time.Time
	Limit    int
	Offset   int
	Sort     string // "created_at", "updated_at"
	Dir      string // "asc", "desc"
	Status   string // "" → default to "active"; "inbox"; "discarded"; "all"
}

// EntityFilter specifies criteria for listing entities.
type EntityFilter struct {
	Namespace string
	Limit     int
	Offset    int
}

// JobStatus represents the state of a job.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

// Job represents an ingestion job.
type Job struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Status      JobStatus  `json:"status"`
	Payload     string     `json:"payload"`
	Pipeline    string     `json:"pipeline"`
	Source      string     `json:"source"`
	ResultID    string     `json:"result_id"`
	Error       string     `json:"error"`
	RetryCount  int        `json:"retry_count"`
	MaxRetries  int        `json:"max_retries"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// JobFilter specifies criteria for listing jobs.
type JobFilter struct {
	Status JobStatus
	Type   string
	Limit  int
	Offset int
}

// Pipeline represents an ingestion pipeline configuration.
type Pipeline struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Steps       []StepRef      `json:"steps"`
	IsBuiltIn   bool           `json:"is_built_in"`
	Archived    bool           `json:"archived"`
	Sandbox     *SandboxConfig `json:"sandbox,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// StepRef references a step with its configuration.
type StepRef struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config,omitempty"`
}

// SandboxConfig defines isolation settings for pipeline execution.
type SandboxConfig struct {
	Enabled        bool                     `json:"enabled"`
	ResourceLimits *ResourceLimitsConfig    `json:"resource_limits,omitempty"`
	Network        bool                     `json:"network"`
	Filesystem     *FilesystemSandboxConfig `json:"filesystem,omitempty"`
	IsolationLevel string                   `json:"isolation_level"` // "process" or "container"
}

// ResourceLimitsConfig defines resource constraints for sandboxed execution.
type ResourceLimitsConfig struct {
	MaxMemory string `json:"max_memory"` // e.g., "512MB"
	MaxCPU    string `json:"max_cpu"`    // e.g., "2.0"
	Timeout   string `json:"timeout"`    // e.g., "30s"
}

// FilesystemSandboxConfig defines filesystem access restrictions.
type FilesystemSandboxConfig struct {
	ReadOnly     bool `json:"read_only"`
	WriteAllowed bool `json:"write_allowed"`
}

// RegisteredStep represents an installed step available for pipelines.
type RegisteredStep struct {
	Name        string        `json:"name"`
	Source      string        `json:"source"` // "builtin", "local:<path>", "registry:<url>"
	Path        string        `json:"path"`
	Metadata    *StepMetadata `json:"metadata"`
	InstalledAt time.Time     `json:"installed_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// StepMetadata contains information extracted from SKILL.md.
type StepMetadata struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	License        string         `json:"license"`
	Version        string         `json:"version"`
	Author         string         `json:"author"`
	ConfigSchema   map[string]any `json:"config_schema,omitempty"`
	SupportedLangs []string       `json:"supported_languages,omitempty"`
}

// RegistryManifest represents a registry's step manifest.
type RegistryManifest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Steps       []ManifestStep `json:"steps"`
	Supports    map[string]any `json:"supports,omitempty"`
}

// ManifestStep describes a step available in a registry.
type ManifestStep struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	License string `json:"license"`
	Version string `json:"version"`
}

// RegistryCache stores fetched registry manifests with update tracking.
type RegistryCache struct {
	RegistryURL string            `json:"registry_url"`
	Manifest    *RegistryManifest `json:"manifest"`
	LastFetched time.Time         `json:"last_fetched"`
	ETag        string            `json:"etag"`
	AutoUpdate  bool              `json:"auto_update"`
}

// SystemReminder represents a notification for the user.
type SystemReminder struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // "update", "alert", "info"
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	ActionURL string    `json:"action_url,omitempty"`
	Dismissed bool      `json:"dismissed"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PipelineFilter specifies criteria for listing pipelines.
type PipelineFilter struct {
	Name            string
	IncludeArchived bool
	OnlyArchived    bool
}

// Feed represents an RSS/Atom/JSON Feed subscription.
type Feed struct {
	ID           string     `json:"id"`
	URL          string     `json:"url"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	SiteURL      string     `json:"site_url"`
	Format       string     `json:"format"` // rss2.0, atom1.0, json1.1
	Status       string     `json:"status"` // active, paused, error, suspended, gone
	SyncInterval string     `json:"sync_interval"`
	ETag         string     `json:"etag"`
	LastModified string     `json:"last_modified"`
	LastSync     *time.Time `json:"last_sync,omitempty"`
	ErrorCount   int        `json:"error_count"`
	LastError    string     `json:"last_error"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// FeedItem tracks an ingested item from a feed.
type FeedItem struct {
	ID         string    `json:"id"`
	FeedID     string    `json:"feed_id"`
	GUID       string    `json:"guid"`
	Link       string    `json:"link"`
	ObjectID   string    `json:"object_id"`
	IngestedAt time.Time `json:"ingested_at"`
}

// FeedFilter specifies criteria for listing feeds.
type FeedFilter struct {
	Status string
	Limit  int
	Offset int
}

// Batch represents a batch import operation.
type Batch struct {
	ID           string       `json:"batch_id"`
	Format       string       `json:"format"`
	TotalRecords int          `json:"total"`
	Completed    int          `json:"completed"`
	Failed       int          `json:"failed"`
	Status       string       `json:"status"` // processing, completed, partial, dry_run_complete
	Errors       []BatchError `json:"errors,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// BatchError records a per-line error in batch import.
type BatchError struct {
	Line  int    `json:"line"`
	Error string `json:"error"`
}

// BatchFilter specifies criteria for listing batches.
type BatchFilter struct {
	Status string
	Limit  int
	Offset int
}

// ImportRecord represents a single record in a batch import file.
type ImportRecord struct {
	Content  string         `json:"content"`
	Type     string         `json:"type,omitempty"`
	Tags     []string       `json:"tags,omitempty"`
	Source   string         `json:"source,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// DetectorKind classifies how a Detector matches content.
type DetectorKind string

const (
	DetectorKindExtension   DetectorKind = "extension"
	DetectorKindURLPattern  DetectorKind = "url_pattern"
	DetectorKindContentTest DetectorKind = "content_test"
)

// DetectorRecord is a persisted detector configuration.
type DetectorRecord struct {
	ID           string       `json:"id"`
	Kind         DetectorKind `json:"kind"`
	Name         string       `json:"name"`          // human label
	PipelineName string       `json:"pipeline_name"` // which pipeline it routes to
	Pattern      string       `json:"pattern"`       // extension (.pdf), URL regex, or content snippet
	Priority     int          `json:"priority"`      // lower = higher priority (default 100)
	Enabled      bool         `json:"enabled"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// DetectorFilter for listing.
type DetectorFilter struct {
	Kind    DetectorKind
	Enabled *bool // nil = all, true = enabled only, false = disabled only
	Limit   int
	Offset  int
}

// ProximityFactors holds per-dimension proximity scores or weights.
type ProximityFactors struct {
	Semantic   float64 `json:"semantic"`
	Temporal   float64 `json:"temporal"`
	Entity     float64 `json:"entity"`
	Origin     float64 `json:"origin"`
	Behavioral float64 `json:"behavioral"`
}

// ProximityScore represents the computed proximity between two knowledge objects.
// ObjectA < ObjectB is enforced to ensure canonical ordering.
type ProximityScore struct {
	ObjectA    string           `json:"object_a"`
	ObjectB    string           `json:"object_b"`
	Score      float64          `json:"score"`
	Factors    ProximityFactors `json:"factors"`
	Weights    ProximityFactors `json:"weights"`
	ComputedAt time.Time        `json:"computed_at"`
}

// ProximityStats summarises aggregate statistics about the proximity index.
type ProximityStats struct {
	TotalPairs      int64   `json:"total_pairs"`
	AvgScore        float64 `json:"avg_score"`
	MaxScore        float64 `json:"max_score"`
	HighProximity   int64   `json:"high_proximity"`   // score >= 0.7
	MediumProximity int64   `json:"medium_proximity"` // score 0.4–0.7
	LowProximity    int64   `json:"low_proximity"`    // score 0.2–0.4
}

// WatchConfig is the persisted configuration for a directory watch.
type WatchConfig struct {
	ID               string    `json:"id"`
	Path             string    `json:"path"`
	Mode             string    `json:"mode"`              // "generic" | "obsidian" | "logseq"
	IncludePatterns  []string  `json:"include_patterns"`
	ExcludePatterns  []string  `json:"exclude_patterns"`
	DebounceMS       int       `json:"debounce_ms"`
	Status           string    `json:"status"`            // "active" | "paused"
	PipelineOverride string    `json:"pipeline_override"`
	LastError        string    `json:"last_error"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// WatchFileRecord tracks the last-known state of a file under a watch.
type WatchFileRecord struct {
	WatchID     string    `json:"watch_id"`
	FilePath    string    `json:"file_path"`
	ObjectID    string    `json:"object_id"`
	ContentHash string    `json:"content_hash"`
	LastSeen    time.Time `json:"last_seen"`
}
