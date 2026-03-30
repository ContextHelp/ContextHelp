package storage

import (
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// KnowledgeObject is the central data structure representing an ingested piece of knowledge.
// The canonical definition lives in pkg/pluginapi; this alias keeps all internal packages working.
type KnowledgeObject = pluginapi.KnowledgeObject

// Draft is a KnowledgeObject being progressively enriched (ADR-053).
type Draft = pluginapi.KnowledgeObject

// Section represents a structural section of a knowledge object.
type Section = pluginapi.Section

// Tag represents a label attached to a knowledge object.
type Tag = pluginapi.Tag

// Decision represents an extracted decision from content.
type Decision = pluginapi.Decision

// Task represents an extracted task from content.
type Task = pluginapi.Task

// Graph-canonical types (ADR-063).
type ObjectGraph        = pluginapi.ObjectGraph
type GraphNode          = pluginapi.GraphNode
type GraphEdge          = pluginapi.GraphEdge
type DocumentProjection = pluginapi.DocumentProjection
type IndexProjection    = pluginapi.IndexProjection

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

// ContentStatus indicates how much of an entity's content is locally stored.
// 'full'         — complete definition present; default for locally-authored entities.
// 'thin'         — index-only stub synced from a registry in thin mode (slug/title/namespace only).
// 'pending_pull' — a pull has been requested but not yet completed.
type ContentStatus string

const (
	ContentStatusFull        ContentStatus = "full"
	ContentStatusThin        ContentStatus = "thin"
	ContentStatusPendingPull ContentStatus = "pending_pull"
)

// Entity represents a named entity in the knowledge graph.
type Entity struct {
	Slug          string        `json:"slug"`
	Title         string        `json:"title"`
	Description   string        `json:"description,omitempty"`
	Namespace     string        `json:"namespace,omitempty"`
	Aliases       []string      `json:"aliases,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	ContentStatus ContentStatus `json:"content_status,omitempty"`
	VersionHash   string        `json:"version_hash,omitempty"`
	RegistryURL   string        `json:"registry_url,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
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
	Type      string
	Subtype   string
	Tag       string
	Mention   string
	Pipeline  string
	ProfileID string // non-empty → restrict to this profile; empty → global objects only
	After     *time.Time
	Before    *time.Time
	Limit     int
	Offset    int
	Sort      string // "created_at", "updated_at"
	Dir       string // "asc", "desc"
	Status    string // "" → default to "active"; "inbox"; "discarded"; "all"
}

// EntityFilter specifies criteria for listing entities.
type EntityFilter struct {
	Namespace     string
	ContentStatus ContentStatus // if non-empty, filter by content_status
	Limit         int
	Offset        int
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

// RegistryCapabilities declares optional features supported by a registry.
// Used in the capability handshake to warn clients before using unsupported features.
type RegistryCapabilities struct {
	EntitySync   bool `json:"entity_sync"`   // registry exposes /entities/index
	Taxonomy     bool `json:"taxonomy"`      // registry exposes /taxonomy
	Translations bool `json:"translations"`  // registry includes i18n labels/descriptions
}

// RegistryManifest represents a registry's step manifest.
type RegistryManifest struct {
	Name             string               `json:"name"`
	Description      string               `json:"description"`
	Version          string               `json:"version"`
	MinClientVersion string               `json:"min_client_version,omitempty"`
	Capabilities     RegistryCapabilities `json:"capabilities,omitempty"`
	Steps            []ManifestStep       `json:"steps"`
	Supports         map[string]any       `json:"supports,omitempty"`
	// EntitlementURL is the endpoint to call before sync to check access.
	// When empty, no entitlement check is performed.
	EntitlementURL string `json:"entitlement_url,omitempty"`
	// Taxonomy is the embedded namespace list (used by the default bundled registry).
	Taxonomy []RegistryTaxonomyEntry `json:"taxonomy,omitempty"`
	// PublicKey is the hex-encoded Ed25519 public key used to verify registry
	// update signatures. When non-empty, the syncer will verify the
	// Content-Signature header (or .sig sidecar) on each sync response.
	PublicKey string `json:"public_key,omitempty"`
}

// RegistryTrustStatus summarises the signature-verification state of a cached registry.
type RegistryTrustStatus string

const (
	// RegistryTrustUnknown means no public key is declared — verification was skipped.
	RegistryTrustUnknown RegistryTrustStatus = "unknown"
	// RegistryTrustVerified means the last sync response passed Ed25519 verification.
	RegistryTrustVerified RegistryTrustStatus = "verified"
	// RegistryTrustFailed means the last sync response failed Ed25519 verification.
	RegistryTrustFailed RegistryTrustStatus = "failed"
)

// RegistryTaxonomyEntry is a localised namespace/tag node in a registry manifest.
type RegistryTaxonomyEntry struct {
	Namespace    string            `json:"namespace"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	Labels       map[string]string `json:"labels,omitempty"`       // locale → label
	Descriptions map[string]string `json:"descriptions,omitempty"` // locale → description
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
	RegistryURL    string              `json:"registry_url"`
	Manifest       *RegistryManifest   `json:"manifest"`
	LastFetched    time.Time           `json:"last_fetched"`
	ETag           string              `json:"etag"`
	AutoUpdate     bool                `json:"auto_update"`
	// TrustStatus records the outcome of the last signature verification attempt.
	TrustStatus    RegistryTrustStatus `json:"trust_status,omitempty"`
	// KeyFingerprint is the SHA-256 fingerprint (hex) of the registry's declared public key.
	KeyFingerprint string              `json:"key_fingerprint,omitempty"`
}

// RegistryEntitlement stores the entitlement record returned by a registry's
// /entitlements endpoint. Namespaces contains glob patterns (e.g. "ai.*").
type RegistryEntitlement struct {
	RegistryName string    `json:"registry_name"`
	Plan         string    `json:"plan"`
	Namespaces   []string  `json:"namespaces"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
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

// MeteringEventType enumerates the events tracked for paid registry access.
type MeteringEventType string

const (
	MeteringEventEntityResolve  MeteringEventType = "entity_resolve"
	MeteringEventContentPull    MeteringEventType = "content_pull"
	MeteringEventTaxonomySync   MeteringEventType = "taxonomy_sync"
)

// MeteringEvent records one billable access for a registry.
type MeteringEvent struct {
	ID           string            `json:"id"`
	RegistryName string            `json:"registry_name"`
	EventType    MeteringEventType `json:"event_type"`
	Namespace    string            `json:"namespace,omitempty"`
	Count        int               `json:"count"`
	OccurredAt   time.Time         `json:"occurred_at"`
}

// MeteringFilter restricts metering queries.
type MeteringFilter struct {
	RegistryName string
	EventType    MeteringEventType
	After        time.Time
	Before       time.Time
}

// MeteringAggregate is the aggregated count per registry+event_type for a period.
type MeteringAggregate struct {
	RegistryName string            `json:"registry_name"`
	EventType    MeteringEventType `json:"event_type"`
	Total        int               `json:"total"`
}

// QuotaConfig declares soft (warn) and hard (limit) quota thresholds.
// Limit == 0 means unlimited.
type QuotaConfig struct {
	// Limit is the hard cap; 0 = unlimited.
	Limit int `json:"limit" yaml:"limit"`
	// WarnAt is the count at which a warning is emitted (0 = default 80% of Limit).
	WarnAt int `json:"warn_at" yaml:"warn_at"`
	// ResetsAt is an informational reset timestamp (e.g. billing period end).
	ResetsAt time.Time `json:"resets_at" yaml:"resets_at"`
}

// VectorHit is a single result returned by VectorStore.Search.
type VectorHit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// SavedSearch is a persisted named search query with optional alert config (US-0054).
type SavedSearch struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`       // unique human-readable identifier
	Query     string    `json:"query"`      // RSQL or NLQ query text
	ProfileID string    `json:"profile_id"` // optional focus profile
	AlertOn   string    `json:"alert_on"`   // "" | "new-results"
	Notify    string    `json:"notify"`     // notification channel: "" | "email" | ...
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SavedSearchFilter restricts saved search listing.
type SavedSearchFilter struct {
	ProfileID string
	Limit     int
	Offset    int
}

// SearchHistoryEntry is one record in the search history log (US-0055).
type SearchHistoryEntry struct {
	ID              string    `json:"id"`
	Query           string    `json:"query"`
	ProfileID       string    `json:"profile_id"`
	StrategiesUsed  string    `json:"strategies_used"`  // comma-separated strategy names
	ResultCount     int       `json:"result_count"`
	SearchedAt      time.Time `json:"searched_at"`
}

// SearchHistoryFilter restricts search history queries.
type SearchHistoryFilter struct {
	ProfileID string
	Limit     int
	Offset    int
}
