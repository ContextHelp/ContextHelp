package storage

import "time"

// KnowledgeObject is the central data structure representing an ingested piece of knowledge.
type KnowledgeObject struct {
	ID                 string         `json:"id"`
	Type               string         `json:"type"`
	Subtype            string         `json:"subtype,omitempty"`
	RawContent         string         `json:"raw_content"`
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
