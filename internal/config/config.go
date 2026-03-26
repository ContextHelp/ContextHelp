package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

const (
	// DefaultConfigFileName is the default configuration file name
	DefaultConfigFileName = "config.yaml"
	// DefaultConfigDir is the default configuration directory
	DefaultConfigDir = ".config/contexthelp"
	// EnvConfigPath is the environment variable for config path override
	EnvConfigPath = "CTXT_CONFIG"
	// EnvDataDir is the environment variable for data directory override
	EnvDataDir = "CTXT_DATA_DIR"
	// EnvProfile is the environment variable for default focus profile
	EnvProfile = "CTXT_PROFILE"
	// EnvDPKMSDataDir is the environment variable for dPKMS data directory
	EnvDPKMSDataDir = "DPKMS_DATA_DIR"
	// EnvDPKMSWorkers is the environment variable for worker thread count
	EnvDPKMSWorkers = "DPKMS_WORKERS"
)

// currentSchemaVersion is the latest config schema version.
const currentSchemaVersion = 1

// Config represents the application configuration
type Config struct {
	// Version is the schema version. Used for migrations.
	Version int `mapstructure:"version" yaml:"version"`

	// Storage configuration
	Storage StorageConfig `mapstructure:"storage"`

	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// Profile configuration
	Profile ProfileConfig `mapstructure:"profile"`

	// Registries configuration
	Registries []RegistryConfig `mapstructure:"registries"`

	// Plugins configuration
	Plugins []PluginConfig `mapstructure:"plugins"`

	// I18n configuration
	I18n I18nConfig `mapstructure:"i18n"`

	// Providers configuration
	Providers ProvidersConfig `mapstructure:"providers"`

	// Retrieval configuration
	Retrieval RetrievalConfig `mapstructure:"retrieval"`

	// Jobs controls worker pool behaviour.
	Jobs JobsConfig `mapstructure:"jobs"`

	// Pipelines holds per-pipeline provider overrides and routing rules.
	Pipelines PipelinesConfig `mapstructure:"pipelines"`

	// Conventions enforces naming rules across the system.
	Conventions ConventionsConfig `mapstructure:"conventions"`

	// Secrets configures the secrets backend.
	Secrets SecretsConfig `mapstructure:"secrets"`

	// Watch configures the filesystem watcher.
	Watch WatchConfig `mapstructure:"watch"`

	// Inbox configures the default inbox for new content.
	Inbox InboxConfig `mapstructure:"inbox"`

	// Duplicates controls duplicate detection policy.
	Duplicates DuplicatesConfig `mapstructure:"duplicates"`

	// Search controls hybrid query execution behaviour.
	Search SearchConfig `mapstructure:"search"`

	// URI controls ctxt:// URL scheme dispatch behaviour.
	URI URIConfig `mapstructure:"uri"`

	// Backup configures the backup command.
	Backup BackupConfig `mapstructure:"backup" yaml:"backup"`

	// Privacy controls user-facing privacy preferences.
	Privacy PrivacyConfig `mapstructure:"privacy" yaml:"privacy"`

	// Offline forces local-only operation: no registry sync, no LLM/embedding API calls.
	// Set via --offline flag or config key offline.enabled.
	// Default: false.
	Offline OfflineConfig `mapstructure:"offline" yaml:"offline"`

	// Resurfacing controls the background resurfacing queue.
	Resurfacing ResurfacingConfig `mapstructure:"resurfacing" yaml:"resurfacing"`

	// Audit controls SIEM-ready audit log export and forwarding.
	Audit AuditConfig `mapstructure:"audit" yaml:"audit"`
}

// AuditConfig controls SIEM-ready audit log export and real-time forwarding.
type AuditConfig struct {
	// Syslog forwards each audit event to a remote syslog receiver.
	Syslog AuditSyslogConfig `mapstructure:"syslog" yaml:"syslog"`
	// Webhook POSTs each audit event as JSON to a configurable URL.
	Webhook AuditWebhookConfig `mapstructure:"webhook" yaml:"webhook"`
}

// AuditSyslogConfig holds syslog forwarding settings.
type AuditSyslogConfig struct {
	// Enabled activates syslog forwarding. Default: false.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
	// Protocol is the transport: "udp", "tcp", or "tls". Default: "udp".
	Protocol string `mapstructure:"protocol" yaml:"protocol"`
	// Address is host:port of the syslog receiver. Default: "localhost:514".
	Address string `mapstructure:"address" yaml:"address"`
	// Facility is the syslog facility name (e.g. "local0"). Default: "local0".
	Facility string `mapstructure:"facility" yaml:"facility"`
}

// AuditWebhookConfig holds webhook delivery settings.
type AuditWebhookConfig struct {
	// Enabled activates webhook delivery. Default: false.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
	// URL is the HTTP/HTTPS endpoint that receives POST requests.
	URL string `mapstructure:"url" yaml:"url"`
}

// OfflineConfig controls strict offline (air-gapped) operation mode.
// When Enabled is true:
//   - Registry sync jobs are queued but not executed.
//   - Unresolved mentions create local stub entities.
//   - LLM and embedding API calls are skipped; pipelines degrade gracefully.
//   - Plugins that require network access are denied.
type OfflineConfig struct {
	// Enabled activates strict offline mode. Default: false.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
}

// ResurfacingConfig controls the background resurfacing queue process.
type ResurfacingConfig struct {
	// Enabled activates the background scoring loop. Default: true.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
	// MaxItems is the maximum items returned by ctxt resurface. Default: 10.
	MaxItems int `mapstructure:"max_items" yaml:"max_items"`
	// MinScore discards candidates below this score. Default: 0.4.
	MinScore float64 `mapstructure:"min_score" yaml:"min_score"`
	// RunInterval is how often the background job re-scores. Default: 1h.
	RunInterval time.Duration `mapstructure:"run_interval" yaml:"run_interval"`
}

// PrivacyConfig holds privacy-related preferences.
type PrivacyConfig struct {
	// Telemetry enables anonymous usage telemetry.
	// Default: false (local-first, privacy-respecting).
	// Override via CH_DISABLE_TELEMETRY=true env var or set this to false.
	Telemetry bool `mapstructure:"telemetry" yaml:"telemetry"`
}

// URIConfig controls how ctxt:// URIs are handled by the OS URL handler.
type URIConfig struct {
	// Handler selects the UI used when a ctxt:// link is opened.
	// Valid values: "cli" (default) | "tui".
	// "cli" prints to stdout; "tui" opens the interactive terminal interface.
	Handler string `mapstructure:"handler" yaml:"handler"`
}

// DuplicatesConfig controls duplicate and near-duplicate detection behaviour at ingest time.
type DuplicatesConfig struct {
	// Policy determines what happens when a duplicate is found.
	// Valid values: "warn" (default), "drop", "keep".
	Policy string `mapstructure:"policy"`
	// SimilarityThreshold is the cosine similarity cutoff for near-duplicate detection.
	// Range: 0.0–1.0. Default: 0.95.
	SimilarityThreshold float64 `mapstructure:"similarity_threshold"`
	// CheckExact enables content-hash exact-match deduplication. Default: true.
	CheckExact bool `mapstructure:"check_exact"`
	// CheckSimilar enables vector-embedding near-duplicate detection. Default: false.
	// Requires embeddings to have been computed (pipeline embedding step must run first).
	CheckSimilar bool `mapstructure:"check_similar"`
}

// ProfileSearchStrategy overrides global search settings for a specific profile.
// Zero values mean "inherit from global config".
type ProfileSearchStrategy struct {
	// Mode overrides search.default_mode for this profile.
	// Valid values: "" (inherit) | "fts" | "vector" | "hybrid".
	Mode string `mapstructure:"mode" yaml:"mode"`
	// RRF overrides RRF parameters. Zero values inherit from global.
	RRF RRFConfig `mapstructure:"rrf" yaml:"rrf"`
	// CandidatePool overrides pool sizes. Zero values inherit from global.
	CandidatePool CandidatePoolConfig `mapstructure:"candidate_pool" yaml:"candidate_pool"`
	// MinScore overrides the minimum score threshold. Negative means inherit.
	MinScore float64 `mapstructure:"min_score" yaml:"min_score"`
}

// SearchConfig controls hybrid search behaviour.
type SearchConfig struct {
	// DefaultMode selects the search strategy used when no flag is passed.
	// Valid values: "fts" | "vector" | "hybrid". Default: "hybrid".
	DefaultMode string `mapstructure:"default_mode" yaml:"default_mode"`

	// RRF controls Reciprocal Rank Fusion parameters.
	RRF RRFConfig `mapstructure:"rrf" yaml:"rrf"`

	// CandidatePool controls how many results each leg fetches before merge.
	CandidatePool CandidatePoolConfig `mapstructure:"candidate_pool" yaml:"candidate_pool"`

	// MinScore discards merged results below this RRF score. Default: 0.0 (off).
	MinScore float64 `mapstructure:"min_score" yaml:"min_score"`

	// FallbackToFTS controls behaviour when embedding provider is unavailable.
	// If true (default), hybrid degrades to FTS-only. If false, returns error.
	FallbackToFTS bool `mapstructure:"fallback_to_fts" yaml:"fallback_to_fts"`

	// Reranker controls signal weights applied after RRF merge.
	Reranker RerankerConfig `mapstructure:"reranker" yaml:"reranker"`
}

// RerankerConfig holds pluggable weight constants for the default Reranker.
// All values are additive bonuses on top of the RRF score.
type RerankerConfig struct {
	// MentionBoostPerMention is added per outbound mention on a result.
	// Default: 0.05.
	MentionBoostPerMention float64 `mapstructure:"mention_boost_per_mention" yaml:"mention_boost_per_mention"`
	// MaxMentionBoost is the ceiling for the total outbound mention bonus.
	// Default: 1.0.
	MaxMentionBoost float64 `mapstructure:"max_mention_boost" yaml:"max_mention_boost"`
	// DirectBacklinkBoost is applied when a result has ≥1 direct inbound edge.
	// Default: 0.08.
	DirectBacklinkBoost float64 `mapstructure:"direct_backlink_boost" yaml:"direct_backlink_boost"`
	// HopBacklinkBoost is applied for 2-hop entity proximity connections.
	// Default: 0.03.
	HopBacklinkBoost float64 `mapstructure:"hop_backlink_boost" yaml:"hop_backlink_boost"`
}

// RRFConfig controls Reciprocal Rank Fusion parameters.
type RRFConfig struct {
	// K is the rank constant (default 60). Higher values reduce the impact of top ranks.
	K int `mapstructure:"k" yaml:"k"`
	// FTSWeight is the weight applied to FTS leg scores (default 0.5).
	FTSWeight float64 `mapstructure:"fts_weight" yaml:"fts_weight"`
	// VectorWeight is the weight applied to vector leg scores (default 0.5).
	VectorWeight float64 `mapstructure:"vector_weight" yaml:"vector_weight"`
}

// CandidatePoolConfig controls how many candidates each leg returns before merge.
type CandidatePoolConfig struct {
	// FTS is the max candidates from the FTS leg. Default: 50.
	FTS int `mapstructure:"fts" yaml:"fts"`
	// Vector is the max candidates from the vector leg. Default: 50.
	Vector int `mapstructure:"vector" yaml:"vector"`
}

// RetrievalConfig controls progressive retrieval behaviour.
type RetrievalConfig struct {
	Method                 string     `mapstructure:"method" yaml:"method" json:"method"` // "rag" or "llm"
	EnableSufficiencyCheck bool       `mapstructure:"enable_sufficiency_check" yaml:"enable_sufficiency_check" json:"enable_sufficiency_check"`
	LLMProfile             string     `mapstructure:"llm_profile" yaml:"llm_profile" json:"llm_profile"`
	Categories             TierConfig `mapstructure:"categories" yaml:"categories" json:"categories"`
	Items                  TierConfig `mapstructure:"items" yaml:"items" json:"items"`
	Resources              TierConfig `mapstructure:"resources" yaml:"resources" json:"resources"`
}

// TierConfig controls a single retrieval tier.
type TierConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	TopK    int  `mapstructure:"top_k" yaml:"top_k" json:"top_k"`
}

// DefaultRetrievalConfig returns sensible defaults for retrieval.
func DefaultRetrievalConfig() RetrievalConfig {
	return RetrievalConfig{
		Method:                 "rag",
		EnableSufficiencyCheck: true,
		LLMProfile:             "default",
		Categories:             TierConfig{Enabled: true, TopK: 10},
		Items:                  TierConfig{Enabled: true, TopK: 20},
		Resources:              TierConfig{Enabled: true, TopK: 5},
	}
}

// BackupConfig holds settings for the backup command.
type BackupConfig struct {
	// Dir is the directory where backup archives are written.
	// Defaults to the current working directory if empty.
	Dir string `mapstructure:"dir" yaml:"dir"`
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Type string     `mapstructure:"type"`
	Path string     `mapstructure:"path"`
	Blob BlobConfig `mapstructure:"blob"`
}

// BlobConfig holds configuration for the blob storage backend.
type BlobConfig struct {
	Backend   string          `mapstructure:"backend"`
	Threshold int64           `mapstructure:"threshold"`
	Local     BlobLocalConfig `mapstructure:"local"`
	S3        BlobS3Config    `mapstructure:"s3"`
}

// BlobLocalConfig configures the local filesystem blob backend.
type BlobLocalConfig struct {
	Path string `mapstructure:"path"`
}

// BlobS3Config configures the S3-compatible blob backend.
type BlobS3Config struct {
	Endpoint      string        `mapstructure:"endpoint"`
	Region        string        `mapstructure:"region"`
	Bucket        string        `mapstructure:"bucket"`
	Prefix        string        `mapstructure:"prefix"`
	AccessKey     string        `mapstructure:"access_key"`
	SecretKey     string        `mapstructure:"secret_key"`
	UsePathStyle  bool          `mapstructure:"use_path_style"`
	PresignExpiry time.Duration `mapstructure:"presign_expiry"`
	MaxRetries    int           `mapstructure:"max_retries"`
}

// ServerConfig represents server configuration
type ServerConfig struct {
	Port     int    `mapstructure:"port"`
	GRPCPort int    `mapstructure:"grpc_port"`
	Workers  int    `mapstructure:"workers"`
	Public   bool   `mapstructure:"public"`
}

// ProfileConfig represents profile configuration
type ProfileConfig struct {
	Default  string                  `mapstructure:"default" yaml:"default"`
	Profiles map[string]FocusProfile `mapstructure:"profiles" yaml:"profiles"`
}

// FocusProfile is a named configuration preset for a specific role or project.
type FocusProfile struct {
	// Description is a human-readable label shown in ctxt profile list.
	Description string `mapstructure:"description" yaml:"description"`
	// Tags is the default tag set pre-populated when this profile is active.
	Tags []string `mapstructure:"tags" yaml:"tags"`
	// MentionNamespaces constrains which @namespaces are surfaced in results.
	MentionNamespaces []string `mapstructure:"mention_namespaces" yaml:"mention_namespaces"`
	// RerankBoosts maps entity types to a boost factor (1.0 = no boost).
	RerankBoosts map[string]float64 `mapstructure:"rerank_boosts" yaml:"rerank_boosts"`
	// SearchStrategy overrides global search config for this profile.
	// Zero/empty fields inherit from the global search config.
	SearchStrategy ProfileSearchStrategy `mapstructure:"search_strategy" yaml:"search_strategy"`
}

// RegistryAuthType enumerates supported auth mechanisms.
type RegistryAuthType string

const (
	// RegistryAuthBearer uses an HTTP Bearer token.
	RegistryAuthBearer RegistryAuthType = "bearer"
	// RegistryAuthAPIKey uses a static API key header.
	RegistryAuthAPIKey RegistryAuthType = "api_key"
	// RegistryAuthOAuth2 uses an OAuth2 access token.
	RegistryAuthOAuth2 RegistryAuthType = "oauth2"
)

// RegistryAuthConfig holds auth metadata for a registry.
// Tokens are NEVER stored here — they live in the OS keychain.
type RegistryAuthConfig struct {
	// Type selects the auth mechanism: bearer | api_key | oauth2.
	Type RegistryAuthType `mapstructure:"type" yaml:"type"`
	// HeaderName overrides the HTTP header used for api_key auth.
	// Defaults to "X-API-Key". Ignored for bearer and oauth2 types.
	HeaderName string `mapstructure:"header_name,omitempty" yaml:"header_name,omitempty"`
}

// RegistrySyncMode controls how much content is fetched during a registry sync.
// "full" (default) fetches complete entity definitions.
// "thin" fetches only the entity index (IDs, slugs, titles, version hashes) and taxonomy.
type RegistrySyncMode string

const (
	RegistrySyncModeFull RegistrySyncMode = "full"
	RegistrySyncModeThin RegistrySyncMode = "thin"
)

// RegistryConfig represents a registry configuration
type RegistryConfig struct {
	Name     string             `mapstructure:"name"      yaml:"name"`
	URL      string             `mapstructure:"url"       yaml:"url"`
	Auth     RegistryAuthConfig `mapstructure:"auth,omitempty" yaml:"auth,omitempty"`
	SyncMode RegistrySyncMode   `mapstructure:"sync_mode" yaml:"sync_mode"`
	// EntitlementURL is the endpoint to call before sync to verify access.
	// When empty, no entitlement check is performed.
	// Typically populated at runtime from the registry manifest; can also be
	// declared explicitly in config for override scenarios.
	EntitlementURL string `mapstructure:"entitlement_url,omitempty" yaml:"entitlement_url,omitempty"`
}

// PluginConfig represents a plugin configuration
type PluginConfig struct {
	Type   string                 `mapstructure:"type"`
	Plugin string                 `mapstructure:"plugin"`
	Config map[string]interface{} `mapstructure:"config"`
}

// I18nConfig represents i18n configuration
type I18nConfig struct {
	Enabled            bool     `mapstructure:"enabled"`
	PreferredLanguages []string `mapstructure:"preferred_languages"`
	AutoTranslate      bool     `mapstructure:"auto_translate"`
	TranslateTags      bool     `mapstructure:"translate_tags"`
}

// ProvidersConfig controls backend selection for each provider type.
type ProvidersConfig struct {
	Video         ProviderBackendConfig `mapstructure:"video"`
	Document      ProviderBackendConfig `mapstructure:"document"`
	OCR           ProviderBackendConfig `mapstructure:"ocr"`
	Transcription ProviderBackendConfig `mapstructure:"transcription"`
	Vision        ProviderBackendConfig `mapstructure:"vision"`
	Diarization   ProviderBackendConfig `mapstructure:"diarization"`
	LLM           ProviderBackendConfig `mapstructure:"llm"`
	Embedding     ProviderBackendConfig `mapstructure:"embedding"`
}

// ProviderBackendConfig selects which backend to use for a provider.
type ProviderBackendConfig struct {
	Backend string `mapstructure:"backend"`
	// Model overrides the default model for LLM-based providers (vision, transcription via ollama).
	Model string `mapstructure:"model,omitempty"`
	// Endpoint overrides the default API endpoint (e.g. Ollama URL).
	Endpoint string `mapstructure:"endpoint,omitempty"`
	// Language sets a default language hint (e.g. for OCR, transcription).
	Language string `mapstructure:"language,omitempty"`
}

// Load loads the configuration from file and environment variables
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// Set configuration file
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		// Check environment variable
		if envConfig := os.Getenv(EnvConfigPath); envConfig != "" {
			v.SetConfigFile(envConfig)
		} else {
			// Use default location
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get home directory: %w", err)
			}

			configPath := filepath.Join(home, DefaultConfigDir)
			v.AddConfigPath(configPath)
			v.SetConfigName("config")
			v.SetConfigType("yaml")
		}
	}

	// Set defaults
	setDefaults(v)

	// Bind environment variables
	bindEnvVars(v)

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
		// Config file not found is acceptable
	}

	// Unmarshal configuration
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Run migrations if needed.
	if cfg.Version < currentSchemaVersion {
		if migrate(&cfg) {
			// Best-effort write-back: ignore errors (config path may be read-only).
			cfgPath := v.ConfigFileUsed()
			if cfgPath != "" {
				_ = WriteBack(&cfg, cfgPath)
			}
		}
	}

	return &cfg, nil
}

// migrate applies schema migrations to cfg in-place and returns true if
// any migration was applied (caller should write back).
func migrate(cfg *Config) bool {
	changed := false

	if cfg.Version < 1 {
		// v0 → v1: no structural changes; just stamp the version.
		cfg.Version = 1
		changed = true
	}

	return changed
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".local", "share", "contexthelp")

	// Storage defaults
	v.SetDefault("storage.type", "sqlite")
	v.SetDefault("storage.path", filepath.Join(dataDir, "db.sqlite"))

	// Blob storage defaults
	v.SetDefault("storage.blob.backend", "local")
	v.SetDefault("storage.blob.threshold", int64(65536))
	v.SetDefault("storage.blob.local.path", filepath.Join(dataDir, "blobs"))
	v.SetDefault("storage.blob.s3.region", "us-east-1")
	v.SetDefault("storage.blob.s3.use_path_style", false)
	v.SetDefault("storage.blob.s3.presign_expiry", time.Hour)
	v.SetDefault("storage.blob.s3.max_retries", 3)

	// Server defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.grpc_port", 9090)
	v.SetDefault("server.workers", 4)
	v.SetDefault("server.public", false)

	// Profile defaults
	v.SetDefault("profile.default", "")

	// I18n defaults
	v.SetDefault("i18n.enabled", false)
	v.SetDefault("i18n.preferred_languages", []string{"en"})
	v.SetDefault("i18n.auto_translate", false)
	v.SetDefault("i18n.translate_tags", false)

	// Provider defaults — "auto" probes for tools, falls back to stub
	v.SetDefault("providers.video.backend", "auto")
	v.SetDefault("providers.document.backend", "auto")
	v.SetDefault("providers.ocr.backend", "auto")
	v.SetDefault("providers.transcription.backend", "auto")
	v.SetDefault("providers.vision.backend", "auto")
	v.SetDefault("providers.vision.endpoint", "http://localhost:11434")
	v.SetDefault("providers.vision.model", "llava")
	v.SetDefault("providers.diarization.backend", "auto")
	v.SetDefault("providers.llm.backend", "auto")
	v.SetDefault("providers.llm.endpoint", "http://localhost:11434")
	v.SetDefault("providers.llm.model", "")

	// Schema version
	v.SetDefault("version", 0)

	// Jobs defaults (reproduce current hardcoded values)
	v.SetDefault("jobs.poll_interval", 500*time.Millisecond)
	v.SetDefault("jobs.stale_timeout", 30*time.Minute)
	v.SetDefault("jobs.max_retries", 3)
	v.SetDefault("jobs.max_hops", 5)

	// Pipelines defaults
	v.SetDefault("pipelines.overrides", map[string]any{})

	// Conventions defaults
	v.SetDefault("conventions.enforce_mention_namespaces", "off")

	// Secrets defaults
	v.SetDefault("secrets.backend", "env")
	v.SetDefault("secrets.keychain_service", "ctxt")

	// Watch defaults
	v.SetDefault("watch.enabled", false)
	v.SetDefault("watch.debounce", 500*time.Millisecond)
	v.SetDefault("watch.clipboard.enabled", false)
	v.SetDefault("watch.clipboard.poll_interval", 2*time.Second)
	v.SetDefault("watch.clipboard.min_length", 80)
	v.SetDefault("watch.clipboard.auto_ingest", true)

	// Inbox defaults — leave path empty (resolved at runtime)
	v.SetDefault("inbox.pipeline", "text.short")

	// Duplicates defaults
	v.SetDefault("duplicates.policy", "warn")
	v.SetDefault("duplicates.similarity_threshold", 0.95)
	v.SetDefault("duplicates.check_exact", true)
	v.SetDefault("duplicates.check_similar", false)

	// Search defaults
	v.SetDefault("search.default_mode", "hybrid")
	v.SetDefault("search.rrf.k", 60)
	v.SetDefault("search.rrf.fts_weight", 0.5)
	v.SetDefault("search.rrf.vector_weight", 0.5)
	v.SetDefault("search.candidate_pool.fts", 50)
	v.SetDefault("search.candidate_pool.vector", 50)
	v.SetDefault("search.min_score", 0.0)
	v.SetDefault("search.fallback_to_fts", true)

	// Reranker signal weight defaults.
	v.SetDefault("search.reranker.mention_boost_per_mention", 0.05)
	v.SetDefault("search.reranker.max_mention_boost", 1.0)
	v.SetDefault("search.reranker.direct_backlink_boost", 0.08)
	v.SetDefault("search.reranker.hop_backlink_boost", 0.03)

	// Privacy defaults — telemetry off by default (local-first)
	v.SetDefault("privacy.telemetry", false)

	// Offline mode — disabled by default; enable for air-gapped environments.
	v.SetDefault("offline.enabled", false)

	// Resurfacing defaults
	v.SetDefault("resurfacing.enabled", true)
	v.SetDefault("resurfacing.max_items", 10)
	v.SetDefault("resurfacing.min_score", 0.4)
	v.SetDefault("resurfacing.run_interval", time.Hour)
}

// bindEnvVars binds environment variables to configuration keys
func bindEnvVars(v *viper.Viper) {
	v.SetEnvPrefix("CH")
	v.AutomaticEnv()

	// Explicit bindings for common overrides
	v.BindEnv("storage.type", "CH_STORAGE_TYPE")
	v.BindEnv("storage.path", EnvDataDir)

	// Blob storage env var bindings
	v.BindEnv("storage.blob.backend", "CTXT_BLOB_BACKEND")
	v.BindEnv("storage.blob.threshold", "CTXT_BLOB_THRESHOLD")
	v.BindEnv("storage.blob.s3.endpoint", "CTXT_BLOB_S3_ENDPOINT")
	v.BindEnv("storage.blob.s3.region", "CTXT_BLOB_S3_REGION")
	v.BindEnv("storage.blob.s3.bucket", "CTXT_BLOB_S3_BUCKET")
	v.BindEnv("storage.blob.s3.prefix", "CTXT_BLOB_S3_PREFIX")
	v.BindEnv("storage.blob.s3.access_key", "CTXT_BLOB_S3_ACCESS_KEY")
	v.BindEnv("storage.blob.s3.secret_key", "CTXT_BLOB_S3_SECRET_KEY")
	v.BindEnv("server.port", "CH_SERVER_PORT")
	v.BindEnv("server.grpc_port", "CH_GRPC_PORT")
	v.BindEnv("server.workers", EnvDPKMSWorkers)
	v.BindEnv("server.public", "CH_PUBLIC")
	v.BindEnv("profile.default", EnvProfile)

	v.BindEnv("jobs.poll_interval", "DPKMS_POLL_INTERVAL")
	v.BindEnv("jobs.stale_timeout", "DPKMS_STALE_TIMEOUT")
	v.BindEnv("jobs.max_retries", "DPKMS_MAX_RETRIES")
	v.BindEnv("jobs.max_hops", "DPKMS_MAX_HOPS")
	v.BindEnv("secrets.backend", "CTXT_SECRETS_BACKEND")
	v.BindEnv("secrets.age_file", "CTXT_AGE_FILE")
	v.BindEnv("secrets.age_identity_file", "CTXT_AGE_IDENTITY")
}

// ResolveSearchConfig merges global search config with a profile-level override.
// Profile fields with zero values inherit from global.
func ResolveSearchConfig(global SearchConfig, profile ProfileSearchStrategy) SearchConfig {
	out := global
	if profile.Mode != "" {
		out.DefaultMode = profile.Mode
	}
	if profile.RRF.K != 0 {
		out.RRF.K = profile.RRF.K
	}
	if profile.RRF.FTSWeight != 0 {
		out.RRF.FTSWeight = profile.RRF.FTSWeight
	}
	if profile.RRF.VectorWeight != 0 {
		out.RRF.VectorWeight = profile.RRF.VectorWeight
	}
	if profile.CandidatePool.FTS != 0 {
		out.CandidatePool.FTS = profile.CandidatePool.FTS
	}
	if profile.CandidatePool.Vector != 0 {
		out.CandidatePool.Vector = profile.CandidatePool.Vector
	}
	if profile.MinScore < 0 {
		// negative sentinel = inherit (do nothing)
	} else if profile.MinScore > 0 {
		out.MinScore = profile.MinScore
	}
	return out
}

// GetConfigPath returns the configuration file path being used
func GetConfigPath() string {
	if cfgPath := os.Getenv(EnvConfigPath); cfgPath != "" {
		return cfgPath
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, DefaultConfigDir, DefaultConfigFileName)
}

// EnsureConfigDir ensures the configuration directory exists
func EnsureConfigDir() error {
	configPath := GetConfigPath()
	if configPath == "" {
		return fmt.Errorf("failed to determine config path")
	}

	configDir := filepath.Dir(configPath)
	return os.MkdirAll(configDir, 0755)
}

// EnsureDataDir ensures the data directory exists
func EnsureDataDir() error {
	dataDir := os.Getenv(EnvDataDir)
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		dataDir = filepath.Join(home, ".local", "share", "contexthelp")
	}

	return os.MkdirAll(dataDir, 0755)
}

// JobsConfig controls worker pool runtime parameters.
type JobsConfig struct {
	// PollInterval is how often idle workers poll for new jobs. Min 50ms.
	PollInterval time.Duration `mapstructure:"poll_interval" yaml:"poll_interval"`
	// StaleTimeout is how long a running job can be silent before it is
	// considered stale and eligible for recovery.
	StaleTimeout time.Duration `mapstructure:"stale_timeout" yaml:"stale_timeout"`
	// MaxRetries is the default retry limit for fan-out jobs.
	MaxRetries int `mapstructure:"max_retries" yaml:"max_retries"`
	// MaxHops is the maximum number of pipeline hops a job can take.
	MaxHops int `mapstructure:"max_hops" yaml:"max_hops"`
}

// PipelinesConfig holds per-pipeline overrides and global routing config.
type PipelinesConfig struct {
	// Overrides maps pipeline name to a per-pipeline override.
	Overrides map[string]PipelineOverride `mapstructure:"overrides" yaml:"overrides"`
}

// PipelineOverride allows customising a single named pipeline.
type PipelineOverride struct {
	// Providers overrides individual provider backends for this pipeline only.
	// Keys match ProvidersConfig field names in lowercase: "llm", "vision", etc.
	Providers map[string]ProviderBackendConfig `mapstructure:"providers" yaml:"providers"`
	// SkipSteps is an ordered list of step names to remove from the pipeline.
	SkipSteps []string `mapstructure:"skip_steps" yaml:"skip_steps"`
	// ExtraSteps is an ordered list of step names appended after existing steps.
	ExtraSteps []string `mapstructure:"extra_steps" yaml:"extra_steps"`
}

// ConventionsConfig enforces naming rules across the system.
type ConventionsConfig struct {
	// EnforceMentionNamespaces controls @namespace validation.
	// Valid values: "off" (default), "warn", "error".
	EnforceMentionNamespaces string `mapstructure:"enforce_mention_namespaces" yaml:"enforce_mention_namespaces"`
	// AllowedMentionNamespaces is the set of valid @namespace prefixes.
	// Empty means all namespaces are allowed.
	AllowedMentionNamespaces []string `mapstructure:"allowed_mention_namespaces" yaml:"allowed_mention_namespaces"`
	// TagVocabulary is the canonical set of tags. When non-empty, tags outside
	// this set trigger fuzzy suggestions.
	TagVocabulary []string `mapstructure:"tag_vocabulary" yaml:"tag_vocabulary"`
}

// SecretsConfig controls where API keys and other secrets are read from.
type SecretsConfig struct {
	// Backend selects the secrets provider.
	// Valid values: "env" (default), "keychain", "age-file", "1password", "gh-secrets".
	Backend string `mapstructure:"backend" yaml:"backend"`
	// AgeFile is the path to an age-encrypted YAML secrets file.
	// Only used when Backend == "age-file".
	AgeFile string `mapstructure:"age_file" yaml:"age_file"`
	// AgeIdentityFile is the path to the age identity (private key) file.
	AgeIdentityFile string `mapstructure:"age_identity_file" yaml:"age_identity_file"`
	// KeychainService is the macOS/Linux keychain service name.
	// Defaults to "ctxt".
	KeychainService string `mapstructure:"keychain_service" yaml:"keychain_service"`
	// OnePasswordVault is the 1Password vault name to read secrets from.
	// Only used when Backend == "1password". Requires `op` CLI.
	OnePasswordVault string `mapstructure:"onepassword_vault" yaml:"onepassword_vault"`
	// GHRepo is the GitHub repository (owner/repo) for the gh-secrets backend.
	// Only used when Backend == "gh-secrets". Defaults to current repo if empty.
	// Requires `gh` CLI. Get() falls back to env; Set() writes to GitHub Actions secrets.
	GHRepo string `mapstructure:"gh_repo" yaml:"gh_repo"`
}

// WatchConfig configures the filesystem watcher.
type WatchConfig struct {
	// Enabled activates the watcher on startup.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
	// Dirs is the list of directories to watch (supersedes Paths).
	Dirs []string `mapstructure:"dirs" yaml:"dirs"`
	// Paths is the legacy list of directories to watch (kept for backwards compat).
	Paths []string `mapstructure:"paths" yaml:"paths"`
	// Debounce is how long to wait after a change before processing.
	Debounce time.Duration `mapstructure:"debounce" yaml:"debounce"`
	// Patterns is a list of glob patterns to include (e.g. "*.md").
	Patterns []string `mapstructure:"patterns" yaml:"patterns"`
	// Clipboard configures the clipboard watcher.
	Clipboard ClipboardWatchConfig `mapstructure:"clipboard" yaml:"clipboard"`
}

// ClipboardWatchConfig configures passive clipboard monitoring.
type ClipboardWatchConfig struct {
	// Enabled activates clipboard polling. CTXT_NO_CLIPBOARD=1 overrides to off.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
	// PollInterval is how often to sample the clipboard. Defaults to 2s.
	PollInterval time.Duration `mapstructure:"poll_interval" yaml:"poll_interval"`
	// MinLength is the minimum rune count for plain-text content to qualify.
	// Defaults to 80.
	MinLength int `mapstructure:"min_length" yaml:"min_length"`
	// AutoIngest enqueues detected content without user prompt.
	AutoIngest bool `mapstructure:"auto_ingest" yaml:"auto_ingest"`
}

// InboxConfig configures the default inbox for new content.
type InboxConfig struct {
	// Path is the directory to use as the inbox. Defaults to ~/ctxt-inbox.
	Path string `mapstructure:"path" yaml:"path"`
	// Pipeline is the pipeline to use for inbox items. Defaults to "text.short".
	Pipeline string `mapstructure:"pipeline" yaml:"pipeline"`
	// Tags are auto-applied tags for inbox items.
	Tags []string `mapstructure:"tags" yaml:"tags"`
}
