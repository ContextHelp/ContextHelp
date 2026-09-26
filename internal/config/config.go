package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/urlfilter"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	kitconfig "hop.top/kit/go/core/config"
	"hop.top/kit/go/core/xdg"
)

const (
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
	Storage StorageConfig `mapstructure:"storage" yaml:"storage"`

	// Server configuration
	Server ServerConfig `mapstructure:"server" yaml:"server"`

	// Profile configuration
	Profile ProfileConfig `mapstructure:"profile" yaml:"profile"`

	// Registries configuration
	Registries []RegistryConfig `mapstructure:"registries" yaml:"registries"`

	// RegistriesGlobal holds registry-wide settings that apply to all registries.
	RegistriesGlobal RegistriesGlobalConfig `mapstructure:"registries_global" yaml:"registries_global"`

	// Plugins configuration
	Plugins []PluginConfig `mapstructure:"plugins" yaml:"plugins"`

	// I18n configuration
	I18n I18nConfig `mapstructure:"i18n" yaml:"i18n"`

	// Providers configuration
	Providers ProvidersConfig `mapstructure:"providers" yaml:"providers"`

	// Retrieval configuration
	Retrieval RetrievalConfig `mapstructure:"retrieval" yaml:"retrieval"`

	// Jobs controls worker pool behaviour.
	Jobs JobsConfig `mapstructure:"jobs" yaml:"jobs"`

	// Pipelines holds per-pipeline provider overrides and routing rules.
	Pipelines PipelinesConfig `mapstructure:"pipelines" yaml:"pipelines"`

	// Conventions enforces naming rules across the system.
	Conventions ConventionsConfig `mapstructure:"conventions" yaml:"conventions"`

	// Secrets configures the secrets backend.
	Secrets SecretsConfig `mapstructure:"secrets" yaml:"secrets"`

	// Watch configures the filesystem watcher.
	Watch WatchConfig `mapstructure:"watch" yaml:"watch"`

	// Inbox configures the default inbox for new content.
	Inbox InboxConfig `mapstructure:"inbox" yaml:"inbox"`

	// Duplicates controls duplicate detection policy.
	Duplicates DuplicatesConfig `mapstructure:"duplicates" yaml:"duplicates"`

	// Search controls hybrid query execution behaviour.
	Search SearchConfig `mapstructure:"search" yaml:"search"`

	// URI controls ctxt:// URL scheme dispatch behaviour.
	URI URIConfig `mapstructure:"uri" yaml:"uri"`

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

	// Security configures security alerting hooks.
	Security SecurityConfig `mapstructure:"security" yaml:"security"`

	// Federations lists remote dPKMS instances to sync with.
	Federations []FederationEntry `mapstructure:"federations" yaml:"federations"`

	// Federation holds server-side federation settings (receive-side).
	Federation FederationConfig `mapstructure:"federation" yaml:"federation"`

	// Browser configures the built-in IBR browser automation daemon.
	Browser BrowserConfig `mapstructure:"browser" yaml:"browser"`

	// FanOut controls post-ingest fan-out enrichment.
	FanOut FanOutConfig `mapstructure:"fanout" yaml:"fanout"`

	// Capture configures browser capture (open tabs, history).
	Capture CaptureConfig `mapstructure:"capture" yaml:"capture"`
}

// CaptureConfig configures browser capture.
type CaptureConfig struct {
	// URLFilter holds the deny/allow rules every browser capture path
	// applies before sending a URL for ingestion. Rules can be global or
	// scoped under browsers.<browser>[.profiles.<profile>]. Builtin
	// generic denies (localhost, file:, browser-internal schemes) always
	// apply on top; see urlfilter.BuiltinDeny.
	URLFilter urlfilter.Config `mapstructure:"url_filter" yaml:"url_filter"`
}

// FanOutConfig controls post-ingest fan-out enrichment behaviour.
// Fan-out runs as an async job after a successful ingest, creating
// cross-reference edges and audit log entries for extracted entities.
type FanOutConfig struct {
	// Enabled activates fan-out enrichment. Default: true.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
	// Entities enables entity cross-reference edge creation. Default: true.
	Entities bool `mapstructure:"entities" yaml:"entities"`
	// AuditLog enables changelog entries for fan-out mutations. Default: true.
	AuditLog bool `mapstructure:"audit_log" yaml:"audit_log"`
}

// DefaultFanOutConfig returns sensible defaults (all enabled).
func DefaultFanOutConfig() FanOutConfig {
	return FanOutConfig{
		Enabled:  true,
		Entities: true,
		AuditLog: true,
	}
}

// BrowserConfig controls the built-in IBR browser automation daemon.
type BrowserConfig struct {
	Enabled    bool   `mapstructure:"enabled" yaml:"enabled"`
	Binary     string `mapstructure:"binary" yaml:"binary"`
	Port       int    `mapstructure:"port" yaml:"port"`
	MaxClients int    `mapstructure:"max_clients" yaml:"max_clients"`
	Headless   bool   `mapstructure:"headless" yaml:"headless"`
	AIProvider string `mapstructure:"ai_provider" yaml:"ai_provider"`
	AIModel    string `mapstructure:"ai_model" yaml:"ai_model"`
}

// FederationConfig holds server-side (receive-side) federation settings.
type FederationConfig struct {
	// Token is the expected Bearer token for incoming push requests.
	// Empty = no auth check; any token accepted.
	Token string `mapstructure:"token" yaml:"token"`
}

// FederationEntry describes one remote dPKMS instance to federate with.
type FederationEntry struct {
	// Name is a unique local identifier for this federation peer.
	Name string `mapstructure:"name" yaml:"name"`
	// URL is the address of the remote dPKMS instance (e.g. https://host:8080 or file:///path/to/db).
	URL string `mapstructure:"url" yaml:"url"`
	// Interval is how often to sync. Required for async mode; zero = no scheduled sync.
	Interval time.Duration `mapstructure:"interval" yaml:"interval"`
	// SyncMode controls how sync runs. Valid values: "async" | "inline".
	SyncMode string `mapstructure:"sync_mode" yaml:"sync_mode"`
	// Token is the Bearer token sent to the remote instance. Empty = no auth header sent.
	// Tokens are stored here for remote HTTP peers only; local file targets use path-level auth.
	Token string `mapstructure:"token" yaml:"token"`
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

// SecurityConfig holds security alerting hook settings.
type SecurityConfig struct {
	Alerts SecurityAlertsConfig `mapstructure:"alerts" yaml:"alerts"`
}

// SecurityAlertsConfig holds threshold and delivery config for security alerts.
type SecurityAlertsConfig struct {
	// AuthFailureThreshold is the max auth failures per principal in 60s before alerting.
	// Default: 3.
	AuthFailureThreshold int `mapstructure:"auth_failure_threshold" yaml:"auth_failure_threshold"`
	// ACLDenialThreshold is the max ACL denials per principal in 60s before alerting.
	// Default: 10.
	ACLDenialThreshold int `mapstructure:"acl_denial_threshold" yaml:"acl_denial_threshold"`
	// WebhookURL is an optional endpoint for webhook alerts (empty = disabled).
	WebhookURL string `mapstructure:"webhook_url" yaml:"webhook_url"`
	// SMTP holds optional SMTP delivery config.
	SMTP SecuritySMTPConfig `mapstructure:"smtp" yaml:"smtp"`
}

// SecuritySMTPConfig holds SMTP delivery settings for security alerts.
type SecuritySMTPConfig struct {
	Host string `mapstructure:"host" yaml:"host"`
	Port int    `mapstructure:"port" yaml:"port"`
	From string `mapstructure:"from" yaml:"from"`
	To   string `mapstructure:"to" yaml:"to"`
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
	Policy string `mapstructure:"policy" yaml:"policy"`
	// SimilarityThreshold is the cosine similarity cutoff for near-duplicate detection.
	// Range: 0.0–1.0. Default: 0.95.
	SimilarityThreshold float64 `mapstructure:"similarity_threshold" yaml:"similarity_threshold"`
	// CheckExact enables content-hash exact-match deduplication. Default: true.
	CheckExact bool `mapstructure:"check_exact" yaml:"check_exact"`
	// CheckSimilar enables vector-embedding near-duplicate detection. Default: false.
	// Requires embeddings to have been computed (pipeline embedding step must run first).
	CheckSimilar bool `mapstructure:"check_similar" yaml:"check_similar"`
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

	// EncryptByDefault enables AES-256-GCM encryption for every backup
	// when true.  Equivalent to always passing --encrypt to `ctxt config backup`.
	// Passphrase is sourced from the same priority chain as the --encrypt flow:
	//   1. CTXT_BACKUP_PASSPHRASE env var
	//   2. OS keychain (ctxt.backup / passphrase)
	//   3. Interactive prompt
	EncryptByDefault bool `mapstructure:"encrypt_by_default" yaml:"encrypt_by_default"`
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Type string     `mapstructure:"type" yaml:"type"`
	Path string     `mapstructure:"path" yaml:"path"`
	Blob BlobConfig `mapstructure:"blob" yaml:"blob"`
}

// BlobConfig holds configuration for the blob storage backend.
type BlobConfig struct {
	Backend   string          `mapstructure:"backend" yaml:"backend"`
	Threshold int64           `mapstructure:"threshold" yaml:"threshold"`
	Local     BlobLocalConfig `mapstructure:"local" yaml:"local"`
	S3        BlobS3Config    `mapstructure:"s3" yaml:"s3"`
}

// BlobLocalConfig configures the local filesystem blob backend.
type BlobLocalConfig struct {
	Path string `mapstructure:"path" yaml:"path"`
}

// BlobS3Config configures the S3-compatible blob backend.
type BlobS3Config struct {
	Endpoint      string        `mapstructure:"endpoint" yaml:"endpoint"`
	Region        string        `mapstructure:"region" yaml:"region"`
	Bucket        string        `mapstructure:"bucket" yaml:"bucket"`
	Prefix        string        `mapstructure:"prefix" yaml:"prefix"`
	AccessKey     string        `mapstructure:"access_key" yaml:"access_key"`
	SecretKey     string        `mapstructure:"secret_key" yaml:"secret_key"`
	UsePathStyle  bool          `mapstructure:"use_path_style" yaml:"use_path_style"`
	PresignExpiry time.Duration `mapstructure:"presign_expiry" yaml:"presign_expiry"`
	MaxRetries    int           `mapstructure:"max_retries" yaml:"max_retries"`
}

// Access classes for ServerConfig.Access. They classify who may reach
// the instance and drive load-time validation plus serve-time policy:
// non-private instances require inbound authentication.
const (
	// AccessPrivate is a loopback-only instance; no inbound auth required.
	AccessPrivate = "private"
	// AccessProtected is reachable by remote callers on a trusted
	// network; inbound auth is mandatory.
	AccessProtected = "protected"
	// AccessPublic is internet-facing; inbound auth is mandatory.
	AccessPublic = "public"
)

// ServerConfig represents server configuration
type ServerConfig struct {
	Port     int  `mapstructure:"port" yaml:"port"`
	GRPCPort int  `mapstructure:"grpc_port" yaml:"grpc_port"`
	Workers  int  `mapstructure:"workers" yaml:"workers"`
	Public   bool `mapstructure:"public" yaml:"public"`
	// Access classifies the instance: "private" (default), "protected",
	// or "public". Empty means unset; EffectiveAccess resolves the
	// default and the legacy server.public shorthand.
	Access string `mapstructure:"access" yaml:"access"`
	// Auth selects and configures the inbound authentication provider.
	// Consumed through the internal/auth Provider interface so the
	// identity backend is an ops decision, never a rebuild.
	Auth AuthConfig `mapstructure:"auth" yaml:"auth"`
	// Quotas declares per-principal metering quotas enforced on the
	// entity-serving surface of non-private instances. Namespace
	// entitlement grants are data (entitlement rows keyed by principal
	// ID); quotas are operator config, applied at serve start.
	Quotas []ServerQuotaConfig `mapstructure:"quotas" yaml:"quotas"`

	// URL is the single dpkms instance clients route to when URLs is
	// empty (also settable per command with --server where the flag
	// exists). Validated at load: scheme http/https + host required.
	URL string `mapstructure:"url" yaml:"url,omitempty"`
	// URLs is the ordered client-routing list, primary first. Takes
	// precedence over URL. Each entry is a bare URL string or a
	// {url, token} mapping (see ServerEndpoint). Validated at load —
	// a malformed entry fails the load instead of silently probing as
	// a permanently "down" instance.
	URLs []ServerEndpoint `mapstructure:"urls" yaml:"urls,omitempty"`
	// Token is the default bearer token clients attach to requests
	// against instances whose URLs entry carries no token of its own.
	// Empty = unauthenticated.
	Token string `mapstructure:"token" yaml:"token,omitempty"`
}

// ServerQuotaConfig caps one principal's metered usage of the
// entity-serving surface for one event type per billing period
// (calendar month, UTC).
type ServerQuotaConfig struct {
	// Principal is the authenticated principal ID the quota applies to.
	Principal string `mapstructure:"principal" yaml:"principal"`
	// Event is the metered event type: entity_resolve | content_pull |
	// taxonomy_sync.
	Event string `mapstructure:"event" yaml:"event"`
	// Limit is the hard cap per billing period; 0 = unlimited.
	Limit int `mapstructure:"limit" yaml:"limit"`
	// WarnAt is the usage count that triggers a warning log
	// (0 = default 80% of Limit).
	WarnAt int `mapstructure:"warn_at" yaml:"warn_at"`
}

// EffectiveAccess resolves the instance access class. An explicit
// server.access always wins; the legacy server.public flag is shorthand
// for "public" when access is unset (deprecated, flagged by lint);
// otherwise the default is private.
func (s ServerConfig) EffectiveAccess() string {
	if s.Access != "" {
		return s.Access
	}
	if s.Public {
		return AccessPublic
	}
	return AccessPrivate
}

// AuthConfig selects the inbound authentication provider and its settings.
type AuthConfig struct {
	// Provider names the authentication backend. "static" is implemented;
	// "oidc" and "mtls" are reserved for future backends behind the same
	// interface. Empty = no inbound auth configured.
	Provider string `mapstructure:"provider" yaml:"provider"`
	// Static configures the static token/API-key provider.
	Static StaticAuthConfig `mapstructure:"static" yaml:"static"`
}

// StaticAuthConfig holds credentials for the static token provider.
type StaticAuthConfig struct {
	// Tokens maps bearer tokens / API keys to principals.
	Tokens []StaticTokenConfig `mapstructure:"tokens" yaml:"tokens"`
}

// StaticTokenConfig is one accepted credential and the principal it
// authenticates as.
type StaticTokenConfig struct {
	// Token is the shared secret presented by the caller.
	Token string `mapstructure:"token" yaml:"token"`
	// Principal is the stable identity assigned to callers of this token.
	Principal string `mapstructure:"principal" yaml:"principal"`
	// Roles grants coarse roles to the principal (e.g. "admin", "reader").
	Roles []string `mapstructure:"roles" yaml:"roles"`
}

// HasInboundAuth reports whether the auth config carries a usable
// credential set: a provider is selected and (for the static provider)
// at least one token is configured. Reserved providers count as
// configured here; their sub-config is validated at construction time.
func (a AuthConfig) HasInboundAuth() bool {
	switch a.Provider {
	case "":
		return false
	case "static":
		return len(a.Static.Tokens) > 0
	default:
		return true
	}
}

// ServerEndpoint is one client-routing target: a dpkms base URL plus an
// optional bearer token overriding the server.token default for that
// instance. In YAML an entry is either a bare string URL (no token) or a
// mapping:
//
//	server:
//	  urls:
//	    - http://127.0.0.1:8080
//	    - url: https://primary.example.net:7700
//	      token: s3cret
type ServerEndpoint struct {
	URL   string `mapstructure:"url" yaml:"url"`
	Token string `mapstructure:"token,omitempty" yaml:"token,omitempty"`
}

// UnmarshalYAML accepts both entry forms: a bare string URL and a
// {url, token} mapping.
func (e *ServerEndpoint) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		e.Token = ""
		return node.Decode(&e.URL)
	case yaml.MappingNode:
		type plain ServerEndpoint
		var p plain
		if err := node.Decode(&p); err != nil {
			return err
		}
		*e = ServerEndpoint(p)
		return nil
	default:
		return fmt.Errorf("server.urls entry must be a URL string or a {url, token} mapping")
	}
}

// MarshalYAML writes the bare-string form back when no token is set, so a
// config written back (schema migration) keeps its original shape.
func (e ServerEndpoint) MarshalYAML() (any, error) {
	if e.Token == "" {
		return e.URL, nil
	}
	type plain ServerEndpoint
	return plain(e), nil
}

// ProfileConfig represents profile configuration
type ProfileConfig struct {
	Default  string                  `mapstructure:"default" yaml:"default"`
	Profiles map[string]FocusProfile `mapstructure:"profiles" yaml:"profiles"`
}

// FocusProfile is a named configuration preset for a specific role or project.
type FocusProfile struct {
	// Default marks this profile as the server default. At most one profile
	// may have Default: true; it is equivalent to setting profile.default in
	// the top-level config. If both are set they must agree.
	Default bool `mapstructure:"default" yaml:"default,omitempty"`
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
	// Schema holds per-profile entity/topic vocabulary and classification rules.
	Schema ProfileSchema `mapstructure:"schema" yaml:"schema,omitempty"`
}

// ProfileSchema defines per-profile vocabulary constraints for metadata extraction.
type ProfileSchema struct {
	// Version is incremented on each schema mutation.
	Version int `mapstructure:"version" yaml:"version" json:"version"`
	// EntityTypes constrains the "type" field during metadata extraction.
	EntityTypes []string `mapstructure:"entity_types" yaml:"entity_types" json:"entity_types"`
	// TopicVocabulary constrains extracted topics to this vocabulary.
	TopicVocabulary []string `mapstructure:"topic_vocabulary" yaml:"topic_vocabulary" json:"topic_vocabulary"`
	// ClassificationRules map regex patterns to entity types.
	ClassificationRules []ClassificationRule `mapstructure:"classification_rules" yaml:"classification_rules" json:"classification_rules"`
}

// ClassificationRule maps a regex pattern to an entity type.
type ClassificationRule struct {
	Pattern string `mapstructure:"pattern" yaml:"pattern" json:"pattern"`
	Type    string `mapstructure:"type" yaml:"type" json:"type"`
}

// RegistriesGlobalConfig holds registry-wide settings that apply to all registries.
type RegistriesGlobalConfig struct {
	// RequireSignatures requires that every registry must declare a public_key and
	// that each sync response carries a valid Ed25519 signature.
	// Default: false (registries without a public_key are still allowed).
	RequireSignatures bool `mapstructure:"require_signatures" yaml:"require_signatures"`
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

// RegistryTrustLevel defines how much authority a registry has over the local graph.
//
//   - trusted   — may define entities, aliases, and overwrite local entities.
//   - untrusted — may be queried for lookup but entity writes require user approval.
//   - sandboxed — isolated; no writes to the local graph at all; lookup only.
type RegistryTrustLevel string

const (
	// RegistryTrustLevelTrusted grants full write access to the local graph.
	RegistryTrustLevelTrusted RegistryTrustLevel = "trusted"
	// RegistryTrustLevelUntrusted allows lookup but entities need approval before write.
	RegistryTrustLevelUntrusted RegistryTrustLevel = "untrusted"
	// RegistryTrustLevelSandboxed read-only; no graph writes regardless of approval.
	RegistryTrustLevelSandboxed RegistryTrustLevel = "sandboxed"
)

// RegistryConfig represents a registry configuration
type RegistryConfig struct {
	Name     string             `mapstructure:"name"      yaml:"name"`
	URL      string             `mapstructure:"url"       yaml:"url"`
	Auth     RegistryAuthConfig `mapstructure:"auth,omitempty" yaml:"auth,omitempty"`
	SyncMode RegistrySyncMode   `mapstructure:"sync_mode" yaml:"sync_mode"`
	// TrustLevel controls what the registry is allowed to do to the local graph.
	// Default (empty) is treated as "untrusted".
	// Valid values: "trusted" | "untrusted" | "sandboxed".
	TrustLevel RegistryTrustLevel `mapstructure:"trust_level,omitempty" yaml:"trust_level,omitempty"`
	// EntitlementURL is the endpoint to call before sync to verify access.
	// When empty, no entitlement check is performed.
	// Typically populated at runtime from the registry manifest; can also be
	// declared explicitly in config for override scenarios.
	EntitlementURL string `mapstructure:"entitlement_url,omitempty" yaml:"entitlement_url,omitempty"`
}

// EffectiveTrustLevel returns the trust level with a safe default of untrusted.
func (r RegistryConfig) EffectiveTrustLevel() RegistryTrustLevel {
	switch r.TrustLevel {
	case RegistryTrustLevelTrusted, RegistryTrustLevelUntrusted, RegistryTrustLevelSandboxed:
		return r.TrustLevel
	default:
		return RegistryTrustLevelUntrusted
	}
}

// PluginConfig represents a plugin configuration
type PluginConfig struct {
	Type   string                 `mapstructure:"type" yaml:"type"`
	Plugin string                 `mapstructure:"plugin" yaml:"plugin"`
	Config map[string]interface{} `mapstructure:"config" yaml:"config"`
}

// I18nConfig represents i18n configuration
type I18nConfig struct {
	Enabled            bool     `mapstructure:"enabled" yaml:"enabled"`
	PreferredLanguages []string `mapstructure:"preferred_languages" yaml:"preferred_languages"`
	AutoTranslate      bool     `mapstructure:"auto_translate" yaml:"auto_translate"`
	TranslateTags      bool     `mapstructure:"translate_tags" yaml:"translate_tags"`
}

// ProvidersConfig controls backend selection for each provider type.
type ProvidersConfig struct {
	Video         ProviderBackendConfig `mapstructure:"video" yaml:"video"`
	Document      ProviderBackendConfig `mapstructure:"document" yaml:"document"`
	OCR           ProviderBackendConfig `mapstructure:"ocr" yaml:"ocr"`
	Transcription ProviderBackendConfig `mapstructure:"transcription" yaml:"transcription"`
	Vision        ProviderBackendConfig `mapstructure:"vision" yaml:"vision"`
	Diarization   ProviderBackendConfig `mapstructure:"diarization" yaml:"diarization"`
	LLM           ProviderBackendConfig `mapstructure:"llm" yaml:"llm"`
	Embedding     ProviderBackendConfig `mapstructure:"embedding" yaml:"embedding"`
}

// ProviderBackendConfig selects which backend to use for a provider.
type ProviderBackendConfig struct {
	Backend string `mapstructure:"backend" yaml:"backend"`
	// Model overrides the default model for LLM-based providers (vision, transcription via ollama).
	Model string `mapstructure:"model,omitempty" yaml:"model,omitempty"`
	// Endpoint overrides the default API endpoint (e.g. Ollama URL).
	Endpoint string `mapstructure:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	// Language sets a default language hint (e.g. for OCR, transcription).
	Language string `mapstructure:"language,omitempty" yaml:"language,omitempty"`
}

// Load loads the configuration from file and environment variables.
//
// bin selects the per-binary config file under the shared `contexthelp/`
// namespace. Pass "ctxt" or "dpkms" — the cascade then looks for
// `contexthelp/<bin>.yaml` at every layer.
//
// cfgFile, when non-empty, takes precedence over the cascade and is loaded
// as the sole file source. When empty the legacy CTXT_CONFIG env var is
// honored, and otherwise the system → user → project cascade is walked.
//
// For -c/--config CLI integration prefer LoadWithOverrides, which lets
// callers wire kit/cli's ConfigArgs() output directly.
func Load(bin, cfgFile string) (*Config, error) {
	return LoadWithOverrides(bin, cfgFile, nil, nil)
}

// LoadWithOverrides is the full-fat entry point used by adopters wiring
// kit/cli's -c/--config global. extraPaths are loaded after the standard
// cascade (system → user → project) and before overrides; overrides win
// over every file layer. Pass paths/overrides from root.ConfigArgs().
//
// cfgFile retains the legacy single-file override semantic for tools
// that still wire it; new call sites should leave it empty and rely on
// the cascade plus extraPaths.
func LoadWithOverrides(bin, cfgFile string, extraPaths []string, overrides map[string]any) (*Config, error) {
	if bin == "" {
		return nil, fmt.Errorf("config.Load: bin name is required (e.g. \"ctxt\" or \"dpkms\")")
	}

	// Stage 1: viper holds defaults + env binds. Defaults seed cfg as a
	// baseline; env binds resolve at viper.Get* time so we can re-apply
	// them after the file merge (kit's EnvOverride hook).
	v := viper.New()
	v.SetConfigType("yaml")
	setDefaults(v)
	bindEnvVars(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal defaults: %w", err)
	}

	// Stage 2: kit/core/config layered file merge. The cascade slots are
	// explicit (no kitconfig.OptionsForTool here) because contexthelp
	// uses a per-binary file under a shared "contexthelp/" namespace
	// rather than a single tool-level config.yaml.
	system, user, project := cascadeSlots(bin)
	if cfgFile != "" {
		// Legacy single-file override: replace cascade with just this file.
		// Stays in ExtraConfigPaths (strict) — a caller naming a file
		// outright means it, so a missing one is an error.
		system, user, project = "", "", ""
		extraPaths = append([]string{cfgFile}, extraPaths...)
	} else if envCfg := os.Getenv(EnvConfigPath); envCfg != "" {
		// CTXT_CONFIG goes in the `user` SLOT, not ExtraConfigPaths:
		// kit's loader tolerates a missing cascade slot but treats a
		// missing extra path as a hard error. This env var is ambient
		// rather than per-invocation, and pointing it at a not-yet-
		// created file is the normal first-run state — `ctxt setup` is
		// the command that CREATES it.
		//
		// Explicit -c paths keep flowing through extraPaths, where
		// strictness is correct: ParseConfigArgs has already proven
		// those files exist, and -c still layers after (wins over) this.
		system, user, project = "", envCfg, ""
	}

	// envOverride re-applies env-bound viper keys to dst after files merge,
	// preserving viper's "env > file" precedence. Without this, a yaml file
	// with `server.port: 4242` would silently override `CH_SERVER_PORT=9999`.
	envOverride := func(dst any) {
		applyEnvOverrides(v, dst.(*Config))
	}

	if err := kitconfig.Load(&cfg, kitconfig.Options{
		SystemConfigPath:  system,
		UserConfigPath:    user,
		ProjectConfigPath: project,
		ExtraConfigPaths:  extraPaths,
		EnvOverride:       envOverride,
		Overrides:         overrides,
	}); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Sync FocusProfile.Default bool → ProfileConfig.Default string.
	if err := syncProfileDefault(&cfg); err != nil {
		return nil, err
	}

	// Reject malformed client-routing endpoints loudly at load time — a
	// bad server.url/server.urls entry would otherwise probe as a
	// permanently "down" instance and silently shift traffic to the next
	// instance or the local fallback.
	if err := cfg.validateServerEndpoints(); err != nil {
		return nil, err
	}

	// Run migrations if needed. Write-back targets the highest-precedence
	// file that actually contributed to the merge (project > user > system),
	// or the explicit cfgFile/CTXT_CONFIG path when one was used.
	if cfg.Version < currentSchemaVersion {
		if migrate(&cfg) {
			if cfgPath := writeBackTarget(cfgFile, system, user, project); cfgPath != "" {
				_ = WriteBack(&cfg, cfgPath)
			}
		}
	}

	return &cfg, nil
}

// writeBackTarget picks the file to write a migrated config to. Priority:
// explicit override (cfgFile or CTXT_CONFIG) > project > user > system.
// Returns "" when nothing on the cascade existed.
func writeBackTarget(cfgFile, system, user, project string) string {
	if cfgFile != "" {
		return cfgFile
	}
	if env := os.Getenv(EnvConfigPath); env != "" {
		return env
	}
	for _, p := range []string{project, user, system} {
		if p == "" {
			continue
		}
		// #nosec G703 -- p is a config-file candidate from the XDG
		// search path or an explicit --config value, both operator
		// supplied. Stat reads metadata only.
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// cascadeSlots returns the system/user/project paths for the given bin.
// Empty strings mean "no path resolvable" (e.g. xdg failure, no project
// marker found); kit/core/config.Load skips empty slots.
func cascadeSlots(bin string) (system, user, project string) {
	return CascadeSlots(bin)
}

// CascadeSlots is the exported counterpart to [cascadeSlots], usable by
// adopters wiring `<bin> config path` / `<bin> config paths` via
// kit/console/cli/config.RegisterPathSubcommands. It is the source of
// truth for the system/user/project cascade walked by
// [LoadWithOverrides], so resolvers built on top stay in sync.
//
// Layers (highest precedence first when consumed by callers):
//
//   - project: nearest `.contexthelp/<bin>.yaml` walking up from cwd,
//     stopping at $HOME or fs root. Empty when no marker is found.
//   - user:    `$XDG_CONFIG_HOME/contexthelp/<bin>.yaml`. Empty when xdg
//     resolution fails (e.g. no $HOME).
//   - system:  `/etc/contexthelp/<bin>.yaml`. Always populated.
//
// `$CTXT_CONFIG` and `-c/--config` are not part of the cascade — they
// short-circuit it. Callers building a resolver for `config path(s)` must
// prepend those overrides before this chain.
func CascadeSlots(bin string) (system, user, project string) {
	system = filepath.Join("/etc", xdgTool, bin+".yaml")
	if dir, err := configDirXDG(); err == nil && dir != "" {
		user = filepath.Join(dir, bin+".yaml")
	}
	project = walkUpForMarker(bin)
	return
}

// WalkUpForMarker is the exported counterpart to [walkUpForMarker].
// Exported so adopters building a custom `config path(s)` resolver can
// reproduce the project-layer walk-up against an explicit cwd (e.g.
// kit/console/cli/config's `--from <dir>` flag) rather than the process
// cwd.
func WalkUpForMarker(cwd, bin string) string {
	if cwd == "" {
		return walkUpForMarker(bin)
	}
	home, _ := os.UserHomeDir()
	marker := filepath.Join(".contexthelp", bin+".yaml")
	dir := cwd
	for {
		candidate := filepath.Join(dir, marker)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		if dir == home || dir == "/" || dir == filepath.Dir(dir) {
			return ""
		}
		dir = filepath.Dir(dir)
	}
}

// syncProfileDefault reconciles FocusProfile.Default bool with
// ProfileConfig.Default string. Rules:
//   - At most one FocusProfile may have Default:true.
//   - If one does, and ProfileConfig.Default is empty, set it.
//   - If both are set they must agree; conflict is an error.
func syncProfileDefault(cfg *Config) error {
	var inlineDefault string
	for name, p := range cfg.Profile.Profiles {
		if p.Default {
			if inlineDefault != "" {
				return fmt.Errorf("config: profiles %q and %q both have default:true; only one may be default", inlineDefault, name)
			}
			inlineDefault = name
		}
	}
	if inlineDefault == "" {
		return nil
	}
	if cfg.Profile.Default == "" {
		cfg.Profile.Default = inlineDefault
		return nil
	}
	if cfg.Profile.Default != inlineDefault {
		return fmt.Errorf("config: profile.default %q conflicts with profile %q default:true", cfg.Profile.Default, inlineDefault)
	}
	return nil
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
	dataDir, err := dataDirXDG()
	if err != nil {
		// Fallback only when XDG resolution fails (e.g. no $HOME).
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share", "contexthelp")
	}

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

	// Registries global — require_signatures is off by default (permissive).
	v.SetDefault("registries_global.require_signatures", false)

	// Resurfacing defaults
	v.SetDefault("resurfacing.enabled", true)
	v.SetDefault("resurfacing.max_items", 10)
	v.SetDefault("resurfacing.min_score", 0.4)
	v.SetDefault("resurfacing.run_interval", time.Hour)

	// Browser defaults — disabled by default; Playwright/Chromium is heavy.
	v.SetDefault("browser.enabled", false)
	v.SetDefault("browser.binary", "ibr")
	v.SetDefault("browser.port", 0)
	v.SetDefault("browser.max_clients", 3)
	v.SetDefault("browser.headless", true)

	// Fan-out enrichment defaults — enabled by default.
	v.SetDefault("fanout.enabled", true)
	v.SetDefault("fanout.entities", true)
	v.SetDefault("fanout.audit_log", true)

	// Security alerting defaults
	v.SetDefault("security.alerts.auth_failure_threshold", 3)
	v.SetDefault("security.alerts.acl_denial_threshold", 10)
	v.SetDefault("security.alerts.smtp.port", 587)
}

// bindEnvVars binds environment variables to configuration keys
func bindEnvVars(v *viper.Viper) {
	v.SetEnvPrefix("CH")
	v.AutomaticEnv()

	// Single source of truth for env-binding lives in envBindings; this loop
	// wires each entry into viper so code paths still calling viper.Get* see
	// env values, and applyEnvOverrides walks the same list to enforce
	// env > file precedence on the typed Config after kit's file merge.
	for _, b := range envBindings {
		_ = v.BindEnv(b.Key, b.Env)
	}
}

// envBindings is the registry of (dotted_key, env_var) pairs honored by
// LoadWithOverrides for the env > file precedence layer. Keep in sync with
// bindEnvVars — the latter wires viper for code paths that still call
// viper.Get*; this list drives env > file overlay on the typed Config.
var envBindings = []struct{ Key, Env string }{
	{"storage.type", "CH_STORAGE_TYPE"},
	{"storage.path", EnvDataDir},
	{"storage.blob.backend", "CTXT_BLOB_BACKEND"},
	{"storage.blob.threshold", "CTXT_BLOB_THRESHOLD"},
	{"storage.blob.s3.endpoint", "CTXT_BLOB_S3_ENDPOINT"},
	{"storage.blob.s3.region", "CTXT_BLOB_S3_REGION"},
	{"storage.blob.s3.bucket", "CTXT_BLOB_S3_BUCKET"},
	{"storage.blob.s3.prefix", "CTXT_BLOB_S3_PREFIX"},
	{"storage.blob.s3.access_key", "CTXT_BLOB_S3_ACCESS_KEY"},
	{"storage.blob.s3.secret_key", "CTXT_BLOB_S3_SECRET_KEY"},
	{"server.port", "CH_SERVER_PORT"},
	{"server.grpc_port", "CH_GRPC_PORT"},
	{"server.workers", EnvDPKMSWorkers},
	{"server.public", "CH_PUBLIC"},
	{"profile.default", EnvProfile},
	{"jobs.poll_interval", "DPKMS_POLL_INTERVAL"},
	{"jobs.stale_timeout", "DPKMS_STALE_TIMEOUT"},
	{"jobs.max_retries", "DPKMS_MAX_RETRIES"},
	{"jobs.max_hops", "DPKMS_MAX_HOPS"},
	{"secrets.backend", "CTXT_SECRETS_BACKEND"},
	{"secrets.age_file", "CTXT_AGE_FILE"},
	{"secrets.age_identity_file", "CTXT_AGE_IDENTITY"},
	{"browser.enabled", "CTXT_BROWSER_ENABLED"},
	{"browser.binary", "CTXT_BROWSER_BINARY"},
	{"browser.port", "CTXT_BROWSER_PORT"},
}

// applyEnvOverrides re-applies env-bound values on top of cfg after kit's
// file merge, so env > file precedence holds. Routes through kit's override
// machinery (ParseOverrides → ApplyOverrides) so dotted keys land in the
// same nested-map shape that file-based overrides do.
func applyEnvOverrides(_ *viper.Viper, cfg *Config) {
	pairs := make([]string, 0, len(envBindings))
	for _, b := range envBindings {
		val, ok := os.LookupEnv(b.Env)
		if !ok || val == "" {
			continue
		}
		pairs = append(pairs, b.Key+"="+val)
	}
	if len(pairs) == 0 {
		return
	}
	_, overrides, err := kitconfig.ParseConfigArgs(pairs)
	if err != nil {
		// Malformed key=value can only come from a bug in envBindings, not
		// user input — best-effort skip rather than fail Load.
		return
	}
	_ = kitconfig.ApplyOverrides(cfg, overrides)
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

const xdgTool = "contexthelp"

// configDirXDG returns the XDG-resolved config directory for contexthelp,
// using kit/xdg's RawConfigDir (the guard-respecting ConfigDir is unsuitable
// here because it errors when the dir doesn't exist yet).
func configDirXDG() (string, error) {
	return xdg.RawConfigDir(xdgTool)
}

// dataDirXDG returns the XDG-resolved data directory for contexthelp.
func dataDirXDG() (string, error) {
	return xdg.RawDataDir(xdgTool)
}

// walkUpForMarker walks up from cwd looking for `.contexthelp/<bin>.yaml`,
// stopping at $HOME or fs root. Returns "" if cwd is unobtainable or no
// marker is found.
func walkUpForMarker(bin string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	home, _ := os.UserHomeDir()
	marker := filepath.Join(".contexthelp", bin+".yaml")

	dir := cwd
	for {
		candidate := filepath.Join(dir, marker)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		if dir == home || dir == "/" || dir == filepath.Dir(dir) {
			return ""
		}
		dir = filepath.Dir(dir)
	}
}

// GetConfigPath returns the user-layer configuration file path for the
// given binary. Resolution: $CTXT_CONFIG > $XDG_CONFIG_HOME/contexthelp/<bin>.yaml.
func GetConfigPath(bin string) string {
	if cfgPath := os.Getenv(EnvConfigPath); cfgPath != "" {
		return cfgPath
	}
	dir, err := configDirXDG()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, bin+".yaml")
}

// EnsureConfigDir ensures the configuration directory for the given
// binary exists.
func EnsureConfigDir(bin string) error {
	configPath := GetConfigPath(bin)
	if configPath == "" {
		return fmt.Errorf("failed to determine config path")
	}
	configDir := filepath.Dir(configPath)
	return os.MkdirAll(configDir, 0750)
}

// EnsureDataDir ensures the data directory exists.
// Resolution order: $CTXT_DATA_DIR > kit/xdg DataDir.
func EnsureDataDir() error {
	dataDir := os.Getenv(EnvDataDir)
	if dataDir == "" {
		var err error
		dataDir, err = dataDirXDG()
		if err != nil {
			return fmt.Errorf("data dir: %w", err)
		}
	}
	// #nosec G703 -- dataDir derives from XDG_DATA_HOME or the
	// operator's config, not from captured content or a request.
	return os.MkdirAll(dataDir, 0750)
}

// RunDir returns the directory used for runtime files (pidfiles).
// Resolution order: $CTXT_DATA_DIR/run > kit/xdg DataDir/run.
func RunDir() (string, error) {
	base := os.Getenv(EnvDataDir)
	if base == "" {
		var err error
		base, err = dataDirXDG()
		if err != nil {
			return "", fmt.Errorf("data dir: %w", err)
		}
	}
	dir := filepath.Join(base, "run")
	// #nosec G703 -- base is the operator's XDG state/runtime dir.
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("run dir: %w", err)
	}
	return dir, nil
}

// CurrentInstanceFile returns the path to the state file that stores the
// user's chosen current instance name (written by `ctxt instance use`).
// Stored alongside pidfiles: $XDG_DATA_HOME/contexthelp/run/current-instance
func CurrentInstanceFile() (string, error) {
	dir, err := RunDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "current-instance"), nil
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
	// DrainTimeout is how long to wait for in-flight jobs to finish on
	// graceful shutdown. Defaults to 30s if zero.
	DrainTimeout time.Duration `mapstructure:"drain_timeout" yaml:"drain_timeout"`
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
	//
	// Canonical values (preferred, match hop.top/kit/go/storage/secret):
	//   "env" (default), "keyring", "agefile", "onepassword", "ghsecrets".
	//
	// Deprecated aliases (still accepted with one-time deprecation warning):
	//   "keychain"  -> "keyring"
	//   "age-file"  -> "agefile"
	//   "1password" -> "onepassword"
	//   "gh-secrets" -> "ghsecrets"
	Backend string `mapstructure:"backend" yaml:"backend"`
	// AgeFile is the path to an age-encrypted YAML secrets file.
	// Only used when Backend == "agefile".
	AgeFile string `mapstructure:"age_file" yaml:"age_file"`
	// AgeIdentityFile is the path to the age identity (private key) file.
	AgeIdentityFile string `mapstructure:"age_identity_file" yaml:"age_identity_file"`
	// KeychainService is the macOS/Linux keychain service name.
	// Defaults to "ctxt".
	KeychainService string `mapstructure:"keychain_service" yaml:"keychain_service"`
	// OnePasswordVault is the 1Password vault name to read secrets from.
	// Only used when Backend == "onepassword". Requires `op` CLI.
	OnePasswordVault string `mapstructure:"onepassword_vault" yaml:"onepassword_vault"`
	// GHRepo is the GitHub repository (owner/repo) for the ghsecrets backend.
	// Only used when Backend == "ghsecrets". Defaults to current repo if empty.
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
